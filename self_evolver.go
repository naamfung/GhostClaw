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
	lastPromptAnalysis      time.Time
	lastToolAnalysis        time.Time
	lastErrorAnalysis       time.Time
	lastCrossSessionAnalysis time.Time

	// 冷卻間隔
	minPromptInterval time.Duration // 30 min
	minToolInterval   time.Duration // 20 min
	minErrorInterval  time.Duration // 15 min
	minCrossInterval  time.Duration // 60 min

	// 跨 session 追蹤
	sessionsAnalyzed     map[string]bool
	analyzedSessionCount int

	// 觸發閾值
	minSessionsForCrossAnalysis int // >= 5 個 session 先做跨 session 匯總
	minToolCallsForAnalysis     int // >= 10 個 tool call 先做工具鏈分析
}

var globalSelfEvolver = &SelfEvolver{
	minPromptInterval:           30 * time.Minute,
	minToolInterval:             20 * time.Minute,
	minErrorInterval:            15 * time.Minute,
	minCrossInterval:            60 * time.Minute,
	sessionsAnalyzed:            make(map[string]bool),
	minSessionsForCrossAnalysis: 5,
	minToolCallsForAnalysis:     10,
}

// ============================================================
// AnalyzePromptEffectiveness — 分析 system prompt 對行為嘅影響
// ============================================================
func (se *SelfEvolver) AnalyzePromptEffectiveness(ctx context.Context, sessionID string) {
	if globalSessionPersist == nil || globalUnifiedMemory == nil || !se.canRun("prompt") {
		return
	}

	// 加載完整消息鏈（含 system prompt）
	messages := se.loadFullMessageChain(sessionID)
	if len(messages) < 4 {
		return // 太少數據
	}

	// 搵出 system prompt 同後續行為
	systemMsgs, userMsgs, assistantMsgs, toolMsgs := se.categorizeMessages(messages)
	if len(systemMsgs) == 0 || len(userMsgs) == 0 {
		return
	}

	prompt := se.buildPromptAnalysisPrompt(systemMsgs, userMsgs, assistantMsgs, toolMsgs)
	if prompt == "" {
		return
	}

	messages = []Message{
		{Role: "system", Content: promptAnalysisSystemPrompt},
		{Role: "user", Content: prompt},
	}
	useAPIType, useBaseURL, useAPIKey, useModelID, _, _, _, _ := getEffectiveAPIConfig()
	resp, err := CallModelSync(ctx, messages, useAPIType, useBaseURL, useAPIKey, useModelID, 0, 300, false, false)
	if err != nil {
		log.Printf("[SelfEvolver] PromptAnalysis LLM call failed: %v", err)
		return
	}
	content, ok := resp.Content.(string)
	if !ok || content == "" {
		if rc, ok2 := resp.ReasoningContent.(string); ok2 && rc != "" {
			content = rc
		}
	}
	if content == "" {
		log.Printf("[SelfEvolver] PromptAnalysis empty response content")
		return
	}

	se.processAnalysisResult(content, "prompt_insight")
	se.markSessionAnalyzed(sessionID)
}

// ============================================================
// AnalyzeToolPatterns — 分析工具調用鏈條
// ============================================================
func (se *SelfEvolver) AnalyzeToolPatterns(ctx context.Context, sessionID string) {
	if globalSessionPersist == nil || globalUnifiedMemory == nil || !se.canRun("tool") {
		return
	}

	messages := se.loadFullMessageChain(sessionID)
	_, _, _, toolMsgs := se.categorizeMessages(messages)

	toolCallCount := se.countToolCalls(toolMsgs)
	if toolCallCount < se.minToolCallsForAnalysis {
		return
	}

	prompt := se.buildToolPatternPrompt(toolMsgs, toolCallCount)
	if prompt == "" {
		return
	}

	messages = []Message{
		{Role: "system", Content: toolAnalysisSystemPrompt},
		{Role: "user", Content: prompt},
	}
	useAPIType, useBaseURL, useAPIKey, useModelID, _, _, _, _ := getEffectiveAPIConfig()
	resp, err := CallModelSync(ctx, messages, useAPIType, useBaseURL, useAPIKey, useModelID, 0, 300, false, false)
	if err != nil {
		log.Printf("[SelfEvolver] ToolAnalysis LLM call failed: %v", err)
		return
	}
	content, ok := resp.Content.(string)
	if !ok || content == "" {
		if rc, ok2 := resp.ReasoningContent.(string); ok2 && rc != "" {
			content = rc
		}
	}
	if content == "" {
		log.Printf("[SelfEvolver] ToolAnalysis empty response content")
		return
	}

	se.processAnalysisResult(content, "tool_pattern")
	se.markSessionAnalyzed(sessionID)
}

// ============================================================
// AnalyzeErrorRecovery — 分析錯誤恢復模式
// ============================================================
func (se *SelfEvolver) AnalyzeErrorRecovery(ctx context.Context, sessionID string) {
	if globalSessionPersist == nil || globalUnifiedMemory == nil || !se.canRun("error") {
		return
	}

	messages := se.loadFullMessageChain(sessionID)
	_, _, _, toolMsgs := se.categorizeMessages(messages)

	errorChains := se.extractErrorChains(toolMsgs)
	if len(errorChains) == 0 {
		return
	}

	prompt := se.buildErrorRecoveryPrompt(errorChains)
	if prompt == "" {
		return
	}

	messages = []Message{
		{Role: "system", Content: errorAnalysisSystemPrompt},
		{Role: "user", Content: prompt},
	}
	useAPIType, useBaseURL, useAPIKey, useModelID, _, _, _, _ := getEffectiveAPIConfig()
	resp, err := CallModelSync(ctx, messages, useAPIType, useBaseURL, useAPIKey, useModelID, 0, 300, false, false)
	if err != nil {
		log.Printf("[SelfEvolver] ErrorAnalysis LLM call failed: %v", err)
		return
	}
	content, ok := resp.Content.(string)
	if !ok || content == "" {
		if rc, ok2 := resp.ReasoningContent.(string); ok2 && rc != "" {
			content = rc
		}
	}
	if content == "" {
		log.Printf("[SelfEvolver] ErrorAnalysis empty response content")
		return
	}

	se.processAnalysisResult(content, "error_recovery")
	se.markSessionAnalyzed(sessionID)
}

// ============================================================
// SynthesizeCrossSession — 跨 session 匯總，歸納通用策略
// ============================================================
func (se *SelfEvolver) SynthesizeCrossSession(ctx context.Context) {
	if globalSessionPersist == nil || globalUnifiedMemory == nil || !se.canRun("cross") {
		return
	}

	se.mu.Lock()
	count := se.analyzedSessionCount
	se.mu.Unlock()

	if count < se.minSessionsForCrossAnalysis {
		return
	}

	// 加載多個 session 嘅 messages
	allMessages := se.loadMultiSessionMessages(5)
	if len(allMessages) == 0 {
		return
	}

	prompt := se.buildCrossSessionPrompt(allMessages)
	if prompt == "" {
		return
	}

	messages := []Message{
		{Role: "system", Content: crossSessionSystemPrompt},
		{Role: "user", Content: prompt},
	}
	useAPIType, useBaseURL, useAPIKey, useModelID, _, _, _, _ := getEffectiveAPIConfig()
	resp, err := CallModelSync(ctx, messages, useAPIType, useBaseURL, useAPIKey, useModelID, 0, 300, false, false)
	if err != nil {
		log.Printf("[SelfEvolver] CrossSession LLM call failed: %v", err)
		return
	}
	content, ok := resp.Content.(string)
	if !ok || content == "" {
		if rc, ok2 := resp.ReasoningContent.(string); ok2 && rc != "" {
			content = rc
		}
	}
	if content == "" {
		log.Printf("[SelfEvolver] CrossSession empty response content")
		return
	}

	se.processAnalysisResult(content, "cross_strategy")
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
	case "prompt":
		if now.Sub(se.lastPromptAnalysis) < se.minPromptInterval {
			return false
		}
		se.lastPromptAnalysis = now
	case "tool":
		if now.Sub(se.lastToolAnalysis) < se.minToolInterval {
			return false
		}
		se.lastToolAnalysis = now
	case "error":
		if now.Sub(se.lastErrorAnalysis) < se.minErrorInterval {
			return false
		}
		se.lastErrorAnalysis = now
	case "cross":
		if now.Sub(se.lastCrossSessionAnalysis) < se.minCrossInterval {
			return false
		}
		se.lastCrossSessionAnalysis = now
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
// 系統提示（每個維度獨立，確保 LLM 專注分析）
// ============================================================

var promptAnalysisSystemPrompt = `你是一個 Prompt 效能分析器。根據系統提示詞和後續的助理行為，分析系統提示詞的優缺點。

只輸出有價值的改進建議，嚴格按以下格式：

### PromptSuggestions
- 改進點簡述: 具體改進建議（一行）
- 改進點簡述: 具體改進建議（一行）

每條必須是 "- key: value" 格式。如果沒有值得記錄的建議，輸出 "### PromptSuggestions" 並留空。`

var toolAnalysisSystemPrompt = `你是一個工具使用模式分析器。根據工具調用歷史，識別低效模式、冗餘調用、可優化序列。

只輸出有價值的模式發現，嚴格按以下格式：

### Patterns
- 模式簡述: 具體發現和優化建議（一行）

每條必須是 "- key: value" 格式。如果沒有值得記錄的模式，輸出 "### Patterns" 並留空。`

var errorAnalysisSystemPrompt = `你是一個錯誤恢復模式分析器。根據工具錯誤鏈（error → retry → result），提取成功的恢復策略。

只輸出有價值的恢復模式，嚴格按以下格式：

### Patterns
- 恢復策略簡述: 具體策略描述（一行）

每條必須是 "- key: value" 格式。如果沒有值得記錄的策略，輸出 "### Patterns" 並留空。`

var crossSessionSystemPrompt = `你是一個跨任務策略分析器。根據多個會話的用戶請求和助理回應，歸納通用策略和行為模式。

只輸出有長期價值的通用策略，嚴格按以下格式：

### Strategies
- 策略簡述: 具體策略描述（一行）

每條必須是 "- key: value" 格式。不要記錄一次性事務信息。如果沒有值得記錄的策略，輸出 "### Strategies" 並留空。`
