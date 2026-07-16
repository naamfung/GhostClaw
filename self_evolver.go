package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// SelfEvolver 跨會話自進化引擎。
// 基於完整消息鏈（system prompt → user → assistant → tool → result）進行分析，
// 產生 Prompt 改進建議、工具鏈優化、錯誤恢復模式、跨任務策略，
// 全部存入 UnifiedMemory 供後續 memory injection 使用。
type SelfEvolver struct {
	mu sync.Mutex

	// 冷卻追蹤
	lastSessionAnalysis time.Time

	// 冷卻間隔
	minSessionInterval time.Duration // 30 min

	// 跨 session 追蹤
	sessionsAnalyzed     map[string]bool
	analyzedSessionCount int

	// 觸發閾值
	minSessionsForCrossAnalysis int // >= 5 個 session 才加入跨 session 部分
	minToolCallsForAnalysis     int // >= 10 個 tool call 才做工具鏈分析
}

var globalSelfEvolver = &SelfEvolver{
	minSessionInterval:           30 * time.Minute,
	sessionsAnalyzed:            make(map[string]bool),
	minSessionsForCrossAnalysis: 5,
	minToolCallsForAnalysis:     10,
}

// ============================================================
// AnalyzeSession — 綜合分析單個 session（合併原 4 個獨立分析）
// ============================================================
// 將 prompt 效能、工具模式、錯誤恢復、跨 session 策略合併為 1 次 LLM 調用，
// 減少 75% post-loop LLM 往返時間。
func (se *SelfEvolver) AnalyzeSession(ctx context.Context, sessionID string) {
	if globalSessionPersist == nil || globalUnifiedMemory == nil || !se.canRun("session") {
		return
	}

	// 加載完整消息鏈（含 system prompt）
	messages := se.loadFullMessageChain(sessionID)
	if len(messages) < 4 {
		return
	}

	systemMsgs, userMsgs, assistantMsgs, toolMsgs := se.categorizeMessages(messages)
	if len(systemMsgs) == 0 || len(userMsgs) == 0 {
		return
	}

	toolCallCount := se.countToolCalls(toolMsgs)
	errorChains := se.extractErrorChains(toolMsgs)

	// 跨 session 摘要（僅當已分析 >= 5 個 session 時加入）
	var crossSessionMsgs []Message
	se.mu.Lock()
	count := se.analyzedSessionCount
	se.mu.Unlock()
	if count >= se.minSessionsForCrossAnalysis {
		crossSessionMsgs = se.loadMultiSessionMessages(5)
	}

	// 構建綜合 prompt
	prompt := se.buildComprehensivePrompt(systemMsgs, userMsgs, assistantMsgs, toolMsgs, toolCallCount, errorChains, crossSessionMsgs)
	if prompt == "" {
		return
	}

	messages = []Message{
		{Role: "system", Content: comprehensiveAnalysisSystemPrompt},
		{Role: "user", Content: prompt},
	}
	useAPIType, useBaseURL, useAPIKey, useModelID, _, _, _, _ := getEffectiveAPIConfig()
	resp, err := CallModelSync(ctx, messages, useAPIType, useBaseURL, useAPIKey, useModelID, 0, 800, false, false)
	if err != nil {
		log.Printf("[SelfEvolver] AnalyzeSession LLM call failed: %v", err)
		return
	}
	content, ok := resp.Content.(string)
	if !ok || content == "" {
		if rc, ok2 := resp.ReasoningContent.(string); ok2 && rc != "" {
			content = rc
		}
	}
	if content == "" {
		log.Printf("[SelfEvolver] AnalyzeSession empty response content")
		return
	}

	// 按 section 分別存入對應 prefix
	se.processComprehensiveResult(content)
	se.markSessionAnalyzed(sessionID)
}

// buildComprehensivePrompt 構建綜合分析 prompt，合併 4 個維度的數據
func (se *SelfEvolver) buildComprehensivePrompt(
	systemMsgs, userMsgs, assistantMsgs, toolMsgs []Message,
	toolCallCount int,
	errorChains [][]Message,
	crossSessionMsgs []Message,
) string {
	var sb strings.Builder

	// 1. Prompt 效能分析數據
	sb.WriteString("# Section 1: Prompt 效能分析\n")
	sb.WriteString(se.buildPromptAnalysisPrompt(systemMsgs, userMsgs, assistantMsgs, toolMsgs))

	// 2. 工具模式分析數據（僅當工具調用足夠時）
	if toolCallCount >= se.minToolCallsForAnalysis {
		sb.WriteString("\n# Section 2: 工具使用模式\n")
		sb.WriteString(se.buildToolPatternPrompt(toolMsgs, toolCallCount))
	}

	// 3. 錯誤恢復分析數據（僅當有錯誤鏈時）
	if len(errorChains) > 0 {
		sb.WriteString("\n# Section 3: 錯誤恢復模式\n")
		sb.WriteString(se.buildErrorRecoveryPrompt(errorChains))
	}

	// 4. 跨 session 策略數據（僅當有跨 session 消息時）
	if len(crossSessionMsgs) > 0 {
		sb.WriteString("\n# Section 4: 跨會話策略\n")
		sb.WriteString(se.buildCrossSessionPrompt(crossSessionMsgs))
	}

	return sb.String()
}

// processComprehensiveResult 按 section 分別存入對應 prefix
func (se *SelfEvolver) processComprehensiveResult(result string) {
	if globalUnifiedMemory == nil {
		return
	}

	// section header → prefix 映射
	sectionPrefix := map[string]string{
		"### PromptSuggestions": "prompt_insight",
		"### ToolPatterns":     "tool_pattern",
		"### ErrorRecovery":    "error_recovery",
		"### CrossSession":     "cross_strategy",
	}

	savedByPrefix := map[string]int{}
	lines := strings.Split(result, "\n")
	currentPrefix := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 檢查是否是 section header
		if prefix, ok := sectionPrefix[trimmed]; ok {
			currentPrefix = prefix
			continue
		}
		// 其他 ### header 重置
		if strings.HasPrefix(trimmed, "###") {
			currentPrefix = ""
			continue
		}

		if currentPrefix == "" || !strings.HasPrefix(trimmed, "- ") {
			continue
		}

		entry := strings.TrimPrefix(trimmed, "- ")
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}

		memKey := fmt.Sprintf("%s_%s", currentPrefix, key)
		if err := globalUnifiedMemory.SaveEntry(MemoryCategoryExperience, memKey, value, nil, MemoryScopeUser); err != nil {
			continue
		}
		savedByPrefix[currentPrefix]++
	}

	for prefix, count := range savedByPrefix {
		if count > 0 {
			log.Printf("[SelfEvolver] Saved %d insights (prefix=%s)", count, prefix)
		}
	}
}

// ============================================================
// 輔助方法
// ============================================================

// canRun 檢查冷卻，返回是否可以執行
func (se *SelfEvolver) canRun(dimension string) bool {
	se.mu.Lock()
	defer se.mu.Unlock()

	now := time.Now()
	switch dimension {
	case "session":
		if now.Sub(se.lastSessionAnalysis) < se.minSessionInterval {
			return false
		}
		se.lastSessionAnalysis = now
	}
	return true
}

// loadFullMessageChain 從 DB 加載完整消息鏈
func (se *SelfEvolver) loadFullMessageChain(sessionID string) []Message {
	saved, err := globalSessionPersist.LoadSession(sessionID)
	if err != nil || saved == nil {
		return nil
	}
	return saved.History
}

// markSessionAnalyzed 標記 session 已分析（只在分析實際執行後調用）
func (se *SelfEvolver) markSessionAnalyzed(sessionID string) {
	se.mu.Lock()
	defer se.mu.Unlock()
	if !se.sessionsAnalyzed[sessionID] {
		se.sessionsAnalyzed[sessionID] = true
		se.analyzedSessionCount++
	}
}

// loadMultiSessionMessages 加載多個 session 嘅消息
func (se *SelfEvolver) loadMultiSessionMessages(count int) []Message {
	sessions, err := globalSessionPersist.ListSessions()
	if err != nil || len(sessions) == 0 {
		return nil
	}

	var allMessages []Message
	loaded := 0
	for _, s := range sessions {
		if loaded >= count {
			break
		}
		saved, err := globalSessionPersist.LoadSession(s.ID)
		if err != nil || saved == nil || len(saved.History) < 4 {
			continue
		}
		// 只取每個 session 嘅最近 30 條
		msgs := saved.History
		if len(msgs) > 30 {
			msgs = msgs[len(msgs)-30:]
		}
		allMessages = append(allMessages, msgs...)
		loaded++
	}
	return allMessages
}

// categorizeMessages 將消息按角色分類
func (se *SelfEvolver) categorizeMessages(messages []Message) (system, user, assistant, tool []Message) {
	for _, msg := range messages {
		switch msg.Role {
		case "system":
			system = append(system, msg)
		case "user":
			user = append(user, msg)
		case "assistant":
			assistant = append(assistant, msg)
		case "tool":
			tool = append(tool, msg)
		}
	}
	return
}

// countToolCalls 計算工具調用總數（tool 角色消息數）
func (se *SelfEvolver) countToolCalls(toolMsgs []Message) int {
	return len(toolMsgs)
}

// extractErrorChains 提取錯誤鏈（tool error → retry → recovery）
func (se *SelfEvolver) extractErrorChains(toolMsgs []Message) [][]Message {
	var chains [][]Message
	var currentChain []Message

	for _, msg := range toolMsgs {
		content, _ := msg.Content.(string)
		isError := strings.Contains(strings.ToLower(content), "error") ||
			strings.Contains(strings.ToLower(content), "failed") ||
			strings.Contains(strings.ToLower(content), "permission denied") ||
			strings.Contains(strings.ToLower(content), "not found")

		if isError {
			// 新錯誤開始：保存上一條鏈，開始新鏈
			if len(currentChain) > 0 {
				chains = append(chains, currentChain)
			}
			currentChain = []Message{msg}
		} else if len(currentChain) > 0 {
			// 非錯誤消息：擴展現有鏈（可能係 retry 或 recovery）
			currentChain = append(currentChain, msg)
		}
	}

	// 最後一條鏈
	if len(currentChain) > 0 {
		chains = append(chains, currentChain)
	}
	return chains
}

// ============================================================
// Prompt 構建
// ============================================================

func (se *SelfEvolver) buildPromptAnalysisPrompt(systemMsgs, userMsgs, assistantMsgs, toolMsgs []Message) string {
	var sb strings.Builder
	sb.WriteString("## System Prompt\n")
	for _, msg := range systemMsgs {
		content, _ := msg.Content.(string)
		sb.WriteString(TruncateRunes(content, 1000))
		sb.WriteString("\n")
	}

	sb.WriteString("\n## 用戶請求\n")
	for _, msg := range userMsgs {
		content, _ := msg.Content.(string)
		sb.WriteString(TruncateRunes(content, 200))
		sb.WriteString("\n")
	}

	sb.WriteString("\n## Assistant 行為\n")
	limit := 5
	for i := len(assistantMsgs) - 1; i >= 0 && limit > 0; i-- {
		content, _ := assistantMsgs[i].Content.(string)
		if content != "" {
			sb.WriteString(TruncateRunes(content, 300))
			sb.WriteString("\n")
		}
		if assistantMsgs[i].ToolCalls != nil {
			sb.WriteString("[使用了工具]\n")
		}
		limit--
	}

	sb.WriteString(fmt.Sprintf("\n## 工具調用統計\n總工具消息數: %d\n", len(toolMsgs)))
	errorCount := 0
	for _, msg := range toolMsgs {
		content, _ := msg.Content.(string)
		if strings.Contains(strings.ToLower(content), "error") {
			errorCount++
		}
	}
	sb.WriteString(fmt.Sprintf("錯誤數: %d\n", errorCount))

	return sb.String()
}

func (se *SelfEvolver) buildToolPatternPrompt(toolMsgs []Message, callCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 工具調用總數: %d\n\n", callCount))

	// 提取工具名稱序列
	toolNames := make(map[string]int)
	var sequence []string
	for _, msg := range toolMsgs {
		if msg.ToolCallID != "" {
			sequence = append(sequence, msg.ToolCallID)
		}
		content, _ := msg.Content.(string)
		// 嘗試從 content 推斷工具名
		for _, name := range []string{"read_file", "write_file", "bash", "shell", "web_fetch", "web_search", "grep", "glob", "edit", "task"} {
			if strings.Contains(strings.ToLower(content), name) {
				toolNames[name]++
				break
			}
		}
	}

	sb.WriteString("## 工具使用頻率\n")
	for name, count := range toolNames {
		sb.WriteString(fmt.Sprintf("- %s: %d 次\n", name, count))
	}

	sb.WriteString(fmt.Sprintf("\n## 工具調用序列長度: %d\n", len(sequence)))

	// 最近 5 條工具結果
	sb.WriteString("\n## 最近工具結果\n")
	limit := 5
	for i := len(toolMsgs) - 1; i >= 0 && limit > 0; i-- {
		content, _ := toolMsgs[i].Content.(string)
		sb.WriteString(TruncateRunes(content, 200))
		sb.WriteString("\n")
		limit--
	}

	return sb.String()
}

func (se *SelfEvolver) buildErrorRecoveryPrompt(errorChains [][]Message) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 錯誤鏈總數: %d\n\n", len(errorChains)))

	for i, chain := range errorChains {
		if i >= 3 {
			break
		}
		sb.WriteString(fmt.Sprintf("### 錯誤鏈 %d\n", i+1))
		for _, msg := range chain {
			content, _ := msg.Content.(string)
			sb.WriteString(fmt.Sprintf("[%s] %s\n", msg.Role, TruncateRunes(content, 300)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (se *SelfEvolver) buildCrossSessionPrompt(allMessages []Message) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 跨會話消息總數: %d\n\n", len(allMessages)))

	// 提取每個 session 嘅任務摘要
	_, userMsgs, assistantMsgs, _ := se.categorizeMessages(allMessages)

	sb.WriteString("## 用戶請求樣本\n")
	limit := 10
	for i := len(userMsgs) - 1; i >= 0 && limit > 0; i-- {
		content, _ := userMsgs[i].Content.(string)
		if content != "" && !strings.Contains(content, "[SYSTEM") {
			sb.WriteString(fmt.Sprintf("- %s\n", TruncateRunes(content, 200)))
			limit--
		}
	}

	sb.WriteString("\n## Assistant 回應模式\n")
	limit = 5
	for i := len(assistantMsgs) - 1; i >= 0 && limit > 0; i-- {
		content, _ := assistantMsgs[i].Content.(string)
		if content != "" {
			sb.WriteString(fmt.Sprintf("- %s\n", TruncateRunes(content, 300)))
			limit--
		}
	}

	return sb.String()
}

// ============================================================
// 結果處理 — 存入 UnifiedMemory
// ============================================================

func (se *SelfEvolver) processAnalysisResult(result string, prefix string) {
	if globalUnifiedMemory == nil {
		return
	}

	saved := 0
	lines := strings.Split(result, "\n")
	var currentCategory MemoryCategory

	for _, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "### Insights") || strings.HasPrefix(line, "## Insights"):
			currentCategory = MemoryCategoryExperience
			continue
		case strings.HasPrefix(line, "### Patterns") || strings.HasPrefix(line, "## Patterns"):
			currentCategory = MemoryCategoryExperience
			continue
		case strings.HasPrefix(line, "### Strategies") || strings.HasPrefix(line, "## Strategies"):
			currentCategory = MemoryCategoryExperience
			continue
		case strings.HasPrefix(line, "### PromptSuggestions") || strings.HasPrefix(line, "## PromptSuggestions"):
			currentCategory = MemoryCategoryExperience
			continue
		case strings.HasPrefix(line, "###") || strings.HasPrefix(line, "##"):
			currentCategory = ""
			continue
		}

		if currentCategory == "" || !strings.HasPrefix(line, "- ") {
			continue
		}

		entry := strings.TrimPrefix(line, "- ")
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}

		memKey := fmt.Sprintf("%s_%s", prefix, key)
		if err := globalUnifiedMemory.SaveEntry(currentCategory, memKey, value, nil, MemoryScopeUser); err != nil {
			log.Printf("[SelfEvolver] Failed to save %s/%s: %v", currentCategory, memKey, err)
			continue
		}
		saved++
	}

	if saved > 0 {
		log.Printf("[SelfEvolver] Saved %d insights (prefix=%s)", saved, prefix)
	}
}

// ============================================================
// 系統提示（綜合分析，一次調用覆蓋 4 個維度）
// ============================================================

var comprehensiveAnalysisSystemPrompt = `你是一個綜合會話分析器。根據完整消息鏈，從 4 個維度分析並輸出改進建議。

嚴格按以下格式輸出 4 個 section（每條必須是 "- key: value" 格式）：

### PromptSuggestions
- 改進點簡述: 具體改進建議（一行）

### ToolPatterns
- 模式簡述: 具體發現和優化建議（一行）

### ErrorRecovery
- 恢復策略簡述: 具體策略描述（一行）

### CrossSession
- 策略簡述: 具體策略描述（一行）

如果某個 section 沒有值得記錄的內容，輸出 section header 後留空。不要記錄一次性事務信息。`
