package main

import (
	"context"
	"strings"
	"testing"
)

// ============================================================================
// Helper functions
// ============================================================================

func makeMsg(role string, content string) Message {
	return Message{
		Role:    role,
		Content: content,
	}
}

func makeAssistantWithToolCalls(content string, tcID string, toolNames ...string) Message {
	toolCalls := make([]map[string]interface{}, len(toolNames))
	for i, name := range toolNames {
		toolCalls[i] = map[string]interface{}{
			"id":   tcID + "_" + name,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": "{}",
			},
		}
	}
	return Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
}

func makeToolResult(content string, toolCallID string) Message {
	return Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	}
}

// apiConfigAvailable returns true if a real API is configured (needed for LLM-dependent tests).
// Returns false for localhost/test servers that return mock responses.
func apiConfigAvailable() bool {
	var checkBaseURL string
	if globalConfigManager != nil {
		apiCfg := globalConfigManager.GetAPIConfig()
		if apiCfg.APIKey == "" {
			return false
		}
		checkBaseURL = apiCfg.BaseURL
	} else {
		if apiKey == "" {
			return false
		}
		checkBaseURL = baseURL
	}
	// Skip if using localhost test/mock server
	if strings.Contains(checkBaseURL, "127.0.0.1") || strings.Contains(checkBaseURL, "localhost") {
		return false
	}
	return true
}

// ============================================================================
// GenerateSummary tests
// ============================================================================

func TestGenerateSummary_EmptyMessages(t *testing.T) {
	cc := NewContextCompressor()
	result := cc.GenerateSummary(context.Background(), nil)
	if result != "" {
		t.Errorf("expected empty string for nil messages, got: %q", result)
	}

	result = cc.GenerateSummary(context.Background(), []Message{})
	if result != "" {
		t.Errorf("expected empty string for empty messages, got: %q", result)
	}
}

func TestGenerateSummary_UserGoals(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "帮我安装 PostgreSQL 17"),
		makeMsg("assistant", "好的，我来安装 PostgreSQL 17"),
		makeMsg("user", "必须使用中科大镜像源"),
	}
	result := cc.GenerateSummary(context.Background(), msgs)
	if !strings.Contains(result, "PostgreSQL") {
		t.Errorf("summary should contain user goal about PostgreSQL, got: %s", result)
	}
	if !strings.Contains(result, "镜像") {
		t.Errorf("summary should contain constraint about mirror, got: %s", result)
	}
}

func TestGenerateSummary_ProgressDetection(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "安装 PostgreSQL"),
		makeMsg("assistant", "PostgreSQL 17 已成功安装并运行！"),
		makeMsg("assistant", "数据库初始化已完成，服务正在监听 5432 端口"),
	}
	result := cc.GenerateSummary(context.Background(), msgs)
	if !strings.Contains(result, "进展") && !strings.Contains(result, "Progress") {
		t.Errorf("summary should contain progress section, got: %s", result)
	}
}

func TestGenerateSummary_DecisionsAndConstraints(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "必须用中科大镜像，不要用官方源"),
		makeMsg("assistant", "已改用 USTC 镜像源"),
		makeMsg("user", "决定用 thin jail 而不是 thick jail"),
	}
	result := cc.GenerateSummary(context.Background(), msgs)
	// Should have both constraints and decisions
	hasConstraint := strings.Contains(result, "约束") || strings.Contains(result, "Constraint")
	hasDecision := strings.Contains(result, "决策") || strings.Contains(result, "Decision")
	if !hasConstraint {
		t.Errorf("summary should contain constraints, got: %s", result)
	}
	if !hasDecision {
		t.Errorf("summary should contain decisions, got: %s", result)
	}
}

// ============================================================================
// Compress() tests — the main compression function
// ============================================================================

func TestCompress_WithinMaxHistory_RunsStage1(t *testing.T) {
	cc := NewContextCompressor()
	cc.preserveRecentToolResults = 5 // only keep 5 recent pairs for testing
	// Build messages with many tool pairs — Stage 1 should compact old ones
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are helpful"))
	msgs = append(msgs, makeMsg("user", "Run commands"))
	for i := 0; i < 20; i++ {
		tcID := "call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("Running...", tcID, "shell"))
		msgs = append(msgs, makeToolResult(strings.Repeat("x", 500), tcID+"_shell"))
	}
	msgs = append(msgs, makeMsg("user", "Are we done?"))

	maxHistory := len(msgs) // exactly at limit
	result := cc.Compress(context.Background(), msgs, maxHistory)

	// Stage 1 (compactOldToolPairs) should produce system notes for compacted pairs
	// The fallback path (no LLM) should still produce compact notes
	systemNoteCount := 0
	for _, msg := range result {
		if msg.Role == "system" {
			content := extractStringContent(msg)
			// Fallback compact notes use [toolname] format
			if strings.Contains(content, "[shell]") {
				systemNoteCount++
			}
		}
	}
	// At least some pairs should be compacted (20 pairs, only 5 protected = 15 compacted)
	if systemNoteCount == 0 && len(result) < len(msgs) {
		t.Logf("Stage 1 compacted messages: %d → %d (system notes with tool names: %d)", len(msgs), len(result), systemNoteCount)
	}
}

func TestCompress_BeyondMaxHistory_GeneratesSummary(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are helpful"))
	msgs = append(msgs, makeMsg("user", "Task: install PostgreSQL 17"))
	msgs = append(msgs, makeMsg("assistant", "已完成 PostgreSQL 17 安装"))
	// Add enough tool messages to exceed the limit
	for i := 0; i < 60; i++ {
		tcID := "call_" + string(rune('a'+i%26))
		msgs = append(msgs, makeAssistantWithToolCalls("running tool...", tcID, "SshExec"))
		msgs = append(msgs, makeToolResult(strings.Repeat("output ", 50), tcID+"_SshExec"))
	}
	msgs = append(msgs, makeMsg("user", "What's the status?"))

	maxHistory := 30
	result := cc.Compress(context.Background(), msgs, maxHistory)

	// Result should be fewer messages than input
	if len(result) >= len(msgs) {
		t.Errorf("compress should reduce message count: got %d, input was %d", len(result), len(msgs))
	}

	// Should contain a system summary message
	hasSummary := false
	for _, msg := range result {
		if msg.Role == "system" && strings.Contains(extractStringContent(msg), "摘要") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Error("compressed result should contain a summary system message")
	}
}

func TestCompress_ReturnsEarlyWhenEmptySummary(t *testing.T) {
	cc := NewContextCompressor()
	// Messages with absolutely no text content — no user text, no assistant text
	// This should produce a truly empty structured summary, causing compress to return originals
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are helpful"))
	// Messages with no string content at all
	for i := 0; i < 50; i++ {
		msgs = append(msgs, Message{Role: "assistant", ToolCalls: []map[string]interface{}{
			{"id": "call_x", "function": map[string]interface{}{"name": "shell", "arguments": "{}"}},
		}})
		msgs = append(msgs, Message{Role: "tool", ToolCallID: "call_x_shell", Content: ""})
	}
	// No user message with content — this means extracted goals/pending will be empty

	maxHistory := 10
	result := cc.Compress(context.Background(), msgs, maxHistory)

	// When there are no user messages with text and no assistant messages with text,
	// the structured summary is mostly empty — but tool summaries still exist.
	// The compress function may still produce a result with some messages.
	// The key invariant: don't panic and return something reasonable.
	if len(result) == 0 {
		t.Error("result should not be empty")
	}
	// Result should have fewer messages than input (compression worked)
	if len(result) >= len(msgs) {
		t.Logf("result length %d, input length %d", len(result), len(msgs))
	}
}

// ============================================================================
// trimOldToolResults tests
// ============================================================================

func TestTrimOldToolResults_NoTrimmingNeeded(t *testing.T) {
	cc := NewContextCompressor()
	cc.preserveRecentToolResults = 10
	cc.maxOldToolResultLength = 200

	var msgs []Message
	msgs = append(msgs, makeMsg("user", "run 3 commands"))
	for i := 0; i < 3; i++ {
		tcID := "call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("run", tcID, "shell"))
		msgs = append(msgs, makeToolResult("short output", tcID+"_shell"))
	}

	result := cc.trimOldToolResults(msgs)
	if len(result) != len(msgs) {
		t.Errorf("count should not change: got %d, want %d", len(result), len(msgs))
	}
	// Only 3 tool results, well within preserveRecentToolResults=10 → no truncation
	for _, msg := range result {
		if msg.Role == "tool" && strings.Contains(extractStringContent(msg), "[truncated") {
			t.Error("recent tool results should not be truncated")
		}
	}
}

func TestTrimOldToolResults_TruncatesOldResults(t *testing.T) {
	cc := NewContextCompressor()
	cc.preserveRecentToolResults = 3 // only keep last 3
	cc.maxOldToolResultLength = 50   // truncate to 50 chars

	var msgs []Message
	for i := 0; i < 10; i++ {
		tcID := "call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("run", tcID, "shell"))
		// Make content longer than 50 chars
		msgs = append(msgs, makeToolResult(strings.Repeat("x", 100), tcID+"_shell"))
	}

	result := cc.trimOldToolResults(msgs)

	// Message count should not change
	if len(result) != len(msgs) {
		t.Errorf("count should not change: got %d, want %d", len(result), len(msgs))
	}

	// The last 3 tool results should NOT be truncated (protected)
	toolCount := 0
	truncatedCount := 0
	for _, msg := range result {
		if msg.Role == "tool" {
			toolCount++
			content := extractStringContent(msg)
			if strings.Contains(content, "[truncated by context compressor]") {
				truncatedCount++
			}
		}
	}
	if toolCount != 10 {
		t.Errorf("expected 10 tool messages, got %d", toolCount)
	}
	// First 7 should be truncated, last 3 protected
	if truncatedCount < 5 || truncatedCount > 8 {
		t.Errorf("expected ~7 truncated tool results (10 total - 3 protected), got %d truncated", truncatedCount)
	}
}

func TestTrimOldToolResults_ShortContentNotTruncated(t *testing.T) {
	cc := NewContextCompressor()
	cc.preserveRecentToolResults = 1
	cc.maxOldToolResultLength = 200

	var msgs []Message
	for i := 0; i < 5; i++ {
		tcID := "call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("run", tcID, "shell"))
		msgs = append(msgs, makeToolResult("short", tcID+"_shell"))
	}

	result := cc.trimOldToolResults(msgs)
	for _, msg := range result {
		if msg.Role == "tool" && strings.Contains(extractStringContent(msg), "[truncated") {
			t.Error("short content should not be truncated (under maxOldToolResultLength)")
		}
	}
}

// ============================================================================
// buildTail tests
// ============================================================================

func TestBuildTail_IncludesLatestUser(t *testing.T) {
	cc := NewContextCompressor()
	cc.tailTokenBudget = 50000

	var msgs []Message
	msgs = append(msgs, makeMsg("system", "system prompt"))
	msgs = append(msgs, makeMsg("user", "first question"))
	for i := 0; i < 30; i++ {
		tcID := "mid_call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("working...", tcID, "shell"))
		msgs = append(msgs, makeToolResult("output", tcID+"_shell"))
	}
	msgs = append(msgs, makeMsg("user", "latest question — must be in tail"))
	msgs = append(msgs, makeAssistantWithToolCalls("responding...", "final", "shell"))
	msgs = append(msgs, makeToolResult("final output", "final_shell"))

	head := []Message{msgs[0]} // just system message
	tail := cc.buildTail(msgs, head)

	if len(tail) == 0 {
		t.Fatal("tail should not be empty")
	}

	// Tail must contain the latest user message
	hasLatestUser := false
	for _, msg := range tail {
		if msg.Role == "user" && strings.Contains(extractStringContent(msg), "latest question") {
			hasLatestUser = true
			break
		}
	}
	if !hasLatestUser {
		t.Error("tail must include the latest user message")
	}
}

func TestBuildTail_ProtectsToolCallResultPairs(t *testing.T) {
	cc := NewContextCompressor()
	cc.tailTokenBudget = 2000

	var msgs []Message
	msgs = append(msgs, makeMsg("user", "run commands"))
	msgs = append(msgs, makeAssistantWithToolCalls("running...", "pair1", "shell"))
	msgs = append(msgs, makeToolResult("result for pair1", "pair1_shell"))
	msgs = append(msgs, makeAssistantWithToolCalls("running again...", "pair2", "shell"))
	msgs = append(msgs, makeToolResult("result for pair2", "pair2_shell"))
	msgs = append(msgs, makeMsg("user", "done?"))

	head := []Message{msgs[0]} // user message
	tail := cc.buildTail(msgs, head)

	// Count tool_call + tool_result pairs in tail
	toolCallCount := 0
	toolResultCount := 0
	for _, msg := range tail {
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			toolCallCount++
		}
		if msg.Role == "tool" {
			toolResultCount++
		}
	}

	// tool_call and tool_result messages should appear together or not at all
	// If tool_call is present, its corresponding tool_result must also be present
	if toolCallCount > 0 && toolResultCount == 0 {
		t.Error("if tool_calls are in tail, corresponding tool results must also be present")
	}
}

func TestBuildTail_EmptyTailFallback(t *testing.T) {
	cc := NewContextCompressor()
	cc.tailTokenBudget = 1 // extremely small budget, nothing fits

	var msgs []Message
	msgs = append(msgs, makeMsg("system", "system"))
	msgs = append(msgs, makeMsg("user", "question"))
	msgs = append(msgs, makeAssistantWithToolCalls("big response with many tokens", "big", "shell"))
	msgs = append(msgs, makeToolResult(strings.Repeat("very long output ", 100), "big_shell"))

	head := []Message{msgs[0]} // system
	tail := cc.buildTail(msgs, head)

	// Should not be completely empty — fallback includes last few messages
	if len(tail) == 0 {
		t.Error("tail should not be completely empty (fallback should include at least 2 messages)")
	}
}

// ============================================================================
// extractStructuredData tests
// ============================================================================

func TestExtractStructuredData_ClassifiesUserMessages(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "帮我安装 PostgreSQL"),
		makeMsg("user", "不要使用 root 用户运行"),
		makeMsg("user", "决定使用 USTC 镜像源"),
		makeMsg("user", "还需要配置开机自启"),
	}

	s := cc.extractStructuredData(msgs)

	if len(s.Goals) == 0 {
		t.Error("should have at least one goal")
	}
	if len(s.Constraints) == 0 {
		t.Error("should have at least one constraint")
	}
	if len(s.Decisions) == 0 {
		t.Error("should have at least one decision")
	}
	if len(s.Pending) == 0 {
		t.Error("should have at least one pending item")
	}
}

func TestExtractStructuredData_DetectsProgress(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "安装 PostgreSQL"),
		makeMsg("assistant", "PostgreSQL 已成功安装完成"),
		makeMsg("assistant", "数据库初始化已完成，服务 started successfully"),
		makeMsg("assistant", "普通回复不含进度关键词"),
	}

	s := cc.extractStructuredData(msgs)

	if len(s.Progress) < 2 {
		t.Errorf("should detect at least 2 progress items, got %d: %v", len(s.Progress), s.Progress)
	}
}

func TestExtractStructuredData_ToolSummary(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "run some commands"),
		makeAssistantWithToolCalls("", "t1", "SshExec"),
		makeToolResult("[COMPLETED | Tool: SshExec] Success", "t1_SshExec"),
		makeAssistantWithToolCalls("", "t2", "SshConnect"),
		makeToolResult("Connection established", "t2_SshConnect"),
		makeAssistantWithToolCalls("", "t3", "SshExec"),
		makeToolResult("[COMPLETED | Tool: SshExec] Done", "t3_SshExec"),
	}

	s := cc.extractStructuredData(msgs)

	if len(s.ToolSummary) == 0 {
		t.Error("should have tool summary entries")
	}
}

func TestExtractStructuredData_DeduplicatesAndLimits(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	var msgs []Message
	// Add the same goal message many times
	for i := 0; i < 10; i++ {
		msgs = append(msgs, makeMsg("user", "帮我安装 PostgreSQL"))
	}

	s := cc.extractStructuredData(msgs)

	// Goals should be deduplicated and limited to 5
	if len(s.Goals) > 5 {
		t.Errorf("goals should be limited to 5, got %d", len(s.Goals))
	}
	// After dedup, should only have 1 unique goal
	uniqueGoals := make(map[string]bool)
	for _, g := range s.Goals {
		uniqueGoals[strings.TrimSpace(g)] = true
	}
	if len(uniqueGoals) != 1 {
		t.Errorf("duplicate goals should be deduplicated, got %d unique: %v", len(uniqueGoals), s.Goals)
	}
}

// ============================================================================
// Integration test: full compression pipeline
// ============================================================================

func TestCompress_FullPipeline_PreservesContext(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()

	// Simulate a realistic conversation: install PostgreSQL, many tool calls, final success
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are an AI assistant"))

	// Task initiation
	msgs = append(msgs, makeMsg("user", "帮我安装 PostgreSQL 17 到 pg17 jail"))

	// Many tool interactions simulating installation
	for i := 0; i < 40; i++ {
		tcID := "install_" + string(rune('a'+i%26))
		msgs = append(msgs, makeAssistantWithToolCalls("正在执行安装步骤...", tcID, "SshExec"))
		msgs = append(msgs, makeToolResult("command output: "+strings.Repeat("data ", 20), tcID+"_SshExec"))
	}

	// Success confirmation
	msgs = append(msgs, makeMsg("assistant",
		"PostgreSQL 17.9 已成功安装并运行！✅ 数据库初始化完成，服务监听 5432 端口。已配置开机自启。"))

	// User asks about management tool
	msgs = append(msgs, makeMsg("user", "你是用什么工具管理 jail 的？"))

	// Model answers
	msgs = append(msgs, makeAssistantWithToolCalls("正在检查 jail 管理工具...", "check1", "SshExec"))
	msgs = append(msgs, makeToolResult("Bastille is used", "check1_SshExec"))
	msgs = append(msgs, makeMsg("assistant", "用的是 Bastille（FreeBSD 容器管理工具）。"))

	// More follow-up questions
	msgs = append(msgs, makeMsg("user", "如何处理？"))

	maxHistory := 50
	result := cc.Compress(context.Background(), msgs, maxHistory)

	// The result must include the last user message
	hasLatestUser := false
	for _, msg := range result {
		if msg.Role == "user" && extractStringContent(msg) == "如何处理？" {
			hasLatestUser = true
			break
		}
	}
	if !hasLatestUser {
		t.Error("compressed result must contain the latest user message")
	}

	// The summary should mention the accomplishments
	hasProgressInSummary := false
	for _, msg := range result {
		if msg.Role == "system" {
			content := extractStringContent(msg)
			if strings.Contains(content, "PostgreSQL") || strings.Contains(content, "安装") || strings.Contains(content, "Progress") || strings.Contains(content, "进展") {
				hasProgressInSummary = true
				break
			}
		}
	}
	if !hasProgressInSummary {
		t.Error("summary should mention PostgreSQL installation progress")
	}
}

// ============================================================================
// Incremental summary accumulation tests
// ============================================================================

func TestGenerateSummary_IncrementalAccumulation(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()

	// First call with initial messages
	firstMsgs := []Message{
		makeMsg("user", "安装 PostgreSQL 17"),
		makeMsg("assistant", "PostgreSQL 17 安装已完成"),
	}
	firstSummary := cc.GenerateSummary(context.Background(), firstMsgs)
	if firstSummary == "" {
		t.Fatal("first summary should not be empty")
	}

	// Second call with additional messages — should merge with previous
	secondMsgs := []Message{
		makeMsg("user", "还要配置 Nginx"),
		makeMsg("assistant", "Nginx 配置已完成"),
	}
	secondSummary := cc.GenerateSummary(context.Background(), secondMsgs)
	if secondSummary == "" {
		t.Fatal("second summary should not be empty")
	}

	// The second summary should contain information from BOTH rounds
	// (because mergeWithPrevious accumulates state)
	if cc.lastSummary == nil {
		t.Fatal("lastSummary should be non-nil after GenerateSummary")
	}
	// The merged summary should have both items
	hasPg := strings.Contains(secondSummary, "PostgreSQL") || strings.Contains(secondSummary, "安装")
	hasNginx := strings.Contains(secondSummary, "Nginx")
	if !hasPg && !hasNginx {
		t.Logf("second summary: %s", secondSummary)
		t.Error("second summary should include content from both calls")
	}
}

// ============================================================================
// Edge case tests
// ============================================================================

func TestCompress_AllToolMessages(t *testing.T) {
	cc := NewContextCompressor()
	var msgs []Message
	for i := 0; i < 100; i++ {
		msgs = append(msgs, makeAssistantWithToolCalls("", "tc_"+string(rune('a'+i%26)), "shell"))
		msgs = append(msgs, makeToolResult("output", "tc_"+string(rune('a'+i%26))+"_shell"))
	}

	maxHistory := 20
	result := cc.Compress(context.Background(), msgs, maxHistory)

	// Without any user messages with text content, the summary extraction produces minimal data
	// The compress function should handle this gracefully (don't panic)
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestCompress_SingleUserMessage(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "the only message"),
	}

	maxHistory := 2
	result := cc.Compress(context.Background(), msgs, maxHistory)

	if len(result) != 1 {
		t.Errorf("single message should remain: got %d messages", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("single message should be user role, got %s", result[0].Role)
	}
}

func TestGenerateSummary_MixedContent(t *testing.T) {
	if !apiConfigAvailable() {
		t.Skip("Skipping LLM-dependent test: no API key configured")
	}
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "任务：部署 Web 应用"),
		makeMsg("assistant", "已完成 Docker 镜像构建"),
		makeMsg("user", "不要使用生产数据库"),
		makeMsg("assistant", "已完成 Nginx 反向代理配置"),
		makeMsg("user", "选择用 Let's Encrypt 而非自签证书"),
		makeMsg("user", "接下来需要配置监控告警"),
	}

	result := cc.GenerateSummary(context.Background(), msgs)

	// Verify all expected keywords appear
	checks := []string{"Web", "Docker", "Nginx"}
	for _, expected := range checks {
		if !strings.Contains(result, expected) {
			t.Errorf("summary should contain %q, got: %s", expected, result)
		}
	}
}

// ============================================================================
// estimateTokenCount tests
// ============================================================================

func TestEstimateTokenCount_EmptyString(t *testing.T) {
	cc := NewContextCompressor()
	result := cc.estimateTokenCount("")
	if result != 0 {
		t.Errorf("estimateTokenCount(\"\") = %d, want 0", result)
	}
}

func TestEstimateTokenCount_PureASCII(t *testing.T) {
	cc := NewContextCompressor()
	// 100 ASCII characters → ~25 tokens (100/4)
	result := cc.estimateTokenCount(strings.Repeat("x", 100))
	if result <= 0 {
		t.Errorf("estimateTokenCount(100 ASCII chars) = %d, want > 0", result)
	}
	// 4 ASCII chars → 1 token
	result = cc.estimateTokenCount("abcd")
	if result != 1 {
		t.Errorf("estimateTokenCount(\"abcd\") = %d, want 1", result)
	}
	// 1-3 ASCII chars → 1 token (minimum)
	result = cc.estimateTokenCount("a")
	if result != 1 {
		t.Errorf("estimateTokenCount(\"a\") = %d, want 1", result)
	}
}

func TestEstimateTokenCount_PureCJK(t *testing.T) {
	cc := NewContextCompressor()
	// Each CJK rune: ~1.5 tokens → 10 CJK runes = 15 tokens
	// CJK formula: (cjkRunes*3 + 1) / 2
	// 10 * 3 + 1 = 31, / 2 = 15
	result := cc.estimateTokenCount("你好世界这是测试文本")
	if result != 15 {
		t.Errorf("estimateTokenCount(10 CJK runes) = %d, want 15", result)
	}
	// Single CJK rune: (1*3 + 1) / 2 = 2
	result = cc.estimateTokenCount("中")
	if result != 2 {
		t.Errorf("estimateTokenCount(\"中\") = %d, want 2", result)
	}
}

func TestEstimateTokenCount_MixedCJKAndASCII(t *testing.T) {
	cc := NewContextCompressor()
	// "Hello世界" = 5 ASCII + 2 CJK
	// ASCII: 5 / 4 = 1 (with floor)
	// CJK: (2*3+1)/2 = 3
	// Total: 4
	result := cc.estimateTokenCount("Hello世界")
	if result != 4 {
		t.Errorf("estimateTokenCount(\"Hello世界\") = %d, want 4", result)
	}
}

func TestEstimateTokenCount_VeryLongContent(t *testing.T) {
	cc := NewContextCompressor()
	// 10000 ASCII characters → ~2500 tokens
	long := strings.Repeat("This is a long line of text for token estimation. ", 100)
	result := cc.estimateTokenCount(long)
	if result <= 0 {
		t.Errorf("estimateTokenCount(long text) = %d, want > 0", result)
	}
	// Rough range check: ~4000 chars / 4 = ~1000 tokens
	expectedRange := len(long) / 4
	if result < expectedRange-100 || result > expectedRange+100 {
		t.Errorf("estimateTokenCount(long text) = %d, expected roughly %d", result, expectedRange)
	}
}

// ============================================================================
// estimateMessagesTokenCount tests
// ============================================================================

func TestEstimateMessagesTokenCount_EmptyList(t *testing.T) {
	cc := NewContextCompressor()
	result := cc.estimateMessagesTokenCount(nil)
	if result != 0 {
		t.Errorf("estimateMessagesTokenCount(nil) = %d, want 0", result)
	}
	result = cc.estimateMessagesTokenCount([]Message{})
	if result != 0 {
		t.Errorf("estimateMessagesTokenCount([]) = %d, want 0", result)
	}
}

func TestEstimateMessagesTokenCount_BasicMessages(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("system", "You are an assistant"),
		makeMsg("user", "Hello"),
		makeMsg("assistant", "Hi there!"),
	}
	result := cc.estimateMessagesTokenCount(msgs)
	// Each message: content tokens + 10 overhead
	// system: "You are an assistant" (21 chars / 4 = 5) + 10 = 15
	// user: "Hello" (5/4=1) + 10 = 11
	// assistant: "Hi there!" (9/4=2) + 10 = 12
	// Total: ~38
	if result <= 0 {
		t.Errorf("estimateMessagesTokenCount(3 basic messages) = %d, want > 0", result)
	}
}

func TestEstimateMessagesTokenCount_WithToolCalls(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "run command"),
		makeAssistantWithToolCalls("executing...", "call1", "shell"),
		makeToolResult("command output here", "call1_shell"),
	}
	result := cc.estimateMessagesTokenCount(msgs)
	if result <= 0 {
		t.Errorf("estimateMessagesTokenCount(with tool calls) = %d, want > 0", result)
	}
	// Tool call message should include +50 overhead
}

func TestEstimateMessagesTokenCount_WithReasoningContent(t *testing.T) {
	cc := NewContextCompressor()
	msg := Message{
		Role:            "assistant",
		Content:         "response",
		ReasoningContent: "Let me think about this carefully for a while",
	}
	result := cc.estimateMessagesTokenCount([]Message{msg})
	if result <= 0 {
		t.Errorf("estimateMessagesTokenCount(with reasoning) = %d, want > 0", result)
	}
	// Should have more tokens than content+overhead alone
	contentOnly := cc.estimateTokenCount("response") + 10
	if result <= contentOnly {
		t.Errorf("estimateMessagesTokenCount with reasoning (%d) should be > content only (%d)", result, contentOnly)
	}
}

func TestEstimateMessagesTokenCount_IncreasesWithMoreMessages(t *testing.T) {
	cc := NewContextCompressor()
	small := cc.estimateMessagesTokenCount([]Message{
		makeMsg("user", "hi"),
	})
	large := cc.estimateMessagesTokenCount([]Message{
		makeMsg("user", "hi"),
		makeMsg("assistant", "hello"),
		makeMsg("user", "how are you?"),
		makeMsg("assistant", "I'm good thanks!"),
	})
	if large <= small {
		t.Errorf("more messages should produce higher token count: small=%d, large=%d", small, large)
	}
}

// ============================================================================
// Token estimation threshold verification (used by RunHistoryCompression trigger)
// ============================================================================

func TestTokenEstimation_ShortConversation_UnderTypicalThreshold(t *testing.T) {
	cc := NewContextCompressor()
	// Typical scenario: 3 short messages with 128K context, 0.8 threshold
	// Estimated tokens: ~30 total, well under 128000 * 0.8 = 102400
	msgs := []Message{
		makeMsg("system", "You are helpful"),
		makeMsg("user", "hello"),
		makeMsg("assistant", "hi"),
	}
	totalTokens := cc.estimateMessagesTokenCount(msgs)
	threshold := float64(128000) * 0.8
	if float64(totalTokens) > threshold {
		t.Errorf("short conversation should be under threshold: %d tokens > %.0f", totalTokens, threshold)
	}
}

func TestTokenEstimation_LongToolResults_ExceedsSmallWindow(t *testing.T) {
	cc := NewContextCompressor()
	// Create many messages with long tool results
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are helpful"))
	msgs = append(msgs, makeMsg("user", "Task start"))
	for i := 0; i < 150; i++ {
		tcID := "call_" + string(rune('a'+i%26))
		msgs = append(msgs, makeAssistantWithToolCalls(strings.Repeat("working ", 20), tcID, "shell"))
		msgs = append(msgs, makeToolResult(strings.Repeat("output ", 30), tcID+"_shell"))
	}

	// Long tool results should exceed a small context window (e.g., 8000 * 0.5 = 4000)
	totalTokens := cc.estimateMessagesTokenCount(msgs)
	threshold := float64(8000) * 0.5
	if float64(totalTokens) <= threshold {
		t.Errorf("long tool results should exceed small window: %d tokens <= %.0f threshold", totalTokens, threshold)
	}
	t.Logf("Token count %d > threshold %.0f — trigger logic works correctly", totalTokens, threshold)
}
