package main

import (
	"context"
	"strings"
	"testing"
)

func newTestEC(args map[string]interface{}) *ToolExecContext {
	return &ToolExecContext{
		Ctx:     context.Background(),
		ArgsMap: args,
	}
}

func requireFailed(t *testing.T, status TaskStatus, msg string) {
	t.Helper()
	if status != TaskStatusFailed {
		t.Errorf("%s: should fail", msg)
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
	ec := newTestEC(map[string]interface{}{})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "missing action")
	requireContains(t, result, "action")
}

func TestOpenCLI_UnknownAction(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "nonexistent"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "unknown action")
	requireContains(t, result, "未知")
}

func TestOpenCLI_WebRead_MissingURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "WebRead"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "WebRead no url")
	requireContains(t, result, "url")
}

func TestOpenCLI_Adapter_MissingSite(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Adapter", "command": "search"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "Adapter no site")
	requireContains(t, result, "site")
}

func TestOpenCLI_Adapter_MissingCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Adapter", "site": "google"})
	result, status := execOpenCLITool(ec)
	requireFailed(t, status, "Adapter no command")
	requireContains(t, result, "command")
}

func TestOpenCLI_Explore_MissingURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Explore"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Explore no url")
}

func TestOpenCLI_Synthesize_MissingSite(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Synthesize"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Synthesize no site")
}

func TestOpenCLI_Generate_MissingURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Generate"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Generate no url")
}

func TestOpenCLI_Record_MissingURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Record"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Record no url")
}

func TestOpenCLI_Cascade_MissingURL(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Cascade"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Cascade no url")
}

func TestOpenCLI_AdapterEject_MissingSite(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "AdapterEject"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "AdapterEject no site")
}

func TestOpenCLI_AdapterReset_MissingAll(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "AdapterReset"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "AdapterReset missing all/site")
}

func TestOpenCLI_Register_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Register"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Register no name")
}

func TestOpenCLI_Install_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "Install"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "Install no name")
}

func TestOpenCLI_PluginInstall_MissingSource(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "PluginInstall"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginInstall no source")
}

func TestOpenCLI_PluginUninstall_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "PluginUninstall"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginUninstall no name")
}

func TestOpenCLI_PluginCreate_MissingName(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"action": "PluginCreate"})
	_, status := execOpenCLITool(ec)
	requireFailed(t, status, "PluginCreate no name")
}

func TestOpenCLI_AllActionsValid(t *testing.T) {
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

func TestExecReadAllLines_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execReadAllLines(ec)
	requireFailed(t, status, "ReadAllLines no args")
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

// ============================================================
// Write handlers
// ============================================================

func TestExecWriteAllLines_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execWriteAllLines(ec)
	requireFailed(t, status, "WriteAllLines no args")
}

func TestExecWriteAllLines_NoFilename(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"lines": []interface{}{"a", "b"}})
	_, status := execWriteAllLines(ec)
	requireFailed(t, status, "WriteAllLines no filename")
}

func TestExecWriteAllLines_NoLines(t *testing.T) {
	ec := newTestEC(map[string]interface{}{"filename": "/tmp/test.txt"})
	_, status := execWriteAllLines(ec)
	requireFailed(t, status, "WriteAllLines no lines")
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
// Todos
// ============================================================

func TestExecTodos_EmptyArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execTodos(ec)
	requireFailed(t, status, "Todos empty args")
}

// ============================================================
// SSH
// ============================================================

func TestExecSSHConnect_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHConnect(ec)
	requireFailed(t, status, "SshConnect no args")
}

func TestExecSSHExec_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHExec(ec)
	requireFailed(t, status, "SshExec no args")
}

func TestExecSSHClose_MissingArgs(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execSSHClose(ec)
	requireFailed(t, status, "SshClose no args")
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

func TestExecNextPhase_NotInPlanMode(t *testing.T) {
	defer resetGlobalPlanMode()
	ec := newTestEC(map[string]interface{}{})
	result, _ := execNextPhase(ec)
	if !strings.Contains(result, "不在 Plan Mode") && !strings.Contains(result, "not started") {
		t.Logf("NextPhase outside plan mode: %s", result)
	}
}

func TestExecPrevPhase_NotInPhase2(t *testing.T) {
	defer resetGlobalPlanMode()
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
// ShellDelayed
// ============================================================

func TestExecShellDelayed_NoCommand(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execShellDelayed(ec)
	requireFailed(t, status, "ShellDelayed no command")
}

func TestExecShellDelayedCheck_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execShellDelayedCheck(ec)
	requireFailed(t, status, "ShellDelayedCheck no task_id")
}

func TestExecShellDelayedTerminate_NoTaskID(t *testing.T) {
	ec := newTestEC(map[string]interface{}{})
	_, status := execShellDelayedTerminate(ec)
	requireFailed(t, status, "ShellDelayedTerminate no task_id")
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
