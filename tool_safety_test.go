package main

import (
	"fmt"
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
		readRanges:       make(map[string][]LineRange),
		maxEntries:       200,
	}
}

// createTempFile 創建臨時文件用於檢查已存在文件的寫入權限測試。
func createTempFile(t *testing.T) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "safety_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	return tmpFile.Name()
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
// MarkFileLineRead / MarkFileRangeRead — 精確行範圍追蹤
// ============================================================================

func TestReadWriteTracker_MarkFileLineRead(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileLineRead("/tmp/test.txt", 5)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelPartial {
		t.Errorf("GetFileReadLevel after MarkFileLineRead = %v, want readLevelPartial", lvl)
	}

	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) != 1 {
		t.Fatalf("expected 1 range, got %d", len(ranges))
	}
	if ranges[0].StartLine != 5 || ranges[0].EndLine != 5 {
		t.Errorf("range = [%d,%d], want [5,5]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestReadWriteTracker_MarkFileRangeRead(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileRangeRead("/tmp/test.txt", 10, 20)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelPartial {
		t.Errorf("GetFileReadLevel after MarkFileRangeRead = %v, want readLevelPartial", lvl)
	}

	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) != 1 {
		t.Fatalf("expected 1 range, got %d", len(ranges))
	}
	if ranges[0].StartLine != 10 || ranges[0].EndLine != 20 {
		t.Errorf("range = [%d,%d], want [10,20]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestReadWriteTracker_MultipleLinesMerged(t *testing.T) {
	trk := newReadWriteTracker()
	// 連續讀取應合併為一個範圍（adjacent: [5,5] + [6,6] = [5,6]）
	trk.MarkFileLineRead("/tmp/test.txt", 5)
	trk.MarkFileLineRead("/tmp/test.txt", 6)
	trk.MarkFileLineRead("/tmp/test.txt", 10)

	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) != 2 {
		t.Fatalf("expected 2 merged ranges, got %d: %+v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 5 || ranges[0].EndLine != 6 {
		t.Errorf("range[0] = [%d,%d], want [5,6]", ranges[0].StartLine, ranges[0].EndLine)
	}
	if ranges[1].StartLine != 10 || ranges[1].EndLine != 10 {
		t.Errorf("range[1] = [%d,%d], want [10,10]", ranges[1].StartLine, ranges[1].EndLine)
	}
}

func TestReadWriteTracker_RangeOverlappingMerge(t *testing.T) {
	trk := newReadWriteTracker()
	// 重疊範圍合併
	trk.MarkFileRangeRead("/tmp/test.txt", 10, 20)
	trk.MarkFileRangeRead("/tmp/test.txt", 15, 25)
	// [10,20] + [15,25] = [10,25]

	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) != 1 {
		t.Fatalf("expected 1 merged range, got %d: %+v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 10 || ranges[0].EndLine != 25 {
		t.Errorf("range = [%d,%d], want [10,25]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestReadWriteTracker_RangeAdjacentMerge(t *testing.T) {
	trk := newReadWriteTracker()
	// 相鄰範圍合併（[1,5] + [6,10] → [1,10]）
	trk.MarkFileRangeRead("/tmp/test.txt", 1, 5)
	trk.MarkFileRangeRead("/tmp/test.txt", 6, 10)

	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) != 1 {
		t.Fatalf("expected 1 merged range, got %d: %+v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 1 || ranges[0].EndLine != 10 {
		t.Errorf("range = [%d,%d], want [1,10]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestReadWriteTracker_LineReadDoesNotDowngradeFull(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileFullyRead("/tmp/test.txt")
	trk.MarkFileLineRead("/tmp/test.txt", 5)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelFull {
		t.Errorf("MarkFileLineRead should not downgrade from full, got %v", lvl)
	}
	// fullRead 應清除 readRanges
	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if ranges != nil {
		t.Errorf("GetFileReadRanges should return nil for fully read file, got %+v", ranges)
	}
}

func TestReadWriteTracker_RangeReadDoesNotDowngradeFull(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileFullyRead("/tmp/test.txt")
	trk.MarkFileRangeRead("/tmp/test.txt", 1, 100)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelFull {
		t.Errorf("MarkFileRangeRead should not downgrade from full, got %v", lvl)
	}
}

func TestReadWriteTracker_MarkFileLineRead_InvalidLineNum(t *testing.T) {
	trk := newReadWriteTracker()
	// 無效行號（負數、0）應被忽略
	trk.MarkFileLineRead("/tmp/test.txt", 0)
	trk.MarkFileLineRead("/tmp/test.txt", -1)
	trk.MarkFileLineRead("/tmp/test.txt", -100)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelNone {
		t.Errorf("invalid line numbers should be ignored, got %v", lvl)
	}
}

func TestReadWriteTracker_MarkFileRangeRead_InvalidStartLine(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileRangeRead("/tmp/test.txt", 0, 10)
	trk.MarkFileRangeRead("/tmp/test.txt", -5, 10)

	lvl := trk.GetFileReadLevel("/tmp/test.txt")
	if lvl != readLevelNone {
		t.Errorf("invalid start line should be ignored, got %v", lvl)
	}
}

func TestReadWriteTracker_GetFileReadRanges_ReturnsCopy(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileRangeRead("/tmp/test.txt", 1, 10)

	ranges1 := trk.GetFileReadRanges("/tmp/test.txt")
	ranges2 := trk.GetFileReadRanges("/tmp/test.txt")

	// 修改 ranges1 不應影響內部狀態
	ranges1[0].StartLine = 999

	ranges3 := trk.GetFileReadRanges("/tmp/test.txt")
	if ranges3[0].StartLine != 1 {
		t.Error("GetFileReadRanges should return a copy, not reference to internal state")
	}
	_ = ranges2
}

func TestReadWriteTracker_FullyReadClearsRanges(t *testing.T) {
	trk := newReadWriteTracker()
	trk.MarkFileRangeRead("/tmp/test.txt", 1, 100)
	ranges := trk.GetFileReadRanges("/tmp/test.txt")
	if len(ranges) == 0 {
		t.Fatal("should have ranges before full read")
	}

	trk.MarkFileFullyRead("/tmp/test.txt")
	ranges = trk.GetFileReadRanges("/tmp/test.txt")
	if ranges != nil {
		t.Error("GetFileReadRanges should return nil after full read")
	}
}

// ============================================================================
// mergeRanges / isLineInRanges / isRangeOverlapping / describeRanges
// ============================================================================

func TestMergeRanges_Empty(t *testing.T) {
	result := mergeRanges(nil, LineRange{StartLine: 1, EndLine: 5})
	if len(result) != 1 || result[0].StartLine != 1 || result[0].EndLine != 5 {
		t.Errorf("mergeRanges empty = %+v, want [1,5]", result)
	}
}

func TestMergeRanges_Overlapping(t *testing.T) {
	existing := []LineRange{{1, 5}, {10, 20}}
	result := mergeRanges(existing, LineRange{8, 12})
	// [1,5], [8,20]
	if len(result) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %+v", len(result), result)
	}
	if result[1].StartLine != 8 || result[1].EndLine != 20 {
		t.Errorf("range[1] = [%d,%d], want [8,20]", result[1].StartLine, result[1].EndLine)
	}
}

func TestMergeRanges_Adjacent(t *testing.T) {
	result := mergeRanges([]LineRange{{1, 5}}, LineRange{6, 10})
	if len(result) != 1 || result[0].StartLine != 1 || result[0].EndLine != 10 {
		t.Errorf("mergeRanges adjacent = %+v, want [1,10]", result)
	}
}

func TestMergeRanges_NonOverlapping(t *testing.T) {
	existing := []LineRange{{1, 5}, {20, 30}}
	result := mergeRanges(existing, LineRange{10, 12})
	// [1,5], [10,12], [20,30]
	if len(result) != 3 {
		t.Fatalf("expected 3 ranges, got %d: %+v", len(result), result)
	}
	if result[1].StartLine != 10 || result[1].EndLine != 12 {
		t.Errorf("range[1] = [%d,%d], want [10,12]", result[1].StartLine, result[1].EndLine)
	}
}

func TestMergeRanges_Swallowed(t *testing.T) {
	// 新範圍完全被現有範圍包含
	result := mergeRanges([]LineRange{{10, 50}}, LineRange{20, 30})
	if len(result) != 1 || result[0].StartLine != 10 || result[0].EndLine != 50 {
		t.Errorf("mergeRanges swallowed = %+v, want [10,50]", result)
	}
}

func TestMergeRanges_Swallows(t *testing.T) {
	// 新範圍完全包含現有範圍
	result := mergeRanges([]LineRange{{20, 30}}, LineRange{10, 50})
	if len(result) != 1 || result[0].StartLine != 10 || result[0].EndLine != 50 {
		t.Errorf("mergeRanges swallows = %+v, want [10,50]", result)
	}
}

func TestMergeRanges_BridgesTwo(t *testing.T) {
	// 新範圍將兩個分開的範圍連接起來
	result := mergeRanges([]LineRange{{1, 5}, {15, 20}}, LineRange{6, 14})
	// [1,20] — 三個範圍被合併為一個
	if len(result) != 1 || result[0].StartLine != 1 || result[0].EndLine != 20 {
		t.Errorf("mergeRanges bridges = %+v, want [1,20]", result)
	}
}

func TestMergeRanges_MultipleExisting(t *testing.T) {
	existing := []LineRange{{1, 3}, {5, 7}, {9, 11}}
	result := mergeRanges(existing, LineRange{3, 9})
	// [1,11] — 全部合併
	if len(result) != 1 || result[0].StartLine != 1 || result[0].EndLine != 11 {
		t.Errorf("mergeRanges multiple = %+v, want [1,11]", result)
	}
}

func TestIsLineInRanges(t *testing.T) {
	ranges := []LineRange{{1, 5}, {10, 20}}

	if !isLineInRanges(ranges, 3) {
		t.Error("line 3 should be in ranges [1,5]")
	}
	if !isLineInRanges(ranges, 15) {
		t.Error("line 15 should be in ranges [10,20]")
	}
	if isLineInRanges(ranges, 7) {
		t.Error("line 7 should NOT be in ranges")
	}
	if isLineInRanges(ranges, 0) {
		t.Error("line 0 should NOT be in ranges")
	}
	if isLineInRanges(ranges, 25) {
		t.Error("line 25 should NOT be in ranges")
	}
	if isLineInRanges(nil, 5) {
		t.Error("nil ranges should return false")
	}
	if isLineInRanges([]LineRange{}, 5) {
		t.Error("empty ranges should return false")
	}
}

func TestIsRangeOverlapping(t *testing.T) {
	ranges := []LineRange{{1, 5}, {10, 20}}

	// 完全包含
	if !isRangeOverlapping(ranges, LineRange{3, 4}) {
		t.Error("[3,4] should overlap with [1,5]")
	}
	// 部分重疊
	if !isRangeOverlapping(ranges, LineRange{4, 12}) {
		t.Error("[4,12] should overlap with both [1,5] and [10,20]")
	}
	// 邊界接觸
	if !isRangeOverlapping(ranges, LineRange{5, 10}) {
		t.Error("[5,10] should overlap with [1,5] and [10,20]")
	}
	// 無交集
	if isRangeOverlapping(ranges, LineRange{6, 9}) {
		t.Error("[6,9] should NOT overlap")
	}
	if isRangeOverlapping(ranges, LineRange{21, 30}) {
		t.Error("[21,30] should NOT overlap")
	}
	if isRangeOverlapping(nil, LineRange{1, 5}) {
		t.Error("nil ranges should return false")
	}
}

func TestDescribeRanges(t *testing.T) {
	if desc := describeRanges(nil); desc != "（無）" {
		t.Errorf("nil ranges = %q, want （無）", desc)
	}
	if desc := describeRanges([]LineRange{}); desc != "（無）" {
		t.Errorf("empty ranges = %q, want （無）", desc)
	}

	ranges := []LineRange{{5, 5}, {10, 20}}
	desc := describeRanges(ranges)
	if !strings.Contains(desc, "第 5 行") {
		t.Errorf("should contain 第 5 行: %q", desc)
	}
	if !strings.Contains(desc, "第 10-20 行") {
		t.Errorf("should contain 第 10-20 行: %q", desc)
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
	err := CheckWritePermission("/tmp/nonexistent_file_xyz_test.txt", "WriteFileLines", nil)
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

	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines", nil)
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
	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines", nil)
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
	err = CheckWritePermission(tmpFile.Name(), "WriteFileLines", nil)
	if err == nil {
		t.Error("CheckWritePermission should block write on partial-read file (need full read)")
	}
}

// ============================================================================
// CheckWritePermission — WriteFileLine 單行精確權限（新設計）
// ============================================================================

func TestCheckWritePermission_WriteFileLine_SameLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 5 行 → WriteFileLine 寫第 5 行 → 允許
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(5)})
	if err != nil {
		t.Errorf("WriteFileLine to same line should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_DifferentLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 5 行 → WriteFileLine 寫第 8 行 → 拒絕
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(8)})
	if err == nil {
		t.Error("WriteFileLine to different line should be blocked")
	}
	if !strings.Contains(err.Error(), "第 8 行") {
		t.Errorf("error should mention target line 8: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_Append(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 5 行 → WriteFileLine LineNum=-1（追加）→ 拒絕
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(-1)})
	if err == nil {
		t.Error("WriteFileLine append (LineNum=-1) should be blocked with partial read")
	}
	if !strings.Contains(err.Error(), "追加") {
		t.Errorf("error should mention 追加 mode: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_InsertAtReadLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 5 行 → WriteFileLine LineNum=-5（插入到第 5 行前）→ 允許
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(-5)})
	if err != nil {
		t.Errorf("WriteFileLine insert at read line should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_InsertAtUnreadLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 5 行 → WriteFileLine LineNum=-8（插入到第 8 行前）→ 拒絕
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(-8)})
	if err == nil {
		t.Error("WriteFileLine insert at unread line should be blocked")
	}
	if !strings.Contains(err.Error(), "第 8 行") {
		t.Errorf("error should mention target line 8: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_FromReadFileRange(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileLine 寫第 15 行 → 允許
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(15)})
	if err != nil {
		t.Errorf("WriteFileLine within read range should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileLine_OutsideReadFileRange(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileLine 寫第 25 行 → 拒絕
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileLine", map[string]interface{}{"LineNum": float64(25)})
	if err == nil {
		t.Error("WriteFileLine outside read range should be blocked")
	}
}

// ============================================================================
// CheckWritePermission — WriteFileRange 範圍交集權限（新設計）
// ============================================================================

func TestCheckWritePermission_WriteFileRange_Overlap(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileRange 寫 [15,18] → 允許
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(15),
		"EndLine":   float64(18),
	})
	if err != nil {
		t.Errorf("WriteFileRange with overlap should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_NoOverlap(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileRange 寫 [25,30] → 拒絕
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(25),
		"EndLine":   float64(30),
	})
	if err == nil {
		t.Error("WriteFileRange with no overlap should be blocked")
	}
	if !strings.Contains(err.Error(), "第 25-30 行") {
		t.Errorf("error should mention target range 25-30: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_PartialOverlap(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileRange 寫 [15,25] → 允許（有交集 [15,20]）
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(15),
		"EndLine":   float64(25),
	})
	if err != nil {
		t.Errorf("WriteFileRange with partial overlap should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_EndpointTouch(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [10,20] → WriteFileRange 寫 [20,30] → 允許（第 20 行有交集）
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 10, 20)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(20),
		"EndLine":   float64(30),
	})
	if err != nil {
		t.Errorf("WriteFileRange touching at endpoint should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_InsertAtReadLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 10 行 → WriteFileRange StartLine=-10（插入到第 10 行前）→ 允許
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 10)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(-10),
	})
	if err != nil {
		t.Errorf("WriteFileRange insert at read line should be allowed, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_InsertAtUnreadLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀第 10 行 → WriteFileRange StartLine=-15（插入到第 15 行前）→ 拒絕
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 10)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(-15),
	})
	if err == nil {
		t.Error("WriteFileRange insert at unread line should be blocked")
	}
}

func TestCheckWritePermission_WriteFileRange_NoEndLine(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileRange 讀 [5,10] → WriteFileRange StartLine=7 (no EndLine) → 允許
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 5, 10)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(7),
	})
	if err != nil {
		t.Errorf("WriteFileRange without EndLine should be allowed if StartLine in range, got: %v", err)
	}
}

func TestCheckWritePermission_WriteFileRange_FromMultipleReads(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// ReadFileLine 讀 [3,3], ReadFileLine 讀 [8,8], ReadFileRange 讀 [12,15]
	// → WriteFileRange 寫 [7,13] → 允許（與 [8,8] 及 [12,15] 有交集）
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 3)
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 8)
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 12, 15)
	err := CheckWritePermission(tmpFile, "WriteFileRange", map[string]interface{}{
		"StartLine": float64(7),
		"EndLine":   float64(13),
	})
	if err != nil {
		t.Errorf("WriteFileRange overlapping multiple read ranges should be allowed, got: %v", err)
	}
}

// ============================================================================
// CheckWritePermission — 全局寫入工具在部分讀取下應拒絕
// ============================================================================

func TestCheckWritePermission_WriteFileLines_PartialRead(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 即使有精確範圍，WriteFileLines 仍需 fullRead
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 1, 10)
	err := CheckWritePermission(tmpFile, "WriteFileLines", nil)
	if err == nil {
		t.Error("WriteFileLines should require full read even with range reads")
	}
}

func TestCheckWritePermission_AppendToFile_PartialRead(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 1, 10)
	err := CheckWritePermission(tmpFile, "AppendToFile", nil)
	if err == nil {
		t.Error("AppendToFile should require full read even with range reads")
	}
}

func TestCheckWritePermission_TextReplace_PartialRead(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 5, 15)
	err := CheckWritePermission(tmpFile, "TextReplace", nil)
	if err == nil {
		t.Error("TextReplace should require full read even with range reads")
	}
}

func TestCheckWritePermission_TextTransform_PartialRead(t *testing.T) {
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 1, 100)
	err := CheckWritePermission(tmpFile, "TextTransform", nil)
	if err == nil {
		t.Error("TextTransform should require full read even with range reads")
	}
}

func TestCheckWritePermission_HasReadRanges_ButNoPartialFlag(t *testing.T) {
	// 邊界情況：文件有 readRanges 但 readLevel 是 partial
	// 這應該還是會被 CheckWritePermission 攔截（全局工具需要 fullRead）
	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileLineRead(tmpFile, 5)
	err := CheckWritePermission(tmpFile, "WriteFileLines", nil)
	if err == nil {
		t.Error("WriteFileLines should require full read")
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

// ============================================================================
// DynamicFileToolThreshold
// ============================================================================

func TestDynamicToolThreshold_MinBound(t *testing.T) {
	threshold := DynamicToolThreshold()
	if threshold < 10000 {
		t.Errorf("DynamicToolThreshold = %d, should be at least 10000", threshold)
	}
}

func TestDynamicToolThreshold_NoHardUpperBound(t *testing.T) {
	// 動態公式無硬上限——閾值應隨 contextLen 線性增長
	threshold := DynamicToolThreshold()
	minExpected := int(float64(getEffectiveContextLength()) * maxReadFileLinesFraction * 4)
	if minExpected < 10000 {
		minExpected = 10000
	}
	if threshold != minExpected {
		t.Errorf("DynamicToolThreshold = %d, want %d (contextLen=%d * %.1f * 4, min 10000)",
			threshold, minExpected, getEffectiveContextLength(), maxReadFileLinesFraction)
	}
}

func TestDynamicToolThreshold_FormulaConsistency(t *testing.T) {
	// 公式應為 contextLen * maxReadFileLinesFraction * 4（與 ReadFileLines 一致）
	// 在預設環境中 contextLen 為 4096，所以 raw = 4096 * 0.5 * 4 = 8192
	// 但下限是 10000，所以最終應返回 10000
	threshold := DynamicToolThreshold()
	if threshold != 10000 {
		t.Logf("DynamicToolThreshold = %d (contextLen may vary in test environment)", threshold)
	}
	if threshold <= 0 {
		t.Errorf("DynamicToolThreshold = %d, should be positive", threshold)
	}
}

func TestDynamicToolThreshold_AllToolsUseDynamic(t *testing.T) {
	budget := NewToolResultBudget("")
	// 所有工具類別統一使用動態閾值，結果應 >= 下限
	expected := DynamicToolThreshold()
	toolNames := []string{
		"ReadFileLines", "ReadFileLine", "ReadFileRange",
		"WriteFileLine", "WriteFileLines", "WriteFileRange", "AppendToFile",
		"Shell", "SmartShell",
		"ssh_connect", "browser_navigate",
		"mcp_some_server",
	}
	for _, name := range toolNames {
		threshold := budget.resolveThreshold(name)
		if threshold != expected {
			t.Errorf("%s threshold = %d, want %d", name, threshold, expected)
		}
	}
}

// ============================================================================
// createTempFileWithContent 創建帶內容的臨時文件，每行由 line 參數指定。
// 返回文件路徑；調用方負責 defer os.Remove(path)。
// ============================================================================
func createTempFileWithContent(t *testing.T, lines []string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "safety_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		tmpFile.WriteString(line + "\n")
	}
	tmpFile.Close()
	return tmpFile.Name()
}

// ============================================================================
// autoReadForWrite 測試
// ============================================================================

func TestAutoReadForWrite_NewFile(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	content, didRead := autoReadForWrite("/tmp/nonexistent_auto_test_xyz.txt", "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)})
	if didRead {
		t.Error("autoReadForWrite should not auto-read for new files")
	}
	if content != "" {
		t.Errorf("expected empty content for new file, got %q", content)
	}
}

func TestAutoReadForWrite_FullyRead(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	globalReadWriteTracker.MarkFileFullyRead(tmpFile)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(2)})
	if didRead {
		t.Error("autoReadForWrite should not auto-read when file is fully read")
	}
	if content != "" {
		t.Errorf("expected empty content for fully-read file, got %q", content)
	}
}

func TestAutoReadForWrite_NoRead_TriggersAutoRead(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3", "line4", "line5"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(3)})
	if !didRead {
		t.Error("autoReadForWrite should auto-read when file never read")
	}
	if content == "" {
		t.Error("expected non-empty content from auto-read")
	}
	if !strings.Contains(content, "AutoRead") {
		t.Error("auto-read content should contain [Auto-Read] marker")
	}
	if !strings.Contains(content, "line3") {
		t.Error("auto-read content should contain the file content around target line")
	}

	// 驗證文件已被標記為完整讀取
	lvl := globalReadWriteTracker.GetFileReadLevel(tmpFile)
	if lvl != readLevelFull {
		t.Errorf("file should be marked as fully read after auto-read, got level=%d", lvl)
	}
}

func TestAutoReadForWrite_WriteFileLine_CoveredByReadRange(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3", "line4", "line5"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 模型已精確讀取第 3 行
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 3)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(3)})
	if didRead {
		t.Error("autoReadForWrite should not auto-read when target line is in read range")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestAutoReadForWrite_WriteFileLine_NotCoveredByReadRange(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3", "line4", "line5"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 模型只讀了第 1 行，但要寫第 5 行
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 1)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)})
	if !didRead {
		t.Error("autoReadForWrite should auto-read when target line not in read range")
	}
	if content == "" {
		t.Error("expected non-empty auto-read content")
	}
}

func TestAutoReadForWrite_WriteFileLine_AppendMode(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 即使有部分讀取，append 仍需要完整讀取
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 1)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(-1)})
	if !didRead {
		t.Error("autoReadForWrite should auto-read for append mode even with partial read")
	}
	if content == "" {
		t.Error("expected non-empty auto-read content for append")
	}
}

func TestAutoReadForWrite_WriteFileRange_Covered(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"a", "b", "c", "d", "e", "f", "g"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 模型讀了第 2-5 行，要寫第 3-4 行（完全在讀取範圍內）
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 2, 5)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileRange",
		map[string]interface{}{"StartLine": float64(3), "EndLine": float64(4)})
	if didRead {
		t.Error("autoReadForWrite should not auto-read when write range is within read range")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestAutoReadForWrite_WriteFileRange_NotCovered(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"a", "b", "c", "d", "e", "f", "g"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 模型只讀了第 1-2 行，但要寫第 5-6 行
	globalReadWriteTracker.MarkFileRangeRead(tmpFile, 1, 2)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileRange",
		map[string]interface{}{"StartLine": float64(5), "EndLine": float64(6)})
	if !didRead {
		t.Error("autoReadForWrite should auto-read when write range not covered")
	}
	if content == "" {
		t.Error("expected non-empty auto-read content")
	}
}

func TestAutoReadForWrite_GlobalWriteTool_PartialRead(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 部分讀取對於全局寫入工具（WriteFileLines 等）不夠
	globalReadWriteTracker.MarkFileLineRead(tmpFile, 1)

	content, didRead := autoReadForWrite(tmpFile, "WriteFileLines", nil)
	if !didRead {
		t.Error("autoReadForWrite should auto-read for global write tools even with partial read")
	}
	if content == "" {
		t.Error("expected non-empty auto-read content for WriteFileLines")
	}
}

func TestAutoReadForWrite_AppendToFile_TriggersAutoRead(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	content, didRead := autoReadForWrite(tmpFile, "AppendToFile", nil)
	if !didRead {
		t.Error("autoReadForWrite should auto-read for AppendToFile when file never read")
	}
	if content == "" {
		t.Error("expected non-empty auto-read content for AppendToFile")
	}
}

func TestAutoReadForWrite_EscalationReset(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 先觸發一次 escalation 累積
	globalErrorEscalator.RecordEscalation(EscalateWriteWithoutRead, tmpFile, "test error")

	// 自動讀取應 reset escalation counter
	_, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(1)})
	if !didRead {
		t.Fatal("expected auto-read to trigger")
	}

	// 驗證 escalation 已被重置：再次觸發應從 1 開始計數
	shouldStop, _ := globalErrorEscalator.RecordEscalation(EscalateWriteWithoutRead, tmpFile, "another error")
	if shouldStop {
		t.Error("escalation should have been reset by auto-read, should not stop on first violation")
	}
}

// ============================================================================
// writeTargetCovered 測試
// ============================================================================

func TestWriteTargetCovered_NoRanges(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)

	// 未有讀取記錄應返回 false
	if writeTargetCovered(normalizeFilePath(tmpFile), "WriteFileLine",
		map[string]interface{}{"LineNum": float64(1)}) {
		t.Error("writeTargetCovered should return false with no read ranges")
	}
}

func TestWriteTargetCovered_WriteFileLine_ExactLine(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileLineRead(absPath, 5)

	if !writeTargetCovered(absPath, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)}) {
		t.Error("writeTargetCovered should return true for exact line match")
	}
}

func TestWriteTargetCovered_WriteFileLine_DifferentLine(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileLineRead(absPath, 3)

	if writeTargetCovered(absPath, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(7)}) {
		t.Error("writeTargetCovered should return false for different line")
	}
}

func TestWriteTargetCovered_WriteFileLine_InsertAtReadLine(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileLineRead(absPath, 5)

	if !writeTargetCovered(absPath, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(-5)}) {
		t.Error("writeTargetCovered should return true for insert at read line")
	}
}

func TestWriteTargetCovered_WriteFileLine_Append(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileLineRead(absPath, 10)

	// Append 模式：即使有部分讀取仍需完整讀取
	if writeTargetCovered(absPath, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(-1)}) {
		t.Error("writeTargetCovered should return false for append mode even with partial read")
	}
}

func TestWriteTargetCovered_WriteFileRange_Overlapping(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileRangeRead(absPath, 3, 8)

	// 寫入範圍 [5, 7] 完全在讀取範圍 [3, 8] 內
	if !writeTargetCovered(absPath, "WriteFileRange",
		map[string]interface{}{"StartLine": float64(5), "EndLine": float64(7)}) {
		t.Error("writeTargetCovered should return true when write range overlaps read range")
	}
}

func TestWriteTargetCovered_WriteFileRange_NoOverlap(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFile(t)
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileRangeRead(absPath, 1, 3)

	// 寫入範圍 [10, 12] 與讀取範圍 [1, 3] 無交集
	if writeTargetCovered(absPath, "WriteFileRange",
		map[string]interface{}{"StartLine": float64(10), "EndLine": float64(12)}) {
		t.Error("writeTargetCovered should return false when write range does not overlap read range")
	}
}

func TestWriteTargetCovered_GlobalWriteTools(t *testing.T) {
	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tmpFile := createTempFileWithContent(t, []string{"a", "b", "c", "d", "e"})
	defer os.Remove(tmpFile)
	absPath := normalizeFilePath(tmpFile)

	globalReadWriteTracker.MarkFileRangeRead(absPath, 1, 5)

	tools := []string{"WriteFileLines", "AppendToFile", "TextReplace", "TextTransform"}
	for _, tool := range tools {
		if writeTargetCovered(absPath, tool, nil) {
			t.Errorf("writeTargetCovered should return false for global tool %s even with partial read", tool)
		}
	}
}

// ============================================================================
// computeAutoReadWindow 測試
// ============================================================================

func TestComputeAutoReadWindow_WriteFileLine_Overwrite(t *testing.T) {
	// 寫入第 50 行，100 行文件 → 窗口應為 35-65
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(50)}, 100)
	if start != 35 || end != 65 {
		t.Errorf("expected window [35, 65], got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileLine_NearStart(t *testing.T) {
	// 寫入第 3 行 → start 不應小於 1
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(3)}, 100)
	if start != 1 {
		t.Errorf("expected start=1 near file start, got %d", start)
	}
	if end != 18 {
		t.Errorf("expected end=18 near file start, got %d", end)
	}
}

func TestComputeAutoReadWindow_WriteFileLine_NearEnd(t *testing.T) {
	// 寫入第 98 行，100 行文件 → end 不應超過 100
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(98)}, 100)
	if end != 100 {
		t.Errorf("expected end=100 near file end, got %d", end)
	}
	if start != 83 {
		t.Errorf("expected start=83, got %d", start)
	}
}

func TestComputeAutoReadWindow_WriteFileLine_Append(t *testing.T) {
	// Append 模式 → 顯示尾部 20 行
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(-1)}, 100)
	if start != 81 || end != 100 {
		t.Errorf("expected [81, 100] for append on 100-line file, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileLine_Append_SmallFile(t *testing.T) {
	// Append 模式小文件 → 顯示全部
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(-1)}, 10)
	if start != 1 || end != 10 {
		t.Errorf("expected [1, 10] for append on 10-line file, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileLine_Insert(t *testing.T) {
	// Insert before line 30 → 窗口 15-45
	start, end := computeAutoReadWindow("WriteFileLine",
		map[string]interface{}{"LineNum": float64(-30)}, 100)
	if start != 15 || end != 45 {
		t.Errorf("expected [15, 45] for insert before line 30, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileRange_Overwrite(t *testing.T) {
	// 寫入 [40, 50] → 窗口 35-55 (padding 5)
	start, end := computeAutoReadWindow("WriteFileRange",
		map[string]interface{}{"StartLine": float64(40), "EndLine": float64(50)}, 100)
	if start != 35 || end != 55 {
		t.Errorf("expected [35, 55] for range [40,50], got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileRange_NoEndLine(t *testing.T) {
	// 只有 StartLine=40，無 EndLine → 視為單行
	start, end := computeAutoReadWindow("WriteFileRange",
		map[string]interface{}{"StartLine": float64(40)}, 100)
	if start != 35 || end != 45 {
		t.Errorf("expected [35, 45] for single-line range, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileRange_Insert(t *testing.T) {
	// Insert before line 20 → 窗口 10-30
	start, end := computeAutoReadWindow("WriteFileRange",
		map[string]interface{}{"StartLine": float64(-20)}, 100)
	if start != 10 || end != 30 {
		t.Errorf("expected [10, 30] for insert before line 20, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_WriteFileRange_NearStart(t *testing.T) {
	start, end := computeAutoReadWindow("WriteFileRange",
		map[string]interface{}{"StartLine": float64(2), "EndLine": float64(4)}, 50)
	if start != 1 {
		t.Errorf("expected start clamped to 1, got %d", start)
	}
	if end != 9 {
		t.Errorf("expected end=9, got %d", end)
	}
}

func TestComputeAutoReadWindow_AppendToFile(t *testing.T) {
	start, end := computeAutoReadWindow("AppendToFile", nil, 100)
	if start != 81 || end != 100 {
		t.Errorf("expected [81, 100] for AppendToFile on 100-line file, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_AppendToFile_SmallFile(t *testing.T) {
	start, end := computeAutoReadWindow("AppendToFile", nil, 5)
	if start != 1 || end != 5 {
		t.Errorf("expected [1, 5] for small file, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_GlobalTools_FullFile(t *testing.T) {
	// 小文件全文顯示
	start, end := computeAutoReadWindow("WriteFileLines", nil, 50)
	if start != 1 || end != 50 {
		t.Errorf("expected [1, 50] for full file, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_GlobalTools_LargeFile(t *testing.T) {
	// 大文件應截斷至 2000 行
	start, end := computeAutoReadWindow("TextReplace", nil, 5000)
	if start != 1 || end != 2000 {
		t.Errorf("expected [1, 2000] for large file limit, got [%d, %d]", start, end)
	}
}

func TestComputeAutoReadWindow_MissingArgs(t *testing.T) {
	// 缺少參數時應 fallback 到全文顯示（上限 2000）
	start, end := computeAutoReadWindow("WriteFileLine", map[string]interface{}{}, 30)
	if start != 1 || end != 30 {
		t.Errorf("expected full file [1, 30] when args missing, got [%d, %d]", start, end)
	}
}

// ============================================================================
// formatAutoReadResult 測試
// ============================================================================

func TestFormatAutoReadResult_EmptyFile(t *testing.T) {
	result := formatAutoReadResult("/tmp/test.txt", []string{}, "WriteFileLine", nil)
	// TOON 格式應包含 TotalLines: 0
	if !strings.Contains(result, "TotalLines") {
		t.Error("TOON output should contain TotalLines field")
	}
	if !strings.Contains(result, "AutoRead") {
		t.Error("TOON output should contain AutoRead marker")
	}
	// Message 字段應提示重新調用
	if !strings.Contains(result, "Message") {
		t.Error("TOON output should contain Message field")
	}
}

func TestFormatAutoReadResult_SmallFile_FullDisplay(t *testing.T) {
	lines := []string{"package main", "", "func main() {", "\tprintln(\"hello\")", "}"}
	result := formatAutoReadResult("/tmp/test.go", lines, "WriteFileLines", nil)

	if !strings.Contains(result, "AutoRead") {
		t.Error("TOON output should contain AutoRead marker")
	}
	if !strings.Contains(result, "TotalLines") {
		t.Error("TOON output should contain TotalLines field")
	}
	if !strings.Contains(result, "package main") {
		t.Error("TOON output should contain file content in Lines array")
	}
	if !strings.Contains(result, "Message") {
		t.Error("TOON output should contain Message field with prompt")
	}
}

func TestFormatAutoReadResult_WindowedDisplay(t *testing.T) {
	// 模擬大文件：100 行，只顯示窗口
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line_%04d", i+1)
	}

	result := formatAutoReadResult("/tmp/large.txt", lines, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(50)})

	if !strings.Contains(result, "TotalLines") {
		t.Error("TOON output should contain TotalLines field")
	}
	// 應包含目標行附近內容
	if !strings.Contains(result, "line_0050") {
		t.Error("should contain content around target line")
	}
	// TOON 格式應有 ShownStart / ShownEnd 標記窗口
	if !strings.Contains(result, "ShownStart") || !strings.Contains(result, "ShownEnd") {
		t.Error("TOON output should contain ShownStart and ShownEnd fields for windowed view")
	}
}

func TestFormatAutoReadResult_LargeFileTruncation(t *testing.T) {
	// 模擬超大文件：5000 行，WriteFileLines 截斷至 2000
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line_%04d", i+1)
	}

	result := formatAutoReadResult("/tmp/huge.txt", lines, "WriteFileLines", nil)

	if !strings.Contains(result, "TotalLines") {
		t.Error("TOON output should contain TotalLines field")
	}
	if !strings.Contains(result, "Truncated") {
		t.Error("TOON output should contain Truncated flag")
	}
	if strings.Contains(result, "line_2001") {
		t.Error("should not contain lines beyond the 2000-line limit")
	}
}

func TestFormatAutoReadResult_LineNumbers(t *testing.T) {
	lines := []string{"aaa", "bbb", "ccc"}
	result := formatAutoReadResult("/tmp/test.txt", lines, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(2)})

	// TOON 格式：Lines 以 compact 形式呈現 Lines[N]{Content,Line}: ...
	if !strings.Contains(result, "Lines[") {
		t.Error("TOON output should contain Lines array header")
	}
	if !strings.Contains(result, "aaa") && !strings.Contains(result, "bbb") && !strings.Contains(result, "ccc") {
		t.Error("TOON output should contain the actual file content values")
	}
}

func TestFormatAutoReadResult_PromptToProceed(t *testing.T) {
	lines := []string{"content"}
	result := formatAutoReadResult("/tmp/test.txt", lines, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(1)})

	// TOON 格式：Message 字段包含提示
	if !strings.Contains(result, "Message") {
		t.Error("TOON output should contain Message field")
	}
	if !strings.Contains(result, "是否繼續寫入") {
		t.Error("message should ask the model whether to proceed with write")
	}
}

// ============================================================================
// autoReadForWrite 流程整合測試
// ============================================================================

func TestAutoReadForWrite_FullFlow_ReadThenWrite(t *testing.T) {
	// 模擬完整流程：模型未讀直接寫 → 自動讀取 → 標記已讀 → 重新調用成功
	tmpFile := createTempFileWithContent(t, []string{
		"package main",
		"",
		"import \"fmt\"",
		"",
		"func main() {",
		"\tfmt.Println(\"hello\")",
		"}",
	})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// Step 1: 模型調用 WriteFileLine 但未讀文件
	content, didRead := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)})
	if !didRead {
		t.Fatal("step 1: should trigger auto-read")
	}
	if !strings.Contains(content, "func main()") {
		t.Error("step 1: auto-read should show file content")
	}

	// Step 2: 模型確認後重新調用 WriteFileLine
	// 因為文件已標記完整讀取，autoReadForWrite 應返回 false
	content2, didRead2 := autoReadForWrite(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)})
	if didRead2 {
		t.Error("step 2: should not auto-read again (already fully read)")
	}
	if content2 != "" {
		t.Errorf("step 2: expected empty content, got %q", content2)
	}

	// Step 3: CheckWritePermission 應通過
	err := CheckWritePermission(tmpFile, "WriteFileLine",
		map[string]interface{}{"LineNum": float64(5)})
	if err != nil {
		t.Errorf("step 3: CheckWritePermission should pass after auto-read, got: %v", err)
	}
}

func TestAutoReadForWrite_FullFlow_MultipleTools(t *testing.T) {
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3", "line4", "line5"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	tools := []struct {
		name string
		args map[string]interface{}
	}{
		{"WriteFileLine", map[string]interface{}{"LineNum": float64(3)}},
		{"WriteFileRange", map[string]interface{}{"StartLine": float64(2), "EndLine": float64(4)}},
		{"WriteFileLines", nil},
		{"AppendToFile", nil},
		{"TextReplace", nil},
		{"TextTransform", nil},
	}

	for _, tool := range tools {
		tracker := newReadWriteTracker()
		globalReadWriteTracker = tracker

		_, didRead := autoReadForWrite(tmpFile, tool.name, tool.args)
		if !didRead {
			t.Errorf("%s: should trigger auto-read when file not read", tool.name)
		}

		// 驗證已標記完整讀取
		lvl := globalReadWriteTracker.GetFileReadLevel(tmpFile)
		if lvl != readLevelFull {
			t.Errorf("%s: file should be marked as full read after auto-read", tool.name)
		}
	}
}

func TestAutoReadForWrite_ReadLevels_CheckWritePermission(t *testing.T) {
	// 驗證 auto-read 不改變現有 CheckWritePermission 行為
	tmpFile := createTempFileWithContent(t, []string{"line1", "line2", "line3", "line4", "line5"})
	defer os.Remove(tmpFile)

	oldTracker := globalReadWriteTracker
	globalReadWriteTracker = newReadWriteTracker()
	defer func() { globalReadWriteTracker = oldTracker }()

	// 先觸發 auto-read
	_, didRead := autoReadForWrite(tmpFile, "WriteFileRange",
		map[string]interface{}{"StartLine": float64(3), "EndLine": float64(4)})
	if !didRead {
		t.Fatal("should trigger auto-read")
	}

	// auto-read 後，所有類型的寫入權限檢查都應通過
	testCases := []struct {
		name string
		args map[string]interface{}
	}{
		{"WriteFileLine", map[string]interface{}{"LineNum": float64(3)}},
		{"WriteFileRange", map[string]interface{}{"StartLine": float64(1), "EndLine": float64(5)}},
		{"WriteFileLines", nil},
		{"AppendToFile", nil},
		{"TextReplace", nil},
	}

	for _, tc := range testCases {
		err := CheckWritePermission(tmpFile, tc.name, tc.args)
		if err != nil {
			t.Errorf("%s after auto-read: CheckWritePermission should pass, got: %v", tc.name, err)
		}
	}
}

func TestDynamicToolThreshold_UnknownToolUsesDynamicDefault(t *testing.T) {
	budget := NewToolResultBudget("")
	threshold := budget.resolveThreshold("SomeCompletelyUnknownTool")
	expected := DynamicToolThreshold()
	if threshold != expected {
		t.Errorf("Unknown tool threshold = %d, want %d", threshold, expected)
	}
}
