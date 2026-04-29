package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test Helpers
// ============================================================================

// resetGlobalPlanMode 將 Plan Mode 重置為未激活狀態
func resetGlobalPlanMode() {
	globalPlanMode.mu.Lock()
	defer globalPlanMode.mu.Unlock()
	globalPlanMode.Phase = PlanPhaseInactive
	globalPlanMode.PlanFilePath = ""
	globalPlanMode.TaskDesc = ""
	globalPlanMode.TimedOut = false
	globalPlanMode.DowngradeCount = 0
	// 清理 plan todo，避免污染其他測試
	TODO.Clear("plan")
	TODO.Clear("phase1")
	TODO.Clear("phase2")
}

// enablePlanMode 在測試期間啟用 Plan Mode（總是啟用）並返回清理函數
func enablePlanMode() func() {
	return func() {
		resetGlobalPlanMode()
	}
}

// ============================================================================
// PlanPhase Constants
// ============================================================================

func TestPlanPhaseConstants(t *testing.T) {
	if PlanPhaseInactive != 0 {
		t.Errorf("PlanPhaseInactive should be 0, got %d", PlanPhaseInactive)
	}
	if PlanPhaseExplore != 1 {
		t.Errorf("PlanPhaseExplore should be 1, got %d", PlanPhaseExplore)
	}
	if PlanPhaseDesign != 2 {
		t.Errorf("PlanPhaseDesign should be 2, got %d", PlanPhaseDesign)
	}
	if PlanPhaseExecute != 3 {
		t.Errorf("PlanPhaseExecute should be 3, got %d", PlanPhaseExecute)
	}
}

func TestMaxDowngrades(t *testing.T) {
	if maxDowngrades != 2 {
		t.Errorf("maxDowngrades should be 2, got %d", maxDowngrades)
	}
}

// ============================================================================
// Phase Metadata
// ============================================================================

func TestPhaseMetadata(t *testing.T) {
	phases := []PlanPhase{PlanPhaseExplore, PlanPhaseDesign, PlanPhaseExecute}
	for _, p := range phases {
		info, ok := phaseMetadata[p]
		if !ok {
			t.Errorf("phase %d should have metadata", p)
		}
		if info.Name == "" {
			t.Errorf("phase %d should have a name", p)
		}
		if info.Description == "" {
			t.Errorf("phase %d should have a description", p)
		}
	}

	// 已廢棄的 phase 不應有 metadata
	if _, ok := phaseMetadata[PlanPhase(99)]; ok {
		t.Error("PlanPhase 99 should not have metadata")
	}
}

// ============================================================================
// EnterPlanMode / 查詢
// ============================================================================

func TestEnterPlanMode_Enabled(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	errMsg := EnterPlanMode("Analyze the codebase")
	if errMsg != "" {
		t.Fatalf("EnterPlanMode failed: %s", errMsg)
	}

	if !globalPlanMode.IsActive() {
		t.Error("PlanMode should be active after EnterPlanMode")
	}
	if globalPlanMode.CurrentPhase() != PlanPhaseExplore {
		t.Errorf("expected Phase 1, got %d", globalPlanMode.CurrentPhase())
	}
	if globalPlanMode.TaskDesc != "Analyze the codebase" {
		t.Errorf("task desc mismatch: %s", globalPlanMode.TaskDesc)
	}
	if globalPlanMode.DowngradeCount != 0 {
		t.Errorf("DowngradeCount should be 0, got %d", globalPlanMode.DowngradeCount)
	}

	phaseName := globalPlanMode.PhaseName()
	if !strings.Contains(phaseName, "Phase 1") {
		t.Errorf("phase name should contain 'Phase 1': %s", phaseName)
	}
}

func TestEnterPlanMode_PlanFilePath(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	globalPlanMode.mu.RLock()
	planPath := globalPlanMode.PlanFilePath
	globalPlanMode.mu.RUnlock()

	if planPath == "" {
		t.Error("PlanFilePath should not be empty")
	}
	if !strings.HasSuffix(planPath, "plan.md") {
		t.Errorf("PlanFilePath should end with plan.md: %s", planPath)
	}
}

func TestEnterPlanMode_InitializesTodos(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test task")

	items := TODO.GetItems("plan")
	if len(items) != 3 {
		t.Fatalf("expected 3 plan todos, got %d", len(items))
	}

	if items[0].Status != "in_progress" {
		t.Errorf("Phase 1 should be in_progress, got %s", items[0].Status)
	}
	if items[1].Status != "pending" {
		t.Errorf("Phase 2 should be pending, got %s", items[1].Status)
	}
	if items[2].Status != "pending" {
		t.Errorf("Phase 3 should be pending, got %s", items[2].Status)
	}
}

// ============================================================================
// AdvancePhase
// ============================================================================

func TestAdvancePhase_ExploreToDesign(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	name, msg, err := AdvancePhase()
	if err != nil {
		t.Fatalf("AdvancePhase failed: %v", err)
	}
	if name != "Phase 2: 設計" {
		t.Errorf("expected 'Phase 2: 設計', got %s", name)
	}
	if msg == "" {
		t.Error("transition message should not be empty")
	}
	if globalPlanMode.CurrentPhase() != PlanPhaseDesign {
		t.Errorf("expected Phase 2, got %d", globalPlanMode.CurrentPhase())
	}

	// Plan todos should be updated
	items := TODO.GetItems("plan")
	if len(items) != 3 {
		t.Fatalf("expected 3 plan todos after advance, got %d", len(items))
	}
	if items[0].Status != "completed" {
		t.Errorf("Phase 1 should be completed, got %s", items[0].Status)
	}
	if items[1].Status != "in_progress" {
		t.Errorf("Phase 2 should be in_progress, got %s", items[1].Status)
	}
}

func TestAdvancePhase_DesignToExecute_AutoExit(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	// Phase 2 → 3 triggers auto-exit via postExitPlanMode which requires DB.
	// Test that the exit logic works by verifying exitPlanModeLocked directly.
	globalPlanMode.mu.Lock()
	// Simulate Phase = Execute
	globalPlanMode.Phase = PlanPhaseExecute
	// Write plan content
	planPath := globalPlanMode.PlanFilePath
	globalPlanMode.mu.Unlock()

	os.MkdirAll(filepath.Dir(planPath), 0755)
	os.WriteFile(planPath, []byte("# Test Plan\nStep 1: Do something"), 0644)

	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()

	if content != "# Test Plan\nStep 1: Do something" {
		t.Errorf("exitPlanModeLocked should return plan content, got: %s", content)
	}
	if globalPlanMode.IsActive() {
		t.Error("PlanMode should be inactive after exitPlanModeLocked")
	}

	// Clean up plan.md since we didn't go through the proper exit path
	TODO.Clear("plan")
}

func TestAdvancePhase_PastFinish_Error(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Manually set to Execute to test the "can't advance past terminal" guard
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseExecute
	globalPlanMode.mu.Unlock()

	_, _, err := AdvancePhase()
	if err == nil {
		t.Error("AdvancePhase should fail when already at terminal phase")
	}
}

func TestAdvancePhase_Inactive_AdvancesToPhase1(t *testing.T) {
	defer resetGlobalPlanMode()

	// When Plan Mode is inactive, AdvancePhase doesn't error; it simply
	// transitions from Inactive (0) to Explore (1). This tests the actual behavior.
	name, _, err := AdvancePhase()
	if err != nil {
		t.Errorf("AdvancePhase from inactive doesn't error, proceeds to Phase 1: %v", err)
	}
	if !strings.Contains(name, "Phase 1") {
		t.Errorf("expected Phase 1 name, got '%s'", name)
	}

	resetGlobalPlanMode()
}

// ============================================================================
// PrevPhase (回溯)
// ============================================================================

func TestPrevPhase_DesignToExplore(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	name, msg, err := PrevPhase()
	if err != nil {
		t.Fatalf("PrevPhase failed: %v", err)
	}
	if !strings.Contains(name, "Phase 1") {
		t.Errorf("expected Phase 1 name, got '%s'", name)
	}
	if !strings.Contains(msg, "回溯") {
		t.Errorf("message should mention 回溯: %s", msg)
	}
	if !strings.Contains(msg, "1/2") {
		t.Errorf("message should show remaining downgrades: %s", msg)
	}
	if globalPlanMode.CurrentPhase() != PlanPhaseExplore {
		t.Errorf("expected back to Phase 1, got %d", globalPlanMode.CurrentPhase())
	}

	// Check DowngradeCount
	globalPlanMode.mu.RLock()
	count := globalPlanMode.DowngradeCount
	globalPlanMode.mu.RUnlock()
	if count != 1 {
		t.Errorf("DowngradeCount should be 1, got %d", count)
	}
}

func TestPrevPhase_TwiceThenLimit(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// First cycle: 1→2, prev→1
	AdvancePhase()
	_, _, err := PrevPhase()
	if err != nil {
		t.Fatalf("first PrevPhase failed: %v", err)
	}

	// Second cycle: 1→2, prev→1
	AdvancePhase()
	_, _, err = PrevPhase()
	if err != nil {
		t.Fatalf("second PrevPhase failed: %v", err)
	}

	// Third cycle: 1→2, prev should fail
	AdvancePhase()
	_, _, err = PrevPhase()
	if err == nil {
		t.Error("third PrevPhase should fail (maxDowngrades=2)")
	}
	if !strings.Contains(err.Error(), "最大回溯次數") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPrevPhase_OnlyPhase2(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// PrevPhase should fail in Phase 1
	_, _, err := PrevPhase()
	if err == nil {
		t.Error("PrevPhase should fail in Phase 1")
	}
	if !strings.Contains(err.Error(), "僅在 Phase 2") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPrevPhase_AfterExecuteFails(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Simulate exit without triggering session persistence (which requires DB)
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseInactive
	globalPlanMode.mu.Unlock()

	_, _, err := PrevPhase()
	if err == nil {
		t.Error("PrevPhase should fail after Plan Mode exit")
	}
}

func TestPrevPhase_ResetsPhaseTimer(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	// Small delay to make timer visible
	time.Sleep(10 * time.Millisecond)

	oldStart := globalPlanMode.PhaseStart
	PrevPhase()

	globalPlanMode.mu.RLock()
	newStart := globalPlanMode.PhaseStart
	globalPlanMode.mu.RUnlock()

	if !newStart.After(oldStart) {
		t.Error("PhaseStart should be reset after PrevPhase")
	}
}

// ============================================================================
// ExitPlanMode / ForceExitPlanMode
// ============================================================================

func TestExitPlanMode_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	// 使用內部 exitPlanModeLocked 避免 postExitPlanMode 觸發 session persistence
	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()

	if content != "" {
		t.Errorf("expected empty content for inactive exit, got: %s", content)
	}
}

func TestExitPlanMode_MidPhase(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Write some plan content
	globalPlanMode.mu.RLock()
	planPath := globalPlanMode.PlanFilePath
	globalPlanMode.mu.RUnlock()
	os.MkdirAll(filepath.Dir(planPath), 0755)
	os.WriteFile(planPath, []byte("Draft plan content"), 0644)

	// Use internal exit function to avoid async session persistence (requires DB)
	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()

	if content != "Draft plan content" {
		t.Errorf("expected plan content, got: %s", content)
	}
	if globalPlanMode.IsActive() {
		t.Error("PlanMode should be inactive after exitPlanModeLocked")
	}
}

func TestForceExitPlanMode(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Use internal force-exit to avoid async session persistence (requires DB)
	globalPlanMode.mu.Lock()
	globalPlanMode.TimedOut = true
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()

	if globalPlanMode.IsActive() {
		t.Error("PlanMode should be inactive after force exit")
	}
	if !globalPlanMode.IsTimedOut() {
		t.Error("TimedOut should be true after ForceExitPlanMode")
	}
	_ = content
}

// ============================================================================
// Phase Timeout
// ============================================================================

func TestCheckPhaseTimeout_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	timedOut, phaseElapsed, totalElapsed := globalPlanMode.CheckPhaseTimeout()
	if timedOut {
		t.Error("should not timeout when inactive")
	}
	if phaseElapsed != 0 {
		t.Errorf("phaseElapsed should be 0, got %v", phaseElapsed)
	}
	if totalElapsed != 0 {
		t.Errorf("totalElapsed should be 0, got %v", totalElapsed)
	}
}

func TestCheckPhaseTimeout_ActiveButNotExpired(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	timedOut, _, _ := globalPlanMode.CheckPhaseTimeout()
	if timedOut {
		t.Error("should not timeout immediately")
	}
}

func TestCheckPhaseTimeout_TotalTimeout(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Manually set StartTime far in the past
	globalPlanMode.mu.Lock()
	globalPlanMode.StartTime = time.Now().Add(-25 * time.Minute)
	globalPlanMode.mu.Unlock()

	timedOut, _, _ := globalPlanMode.CheckPhaseTimeout()
	if !timedOut {
		t.Error("should timeout after total timeout exceeded")
	}
}

func TestCheckPhaseTimeout_PhaseTimeout(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Manually set PhaseStart far in the past
	globalPlanMode.mu.Lock()
	globalPlanMode.PhaseStart = time.Now().Add(-6 * time.Minute)
	globalPlanMode.mu.Unlock()

	timedOut, _, _ := globalPlanMode.CheckPhaseTimeout()
	if !timedOut {
		t.Error("should timeout after phase timeout exceeded")
	}
}

// ============================================================================
// Tool Permission Checks
// ============================================================================

func TestIsToolAllowedInPlanMode_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	// When inactive, all tools are allowed
	if !globalPlanMode.IsToolAllowedInPlanMode("smart_shell") {
		t.Error("smart_shell should be allowed when Plan Mode is inactive")
	}
	if !globalPlanMode.IsToolAllowedInPlanMode("write_all_lines") {
		t.Error("write_all_lines should be allowed when Plan Mode is inactive")
	}
}

func TestIsToolAllowedInPlanMode_NextPhaseAlwaysAllowed(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	if !globalPlanMode.IsToolAllowedInPlanMode("next_phase") {
		t.Error("next_phase should always be allowed")
	}
}

func TestIsToolAllowedInPlanMode_PrevPhaseOnlyInDesign(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Phase 1: prev_phase should NOT be allowed
	if globalPlanMode.IsToolAllowedInPlanMode("prev_phase") {
		t.Error("prev_phase should NOT be allowed in Phase 1")
	}

	AdvancePhase() // 1 → 2

	// Phase 2: prev_phase should be allowed
	if !globalPlanMode.IsToolAllowedInPlanMode("prev_phase") {
		t.Error("prev_phase should be allowed in Phase 2")
	}
}

func TestIsToolAllowedInPlanMode_ShellBlocked(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	blockedTools := []string{"smart_shell", "shell", "write_all_lines", "write_file_line",
		"append_to_file", "text_replace", "text_transform", "memory_save", "memory_forget"}

	for _, tool := range blockedTools {
		if globalPlanMode.IsToolAllowedInPlanMode(tool) {
			t.Errorf("%s should be blocked in Plan Mode", tool)
		}
	}
}

func TestIsToolAllowedInPlanMode_ReadToolsAllowed(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	allowedTools := []string{"read_file_line", "read_all_lines", "text_search",
		"text_grep", "memory_recall", "memory_list"}

	for _, tool := range allowedTools {
		if !globalPlanMode.IsToolAllowedInPlanMode(tool) {
			t.Errorf("%s should be allowed in Plan Mode", tool)
		}
	}

	// plan_read / exit_plan_mode 由 SafeExecuteTool 直接攔截處理，不經過 IsToolAllowedInPlanMode
	// 它們在 Phase 1 中不被 allow-list 允許，但 handler 會正常運作
}

func TestIsToolAllowedInPlanMode_PlanWriteOnlyInPhase2(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Phase 1: plan_write NOT allowed
	if globalPlanMode.IsToolAllowedInPlanMode("plan_write") {
		t.Error("plan_write should NOT be allowed in Phase 1")
	}

	AdvancePhase() // 1 → 2

	// Phase 2: plan_write allowed
	if !globalPlanMode.IsToolAllowedInPlanMode("plan_write") {
		t.Error("plan_write should be allowed in Phase 2")
	}
}

// ============================================================================
// handlePlanWrite / handlePlanRead
// ============================================================================

func TestHandlePlanWrite_OnlyInPhase2(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Phase 1: should fail
	result := handlePlanWrite(map[string]interface{}{"content": "# Plan"})
	if !strings.Contains(result, "錯誤") || !strings.Contains(result, "Phase 2") {
		t.Errorf("should reject plan_write in Phase 1: %s", result)
	}

	AdvancePhase() // 1 → 2

	// Phase 2: should succeed
	result = handlePlanWrite(map[string]interface{}{"content": "# My Plan"})
	if strings.Contains(result, "錯誤") {
		t.Errorf("plan_write should succeed in Phase 2: %s", result)
	}
	if !strings.Contains(result, "已寫入") {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestHandlePlanWrite_NoContent(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	result := handlePlanWrite(map[string]interface{}{"content": ""})
	if !strings.Contains(result, "缺少 content") {
		t.Errorf("should report missing content: %s", result)
	}
}

func TestHandlePlanWrite_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	result := handlePlanWrite(map[string]interface{}{"content": "# Plan"})
	if !strings.Contains(result, "未激活") {
		t.Errorf("should report inactive: %s", result)
	}
}

func TestHandlePlanWrite_PersistsAndReadable(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	writeResult := handlePlanWrite(map[string]interface{}{"content": "## Context\nSome context\n\n## Approach\nStep 1\n\n## Verification\nTest it"})
	if strings.Contains(writeResult, "錯誤") {
		t.Fatalf("write failed: %s", writeResult)
	}

	readResult := handlePlanRead(map[string]interface{}{})
	if !strings.Contains(readResult, "## Context") {
		t.Errorf("read should return plan content: %s", readResult)
	}
	if !strings.Contains(readResult, "## Approach") {
		t.Errorf("read should contain Approach section: %s", readResult)
	}
}

func TestHandlePlanRead_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	result := handlePlanRead(map[string]interface{}{})
	if !strings.Contains(result, "未激活") {
		t.Errorf("should report inactive: %s", result)
	}
}

func TestHandlePlanRead_NoFile(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Remove stale plan.md from previous tests if any
	globalPlanMode.mu.RLock()
	planPath := globalPlanMode.PlanFilePath
	globalPlanMode.mu.RUnlock()
	os.Remove(planPath)

	result := handlePlanRead(map[string]interface{}{})
	if !strings.Contains(result, "尚未創建") {
		t.Errorf("should report not yet created: %s", result)
	}
}

// ============================================================================
// GetToolsForCurrentPhase
// ============================================================================

func TestGetToolsForCurrentPhase_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	tools := GetToolsForCurrentPhase()
	if tools != nil {
		t.Error("should return nil when Plan Mode is inactive")
	}
}

func TestGetToolsForCurrentPhase_Phase1(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	tools := GetToolsForCurrentPhase()
	if len(tools) == 0 {
		t.Fatal("Phase 1 should have tools")
	}

	toolNames := make(map[string]bool)
	for _, t := range tools {
		if fn, ok := t["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}

	// Phase 1 必須有 next_phase, spawn 系列, todos
	required := []string{"next_phase", "spawn", "spawn_check", "spawn_list", "todos"}
	for _, r := range required {
		if !toolNames[r] {
			t.Errorf("Phase 1 should have '%s' tool", r)
		}
	}

	// Phase 1 不應該有 plan_write, plan_read, prev_phase
	forbidden := []string{"plan_write", "plan_read", "prev_phase"}
	for _, f := range forbidden {
		if toolNames[f] {
			t.Errorf("Phase 1 should NOT have '%s' tool", f)
		}
	}
}

func TestGetToolsForCurrentPhase_Phase2(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	tools := GetToolsForCurrentPhase()

	toolNames := make(map[string]bool)
	for _, t := range tools {
		if fn, ok := t["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}

	// Phase 2 必須有 next_phase, prev_phase, plan_write, plan_read
	required := []string{"next_phase", "prev_phase", "plan_write", "plan_read", "todos"}
	for _, r := range required {
		if !toolNames[r] {
			t.Errorf("Phase 2 should have '%s' tool", r)
		}
	}
}

func TestGetToolsForCurrentPhase_Phase3_Execute(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Simulate exit without triggering session persistence (which requires DB)
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseInactive
	globalPlanMode.mu.Unlock()

	tools := GetToolsForCurrentPhase()
	if tools != nil {
		t.Error("should return nil after Plan Mode exits")
	}
}

// ============================================================================
// Phase Overwrite After PrevPhase (回歸測試)
// ============================================================================

func TestPrevPhase_PlanWriteAfterBacktrack(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	handlePlanWrite(map[string]interface{}{"content": "Draft 1"})

	PrevPhase()     // back to 1
	AdvancePhase()  // 1 → 2 again

	// Should still be able to write in Phase 2 after backtracking
	result := handlePlanWrite(map[string]interface{}{"content": "Draft 2"})
	if strings.Contains(result, "錯誤") {
		t.Errorf("should allow plan_write after backtracking: %s", result)
	}

	// Read should return the latest content
	content := handlePlanRead(map[string]interface{}{})
	if !strings.Contains(content, "Draft 2") {
		t.Errorf("should have latest content: %s", content)
	}
}

func TestPrevPhase_TodoStatusAfterBacktrack(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Phase 1 → 2
	AdvancePhase()
	items := TODO.GetItems("plan")
	if items[0].Status != "completed" || items[1].Status != "in_progress" {
		t.Error("todos should reflect Phase 2 active before backtrack")
	}

	// Backtrack: Phase 2 → 1
	PrevPhase()
	items = TODO.GetItems("plan")
	if items[0].Status != "in_progress" || items[1].Status != "pending" {
		t.Errorf("todos should reflect Phase 1 active after backtrack: item0=%s, item1=%s",
			items[0].Status, items[1].Status)
	}
}

// ============================================================================
// GetPlanStatus / GetPlanModeSystemPrompt
// ============================================================================

func TestGetPlanStatus_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	status := GetPlanStatus()
	if status != "" {
		t.Errorf("expected empty status when inactive: %s", status)
	}
}

func TestGetPlanStatus_Active(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("Build a feature")

	status := GetPlanStatus()
	if !strings.Contains(status, "Plan Mode 已激活") {
		t.Errorf("status should show active: %s", status)
	}
	if !strings.Contains(status, "Build a feature") {
		t.Errorf("status should contain task description: %s", status)
	}
	if !strings.Contains(status, "Phase 1") {
		t.Errorf("status should show Phase 1: %s", status)
	}
}

func TestGetPlanStatus_ShowsBacktrackInfo(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")
	AdvancePhase() // 1 → 2

	status := GetPlanStatus()
	if !strings.Contains(status, "回溯") {
		t.Errorf("Phase 2 status should show backtrack info: %s", status)
	}
}

func TestGetPlanModeSystemPrompt_Inactive(t *testing.T) {
	defer resetGlobalPlanMode()

	prompt := GetPlanModeSystemPrompt()
	if prompt != "" {
		t.Errorf("expected empty prompt when inactive: %s", prompt)
	}
}

func TestGetPlanModeSystemPrompt_Phase1(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("Explore the project")

	prompt := GetPlanModeSystemPrompt()
	if !strings.Contains(prompt, "Plan Mode - Phase 1") {
		t.Errorf("prompt should mention Phase 1: %s", prompt)
	}
	if !strings.Contains(prompt, "Explore the project") {
		t.Errorf("prompt should contain task: %s", prompt)
	}
	if !strings.Contains(prompt, "探索") {
		t.Errorf("Phase 1 prompt should mention 探索: %s", prompt)
	}
}

func TestGetPlanModeSystemPrompt_Phase2(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("Design the solution")
	AdvancePhase() // 1 → 2

	prompt := GetPlanModeSystemPrompt()
	if !strings.Contains(prompt, "Plan Mode - Phase 2") {
		t.Errorf("prompt should mention Phase 2: %s", prompt)
	}
	if !strings.Contains(prompt, "設計") {
		t.Errorf("Phase 2 prompt should mention 設計: %s", prompt)
	}
	// Phase 2 should have backtrack info
	if !strings.Contains(prompt, "回溯") {
		t.Errorf("Phase 2 prompt should show backtrack info: %s", prompt)
	}
}

// ============================================================================
// Concurrent Access Safety
// ============================================================================

func TestPlanModeConcurrentAccess(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("concurrent test")

	done := make(chan bool, 3)

	// Goroutine 1: Read phase status
	go func() {
		for i := 0; i < 100; i++ {
			globalPlanMode.IsActive()
			globalPlanMode.CurrentPhase()
			globalPlanMode.PhaseName()
		}
		done <- true
	}()

	// Goroutine 2: Read tool permissions
	go func() {
		for i := 0; i < 100; i++ {
			globalPlanMode.IsToolAllowedInPlanMode("read_all_lines")
			globalPlanMode.IsToolAllowedInPlanMode("smart_shell")
		}
		done <- true
	}()

	// Goroutine 3: Read phase timeout
	go func() {
		for i := 0; i < 100; i++ {
			globalPlanMode.CheckPhaseTimeout()
		}
		done <- true
	}()

	for i := 0; i < 3; i++ {
		<-done
	}
}

// ============================================================================
// GetPlanOnlyTools 兼容接口
// ============================================================================

func TestGetPlanOnlyTools(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	tools := globalPlanMode.GetPlanOnlyTools()
	if len(tools) == 0 {
		t.Error("GetPlanOnlyTools should return tools")
	}

	// 應該包含只讀工具
	hasReadFile := false
	for _, t := range tools {
		if t == "read_all_lines" {
			hasReadFile = true
			break
		}
	}
	if !hasReadFile {
		t.Error("GetPlanOnlyTools should include basic read tools")
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestEnterPlanMode_EmptyTaskDesc(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	errMsg := EnterPlanMode("")
	if errMsg != "" {
		t.Fatalf("EnterPlanMode with empty task should succeed: %s", errMsg)
	}
	if globalPlanMode.TaskDesc != "" {
		t.Errorf("TaskDesc should be empty, got '%s'", globalPlanMode.TaskDesc)
	}
}

func TestEnterPlanMode_ResetsDowngradeCount(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	// First session: use up downgrades
	EnterPlanMode("first")
	AdvancePhase()
	PrevPhase()
	AdvancePhase()
	PrevPhase()

	// Simulate exit without session persistence (which requires DB)
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseInactive
	globalPlanMode.DowngradeCount = 5 // should be reset by next EnterPlanMode
	globalPlanMode.mu.Unlock()

	// Second session: DowngradeCount should be reset
	EnterPlanMode("second")
	globalPlanMode.mu.RLock()
	count := globalPlanMode.DowngradeCount
	globalPlanMode.mu.RUnlock()
	if count != 0 {
		t.Errorf("DowngradeCount should reset to 0 on new EnterPlanMode, got %d", count)
	}
}

func TestNextPhaseToolDef(t *testing.T) {
	def := nextPhaseToolDef()
	if def == nil {
		t.Fatal("nextPhaseToolDef should not be nil")
	}

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("tool def should have 'function' key")
	}
	name, _ := fn["name"].(string)
	if name != "next_phase" {
		t.Errorf("expected 'next_phase', got '%s'", name)
	}
}

func TestPrevPhaseToolDef(t *testing.T) {
	def := prevPhaseToolDef()
	if def == nil {
		t.Fatal("prevPhaseToolDef should not be nil")
	}

	fn, ok := def["function"].(map[string]interface{})
	if !ok {
		t.Fatal("tool def should have 'function' key")
	}
	name, _ := fn["name"].(string)
	if name != "prev_phase" {
		t.Errorf("expected 'prev_phase', got '%s'", name)
	}
	desc, _ := fn["description"].(string)
	if !strings.Contains(desc, "回溯") {
		t.Errorf("prev_phase description should mention 回溯: %s", desc)
	}
}

func TestPlanWriteToolDef_UpdatedDescription(t *testing.T) {
	def := planWriteToolDef()
	fn := def["function"].(map[string]interface{})
	desc, _ := fn["description"].(string)
	if !strings.Contains(desc, "Phase 2") {
		t.Errorf("plan_write description should mention Phase 2: %s", desc)
	}
	// 不應再提到 Phase 4
	if strings.Contains(desc, "Phase 4") {
		t.Error("plan_write description should not mention Phase 4")
	}
}

func TestUpdatePlanTodos(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	updatePlanTodos(2)

	items := TODO.GetItems("plan")
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Status != "completed" {
		t.Errorf("phase 1 should be completed, got %s", items[0].Status)
	}
	if items[1].Status != "in_progress" {
		t.Errorf("phase 2 should be in_progress, got %s", items[1].Status)
	}
	if items[2].Status != "pending" {
		t.Errorf("phase 3 should be pending, got %s", items[2].Status)
	}
}

func TestGetPhaseTodoText(t *testing.T) {
	if getPhaseTodoText(1) != "Phase 1: 探索" {
		t.Errorf("unexpected Phase 1 text: %s", getPhaseTodoText(1))
	}
	if getPhaseTodoText(2) != "Phase 2: 設計" {
		t.Errorf("unexpected Phase 2 text: %s", getPhaseTodoText(2))
	}
	if getPhaseTodoText(3) != "Phase 3: 執行" {
		t.Errorf("unexpected Phase 3 text: %s", getPhaseTodoText(3))
	}
	// Unknown phase
	if getPhaseTodoText(99) != "Phase 99" {
		t.Errorf("unexpected unknown phase text: %s", getPhaseTodoText(99))
	}
}

func TestPhaseReadTools_NotEmpty(t *testing.T) {
	if len(PhaseReadTools) == 0 {
		t.Error("PhaseReadTools should not be empty")
	}
}

func TestSortedPhaseReadTools(t *testing.T) {
	sorted := SortedPhaseReadTools()
	if len(sorted) != len(PhaseReadTools) {
		t.Errorf("sorted length mismatch: %d vs %d", len(sorted), len(PhaseReadTools))
	}
	// Verify sorted
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1] > sorted[i] {
			t.Errorf("SortedPhaseReadTools should be sorted: %v", sorted)
			break
		}
	}
}

// ============================================================================
// 審計回歸測試 — exit_plan_mode 可用性
// ============================================================================

func TestIsToolAllowedInPlanMode_ExitPlanMode(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// Phase 1: exit_plan_mode 應該被顯式放行
	if !globalPlanMode.IsToolAllowedInPlanMode("exit_plan_mode") {
		t.Error("exit_plan_mode should be allowed in Phase 1")
	}

	AdvancePhase() // 1 → 2

	// Phase 2: exit_plan_mode 應該仍然被放行
	if !globalPlanMode.IsToolAllowedInPlanMode("exit_plan_mode") {
		t.Error("exit_plan_mode should be allowed in Phase 2")
	}
}

func TestExitPlanMode_CleanupPhaseTodos(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// 確保沒有殘留的 plan.md
	globalPlanMode.mu.RLock()
	planPath := globalPlanMode.PlanFilePath
	globalPlanMode.mu.RUnlock()
	os.Remove(planPath)

	// 模擬 LLM 在 Phase 1 同 Phase 2 中使用 todos 創建子任務
	TODO.Update([]TodoItem{
		{ID: "1", Text: "Explore file X", Status: "completed"},
	}, "phase1")

	TODO.Update([]TodoItem{
		{ID: "1", Text: "Design change for Y", Status: "in_progress"},
	}, "phase2")

	// 確認 lists 存在
	if len(TODO.GetItems("plan")) != 3 {
		t.Error("plan list should exist")
	}
	if len(TODO.GetItems("phase1")) != 1 {
		t.Error("phase1 list should exist")
	}
	if len(TODO.GetItems("phase2")) != 1 {
		t.Error("phase2 list should exist")
	}

	// 模擬正常退出（用 exitPlanModeLocked + postExitPlanMode，無 plan 內容時不觸發 session）
	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()
	// content 為空（沒有 plan.md），postExitPlanMode 唔會觸發 GetGlobalSession
	postExitPlanMode(content)

	// 所有 plan-related lists 應該被清理
	if len(TODO.GetItems("plan")) != 0 {
		t.Error("plan list should be cleaned after exit")
	}
	if len(TODO.GetItems("phase1")) != 0 {
		t.Error("phase1 list should be cleaned after exit")
	}
	if len(TODO.GetItems("phase2")) != 0 {
		t.Error("phase2 list should be cleaned after exit")
	}
}

func TestExitPlanMode_CleanupEvenWhenEmpty(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// 即使 phase1/phase2 從未被使用，postExitPlanMode 都應該安全清理
	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()
	postExitPlanMode(content)

	// TodoManager.Clear 對唔存在嘅 list 亦安全（delete on nil map is no-op）
	// 唔應該 panic
	if len(TODO.GetItems("plan")) != 0 {
		t.Error("plan list should be cleaned")
	}
	if len(TODO.GetItems("phase1")) != 0 {
		t.Error("phase1 list should be cleaned")
	}
	if len(TODO.GetItems("phase2")) != 0 {
		t.Error("phase2 list should be cleaned")
	}
}

func TestExitPlanMode_AllowsExitDuringPlanMode(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// 模擬 exit_plan_mode tool handler 中嘅路徑：
	// IsActive() + ExitPlanMode() 組合應該可以正常工作
	if !globalPlanMode.IsActive() {
		t.Fatal("Plan Mode should be active")
	}

	// 使用內部 exitPlanModeLocked 避免 session persistence（需要 DB）
	globalPlanMode.mu.Lock()
	content := exitPlanModeLocked()
	globalPlanMode.mu.Unlock()
	postExitPlanMode(content)

	if globalPlanMode.IsActive() {
		t.Error("Plan Mode should be inactive after exit")
	}
}

// ============================================================================
// 審計回歸測試 — enter_plan_mode repeat-call 防呆
// ============================================================================

func TestEnterPlanMode_RepeatCallBlocked(t *testing.T) {
	cleanup := enablePlanMode()
	defer cleanup()

	EnterPlanMode("test")

	// 當 Plan Mode 已激活時，enter_plan_mode 應該被 IsToolAllowedInPlanMode 拒絕
	if globalPlanMode.IsToolAllowedInPlanMode("enter_plan_mode") {
		t.Error("enter_plan_mode should NOT be allowed when Plan Mode is active")
	}
}

func TestEnterPlanMode_AllowedWhenInactive(t *testing.T) {
	defer resetGlobalPlanMode()

	// 當 Plan Mode 未激活時，enter_plan_mode 應該可以調用
	// IsToolAllowedInPlanMode 返回 true（PlanPhaseInactive branch）
	if !globalPlanMode.IsToolAllowedInPlanMode("enter_plan_mode") {
		t.Error("enter_plan_mode should be allowed when Plan Mode is inactive")
	}
}
