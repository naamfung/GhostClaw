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
        readLevelPartial                  // 部分讀取（read_file_line, text_grep）
        readLevelFull                     // 完整讀取（read_all_lines）
)

// forceStopWriteWithoutReadPrefix sentinel prefix for write-without-read force-stop signals.
// When SafeExecuteTool returns an EnrichedMessage with Content starting with this prefix,
// the main AgentLoop will extract the message and inject it as a user message.
const forceStopWriteWithoutReadPrefix = "__FORCE_STOP_WRITE_WITHOUT_READ__:"

// readWriteTracker 追踪已讀取的文件及其讀取級別，強制先讀後寫
// 核心設計：只有完整讀取（read_all_lines）才能滿足全量寫入工具的先讀要求，
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

// MarkFileFullyRead 標記文件已被完整讀取（僅由 read_all_lines 調用）
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
        globalWriteWithoutReadTracker.Reset()
}

// MarkFilePartialRead 標記文件已被部分讀取（由 read_file_line, text_grep 調用）
// 部分讀取不滿足任何寫入操作的先讀要求，僅作內部追蹤用途；
// 所有寫入操作統一要求完整讀取（read_all_lines）
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
                if time.Since(ts) > 10*time.Minute {
                        delete(t.partialReadFiles, key)
                        count++
                }
        }
        for key, ts := range t.fullReadFiles {
                if count >= 50 {
                        break
                }
                if time.Since(ts) > 10*time.Minute {
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
        if ts, ok := t.fullReadFiles[filePath]; ok && time.Since(ts) < 10*time.Minute {
                return readLevelFull
        }
        if ts, ok := t.partialReadFiles[filePath]; ok && time.Since(ts) < 10*time.Minute {
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
//   - 所有寫入工具（write_file_line, write_all_lines, append_to_file,
//     write_file_range, text_replace, text_transform）統一要求完整讀取（read_all_lines）
//   - read_file_line 或 text_grep 僅讀取部分內容，不被視為已讀過文件
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
                return fmt.Errorf("安全檢查失敗：你必須先使用 read_all_lines 完整讀取 %s 才能進行寫入/編輯操作（read_file_line / read_file_range 或 text_grep 僅讀取部分內容，不被視為已讀過文件）。這是為了確保你理解現有文件內容後再修改。", filePath)
        }
        return nil
}

// ============================================================================
// 寫入前未讀取違規追蹤（Write-Without-Read Violation Tracking）
// ============================================================================

// writeWithoutReadTracker 追蹤連續寫入違規次數。當模型連續 3 次在未讀取文件的情況下
// 嘗試寫入，觸發強制終止，以用戶身份將錯誤信息注入消息歷史。
type writeWithoutReadTracker struct {
	mu                    sync.Mutex
	consecutiveViolations int
	violationMessages     []string // 保存每次違規的完整錯誤信息
}

var globalWriteWithoutReadTracker = &writeWithoutReadTracker{}

const maxWriteWithoutReadViolations = 3

// RecordViolation 記錄一次寫入違規。返回 shouldStop=true 表示已達到最大違規次數，
// 需要強制終止並以用戶消息形式通知模型。
func (t *writeWithoutReadTracker) RecordViolation(filePath, errMsg string) (shouldStop bool, userMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.consecutiveViolations++
	t.violationMessages = append(t.violationMessages, errMsg)

	if t.consecutiveViolations >= maxWriteWithoutReadViolations {
		var sb strings.Builder
		sb.WriteString("以下是你連續多次無視安全檢查錯誤的記錄：\n\n")
		for i, msg := range t.violationMessages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("你必須使用 read_all_lines 完整讀取目標文件後才能進行寫入操作。請立即讀取相關文件。")

		userMsg = sb.String()
		shouldStop = true

		// 重置計數器，為下一輪追蹤做準備
		t.consecutiveViolations = 0
		t.violationMessages = nil
	}

	return
}

// Reset 重置所有違規計數和消息
func (t *writeWithoutReadTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.consecutiveViolations = 0
	t.violationMessages = nil
}

// ============================================================================
// 未知工具引导（Unknown Tool Guidance）
// ============================================================================

// allKnownToolNames 所有已知工具名称列表
// 用于在模型调用不存在的工具时提供模糊匹配建议
var allKnownToolNames = []string{
        // 命令执行
        "smart_shell", "shell",
        "shell_delayed", "shell_delayed_check", "shell_delayed_terminate",
        "shell_delayed_list", "shell_delayed_wait", "shell_delayed_remove",
        "spawn", "spawn_check", "spawn_list", "spawn_cancel",
        // 文件操作
        "read_file_line", "read_file_range", "write_file_line", "read_all_lines", "write_all_lines",
        "append_to_file", "write_file_range",
        // 文本处理
        "text_search", "text_grep", "text_replace", "text_transform",
        // 浏览器
        "browser_visit", "browser_search", "browser_download", "browser_click",
        "browser_type", "browser_scroll", "browser_screenshot", "browser_execute_js",
        "browser_fill_form", "browser_hover", "browser_double_click", "browser_navigate",
        "browser_wait_element", "browser_wait_smart",
        "browser_extract_links", "browser_extract_images", "browser_extract_elements",
        "browser_right_click", "browser_drag",
        "browser_get_cookies", "browser_cookie_save", "browser_cookie_load",
        "browser_snapshot", "browser_upload_file", "browser_select_option",
        "browser_key_press", "browser_element_screenshot",
        "browser_pdf", "browser_pdf_from_file",
        "browser_set_headers", "browser_set_user_agent", "browser_emulate_device",
        "browser_interact", "browser_extract", "browser_form_fill",
        // 记忆
        "memory_save", "memory_recall", "memory_forget", "memory_list",
        // 插件
        "plugin_list", "plugin_create", "plugin_load", "plugin_call",
        "plugin_unload", "plugin_reload", "plugin_compile", "plugin_delete",
        "plugin_apis", "plugin_detail",
        // Cron
        "cron_add", "cron_list", "cron_remove", "cron_status", "todos",
        // SSH
        "ssh_connect", "ssh_exec", "ssh_list", "ssh_close",
        // 技能
        "skill_list", "skill_create", "skill_delete", "skill_get",
        "skill_load", "skill_reload", "skill_update", "skill_suggest", "skill_stats",
        "skill_evaluate",
        // 配置
        "profile_check", "profile_reload", "actor_identity_set", "actor_identity_clear",
        // Plan Mode
        "plan_write", "plan_read", "enter_plan_mode", "exit_plan_mode", "next_phase",
        // 记忆整合
        "consolidate_memory",
        // 其他
        "scheme_eval", "opencli",
        // 工具菜单
        "menu",
}

// FindSimilarTool 找到与输入最相似的工具名称
// 使用简单的编辑距离算法
func FindSimilarTool(input string) string {
        input = strings.ToLower(strings.TrimSpace(input))

        bestMatch := ""
        bestDistance := len(input) + 5 // 初始阈值

        for _, name := range allKnownToolNames {
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

// GetUnknownToolErrorMessage 生成未知工具的错误消息
func GetUnknownToolErrorMessage(toolName string) string {
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
        "read_file_line": true,
        "read_all_lines":  true,
        "text_search":     true,
        "text_grep":       true,
        "memory_recall":   true,
        "memory_list":     true,
        "plan_read":       true,
        "plugin_list":     true,
        "skill_list":      true,
        "skill_get":       true,
        "cron_list":       true,
        "cron_status":     true,
        "spawn_list":      true,
        "ssh_list":        true,
        "profile_check":   true,
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
        // Plan Mode 权限检查（分階段工具控制）
        if globalPlanMode.IsActive() && !globalPlanMode.IsToolAllowedInPlanMode(toolName) {
                currentPhase := globalPlanMode.PhaseName()
                var content string
                // 針對模型常見誤操作給出明確指引，防止死循環
                switch toolName {
                case "enter_plan_mode":
                        content = fmt.Sprintf("你已經在 Plan Mode 中（%s）。不要重複調用 enter_plan_mode。\n\n當前可用操作：\n- 使用只讀工具（read_file_line, read_file_range, read_all_lines, text_search, text_grep）探索專案文件\n- 使用 spawn 創建並行子代理\n- 完成當前階段後調用 next_phase 推進\n- 如需退出 Plan Mode，使用 exit_plan_mode", currentPhase)
                case "smart_shell", "shell":
                        content = fmt.Sprintf("Plan Mode %s 中不允許使用 shell/smart_shell。此階段僅允許只讀工具。\n\n請改用：\n- read_file_line / read_file_range / read_all_lines 讀取文件\n- text_search / text_grep 搜索內容\n- spawn 創建只讀子代理\n\n完成當前階段後調用 next_phase 進入下一階段（設計階段起可以使用寫入工具）。", currentPhase)
                case "write_file_line", "write_all_lines", "append_to_file", "write_file_range", "text_replace":
                        content = fmt.Sprintf("Plan Mode %s 中不允許使用寫入工具 '%s'。先完成探索和設計，最終計劃確認後再執行寫入操作。\n\n當前階段請使用只讀工具。完成後調用 next_phase。", currentPhase, toolName)
                default:
                        content = fmt.Sprintf("Plan Mode %s 中不允許使用工具 '%s'。當前階段可用工具有限。完成後請調用 next_phase 推進到下一階段。", currentPhase, toolName)
                }
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusFailed},
                }
        }

        // Plan Mode 专用工具处理
        switch toolName {
        case "plan_write":
                content := handlePlanWrite(argsMap)
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        case "plan_read":
                content := handlePlanRead(argsMap)
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        case "enter_plan_mode":
                // enter_plan_mode 仍然作為靜態 Core 工具存在
                // 但如果在 Plan Mode 中被調用，給出明確指引防止重複調用死循環
                if globalPlanMode.IsActive() {
                        currentPhase := globalPlanMode.PhaseName()
                        content := fmt.Sprintf("你已經在 Plan Mode 中（%s），無需再次調用 enter_plan_mode。\n\n請根據當前階段使用可用工具：\n- Phase 1: 使用 read_file_line, read_file_range, read_all_lines, text_search, text_grep, spawn\n- Phase 2/4: 使用 plan_write, plan_read\n- 任何階段: 調用 next_phase 推進\n- 需要退出: 使用 exit_plan_mode", currentPhase)
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                        return EnrichedMessage{
                                Content: content,
                                Meta:    MessageMeta{Status: TaskStatusFailed},
                        }
                }
                taskDesc, _ := argsMap["task"].(string)
                errMsg := EnterPlanMode(taskDesc)
                if errMsg != "" {
                        // 規劃模式未啟用
                        emitToolCallTags(ch, toolName, argsMap, errMsg, TaskStatusFailed)
                        return EnrichedMessage{
                                Content: errMsg,
                                Meta:    MessageMeta{Status: TaskStatusFailed},
                        }
                }
                content := "已進入 Plan Mode Phase 1（初始理解）。使用只讀工具探索專案文件，善用 spawn 並行探索。完成后調用 next_phase。"
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        case "exit_plan_mode":
                // 舊接口兼容：等同於 /plan off（強制退出，跳過剩餘階段）
                if !globalPlanMode.IsActive() {
                        content := "Plan Mode 當前未激活。"
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                        return EnrichedMessage{
                                Content: content,
                                Meta:    MessageMeta{Status: TaskStatusSuccess},
                        }
                }
                planContent := ExitPlanMode()
                if planContent != "" {
                        content := fmt.Sprintf("已強制退出 Plan Mode。計劃如下：\n\n%s\n\n現在你可以使用所有工具來執行計劃。", planContent)
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                        return EnrichedMessage{
                                Content: content,
                                Meta:    MessageMeta{Status: TaskStatusSuccess},
                        }
                }
                content := "已強制退出 Plan Mode。現在你可以使用所有工具。"
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        }

        // 未知工具检查 - 检查是否是已知工具
        isKnown := false
        for _, name := range allKnownToolNames {
                if name == toolName {
                        isKnown = true
                        break
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
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                        return EnrichedMessage{
                                Content: content,
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
                                shouldStop, userMsg := globalWriteWithoutReadTracker.RecordViolation(filePath, errStr)
                                if shouldStop {
                                        // 連續 3 次違規：前端只顯示一般錯誤，內部返回 force-stop 標記
                                        // 主循環檢測標記後會以用戶身份注入消息（僅模型可見）
                                        emitToolCallTags(ch, toolName, argsMap, errStr, TaskStatusFailed)
                                        return EnrichedMessage{
                                                Content: forceStopWriteWithoutReadPrefix + userMsg,
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
                "write_file_line": true,
                "write_all_lines": true,
                "append_to_file":  true,
                "write_file_range": true,
                "text_replace":    true,
                "text_transform":  true,
                "memory_save":     true,
                "memory_forget":   true,
        }
        return writeTools[toolName]
}

// extractFilePathFromArgs 从工具参数中提取文件路径
func extractFilePathFromArgs(args map[string]interface{}) string {
        // 尝试常见的文件路径参数名
        for _, key := range []string{"file_path", "filePath", "path", "filename", "file"} {
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
//   - executeTool.go: execReadAllLines -> MarkFileFullyRead
//   - executeTool.go: execReadFileLine -> MarkFilePartialRead
//   - text_replace_tools.go: handleTextSearch (text_grep) -> MarkFilePartialRead
func init() {
        log.Printf("[ToolSafety] 工具安全网已初始化: MaxIterations=%d, ReadOnlyTools=%d",
                MaxAgentLoopIterations, len(ReadOnlyTools))
}
