package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
)

// ============================================================================
// 工具安全网 - 灵感来自 cc-mini 的先读后写检查、迭代上限等设计
// ============================================================================

var (
        // MaxAgentLoopIterations Agent Loop 最大迭代次数（每轮 = 一次 LLM 调用 + 工具执行）
        // 0 = 不限制。可通过配置文件 Tools.MaxAgentIterations 覆盖
        MaxAgentLoopIterations = 0

        // IterationWarningThreshold 迭代警告阈值
        // 接近上限时注入提醒消息（默认为上限的 80%）
        IterationWarningThreshold = 0
)

// ============================================================================
// 先读后写检查（Read-Before-Write Enforcement）
// ============================================================================

// readLevel 表示文件的讀取級別
type readLevel int

const (
        readLevelNone    readLevel = iota // 未讀取
        readLevelPartial                  // 部分讀取（ReadFileLine, TextGrep）
        readLevelFull                     // 完整讀取（ReadFileLines）
)

// readExpiryTime ReadFileLines 完整讀取記錄的有效期。
// SSH 持久會話等耗時操作可能令模型在讀取後超過 10 分鐘先寫入，
// 因此設為 60 分鐘以容納長操作場景。evict 使用相同閾值。
const readExpiryTime = 60 * time.Minute

// escalatePrefix 通用錯誤升級 sentinel prefix。
// 當 SafeExecuteTool 或其他錯誤處理返回以此前綴開頭的 EnrichedMessage 時，
// AgentLoop 主循環會提取消息內容並以用戶身份注入對話歷史。
// 格式：__ESCALATE__:<category>:<message>
// 目前支援的 category: write_without_read, repeated_tool_failure
const escalatePrefix = "__ESCALATE__:"

// readWriteTracker 追踪已讀取的文件及其讀取級別，強制先讀後寫
// 核心設計：只有完整讀取（ReadFileLines）才能滿足全量寫入工具的先讀要求，
// 防止模型只讀一行就用幻覺重寫整個文件
type readWriteTracker struct {
        mu               sync.RWMutex
        fullReadFiles    map[string]time.Time // 完整讀取：文件路徑 -> 讀取時間
        partialReadFiles map[string]time.Time // 部分讀取：文件路徑 -> 讀取時間
        maxEntries       int                  // 最大緩存條目數
}

var globalReadWriteTracker = &readWriteTracker{
        fullReadFiles:    make(map[string]time.Time),
        partialReadFiles: make(map[string]time.Time),
        maxEntries:       200,
}

// MarkFileFullyRead 標記文件已被完整讀取（僅由 ReadFileLines 調用）
// 完整讀取是滿足任何寫入操作的最高級別要求
func (t *readWriteTracker) MarkFileFullyRead(filePath string) {
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        t.fullReadFiles[filePath] = time.Now()
        // 升級後同時從部分讀取中移除（避免冗餘）
        delete(t.partialReadFiles, filePath)
        t.evictIfNeeded()

        // 模型正確讀取文件後，重置寫入違規計數
        globalErrorEscalator.ResetCategory(EscalateWriteWithoutRead)
}

// MarkFilePartialRead 標記文件已被部分讀取（由 ReadFileLine, TextGrep 調用）
// 部分讀取不滿足任何寫入操作的先讀要求，僅作內部追蹤用途；
// 所有寫入操作統一要求完整讀取（ReadFileLines）
func (t *readWriteTracker) MarkFilePartialRead(filePath string) {
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        // 如果已經是完整讀取，不降級
        if _, ok := t.fullReadFiles[filePath]; ok {
                return
        }
        t.partialReadFiles[filePath] = time.Now()
        t.evictIfNeeded()
}

// evictIfNeeded 防止緩存無限增長，清理最舊的條目
func (t *readWriteTracker) evictIfNeeded() {
        total := len(t.fullReadFiles) + len(t.partialReadFiles)
        if total <= t.maxEntries {
                return
        }
        count := 0
        for key, ts := range t.partialReadFiles {
                if count >= 50 {
                        break
                }
                if time.Since(ts) > readExpiryTime {
                        delete(t.partialReadFiles, key)
                        count++
                }
        }
        for key, ts := range t.fullReadFiles {
                if count >= 50 {
                        break
                }
                if time.Since(ts) > readExpiryTime {
                        delete(t.fullReadFiles, key)
                        count++
                }
        }
}

// GetFileReadLevel 獲取文件的讀取級別
func (t *readWriteTracker) GetFileReadLevel(filePath string) readLevel {
        t.mu.RLock()
        defer t.mu.RUnlock()

        filePath = normalizeFilePath(filePath)
        if ts, ok := t.fullReadFiles[filePath]; ok && time.Since(ts) < readExpiryTime {
                return readLevelFull
        }
        if ts, ok := t.partialReadFiles[filePath]; ok && time.Since(ts) < readExpiryTime {
                return readLevelPartial
        }
        return readLevelNone
}

// HasFileBeenRead 檢查文件是否已被讀取（兼容舊接口，任何級別均返回 true）
func (t *readWriteTracker) HasFileBeenRead(filePath string) bool {
        return t.GetFileReadLevel(filePath) != readLevelNone
}

// normalizeFilePath 規範化文件路徑
func normalizeFilePath(path string) string {
        // 使用 filepath.Abs + filepath.Clean 進行規範化，防止路徑遍歷繞過安全檢查
        abs, err := filepath.Abs(path)
        if err != nil {
                return path
        }
        return filepath.Clean(abs)
}

// CheckWritePermission 檢查是否允許寫入文件
// 返回 nil 表示允許，返回 error 表示需要先讀取
// 新建文件（目標路徑不存在）無需先讀，直接允許寫入
//
// 安全策略：
//   - 所有寫入工具（WriteFileLine, WriteFileLines, AppendToFile,
//     WriteFileRange, TextReplace, TextTransform）統一要求完整讀取（ReadFileLines）
//   - ReadFileLine 或 TextGrep 僅讀取部分內容，不被視為已讀過文件
//   - 防止模型只讀一行就用幻覺寫入/修改文件
func CheckWritePermission(filePath string, toolName string) error {
        // 歸一化路徑，確保 os.Stat 和 GetFileReadLevel 使用相同的路徑表示
        filePath = normalizeFilePath(filePath)
        // 如果文件不存在，視為新建文件，無需先讀
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                return nil
        }

        readLvl := globalReadWriteTracker.GetFileReadLevel(filePath)

        // 所有寫入工具統一要求完整讀取
        if readLvl != readLevelFull {
                return fmt.Errorf("安全檢查失敗：你必須先使用 ReadFileLines 完整讀取 %s 才能進行寫入/編輯操作（ReadFileLine / ReadFileRange 或 TextGrep 僅讀取部分內容，不被視為已讀過文件）。這是為了確保你理解現有文件內容後再修改。", filePath)
        }
        return nil
}

// ============================================================================
// 寫入前未讀取違規追蹤（Write-Without-Read Violation Tracking）
// ============================================================================

// EscalationCategory 錯誤升級類別
type EscalationCategory string

const (
	// EscalateWriteWithoutRead 寫入前未讀取違規
	EscalateWriteWithoutRead EscalationCategory = "write_without_read"
	// EscalateRepeatedFailure 重複工具調用失敗（同一工具+參數連續失敗）
	EscalateRepeatedFailure EscalationCategory = "repeated_tool_failure"
)

// escalationTracker 單個錯誤類別的追蹤器
type escalationTracker struct {
	category  EscalationCategory
	errorKey  string   // 錯誤鍵（如文件路徑、工具名+參數哈希）
	count     int
	messages  []string // 保存每次錯誤的完整信息
}

// RepeatedErrorEscalator 通用重複錯誤升級器。
// 為不同類別和鍵的錯誤獨立追蹤連續失敗次數，
// 達到閾值後觸發升級：以用戶身份將錯誤摘要注入消息歷史。
type RepeatedErrorEscalator struct {
	mu       sync.Mutex
	trackers map[string]*escalationTracker // key: "category:errorKey"
}

var globalErrorEscalator = &RepeatedErrorEscalator{
	trackers: make(map[string]*escalationTracker),
}

const defaultEscalationThresholdValue = 3

// getEscalationThreshold returns the configurable escalation threshold
// (1-5, default 3). Uses globalEscalationThreshold if set, otherwise falls
// back to the hardcoded default.
func getEscalationThreshold() int {
	if globalEscalationThreshold > 0 {
		return globalEscalationThreshold
	}
	return defaultEscalationThresholdValue
}

// RecordEscalation 記錄一次錯誤並判斷是否需要升級。
// category: 錯誤類別
// errorKey: 錯誤鍵（同類別+同鍵的錯誤累計計數）
// errMsg:   錯誤消息
// 返回 shouldStop=true 表示已達到閾值，需強制升級
func (e *RepeatedErrorEscalator) RecordEscalation(
	category EscalationCategory, errorKey, errMsg string,
) (shouldStop bool, userMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	trackKey := string(category) + ":" + errorKey
	t, ok := e.trackers[trackKey]
	if !ok {
		t = &escalationTracker{
			category: category,
			errorKey: errorKey,
		}
		e.trackers[trackKey] = t
	}

	t.count++
	t.messages = append(t.messages, errMsg)

	if t.count >= getEscalationThreshold() {
		userMsg = e.buildEscalationMessage(t)
		shouldStop = true
		// 重置此追蹤器，為下一輪做準備
		delete(e.trackers, trackKey)
	}

	return
}

// buildEscalationMessage 根據類別構建升級消息
func (e *RepeatedErrorEscalator) buildEscalationMessage(t *escalationTracker) string {
	var sb strings.Builder

	switch t.category {
	case EscalateWriteWithoutRead:
		sb.WriteString("以下是你連續多次無視安全檢查錯誤的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("你必須使用 ReadFileLines 完整讀取目標文件後才能進行寫入操作。請立即讀取相關文件。")

	case EscalateRepeatedFailure:
		sb.WriteString("以下是你連續多次重複相同失敗操作的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("請停止重複此操作。分析錯誤原因後嘗試不同的方法，或向用戶說明遇到的問題並請求指導。")

	default:
		sb.WriteString("以下是你連續多次錯誤的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("請停止重複操作，分析原因並採取不同的策略。")
	}

	return sb.String()
}

// ResetCategory 重置指定類別的所有追蹤器
func (e *RepeatedErrorEscalator) ResetCategory(category EscalationCategory) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, t := range e.trackers {
		if t.category == category {
			delete(e.trackers, key)
		}
	}
}

// ResetKey 重置指定類別+鍵的追蹤器
func (e *RepeatedErrorEscalator) ResetKey(category EscalationCategory, errorKey string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	trackKey := string(category) + ":" + errorKey
	delete(e.trackers, trackKey)
}
// ============================================================================
// 未知工具引导（Unknown Tool Guidance）
// ============================================================================

// allRegisteredToolNames returns a snapshot of all tool names in toolRegistryMap.
// Used for fuzzy matching suggestions when the model calls an unknown tool.
func allRegisteredToolNames() []string {
        names := make([]string, 0, len(toolRegistryMap))
        for name := range toolRegistryMap {
                names = append(names, name)
        }
        return names
}

// FindSimilarTool 找到与输入最相似的工具名称
// 使用简单的编辑距离算法
func FindSimilarTool(input string) string {
        input = strings.ToLower(strings.TrimSpace(input))

        bestMatch := ""
        bestDistance := len(input) + 5 // 初始阈值

        for _, name := range allRegisteredToolNames() {
                distance := levenshteinDistance(input, name)
                // 只考虑距离足够小的匹配
                if distance < bestDistance && distance <= max(len(input), len(name))/2+1 {
                        bestDistance = distance
                        bestMatch = name
                }
        }

        return bestMatch
}

// levenshteinDistance 计算两个字符串的 Levenshtein 编辑距离
func levenshteinDistance(s1, s2 string) int {
        if len(s1) == 0 {
                return len(s2)
        }
        if len(s2) == 0 {
                return len(s1)
        }

        // 优化：如果长度差异太大，直接返回
        diff := absInt(len(s1) - len(s2))
        if diff > max(len(s1), len(s2))/2 {
                return diff
        }

        // 创建距离矩阵
        prev := make([]int, len(s2)+1)
        curr := make([]int, len(s2)+1)

        for j := range prev {
                prev[j] = j
        }

        for i := 1; i <= len(s1); i++ {
                curr[0] = i
                for j := 1; j <= len(s2); j++ {
                        cost := 1
                        if s1[i-1] == s2[j-1] {
                                cost = 0
                        }
                        curr[j] = minInt(
                                curr[j-1]+1,      // 插入
                                prev[j]+1,       // 删除
                                prev[j-1]+cost,  // 替换
                        )
                }
                prev, curr = curr, prev
        }

        return prev[len(s2)]
}

func absInt(x int) int {
        if x < 0 {
                return -x
        }
        return x
}

func minInt(a, b, c int) int {
        if a < b {
                if a < c {
                        return a
                }
                return c
        }
        if b < c {
                return b
        }
        return c
}

// snakeToPascalCase 將 snake_case 工具名轉換為 PascalCase
// 例如：SSHConnect, BrowserClick
func snakeToPascalCase(s string) string {
        parts := strings.Split(s, "_")
        for i, part := range parts {
                if len(part) > 0 {
                        parts[i] = strings.Title(part)
                }
        }
        return strings.Join(parts, "")
}

// normalizeArgsMapKeys 將含底線嘅 snake_case key 轉為 PascalCase 版本並合併入 argsMap
// 處理 LLM 用 snake_case 傳參但 handler 用 PascalCase 讀取嘅兼容問題
// 只處理含 "_" 嘅 key，保留原始 key 並新增 PascalCase 版本，雙向兼容
func normalizeArgsMapKeys(argsMap map[string]interface{}) {
        if argsMap == nil {
                return
        }
        for key, value := range argsMap {
                if strings.Contains(key, "_") {
                        pascalKey := snakeToPascalCase(key)
                        if pascalKey != key {
                                if _, exists := argsMap[pascalKey]; !exists {
                                        argsMap[pascalKey] = value
                                }
                        }
                }
        }
}

// GetUnknownToolErrorMessage 生成未知工具的错误消息
// 自動檢測 snake_case 命名並提供明確的 PascalCase 修正指引
func GetUnknownToolErrorMessage(toolName string) string {
        // 檢測 snake_case：如果工具名含底線，先試自動轉 PascalCase
        if strings.Contains(toolName, "_") {
                pascalName := snakeToPascalCase(toolName)
                for _, name := range allRegisteredToolNames() {
                        if name == pascalName {
                                return fmt.Sprintf("工具名不能使用底線格式 (snake_case)！請使用駝峰式 (PascalCase)：'%s'", pascalName)
                        }
                }
        }

        suggestion := FindSimilarTool(toolName)
        if suggestion != "" {
                return fmt.Sprintf("工具 '%s' 不存在。你是否想使用 '%s'？\n可用的工具列表请参考系统提示中的工具部分。", toolName, suggestion)
        }
        return fmt.Sprintf("工具 '%s' 不存在。请检查工具名称是否正确。\n可用的工具列表请参考系统提示中的工具部分。", toolName)
}

// ============================================================================
// 迭代上限与智能中断
// ============================================================================

// LoopWarningInjector 迭代警告注入器
type LoopWarningInjector struct {
        warningInjected bool
        lastWarnTime   time.Time
}

var globalLoopWarningInjector = &LoopWarningInjector{}

// ShouldInjectWarning 是否应该注入迭代警告
func (l *LoopWarningInjector) ShouldInjectWarning(iteration int) bool {
        // 未设置上限则不警告
        if MaxAgentLoopIterations <= 0 || iteration < IterationWarningThreshold {
                return false
        }
        // 每次警告间隔至少 3 次迭代
        if l.warningInjected && int64(iteration)-lastWarnIteration < 3 {
                return false
        }
        return true
}

var lastWarnIteration int64 = 0

// GetIterationWarningMessage 获取迭代警告消息
func GetIterationWarningMessage(iteration int) string {
        lastWarnIteration = int64(iteration)
        remaining := MaxAgentLoopIterations - iteration
        if remaining <= 5 {
                return fmt.Sprintf(`[系统警告] Agent Loop 已迭代 %d 轮（上限 %d 轮，剩余 %d 轮）。
请尽快总结当前进展并完成最后的步骤。如果无法完成，请向用户报告当前进度和未完成的事项。`, iteration, MaxAgentLoopIterations, remaining)
        }
        return fmt.Sprintf(`[系统提醒] Agent Loop 已迭代 %d 轮（上限 %d 轮）。
建议你合理安排剩余步骤，避免不必要的重复操作。`, iteration, MaxAgentLoopIterations)
}

// ShouldForceStop 是否应该强制停止 Agent Loop
func ShouldForceStop(iteration int) bool {
        return MaxAgentLoopIterations > 0 && iteration >= MaxAgentLoopIterations
}

// ============================================================================
// 只读工具并行执行标记
// ============================================================================

// ReadOnlyTools 只读工具列表，这些工具可以并行执行
var ReadOnlyTools = map[string]bool{
        "ReadFileLine": true,
        "ReadFileLines":  true,
        "TextSearch":     true,
        "TextGrep":       true,
        "MemoryRecall":   true,
        "MemoryList":     true,
        "PlanRead":       true,
        "PluginList":     true,
        "SkillList":      true,
        "SkillGet":       true,
        "CronList":       true,
        "CronStatus":     true,
        "SpawnList":      true,
        "SSHList":        true,
        "ProfileCheck":   true,
}

// IsReadOnlyTool 检查工具是否为只读工具
func IsReadOnlyTool(toolName string) bool {
        return ReadOnlyTools[toolName]
}

// ============================================================================
// 工具执行包装器 - 集成安全检查
// ============================================================================

// emitToolCallTags 向前端发送完整的工具调用 agentic tags（用于早期返回路径）
// 确保所有工具执行路径（包括安全检查拒绝、Plan Mode 拦截等）都能在网页端显示为工具块
func emitToolCallTags(ch Channel, toolName string, argsMap map[string]interface{}, content string, status TaskStatus) {
        // 檢查任務是否已被取消：若已取消則不應再向前端推送工具結果
        // 避免用戶在 /stop 之後仍然看到 SSH 錯誤等後續輸出
        session := GetGlobalSession()
        if session.IsTaskCancelled() {
                return
        }
        argsJSON, _ := json.Marshal(argsMap)
        sendToolCallStart(ch, toolName, string(argsJSON))
        if content != "" {
                ch.WriteChunk(StreamChunk{Content: content + "\n"})
        }
        sendToolCallStatus(ch, status)
        sendToolCallEnd(ch)
}

// SafeExecuteTool 安全工具执行包装器
// 在原有 executeTool 基础上添加安全检查：
// 1. Plan Mode 权限检查
// 2. 先读后写检查
// 3. 未知工具引导
func SafeExecuteTool(ctx context.Context, toolID, toolName string, argsMap map[string]interface{}, ch Channel, role *Role) EnrichedMessage {
        // 參數名歸一化：將 snake_case key 轉為 PascalCase 兼容版本
        normalizeArgsMapKeys(argsMap)

        // Tasks Mode 权限检查（分階段工具控制）
        if globalTasksMode.IsActive() && !IsToolAllowedInTasksMode(toolName) {
                currentPhase := globalTasksMode.Phase()
                var content string
                switch toolName {
                case "EnterPlanMode":
                        content = fmt.Sprintf("你已經在 Tasks Mode 中（%s）。使用 Tasks(PlanPhase=\"design\") 進入設計階段，或 Tasks(PlanPhase=\"execute\") 退出。", currentPhase)
                case "SmartShell", "Shell":
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用 shell。此階段僅允許只讀工具（TextSearch、ReadFileLines 等）。", currentPhase)
                case "WriteFileLine", "WriteFileLines", "AppendToFile", "WriteFileRange", "TextReplace":
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用寫入工具 '%s'。先完成探索和設計，最終計劃確認後再執行。", currentPhase, toolName)
                default:
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用工具 '%s'。當前階段可用工具有限。", currentPhase, toolName)
                }
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusFailed},
                }
        }

        // Tasks Mode 专用工具处理
        switch toolName {
        case "EnterPlanMode":
                // 引導使用新 Tasks 工具
                content := "EnterPlanMode 已棄用。請改用 Tasks 工具：\n- Tasks(PlanPhase=\"explore\") 進入探索階段\n- Tasks(PlanPhase=\"design\", plan_content=\"...\", tasks=[...]) 進入設計階段\n- Tasks(PlanPhase=\"execute\") 退出開始執行"
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        case "ExitPlanMode":
                if globalTasksMode.IsActive() {
                        content, _ := handleTasks(map[string]interface{}{"PlanPhase": "execute"})
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                        return EnrichedMessage{
                                Content: content,
                                Meta:    MessageMeta{Status: TaskStatusSuccess},
                        }
                }
                content := "Tasks Mode 當前未激活。"
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusFailed},
                }
        }

		// 未知工具检查 - 先查 toolRegistryMap，再查 toolHandlerRegistry（含 Menu 等特殊工具），最后查 MCP
		_, isKnown := toolRegistryMap[toolName]
		if !isKnown {
			// 检查是否在 toolHandlerRegistry 中（例如 Menu, Tasks 等特殊工具）
			if _, handlerExists := toolHandlerRegistry[toolName]; handlerExists {
				isKnown = true
			}
		}
		if !isKnown {
			// 检查是否是 MCP 动态工具
			isMCP := false
			if globalMCPClientManager != nil {
				mcpTools := globalMCPClientManager.GetAllTools()
				for _, t := range mcpTools {
					if t["name"] == toolName {
						isMCP = true
						break
					}
				}
			}
			if !isMCP {
				log.Printf("[ToolSafety] 未知工具调用: %s", toolName)
				content := GetUnknownToolErrorMessage(toolName)

				// 追蹤重複未知工具調用，達到閾值後觸發 escalation
				shouldStop, userMsg := globalErrorEscalator.RecordEscalation(
					EscalateRepeatedFailure, toolName, content,
				)

				emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)

				finalContent := content
				if shouldStop {
					finalContent = escalatePrefix + userMsg
				}

				return EnrichedMessage{
					Content: finalContent,
					Meta:    MessageMeta{Status: TaskStatusFailed},
				}
			}
		}

        // 先读后写检查 - 对写入类工具
        if isWriteTool(toolName) {
                filePath := extractFilePathFromArgs(argsMap)
                if filePath != "" {
                        if err := CheckWritePermission(filePath, toolName); err != nil {
                                log.Printf("[ToolSafety] 先读后写检查失败: tool=%s file=%s", toolName, filePath)

                                errStr := err.Error()
                                shouldStop, userMsg := globalErrorEscalator.RecordEscalation(
                                        EscalateWriteWithoutRead, filePath, errStr,
                                )
                                if shouldStop {
                                        // 連續 3 次違規：前端只顯示一般錯誤，內部返回 force-stop 標記
                                        // 主循環檢測標記後會以用戶身份注入消息（僅模型可見）
                                        emitToolCallTags(ch, toolName, argsMap, errStr, TaskStatusFailed)
                                        return EnrichedMessage{
                                                Content: escalatePrefix + userMsg,
                                                Meta:    MessageMeta{Status: TaskStatusFailed},
                                        }
                                }

                                emitToolCallTags(ch, toolName, argsMap, errStr, TaskStatusFailed)
                                return EnrichedMessage{
                                        Content: errStr,
                                        Meta:    MessageMeta{Status: TaskStatusFailed},
                                }
                        }
                }
        }

        // 调用原始工具执行（executeTool 内部会自行发送 agentic tags）
        return executeTool(ctx, toolID, toolName, argsMap, ch, role)
}

// isWriteTool 检查工具是否为写入类工具
func isWriteTool(toolName string) bool {
        writeTools := map[string]bool{
                "WriteFileLine": true,
                "WriteFileLines": true,
                "AppendToFile":  true,
                "WriteFileRange": true,
                "TextReplace":    true,
                "TextTransform":  true,
                "MemorySave":     true,
                "MemoryForget":   true,
        }
        return writeTools[toolName]
}

// extractFilePathFromArgs 从工具参数中提取文件路径
func extractFilePathFromArgs(args map[string]interface{}) string {
        // 尝试常见的文件路径参数名
        for _, key := range []string{"FilePath", "filePath", "path", "filename", "file"} {
                if val, ok := args[key]; ok {
                        if str, ok := val.(string); ok && str != "" {
                                return str
                        }
                }
        }
        return ""
}

// init 初始化：工具安全网启动日志
// MarkFileFullyRead / MarkFilePartialRead 已集成到以下工具中：
//   - executeTool.go: execReadFileLines -> MarkFileFullyRead
//   - executeTool.go: execReadFileLine -> MarkFilePartialRead
//   - TextReplace_tools.go: handleTextSearch (TextGrep) -> MarkFilePartialRead
func init() {
        log.Printf("[ToolSafety] 工具安全网已初始化: MaxIterations=%d, ReadOnlyTools=%d",
                MaxAgentLoopIterations, len(ReadOnlyTools))
}
