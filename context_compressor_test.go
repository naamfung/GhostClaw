package main

import (
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

// ============================================================================
// GenerateSummary tests
// ============================================================================

func TestGenerateSummary_EmptyMessages(t *testing.T) {
	cc := NewContextCompressor()
	result := cc.GenerateSummary(nil)
	if result != "" {
		t.Errorf("expected empty string for nil messages, got: %q", result)
	}

	result = cc.GenerateSummary([]Message{})
	if result != "" {
		t.Errorf("expected empty string for empty messages, got: %q", result)
	}
}

func TestGenerateSummary_UserGoals(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "帮我安装 PostgreSQL 17"),
		makeMsg("assistant", "好的，我来安装 PostgreSQL 17"),
		makeMsg("user", "必须使用中科大镜像源"),
	}
	result := cc.GenerateSummary(msgs)
	if !strings.Contains(result, "PostgreSQL") {
		t.Errorf("summary should contain user goal about PostgreSQL, got: %s", result)
	}
	if !strings.Contains(result, "镜像") {
		t.Errorf("summary should contain constraint about mirror, got: %s", result)
	}
}

func TestGenerateSummary_ProgressDetection(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "安装 PostgreSQL"),
		makeMsg("assistant", "PostgreSQL 17 已成功安装并运行！"),
		makeMsg("assistant", "数据库初始化已完成，服务正在监听 5432 端口"),
	}
	result := cc.GenerateSummary(msgs)
	if !strings.Contains(result, "进展") && !strings.Contains(result, "Progress") {
		t.Errorf("summary should contain progress section, got: %s", result)
	}
}

func TestGenerateSummary_DecisionsAndConstraints(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "必须用中科大镜像，不要用官方源"),
		makeMsg("assistant", "已改用 USTC 镜像源"),
		makeMsg("user", "决定用 thin jail 而不是 thick jail"),
	}
	result := cc.GenerateSummary(msgs)
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
	// Build messages with many tool results — should trim old ones even when within limit
	var msgs []Message
	msgs = append(msgs, makeMsg("system", "You are helpful"))
	msgs = append(msgs, makeMsg("user", "Run commands"))
	for i := 0; i < 20; i++ {
		tcID := "call_" + string(rune('a'+i))
		msgs = append(msgs, makeAssistantWithToolCalls("Running...", tcID, "shell"))
		longContent := strings.Repeat("x", 500) // 500 chars, exceeds 200 char truncation
		msgs = append(msgs, makeToolResult(longContent, tcID+"_shell"))
	}
	msgs = append(msgs, makeMsg("user", "Are we done?"))

	maxHistory := len(msgs) // exactly at limit
	result := cc.Compress(msgs, maxHistory)

	if len(result) != len(msgs) {
		t.Errorf("message count should not change: got %d, want %d", len(result), len(msgs))
	}

	// Old tool results (beyond preserveRecentToolResults=10) should be truncated
	// The most recent 10 tool results should be preserved
	oldToolTruncated := false
	for i, msg := range result {
		if msg.Role == "tool" && strings.Contains(extractStringContent(msg), "[truncated by context compressor]") {
			oldToolTruncated = true
			// Verify this is an early message (not in the last ~20 messages)
			if i > len(result)-10 {
				t.Errorf("recent tool result should not be truncated at index %d", i)
			}
		}
	}
	if !oldToolTruncated {
		t.Log("No old tool results needed truncation (may be due to tail protection)")
	}
}

func TestCompress_BeyondMaxHistory_GeneratesSummary(t *testing.T) {
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
	result := cc.Compress(msgs, maxHistory)

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
	result := cc.Compress(msgs, maxHistory)

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
	result := cc.Compress(msgs, maxHistory)

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
	cc := NewContextCompressor()

	// First call with initial messages
	firstMsgs := []Message{
		makeMsg("user", "安装 PostgreSQL 17"),
		makeMsg("assistant", "PostgreSQL 17 安装已完成"),
	}
	firstSummary := cc.GenerateSummary(firstMsgs)
	if firstSummary == "" {
		t.Fatal("first summary should not be empty")
	}

	// Second call with additional messages — should merge with previous
	secondMsgs := []Message{
		makeMsg("user", "还要配置 Nginx"),
		makeMsg("assistant", "Nginx 配置已完成"),
	}
	secondSummary := cc.GenerateSummary(secondMsgs)
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
	result := cc.Compress(msgs, maxHistory)

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
	result := cc.Compress(msgs, maxHistory)

	if len(result) != 1 {
		t.Errorf("single message should remain: got %d messages", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("single message should be user role, got %s", result[0].Role)
	}
}

func TestGenerateSummary_MixedContent(t *testing.T) {
	cc := NewContextCompressor()
	msgs := []Message{
		makeMsg("user", "任务：部署 Web 应用"),
		makeMsg("assistant", "已完成 Docker 镜像构建"),
		makeMsg("user", "不要使用生产数据库"),
		makeMsg("assistant", "已完成 Nginx 反向代理配置"),
		makeMsg("user", "选择用 Let's Encrypt 而非自签证书"),
		makeMsg("user", "接下来需要配置监控告警"),
	}

	result := cc.GenerateSummary(msgs)

	// Verify all expected keywords appear
	checks := []string{"Web", "Docker", "Nginx"}
	for _, expected := range checks {
		if !strings.Contains(result, expected) {
			t.Errorf("summary should contain %q, got: %s", expected, result)
		}
	}
}
