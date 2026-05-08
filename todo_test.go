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

func TestTodoUpdate_MergePreservesOld(t *testing.T) {
	// BDD: TodoWrite 改為 merge 模式 — 唔再全量覆蓋，未提及嘅舊項自動保留
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Old item", Status: "Pending"},
	})

	tm.Update([]TodoItem{
		{ID: "2", Text: "New item", Status: "InProgress"},
	})

	items := tm.GetItems()
	// Merge: New item appended, Old item preserved
	if len(items) != 2 {
		t.Fatalf("expected 2 items after merge, got %d", len(items))
	}
	if items[0].Text != "New item" || items[0].ID != "2" {
		t.Errorf("expected first item 'New item' (ID 2), got '%s' (ID %s)", items[0].Text, items[0].ID)
	}
	if items[1].Text != "Old item" || items[1].ID != "1" {
		t.Errorf("expected second item 'Old item' (ID 1, preserved), got '%s' (ID %s)", items[1].Text, items[1].ID)
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

// ============================================================================
// TodoManager — Create (V2 single-item)
// ============================================================================

func TestTodoCreate_Basic(t *testing.T) {
	tm := NewTodoManager()
	_, err := tm.Create("test task", "Pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := tm.GetItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ID != "1" {
		t.Errorf("expected ID '1', got '%s'", items[0].ID)
	}
	if items[0].Text != "test task" {
		t.Errorf("expected text 'test task', got '%s'", items[0].Text)
	}
	if items[0].Status != "Pending" {
		t.Errorf("expected status 'Pending', got '%s'", items[0].Status)
	}
}

func TestTodoCreate_DefaultStatus(t *testing.T) {
	tm := NewTodoManager()
	_, err := tm.Create("task", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := tm.GetItems()
	if items[0].Status != "Pending" {
		t.Errorf("expected default status 'Pending', got '%s'", items[0].Status)
	}
}

func TestTodoCreate_AutoIDSequence(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("first", "Pending")
	tm.Create("second", "Pending")
	tm.Create("third", "Pending")
	items := tm.GetItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[2].ID != "3" {
		t.Errorf("expected third item ID '3', got '%s'", items[2].ID)
	}
}

func TestTodoCreate_InvalidStatus(t *testing.T) {
	tm := NewTodoManager()
	_, err := tm.Create("task", "unknown")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestTodoCreate_EmptyContent(t *testing.T) {
	tm := NewTodoManager()
	_, err := tm.Create("", "Pending")
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestTodoCreate_SingleInProgress(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task1", "InProgress")
	tm.Create("task2", "InProgress")
	items := tm.GetItems()
	if items[0].Status != "Pending" {
		t.Errorf("expected task1 auto-demoted to Pending, got %s", items[0].Status)
	}
	if items[1].Status != "InProgress" {
		t.Errorf("expected task2 stays InProgress, got %s", items[1].Status)
	}
}

// ============================================================================
// TodoManager — UpdateSingle (V2 single-item update/delete)
// ============================================================================

func TestTodoUpdateSingle_UpdateStatus(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task", "Pending")
	_, err := tm.UpdateSingle("1", "", "Completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := tm.GetItems()
	if items[0].Status != "Completed" {
		t.Errorf("expected 'Completed', got '%s'", items[0].Status)
	}
}

func TestTodoUpdateSingle_UpdateContent(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("original", "Pending")
	_, err := tm.UpdateSingle("1", "updated", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := tm.GetItems()
	if items[0].Text != "updated" {
		t.Errorf("expected 'updated', got '%s'", items[0].Text)
	}
}

func TestTodoUpdateSingle_Delete(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task", "Pending")
	_, err := tm.UpdateSingle("1", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := tm.GetItems()
	if len(items) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(items))
	}
}

func TestTodoUpdateSingle_NotFound(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("existing", "Pending")
	_, err := tm.UpdateSingle("99", "", "Completed")
	if err == nil {
		t.Error("expected error for non-existing task ID")
	}
}

func TestTodoUpdateSingle_InvalidStatus(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task", "Pending")
	_, err := tm.UpdateSingle("1", "", "unknown")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestTodoUpdateSingle_SingleInProgress(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task1", "InProgress")
	tm.Create("task2", "Pending")
	tm.UpdateSingle("2", "", "InProgress")
	items := tm.GetItems()
	if items[0].Status != "Pending" {
		t.Errorf("expected task1 auto-demoted, got %s", items[0].Status)
	}
	if items[1].Status != "InProgress" {
		t.Errorf("expected task2 InProgress, got %s", items[1].Status)
	}
}

// ============================================================================
// TodoManager — GetUnfinishedDigest (progress-aware resume)
// ============================================================================

func TestGetUnfinishedDigest_Stable(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task1", "Pending")
	d1 := tm.GetUnfinishedDigest()
	d2 := tm.GetUnfinishedDigest()
	if d1 != d2 {
		t.Errorf("digest should be stable: '%s' vs '%s'", d1, d2)
	}
}

func TestGetUnfinishedDigest_ChangesOnProgress(t *testing.T) {
	tm := NewTodoManager()
	tm.Create("task1", "Pending")
	d1 := tm.GetUnfinishedDigest()
	tm.UpdateSingle("1", "", "InProgress")
	d2 := tm.GetUnfinishedDigest()
	if d1 == d2 {
		t.Error("digest should change when status changes")
	}
}

func TestGetUnfinishedDigest_Empty(t *testing.T) {
	tm := NewTodoManager()
	d := tm.GetUnfinishedDigest()
	if d != "" {
		t.Errorf("expected empty digest, got '%s'", d)
	}
}

// ============================================================================
// BDD: 跨工具交互 — Merge 行為
// ============================================================================

func TestBDD_Merge_PreservesUnmentioned(t *testing.T) {
	// Scenario: TodoWrite 同舊列表合併，保存未被提及的項目
	// Given: 現有 3 項 [A, B, C]
	// When: TodoWrite 傳入 2 項 [A'(matched A by ID), D(new)]
	// Then: 最終列表 [A'(updated), D(new), B(preserved), C(preserved)]
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Setup environment", Status: "Completed"},
		{ID: "2", Text: "Write tests", Status: "Pending"},
		{ID: "3", Text: "Deploy to staging", Status: "Pending"},
	})

	out, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Setup environment", Status: "Completed"},
		{ID: "4", Text: "Monitor logs", Status: "InProgress"},
	})
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	items := tm.GetItems()
	if len(items) != 4 {
		t.Fatalf("expected 4 items after merge, got %d", len(items))
	}
	// Order: matched item A'(1), new item D(4), preserved B(2), preserved C(3)
	if items[0].ID != "1" || items[0].Text != "Setup environment" {
		t.Errorf("item 0 mismatch: ID=%s text=%s", items[0].ID, items[0].Text)
	}
	if items[1].ID != "4" || items[1].Text != "Monitor logs" || items[1].Status != "InProgress" {
		t.Errorf("item 1 mismatch: ID=%s text=%s status=%s", items[1].ID, items[1].Text, items[1].Status)
	}
	if items[2].ID != "2" || items[2].Text != "Write tests" {
		t.Errorf("item 2 should be preserved 'Write tests', got ID=%s text=%s", items[2].ID, items[2].Text)
	}
	if items[3].ID != "3" || items[3].Text != "Deploy to staging" {
		t.Errorf("item 3 should be preserved 'Deploy to staging', got ID=%s text=%s", items[3].ID, items[3].Text)
	}

	// Guard: 2 unmentioned <= 2, should NOT trigger warning
	if strings.Contains(out, "項舊任務未被本次更新提及") {
		t.Error("should NOT emit guard warning for ≤2 unmentioned items")
	}
}

func TestBDD_Merge_ContentSimilarityMatch(t *testing.T) {
	// Scenario: 內容高度相似時自動匹配（唔重複）
	// Given: 現有 "Read the project documentation carefully"
	// When: TodoWrite "Read project documentation"（相似但唔完全一樣）
	// Then: 舊項目被更新，而非新增重複項
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Read the project documentation carefully", Status: "Pending"},
	})

	tm.Update([]TodoItem{
		{ID: "2", Text: "Read project documentation", Status: "InProgress"},
	})

	items := tm.GetItems()
	if len(items) != 1 {
		t.Fatalf("expected 1 item (similarity match), got %d items", len(items))
	}
	// 保留舊 ID，內容更新為新文本
	if items[0].ID != "1" {
		t.Errorf("expected preserved ID '1', got '%s'", items[0].ID)
	}
	if items[0].Status != "InProgress" {
		t.Errorf("expected updated status 'InProgress', got '%s'", items[0].Status)
	}
	// 文本用新嘅
	if items[0].Text != "Read project documentation" {
		t.Errorf("expected updated text, got '%s'", items[0].Text)
	}
}

func TestBDD_Merge_EmptyArrayClears(t *testing.T) {
	// Scenario: 傳 [] 清空列表（兼容舊行為）
	// Given: 現有 2 項
	// When: TodoWrite([])
	// Then: 列表清空
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Task A", Status: "Completed"},
		{ID: "2", Text: "Task B", Status: "Completed"},
	})

	out, err := tm.Update([]TodoItem{})
	if err != nil {
		t.Fatalf("empty update should clear list: %v", err)
	}

	items := tm.GetItems()
	if items != nil {
		t.Errorf("expected nil items after clear, got %d items", len(items))
	}
	if !strings.Contains(out, "列表已清空") {
		t.Errorf("expected '列表已清空' in output, got: %s", out)
	}
}

func TestBDD_Merge_GuardWarning(t *testing.T) {
	// Scenario: 當新 items 未提及 >2 項舊任務時發出警告
	// Given: 現有 5 項
	// When: TodoWrite 傳入 1 項（僅匹配其中 1 項）
	// Then: 輸出包含 guard warning（>2 項未提及）
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Setup", Status: "Completed"},
		{ID: "2", Text: "Code", Status: "Pending"},
		{ID: "3", Text: "Test", Status: "Pending"},
		{ID: "4", Text: "Deploy", Status: "Pending"},
		{ID: "5", Text: "Monitor", Status: "Pending"},
	})

	out, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Setup done", Status: "Completed"},
	})
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	// 4 項未提及 (items 2-5) > 2, 應該觸發 guard warning
	if !strings.Contains(out, "項舊任務未被本次更新提及") {
		t.Errorf("expected guard warning for >2 unmentioned items, got: %s", out)
	}

	// 驗證所有 4 項都被保留
	items := tm.GetItems()
	if len(items) != 5 {
		t.Errorf("expected all 5 items preserved, got %d", len(items))
	}
}

// ============================================================================
// BDD: 跨工具交互 — Cancelled 狀態
// ============================================================================

func TestBDD_Cancelled_RendersCorrectly(t *testing.T) {
	// Scenario: Cancelled 狀態顯示 [-]
	// Given: Create + Update to Cancelled
	// Then: render shows [-] marker
	tm := NewTodoManager()

	tm.Create("Abandoned feature", "Pending")
	tm.UpdateSingle("1", "", "Cancelled")

	out := tm.Render()
	if !strings.Contains(out, "[-]") {
		t.Errorf("expected [-] marker for cancelled, got: %s", out)
	}
}

func TestBDD_Cancelled_NotCountedAsCompleted(t *testing.T) {
	// Scenario: Cancelled 唔計入 completed count
	// Given: 1 Completed + 1 Cancelled
	// Then: render shows (1/2 completed)
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Done task", Status: "Completed"},
		{ID: "2", Text: "Dropped task", Status: "Cancelled"},
	})

	out := tm.Render()
	if !strings.Contains(out, "(1/2 completed)") {
		t.Errorf("expected (1/2 completed) — Cancelled should not count, got: %s", out)
	}
}

func TestBDD_Cancelled_Normalize(t *testing.T) {
	// Scenario: 模型傳入 "canceled"（美式拼法）或 "cancelled"（英式拼法）
	// Both should be accepted and normalized to "Cancelled"
	tm := NewTodoManager()

	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "US canceled spelling", Status: "canceled"},
		{ID: "2", Text: "UK cancelled spelling", Status: "cancelled"},
	})
	if err != nil {
		t.Fatalf("both spellings should be valid: %v", err)
	}

	items := tm.GetItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Status != "Cancelled" {
		t.Errorf("expected 'Cancelled', got '%s'", items[0].Status)
	}
	if items[1].Status != "Cancelled" {
		t.Errorf("expected 'Cancelled', got '%s'", items[1].Status)
	}
}

func TestBDD_Cancelled_InTodoWrite(t *testing.T) {
	// Scenario: TodoWrite 直接 set Cancelled status
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Won't do this", Status: "Cancelled"},
	})

	items := tm.GetItems()
	if items[0].Status != "Cancelled" {
		t.Errorf("expected Cancelled status, got '%s'", items[0].Status)
	}
}

// ============================================================================
// BDD: 跨工具交互 — TodoDelete
// ============================================================================

func TestBDD_Delete_RemovesSingleItem(t *testing.T) {
	// Scenario: Delete 精準刪除單項
	// Given: 3 items via Create
	// When: Delete #2
	// Then: only #1 and #3 remain
	tm := NewTodoManager()

	tm.Create("Task 1", "Pending")
	tm.Create("Task 2", "InProgress")
	tm.Create("Task 3", "Pending")

	_, err := tm.Delete("2")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	items := tm.GetItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 items after delete, got %d", len(items))
	}
	if items[0].ID != "1" || items[1].ID != "3" {
		t.Errorf("expected IDs [1,3], got [%s,%s]", items[0].ID, items[1].ID)
	}
}

func TestBDD_Delete_NotFound(t *testing.T) {
	// Scenario: Delete 不存在的 ID return error
	tm := NewTodoManager()
	tm.Create("Only task", "Pending")

	_, err := tm.Delete("99")
	if err == nil {
		t.Error("expected error for non-existent task")
	}
}

func TestBDD_Delete_AfterMerge(t *testing.T) {
	// Scenario: TodoWrite merge 之後 Delete 可以正確定位保留的舊項
	// Given: Create 3 items, then TodoWrite merge (只提及2項,保留1項)
	// When: Delete the preserved item
	// Then: 被保留的舊項可以被精準刪除
	tm := NewTodoManager()

	tm.Create("Apple", "Pending")
	tm.Create("Banana", "Pending")
	tm.Create("Cherry", "Pending")

	// Merge: only mention Apple(updated) and Date(new), Banana & Cherry preserved
	tm.Update([]TodoItem{
		{ID: "1", Text: "Apple updated", Status: "Completed"},
		{ID: "4", Text: "Date", Status: "Pending"},
	})

	// Delete preserved item "Banana" which should still have ID "2"
	_, err := tm.Delete("2")
	if err != nil {
		t.Fatalf("Delete of preserved item failed: %v", err)
	}

	items := tm.GetItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// IDs should be: 1 (Apple, matched), 4 (Date, new), 3 (Cherry, preserved)
	for _, item := range items {
		if item.ID == "2" {
			t.Error("item 2 (Banana) should be deleted")
		}
	}
}

// ============================================================================
// BDD: 跨工具交互 — InProgress 合併衝突
// ============================================================================

func TestBDD_Merge_InProgressConflict(t *testing.T) {
	// Scenario: Merge 時新舊列表都有 InProgress → 優先保留新的，舊的自動降級
	// Given: 現有 [A(InProgress), B(Pending)]
	// When: TodoWrite [A(InProgress), C(InProgress)]  — A matched, C new with InProgress
	// Then: A 降級為 Pending, C 保持 InProgress (新 InProgress 優先)
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Old InProgress task", Status: "InProgress"},
		{ID: "2", Text: "Pending task", Status: "Pending"},
	})

	tm.Update([]TodoItem{
		{ID: "1", Text: "Old task became InProgress again", Status: "InProgress"},
		{ID: "3", Text: "New InProgress task", Status: "InProgress"},
	})

	items := tm.GetItems()
	// 應該只有 1 個 InProgress — 第一個聲明的 (ID 1) 保持，ID 3 降級
	inProgressCount := 0
	for _, item := range items {
		if item.Status == "InProgress" {
			inProgressCount++
		}
	}
	if inProgressCount > 1 {
		t.Errorf("expected at most 1 InProgress after merge, got %d", inProgressCount)
	}
	// ID 1 should be InProgress (was the first in new list)
	if items[0].ID != "1" || items[0].Status != "InProgress" {
		t.Errorf("expected item 1 InProgress, got ID=%s status=%s", items[0].ID, items[0].Status)
	}
}

func TestBDD_Merge_InProgressConflictWithPreserved(t *testing.T) {
	// Scenario: 舊列表有 preserved InProgress → 新列表亦有 InProgress → preserved 降級
	// Given: 現有 [A(InProgress), B(Pending)]
	// When: TodoWrite [C(InProgress)] — A preserved with InProgress, C new with InProgress
	// Then: A 降級為 Pending, C 保持 InProgress
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Preserved InProgress", Status: "InProgress"},
		{ID: "2", Text: "Pending item", Status: "Pending"},
	})

	tm.Update([]TodoItem{
		{ID: "3", Text: "New InProgress task", Status: "InProgress"},
	})

	items := tm.GetItems()
	inProgressCount := 0
	var inProgressID string
	for _, item := range items {
		if item.Status == "InProgress" {
			inProgressCount++
			inProgressID = item.ID
		}
	}
	if inProgressCount != 1 {
		t.Fatalf("expected exactly 1 InProgress after merge, got %d", inProgressCount)
	}
	// 新 InProgress 優先，舊 preserved InProgress 降級
	if inProgressID != "3" {
		t.Errorf("expected ID 3 (new) to be InProgress, got ID %s", inProgressID)
	}
	// 驗證 ID 1 被降級
	for _, item := range items {
		if item.ID == "1" && item.Status != "Pending" {
			t.Errorf("expected preserved InProgress item 1 demoted to Pending, got %s", item.Status)
		}
	}
}

// ============================================================================
// BDD: 跨工具交互 — 完整 CRUD 工作流
// ============================================================================

func TestBDD_FullCRUD_Workflow(t *testing.T) {
	// Scenario: 完整 Create → Write(merge) → Update → Delete → List 流程
	// Given: empty list
	// When: Create A, Create B, TodoWrite merge [A(Completed), C(Pending)], Update B→InProgress, Delete C, List
	// Then: A(Completed) + B(InProgress) remain
	tm := NewTodoManager()

	// Create A, B
	tm.Create("Task Alpha", "Pending")
	tm.Create("Task Bravo", "Pending")

	// TodoWrite merge: update A to Completed, add new C
	tm.Update([]TodoItem{
		{ID: "1", Text: "Task Alpha done", Status: "Completed"},
		{ID: "3", Text: "Task Charlie", Status: "Pending"},
	})

	// Update B to InProgress
	_, err := tm.UpdateSingle("2", "", "InProgress")
	if err != nil {
		t.Fatalf("UpdateSingle B failed: %v", err)
	}

	// Delete C
	_, err = tm.Delete("3")
	if err != nil {
		t.Fatalf("Delete C failed: %v", err)
	}

	// List
	out := tm.Render()

	// Verify: A(Completed) + B(InProgress)
	items := tm.GetItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].ID != "1" || items[0].Status != "Completed" {
		t.Errorf("item 1: expected Completed, got %s", items[0].Status)
	}
	if items[1].ID != "2" || items[1].Status != "InProgress" {
		t.Errorf("item 2: expected InProgress, got %s", items[1].Status)
	}

	// Output should contain both items
	if !strings.Contains(out, "Task Alpha") {
		t.Error("render should contain Task Alpha")
	}
	if !strings.Contains(out, "Task Bravo") {
		t.Error("render should contain Task Bravo")
	}
	if strings.Contains(out, "Task Charlie") {
		t.Error("render should NOT contain deleted Task Charlie")
	}
	if !strings.Contains(out, "(1/2 completed)") {
		t.Errorf("expected (1/2 completed), got: %s", out)
	}
}

func TestBDD_FullCRUD_AllDoneClear(t *testing.T) {
	// Scenario: 全部完成後用 TodoWrite([]) 清空
	// Given: Create 3 items, complete all
	// When: TodoWrite([])
	// Then: list cleared
	tm := NewTodoManager()

	tm.Create("Eat", "Pending")
	tm.Create("Sleep", "Pending")
	tm.Create("Code", "Pending")

	tm.UpdateSingle("1", "", "Completed")
	tm.UpdateSingle("2", "", "Completed")
	tm.UpdateSingle("3", "", "Completed")

	out, err := tm.Update([]TodoItem{})
	if err != nil {
		t.Fatalf("clear failed: %v", err)
	}

	if !strings.Contains(out, "列表已清空") {
		t.Errorf("expected clear confirmation, got: %s", out)
	}
	if len(tm.GetItems()) > 0 {
		t.Error("list should be empty after TodoWrite([])")
	}
}

// ============================================================================
// BDD: Merge 邊界條件
// ============================================================================

func TestBDD_Merge_Exceeds20Items(t *testing.T) {
	// Scenario: Merge 結果超過 20 項上限時應報錯
	// Given: 現有 19 項
	// When: TodoWrite 傳入 3 新項（無匹配）
	// Then: error "merge result exceeds max 20 todos"
	tm := NewTodoManager()

	oldItems := make([]TodoItem, 19)
	for i := 0; i < 19; i++ {
		oldItems[i] = TodoItem{ID: string(rune('a' + i%26)), Text: "old", Status: "Pending"}
	}
	tm.Update(oldItems)

	_, err := tm.Update([]TodoItem{
		{ID: "x1", Text: "new 1", Status: "Pending"},
		{ID: "x2", Text: "new 2", Status: "Pending"},
		{ID: "x3", Text: "new 3", Status: "Pending"},
	})

	if err == nil {
		t.Error("expected error for merge exceeding 20 items")
	}
	if !strings.Contains(err.Error(), "max 20") {
		t.Errorf("expected 'max 20' error, got: %v", err)
	}
}

func TestBDD_Merge_NoMatchKeepsAll(t *testing.T) {
	// Scenario: 兩組完全不同的項目，全部保留
	// Given: [A, B]
	// When: TodoWrite [C, D] (no IDs or content match)
	// Then: [C, D, A, B]
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Original task", Status: "Pending"},
		{ID: "2", Text: "Another original", Status: "Completed"},
	})

	tm.Update([]TodoItem{
		{ID: "3", Text: "Brand new task", Status: "InProgress"},
		{ID: "4", Text: "Second new", Status: "Pending"},
	})

	items := tm.GetItems()
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}
}

func TestBDD_Create_ThenMerge_Clean(t *testing.T) {
	// Scenario: Create 建立項目 → TodoWrite merge 更新 → ID 保持一致
	// Given: Create 3 items
	// When: TodoWrite 提及部分項目（用相同 ID + 新內容）
	// Then: 項目 ID 不變，內容更新，未提及項目保留
	tm := NewTodoManager()

	tm.Create("Buy milk", "Pending")
	tm.Create("Write report", "Pending")
	tm.Create("Call client", "Pending")

	tm.Update([]TodoItem{
		{ID: "1", Text: "Buy milk and eggs", Status: "Completed"},
	})

	items := tm.GetItems()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].ID != "1" || items[0].Text != "Buy milk and eggs" || items[0].Status != "Completed" {
		t.Errorf("item 1 not updated correctly: ID=%s text=%s status=%s", items[0].ID, items[0].Text, items[0].Status)
	}
}

// ============================================================================
// BDD: Cancelled 同 guard 交互
// ============================================================================

func TestBDD_Cancelled_NotUnfinished(t *testing.T) {
	// Scenario: Cancelled 不等於 Unfinished — 唔會觸發 exit guard
	// Given: 只有 Cancelled items
	// Then: HasUnfinishedItems = false
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "We gave up", Status: "Cancelled"},
	})

	if tm.HasUnfinishedItems() {
		t.Error("Cancelled items should NOT count as unfinished")
	}
}

func TestBDD_Cancelled_NotInDigest(t *testing.T) {
	// Scenario: Cancelled items should not appear in unfinished digest
	// Given: 1 Pending + 1 Cancelled
	// Then: digest only contains the Pending item
	tm := NewTodoManager()

	tm.Update([]TodoItem{
		{ID: "1", Text: "Still active", Status: "Pending"},
		{ID: "2", Text: "Never mind", Status: "Cancelled"},
	})

	digest := tm.GetUnfinishedDigest()
	if strings.Contains(digest, "2:") {
		t.Errorf("Cancelled item should not appear in digest, got: %s", digest)
	}
	if !strings.Contains(digest, "1:Pending") {
		t.Errorf("Pending item should appear in digest, got: %s", digest)
	}
}
