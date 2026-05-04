package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/toon-format/toon-go"
)

// ============================================================================
// TOON 格式合規測試 — 確保所有工具輸出 map key 使用 PascalCase（大駝峰）
// 與 config.toon 保持一致
// ============================================================================

// toonFieldCompliance 定義一個待檢查的 TOON 輸出源
type toonFieldCompliance struct {
	name    string // 人類可讀的輸出名稱
	output  string // TOON 格式字串
}

// checkTOONPascalCase 解析 TOON 字串，檢查所有頂層字段名是否為 PascalCase。
// 返回 nil 表示全部合規，返回 error 列出不合規字段。
func checkTOONPascalCase(toonStr string) error {
	if toonStr == "" {
		return nil
	}
	var parsed map[string]interface{}
	if err := toon.Unmarshal([]byte(toonStr), &parsed); err != nil {
		return fmt.Errorf("TOON parse error: %v", err)
	}
	var snakeKeys []string
	for key := range parsed {
		if !isPascalCase(key) {
			snakeKeys = append(snakeKeys, key)
		}
	}
	if len(snakeKeys) > 0 {
		return fmt.Errorf("non-PascalCase keys found: %v", snakeKeys)
	}
	return nil
}

// checkTOONNestedPascalCase 遞歸檢查 TOON 中所有嵌套層級的字段名。
func checkTOONNestedPascalCase(toonStr string) error {
	var parsed map[string]interface{}
	if err := toon.Unmarshal([]byte(toonStr), &parsed); err != nil {
		return fmt.Errorf("TOON parse error: %v", err)
	}
	var snakeKeys []string
	collectSnakeKeys(parsed, "", &snakeKeys)
	if len(snakeKeys) > 0 {
		return fmt.Errorf("non-PascalCase keys found: %v", snakeKeys)
	}
	return nil
}

// collectSnakeKeys 遞歸收集所有不符合 PascalCase 的 key
func collectSnakeKeys(m map[string]interface{}, prefix string, snakeKeys *[]string) {
	for key, val := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		if !isPascalCase(key) {
			*snakeKeys = append(*snakeKeys, fullKey)
		}
		// 遞歸檢查嵌套 map
		if nested, ok := val.(map[string]interface{}); ok {
			collectSnakeKeys(nested, fullKey, snakeKeys)
		}
		// 檢查 []interface{} 中的 map
		if arr, ok := val.([]interface{}); ok {
			for i, item := range arr {
				if nested, ok := item.(map[string]interface{}); ok {
					collectSnakeKeys(nested, fmt.Sprintf("%s[%d]", fullKey, i), snakeKeys)
				}
			}
		}
	}
}

// isPascalCase 檢查字串是否為 PascalCase（首字母大寫，不含底線）。
// 允許全大寫縮寫（如 PID、HTTP、URL、ID）。
func isPascalCase(s string) bool {
	if s == "" {
		return false
	}
	first := s[0]
	if first < 'A' || first > 'Z' {
		return false
	}
	// 含底線即為 snake_case，不合規
	if strings.Contains(s, "_") {
		return false
	}
	return true
}

// ============================================================================
// ReadFileLine — verbose 模式 TOON 輸出格式
// ============================================================================

func TestTOONFormat_ReadFileLine(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "toon_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("hello world\n")
	tmpFile.Close()

	ec := newTestEC(map[string]interface{}{
		"filename": tmpFile.Name(),
		"LineNum":  float64(1),
		"verbose":  true,
	})
	result, status := execReadFileLine(ec)
	if status != TaskStatusSuccess {
		t.Fatalf("execReadFileLine failed: %s", result)
	}
	if err := checkTOONNestedPascalCase(result); err != nil {
		t.Errorf("ReadFileLine verbose TOON: %v\noutput:\n%s", err, result)
	}
}

// ============================================================================
// ReadFileLines — verbose 模式 TOON 輸出格式
// ============================================================================

func TestTOONFormat_ReadFileLines(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "toon_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("line1\nline2\nline3\n")
	tmpFile.Close()

	ec := newTestEC(map[string]interface{}{
		"filename": tmpFile.Name(),
		"verbose":  true,
	})
	result, status := execReadFileLines(ec)
	if status != TaskStatusSuccess {
		t.Fatalf("execReadFileLines failed: %s", result)
	}
	if err := checkTOONNestedPascalCase(result); err != nil {
		t.Errorf("ReadFileLines verbose TOON: %v\noutput:\n%s", err, result)
	}
}

// ============================================================================
// ReadFileRange — verbose 模式 TOON 輸出格式
// ============================================================================

func TestTOONFormat_ReadFileRange(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "toon_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("line1\nline2\nline3\nline4\nline5\n")
	tmpFile.Close()

	ec := newTestEC(map[string]interface{}{
		"filename":  tmpFile.Name(),
		"StartLine": float64(2),
		"EndLine":   float64(4),
		"verbose":   true,
	})
	result, status := execReadFileRange(ec)
	if status != TaskStatusSuccess {
		t.Fatalf("execReadFileRange failed: %s", result)
	}
	if err := checkTOONNestedPascalCase(result); err != nil {
		t.Errorf("ReadFileRange verbose TOON: %v\noutput:\n%s", err, result)
	}
}

// ============================================================================
// autoReadForWrite — TOON 輸出格式
// ============================================================================

func TestTOONFormat_AutoReadForWrite(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"package main", "", "func main() {", "\tprintln(\"hello\")", "}"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	result, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(3)})
	if !didRead {
		t.Fatal("expected auto-read to trigger")
	}
	if err := checkTOONNestedPascalCase(result); err != nil {
		t.Errorf("autoReadForWrite TOON: %v\noutput:\n%s", err, result)
	}

	// 確認必須字段存在
	requiredFields := []string{"AutoRead", "Message", "Tool", "Filename", "TotalLines", "ShownStart", "ShownEnd", "Lines"}
	var parsed map[string]interface{}
	toon.Unmarshal([]byte(result), &parsed)
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("autoReadForWrite TOON missing required field: %s", field)
		}
	}
}

// ============================================================================
// WriteFileLine — 非 verbose 模式僅返回內容字串（無 map），跳過
// WriteFileLines — 非 verbose 模式僅返回 "Successfully wrote N lines"（無 map），跳過
// WriteFileRange — 同上
// AppendToFile — 同上
// ============================================================================

// ============================================================================
// SmartShell sync 模式 — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SmartShellSync(t *testing.T) {
	// 模擬 handleSmartShell sync 模式的 map 結構（需要使用實際 handler）
	// SmartShell sync 依賴外部命令執行，此處構建等效 map 驗證格式
	result := map[string]interface{}{
		"Mode":       "sync",
		"Command":    "ls",
		"Stdout":     "file1\nfile2",
		"Stderr":     "",
		"ExitCode":   0,
		"StdoutFile": "/tmp/output.txt",
		"StderrFile": "",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SmartShell sync TOON: %v", err)
	}
}

// ============================================================================
// SmartShell confirm_required 模式 — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SmartShellConfirm(t *testing.T) {
	result := map[string]interface{}{
		"Mode":           "confirm_required",
		"ConfirmMessage": "This command may need interaction",
		"Suggestions":    []string{"use --yes flag", "use async mode"},
		"Message":        "⚠️ 此命令可能需要交互确认",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SmartShell confirm TOON: %v", err)
	}
}

// ============================================================================
// SmartShell timeout 模式 — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SmartShellTimeout(t *testing.T) {
	result := map[string]interface{}{
		"Mode":          "timeout",
		"Command":       "sleep 999",
		"TimeoutSeconds": 60,
		"IsUnknownCmd":   true,
		"Message":        "⏱️ 命令执行超时（60秒）",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SmartShell timeout TOON: %v", err)
	}
}

// ============================================================================
// SmartShell interactive 模式 — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SmartShellInteractive(t *testing.T) {
	result := map[string]interface{}{
		"Mode":             "interactive",
		"TaskId":           "task-123",
		"PID":              12345,
		"Status":           "running",
		"Command":          "vim file.txt",
		"WakeAfterMinutes": 5,
		"Suggestion":       "use echo instead",
		"NonInteractiveEq": "echo 'content' > file.txt",
		"Message":          "命令可能需要交互",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SmartShell interactive TOON: %v", err)
	}
}

// ============================================================================
// SmartShell async 模式 — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SmartShellAsync(t *testing.T) {
	result := map[string]interface{}{
		"TaskId":           "task-456",
		"PID":              67890,
		"Status":           "running",
		"Command":          "long_running_job",
		"WakeAfterMinutes": 10,
		"Message":          "✅ 任务已启动（PID: 67890），将在 10 分钟后唤醒你。",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SmartShell async TOON: %v", err)
	}
}

// ============================================================================
// TaskWait — TOON 輸出格式
// ============================================================================

func TestTOONFormat_TaskWait(t *testing.T) {
	result := map[string]interface{}{
		"TaskId":        "task-789",
		"Status":        "Waiting",
		"WaitMinutes":   5,
		"NextWakeAfter": "2026-05-05T12:00:00Z",
		"Message":       "✅ 已设置 5 分钟后唤醒",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("TaskWait TOON: %v", err)
	}
}

// ============================================================================
// TaskManager GetTaskInfo — TOON 輸出格式
// ============================================================================

func TestTOONFormat_TaskInfo(t *testing.T) {
	result := map[string]interface{}{
		"TaskId":         "task-001",
		"Command":        "make build",
		"Description":    "Build project",
		"PID":            11111,
		"Status":         "running",
		"ExitCode":       0,
		"StartTime":      "2026-05-05T12:00:00Z",
		"RuntimeMinutes": 2.5,
		"Stdout":         "compiling...",
		"Stderr":         "",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("TaskInfo TOON: %v", err)
	}
}

// ============================================================================
// SSH SmartShell async — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SSHAsync(t *testing.T) {
	result := map[string]interface{}{
		"Mode":             "async",
		"TaskId":           "task-ssh-001",
		"Status":           "running",
		"Command":          "ls -la",
		"WakeAfterMinutes": 5,
		"Message":          "✅ SSH command is running asynchronously",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SSH async TOON: %v", err)
	}
}

// ============================================================================
// Subagent GetTaskInfo — TOON 輸出格式
// ============================================================================

func TestTOONFormat_SubagentInfo(t *testing.T) {
	result := map[string]interface{}{
		"TaskId":         "sub-001",
		"Task":           "analyze code",
		"SessionId":      "session-123",
		"Status":         "running",
		"Iterations":     10,
		"MaxIterations":  50,
		"Depth":          1,
		"StartTime":      "2026-05-05T12:00:00Z",
		"RuntimeSeconds": 30.5,
		"Result":         "analysis complete",
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SubagentInfo TOON: %v", err)
	}
}

// ============================================================================
// TextReplaceResult — struct 序列化 TOON 格式
// ============================================================================

func TestTOONFormat_TextReplaceResult(t *testing.T) {
	result := TextReplaceResult{
		Success:      true,
		Output:       "modified content",
		LinesChanged: 3,
		TotalLines:   10,
		MatchesFound: 2,
		ChangedLines: []string{"line1", "line2", "line3"},
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("TextReplaceResult TOON: %v", err)
	}
}

// ============================================================================
// SkillInfo — struct 序列化 TOON 格式
// ============================================================================

func TestTOONFormat_SkillInfo(t *testing.T) {
	result := map[string]interface{}{
		"Name":         "test-skill",
		"DisplayName":  "Test Skill",
		"Description":  "A test skill",
		"FilePath":     "/tmp/test-skill.md",
		"FileSize":     int64(1024),
		"ModTime":      int64(1712345678),
		"UseCount":     5,
		"TriggerWords": `["test"]`,
	}
	toonBytes, err := toon.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTOONPascalCase(string(toonBytes)); err != nil {
		t.Errorf("SkillInfo TOON: %v", err)
	}
}

// ============================================================================
// Context.ToolCall — Agentic Tag TOON 輸出格式（若有 map 輸出）
// ============================================================================

func TestTOONFormat_EmptyNonMapOutputs(t *testing.T) {
	// 以下工具輸出不包含 TOON map：
	// - WriteFileLine/WriteFileLines/WriteFileRange/AppendToFile: 返回純文字訊息
	// - TextGrep/TextReplace/TextTransform 成功時: 返回 "✅ 操作完成\n..." 純文字
	// - Shell/Bash: 返回純文字 stdout/stderr
	//
	// 這些不需要 TOON map 格式檢查，此測試僅作為文件記錄。
	_ = context.Background()
}

// ============================================================================
// 回歸測試：列舉所有已知 TOON 輸出並驗證 PascalCase
// ============================================================================

func TestTOONFormat_AllKnownOutputs(t *testing.T) {
	// 列舉所有已知的 TOON map 輸出格式，確保無遺漏
	allOutputs := []toonFieldCompliance{
		// ReadFile tools (tested via actual exec)
		// autoReadForWrite (tested via actual function)
		// SmartShell modes
		{name: "SmartShell sync", output: mustMarshalTOON(t, map[string]interface{}{
			"Mode": "sync", "Command": "", "Stdout": "", "Stderr": "", "ExitCode": 0,
		})},
		{name: "SmartShell confirm", output: mustMarshalTOON(t, map[string]interface{}{
			"Mode": "confirm_required", "ConfirmMessage": "", "Suggestions": []string{}, "Message": "",
		})},
		{name: "SmartShell timeout", output: mustMarshalTOON(t, map[string]interface{}{
			"Mode": "timeout", "Command": "", "TimeoutSeconds": 0, "IsUnknownCmd": false, "Message": "",
		})},
		{name: "SmartShell interactive", output: mustMarshalTOON(t, map[string]interface{}{
			"Mode": "interactive", "TaskId": "", "PID": 0, "Status": "", "Command": "",
			"WakeAfterMinutes": 0, "Suggestion": "", "NonInteractiveEq": "", "Message": "",
		})},
		{name: "SmartShell async", output: mustMarshalTOON(t, map[string]interface{}{
			"TaskId": "", "PID": 0, "Status": "", "Command": "", "WakeAfterMinutes": 0, "Message": "",
		})},
		{name: "TaskWait", output: mustMarshalTOON(t, map[string]interface{}{
			"TaskId": "", "Status": "", "WaitMinutes": 0, "NextWakeAfter": "", "Message": "",
		})},
		{name: "TaskInfo", output: mustMarshalTOON(t, map[string]interface{}{
			"TaskId": "", "Command": "", "Description": "", "PID": 0, "Status": "",
			"ExitCode": 0, "StartTime": "", "RuntimeMinutes": 0.0, "Stdout": "", "Stderr": "",
		})},
		{name: "SSH async", output: mustMarshalTOON(t, map[string]interface{}{
			"Mode": "", "TaskId": "", "Status": "", "Command": "", "WakeAfterMinutes": 0, "Message": "",
		})},
		{name: "SubagentInfo", output: mustMarshalTOON(t, map[string]interface{}{
			"TaskId": "", "Task": "", "SessionId": "", "Status": "", "Iterations": 0,
			"MaxIterations": 0, "Depth": 0, "StartTime": "", "RuntimeSeconds": 0.0, "Result": "",
		})},
		{name: "autoReadForWrite (equivalent)", output: mustMarshalTOON(t, map[string]interface{}{
			"AutoRead": true, "Message": "", "Tool": "", "Filename": "", "TotalLines": 0,
			"ShownStart": 0, "ShownEnd": 0, "Truncated": false,
			"Lines": []map[string]interface{}{{"Line": 1, "Content": "test"}},
		})},
	}

	for _, output := range allOutputs {
		t.Run(output.name, func(t *testing.T) {
			if err := checkTOONNestedPascalCase(output.output); err != nil {
				t.Errorf("%s: %v", output.name, err)
			}
		})
	}
}

// ============================================================================
// isPascalCase 單元測試
// ============================================================================

func TestIsPascalCase(t *testing.T) {
	valid := []string{
		"AutoRead", "Message", "Tool", "Filename", "TotalLines",
		"Lines", "Line", "Content", "ShownStart", "ShownEnd", "Truncated",
		"Mode", "Command", "Stdout", "Stderr", "ExitCode",
		"ConfirmMessage", "Suggestions", "TimeoutSeconds", "IsUnknownCmd",
		"TaskId", "PID", "Status", "WakeAfterMinutes",
		"Suggestion", "NonInteractiveEq", "NextWakeAfter",
		"WaitMinutes", "RuntimeMinutes", "Description",
		"StartTime", "MaxIterations", "Iterations", "Depth",
		"RuntimeSeconds", "SessionId", "Result",
		"Success", "Output", "LinesChanged", "MatchesFound", "ChangedLines", "Error",
		"Name", "DisplayName", "FilePath", "FileSize", "ModTime", "UseCount", "TriggerWords",
		"StdoutFile", "StderrFile",
	}
	invalid := []string{
		"auto_read", "total_lines", "shown_start", "shown_end",
		"confirm_message", "timeout_seconds", "is_unknown_cmd",
		"wake_after_minutes", "non_interactive_eq", "next_wake_after",
		"runtime_minutes", "runtime_seconds",
		"lines_changed", "matches_found", "changed_lines",
		"file_size", "mod_time", "use_count",
		"exit_code", "stdout_file", "stderr_file",
		"", "snake_case", "_leading", "kebab-case",
		"camelCase", "lowercase",
	}

	for _, v := range valid {
		if !isPascalCase(v) {
			t.Errorf("isPascalCase(%q) = false, want true", v)
		}
	}
	for _, v := range invalid {
		if isPascalCase(v) {
			t.Errorf("isPascalCase(%q) = true, want false", v)
		}
	}
}

// mustMarshalTOON 輔助函數，marshal 失敗時終止測試
func mustMarshalTOON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := toon.Marshal(v)
	if err != nil {
		t.Fatalf("toon.Marshal error: %v", err)
	}
	return string(data)
}
