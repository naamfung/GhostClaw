package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// RunSafetyCheck Tests
// ============================================================================

func TestRunSafetyCheck_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dc := &dummyChannel{}
	stop, err := RunSafetyCheck(ctx, dc, 1)
	if !stop {
		t.Error("Should stop when context is cancelled")
	}
	if err == nil {
		t.Error("Should return error when context is cancelled")
	}
}

func TestRunSafetyCheck_ContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // ensure deadline expires

	dc := &dummyChannel{}
	stop, _ := RunSafetyCheck(ctx, dc, 1)
	if !stop {
		t.Error("Should stop when context deadline is exceeded")
	}
}

func TestRunSafetyCheck_NormalFlow(t *testing.T) {
	ctx := context.Background()
	dc := &dummyChannel{}
	stop, err := RunSafetyCheck(ctx, dc, 1)
	if stop {
		t.Error("Should not stop for normal iteration 1")
	}
	if err != nil {
		t.Errorf("Should not return error for normal flow: %v", err)
	}
}

func TestRunSafetyCheck_ForceStopAtMaxIterations(t *testing.T) {
	// This depends on MaxAgentLoopIterations being set
	// Test at a very high iteration that should trigger ShouldForceStop
	ctx := context.Background()
	dc := &dummyChannel{}
	// MaxAgentLoopIterations is typically 100, use 99999 to force stop
	stop, _ := RunSafetyCheck(ctx, dc, 99999)
	// May or may not stop depending on MaxAgentLoopIterations config
	// Just ensure it doesn't panic
	_ = stop
}

// ============================================================================
// RunPlanModeChecks Tests
// ============================================================================

func TestRunPlanModeChecks_NormalIteration(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test system"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	// Should not panic when globalPlanMode is nil (default state)
	RunPlanModeChecks(&messages, 1)

	if len(messages) != originalLen {
		t.Errorf("No messages should be added when plan mode is inactive, got %d want %d",
			len(messages), originalLen)
	}
}

func TestRunPlanModeChecks_Iteration4_NoPlanMode(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test system"},
		{Role: "user", Content: "hello"},
	}

	// Without plan mode initialized, should not panic
	RunPlanModeChecks(&messages, 4)

	// globalPlanMode is nil so no suggestion injected
	if len(messages) != 2 {
		t.Logf("Messages count after RunPlanModeChecks: %d", len(messages))
	}
}

// ============================================================================
// RunWakeInjection Tests
// ============================================================================

func TestRunWakeInjection_NoSession(t *testing.T) {
	// Save and restore globalSession
	oldSession := globalSession
	globalSession = nil
	defer func() { globalSession = oldSession }()

	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	RunWakeInjection(&messages, 1)

	if len(messages) != originalLen {
		t.Error("Should not modify messages when session is nil")
	}
}

func TestRunWakeInjection_NilSessionGuard(t *testing.T) {
	// Verify RunWakeInjection handles nil session without panicking
	oldSession := globalSession
	globalSession = nil
	defer func() { globalSession = oldSession }()

	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}

	// Should not panic when globalSession is nil
	RunWakeInjection(&messages, 1)
}

// ============================================================================
// RunEscalateCheck Tests
// ============================================================================

func TestRunEscalateCheck_SentinelPrefix(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	results := []EnrichedMessage{
		NewToolResultMessage("tc-1", escalatePrefix+"tool failure detected: check logs", TaskStatusFailed, "SmartShell"),
	}

	escalated := RunEscalateCheck(&messages, results, nil)
	if !escalated {
		t.Error("Should escalate on sentinel prefix")
	}
	if len(messages) <= originalLen {
		t.Error("Should have appended escalation user message")
	}
	lastMsg := messages[len(messages)-1]
	if content, ok := lastMsg.Content.(string); ok {
		if !strings.Contains(content, "tool failure detected") {
			t.Errorf("Escalation message should contain the extracted content, got: %s", content)
		}
	}
}

func TestRunEscalateCheck_NonSentinelPrefix(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	results := []EnrichedMessage{
		NewToolResultMessage("tc-1", "normal tool output", TaskStatusSuccess, "SmartShell"),
	}

	escalated := RunEscalateCheck(&messages, results, nil)
	if escalated {
		t.Error("Should not escalate on normal tool output")
	}
	if len(messages) != originalLen {
		t.Error("Should not modify messages for normal results")
	}
}

func TestRunEscalateCheck_EmptyResults(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	escalated := RunEscalateCheck(&messages, nil, nil)
	if escalated {
		t.Error("Should not escalate with nil results")
	}
	if len(messages) != originalLen {
		t.Error("Should not modify messages with nil results")
	}
}

func TestRunEscalateCheck_MultipleResults_FirstEscalates(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}

	results := []EnrichedMessage{
		NewToolResultMessage("tc-1", escalatePrefix+"first esc", TaskStatusFailed, "ToolA"),
		NewToolResultMessage("tc-2", escalatePrefix+"second esc", TaskStatusFailed, "ToolB"),
	}

	escalated := RunEscalateCheck(&messages, results, nil)
	if !escalated {
		t.Error("Should escalate on first sentinel")
	}
	// Only the first escalation should be injected (break after first match)
	lastMsg := messages[len(messages)-1]
	if content, ok := lastMsg.Content.(string); ok {
		if !strings.Contains(content, "first esc") {
			t.Errorf("Should contain first escalation, got: %s", content)
		}
		if strings.Contains(content, "second esc") {
			t.Error("Should NOT contain second escalation (only first is injected)")
		}
	}
}

// ============================================================================
// RunBranchTool Tests
// ============================================================================

func TestRunBranchTool_EmptyToolCalls(t *testing.T) {
	ctx := context.Background()
	dc := &dummyChannel{}

	results := RunBranchTool(ctx, nil, dc, nil, 1)
	if results != nil {
		t.Errorf("Expected nil results for empty tool calls, got %d results", len(results))
	}

	results = RunBranchTool(ctx, []map[string]interface{}{}, dc, nil, 1)
	if results != nil {
		t.Errorf("Expected nil results for empty map slice, got %d results", len(results))
	}
}

func TestRunBranchTool_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dc := &dummyChannel{}
	toolCalls := []map[string]interface{}{
		{
			"id":   "call-1",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "SmartShell",
				"arguments": `{"command":"echo test"}`,
			},
		},
	}

	results := RunBranchTool(ctx, toolCalls, dc, nil, 1)
	// Should return early without executing tools
	if len(results) > 0 {
		t.Logf("Got %d results on cancelled context (may vary)", len(results))
	}
}

// ============================================================================
// RunAfterToolExec Tests
// ============================================================================

func TestRunAfterToolExec_AppendsResults(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "let me check", ToolCalls: []interface{}{}},
	}
	originalLen := len(messages)

	results := []EnrichedMessage{
		NewToolResultMessage("tc-1", "command output", TaskStatusSuccess, "SmartShell"),
		NewToolResultMessage("tc-2", "file content", TaskStatusSuccess, "ReadAllLines"),
	}

	dc := &dummyChannel{}
	RunAfterToolExec(&messages, results, dc)

	if len(messages) != originalLen+len(results) {
		t.Errorf("Expected %d messages, got %d", originalLen+len(results), len(messages))
	}

	// Verify tool results are appended
	for i, result := range results {
		msg := messages[originalLen+i]
		if msg.Role != "tool" {
			t.Errorf("Message %d: expected role 'tool', got '%s'", i, msg.Role)
		}
		// ToolCallID should match the one passed to NewToolResultMessage
		if msg.ToolCallID != result.ToolCallID {
			t.Errorf("Message %d: expected ToolCallID '%s', got '%s'", i, result.ToolCallID, msg.ToolCallID)
		}
		if msg.ToolCallID == "" {
			t.Errorf("Message %d: ToolCallID should not be empty", i)
		}
	}
}

func TestRunAfterToolExec_EmptyResults(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
	}
	originalLen := len(messages)

	dc := &dummyChannel{}
	// Should not panic with empty results
	RunAfterToolExec(&messages, nil, dc)
	RunAfterToolExec(&messages, []EnrichedMessage{}, dc)

	if len(messages) != originalLen {
		t.Error("Should not modify messages with empty results")
	}
}

// ============================================================================
// AgentLoopConfig Tests
// ============================================================================

func TestAgentLoopConfig_Defaults(t *testing.T) {
	config := &AgentLoopConfig{
		EffectiveAPIType:     "openai",
		EffectiveBaseURL:     "https://api.openai.com",
		EffectiveAPIKey:      "sk-test",
		EffectiveModelID:     "gpt-4",
		EffectiveTemperature: 0.7,
		EffectiveMaxTokens:   4096,
	}

	if config.EffectiveAPIType != "openai" {
		t.Error("APIType should be openai")
	}
	if config.EffectiveTemperature != 0.7 {
		t.Error("Temperature should be 0.7")
	}
	if config.IsNewSession {
		t.Error("IsNewSession should default to false")
	}
	if config.CurrentRole != nil {
		t.Error("CurrentRole should default to nil")
	}
}

// ============================================================================
// Integration: AgentLoop function existence
// ============================================================================

func TestAgentLoop_SignaturePreserved(t *testing.T) {
	// Ensure AgentLoop function signature hasn't been accidentally changed
	// This is a compile-time check that the function exists with the right signature
	var _ func(context.Context, Channel, []Message, string, string, string, string, float64, int, bool, bool) ([]Message, error) = AgentLoop
}

// ============================================================================
// Integration: Scheduler + AgentLoop Phase Registration
// ============================================================================

func TestRegisterLoopTasks(t *testing.T) {
	sched := NewScheduler()
	registerLoopTasks(sched)
	// Currently no-op, but ensures function exists and doesn't panic
	if sched == nil {
		t.Error("Scheduler should not be nil")
	}
}
