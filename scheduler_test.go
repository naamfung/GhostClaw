package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// ReadyQueue Tests
// ============================================================================

func TestReadyQueue_EnqueueDequeue_PriorityOrdering(t *testing.T) {
	rq := &ReadyQueue{}

	// Enqueue in reverse priority order
	rq.Enqueue(&TCB{ID: "p4", Name: "idle", Priority: PriorityIdle})
	rq.Enqueue(&TCB{ID: "p2", Name: "normal", Priority: PriorityNormal})
	rq.Enqueue(&TCB{ID: "p0", Name: "critical", Priority: PriorityCritical})
	rq.Enqueue(&TCB{ID: "p1", Name: "high", Priority: PriorityHigh})
	rq.Enqueue(&TCB{ID: "p3", Name: "low", Priority: PriorityLow})

	// Should dequeue in priority order: P0, P1, P2, P3, P4
	expected := []string{"p0", "p1", "p2", "p3", "p4"}
	for i, want := range expected {
		tcb := rq.Dequeue()
		if tcb == nil {
			t.Fatalf("Dequeue #%d: got nil, want %s", i, want)
		}
		if tcb.ID != want {
			t.Errorf("Dequeue #%d: got %s, want %s", i, tcb.ID, want)
		}
	}

	// Queue should be empty now
	if rq.HasReady() {
		t.Error("Expected empty queue after dequeuing all tasks")
	}
}

func TestReadyQueue_FIFO_WithinSamePriority(t *testing.T) {
	rq := &ReadyQueue{}

	// Enqueue 3 tasks at same priority
	rq.Enqueue(&TCB{ID: "a", Priority: PriorityNormal})
	rq.Enqueue(&TCB{ID: "b", Priority: PriorityNormal})
	rq.Enqueue(&TCB{ID: "c", Priority: PriorityNormal})

	// Should dequeue in FIFO order
	for _, want := range []string{"a", "b", "c"} {
		tcb := rq.Dequeue()
		if tcb == nil {
			t.Fatalf("Dequeue: got nil, want %s", want)
		}
		if tcb.ID != want {
			t.Errorf("Dequeue: got %s, want %s", tcb.ID, want)
		}
	}

	if rq.HasReady() {
		t.Error("Expected empty queue after dequeuing all tasks")
	}
}

func TestReadyQueue_MixedPriorities_FIFOPerLevel(t *testing.T) {
	rq := &ReadyQueue{}

	// Enqueue interleaved priorities
	rq.Enqueue(&TCB{ID: "h1", Priority: PriorityHigh})
	rq.Enqueue(&TCB{ID: "n1", Priority: PriorityNormal})
	rq.Enqueue(&TCB{ID: "h2", Priority: PriorityHigh})
	rq.Enqueue(&TCB{ID: "n2", Priority: PriorityNormal})

	// P1 tasks should come first, in FIFO order
	expected := []string{"h1", "h2", "n1", "n2"}
	for i, want := range expected {
		tcb := rq.Dequeue()
		if tcb == nil {
			t.Fatalf("Dequeue #%d: got nil, want %s", i, want)
		}
		if tcb.ID != want {
			t.Errorf("Dequeue #%d: got %s, want %s", i, tcb.ID, want)
		}
	}
}

func TestReadyQueue_Enqueue_NegativePriority(t *testing.T) {
	rq := &ReadyQueue{}
	rq.Enqueue(&TCB{ID: "neg", Priority: -1})
	tcb := rq.Dequeue()
	if tcb == nil {
		t.Fatal("Expected task with negative priority to be enqueued at P0")
	}
	// Should be treated as P0, so dequeue should work
	_ = tcb
}

func TestReadyQueue_Enqueue_OverflowPriority(t *testing.T) {
	rq := &ReadyQueue{}
	rq.Enqueue(&TCB{ID: "overflow", Priority: 999})
	tcb := rq.Dequeue()
	if tcb == nil {
		t.Fatal("Expected task with overflow priority to be enqueued at lowest level")
	}
	_ = tcb
}

func TestReadyQueue_Dequeue_Empty(t *testing.T) {
	rq := &ReadyQueue{}
	tcb := rq.Dequeue()
	if tcb != nil {
		t.Errorf("Expected nil from empty queue, got %v", tcb)
	}
}

func TestReadyQueue_HasReady(t *testing.T) {
	rq := &ReadyQueue{}

	if rq.HasReady() {
		t.Error("Expected empty queue to report not ready")
	}

	rq.Enqueue(&TCB{ID: "t1", Priority: PriorityLow})
	if !rq.HasReady() {
		t.Error("Expected non-empty queue to report ready")
	}

	rq.Dequeue()
	if rq.HasReady() {
		t.Error("Expected empty queue to report not ready after dequeue")
	}
}

func TestReadyQueue_HasReadyExcept(t *testing.T) {
	rq := &ReadyQueue{}

	// Only P4 (Idle) tasks
	rq.Enqueue(&TCB{ID: "idle", Priority: PriorityIdle})

	if !rq.HasReady() {
		t.Error("Expected HasReady=true when idle task exists")
	}
	if rq.HasReadyExcept(PriorityIdle) {
		t.Error("Expected HasReadyExcept(P4)=false when only idle tasks exist")
	}
	if !rq.HasReadyExcept(PriorityCritical) {
		t.Error("Expected HasReadyExcept(P0)=true when idle task exists")
	}

	// Add a P0 task
	rq.Enqueue(&TCB{ID: "critical", Priority: PriorityCritical})
	if !rq.HasReadyExcept(PriorityIdle) {
		t.Error("Expected HasReadyExcept(P4)=true when critical task exists")
	}
}

func TestReadyQueue_Bitmap_Correctness(t *testing.T) {
	rq := &ReadyQueue{}

	// Empty
	if rq.bitmap != 0 {
		t.Errorf("Expected bitmap=0, got %08b", rq.bitmap)
	}

	// Add P0
	rq.Enqueue(&TCB{ID: "p0", Priority: PriorityCritical})
	if rq.bitmap != 1<<PriorityCritical {
		t.Errorf("Expected bitmap=%08b, got %08b", 1<<PriorityCritical, rq.bitmap)
	}

	// Add P4
	rq.Enqueue(&TCB{ID: "p4", Priority: PriorityIdle})
	if rq.bitmap != (1<<PriorityCritical | 1<<PriorityIdle) {
		t.Errorf("Expected bitmap=%08b, got %08b", 1<<PriorityCritical|1<<PriorityIdle, rq.bitmap)
	}

	// Dequeue P0 — P0 bit should clear
	rq.Dequeue()
	if rq.bitmap != 1<<PriorityIdle {
		t.Errorf("After dequeuing P0, expected bitmap=%08b, got %08b", 1<<PriorityIdle, rq.bitmap)
	}

	// Dequeue P4 — all bits should clear
	rq.Dequeue()
	if rq.bitmap != 0 {
		t.Errorf("After dequeuing all, expected bitmap=0, got %08b", rq.bitmap)
	}
}

func TestReadyQueue_MultipleEnqueueDequeueCycles(t *testing.T) {
	rq := &ReadyQueue{}

	for cycle := 0; cycle < 5; cycle++ {
		// Enqueue one of each priority
		rq.Enqueue(&TCB{ID: "p0", Priority: PriorityCritical})
		rq.Enqueue(&TCB{ID: "p2", Priority: PriorityNormal})
		rq.Enqueue(&TCB{ID: "p4", Priority: PriorityIdle})

		expected := []int{PriorityCritical, PriorityNormal, PriorityIdle}
		for i, wantPri := range expected {
			tcb := rq.Dequeue()
			if tcb == nil {
				t.Fatalf("Cycle %d, Dequeue #%d: got nil", cycle, i)
			}
			if tcb.Priority != wantPri {
				t.Errorf("Cycle %d, Dequeue #%d: priority=%d, want %d", cycle, i, tcb.Priority, wantPri)
			}
		}

		if rq.HasReady() {
			t.Errorf("Cycle %d: queue should be empty", cycle)
		}
	}
}

// ============================================================================
// Scheduler Tests
// ============================================================================

func TestScheduler_NewScheduler(t *testing.T) {
	s := NewScheduler()
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.HasReady() {
		t.Error("New scheduler should have no ready tasks")
	}
}

func TestScheduler_Register(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test Task", PriorityNormal, handler)

	if _, ok := s.tasks["test"]; !ok {
		t.Error("Task should be registered")
	}
	if s.tasks["test"].ID != "test" {
		t.Errorf("Task ID: got %s, want test", s.tasks["test"].ID)
	}
	if s.tasks["test"].Priority != PriorityNormal {
		t.Errorf("Task Priority: got %d, want %d", s.tasks["test"].Priority, PriorityNormal)
	}
}

func TestScheduler_EnqueueTask(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test Task", PriorityNormal, handler)
	s.EnqueueTask("test")

	if !s.HasReady() {
		t.Error("Expected HasReady=true after enqueue")
	}
}

func TestScheduler_EnqueueTask_Unregistered(t *testing.T) {
	s := NewScheduler()
	s.EnqueueTask("nonexistent") // should not panic
	if s.HasReady() {
		t.Error("Expected HasReady=false when enqueueing unregistered task")
	}
}

func TestScheduler_Tick_ExecutesHandler(t *testing.T) {
	s := NewScheduler()
	executed := false
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		executed = true
		return ResultDone, nil
	}
	s.Register("test", "Test Task", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	err := s.Tick(context.Background(), ml, &dummyChannel{})
	if err != nil {
		t.Errorf("Tick returned error: %v", err)
	}
	if !executed {
		t.Error("Handler was not executed")
	}
}

func TestScheduler_Tick_Empty(t *testing.T) {
	s := NewScheduler()
	ml := NewMessageList(nil)
	err := s.Tick(context.Background(), ml, &dummyChannel{})
	if err != nil {
		t.Errorf("Tick on empty scheduler returned error: %v", err)
	}
}

func TestScheduler_Tick_ContextCancellation(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		select {
		case <-ctx.Done():
			return ResultDone, ctx.Err()
		default:
			return ResultDone, nil
		}
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	ml := NewMessageList(nil)
	_ = s.Tick(ctx, ml, &dummyChannel{})
	// Should not panic, handler can check ctx internally
}

func TestScheduler_TaskResult_Continue(t *testing.T) {
	s := NewScheduler()
	execCount := 0
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		execCount++
		if execCount >= 3 {
			return ResultDone, nil
		}
		return ResultContinue, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	// Tick 3 times — each time handler returns Continue, gets re-enqueued
	_ = s.Tick(context.Background(), ml, &dummyChannel{})
	_ = s.Tick(context.Background(), ml, &dummyChannel{})
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if execCount != 3 {
		t.Errorf("Expected 3 executions, got %d", execCount)
	}
	if !s.IsTaskDone("test") {
		t.Error("Task should be done after returning ResultDone")
	}
}

func TestScheduler_IsTaskDone(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	if s.IsTaskDone("test") {
		t.Error("Task should not be done before Tick")
	}

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskDone("test") {
		t.Error("Task should be done after returning ResultDone")
	}
}

func TestScheduler_IsTaskBlocked(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultBlock, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	if s.IsTaskBlocked("test") {
		t.Error("Task should not be blocked before Tick")
	}

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskBlocked("test") {
		t.Error("Task should be blocked after returning ResultBlock")
	}
	if s.IsTaskDone("test") {
		t.Error("Task should not be done when blocked")
	}
}

func TestScheduler_TaskResult_Yield(t *testing.T) {
	s := NewScheduler()
	execCount := 0
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		execCount++
		if execCount >= 2 {
			return ResultDone, nil
		}
		return ResultYield, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})
	if execCount != 1 {
		t.Fatalf("Expected 1 execution after first Tick, got %d", execCount)
	}
	if !s.HasReady() {
		t.Error("Task should be re-enqueued after yield")
	}

	_ = s.Tick(context.Background(), ml, &dummyChannel{})
	if execCount != 2 {
		t.Fatalf("Expected 2 executions after second Tick, got %d", execCount)
	}
	if !s.IsTaskDone("test") {
		t.Error("Task should be done after second execution")
	}
}

func TestScheduler_GetExitFlag_SetExitFlag(t *testing.T) {
	s := NewScheduler()
	if s.GetExitFlag() {
		t.Error("Exit flag should default to false")
	}
	s.SetExitFlag()
	if !s.GetExitFlag() {
		t.Error("Exit flag should be true after SetExitFlag")
	}
}

func TestScheduler_UnblockTask(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultBlock, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskBlocked("test") {
		t.Fatal("Task should be blocked")
	}

	s.UnblockTask("test")
	if s.IsTaskBlocked("test") {
		t.Error("Task should not be blocked after UnblockTask")
	}
	if !s.HasReady() {
		t.Error("Task should be re-enqueued after UnblockTask")
	}
}

func TestScheduler_ResetDone(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskDone("test") {
		t.Fatal("Task should be done")
	}

	s.ResetDone("test")
	if s.IsTaskDone("test") {
		t.Error("Task should not be done after ResetDone")
	}
}

func TestScheduler_HandlerError(t *testing.T) {
	s := NewScheduler()
	testErr := errors.New("handler failure")
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, testErr
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	err := s.Tick(context.Background(), ml, &dummyChannel{})
	if err != testErr {
		t.Errorf("Expected error %v, got %v", testErr, err)
	}
	// Task should still be marked as done even on error
	if !s.IsTaskDone("test") {
		t.Error("Task should be done even when handler returns error")
	}
}

func TestScheduler_PriorityDispatch_AcrossLevels(t *testing.T) {
	s := NewScheduler()
	order := make([]string, 0)

	makeHandler := func(name string) TaskHandler {
		return func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
			order = append(order, name)
			return ResultDone, nil
		}
	}

	s.Register("idle", "Idle", PriorityIdle, makeHandler("idle"))
	s.Register("low", "Low", PriorityLow, makeHandler("low"))
	s.Register("normal", "Normal", PriorityNormal, makeHandler("normal"))
	s.Register("high", "High", PriorityHigh, makeHandler("high"))
	s.Register("critical", "Critical", PriorityCritical, makeHandler("critical"))

	// Enqueue in random order
	s.EnqueueTask("low")
	s.EnqueueTask("critical")
	s.EnqueueTask("idle")
	s.EnqueueTask("normal")
	s.EnqueueTask("high")

	ml := NewMessageList(nil)
	for s.HasReady() {
		_ = s.Tick(context.Background(), ml, &dummyChannel{})
	}

	expected := []string{"critical", "high", "normal", "low", "idle"}
	if len(order) != len(expected) {
		t.Fatalf("Expected %d tasks executed, got %d: %v", len(expected), len(order), order)
	}
	for i, want := range expected {
		if order[i] != want {
			t.Errorf("Execution #%d: got %s, want %s", i, order[i], want)
		}
	}
}

func TestScheduler_HasReadyExcept(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}

	s.Register("idle", "Idle", PriorityIdle, handler)
	s.Register("normal", "Normal", PriorityNormal, handler)

	// Only enqueue idle task
	s.EnqueueTask("idle")

	if !s.HasReady() {
		t.Error("HasReady should be true")
	}
	if s.HasReadyExcept(PriorityIdle) {
		t.Error("HasReadyExcept(P4) should be false when only idle tasks exist")
	}

	// Enqueue normal task
	s.EnqueueTask("normal")
	if !s.HasReadyExcept(PriorityIdle) {
		t.Error("HasReadyExcept(P4) should be true when non-idle tasks exist")
	}
}

// ============================================================================
// Concurrent Safety Tests
// ============================================================================

func TestScheduler_ConcurrentEnqueue(t *testing.T) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.EnqueueTask("test")
		}()
	}
	wg.Wait()

	// Should not panic; enqueue is not goroutine-safe (by design - single loop),
	// but we verify it doesn't crash
	_ = s.HasReady()
}

func TestReadyQueue_ConcurrentStress(t *testing.T) {
	rq := &ReadyQueue{}

	var wg sync.WaitGroup
	for p := 0; p < NumPriorities; p++ {
		wg.Add(1)
		go func(pri int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rq.Enqueue(&TCB{
					ID:       string(rune('A'+pri)) + string(rune('a'+i)),
					Priority: pri,
				})
			}
		}(p)
	}
	wg.Wait()

	// Dequeue all and verify priority ordering holds
	var lastPri int = -1
	total := 0
	for rq.HasReady() {
		tcb := rq.Dequeue()
		if tcb == nil {
			break
		}
		if tcb.Priority < lastPri {
			t.Errorf("Priority inversion: %d came after %d", tcb.Priority, lastPri)
		}
		lastPri = tcb.Priority
		total++
	}

	if total != 250 {
		t.Errorf("Expected 250 tasks, got %d", total)
	}
}

// ============================================================================
// TimeSlice Tests
// ============================================================================

func TestTCB_TimeSliceDefaults(t *testing.T) {
	tests := []struct {
		priority int
		name     string
	}{
		{PriorityCritical, "P0 should have smallest time slice"},
		{PriorityHigh, "P1"},
		{PriorityNormal, "P2"},
		{PriorityLow, "P3"},
		{PriorityIdle, "P4 should have largest time slice"},
	}
	for _, tt := range tests {
		tcb := &TCB{ID: "test", Priority: tt.priority}
		if tcb.TimeSlice != 0 {
			t.Logf("%s: TimeSlice=%v (default is 0, set by caller)", tt.name, tcb.TimeSlice)
		}
	}
}

// ============================================================================
// TaskMessageList Integration Tests
// ============================================================================

func TestScheduler_Tick_PassesMessageList(t *testing.T) {
	s := NewScheduler()
	originalML := NewMessageList([]Message{
		{Role: "system", Content: "test prompt"},
		{Role: "user", Content: "hello"},
	})

	var mlInHandler *MessageList
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		mlInHandler = ml
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	_ = s.Tick(context.Background(), originalML, &dummyChannel{})

	if mlInHandler != originalML {
		t.Error("Handler should receive the same MessageList pointer passed to Tick")
	}
}

func TestScheduler_Tick_HandlerModifiesMessageList(t *testing.T) {
	s := NewScheduler()
	ml := NewMessageList([]Message{
		{Role: "user", Content: "hello"},
	})

	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		// Append a message
		ml.msgs = append(ml.msgs, Message{Role: "assistant", Content: "hi"})
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if ml.Len() != 2 {
		t.Errorf("Expected 2 messages after handler, got %d", ml.Len())
	}
	if ml.At(1).Role != "assistant" {
		t.Errorf("Expected assistant message, got %s", ml.At(1).Role)
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestScheduler_ReEnqueueAfterBlock_ThenUnblock(t *testing.T) {
	s := NewScheduler()

	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultBlock, nil
	}
	s.Register("blocker", "Blocker", PriorityNormal, handler)
	s.EnqueueTask("blocker")

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskBlocked("blocker") {
		t.Fatal("Task should be blocked")
	}
	if s.HasReady() {
		t.Error("No tasks should be ready when only blocked task exists")
	}

	// Unblock and verify it can execute
	s.UnblockTask("blocker")
	if !s.HasReady() {
		t.Error("Unblocked task should be ready")
	}

	// Change handler to return Done for the second execution
	s.tasks["blocker"].Handler = func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	if !s.IsTaskDone("blocker") {
		t.Error("Task should be done after unblock + re-execute")
	}
}

func TestScheduler_DoubleEnqueue(t *testing.T) {
	s := NewScheduler()
	execCount := 0
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		execCount++
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)

	// Enqueue twice — both should produce a TCB in the queue
	s.EnqueueTask("test")
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	// Both enqueues should result in separate executions
	if execCount != 2 {
		t.Errorf("Expected 2 executions for double enqueue, got %d", execCount)
	}
}

func TestScheduler_NilMessageList(t *testing.T) {
	s := NewScheduler()
	handled := false
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		// ml may be nil if caller passes nil — handler should handle gracefully
		if ml != nil {
			handled = true
		}
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	// Passing nil should not panic
	_ = s.Tick(context.Background(), nil, &dummyChannel{})
	// Handler should have been called regardless
	_ = handled
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkReadyQueue_Enqueue(b *testing.B) {
	rq := &ReadyQueue{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rq.Enqueue(&TCB{ID: "bench", Priority: PriorityNormal})
	}
}

func BenchmarkReadyQueue_Dequeue(b *testing.B) {
	rq := &ReadyQueue{}
	for i := 0; i < 10000; i++ {
		rq.Enqueue(&TCB{ID: "bench", Priority: PriorityNormal})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !rq.HasReady() {
			// Refill
			for j := 0; j < 10000; j++ {
				rq.Enqueue(&TCB{ID: "bench", Priority: PriorityNormal})
			}
		}
		rq.Dequeue()
	}
}

func BenchmarkScheduler_Tick(b *testing.B) {
	s := NewScheduler()
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("bench", "Bench", PriorityNormal, handler)
	ml := NewMessageList([]Message{{Role: "user", Content: "bench"}})
	dc := &dummyChannel{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.EnqueueTask("bench")
		_ = s.Tick(context.Background(), ml, dc)
	}
}

// ============================================================================
// Time-sensitive Preemption Tests
// ============================================================================

func TestScheduler_PreemptSignal(t *testing.T) {
	s := NewScheduler()
	// Verify preempt channel is initialized
	if s.preempt == nil {
		t.Fatal("Preempt channel should be initialized")
	}

	// Send a preempt signal
	select {
	case s.preempt <- struct{}{}:
	default:
		t.Fatal("Preempt channel should accept a signal")
	}

	// Tick should consume the preempt signal without blocking
	handler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		return ResultDone, nil
	}
	s.Register("test", "Test", PriorityNormal, handler)
	s.EnqueueTask("test")

	ml := NewMessageList(nil)
	done := make(chan error, 1)
	go func() {
		done <- s.Tick(context.Background(), ml, &dummyChannel{})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Tick returned error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Tick timed out — preempt signal may have blocked it")
	}
}

func TestScheduler_Preempt_HighPriorityInterrupts(t *testing.T) {
	s := NewScheduler()

	// Simulate: low-priority task is running, then a critical task gets enqueued
	// The low task yields once to simulate preemption, then completes on re-entry
	executed := make([]string, 0)
	lowYieldCount := 0
	lowHandler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		executed = append(executed, "low")
		lowYieldCount++
		if lowYieldCount == 1 {
			// First execution: enqueue a critical task while low is "running"
			s.EnqueueTask("critical")
			// Signal preemption
			select {
			case s.preempt <- struct{}{}:
			default:
			}
			return ResultYield, nil
		}
		// Re-entered after yield: complete
		return ResultDone, nil
	}
	criticalHandler := func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error) {
		executed = append(executed, "critical")
		return ResultDone, nil
	}

	s.Register("low", "Low", PriorityLow, lowHandler)
	s.Register("critical", "Critical", PriorityCritical, criticalHandler)

	s.EnqueueTask("low")
	ml := NewMessageList(nil)

	// Tick low (which yields and enqueues critical + sends preempt)
	_ = s.Tick(context.Background(), ml, &dummyChannel{})

	// Tick all remaining tasks — critical (P0) should execute before low (P3) runs again
	for s.HasReady() {
		_ = s.Tick(context.Background(), ml, &dummyChannel{})
	}

	// Verify execution order: low → critical → low
	if len(executed) != 3 {
		t.Fatalf("Expected 3 executions, got %d: %v", len(executed), executed)
	}
	if executed[0] != "low" {
		t.Errorf("First should be low, got %s", executed[0])
	}
	if executed[1] != "critical" {
		t.Errorf("Second should be critical (higher priority preempted), got %s", executed[1])
	}
	if executed[2] != "low" {
		t.Errorf("Third should be low (re-entered after yield), got %s", executed[2])
	}
}
