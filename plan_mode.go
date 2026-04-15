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
//   - 5 個 Phase 由程序狀態機控制，LLM 無法跳過或回退
//   - 每 Phase 暴露不同工具集（工具分階段控制）
//   - 每 Phase 有獨立的 todos 列表追蹤子任務
//   - Phase 轉換需調用 next_phase 工具，程序檢查完成條件
//   - 退出後計劃步驟注入會話歷史，結構化存儲
//
// Phase 工具映射：
//   Phase 1 (探索): read_file_line, read_all_lines, text_search, text_grep, memory_recall, memory_list, spawn, spawn_check, spawn_list, todos
//   Phase 2 (設計): 同 Phase 1 + plan_write, plan_read
//   Phase 3 (審查): 同 Phase 1 + plan_read（不能寫）
//   Phase 4 (計劃): 同 Phase 1 + plan_write, plan_read, todos
//   Phase 5 (退出): 僅 next_phase（自動完成退出）
// ============================================================================

// PlanPhase Plan Mode 階段
type PlanPhase int

const (
        PlanPhaseInactive PlanPhase = 0 // 未激活
        PlanPhaseExplore  PlanPhase = 1 // Phase 1: 初始理解（只讀探索）
        PlanPhaseDesign   PlanPhase = 2 // Phase 2: 方案設計（可寫計劃草稿）
        PlanPhaseReview   PlanPhase = 3 // Phase 3: 審查驗證（只讀確認）
        PlanPhasePlan     PlanPhase = 4 // Phase 4: 編寫最終計劃
        PlanPhaseExit     PlanPhase = 5 // Phase 5: 退出（程序自動處理）
)

// Plan Mode 超時配置
const (
        planPhaseTimeout       = 5 * time.Minute // 單階段最大持續時間
        planTotalTimeout       = 20 * time.Minute // Plan Mode 總最大持續時間
)

// PlanMode Plan 模式管理器
type PlanMode struct {
        Phase       PlanPhase
        PlanFilePath string
        StartTime   time.Time
        PhaseStart  time.Time // 當前階段開始時間
        TaskDesc    string    // 用戶的原始任務描述
        mu          sync.RWMutex
        TimedOut    bool      // 是否因超時而被強制退出
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
                Name:        "Phase 1: 初始理解（探索）",
                Description: "使用只讀工具探索代碼庫，建立整體認識。善用 spawn 並行探索不同模塊。",
                ExtraTools: []string{
                        "spawn", "spawn_check", "spawn_list",
                        "todos",  // 管理 Phase 1 子任務
                },
        },
        PlanPhaseDesign: {
                Name:        "Phase 2: 方案設計",
                Description: "基於探索結果設計實現方案。可使用 plan_write 草擬計劃。",
                ExtraTools: []string{
                        "plan_write", "plan_read",
                        "spawn", "spawn_check", "spawn_list",
                        "todos",
                },
        },
        PlanPhaseReview: {
                Name:        "Phase 3: 審查驗證",
                Description: "重新閱讀關鍵文件，確認方案可行性。此階段不能修改計劃文件。",
                ExtraTools: []string{
                        "plan_read", // 只能讀，不能寫
                        "todos",
                },
        },
        PlanPhasePlan: {
                Name:        "Phase 4: 編寫最終計劃",
                Description: "使用 plan_write 編寫正式的實施計劃。計劃將在退出後作為執行指引。",
                ExtraTools: []string{
                        "plan_write", "plan_read",
                        "todos",
                },
        },
        PlanPhaseExit: {
                Name:        "Phase 5: 退出",
                Description: "系統自動處理退出。恢復完整工具訪問，計劃注入會話歷史。",
                ExtraTools:  []string{},
        },
}

// PhaseReadTools 在 Plan Mode 所有 Phase 中始終可用的只讀基礎工具
var PhaseReadTools = []string{
        "read_file_line",
        "read_all_lines",
        "text_search",
        "text_grep",
        "memory_recall",
        "memory_list",
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
// 如果規劃模式未在配置中啟用，返回錯誤信息
func EnterPlanMode(taskDesc string) string {
        if !globalPlanModeEnabled {
                return "規劃模式（Plan Mode）未啟用。當前工作模式僅使用待辦事項（todos）控制進度。如需啟用規劃模式，請在設置中開啟「規劃模式」選項。"
        }

        globalPlanMode.mu.Lock()
        defer globalPlanMode.mu.Unlock()

        globalPlanMode.Phase = PlanPhaseExplore
        globalPlanMode.StartTime = time.Now()
        globalPlanMode.PhaseStart = time.Now()
        globalPlanMode.TaskDesc = taskDesc

        dataDir := getDataDir()
        globalPlanMode.PlanFilePath = filepath.Join(dataDir, "plan.md")

        // 初始化整體 Plan todos（作為 list_id="plan"）
        _, _ = TODO.Update([]TodoItem{
                {ID: "1", Text: "Phase 1: 初始理解（探索）", Status: "in_progress"},
                {ID: "2", Text: "Phase 2: 方案設計", Status: "pending"},
                {ID: "3", Text: "Phase 3: 審查驗證", Status: "pending"},
                {ID: "4", Text: "Phase 4: 編寫最終計劃", Status: "pending"},
                {ID: "5", Text: "Phase 5: 退出", Status: "pending"},
        }, "plan")

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
        // 清理 Plan todos
        _ = TODO.Clear("plan")
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

        // Phase 5 是終態，由程序自動處理退出
        if currentPhase >= PlanPhaseExit {
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

        // Phase 5 自動退出（使用內部鎖安全方法，避免 unlock/re-lock 競態）
        if nextPhase == PlanPhaseExit {
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

// updatePlanTodos 更新整體 Plan 進度 todos
// 需要在已持有 globalPlanMode.mu 時調用
func updatePlanTodos(completedPhase int) {
        items := make([]TodoItem, 5)
        for i := 1; i <= 5; i++ {
                status := "pending"
                if i < completedPhase {
                        status = "completed"
                } else if i == completedPhase {
                        status = "in_progress"
                }
                phaseName := getPhaseTodoText(i)
                items[i-1] = TodoItem{ID: fmt.Sprintf("%d", i), Text: phaseName, Status: status}
        }
        _, _ = TODO.Update(items, "plan")
}

func getPhaseTodoText(phase int) string {
        names := map[int]string{
                1: "Phase 1: 初始理解（探索）",
                2: "Phase 2: 方案設計",
                3: "Phase 3: 審查驗證",
                4: "Phase 4: 編寫最終計劃",
                5: "Phase 5: 退出",
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
        if toolName == "next_phase" {
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
                // Phase 1: 只讀 + spawn + todos（靜態工具已由 tier 過濾提供 read/search）
                tools = append(tools, spawnToolDefs()...)
                tools = append(tools, todosToolDef("phase1", "管理 Phase 1 探索子任務"))
        case PlanPhaseDesign:
                tools = append(tools, planWriteToolDef(), planReadToolDef())
                tools = append(tools, spawnToolDefs()...)
                tools = append(tools, todosToolDef("phase2", "管理 Phase 2 設計子任務"))
        case PlanPhaseReview:
                tools = append(tools, planReadToolDef())
                tools = append(tools, todosToolDef("phase3", "管理 Phase 3 審查子任務"))
        case PlanPhasePlan:
                tools = append(tools, planWriteToolDef(), planReadToolDef())
                tools = append(tools, todosToolDef("phase4", "管理 Phase 4 計劃子任務"))
        case PlanPhaseExit:
                // Phase 5: 僅 next_phase（自動退出），不額外注入
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
        for p := PlanPhaseExplore; p <= PlanPhaseExit; p++ {
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

        // 當前階段指引
        switch phase {
        case PlanPhaseExplore:
                sb.WriteString(explorePhasePrompt())
        case PlanPhaseDesign:
                sb.WriteString(designPhasePrompt())
        case PlanPhaseReview:
                sb.WriteString(reviewPhasePrompt())
        case PlanPhasePlan:
                sb.WriteString(planPhasePrompt())
        case PlanPhaseExit:
                sb.WriteString(exitPhasePrompt())
        }

        // 通用提醒
        sb.WriteString("\n完成當前階段的工作後，調用 next_phase 推進到下一階段。")

        return sb.String()
}

func explorePhasePrompt() string {
        return `## Phase 1: 初始理解（探索）

目標：充分理解任務涉及的代碼庫、文件結構和依賴關係。

操作指引：
1. 使用 text_search / text_grep 搜索關鍵詞，定位相關文件
2. 使用 read_file_line / read_all_lines 閱讀相關文件
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
        return `## Phase 2: 方案設計

目標：基於 Phase 1 的探索結果，設計具體的實現方案。

操作指引：
1. 綜合 Phase 1 的發現，設計實現方案
2. 使用 plan_write 草擬計劃（可多次修改）
3. 使用 plan_read 查看已寫的計劃
4. 使用 todos 工具管理設計子任務

方案要點：
- 修改步驟的先後順序（考慮依賴）
- 每步修改涉及的具體文件和代碼位置
- 邊界情況和錯誤處理

完成設計後，調用 next_phase 進入審查階段。`
}

func reviewPhasePrompt() string {
        return `## Phase 3: 審查驗證

目標：驗證方案的可行性，確認沒有遺漏。

操作指引：
1. 使用 plan_read 查看已草擬的計劃
2. 重新閱讀關鍵文件，確認方案中的假設
3. 使用 todos 工具管理審查子任務

審查要點：
- 計劃中提到的文件路徑是否正確？
- 修改步驟是否有遺漏？
- 是否有潛在的衝突或副作用？
- 邊界情況是否已考慮？

此階段計劃文件為只讀，如需修正請在 Phase 4 進行。

完成審查後，調用 next_phase 進入最終計劃階段。`
}

func planPhasePrompt() string {
        return `## Phase 4: 編寫最終計劃

目標：編寫正式的實施計劃文件，包含所有必要的上下文和步驟。

操作指引：
1. 使用 plan_write 編寫最終計劃（覆蓋草稿）
2. 使用 plan_read 確認內容正確
3. 使用 todos 工具管理計劃子任務

計劃格式（必須包含）：
  ## Context（上下文）
  你已了解的信息、涉及哪些文件和模塊

  ## Approach（方案）
  按步驟列出要執行的操作，具體到文件路徑和代碼位置

  ## Verification（驗證方式）
  如何驗證修改正確性

完成後，調用 next_phase 退出 Plan Mode 並開始執行。`
}

func exitPhasePrompt() string {
        return `## Phase 5: 退出

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

func handlePlanWrite(args map[string]interface{}) string {
        globalPlanMode.mu.RLock()
        phase := globalPlanMode.Phase
        planPath := globalPlanMode.PlanFilePath
        globalPlanMode.mu.RUnlock()

        if phase == PlanPhaseInactive {
                return "錯誤：Plan Mode 未激活。"
        }
        if phase == PlanPhaseReview {
                return "錯誤：Phase 3（審查驗證）中不能修改計劃文件。請在 Phase 4 修改。"
        }
        if phase != PlanPhaseDesign && phase != PlanPhasePlan {
                return "錯誤：當前階段不能寫入計劃文件。"
        }

        if planPath == "" {
                return "錯誤：Plan Mode 未正確初始化。"
        }

        content, ok := args["content"].(string)
        if !ok || content == "" {
                return "錯誤：缺少 content 參數。"
        }

        dir := filepath.Dir(planPath)
        if err := os.MkdirAll(dir, 0755); err != nil {
                return fmt.Sprintf("錯誤：無法創建計劃目錄: %v", err)
        }

        if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
                return fmt.Sprintf("錯誤：無法寫入計劃文件: %v", err)
        }

        return fmt.Sprintf("計劃已寫入 (%d 字符)", len(content))
}

func handlePlanRead(args map[string]interface{}) string {
        globalPlanMode.mu.RLock()
        planPath := globalPlanMode.PlanFilePath
        globalPlanMode.mu.RUnlock()

        if planPath == "" {
                return "錯誤：Plan Mode 未激活。"
        }

        data, err := os.ReadFile(planPath)
        if err != nil {
                if os.IsNotExist(err) {
                        return "計劃文件尚未創建。在 Phase 2 或 Phase 4 使用 plan_write 編寫計劃。"
                }
                return fmt.Sprintf("錯誤：無法讀取計劃文件: %v", err)
        }

        return string(data)
}

// ============================================================================
// 工具定義（OpenAI 格式）
// ============================================================================

func nextPhaseToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "next_phase",
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

func planWriteToolDef() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        "plan_write",
                        "description": "將計劃內容寫入計劃文件。Phase 2（草擬）和 Phase 4（最終計劃）可用。",
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
                        "name":        "plan_read",
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
                                "name":        "spawn",
                                "description": "創建後台子代理執行獨立的只讀探索任務。",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "task": map[string]interface{}{
                                                        "type":       "string",
                                                        "description": "探索任務描述，如「分析 XXX 模塊的文件結構和關鍵函數」",
                                                },
                                                "max_iterations": map[string]interface{}{
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
                                "name":        "spawn_check",
                                "description": "檢查子代理任務的執行狀態與結果。",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "task_id": map[string]interface{}{
                                                        "type":       "string",
                                                        "description": "子代理任務 ID",
                                                },
                                        },
                                        "required":             []string{"task_id"},
                                        "additionalProperties": false,
                                },
                        },
                },
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name":        "spawn_list",
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
                        "name":        "todos",
                        "description":  fmt.Sprintf("管理當前階段的子任務列表。列表 ID: \"%s\"。%s", listID, desc),
                        "parameters": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "todos": map[string]interface{}{
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
                                                                        "enum":        []string{"pending", "in_progress", "completed", "waiting"},
                                                                        "description": "任務狀態：pending / in_progress / completed / waiting（異步等待中）",
                                                                },
                                                        },
                                                        "required": []string{"id", "content", "status"},
                                                },
                                                "description": fmt.Sprintf("待辦事項列表（list_id=%s）", listID),
                                        },
                                },
                                "required":             []string{"todos"},
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
        for p := PlanPhaseExplore; p <= PlanPhaseExit; p++ {
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

        // Plan todos
        if planTodos := TODO.Render("plan"); planTodos != "" {
                sb.WriteString(fmt.Sprintf("\n\n%s", planTodos))
        }

        return sb.String()
}

// init 初始化日誌
func init() {
        log.Printf("[PlanMode] 結構化任務分解系統已初始化（5 Phase 狀態機）")
}

// SortedPhaseReadTools 返回排序後的只讀工具列表（用於調試）
func SortedPhaseReadTools() []string {
        result := make([]string, len(PhaseReadTools))
        copy(result, PhaseReadTools)
        sort.Strings(result)
        return result
}
