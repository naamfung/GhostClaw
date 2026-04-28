package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// TaskTracker — 初始化 / IsWorkMode
// ============================================================================

func TestTaskTracker_NewIsNotWorkMode(t *testing.T) {
	tt := NewTaskTracker()

	if tt.IsWorkMode() {
		t.Error("new tracker should not be in work mode")
	}
}

func TestTaskTracker_StartNewTask_Chat(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("hello", IntentChat)

	if tt.IsWorkMode() {
		t.Error("IntentChat should not be work mode")
	}
}

func TestTaskTracker_StartNewTask_Task(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("fix the bug", IntentTask)

	if !tt.IsWorkMode() {
		t.Error("IntentTask should be work mode")
	}
}

func TestTaskTracker_StartNewTask_InitializesFields(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("build feature X", IntentTask)

	if tt.GetComplexity() != ComplexityModerate {
		t.Error("should default to ComplexityModerate")
	}
	if tt.GetConsecutiveFails() != 0 {
		t.Error("consecutive fails should start at 0")
	}

	report := tt.GetProgressReport()
	if !strings.Contains(report, "build feature X") {
		t.Errorf("progress report should contain task: %s", report)
	}
}

// ============================================================================
// TaskTracker — RecordToolCall / detectStuckState
// ============================================================================

func TestTaskTracker_RecordSuccess(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read file.go", "content...")

	report := tt.GetProgressReport()
	if !strings.Contains(report, "1 completed") {
		t.Errorf("should have 1 completed: %s", report)
	}
	if tt.GetConsecutiveFails() != 0 {
		t.Error("consecutive fails should be 0 after success")
	}
}

func TestTaskTracker_RecordFailure(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("smart_shell", TaskStatusFailed, "run command", "error")
	tt.RecordToolCall("smart_shell", TaskStatusFailed, "run command", "error")
	tt.RecordToolCall("smart_shell", TaskStatusFailed, "run command", "error")

	if tt.GetConsecutiveFails() != 3 {
		t.Errorf("expected 3 consecutive fails, got %d", tt.GetConsecutiveFails())
	}
}

func TestTaskTracker_StuckAfterConsecutiveFails(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// 3 consecutive failures → stuck
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")

	stuck, reason := tt.ShouldPromptTodo()
	if !stuck {
		t.Error("should be stuck after 3 consecutive fails")
	}
	if !strings.Contains(reason, "Consecutive failures") {
		t.Errorf("stuck reason should mention consecutive failures: %s", reason)
	}
}

func TestTaskTracker_StuckRecoversOnSuccess(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")

	// Verify stuck
	stuck, _ := tt.ShouldPromptTodo()
	if !stuck {
		t.Fatal("should be stuck first")
	}

	// Success resets
	tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")

	if tt.GetConsecutiveFails() != 0 {
		t.Error("consecutive fails should reset after success")
	}
}

func TestTaskTracker_MultipleStuckReasons(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// Trigger condition 1: consecutive failures
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")

	// Manually set condition 4: too many steps
	tt.mu.Lock()
	tt.currentTask.CompletedSteps = 30
	tt.currentTask.FailedSteps = 5
	tt.mu.Unlock()

	// 再觸發一次 RecordToolCall 以重新運行 detectStuckState
	tt.RecordToolCall("other_tool", TaskStatusFailed, "cmd", "err")

	stuck, reason := tt.ShouldPromptTodo()
	if !stuck {
		t.Error("should be stuck with multiple reasons")
	}
	// 審計修復：composable stuck detection — reason 應包含兩個條件
	if !strings.Contains(reason, "Consecutive failures") {
		t.Errorf("reason should contain consecutive failures: %s", reason)
	}
	if !strings.Contains(reason, "Too many steps") {
		t.Errorf("reason should contain too many steps: %s", reason)
	}
}

func TestTaskTracker_StuckHighFailureRateOverTime(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// Set LastSuccessTime 3 minutes ago with high failure ratio
	tt.mu.Lock()
	tt.currentTask.LastSuccessTime = time.Now().Add(-3 * time.Minute)
	tt.currentTask.CompletedSteps = 3
	tt.currentTask.FailedSteps = 8
	tt.currentTask.CancelledSteps = 0
	tt.mu.Unlock()

	// Trigger detectStuckState via RecordToolCall
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")

	stuck, reason := tt.ShouldPromptTodo()
	if !stuck {
		t.Error("should be stuck with high failure rate over time")
	}
	if !strings.Contains(reason, "High failure rate") {
		t.Errorf("reason should mention high failure rate: %s", reason)
	}
}

// ============================================================================
// TaskTracker — ShouldPromptTodo (進度檢查)
// ============================================================================

func TestTaskTracker_PromptAt8Steps(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// Record 7 successful calls (won't prompt)
	for i := 0; i < 7; i++ {
		tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
	}
	_, reason := tt.ShouldPromptTodo()
	if reason != "" {
		t.Errorf("should not prompt at step 7: %s", reason)
	}

	// 8th call should prompt
	tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
	prompt, _ := tt.ShouldPromptTodo()
	if !prompt {
		t.Error("should prompt at step 8")
	}
}

func TestTaskTracker_NoPromptForSimpleTask(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("hello", IntentChat)

	// Simulate complexity = simple
	tt.mu.Lock()
	tt.currentTask.Complexity = ComplexitySimple
	tt.mu.Unlock()

	for i := 0; i < 10; i++ {
		tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
	}

	prompt, _ := tt.ShouldPromptTodo()
	if prompt {
		t.Error("should not prompt for simple tasks")
	}
}

func TestTaskTracker_NoPromptNilTask(t *testing.T) {
	tt := NewTaskTracker()

	prompt, reason := tt.ShouldPromptTodo()
	if prompt || reason != "" {
		t.Errorf("nil task should not prompt: prompt=%v reason=%s", prompt, reason)
	}
}

// ============================================================================
// TaskTracker — MarkCompleted
// ============================================================================

func TestTaskTracker_MarkCompleted(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.MarkCompleted()

	tt.mu.RLock()
	completed := tt.currentTask.IsCompleted
	tt.mu.RUnlock()

	if !completed {
		t.Error("task should be marked completed")
	}
}

func TestTaskTracker_MarkCompleted_NilTask(t *testing.T) {
	tt := NewTaskTracker()

	// Should not panic
	tt.MarkCompleted()
}

// ============================================================================
// TaskTracker — GetProgressReport / GetStatusSummary
// ============================================================================

func TestTaskTracker_GetProgressReport_NoTask(t *testing.T) {
	tt := NewTaskTracker()
	report := tt.GetProgressReport()
	if report != "No active task" {
		t.Errorf("expected 'No active task', got '%s'", report)
	}
}

func TestTaskTracker_GetStatusSummary_NoTask(t *testing.T) {
	tt := NewTaskTracker()
	summary := tt.GetStatusSummary()
	if summary != "" {
		t.Errorf("expected empty summary, got '%s'", summary)
	}
}

func TestTaskTracker_GetStatusSummary_WithFailures(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("shell", TaskStatusFailed, "cmd", "err")

	summary := tt.GetStatusSummary()
	if !strings.Contains(summary, "Failed attempts") {
		t.Errorf("summary should mention failed attempts: %s", summary)
	}
}

func TestTaskTracker_GetStatusSummary_WithCancelled(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("shell", TaskStatusCancelled, "cmd", "cancelled")
	tt.RecordToolCall("shell", TaskStatusCancelled, "cmd", "cancelled")

	summary := tt.GetStatusSummary()
	if !strings.Contains(summary, "User cancelled") {
		t.Errorf("summary should mention user cancelled: %s", summary)
	}
}

// ============================================================================
// TaskTracker — IsSimpleTask / GetConsecutiveFails / GetComplexity
// ============================================================================

func TestTaskTracker_IsSimpleTask_Nil(t *testing.T) {
	tt := NewTaskTracker()
	if !tt.IsSimpleTask() {
		t.Error("nil task should be considered simple")
	}
}

func TestTaskTracker_IsSimpleTask_Moderate(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	if tt.IsSimpleTask() {
		t.Error("moderate task should not be simple")
	}
}

// ============================================================================
// TaskTracker — ToolCallRecord Retention
// ============================================================================

func TestTaskTracker_RecentToolCallsCapped(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// Record 25 calls (cap is 20)
	for i := 0; i < 25; i++ {
		tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
	}

	tt.mu.RLock()
	count := len(tt.currentTask.RecentToolCalls)
	tt.mu.RUnlock()

	if count > 20 {
		t.Errorf("RecentToolCalls should be capped at 20, got %d", count)
	}
}

// ============================================================================
// TaskTracker — Concurrent Safety
// ============================================================================

func TestTaskTracker_ConcurrentReadWrite(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("concurrent test", IntentTask)

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
			}
		}()
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tt.IsWorkMode()
				tt.GetConsecutiveFails()
				tt.GetComplexity()
				tt.IsSimpleTask()
				tt.GetProgressReport()
				tt.GetStatusSummary()
				tt.ShouldPromptTodo()
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// TaskTracker — Edge Cases
// ============================================================================

func TestTaskTracker_RecordToolCall_NilTask(t *testing.T) {
	tt := NewTaskTracker()

	// Should not panic
	tt.RecordToolCall("read_all_lines", TaskStatusSuccess, "read", "ok")
}

func TestTaskTracker_GetConsecutiveFails_Nil(t *testing.T) {
	tt := NewTaskTracker()
	if tt.GetConsecutiveFails() != 0 {
		t.Error("nil task should return 0 consecutive fails")
	}
}

func TestTaskTracker_GetComplexity_Nil(t *testing.T) {
	tt := NewTaskTracker()
	if tt.GetComplexity() != ComplexityUnknown {
		t.Error("nil task should return ComplexityUnknown")
	}
}

func TestTaskTracker_CancelledDoesNotIncrementFails(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	tt.RecordToolCall("shell", TaskStatusCancelled, "cmd", "user cancelled")
	tt.RecordToolCall("shell", TaskStatusCancelled, "cmd", "user cancelled")

	if tt.GetConsecutiveFails() != 0 {
		t.Error("cancelled should not increment consecutive fails")
	}

	report := tt.GetProgressReport()
	if !strings.Contains(report, "2 cancelled") {
		t.Errorf("should have 2 cancelled: %s", report)
	}
}

func TestTaskTracker_RepeatedFailuresResetsOnSuccess(t *testing.T) {
	tt := NewTaskTracker()
	tt.StartNewTask("test", IntentTask)

	// Fail tool A three times
	tt.RecordToolCall("tool_a", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("tool_a", TaskStatusFailed, "cmd", "err")
	tt.RecordToolCall("tool_a", TaskStatusFailed, "cmd", "err")

	// Success on tool A resets its counter
	tt.RecordToolCall("tool_a", TaskStatusSuccess, "cmd", "ok")

	tt.mu.RLock()
	count := tt.currentTask.RepeatedFailures["tool_a"]
	tt.mu.RUnlock()

	if count != 0 {
		t.Errorf("RepeatedFailures should reset on success, got %d", count)
	}
}
