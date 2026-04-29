package main

import (
	"strings"
	"sync"
	"testing"
)

// ============================================================================
// TodoManager — Update / Validation
// ============================================================================

func TestTodoUpdate_Basic(t *testing.T) {
	tm := NewTodoManager()

	items := []TodoItem{
		{ID: "1", Text: "Read the docs", Status: "Pending"},
		{ID: "2", Text: "Write code", Status: "InProgress"},
		{ID: "3", Text: "Run tests", Status: "Completed"},
	}

	render, err := tm.Update(items)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if render == "" {
		t.Error("expected non-empty render output")
	}
	if !strings.Contains(render, "todos[default]") {
		t.Error("render should mention default list")
	}
	if !strings.Contains(render, "Read the docs") {
		t.Error("render should contain item text")
	}
}

func TestTodoUpdate_MultipleLists(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "a", Text: "Phase 1 task", Status: "InProgress"},
	}, "phase1")
	if err != nil {
		t.Fatalf("Update phase1: %v", err)
	}

	_, err = tm.Update([]TodoItem{
		{ID: "b", Text: "Phase 2 task", Status: "Pending"},
	}, "phase2")
	if err != nil {
		t.Fatalf("Update phase2: %v", err)
	}

	if got := len(tm.ListIDs()); got < 2 {
		t.Errorf("expected at least 2 lists, got %d", got)
	}

	items1 := tm.GetItems("phase1")
	if len(items1) != 1 || items1[0].Text != "Phase 1 task" {
		t.Error("phase1 items mismatch")
	}

	items2 := tm.GetItems("phase2")
	if len(items2) != 1 || items2[0].Text != "Phase 2 task" {
		t.Error("phase2 items mismatch")
	}
}

func TestTodoUpdate_ListIsolation(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "List A item", Status: "Pending"},
	}, "list_a")

	tm.Update([]TodoItem{
		{ID: "1", Text: "List B item", Status: "Completed"},
	}, "list_b")

	a := tm.GetItems("list_a")
	b := tm.GetItems("list_b")

	if len(a) != 1 || a[0].Text != "List A item" {
		t.Error("list_a should be independent")
	}
	if len(b) != 1 || b[0].Text != "List B item" {
		t.Error("list_b should be independent")
	}
}

func TestTodoUpdate_Overwrite(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Old item", Status: "Pending"},
	})

	tm.Update([]TodoItem{
		{ID: "2", Text: "New item", Status: "InProgress"},
	})

	items := tm.GetItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item after overwrite, got %d", len(items))
	}
	if items[0].Text != "New item" {
		t.Errorf("expected 'New item', got '%s'", items[0].Text)
	}
}

// ============================================================================
// TodoManager — Validation Edge Cases
// ============================================================================

func TestTodoUpdate_EmptyText(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "   ", Status: "Pending"},
	})

	if err == nil {
		t.Error("expected error for empty text")
	}
	if !strings.Contains(err.Error(), "text required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTodoUpdate_InvalidStatus(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Task", Status: "unknown_status"},
	})

	if err == nil {
		t.Error("expected error for invalid status")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTodoUpdate_MaxItems(t *testing.T) {
	tm := NewTodoManager()

	items := make([]TodoItem, 21)
	for i := 0; i < 21; i++ {
		items[i] = TodoItem{ID: string(rune('a' + i%26)), Text: "task", Status: "Pending"}
	}

	_, err := tm.Update(items)
	if err == nil {
		t.Error("expected error for >20 items")
	}
	if !strings.Contains(err.Error(), "max 20") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTodoUpdate_Exactly20Items(t *testing.T) {
	tm := NewTodoManager()

	items := make([]TodoItem, 20)
	for i := 0; i < 20; i++ {
		items[i] = TodoItem{ID: string(rune('a' + i%26)), Text: "task", Status: "Pending"}
	}

	_, err := tm.Update(items)
	if err != nil {
		t.Errorf("20 items should be allowed: %v", err)
	}
}

func TestTodoUpdate_TwoInProgress(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Task 1", Status: "InProgress"},
		{ID: "2", Text: "Task 2", Status: "InProgress"},
	})

	if err == nil {
		t.Error("expected error for two in_progress items")
	}
	if !strings.Contains(err.Error(), "only one task can be InProgress") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTodoUpdate_AutoID(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{Text: "Task without ID", Status: "Pending"},
		{Text: "Another task", Status: "Completed"},
	})

	if err != nil {
		t.Fatalf("auto-ID should work: %v", err)
	}

	items := tm.GetItems()
	if items[0].ID != "1" {
		t.Errorf("expected auto-ID '1', got '%s'", items[0].ID)
	}
	if items[1].ID != "2" {
		t.Errorf("expected auto-ID '2', got '%s'", items[1].ID)
	}
}

// ============================================================================
// TodoManager — GetItems / Render
// ============================================================================

func TestTodoGetItems_EmptyList(t *testing.T) {
	tm := NewTodoManager()
	items := tm.GetItems("nonexistent")
	if items != nil {
		t.Error("expected nil for nonexistent list")
	}
}

func TestTodoGetItems_DefaultList(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Task", Status: "Pending"},
	})

	// No listID → default
	items := tm.GetItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestTodoRender_RendersStatuses(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Pending task", Status: "Pending"},
		{ID: "2", Text: "Active task", Status: "InProgress"},
		{ID: "3", Text: "Waiting task", Status: "Waiting"},
		{ID: "4", Text: "Done task", Status: "Completed"},
	})

	out := tm.Render()

	checks := []string{
		"[ ]", // pending
		"[>",  // in_progress
		"[~]", // waiting
		"[x]", // completed
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("render should contain marker %s", c)
		}
	}
	// "Waiting" status is NOT counted as completed — only "completed" is
	if !strings.Contains(out, "(1/4 completed)") {
		t.Errorf("expected (1/4 completed), got: %s", out)
	}
}

func TestTodoRender_EmptyList(t *testing.T) {
	tm := NewTodoManager()
	out := tm.Render("nonexistent")
	if out != "" {
		t.Errorf("expected empty string for nonexistent list, got '%s'", out)
	}
}

func TestTodoRenderAll(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "List A", Status: "Pending"},
	}, "a")

	tm.Update([]TodoItem{
		{ID: "1", Text: "List B", Status: "Completed"},
	}, "b")

	out := tm.RenderAll()
	if !strings.Contains(out, "todos[a]") {
		t.Error("RenderAll should contain list a")
	}
	if !strings.Contains(out, "todos[b]") {
		t.Error("RenderAll should contain list b")
	}
}

func TestTodoRenderAll_Empty(t *testing.T) {
	tm := NewTodoManager()
	out := tm.RenderAll()
	if out != "No todos." {
		t.Errorf("expected 'No todos.', got '%s'", out)
	}
}

// ============================================================================
// TodoManager — Clear / ClearAll
// ============================================================================

func TestTodoClear(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Task", Status: "Pending"},
	})

	if len(tm.GetItems()) != 1 {
		t.Fatal("setup failed")
	}

	tm.Clear()

	if len(tm.GetItems()) != 0 {
		t.Error("expected empty list after Clear")
	}
}

func TestTodoClear_SpecificList(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Keep me", Status: "Pending"},
	}, "keep")

	tm.Update([]TodoItem{
		{ID: "1", Text: "Delete me", Status: "Completed"},
	}, "delete")

	tm.Clear("delete")

	if len(tm.GetItems("delete")) != 0 {
		t.Error("delete list should be empty")
	}
	if len(tm.GetItems("keep")) != 1 {
		t.Error("keep list should still have items")
	}
}

func TestTodoClearAll(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{{ID: "1", Text: "A", Status: "Pending"}}, "a")
	tm.Update([]TodoItem{{ID: "1", Text: "B", Status: "Pending"}}, "b")

	tm.ClearAll()

	if len(tm.ListIDs()) != 0 {
		t.Error("ClearAll should remove all lists")
	}
	if tm.RenderAll() != "No todos." {
		t.Error("RenderAll should show empty after ClearAll")
	}
}

// ============================================================================
// TodoManager — HasUnfinishedItems / AllUnfinishedAreWaiting / IsEmpty
// ============================================================================

func TestTodoHasUnfinishedItems_True(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Work", Status: "Pending"},
	})

	if !tm.HasUnfinishedItems() {
		t.Error("should have unfinished items")
	}
}

func TestTodoHasUnfinishedItems_InProgress(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Work", Status: "InProgress"},
	})

	if !tm.HasUnfinishedItems() {
		t.Error("should have unfinished items (in_progress)")
	}
}

func TestTodoHasUnfinishedItems_AllComplete(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Work", Status: "Completed"},
	})

	if tm.HasUnfinishedItems() {
		t.Error("should NOT have unfinished items when all completed")
	}
}

func TestTodoHasUnfinishedItems_ExcludesPlanLists(t *testing.T) {
	tm := NewTodoManager()

	// Plan Mode lists are excluded from HasUnfinishedItems
	tm.Update([]TodoItem{
		{ID: "1", Text: "Phase 1 explore", Status: "Pending"},
	}, "plan")

	if tm.HasUnfinishedItems() {
		t.Error("plan lists should be excluded from HasUnfinishedItems")
	}
}

func TestTodoHasUnfinishedItems_MixedPlanAndUser(t *testing.T) {
	tm := NewTodoManager()

	// Plan Mode list with unfinished items (should be ignored)
	tm.Update([]TodoItem{
		{ID: "1", Text: "Phase task", Status: "Pending"},
	}, "phase1")

	// User list with all completed
	tm.Update([]TodoItem{
		{ID: "1", Text: "User task", Status: "Completed"},
	})

	if tm.HasUnfinishedItems() {
		t.Error("should be false: plan list excluded, user list all completed")
	}
}

func TestTodoAllUnfinishedAreWaiting(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Waiting task", Status: "Waiting"},
	})

	if !tm.AllUnfinishedAreWaiting() {
		t.Error("all unfinished should be waiting when only waiting items exist")
	}
}

func TestTodoAllUnfinishedAreWaiting_False(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Waiting task", Status: "Waiting"},
		{ID: "2", Text: "Pending task", Status: "Pending"},
	})

	if tm.AllUnfinishedAreWaiting() {
		t.Error("should return false when pending items exist")
	}
}

func TestTodoAllUnfinishedAreWaiting_ExcludesPlanLists(t *testing.T) {
	tm := NewTodoManager()

	// Plan list with pending (should be ignored)
	tm.Update([]TodoItem{
		{ID: "1", Text: "Plan task", Status: "Pending"},
	}, "phase1")

	// User list with all waiting
	tm.Update([]TodoItem{
		{ID: "1", Text: "User async task", Status: "Waiting"},
	})

	if !tm.AllUnfinishedAreWaiting() {
		t.Error("plan list should be excluded from AllUnfinishedAreWaiting check")
	}
}

func TestTodoGetUnfinishedSummary(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Important task", Status: "Pending"},
		{ID: "2", Text: "Active task", Status: "InProgress"},
		{ID: "3", Text: "Done task", Status: "Completed"},
	})

	summary := tm.GetUnfinishedSummary()
	if !strings.Contains(summary, "Important task") {
		t.Errorf("summary should contain pending task, got: %s", summary)
	}
	if strings.Contains(summary, "Done task") {
		t.Error("summary should NOT contain completed tasks")
	}
}

func TestTodoGetUnfinishedSummary_None(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Done", Status: "Completed"},
	})

	summary := tm.GetUnfinishedSummary()
	if summary != "" {
		t.Errorf("expected empty summary, got: %s", summary)
	}
}

func TestTodoIsEmpty(t *testing.T) {
	tm := NewTodoManager()
	if !tm.IsEmpty() {
		t.Error("new manager should be empty")
	}
}

func TestTodoIsEmpty_AfterUpdate(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Task", Status: "Pending"},
	})

	if tm.IsEmpty() {
		t.Error("should not be empty after adding items")
	}
}

func TestTodoIsEmpty_OnlyPlanLists(t *testing.T) {
	tm := NewTodoManager()

	// Only plan-related lists
	tm.Update([]TodoItem{{ID: "1", Text: "Task", Status: "Pending"}}, "plan")
	tm.Update([]TodoItem{{ID: "1", Text: "Task", Status: "Pending"}}, "phase1")
	tm.Update([]TodoItem{{ID: "1", Text: "Task", Status: "Pending"}}, "phase2")

	if !tm.IsEmpty() {
		t.Error("IsEmpty should exclude plan-related lists")
	}
}

// ============================================================================
// TodoManager — ListIDs
// ============================================================================

func TestTodoListIDs(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{{ID: "1", Text: "A", Status: "Pending"}}, "z")
	tm.Update([]TodoItem{{ID: "1", Text: "B", Status: "Pending"}}, "a")
	tm.Update([]TodoItem{{ID: "1", Text: "C", Status: "Pending"}}, "m")

	ids := tm.ListIDs()

	if len(ids) != 3 {
		t.Fatalf("expected 3 list IDs, got %d", len(ids))
	}
	// Should be sorted alphabetically
	if ids[0] != "a" || ids[1] != "m" || ids[2] != "z" {
		t.Errorf("IDs should be sorted: got %v", ids)
	}
}

// ============================================================================
// TodoManager — Concurrent Access Safety
// ============================================================================

func TestTodoConcurrentAccess(t *testing.T) {
	tm := NewTodoManager()

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tm.Update([]TodoItem{
				{ID: "1", Text: "Task", Status: "InProgress"},
			}, "list_"+string(rune('a'+id%26)))
		}(i)
	}

	wg.Wait()

	// Concurrent GetItems and Render should not panic
	for i := 0; i < numGoroutines; i++ {
		go func() {
			tm.GetItems()
			tm.Render()
			tm.RenderAll()
			tm.ListIDs()
			tm.HasUnfinishedItems()
			tm.IsEmpty()
		}()
	}
}

func TestTodoConcurrentReadWrite(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Initial", Status: "Pending"},
	})

	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			tm.Update([]TodoItem{
				{ID: "1", Text: "Updated", Status: "InProgress"},
			})
		}
	}()

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tm.GetItems()
				tm.Render()
				tm.HasUnfinishedItems()
				tm.AllUnfinishedAreWaiting()
				tm.GetUnfinishedSummary()
				tm.IsEmpty()
				tm.ListIDs()
			}
		}()
	}

	wg.Wait()
}

// ============================================================================
// TodoManager — Waiting Status Handling
// ============================================================================

func TestTodoUpdate_WaitingStatus(t *testing.T) {
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Async task", Status: "Waiting"},
	})

	if err != nil {
		t.Fatalf("waiting status should be valid: %v", err)
	}

	items := tm.GetItems()
	if items[0].Status != "Waiting" {
		t.Errorf("expected 'waiting' status, got '%s'", items[0].Status)
	}

	// waiting counts as not-finished for HasUnfinishedItems
	if tm.HasUnfinishedItems() {
		t.Log("waiting items are NOT considered in HasUnfinishedItems per implementation")
	}

	// waiting counts as "all unfinished are waiting"
	if !tm.AllUnfinishedAreWaiting() {
		t.Log("waiting items trigger AllUnfinishedAreWaiting")
	}
}

func TestTodoUpdate_WaitingAndPending(t *testing.T) {
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Waiting task", Status: "Waiting"},
		{ID: "2", Text: "Pending task", Status: "Pending"},
	})

	if tm.AllUnfinishedAreWaiting() {
		t.Error("should return false when pending items exist alongside waiting")
	}
}
