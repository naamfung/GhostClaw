package main

import (
        "fmt"
        "log"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "sync"
        "time"
)

// ============================================================================
// Plan Mode - 程序強制的結構化任務分解（靈感來自 cc-mini）
// ============================================================================
//
// 核心設計：
//   - 3 個 Phase 由程序狀態機控制，LLM 無法跳過
//   - Phase 2 支援 prev_phase 回溯（最多 2 次）
//   - 每 Phase 暴露不同工具集（工具分階段控制）
//   - 每 Phase 有獨立的 todos 列表追蹤子任務
//   - Phase 轉換需調用 next_phase 工具
//   - Phase 3 自動退出，計劃步驟注入會話歷史
//
// Phase 工具映射：
//   Phase 1 (探索): 只讀 + spawn + todos + next_phase
//   Phase 2 (設計): Phase 1 全部 + PlanWrite + PlanRead + prev_phase
//   Phase 3 (執行): 僅 next_phase（自動退出）
// ============================================================================

// PlanPhase Plan Mode 階段
type PlanPhase int

const (
        PlanPhaseInactive PlanPhase = 0 // 未激活
        PlanPhaseExplore  PlanPhase = 1 // Phase 1: 探索（只讀探索）
        PlanPhaseDesign   PlanPhase = 2 // Phase 2: 設計（合併舊 Review + Plan）
        PlanPhaseExecute  PlanPhase = 3 // Phase 3: 執行（自動退出，注入計劃）
)

// Plan Mode 超時配置
const (
        planPhaseTimeout       = 5 * time.Minute  // 單階段最大持續時間
        planTotalTimeout       = 20 * time.Minute // Plan Mode 總最大持續時間
)

const maxDowngrades = 2 // 最大回溯次數（Phase 2 → Phase 1）

// PlanMode Plan 模式管理器
type PlanMode struct {
        Phase         PlanPhase
        PlanFilePath  string
        StartTime     time.Time
        PhaseStart    time.Time // 當前階段開始時間
        TaskDesc      string    // 用戶的原始任務描述
        mu            sync.RWMutex
        TimedOut      bool      // 是否因超時而被強制退出
        DowngradeCount int      // 回溯次數計數器（每次 prev_phase +1）
        stopTimeout    chan struct{} // 關閉時取消 timeout goroutine
}

var globalPlanMode = &PlanMode{
        Phase: PlanPhaseInactive,
}

// ============================================================================
// Phase 元數據
// ============================================================================

type phaseInfo struct {
        Name        string
        Description string
        // Tools allowed in this phase (beyond the static tier-filtered tools)
        ExtraTools []string
}

var phaseMetadata = map[PlanPhase]phaseInfo{
        PlanPhaseExplore: {
                Name:        "Phase 1: 探索",
                Description: "使用只讀工具探索專案文件，建立整體認識。善用 spawn 並行探索不同模塊。如發現需要更多探索，Phase 2 可使用 prev_phase 回溯。",
                ExtraTools: []string{
                        "Spawn", "SpawnCheck", "SpawnList",
                        "Todos", // 管理 Phase 1 子任務
                },
        },
        PlanPhaseDesign: {
                Name:        "Phase 2: 設計",
                Description: "設計實現方案、草擬計劃、審查可行性、編寫最終計劃。可使用 PlanWrite/PlanRead，必要時用 prev_phase 回溯探索。",
                ExtraTools: []string{
                        "PlanWrite", "PlanRead",
                        "Spawn", "SpawnCheck", "SpawnList",
                        "Todos",
                },
        },
        PlanPhaseExecute: {
                Name:        "Phase 3: 執行",
                Description: "系統自動處理退出。恢復完整工具訪問，計劃注入會話歷史。",
                ExtraTools:  []string{},
        },
}

// PhaseReadTools 在 Plan Mode 所有 Phase 中始終可用的只讀基礎工具
var PhaseReadTools = []string{
        "ReadFileLine",
        "ReadFileRange",
        "ReadAllLines",
        "TextSearch",
        "TextGrep",
        "MemoryRecall",
        "MemoryList",
}

// ============================================================================
// 狀態查詢
// ============================================================================

func (pm *PlanMode) IsActive() bool {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.Phase != PlanPhaseInactive
}

func (pm *PlanMode) CurrentPhase() PlanPhase {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.Phase
}

func (pm *PlanMode) PhaseName() string {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.phaseNameLocked()
}

// phaseNameLocked 返回當前 Phase 名稱（調用者需持有鎖）
// 用於已在持鎖上下文中調用的場景，避免重入死鎖
func (pm *PlanMode) phaseNameLocked() string {
        if info, ok := phaseMetadata[pm.Phase]; ok {
                return info.Name
        }
        return "未激活"
}

// ============================================================================
// 進入 / 退出 Plan Mode
// ============================================================================

// EnterPlanMode 進入 Plan Mode，從 Phase 1 開始
func EnterPlanMode(taskDesc string) string {
        globalPlanMode.mu.Lock()
        defer globalPlanMode.mu.Unlock()

        globalPlanMode.Phase = PlanPhaseExplore
        globalPlanMode.StartTime = time.Now()
        globalPlanMode.PhaseStart = time.Now()
        globalPlanMode.TaskDesc = taskDesc
        globalPlanMode.DowngradeCount = 0

        dataDir := getDataDir()
        globalPlanMode.PlanFilePath = filepath.Join(dataDir, "plan.md")

        // 初始化整體 Plan todos（作為 list_id="plan"）
        _, _ = TODO.Update([]TodoItem{
                {ID: "1", Text: "Phase 1: 探索", Status: "InProgress"},
                {ID: "2", Text: "Phase 2: 設計", Status: "Pending"},
                {ID: "3", Text: "Phase 3: 執行", Status: "Pending"},
        }, "plan")

        // 啟動 timeout 監控 goroutine（總超時 20 分鐘後強制退出）
        globalPlanMode.stopTimeout = make(chan struct{})
        go func(stop <-chan struct{}) {
                ticker := time.NewTicker(30 * time.Second)
                defer ticker.Stop()
                for {
                        select {
                        case <-stop:
                                return
                        case <-ticker.C:
                                if timedOut, _, _ := globalPlanMode.CheckPhaseTimeout(); timedOut {
                                        content := ForceExitPlanMode("超時強制退出")
                                        log.Printf("[PlanMode] Timeout goroutine: force exited, plan content=%d bytes", len(content))
                                        return
                                }
                        }
                }
        }(globalPlanMode.stopTimeout)

        log.Printf("[PlanMode] 進入 Plan Mode, Phase=1(探索), 任務: %.80s", taskDesc)
        return ""
}

// exitPlanModeLocked 內部退出邏輯（調用者需持有 globalPlanMode.mu）
// 返回計劃文件內容
func exitPlanModeLocked() string {
        content := ""
        if globalPlanMode.PlanFilePath != "" {
                data, err := os.ReadFile(globalPlanMode.PlanFilePath)
                if err == nil {
                        content = string(data)
                }
        }

        elapsed := time.Since(globalPlanMode.StartTime)
        log.Printf("[PlanMode] 退出 Plan Mode, 耗時 %v", elapsed)

        // 停止 timeout goroutine
        if globalPlanMode.stopTimeout != nil {
                close(globalPlanMode.stopTimeout)
                globalPlanMode.stopTimeout = nil
        }

        globalPlanMode.Phase = PlanPhaseInactive
        globalPlanMode.PlanFilePath = ""
        globalPlanMode.TaskDesc = ""

        return content
}

// postExitPlanMode 退出後的後處理（不需持有鎖）
// 將計劃注入會話歷史，清理 Plan todos
func postExitPlanMode(content string) {
        if content != "" {
                session := GetGlobalSession()
                planSystemMsg := fmt.Sprintf("[執行計劃 - 來自 Plan Mode]\n%s\n[計劃結束]", content)
                session.AddToHistory("system", planSystemMsg)
                log.Printf("[PlanMode] 計劃已注入會話歷史（%d 字符）", len(content))
        }
        // 清理所有 Plan Mode 相關 todos，防止洩漏
        _ = TODO.Clear("plan")
        _ = TODO.Clear("phase1")
        _ = TODO.Clear("phase2")
}

// ExitPlanMode 退出 Plan Mode，返回計劃文件內容
// 將計劃作為 system 消息注入會話歷史
func ExitPlanMode() string {
        globalPlanMode.mu.Lock()
        content := exitPlanModeLocked()
        globalPlanMode.mu.Unlock()

        postExitPlanMode(content)
        return content
}

// ============================================================================
// Phase 轉換（程序強制）
// ============================================================================

// AdvancePhase 推進到下一 Phase
// 返回 (newPhaseName, transitionMessage, error)
func AdvancePhase() (string, string, error) {
        globalPlanMode.mu.Lock()

        currentPhase := globalPlanMode.Phase

        // Phase 3 是終態，由程序自動處理退出
        if currentPhase >= PlanPhaseExecute {
                globalPlanMode.mu.Unlock()
                return "", "", fmt.Errorf("已是最終階段，無法繼續推進")
        }

        nextPhase := currentPhase + 1
        globalPlanMode.Phase = nextPhase
        globalPlanMode.PhaseStart = time.Now()

        // 更新整體 Plan todos
        updatePlanTodos(int(nextPhase))

        oldInfo := phaseMetadata[currentPhase]
        newInfo := phaseMetadata[nextPhase]

        log.Printf("[PlanMode] Phase 轉換: %s → %s", oldInfo.Name, newInfo.Name)

        // Phase 3 自動退出（使用內部鎖安全方法，避免 unlock/re-lock 競態）
        if nextPhase == PlanPhaseExecute {
                content := exitPlanModeLocked()
                globalPlanMode.mu.Unlock()
                // 退出後處理（不需持有鎖）
                postExitPlanMode(content)
                var msg string
                if content != "" {
                        msg = fmt.Sprintf("Plan Mode 已退出。計劃如下：\n\n%s\n\n現在你可以使用所有工具來執行計劃。",
                                content)
                } else {
                        msg = "Plan Mode 已退出。現在你可以使用所有工具。"
                }
                return "已退出", msg, nil
        }

        msg := fmt.Sprintf("已進入 %s\n%s\n\n可用工具已更新。",
                newInfo.Name, newInfo.Description)

        globalPlanMode.mu.Unlock()
        return newInfo.Name, msg, nil
}

// PrevPhase 回溯到上一 Phase（僅 Phase 2 → Phase 1）
// 返回 (newPhaseName, transitionMessage, error)
func PrevPhase() (string, string, error) {
        globalPlanMode.mu.Lock()
        defer globalPlanMode.mu.Unlock()

        // 僅在 Phase 2 可用
        if globalPlanMode.Phase != PlanPhaseDesign {
                return "", "", fmt.Errorf("prev_phase 僅在 Phase 2（設計）可用，當前階段不能回溯")
        }

        // 檢查回溯次數上限
        if globalPlanMode.DowngradeCount >= maxDowngrades {
                return "", "", fmt.Errorf("已達最大回溯次數（%d 次）。請基於現有探索結果繼續設計。", maxDowngrades)
        }

        // 回溯到 Phase 1
        globalPlanMode.Phase = PlanPhaseExplore
        globalPlanMode.PhaseStart = time.Now()
        globalPlanMode.DowngradeCount++

        remaining := maxDowngrades - globalPlanMode.DowngradeCount

        // 更新整體 Plan todos
        updatePlanTodos(int(PlanPhaseExplore))

        log.Printf("[PlanMode] Phase 回溯: Phase 2 → Phase 1（剩餘回溯次數：%d）", remaining)

        msg := fmt.Sprintf("已回溯到 %s\n\n需要進一步探索專案文件以補充設計所需信息。使用只讀工具繼續探索。\n\n剩餘回溯次數：%d/%d",
                phaseMetadata[PlanPhaseExplore].Name, remaining, maxDowngrades)

        return phaseMetadata[PlanPhaseExplore].Name, msg, nil
}

// updatePlanTodos 更新整體 Plan 進度 todos
// 需要在已持有 globalPlanMode.mu 時調用
func updatePlanTodos(completedPhase int) {
        items := make([]TodoItem, 3)
        for i := 1; i <= 3; i++ {
                status := "Pending"
                if i < completedPhase {
                        status = "Completed"
                } else if i == completedPhase {
                        status = "InProgress"
                }
                phaseName := getPhaseTodoText(i)
                items[i-1] = TodoItem{ID: fmt.Sprintf("%d", i), Text: phaseName, Status: status}
        }
        _, _ = TODO.Update(items, "plan")
}

func getPhaseTodoText(phase int) string {
        names := map[int]string{
                1: "Phase 1: 探索",
                2: "Phase 2: 設計",
                3: "Phase 3: 執行",
        }
        if name, ok := names[phase]; ok {
                return name
        }
        return fmt.Sprintf("Phase %d", phase)
}

// ============================================================================
// 工具權限控制（分階段）
// ============================================================================

// IsToolAllowedInPlanMode 檢查工具是否在當前 Phase 允許
func (pm *PlanMode) IsToolAllowedInPlanMode(toolName string) bool {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        if pm.Phase == PlanPhaseInactive {
                return true
        }

        // next_phase 始終可用（用於推進階段）
        if toolName == "NextPhase" {
                return true
        }

        // ExitPlanMode 始終可用（用於手動退出 Plan Mode）
        if toolName == "ExitPlanMode" {
                return true
        }

        // prev_phase 僅在 Phase 2 可用
        if toolName == "PrevPhase" && pm.Phase == PlanPhaseDesign {
                return true
        }

        // 獲取當前 Phase 允許的工具集
        allowed := pm.getToolsForCurrentPhase()
        for _, t := range allowed {
                if t == toolName {
                        return true
                }
        }
        return false
}

// getToolsForCurrentPhase 獲取當前 Phase 的完整允許工具列表
func (pm *PlanMode) getToolsForCurrentPhase() []string {
        info, ok := phaseMetadata[pm.Phase]
        if !ok {
                return nil
        }

        // 基礎只讀工具 + Phase 額外工具
        tools := make([]string, 0, len(PhaseReadTools)+len(info.ExtraTools))
        tools = append(tools, PhaseReadTools...)
        tools = append(tools, info.ExtraTools...)

        return tools
}

// GetToolsForCurrentPhase 外部接口：獲取當前 Phase 動態工具定義
func GetToolsForCurrentPhase() []map[string]interface{} {
        globalPlanMode.mu.RLock()
        phase := globalPlanMode.Phase
        taskDesc := globalPlanMode.TaskDesc
        globalPlanMode.mu.RUnlock()

        if phase == PlanPhaseInactive {
                return nil
        }

        // 始終注入 next_phase 工具
        tools := []map[string]interface{}{
                nextPhaseToolDef(),
        }

        // 根據 Phase 注入對應的動態工具
        switch phase {
        case PlanPhaseExplore:
                // Phase 1: 只讀 + spawn + todos
                tools = append(tools, spawnToolDefs()...)
                tools = append(tools, todosToolDef("phase1", "管理 Phase 1 探索子任務"))
        case PlanPhaseDesign:
                // Phase 2: next_phase + prev_phase + PlanWrite/read + spawn + todos
                tools = append(tools, prevPhaseToolDef())
                tools = append(tools, planWriteToolDef(), planReadToolDef())
                tools = append(tools, spawnToolDefs()...)
                tools = append(tools, todosToolDef("phase2", "管理 Phase 2 設計子任務"))
        case PlanPhaseExecute:
                // Phase 3: 僅 next_phase（自動退出），不額外注入
        }

        // 附加任務上下文到 next_phase 的描述中
        if taskDesc != "" && len(tools) > 0 {
                if fn, ok := tools[0]["function"].(map[string]interface{}); ok {
                        desc, _ := fn["description"].(string)
                        fn["description"] = desc + "\n\n當前任務：" + truncateStr(taskDesc, 200)
                }
        }

        return tools
}

// ============================================================================
// 系統提示生成（分階段動態）
// ============================================================================

// GetPlanModeSystemPrompt 返回當前 Phase 的系統提示
func GetPlanModeSystemPrompt() string {
        globalPlanMode.mu.RLock()
        phase := globalPlanMode.Phase
        taskDesc := globalPlanMode.TaskDesc
        phaseStart := globalPlanMode.PhaseStart
        downgradeCount := globalPlanMode.DowngradeCount
        globalPlanMode.mu.RUnlock()

        if phase == PlanPhaseInactive {
                return ""
        }

        info, ok := phaseMetadata[phase]
        if !ok {
                return ""
        }

        elapsed := time.Since(phaseStart).Round(time.Second)

        var sb strings.Builder

        sb.WriteString(fmt.Sprintf("[Plan Mode - %s]\n", info.Name))
        sb.WriteString(fmt.Sprintf("當前階段已持續 %v\n\n", elapsed))

        if taskDesc != "" {
                sb.WriteString(fmt.Sprintf("任務：%s\n\n", taskDesc))
        }

        // 整體進度
        sb.WriteString("整體進度：\n")
        for p := PlanPhaseExplore; p <= PlanPhaseExecute; p++ {
                metadata := phaseMetadata[p]
                if p < phase {
                        sb.WriteString(fmt.Sprintf("  [x] %s\n", metadata.Name))
                } else if p == phase {
                        sb.WriteString(fmt.Sprintf("  [>] %s ← 當前\n", metadata.Name))
                } else {
                        sb.WriteString(fmt.Sprintf("  [ ] %s\n", metadata.Name))
                }
        }
        sb.WriteString("\n")

        // 回溯狀態（僅在 Phase 2 顯示）
        if phase == PlanPhaseDesign {
                sb.WriteString(fmt.Sprintf("回溯狀態：已用 %d/%d 次\n\n", downgradeCount, maxDowngrades))
        }

        // 當前階段指引
        switch phase {
        case PlanPhaseExplore:
                sb.WriteString(explorePhasePrompt())
        case PlanPhaseDesign:
                sb.WriteString(designPhasePrompt())
        case PlanPhaseExecute:
                sb.WriteString(executePhasePrompt())
        }

        // 通用提醒
        sb.WriteString("\n完成當前階段的工作後，調用 next_phase 推進到下一階段。")

        return sb.String()
}

func explorePhasePrompt() string {
        return `## Phase 1: 探索

目標：充分理解任務涉及的文件結構和依賴關係。

操作指引：
1. 使用 TextSearch / TextGrep 搜索關鍵詞，定位相關文件
2. 使用 ReadFileLine / ReadFileRange / ReadAllLines 閱讀相關文件
3. 對於複雜任務，使用 spawn 創建最多 3 個並行子代理探索不同方面
4. 使用 todos 工具管理探索子任務

探索要點：
- 項目整體結構是什麼？
- 需要修改哪些文件？每個文件的職責是什麼？
- 有哪些依賴和約束？
- 是否有類似的現有實現可以參考？

完成探索後，調用 next_phase 進入設計階段。`
}

func designPhasePrompt() string {
        return `## Phase 2: 設計

目標：基於 Phase 1 的探索結果，設計實現方案並編寫最終計劃。

操作指引：
1. 綜合探索發現，設計實現方案
2. 使用 PlanWrite 草擬計劃 — 可多次修改迭代
3. 使用 PlanRead 查看已寫的計劃
4. 重新審查關鍵文件，驗證方案可行性
5. 確認無誤後，編寫最終計劃（覆蓋草稿）
6. 使用 todos 工具管理設計子任務

計劃格式（必須包含）：
  ## Context（上下文）
  你已了解的信息、涉及哪些文件和模塊

  ## Approach（方案）
  按步驟列出要執行的操作，具體到文件路徑和位置

  ## Verification（驗證方式）
  如何驗證修改正確性

如有遺漏信息，可使用 prev_phase 回溯到 Phase 1 繼續探索。

完成設計後，調用 next_phase 退出 Plan Mode 並開始執行。`
}

func executePhasePrompt() string {
        return `## Phase 3: 執行

系統正在處理退出...
調用 next_phase 完成退出。退出後：
- 所有工具訪問權限恢復
- 計劃內容將注入會話歷史作為執行指引`
}

// ============================================================================
// 超時檢查（程序強制退出保護）
// ============================================================================

// CheckPhaseTimeout 檢查當前階段是否已超時
// 返回 (超時, 當前階段已持續時間, 總已持續時間)
func (pm *PlanMode) CheckPhaseTimeout() (bool, time.Duration, time.Duration) {
        pm.mu.RLock()
        defer pm.mu.RUnlock()

        if pm.Phase == PlanPhaseInactive {
                return false, 0, 0
        }

        phaseElapsed := time.Since(pm.PhaseStart)
        totalElapsed := time.Since(pm.StartTime)

        // 單階段超時
        if phaseElapsed > planPhaseTimeout {
                log.Printf("[PlanMode] Phase timeout: %s exceeded %v", pm.phaseNameLocked(), planPhaseTimeout)
                return true, phaseElapsed, totalElapsed
        }

        // 總超時
        if totalElapsed > planTotalTimeout {
                log.Printf("[PlanMode] Total timeout: exceeded %v", planTotalTimeout)
                return true, phaseElapsed, totalElapsed
        }

        return false, phaseElapsed, totalElapsed
}

// ForceExitPlanMode 強制退出 Plan Mode（超時或用戶取消時使用）
// 標記 TimedOut=true，清理資源，返回計劃內容
func ForceExitPlanMode(reason string) string {
        globalPlanMode.mu.Lock()
        globalPlanMode.TimedOut = true
        content := exitPlanModeLocked()
        globalPlanMode.mu.Unlock()

        postExitPlanMode(content)

        log.Printf("[PlanMode] Force exit: %s", reason)
        return content
}

// IsTimedOut 檢查 Plan Mode 是否因超時而退出
func (pm *PlanMode) IsTimedOut() bool {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.TimedOut
}

// ============================================================================
// 計劃文件操作
// ============================================================================

func handlePlanWrite(args map[string]interface{}) (string, bool) {
        globalPlanMode.mu.RLock()
        phase := globalPlanMode.Phase
        planPath := globalPlanMode.PlanFilePath
        globalPlanMode.mu.RUnlock()

        if phase == PlanPhaseInactive {
                return "錯誤：Plan Mode 未激活。", false
        }
        if phase != PlanPhaseDesign {
                return "錯誤：PlanWrite 僅在 Phase 2（設計）可用。當前階段不能寫入計劃文件。", false
        }

        if planPath == "" {
                return "錯誤：Plan Mode 未正確初始化。", false
        }

        content, ok := args["content"].(string)
        if !ok || content == "" {
                return "錯誤：缺少 content 參數。", false
        }

        dir := filepath.Dir(planPath)
        if err := os.MkdirAll(dir, 0755); err != nil {
                return fmt.Sprintf("錯誤：無法創建計劃目錄: %v", err), false
        }

        if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
                return fmt.Sprintf("錯誤：無法寫入計劃文件: %v", err), false
        }

        return fmt.Sprintf("計劃已寫入 (%d 字符)", len(content)), true
}

func handlePlanRead(args map[string]interface{}) (string, bool) {
        globalPlanMode.mu.RLock()
        planPath := globalPlanMode.PlanFilePath
        globalPlanMode.mu.RUnlock()

        if planPath == "" {
                return "錯誤：Plan Mode 未激活。", false
        }

        data, err := os.ReadFile(planPath)
        if err != nil {
                if os.IsNotExist(err) {
                        return "計劃文件尚未創建。在 Phase 2 使用 PlanWrite 編寫計劃。", true
                }
                return fmt.Sprintf("錯誤：無法讀取計劃文件: %v", err), false
        }

        return string(data), true
}

// ============================================================================
// 工具定義（OpenAI 格式）
// ============================================================================

func nextPhaseToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "NextPhase",
                        "description": "推進到 Plan Mode 的下一階段。完成當前階段的所有工作後調用此工具。",
                        "parameters": map[string]interface{}{
                                "type":                "object",
                                "properties":           map[string]interface{}{},
                                "required":             []string{},
                                "additionalProperties": false,
                        },
                },
        }
}

func prevPhaseToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "PrevPhase",
                        "description": "回溯到上一階段（僅 Phase 2→Phase 1）。當設計過程中發現需要更多探索時使用。最多回溯 2 次。",
                        "parameters": map[string]interface{}{
                                "type":                "object",
                                "properties":           map[string]interface{}{},
                                "required":             []string{},
                                "additionalProperties": false,
                        },
                },
        }
}

func planWriteToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "PlanWrite",
                        "description": "將計劃內容寫入計劃文件。僅在 Phase 2（設計）可用。",
                        "parameters": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "content": map[string]interface{}{
                                                "type":       "string",
                                                "description": "計劃內容",
                                        },
                                },
                                "required":             []string{"content"},
                                "additionalProperties": false,
                        },
                },
        }
}

func planReadToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "PlanRead",
                        "description": "讀取當前計劃文件內容。",
                        "parameters": map[string]interface{}{
                                "type":                 "object",
                                "properties":           map[string]interface{}{},
                                "required":             []string{},
                                "additionalProperties": false,
                        },
                },
        }
}

func spawnToolDefs() []map[string]interface{} {
        return []map[string]interface{}{
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name":        "Spawn",
                                "description": "創建後台子代理執行獨立的只讀探索任務。",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "task": map[string]interface{}{
                                                        "type":       "string",
                                                        "description": "探索任務描述，如「分析 XXX 模塊的文件結構和關鍵函數」",
                                                },
                                                "MaxIterations": map[string]interface{}{
                                                        "type":       "integer",
                                                        "description": "最大迭代次數（預設 10）",
                                                },
                                        },
                                        "required":             []string{"task"},
                                        "additionalProperties": false,
                                },
                        },
                },
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name":        "SpawnCheck",
                                "description": "檢查子代理任務的執行狀態與結果。",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "TaskId": map[string]interface{}{
                                                        "type":       "string",
                                                        "description": "子代理任務 ID",
                                                },
                                        },
                                        "required":             []string{"TaskId"},
                                        "additionalProperties": false,
                                },
                        },
                },
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name":        "SpawnList",
                                "description": "列出所有子代理任務。",
                                "parameters": map[string]interface{}{
                                        "type":                 "object",
                                        "properties":           map[string]interface{}{},
                                        "required":             []string{},
                                        "additionalProperties": false,
                                },
                        },
                },
        }
}

func todosToolDef(listID, desc string) map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "Todos",
                        "description":  fmt.Sprintf("管理當前階段的子任務列表。列表 ID: \"%s\"。%s", listID, desc),
                        "parameters": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "Todos": map[string]interface{}{
                                                "type": "array",
                                                "items": map[string]interface{}{
                                                        "type": "object",
                                                        "properties": map[string]interface{}{
                                                                "id": map[string]interface{}{
                                                                        "type":       "string",
                                                                        "description": "任務唯一標識",
                                                                },
                                                                "content": map[string]interface{}{
                                                                        "type":       "string",
                                                                        "description": "任務內容",
                                                                },
                                                                "status": map[string]interface{}{
                                                                        "type":       "string",
                                                                        "enum":        []string{"Pending", "InProgress", "Completed", "Waiting"},
                                                                        "description": "任務狀態：Pending / InProgress / Completed / Waiting（異步等待中）",
                                                                },
                                                        },
                                                        "required": []string{"id", "content", "status"},
                                                },
                                                "description": fmt.Sprintf("待辦事項列表（list_id=%s）", listID),
                                        },
                                },
                                "required":             []string{"Todos"},
                                "additionalProperties": false,
                        },
                },
        }
}

// ============================================================================
// 舊接口兼容
// ============================================================================

// getPlanModeToolDefinitions 舊接口兼容：返回當前 Phase 的工具
// 被 getTools.go 的 appendDynamicTools 調用
func getPlanModeToolDefinitions() []map[string]interface{} {
        return GetToolsForCurrentPhase()
}

// GetPlanOnlyTools 舊接口兼容
func (pm *PlanMode) GetPlanOnlyTools() []string {
        pm.mu.RLock()
        defer pm.mu.RUnlock()
        return pm.getToolsForCurrentPhase()
}

// ============================================================================
// 輔助函數
// ============================================================================

func getDataDir() string {
        if globalConfig.DataDir != "" {
                return globalConfig.DataDir
        }
        execPath, err := os.Executable()
        if err != nil {
                return "./data"
        }
        return filepath.Dir(execPath)
}

func truncateStr(s string, maxLen int) string {
        if len(s) <= maxLen {
                return s
        }
        return s[:maxLen] + "..."
}

// GetPlanStatus 返回 Plan Mode 當前狀態摘要（用於 /plan 命令）
func GetPlanStatus() string {
        globalPlanMode.mu.RLock()
        defer globalPlanMode.mu.RUnlock()

        if globalPlanMode.Phase == PlanPhaseInactive {
                return ""
        }

        var sb strings.Builder
        elapsed := time.Since(globalPlanMode.StartTime).Round(time.Second)
        phaseElapsed := time.Since(globalPlanMode.PhaseStart).Round(time.Second)

        sb.WriteString(fmt.Sprintf("Plan Mode 已激活（%v），當前：%s（%v）\n\n",
                elapsed, globalPlanMode.phaseNameLocked(), phaseElapsed))

        // 進度條
        sb.WriteString("進度：\n")
        for p := PlanPhaseExplore; p <= PlanPhaseExecute; p++ {
                metadata := phaseMetadata[p]
                if p < globalPlanMode.Phase {
                        sb.WriteString(fmt.Sprintf("  [x] %s\n", metadata.Name))
                } else if p == globalPlanMode.Phase {
                        sb.WriteString(fmt.Sprintf("  [>] %s ← 當前\n", metadata.Name))
                } else {
                        sb.WriteString(fmt.Sprintf("  [ ] %s\n", metadata.Name))
                }
        }

        if globalPlanMode.TaskDesc != "" {
                sb.WriteString(fmt.Sprintf("\n任務：%s", globalPlanMode.TaskDesc))
        }

        // 回溯狀態
        if globalPlanMode.Phase == PlanPhaseDesign {
                sb.WriteString(fmt.Sprintf("\n回溯：%d/%d", globalPlanMode.DowngradeCount, maxDowngrades))
        }

        // Plan todos
        if planTodos := TODO.Render("plan"); planTodos != "" {
                sb.WriteString(fmt.Sprintf("\n\n%s", planTodos))
        }

        return sb.String()
}

// init 初始化日誌
func init() {
        log.Printf("[PlanMode] 結構化任務分解系統已初始化（3 Phase 狀態機 + 可回溯）")
}

// SortedPhaseReadTools 返回排序後的只讀工具列表（用於調試）
func SortedPhaseReadTools() []string {
        result := make([]string, len(PhaseReadTools))
        copy(result, PhaseReadTools)
        sort.Strings(result)
        return result
}
