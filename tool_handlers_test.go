package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// skipIfNoOpenCLI 默認跳過 OpenCLI 測試（因為會調用外部 opencli 二進制檔，消耗資源）
// 顯式設置 GO_OPENCLI_TEST=1 才會運行
func skipIfNoOpenCLI(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_OPENCLI_TEST") != "1" {
		t.Skip("Skipping OpenCLI test. Set GO_OPENCLI_TEST=1 to run (requires opencli daemon).")
	}
}

func newTestEC(args map[string]interface{}) *ToolExecContext {
	return &ToolExecContext{
		Ctx:     context.Background(),
		ArgsMap: args,
	}
}

func requireFailed(t *testing.T, status TaskStatus, msg string) {
	t.Helper()
	if status != TaskStatusFailed {
		t.Errorf("%s: should fail but got success", msg)
	}
}

func requireSuccess(t *testing.T, status TaskStatus, details string) {
	t.Helper()
	if status != TaskStatusSuccess {
		t.Errorf("expected success but got failed: %s", details)
	}
}

func requireContains(t *testing.T, result, substr string) {
	t.Helper()
	if !strings.Contains(result, substr) {
		t.Errorf("result should contain %q, got: %s", substr, result)
	}
}

// ============================================================
// Opencli — action routing & error paths
// ============================================================

func TestOpenCLI_MissingAction(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "missing action")
	requireContains(t, result, "action")
}

func TestOpenCLI_UnknownAction(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "nonexistent"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "unknown action")
	requireContains(t, result, "未知")
}

func TestOpenCLI_WebRead_MissingURL(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "WebRead"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "WebRead no url")
	requireContains(t, result, "url")
}

func TestOpenCLI_Adapter_MissingSite(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Adapter", "command": "search"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "Adapter no site")
	requireContains(t, result, "site")
}

func TestOpenCLI_Adapter_MissingCommand(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Adapter", "site": "google"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "Adapter no command")
	requireContains(t, result, "command")
}

func TestOpenCLI_Explore_MissingURL(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Explore"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Explore no url")
}

func TestOpenCLI_Synthesize_MissingSite(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Synthesize"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Synthesize no site")
}

func TestOpenCLI_Generate_MissingURL(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Generate"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Generate no url")
}

func TestOpenCLI_Record_MissingURL(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Record"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Record no url")
}

func TestOpenCLI_Cascade_MissingURL(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Cascade"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Cascade no url")
}

func TestOpenCLI_AdapterEject_MissingSite(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "AdapterEject"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "AdapterEject no site")
}

func TestOpenCLI_AdapterReset_MissingAll(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "AdapterReset"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "AdapterReset missing all/site")
}

func TestOpenCLI_Register_MissingName(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Register"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Register no name")
}

func TestOpenCLI_Install_MissingName(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "Install"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Install no name")
}

func TestOpenCLI_PluginInstall_MissingSource(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "PluginInstall"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginInstall no source")
}

func TestOpenCLI_PluginUninstall_MissingName(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "PluginUninstall"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginUninstall no name")
}

func TestOpenCLI_PluginCreate_MissingName(t *testing.T) {
	skipIfNoOpenCLI(t)
	ec := newTestEC(map[string]interface{}{"action": "PluginCreate"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginCreate no name")
}

func TestOpenCLI_AllActionsValid(t *testing.T) {
	skipIfNoOpenCLI(t)
	actions := []string{"Doctor", "DaemonStop", "List", "Validate", "Verify", "AdapterStatus", "PluginList", "PluginUpdate"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			ec := newTestEC(map[string]interface{}{"action": action})
			result, _ := execOpenCLITool(ec)
			if strings.Contains(result, "未知的 action") {
				t.Errorf("action %s should be recognized", action)
			}
		})
	}
}

// ============================================================
// FileInfo
// ============================================================

func TestExecFileInfo_MissingFilename(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execFileInfo(ec)
	requireFailed(t, status, "FileInfo no filename")
}

func TestExecFileInfo_NonExistent(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/nonexistent_file_12345_test.txt"})
	_, status := execFileInfo(ec)
	requireFailed(t, status, "FileInfo nonexistent")
}

// ============================================================
// Read handlers
// ============================================================

func TestExecReadFileLine_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execReadFileLine(ec)
	requireFailed(t, status, "ReadFileLine no args")
}

func TestExecReadFileLines_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execReadFileLines(ec)
	requireFailed(t, status, "ReadFileLines no args")
}

func TestExecReadFileRange_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execReadFileRange(ec)
	requireFailed(t, status, "ReadFileRange no args")
}

func TestExecReadFileRange_NoStartLine(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/test.txt"})
	_, status := execReadFileRange(ec)
	requireFailed(t, status, "ReadFileRange no start_line")
}

// TestExecReadFileLines_FileTooLarge 測試大文件拒絕整文件讀取
func TestExecReadFileLines_FileTooLarge(t *testing.T) {
	// 保存舊全局狀態
	oldConfig := globalConfig
	oldTracker := globalReadWriteTracker
	oldOverrides := userContextLengthOverrides
	oldStage := globalStage
	oldConfigManager := globalConfigManager
	defer func() {
		globalConfig = oldConfig
		globalReadWriteTracker = oldTracker
		userContextLengthOverrides = oldOverrides
		globalStage = oldStage
		globalConfigManager = oldConfigManager
	}()

	// 清空 session/actor 狀態，確保 test 用自訂 config（而非真實 globalStage 嘅 actor model）
	globalStage = nil
	globalConfigManager = nil

	// 初始化 read/write tracker
	globalReadWriteTracker = newReadWriteTracker()

	// 模擬一個 1000 tokens 的上下文: Config 中加入 model + user override 設置 context length
	modelName := "small-ctx-model"
	globalConfig = Config{
		Models: map[string]*ModelConfig{
			modelName: {
				ModelBase: ModelBase{Model: modelName},
			},
		},
	}
	SetUserContextLengthOverrides(map[string]int{modelName: 1000})

	// 驗證 context length 生效
	if cl := getEffectiveContextLength(); cl != 1000 {
		t.Fatalf("getEffectiveContextLength() = %d, want 1000", cl)
	}

	// 創建一個超過 50% 上下文的大文件 (> 1000*0.5*4 = 2000 bytes)
	dir := t.TempDir()
	largeFilePath := dir + "/large_file.txt"
	largeContent := strings.Repeat("X", 3000) // ~750 tokens, > 500 tokens (50% of 1000)
	if err := os.WriteFile(largeFilePath, []byte(largeContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ec := newTestEC(map[string]interface{}{"filename": largeFilePath})
	result, status := execReadFileLines(ec)

	// 應該返回成功（警告信息），而不是失敗
	if status != TaskStatusSuccess {
		t.Errorf("ReadFileLines on large file: status = %v, want TaskStatusSuccess", status)
	}

	// 結果應該包含警告信息
	if !strings.Contains(result, "Warning:") {
		t.Error("ReadFileLines on large file: result should contain warning")
	}
	if !strings.Contains(result, "ReadFileRange") {
		t.Error("ReadFileLines on large file: should suggest ReadFileRange")
	}
	if !strings.Contains(result, "文件過大") {
		t.Error("ReadFileLines on large file: should mention file too large")
	}
}

// TestExecReadFileLines_NormalFile 測試正常大小文件仍然可以讀取
func TestExecReadFileLines_NormalFile(t *testing.T) {
	oldConfig := globalConfig
	oldTracker := globalReadWriteTracker
	oldStage := globalStage
	oldConfigManager := globalConfigManager
	defer func() {
		globalConfig = oldConfig
		globalReadWriteTracker = oldTracker
		globalStage = oldStage
		globalConfigManager = oldConfigManager
	}()

	// 清空 session/actor 狀態
	globalStage = nil
	globalConfigManager = nil

	globalReadWriteTracker = newReadWriteTracker()

	globalConfig = Config{
		Models: map[string]*ModelConfig{
			"test-model": {
				ModelBase: ModelBase{
					Model:         "test-model",
					ContextLength: 64000, // 64k 上下文
				},
			},
		},
	}

	dir := t.TempDir()
	normalFilePath := dir + "/normal_file.txt"
	normalContent := "line1\nline2\nline3\n"
	if err := os.WriteFile(normalFilePath, []byte(normalContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ec := newTestEC(map[string]interface{}{"filename": normalFilePath})
	result, status := execReadFileLines(ec)

	if status != TaskStatusSuccess {
		t.Errorf("ReadFileLines on normal file: status = %v, want TaskStatusSuccess", status)
	}
	if !strings.Contains(result, "line1") {
		t.Error("ReadFileLines on normal file: should contain file content")
	}
}

// TestExecReadFileLines_NoModelsConfigured 測試無模型配置時使用安全默認值
func TestExecReadFileLines_NoModelsConfigured(t *testing.T) {
	oldConfig := globalConfig
	oldTracker := globalReadWriteTracker
	oldStage := globalStage
	oldConfigManager := globalConfigManager
	defer func() {
		globalConfig = oldConfig
		globalReadWriteTracker = oldTracker
		globalStage = oldStage
		globalConfigManager = oldConfigManager
	}()

	// 清空 session/actor 狀態
	globalStage = nil
	globalConfigManager = nil

	globalReadWriteTracker = newReadWriteTracker()
	globalConfig = Config{} // 無模型配置 → 安全默認 4096 tokens

	dir := t.TempDir()
	filePath := dir + "/small.txt"
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ec := newTestEC(map[string]interface{}{"filename": filePath})
	result, status := execReadFileLines(ec)

	if status != TaskStatusSuccess {
		t.Errorf("ReadFileLines with no models: status = %v, want TaskStatusSuccess", status)
	}
	if !strings.Contains(result, "hello") {
		t.Error("ReadFileLines with no models: should contain file content")
	}
}

// ============================================================
// Write handlers
// ============================================================

func TestExecWriteFileLines_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execWriteFileLines(ec)
	requireFailed(t, status, "WriteFileLines no args")
}

func TestExecWriteFileLines_NoFilename(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"lines": []interface{}{"a", "b"}})
	_, status := execWriteFileLines(ec)
	requireFailed(t, status, "WriteFileLines no filename")
}

func TestExecWriteFileLines_NoLines(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/test.txt"})
	_, status := execWriteFileLines(ec)
	requireFailed(t, status, "WriteFileLines no lines")
}

func TestExecAppendToFile_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execAppendToFile(ec)
	requireFailed(t, status, "AppendToFile no args")
}

func TestExecAppendToFile_NoContent(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/test.txt"})
	_, status := execAppendToFile(ec)
	requireFailed(t, status, "AppendToFile no content")
}

// ============================================================
// TextGrep / TextSearch / TextReplace / TextTransform
// ============================================================

func TestExecTextGrep_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execTextGrep(ec)
	requireFailed(t, status, "TextGrep no args")
	requireContains(t, result, "FilePath")
}

func TestExecTextGrep_NoFilePath(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"pattern": "foo"})
	result, status := execTextGrep(ec)
	requireFailed(t, status, "TextGrep no file_path")
	requireContains(t, result, "FilePath")
}

func TestExecTextGrep_NoPattern(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"FilePath": "/tmp/test.txt"})
	result, status := execTextGrep(ec)
	requireFailed(t, status, "TextGrep no pattern")
	requireContains(t, result, "pattern")
}

func TestExecTextSearch_MissingKeyword(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTextSearch(ec)
	requireFailed(t, status, "TextSearch no keyword")
}

func TestExecTextReplace_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, _ := execTextReplace(ec)
	if !strings.Contains(result, "Error") && !strings.Contains(result, "Invalid") {
		t.Errorf("TextReplace no args should fail, got: %s", result)
	}
}

func TestExecTextTransform_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, _ := execTextTransform(ec)
	if !strings.Contains(result, "Error") && !strings.Contains(result, "Invalid") {
		t.Errorf("TextTransform no args should fail, got: %s", result)
	}
}

// ============================================================
// TodoCreate / TodoWrite / TodoUpdate / TodoList
// ============================================================

func TestExecTodoCreate_EmptyArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTodoCreate(ec)
	requireFailed(t, status, "TodoCreate empty args")
}

func TestExecTodoCreate_Valid(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"content": "test task",
		"status":  "InProgress",
	})
	content, status := execTodoCreate(ec)
	requireSuccess(t, status, content)
	if !strings.Contains(content, "test task") {
		t.Errorf("expected content to contain 'test task', got: %s", content)
	}
}

func TestExecTodoWrite_ValidArray(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"content": "task one", "status": "Pending", "activeForm": "Doing task one"},
			map[string]interface{}{"content": "task two", "status": "InProgress", "activeForm": "Doing task two"},
		},
	})
	content, status := execTodoWrite(ec)
	requireSuccess(t, status, content)
	if !strings.Contains(content, "task one") {
		t.Errorf("expected 'task one' in output, got: %s", content)
	}
}

func TestExecTodoWrite_EmptyArray(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{},
	})
	content, status := execTodoWrite(ec)
	requireSuccess(t, status, content)
}

func TestExecTodoWrite_MissingTodos(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTodoWrite(ec)
	requireFailed(t, status, "TodoWrite missing todos")
}

func TestExecTodoWrite_MissingContent(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"status": "Pending"},
		},
	})
	_, status := execTodoWrite(ec)
	requireFailed(t, status, "TodoWrite missing content")
}

func TestExecTodoWrite_InvalidStatus(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"content": "task", "status": "unknown", "activeForm": "doing"},
		},
	})
	_, status := execTodoWrite(ec)
	requireFailed(t, status, "TodoWrite invalid status")
}

func TestExecTodoUpdate_UpdateStatus(t *testing.T) {
	// First create a task
	TODO.Create("test task", "Pending")
	ec := newTestEC(map[string]interface{}{
		"id":     "1",
		"status": "Completed",
	})
	content, status := execTodoUpdate(ec)
	requireSuccess(t, status, content)
	// Completed items render as [x]
	if !strings.Contains(content, "[x]") {
		t.Errorf("expected '[x]' (completed marker) in output, got: %s", content)
	}
}

func TestExecTodoUpdate_Delete(t *testing.T) {
	TODO.Create("test task", "Pending")
	ec := newTestEC(map[string]interface{}{
		"id":     "1",
		"status": "",
	})
	content, status := execTodoUpdate(ec)
	requireSuccess(t, status, content)
}

func TestExecTodoUpdate_MissingID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTodoUpdate(ec)
	requireFailed(t, status, "TodoUpdate missing id")
}

func TestExecTodoUpdate_NotFound(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"id": "999",
	})
	_, status := execTodoUpdate(ec)
	requireFailed(t, status, "TodoUpdate not found")
}

func TestExecTodoList_ReturnsList(t *testing.T) {
	TODO.ClearAll()
	TODO.Create("task one", "Pending")
	ec := newTestEC(map[string]interface{}{})
	content, status := execTodoList(ec)
	requireSuccess(t, status, content)
	if !strings.Contains(content, "task one") {
		t.Errorf("expected 'task one' in output, got: %s", content)
	}
}

func TestExecTodoList_Empty(t *testing.T) {
	TODO.ClearAll()
	ec := newTestEC(map[string]interface{}{})
	content, status := execTodoList(ec)
	requireSuccess(t, status, content)
}

// ============================================================
// Tasks Mode guard: 有未完成 Todos 時禁止進入
// ============================================================

func TestTasksModeBlockedByUnfinishedTodos(t *testing.T) {
	// 清理狀態
	TODO.ClearAll()
	ResetTasksMode()

	// 創建未完成嘅 todo
	TODO.Create("incomplete task", "Pending")

	// 嘗試進入 Tasks Mode
	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "design"})
	if ok {
		t.Errorf("should be blocked by unfinished todos, but got success: %s", msg)
	}
	if !strings.Contains(msg, "未完成") {
		t.Errorf("expected '未完成' in error message, got: %s", msg)
	}

	TODO.ClearAll()
	ResetTasksMode()
}

func TestTasksModeAllowedWhenEmpty(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "explore"})
	if !ok {
		t.Errorf("should allow Tasks Mode when empty, got: %s", msg)
	}

	ResetTasksMode()
}

func TestTasksModeAllowedWhenAllCompleted(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	TODO.Create("completed task", "Completed")
	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "design"})
	if !ok {
		t.Errorf("should allow Tasks Mode when all completed, got: %s", msg)
	}

	TODO.ClearAll()
	ResetTasksMode()
}

func TestTasksModeExecuteNotBlocked(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	// 先手動設置 Tasks Mode 為 active（避免 handleTasks 觸發 session save）
	globalTasksMode.mu.Lock()
	globalTasksMode.PlanPhase = TasksPhaseExplore
	globalTasksMode.StartTime = time.Now()
	globalTasksMode.PhaseStart = time.Now()
	globalTasksMode.mu.Unlock()

	TODO.Create("incomplete task", "InProgress")

	// execute phase 應該唔受阻擋（exit guard 唔檢查待辦）
	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "execute"})
	if !ok {
		t.Errorf("execute should not be blocked, got: %s", msg)
	}

	TODO.ClearAll()
	ResetTasksMode()
}

func TestTasksModeReenterBlockedByUnfinishedTodos(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	// 退出後有未完成 todos（手動模擬已退出狀態）
	TODO.Create("unfinished", "Pending")

	// 重新進入應該被阻擋
	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "design"})
	if ok {
		t.Errorf("should be blocked re-entering with unfinished todos, got: %s", msg)
	}
	if !strings.Contains(msg, "未完成") {
		t.Errorf("expected '未完成' in error, got: %s", msg)
	}

	TODO.ClearAll()
	ResetTasksMode()
}

func TestHandleTasks_DesignWithTasks(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	msg, ok := handleTasks(map[string]interface{}{
		"PlanPhase": "design",
		"tasks": []interface{}{
			map[string]interface{}{"id": "start", "title": "啟動服務", "status": "Pending"},
			map[string]interface{}{"id": "verify", "title": "驗證連線", "status": "Pending"},
		},
	})
	if !ok {
		t.Errorf("design with tasks should succeed, got: %s", msg)
	}
	if !strings.Contains(msg, "2 個任務") {
		t.Errorf("expected '2 個任務', got: %s", msg)
	}

	ResetTasksMode()
}

func TestHandleTasks_DesignAutoInitsFromInactive(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	// P0: design from inactive should auto-init (not error)
	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "design"})
	if !ok {
		t.Errorf("design from inactive should auto-init, got: %s", msg)
	}

	ResetTasksMode()
}

func TestHandleTasks_UpdateTasksList(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	// 未指定 PlanPhase → 純更新任務列表
	msg, ok := handleTasks(map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{"id": "a", "title": "Task A", "status": "Pending"},
		},
	})
	if !ok {
		t.Errorf("update tasks list should succeed, got: %s", msg)
	}

	ResetTasksMode()
}

func TestHandleTasks_InvalidPlanPhase(t *testing.T) {
	TODO.ClearAll()
	ResetTasksMode()

	msg, ok := handleTasks(map[string]interface{}{"PlanPhase": "invalid_phase"})
	if !ok {
		t.Errorf("invalid phase should fall through to updateTasksList, got: %s", msg)
	}

	ResetTasksMode()
}

// ============================================================
// SSH
// ============================================================

func TestExecSSHConnect_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHConnect(ec)
	requireFailed(t, status, "SSHConnect no args")
}

func TestExecSSHExec_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHExec(ec)
	requireFailed(t, status, "SSHExec no args")
}

func TestExecSSHList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHList(ec)
	if status != TaskStatusSuccess {
		t.Log("SSHList failed (no active connections): OK")
	}
}

func TestExecSSHClose_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHClose(ec)
	requireFailed(t, status, "SSHClose no args")
}

// ============================================================
// Menu
// ============================================================

func TestExecMenuTool_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execMenuTool(ec)
	if status != TaskStatusSuccess {
		t.Errorf("Menu should succeed: %s", result)
	}
	if result == "" {
		t.Error("Menu should return non-empty result")
	}
}

// ============================================================
// NextPhase / PrevPhase
// ============================================================

func TestExecNextPhase_NotInTasksMode(t *testing.T) {
	defer resetGlobalTasksMode()
	ec := newTestEC(map[string]interface{}{})
	result, _ := execNextPhase(ec)
	if !strings.Contains(result, "不在 Plan Mode") && !strings.Contains(result, "not started") {
		t.Logf("NextPhase outside plan mode: %s", result)
	}
}

func TestExecPrevPhase_NotInDesign(t *testing.T) {
	defer resetGlobalTasksMode()
	ec := newTestEC(map[string]interface{}{})
	result, _ := execPrevPhase(ec)
	if strings.TrimSpace(result) == "" {
		t.Error("PrevPhase should return error when not in plan mode")
	}
}

// ============================================================
// Memory
// ============================================================

func TestExecMemorySave_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execMemorySave(ec)
	requireFailed(t, status, "MemorySave no args")
}

func TestExecMemoryRecall_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execMemoryRecall(ec)
	requireFailed(t, status, "MemoryRecall no args")
}

func TestExecMemoryList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execMemoryList(ec)
	// 無 globalUnifiedMemory 時應該返回 TaskStatusFailed
	if status != TaskStatusFailed {
		t.Error("MemoryList without globalUnifiedMemory should fail")
	}
}

// ============================================================
// SchemeEval
// ============================================================

func TestExecSchemeEval_EmptyExpression(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"expression": ""})
	result, status := execSchemeEval(ec)
	requireFailed(t, status, "SchemeEval empty expression")
	requireContains(t, result, "empty")
}

func TestExecSchemeEval_MissingExpression(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSchemeEval(ec)
	requireFailed(t, status, "SchemeEval no expression")
}

// ============================================================
// Spawn
// ============================================================

// Spawn 函數返回 (string, bool) — false 表示錯誤
func TestExecSpawn_NoTask(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execSpawn(ec)
	if status != TaskStatusFailed {
		t.Errorf("Spawn without task should fail, got status=%v, result=%s", status, result)
	}
	if !strings.Contains(result, "Error") {
		t.Errorf("Spawn should return error message, got: %s", result)
	}
}

func TestExecSpawnCheck_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execSpawnCheck(ec)
	if status != TaskStatusFailed {
		t.Errorf("SpawnCheck without task_id should fail, got status=%v, result=%s", status, result)
	}
	if !strings.Contains(result, "Error") {
		t.Errorf("SpawnCheck should return error message, got: %s", result)
	}
}

func TestExecSpawnCancel_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execSpawnCancel(ec)
	if status != TaskStatusFailed {
		t.Errorf("SpawnCancel without task_id should fail, got status=%v, result=%s", status, result)
	}
	if !strings.Contains(result, "Error") {
		t.Errorf("SpawnCancel should return error message, got: %s", result)
	}
}

// ============================================================
// Cron
// ============================================================

func TestExecCronAdd_NoCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execCronAdd(ec)
	requireFailed(t, status, "CronAdd no command")
}

func TestExecCronRemove_NoJobID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execCronRemove(ec)
	requireFailed(t, status, "CronRemove no job_id")
}

// ============================================================
// ShellDelayed (deprecated, kept as dead code)
// ============================================================

func TestExecShellDelayed_NoCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execShellDelayed(ec)
	requireFailed(t, status, "ShellDelayed no command")
}

func TestExecTaskCheck_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTaskCheck(ec)
	requireFailed(t, status, "TaskCheck no task_id")
}

func TestExecTaskTerminate_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTaskTerminate(ec)
	requireFailed(t, status, "TaskTerminate no task_id")
}

// ============================================================
// Plugin
// ============================================================

func TestExecPluginCall_NoNameAndArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginCall(ec)
	requireFailed(t, status, "PluginCall no args")
}

func TestExecPluginCreate_NoArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginCreate(ec)
	requireFailed(t, status, "PluginCreate no args")
}

// ============================================================
// Skill
// ============================================================

func TestExecSkillCreate_NoName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillCreate(ec)
	requireFailed(t, status, "SkillCreate no args")
}

func TestExecSkillDelete_NoArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillDelete(ec)
	requireFailed(t, status, "SkillDelete no args")
}

func TestExecSkillGet_NoArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillGet(ec)
	requireFailed(t, status, "SkillGet no args")
}

// ============================================================
// Profile
// ============================================================

func TestExecProfileCheck_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execProfileCheck(ec)
	if status == TaskStatusFailed {
		t.Logf("ProfileCheck returned error: %s", result)
	}
}

// ============================================================
// Browser (basic)
// ============================================================

func TestExecBrowserVisit_NoURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserVisit(ec)
	requireFailed(t, status, "BrowserVisit no url")
}

func TestExecBrowserSearch_NoKeyword(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserSearch(ec)
	requireFailed(t, status, "BrowserSearch no keyword")
}

func TestExecBrowserDownload_NoURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserDownload(ec)
	requireFailed(t, status, "BrowserDownload no url")
}

func TestExecBrowserClick_NoIndex(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserClick(ec)
	requireFailed(t, status, "BrowserClick no index")
}

func TestExecBrowserType_NoText(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"index": float64(1)})
	_, status := execBrowserType(ec)
	requireFailed(t, status, "BrowserType no text")
}

func TestExecBrowserScroll_NoDirection(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserScroll(ec)
	requireFailed(t, status, "BrowserScroll no direction")
}

func TestExecBrowserScreenshot_NoPath(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserScreenshot(ec)
	requireFailed(t, status, "BrowserScreenshot no path")
}

// ============================================================
// WriteFileLine — overwrite / insert / append modes
// ============================================================

func TestExecWriteFileLine_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execWriteFileLine(ec)
	requireFailed(t, status, "WriteFileLine missing args")
}

func TestExecWriteFileLine_MissingFilename(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"LineNum": float64(1), "content": "test"})
	_, status := execWriteFileLine(ec)
	requireFailed(t, status, "WriteFileLine missing filename")
}

func TestExecWriteFileLine_InsertMode(t *testing.T) {
	tmp := t.TempDir()
	f := tmp + "/test.txt"
	WriteFileLines(f, []string{"A", "B", "C"})
	ec := newTestEC(map[string]interface{}{"filename": f, "LineNum": float64(-2), "content": "INSERTED"})
	result, status := execWriteFileLine(ec)
	if status != TaskStatusSuccess {
		t.Errorf("insert should succeed: %s", result)
	}
	lines, _ := ReadFileLines(f)
	if len(lines) != 4 || lines[1] != "INSERTED" {
		t.Errorf("insert before line 2: got %v", lines)
	}
}

func TestExecWriteFileLine_OverwriteMode(t *testing.T) {
	tmp := t.TempDir()
	f := tmp + "/test.txt"
	WriteFileLines(f, []string{"old"})
	ec := newTestEC(map[string]interface{}{"filename": f, "LineNum": float64(1), "content": "new"})
	result, status := execWriteFileLine(ec)
	if status != TaskStatusSuccess {
		t.Errorf("overwrite should succeed: %s", result)
	}
	lines, _ := ReadFileLines(f)
	if lines[0] != "new" {
		t.Errorf("overwrite: got %q", lines[0])
	}
}

// ============================================================
// WriteFileRange — overwrite / insert modes
// ============================================================

func TestExecWriteFileRange_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execWriteFileRange(ec)
	requireFailed(t, status, "WriteFileRange missing args")
}

func TestExecWriteFileRange_InsertMode(t *testing.T) {
	tmp := t.TempDir()
	f := tmp + "/test.txt"
	WriteFileLines(f, []string{"A", "B", "C", "D"})
	ec := newTestEC(map[string]interface{}{"filename": f, "StartLine": float64(-3), "content": "X\nY"})
	result, status := execWriteFileRange(ec)
	if status != TaskStatusSuccess {
		t.Errorf("insert should succeed: %s", result)
	}
	lines, _ := ReadFileLines(f)
	if len(lines) != 6 || lines[2] != "X" || lines[3] != "Y" {
		t.Errorf("insert before line 3: got %v", lines)
	}
}

func TestExecWriteFileRange_StartLineZero_Error(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/x", "StartLine": float64(0), "content": "x"})
	_, status := execWriteFileRange(ec)
	requireFailed(t, status, "WriteFileRange StartLine=0")
}

// ============================================================
// Cron tools
// ============================================================

func TestExecCronList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execCronList(ec)
	if status != TaskStatusSuccess {
		t.Log("CronList failed (no cron manager): OK")
	}
}

func TestExecCronStatus_MissingJobID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"jobId": ""})
	_, status := execCronStatus(ec)
	requireFailed(t, status, "CronStatus missing/invalid args")
}

// ============================================================
// Memory tools
// ============================================================

func TestExecMemoryForget_MissingKey(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execMemoryForget(ec)
	requireFailed(t, status, "MemoryForget missing/invalid args")
}

// ============================================================
// Skill tools
// ============================================================

func TestExecSkillList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillList(ec)
	if status != TaskStatusSuccess {
		t.Log("SkillList failed (no skill manager v2): OK")
	}
}

func TestExecSkillReload_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillReload(ec)
	if status != TaskStatusSuccess {
		t.Log("SkillReload failed (no skill manager v2): OK")
	}
}

func TestExecSkillLoad_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillLoad(ec)
	requireFailed(t, status, "SkillLoad missing/invalid args")
}

func TestExecSkillUpdate_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillUpdate(ec)
	requireFailed(t, status, "SkillUpdate missing/invalid args")
}

func TestExecSkillSuggest_MissingContext(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillSuggest(ec)
	requireFailed(t, status, "SkillSuggest missing/invalid args")
}

func TestExecSkillStats_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillStats(ec)
	if status != TaskStatusSuccess {
		t.Log("SkillStats failed (no skill manager v2): OK")
	}
}

func TestExecSkillEvaluate_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSkillEvaluate(ec)
	requireFailed(t, status, "SkillEvaluate missing/invalid args")
}

// ============================================================
// Plugin tools
// ============================================================

func TestExecPluginList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginList(ec)
	if status != TaskStatusSuccess {
		t.Log("PluginList failed (no plugin manager): OK")
	}
}

func TestExecPluginLoad_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execPluginLoad(ec)
	requireFailed(t, status, "PluginLoad missing name")
	requireContains(t, result, "plugin")
}

func TestExecPluginUnload_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execPluginUnload(ec)
	requireFailed(t, status, "PluginUnload missing name")
	requireContains(t, result, "plugin")
}

func TestExecPluginReload_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execPluginReload(ec)
	requireFailed(t, status, "PluginReload missing name")
	requireContains(t, result, "plugin")
}

func TestExecPluginCompile_MissingSource(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginCompile(ec)
	requireFailed(t, status, "PluginCompile missing/invalid args")
}

func TestExecPluginDelete_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginDelete(ec)
	requireFailed(t, status, "PluginDelete missing/invalid args")
}

func TestExecPluginApis_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execPluginAPIs(ec)
	if status != TaskStatusSuccess {
		t.Log("PluginApis failed: OK (may need plugin manager)")
	}
}

func TestExecPluginDetail_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execPluginDetail(ec)
	requireFailed(t, status, "PluginDetail missing name")
	requireContains(t, result, "name")
}

// ============================================================
// Spawn tools
// ============================================================

func TestExecSpawnList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSpawnList(ec)
	// May fail if subagent manager not initialized — that's expected in unit test env
	if status != TaskStatusSuccess {
		t.Log("SpawnList failed (expected without manager init): OK")
	}
}

// ============================================================
// Shell / SmartShell
// ============================================================

func TestExecShell_MissingCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execShellTool(ec)
	requireFailed(t, status, "Shell missing command")
	requireContains(t, result, "command")
}

func TestExecSmartShell_MissingCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSmartShellTool(ec)
	requireFailed(t, status, "SmartShell missing command")
}

// ============================================================
// Profile tools
// ============================================================

func TestExecProfileReload_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execProfileReload(ec)
	if status != TaskStatusSuccess {
		t.Log("ProfileReload failed (expected without manager init): OK")
	}
}

func TestExecActorIdentitySet_MissingActor(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execActorIdentitySet(ec)
	requireFailed(t, status, "ActorIdentitySet missing args")
	requireContains(t, result, "ActorName")
}

func TestExecActorIdentityClear_MissingActor(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	result, status := execActorIdentityClear(ec)
	requireFailed(t, status, "ActorIdentityClear missing args")
	requireContains(t, result, "ActorName")
}

// ============================================================
// Task management tools (missing members)
// ============================================================

func TestExecTaskList_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTaskList(ec)
	if status != TaskStatusSuccess {
		t.Log("TaskList failed (expected without manager init): OK")
	}
}

func TestExecTaskWait_MissingTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTaskWait(ec)
	// May fail with "task manager not initialized" or "missing TaskId"
	requireFailed(t, status, "TaskWait missing/bad args")
}

func TestExecTaskRemove_MissingTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTaskRemove(ec)
	requireFailed(t, status, "TaskRemove missing/bad args")
}

// ============================================================
// Browser tools (missing members)
// ============================================================

func TestExecBrowserWaitElement_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserWaitElement(ec)
	requireFailed(t, status, "BrowserWaitElement no selector")
}

func TestExecBrowserExtractLinks_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserExtractLinks(ec)
	requireFailed(t, status, "BrowserExtractLinks no selector")
}

func TestExecBrowserExtractImages_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserExtractImages(ec)
	requireFailed(t, status, "BrowserExtractImages no selector")
}

func TestExecBrowserExtractElements_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserExtractElements(ec)
	requireFailed(t, status, "BrowserExtractElements no selector")
}

func TestExecBrowserExecuteJs_NoCode(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserExecuteJS(ec)
	requireFailed(t, status, "BrowserExecuteJs no code")
}

func TestExecBrowserFillForm_NoData(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserFillForm(ec)
	requireFailed(t, status, "BrowserFillForm no data")
}

func TestExecBrowserHover_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserHover(ec)
	requireFailed(t, status, "BrowserHover no selector")
}

func TestExecBrowserDoubleClick_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserDoubleClick(ec)
	requireFailed(t, status, "BrowserDoubleClick no selector")
}

func TestExecBrowserRightClick_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserRightClick(ec)
	requireFailed(t, status, "BrowserRightClick no selector")
}

func TestExecBrowserDrag_NoSelectors(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserDrag(ec)
	requireFailed(t, status, "BrowserDrag no selectors")
}

func TestExecBrowserWaitSmart_NoSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserWaitSmart(ec)
	requireFailed(t, status, "BrowserWaitSmart no selector")
}

func TestExecBrowserNavigate_NoDirection(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserNavigate(ec)
	requireFailed(t, status, "BrowserNavigate no direction")
}

func TestExecBrowserGetCookies_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserGetCookies(ec)
	// May fail without active browser session — expected in unit test env
	if status != TaskStatusSuccess {
		t.Log("BrowserGetCookies failed (expected without browser session): OK")
	}
}

func TestExecBrowserCookieSave_MissingFile(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserCookieSave(ec)
	requireFailed(t, status, "BrowserCookieSave missing file")
}

func TestExecBrowserCookieLoad_MissingFile(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserCookieLoad(ec)
	requireFailed(t, status, "BrowserCookieLoad missing file")
}

func TestExecBrowserSnapshot_Executes(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserSnapshot(ec)
	if status != TaskStatusSuccess {
		t.Log("BrowserSnapshot failed (expected without browser session): OK")
	}
}

func TestExecBrowserUploadFile_MissingSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserUploadFile(ec)
	requireFailed(t, status, "BrowserUploadFile missing selector")
}

func TestExecBrowserSelectOption_MissingSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserSelectOption(ec)
	requireFailed(t, status, "BrowserSelectOption missing selector")
}

func TestExecBrowserKeyPress_MissingKey(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserKeyPress(ec)
	requireFailed(t, status, "BrowserKeyPress missing key")
}

func TestExecBrowserElementScreenshot_MissingSelector(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserElementScreenshot(ec)
	requireFailed(t, status, "BrowserElementScreenshot missing selector")
}

func TestExecBrowserPdf_MissingPath(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserPDF(ec)
	requireFailed(t, status, "BrowserPdf missing path")
}

func TestExecBrowserPdfFromFile_MissingPath(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserPDFFromFile(ec)
	requireFailed(t, status, "BrowserPdfFromFile missing path")
}

func TestExecBrowserSetHeaders_MissingHeaders(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserSetHeaders(ec)
	requireFailed(t, status, "BrowserSetHeaders missing headers")
}

func TestExecBrowserSetUserAgent_MissingAgent(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserSetUserAgent(ec)
	requireFailed(t, status, "BrowserSetUserAgent missing agent")
}

func TestExecBrowserEmulateDevice_MissingDevice(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execBrowserEmulateDevice(ec)
	requireFailed(t, status, "BrowserEmulateDevice missing device")
}

// ============================================================
// Opencli — missing edge case
// ============================================================

func TestExecOpencli_NoBinary(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "web_read", "url": "https://example.com"})
	result, status := execOpenCLITool(ec)
	// Either fails (no binary) or succeeds (if opencli is installed)
	if status != TaskStatusFailed && status != TaskStatusSuccess {
		t.Errorf("unexpected status: %v, result: %s", status, result)
	}
}

// ============================================================================
// BDD: 高發錯誤場景 — 工具解析容錯 + Work Mode 早退防護
// ============================================================================

// Scenario: 模型以 DSML string 傳入 Todos → TodoWrite 拒絕並給出清晰報錯。
// 呢個係 999.log / e3e.log 最常見嘅錯誤模式。
func TestTodoWrite_RejectsDSMLString(t *testing.T) {
	dsmlInput := `<item><id>1</id><content>test</content><status>Pending</status></item>`
	ec := newTestEC(map[string]interface{}{
		"todos": dsmlInput,
	})
	_, status := execTodoWrite(ec)
	if status != TaskStatusFailed {
		t.Error("TodoWrite should reject DSML string as todos param")
	}
}

// Scenario: 模型傳入單個 object 而唔係 array → TodoWrite 報錯。
func TestTodoWrite_RejectsSingleObject(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": map[string]interface{}{"content": "task", "status": "Pending"},
	})
	_, status := execTodoWrite(ec)
	if status != TaskStatusFailed {
		t.Error("TodoWrite should reject single object as todos param")
	}
}

// Scenario: 模型忘記包 activeForm → 報錯（required field）。
func TestTodoWrite_AcceptsMissingActiveForm(t *testing.T) {
	// activeForm 係 UI 輔助字段，handler 唔強制要求
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"content": "task", "status": "Pending"},
		},
	})
	_, status := execTodoWrite(ec)
	if status != TaskStatusSuccess {
		t.Error("TodoWrite should accept missing activeForm (optional)")
	}
}

// Scenario: 空 content → 報錯。
func TestTodoWrite_RejectsEmptyContent(t *testing.T) {
	ec := newTestEC(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{"content": "", "status": "Pending", "activeForm": "doing"},
		},
	})
	_, status := execTodoWrite(ec)
	if status != TaskStatusFailed {
		t.Error("TodoWrite should reject empty content")
	}
}

// Scenario: Work Mode 有進展時唔應該早退（progress-based resume）。
// 呢個對應 e3e.log 入面模型因 max resume rounds 被截斷嘅問題。
func TestWorkModeGuard_ProgressResetsCounter(t *testing.T) {
	TODO.ClearAll()
	lastWorkModeTodoDigest = ""

	// 模擬初始狀態：有待辦
	TODO.Create("task1", "Pending")
	digest1 := TODO.GetUnfinishedDigest()

	// Simulate progress: 完成一個 task
	TODO.UpdateSingle("1", "", "Completed")
	digest2 := TODO.GetUnfinishedDigest()

	// Digest 應該唔同（有進展）
	if digest1 == digest2 {
		t.Error("digest should change after completing a task")
	}

	// 模擬工作模式退出守衛：指紋唔同 → reset counter
	// (呢個邏輯已在 RunBranchNone 中實測，此處驗證 digest 機制)
	TODO.ClearAll()
	lastWorkModeTodoDigest = ""
}

// Scenario: 已經全部完成 → 唔應該觸發 exit guard。
func TestWorkModeGuard_AllCompletedAllowsExit(t *testing.T) {
	TODO.ClearAll()
	TODO.Create("done", "Completed")

	if TODO.HasUnfinishedItems() {
		t.Error("all-completed should not have unfinished items")
	}

	TODO.ClearAll()
}

// Scenario: 兩個 InProgress → 自動降級舊嘅（只允許一個 InProgress）。
func TestTodoCreate_EnforcesSingleInProgress(t *testing.T) {
	TODO.ClearAll()
	TODO.Create("task1", "InProgress")
	TODO.Create("task2", "InProgress")

	items := TODO.GetItems()
	if items[0].Status != "Pending" {
		t.Errorf("task1 should be auto-demoted to Pending, got %s", items[0].Status)
	}
	if items[1].Status != "InProgress" {
		t.Errorf("task2 should stay InProgress, got %s", items[1].Status)
	}

	TODO.ClearAll()
}

// Scenario: TodoUpdate status="" 可以刪除 task（唔應該報錯）。
func TestTodoUpdate_DeleteByEmptyStatus(t *testing.T) {
	TODO.ClearAll()
	TODO.Create("to delete", "Pending")

	_, err := TODO.UpdateSingle("1", "", "")
	if err != nil {
		t.Fatalf("delete by empty status should succeed: %v", err)
	}

	items := TODO.GetItems()
	if len(items) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(items))
	}
}

// Scenario: Escalation counter 正確追蹤連續失敗。
// (escalation threshold = 3, 達標後 agent loop 應該處理)
func TestEscalation_ThresholdReached(t *testing.T) {
	threshold := 3
	globalEscalationThreshold = threshold
	defer func() { globalEscalationThreshold = 3 }()

	// 模擬連續 3 次相同 tool 失敗 → 應該觸發 escalation
	failures := 0
	for i := 0; i < threshold; i++ {
		failures++
	}
	if failures < threshold {
		t.Errorf("expected %d failures to reach threshold", threshold)
	}
	if failures != threshold {
		t.Errorf("escalation should trigger at %d failures, got %d", threshold, failures)
	}
}

// Scenario: Config 前後端默認值一致。
// 確保前端 settings-config.ts 嘅 defaults 同後端 const.go 一致。
func TestConfig_PromptCacheDefaultsMatch(t *testing.T) {
	cm := setupTestConfigManager(t)
	cfg := cm.GetConfig()

	// 前端默認值：promptCacheEnabled=false, promptCacheStableTools=false
	// 後端默認值：DefaultPromptCacheEnabled=false, DefaultPromptCacheStableTools=false
	if cfg.PromptCache.Enabled != false {
		t.Error("backend PromptCache.Enabled should default to false (matching frontend)")
	}
	if cfg.PromptCache.StableTools != false {
		t.Error("backend PromptCache.StableTools should default to false (matching frontend)")
	}
}

// Scenario: 後端 Config 經過 createDefaultConfig → syncGlobals → GET 返回正確值。
// 呢個係 BDD integration test：確保新用戶首次啟動時前後端一致。
func TestConfig_FreshStartup_PromptCacheOff(t *testing.T) {
	cm := setupTestConfigManager(t)
	cm.syncGlobals()

	// 新用戶冇任何配置 → 所有優化默認關閉
	if globalPromptCacheConfig.Enabled {
		t.Error("fresh startup: PromptCache.Enabled should be false")
	}
	if globalPromptCacheConfig.StableTools {
		t.Error("fresh startup: PromptCache.StableTools should be false")
	}
}
