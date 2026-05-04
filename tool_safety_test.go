package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// absInt / minInt
// ============================================================================

func TestAbsInt(t *testing.T) {
	tests := []struct {
		input, want int
	}{
		{0, 0},
		{1, 1},
		{-1, 1},
		{100, 100},
		{-100, 100},
		{-99999, 99999},
	}
	for _, tt := range tests {
		got := absInt(tt.input)
		if got != tt.want {
			t.Errorf("absInt(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		a, b, c, want int
	}{
		{1, 2, 3, 1},
		{3, 2, 1, 1},
		{2, 1, 3, 1},
		{0, 0, 0, 0},
		{-1, 0, 1, -1},
		{-5, -10, -3, -10},
		{100, 200, 50, 50},
	}
	for _, tt := range tests {
		got := minInt(tt.a, tt.b, tt.c)
		if got != tt.want {
			t.Errorf("minInt(%d, %d, %d) = %d, want %d", tt.a, tt.b, tt.c, got, tt.want)
		}
	}
}

// ============================================================================
// levenshteinDistance
// ============================================================================

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   int
	}{
		// 相同
		{"hello", "hello", 0},
		{"", "", 0},
		// 空字符串
		{"", "abc", 3},
		{"abc", "", 3},
		// 单字符差异
		{"a", "b", 1},
		{"ab", "ac", 1},
		// 插入
		{"abc", "abcd", 1},
		{"test", "tests", 1},
		// 删除
		{"abcd", "abc", 1},
		// 替换
		{"kitten", "sitten", 1},
		{"sitten", "sitting", 2}, // s→t, e→i, +g (replace + insert)
		// 经典案例
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		// 中文 (Levenshtein 操作在 byte 層面，中文字符 = 3 bytes)
		{"你好", "你好吗", 3},   // insert 3-byte UTF-8 char
		{"写文件", "写入文件", 3}, // insert 3-byte UTF-8 char
		{"hello世界", "hello世界!", 1},
		// 长度差异大 (快速路径)
		{"a", "abcdefghij", 9},
		// shell 相关 (PascalCase)
		{"Shell", "SmartShell", 5},
		{"SmartShell", "Shell", 5},
	}
	for _, tt := range tests {
		got := levenshteinDistance(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
		}
	}
}

// ============================================================================
// FindSimilarTool — 依赖 toolRegistryMap（動態查詢）
// ============================================================================

func TestFindSimilarTool(t *testing.T) {
	t.Run("精确匹配自身", func(t *testing.T) {
		result := FindSimilarTool("Shell")
		// 精确匹配应返回自身（编辑距离 0，满足 threshold）
		if result != "Shell" {
			t.Errorf("expected 'shell', got %q", result)
		}
	})

	t.Run("小笔误", func(t *testing.T) {
		result := FindSimilarTool("shel")
		// 编辑距离 1，接近 "Shell"
		if result != "Shell" {
			t.Errorf("expected 'shell', got %q", result)
		}
	})

	t.Run("常见拼写错误", func(t *testing.T) {
		result := FindSimilarTool("smart_shel")
		if result != "SmartShell" {
			t.Errorf("expected 'SmartShell', got %q", result)
		}
	})

	t.Run("带空格输入", func(t *testing.T) {
		result := FindSimilarTool("  shell  ")
		if result != "Shell" {
			t.Errorf("expected 'shell', got %q", result)
		}
	})

	t.Run("大小写不敏感", func(t *testing.T) {
		result := FindSimilarTool("SHELL")
		if result != "Shell" {
			t.Errorf("expected 'shell', got %q", result)
		}
	})

	t.Run("永远返回某个结果 (算法设计如此)", func(t *testing.T) {
		// FindSimilarTool 总是返回最佳匹配（即使距离很远），因为阈值 max/2+1 很宽松
		result := FindSimilarTool("~~~")
		// 只验证返回结果非空且是已知工具
		found := false
		for _, name := range allRegisteredToolNames() {
			if result == name {
				found = true
				break
			}
		}
		if !found && result != "" {
			t.Errorf("returned unknown tool: %q", result)
		}
	})

	t.Run("read 相关", func(t *testing.T) {
		result := FindSimilarTool("ReadFile")
		if result != "ReadFileLine" {
			t.Errorf("expected 'ReadFileLine', got %q", result)
		}
	})

	t.Run("text_grep 变体", func(t *testing.T) {
		// "grep" is not in the tool list; "TextGrep" is
		result := FindSimilarTool("text_grepp")
		if result != "TextGrep" {
			t.Errorf("expected 'text_grep', got %q", result)
		}
	})
}

// ============================================================================
// GetUnknownToolErrorMessage
// ============================================================================

func TestGetUnknownToolErrorMessage(t *testing.T) {
	t.Run("已知近似工具会建议", func(t *testing.T) {
		msg := GetUnknownToolErrorMessage("shel")
		if !strings.Contains(msg, "Shell") {
			t.Errorf("expected suggestion for 'shell', got: %s", msg)
		}
		if !strings.Contains(msg, "shel") {
			t.Errorf("expected original tool name in message, got: %s", msg)
		}
	})

	t.Run("任何輸入都會生成有效消息", func(t *testing.T) {
		// GetUnknownToolErrorMessage 总是返回一条包含原始工具名的消息
		msg := GetUnknownToolErrorMessage("~~~")
		if !strings.Contains(msg, "~~~") {
			t.Errorf("message should contain original tool name, got: %s", msg)
		}
		if !strings.Contains(msg, "不存在") {
			t.Errorf("message should say tool doesn't exist, got: %s", msg)
		}
	})
}

// ============================================================================
// isWriteTool
// ============================================================================

func TestIsWriteTool(t *testing.T) {
	writeTools := []string{
		"WriteFileLine", "WriteFileLines", "AppendToFile",
		"WriteFileRange", "TextReplace", "TextTransform",
		"MemorySave", "MemoryForget",
	}
	for _, name := range writeTools {
		if !isWriteTool(name) {
			t.Errorf("%q should be a write tool", name)
		}
	}

	readTools := []string{
		"ReadFileLine", "ReadFileLines", "TextSearch",
		"Shell", "SmartShell", "Spawn", "mcp_call",
		"Tasks", "Menu",
	}
	for _, name := range readTools {
		if isWriteTool(name) {
			t.Errorf("%q should NOT be a write tool", name)
		}
	}
}

// ============================================================================
// extractFilePathFromArgs
// ============================================================================

func TestExtractFilePathFromArgs(t *testing.T) {
	t.Run("FilePath", func(t *testing.T) {
		args := map[string]interface{}{"FilePath": "/tmp/test.txt"}
		got := extractFilePathFromArgs(args)
		if got != "/tmp/test.txt" {
			t.Errorf("expected '/tmp/test.txt', got %q", got)
		}
	})

	t.Run("filePath (camelCase)", func(t *testing.T) {
		args := map[string]interface{}{"filePath": "/home/user/data.json"}
		got := extractFilePathFromArgs(args)
		if got != "/home/user/data.json" {
			t.Errorf("expected '/home/user/data.json', got %q", got)
		}
	})

	t.Run("path", func(t *testing.T) {
		args := map[string]interface{}{"path": "config.toon"}
		got := extractFilePathFromArgs(args)
		if got != "config.toon" {
			t.Errorf("expected 'config.toon', got %q", got)
		}
	})

	t.Run("filename", func(t *testing.T) {
		args := map[string]interface{}{"filename": "output.txt"}
		got := extractFilePathFromArgs(args)
		if got != "output.txt" {
			t.Errorf("expected 'output.txt', got %q", got)
		}
	})

	t.Run("file", func(t *testing.T) {
		args := map[string]interface{}{"file": "input.csv"}
		got := extractFilePathFromArgs(args)
		if got != "input.csv" {
			t.Errorf("expected 'input.csv', got %q", got)
		}
	})

	t.Run("空参数", func(t *testing.T) {
		args := map[string]interface{}{}
		got := extractFilePathFromArgs(args)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("空值", func(t *testing.T) {
		args := map[string]interface{}{"FilePath": ""}
		got := extractFilePathFromArgs(args)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("非字符串跳过", func(t *testing.T) {
		args := map[string]interface{}{"FilePath": 123}
		got := extractFilePathFromArgs(args)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("优先级: file_path > filePath", func(t *testing.T) {
		args := map[string]interface{}{
			"FilePath": "/first/path.txt",
			"filePath":  "/second/path.txt",
		}
		got := extractFilePathFromArgs(args)
		if got != "/first/path.txt" {
			t.Errorf("expected '/first/path.txt', got %q", got)
		}
	})
}

// ============================================================================
// IsReadOnlyTool
// ============================================================================

func TestIsReadOnlyTool(t *testing.T) {
	roTools := []string{
		"ReadFileLine", "ReadFileLines", "TextSearch", "TextGrep",
		"MemoryRecall", "MemoryList", "PlanRead", "PluginList",
		"SkillList", "SkillGet", "CronList", "CronStatus",
		"SpawnList", "SSHList", "ProfileCheck",
	}
	for _, name := range roTools {
		if !IsReadOnlyTool(name) {
			t.Errorf("%q should be read-only", name)
		}
	}

	if IsReadOnlyTool("Shell") {
		t.Error("shell should NOT be read-only")
	}
	if IsReadOnlyTool("unknown_tool_xyz") {
		t.Error("unknown tool should not be read-only by default")
	}
}

// ============================================================================
// ShouldForceStop
// ============================================================================

func TestShouldForceStop(t *testing.T) {
	t.Run("未设上限不强制停止", func(t *testing.T) {
		oldMax := MaxAgentLoopIterations
		defer func() { MaxAgentLoopIterations = oldMax }()
		MaxAgentLoopIterations = 0
		if ShouldForceStop(1000) {
			t.Error("should not force stop when limit is 0")
		}
	})

	t.Run("达到上限", func(t *testing.T) {
		oldMax := MaxAgentLoopIterations
		defer func() { MaxAgentLoopIterations = oldMax }()
		MaxAgentLoopIterations = 10
		if !ShouldForceStop(10) {
			t.Error("should force stop at iteration == limit")
		}
		if !ShouldForceStop(11) {
			t.Error("should force stop when iteration exceeds limit")
		}
	})

	t.Run("未达上限", func(t *testing.T) {
		oldMax := MaxAgentLoopIterations
		defer func() { MaxAgentLoopIterations = oldMax }()
		MaxAgentLoopIterations = 10
		if ShouldForceStop(9) {
			t.Error("should not force stop when iteration < limit")
		}
		if ShouldForceStop(0) {
			t.Error("should not force stop at iteration 0")
		}
	})
}

// ============================================================================
// GetIterationWarningMessage
// ============================================================================

func TestGetIterationWarningMessage(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	defer func() { MaxAgentLoopIterations = oldMax }()
	MaxAgentLoopIterations = 10

	t.Run("接近上限 (剩余 <= 5)", func(t *testing.T) {
		msg := GetIterationWarningMessage(8)
		if !strings.Contains(msg, "系统警告") {
			t.Errorf("expected 系统警告 for near limit, got: %s", msg)
		}
		if !strings.Contains(msg, "剩余 2 轮") {
			t.Errorf("expected '剩余 2 轮', got: %s", msg)
		}
	})

	t.Run("还有余地 (剩余 > 5)", func(t *testing.T) {
		msg := GetIterationWarningMessage(3)
		if !strings.Contains(msg, "系统提醒") {
			t.Errorf("expected 系统提醒 for early warning, got: %s", msg)
		}
	})
}

// ============================================================================
// sanitizeContent — AgentLoop.go
// ============================================================================

func TestSanitizeContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"正常文本", "Hello World", "Hello World"},
		{"换行保留", "Line1\nLine2", "Line1\nLine2"},
		{"制表符保留", "Col1\tCol2", "Col1\tCol2"},
		{"回车删除", "Line1\rLine2", "Line1Line2"},
		{"NULL 字符删除", "Hello\x00World", "HelloWorld"},
		{"BEL 删除", "Hello\x07World", "HelloWorld"},
		{"BS 删除", "Hello\x08World", "HelloWorld"},
		{"DEL 删除", "Hello\x7FWorld", "HelloWorld"},
		{"多项混合", "\x00Hello\x07\x08World\x7F\nTest", "HelloWorld\nTest"},
		{"空字符串", "", ""},
		{"只有控制字符", "\x00\x01\x02\x03", ""},
		{"中文", "你好世界\n测试", "你好世界\n测试"},
		{"中文+控制字符", "你好\x00世界\x07测试", "你好世界测试"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeContent(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeContent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// readWriteTracker
// ============================================================================

func newReadWriteTracker() *readWriteTracker {
	return &readWriteTracker{
		fullReadFiles:    make(map[string]time.Time),
		partialReadFiles: make(map[string]time.Time),
		maxEntries:       200,
	}
}

func TestReadWriteTracker_MarkFileFullyRead(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileFullyRead("/tmp/test.txt")
	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelFull {
		t.Errorf("GetFileReadLevel after MarkFileFullyRead = %v, want readLevelFull", lvl)
	}
}

func TestReadWriteTracker_MarkFilePartialRead(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFilePartialRead("/tmp/test.txt")
	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelPartial {
		t.Errorf("GetFileReadLevel after MarkFilePartialRead = %v, want readLevelPartial", lvl)
	}
}

func TestReadWriteTracker_PartialReadDoesNotDowngradeFull(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileFullyRead("/tmp/test.txt")
	trk.MarkFilePartialRead("/tmp/test.txt")
	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelFull {
		t.Errorf("MarkFilePartialRead should not downgrade from full, got %v", lvl)
	}
}

func TestReadWriteTracker_NeverReadIsNone(t *testing.T) {
	trk := newReadWriteTracker()
	lvl := trk.GetFileReadLevel("/tmp/never_read.txt")
	if lvl != readLevelNone {
		t.Errorf("unread file GetFileReadLevel = %v, want readLevelNone", lvl)
	}
}

func TestReadWriteTracker_HasFileBeenRead(t *testing.T) {
	trk := newReadWriteTracker()
	if trk.HasFileBeenRead("/tmp/test.txt") {
		t.Error("HasFileBeenRead should be false for unread file")
	}
	trk.MarkFileFullyRead("/tmp/test.txt")
	if !trk.HasFileBeenRead("/tmp/test.txt") {
		t.Error("HasFileBeenRead should be true after MarkFileFullyRead")
	}
}

func TestReadWriteTracker_HasFileBeenRead_Partial(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFilePartialRead("/tmp/partial.txt")
	if !trk.HasFileBeenRead("/tmp/partial.txt") {
		t.Error("HasFileBeenRead should be true after MarkFilePartialRead")
	}
}

func TestReadWriteTracker_MultipleFiles(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileFullyRead("/tmp/a.txt")
	trk.MarkFilePartialRead("/tmp/b.txt")

	if trk.GetFileReadLevel("/tmp/a.txt") != readLevelFull {
		t.Error("a.txt should be full read")
	}
	if trk.GetFileReadLevel("/tmp/b.txt") != readLevelPartial {
		t.Error("b.txt should be partial read")
	}
	if trk.GetFileReadLevel("/tmp/c.txt") != readLevelNone {
		t.Error("c.txt should be none")
	}
}

// ============================================================================
// normalizeFilePath
// ============================================================================

func TestNormalizeFilePath_Relative(t *testing.T) {
	result := normalizeFilePath("foo/bar")
	if !filepath.IsAbs(result) {
		t.Errorf("normalizeFilePath(%q) = %q, should be absolute", "foo/bar", result)
	}
	if strings.Contains(result, "..") {
		t.Errorf("normalizeFilePath should clean .. components: %q", result)
	}
}

func TestNormalizeFilePath_Absolute(t *testing.T) {
	result := normalizeFilePath("/tmp/foo/bar")
	if result != "/tmp/foo/bar" {
		t.Errorf("normalizeFilePath(/tmp/foo/bar) = %q, want %q", result, "/tmp/foo/bar")
	}
}

func TestNormalizeFilePath_WithDotDot(t *testing.T) {
	result := normalizeFilePath("/tmp/foo/../bar")
	if strings.Contains(result, "..") {
		t.Errorf("normalizeFilePath should resolve .. : %q", result)
	}
}

// ============================================================================
// CheckWritePermission
// ============================================================================

func TestCheckWritePermission_NewFile(t *testing.T) {
	err := CheckWritePermission("/tmp/nonexistent_file_xyz_test.txt", "WriteFileLines")
	if err != nil {
		t.Errorf("CheckWritePermission for new file should allow, got error: %v", err)
	}
}

func TestCheckWritePermission_ExistingFileNotRead(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "safety_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines")
	if err == nil {
		t.Error("CheckWritePermission should block write on unread existing file")
	}
}

func TestCheckWritePermission_ExistingFileFullyRead(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "safety_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileFullyRead(tmpFile.Name())
	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines")
	if err != nil {
		t.Errorf("CheckWritePermission should allow write on fully read file, got error: %v", err)
	}
}

func TestCheckWritePermission_ExistingFilePartialRead(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "safety_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFilePartialRead(tmpFile.Name())
	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines")
	if err == nil {
		t.Error("CheckWritePermission should block write on partial-read file (need full read)")
	}
}

// ============================================================================
// RepeatedErrorEscalator
// ============================================================================

func TestRepeatedErrorEscalator_BelowThreshold(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	shouldStop, _ := e.RecordEscalation(EscalateRepeatedFailure, "key1", "error msg 1")
	if shouldStop {
		t.Error("first error should not trigger escalation")
	}

	shouldStop, _ = e.RecordEscalation(EscalateRepeatedFailure, "key1", "error msg 2")
	if shouldStop {
		t.Error("second error should not trigger escalation")
	}
}

func TestRepeatedErrorEscalator_AtThreshold(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 1")
	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 2")
	shouldStop, userMsg := e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 3")

	if !shouldStop {
		t.Error("third error should trigger escalation")
	}
	if userMsg == "" {
		t.Error("escalation message should not be empty")
	}
	if !strings.Contains(userMsg, "msg 1") {
		t.Error("escalation message should contain all recorded messages")
	}
}

func TestRepeatedErrorEscalator_ResetAfterEscalation(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 1")
	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 2")
	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 3")

	shouldStop, _ := e.RecordEscalation(EscalateRepeatedFailure, "key1", "new msg")
	if shouldStop {
		t.Error("after escalation reset, first new error should not trigger")
	}
}

func TestRepeatedErrorEscalator_DifferentKeys(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg")
	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg")
	e.RecordEscalation(EscalateRepeatedFailure, "key2", "msg")
	shouldStop, _ := e.RecordEscalation(EscalateRepeatedFailure, "key2", "msg")

	if shouldStop {
		t.Error("different error key should not trigger escalation with only 2 occurrences")
	}
}

func TestRepeatedErrorEscalator_ResetCategory(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 1")
	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 2")
	e.ResetCategory(EscalateRepeatedFailure)

	shouldStop, _ := e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 3")
	if shouldStop {
		t.Error("after ResetCategory, count should restart from 0")
	}
}

func TestRepeatedErrorEscalator_ResetKey(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateRepeatedFailure, "key1", "msg 1")
	e.RecordEscalation(EscalateRepeatedFailure, "key2", "msg 1")
	e.ResetKey(EscalateRepeatedFailure, "key1")

	e.RecordEscalation(EscalateRepeatedFailure, "key2", "msg 2")
	shouldStop, _ := e.RecordEscalation(EscalateRepeatedFailure, "key2", "msg 3")
	if !shouldStop {
		t.Error("key2 should still escalate after key1 was reset")
	}
}

func TestRepeatedErrorEscalator_WriteWithoutReadMessage(t *testing.T) {
	e := &RepeatedErrorEscalator{
		trackers: make(map[string]*escalationTracker),
	}

	e.RecordEscalation(EscalateWriteWithoutRead, "file.txt", "write blocked 1")
	e.RecordEscalation(EscalateWriteWithoutRead, "file.txt", "write blocked 2")
	_, userMsg := e.RecordEscalation(EscalateWriteWithoutRead, "file.txt", "write blocked 3")

	if !strings.Contains(userMsg, "ReadFileLines") {
		t.Error("WriteWithoutRead escalation should mention ReadFileLines")
	}
}

// ============================================================================
// LoopWarningInjector
// ============================================================================

func TestLoopWarningInjector_NoLimit(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	MaxAgentLoopIterations = 0
	defer func() { MaxAgentLoopIterations = oldMax }()

	inj := &LoopWarningInjector{}
	if inj.ShouldInjectWarning(100) {
		t.Error("ShouldInjectWarning with no limit should return false")
	}
}

func TestLoopWarningInjector_BelowThreshold(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	oldThreshold := IterationWarningThreshold
	MaxAgentLoopIterations = 100
	IterationWarningThreshold = 80 // 80% of 100
	defer func() {
		MaxAgentLoopIterations = oldMax
		IterationWarningThreshold = oldThreshold
	}()

	inj := &LoopWarningInjector{}
	if inj.ShouldInjectWarning(10) {
		t.Error("ShouldInjectWarning below threshold (10 < 80) should return false")
	}
}

func TestLoopWarningInjector_NotBeforeCooldown(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	oldThreshold := IterationWarningThreshold
	MaxAgentLoopIterations = 100
	IterationWarningThreshold = 10
	defer func() {
		MaxAgentLoopIterations = oldMax
		IterationWarningThreshold = oldThreshold
	}()

	inj := &LoopWarningInjector{}
	if !inj.ShouldInjectWarning(80) {
		t.Error("first call above threshold should return true")
	}

	inj.warningInjected = true
	lastWarnIteration = 80

	if inj.ShouldInjectWarning(81) {
		t.Error("ShouldInjectWarning should respect cooldown (min 3 iterations)")
	}
}

func TestLoopWarningInjector_AfterCooldown(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	oldThreshold := IterationWarningThreshold
	MaxAgentLoopIterations = 100
	IterationWarningThreshold = 10
	defer func() {
		MaxAgentLoopIterations = oldMax
		IterationWarningThreshold = oldThreshold
	}()

	inj := &LoopWarningInjector{}
	inj.warningInjected = true
	lastWarnIteration = 80

	if !inj.ShouldInjectWarning(84) {
		t.Error("ShouldInjectWarning after cooldown should return true")
	}
}

func TestLoopWarningInjector_ResetState(t *testing.T) {
	oldMax := MaxAgentLoopIterations
	oldThreshold := IterationWarningThreshold
	MaxAgentLoopIterations = 100
	IterationWarningThreshold = 10
	defer func() {
		MaxAgentLoopIterations = oldMax
		IterationWarningThreshold = oldThreshold
	}()

	inj := &LoopWarningInjector{}
	if !inj.ShouldInjectWarning(90) {
		t.Error("fresh LoopWarningInjector should allow first warning above threshold")
	}
}
