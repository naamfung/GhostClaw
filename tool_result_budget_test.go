package main

import (
	"os"
	"strings"
	"testing"
)

// ============================================================================
// ToolResultBudget - CheckAndPersistResult 單元測試
// ============================================================================

func TestCheckAndPersistResult_BelowThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(100)

	result := budget.CheckAndPersistResult("unknown_tool", strings.Repeat("a", 50))
	if result != strings.Repeat("a", 50) {
		t.Error("below threshold: should return original content unchanged")
	}
}

func TestCheckAndPersistResult_AtExactThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(100)
	budget.SetToolThreshold("custom_tool", 100)

	content := strings.Repeat("b", 100)
	result := budget.CheckAndPersistResult("custom_tool", content)
	if result != content {
		t.Error("at exact threshold: should return original content unchanged")
	}
}

func TestCheckAndPersistResult_JustAboveThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(100)

	content := strings.Repeat("c", 101)
	result := budget.CheckAndPersistResult("mytool", content)

	if result == content {
		t.Fatal("just above threshold: should NOT return original content unchanged")
	}
	if !strings.Contains(result, "[TOOL_RESULT_TRUNCATED]") {
		t.Error("just above threshold: preview should contain truncation marker")
	}
	if !strings.Contains(result, "Cached to:") {
		t.Error("just above threshold: preview should contain cache path")
	}
}

func TestCheckAndPersistResult_WellAboveThreshold_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	budget := NewToolResultBudget(dir)
	budget.SetDefaultThreshold(10)

	content := strings.Repeat("x", 500)
	result := budget.CheckAndPersistResult("TestTool", content)

	if !strings.Contains(result, "[TOOL_RESULT_TRUNCATED]") {
		t.Fatal("well above threshold: preview should contain truncation marker")
	}

	// 提取快取路徑並驗證文件內容
	lines := strings.Split(result, "\n")
	var cachePath string
	for _, line := range lines {
		if strings.HasPrefix(line, "Cached to: ") {
			cachePath = strings.TrimPrefix(line, "Cached to: ")
			break
		}
	}
	if cachePath == "" {
		t.Fatal("well above threshold: preview should have Cached to: line")
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("well above threshold: failed to read cached file: %v", err)
	}
	if string(data) != content {
		t.Errorf("well above threshold: cached content = %q, want original content", string(data))
	}
}

func TestCheckAndPersistResult_SSHExecThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// SSHExec 應該匹配 "ssh" prefix，threshold = 15000
	threshold := budget.resolveThreshold("SSHExec")
	if threshold != ShellToolThreshold {
		t.Errorf("SSHExec threshold = %d, want %d", threshold, ShellToolThreshold)
	}

	// SSHExec 輸出 14999 chars — 應該不截斷
	content := strings.Repeat("e", 14999)
	result := budget.CheckAndPersistResult("SSHExec", content)
	if result != content {
		t.Error("SSHExec 14999 chars: should return unchanged (below 15000 threshold)")
	}

	// SSHExec 輸出 15001 chars — 應該截斷並存檔
	content2 := strings.Repeat("f", 15001)
	result2 := budget.CheckAndPersistResult("SSHExec", content2)
	if !strings.Contains(result2, "[TOOL_RESULT_TRUNCATED]") {
		t.Error("SSHExec 15001 chars: should be truncated (above 15000 threshold)")
	}
}

func TestCheckAndPersistResult_ShellToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// Shell, SmartShell 都應該匹配 "Shell" prefix，threshold = 15000
	for _, tool := range []string{"Shell", "SmartShell"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != ShellToolThreshold {
			t.Errorf("%s threshold = %d, want %d", tool, threshold, ShellToolThreshold)
		}
	}
}

func TestCheckAndPersistResult_SSHAliasThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// 所有 ssh 前綴工具都應該匹配 15000
	for _, tool := range []string{"ssh", "SSHConnect", "SSHExec"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != ShellToolThreshold {
			t.Errorf("%s threshold = %d, want %d", tool, threshold, ShellToolThreshold)
		}
	}
}

func TestCheckAndPersistResult_FileToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	tests := []struct {
		tool      string
		threshold int
	}{
		{"ReadFileLines", FileToolThreshold},
		{"ReadFileLine", FileToolThreshold},
		{"ReadFileRange", FileToolThreshold},
		{"WriteFileLines", FileToolThreshold},
		{"WriteFileLine", FileToolThreshold},
		{"WriteFileRange", FileToolThreshold},
		{"AppendToFile", FileToolThreshold},
	}

	for _, tt := range tests {
		got := budget.resolveThreshold(tt.tool)
		if got != tt.threshold {
			t.Errorf("%s threshold = %d, want %d", tt.tool, got, tt.threshold)
		}
	}
}

func TestCheckAndPersistResult_BrowserToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	for _, tool := range []string{"browser", "BrowserVisit", "BrowserScreenshot"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != BrowserToolThreshold {
			t.Errorf("%s threshold = %d, want %d", tool, threshold, BrowserToolThreshold)
		}
	}
}

func TestCheckAndPersistResult_DefaultThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// 未知工具應該使用默認閾值
	threshold := budget.resolveThreshold("SomeUnknownTool")
	if threshold != DefaultToolResultThreshold {
		t.Errorf("unknown tool threshold = %d, want %d", threshold, DefaultToolResultThreshold)
	}
}

func TestCheckAndPersistResult_PreviewFormat(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(10)

	// 內容必須足夠長以確保 head 和 tail preview 都會顯示（> PreviewHeadChars + PreviewTailChars = 2500）
	content := "LINE_ONE\nLINE_TWO\n" + strings.Repeat("data ", 1000) // > 2500 chars
	result := budget.CheckAndPersistResult("ProbeTool", content)

	requiredMarkers := []string{
		"[TOOL_RESULT_TRUNCATED]",
		"Tool: ProbeTool",
		"Original size:",
		"Threshold:",
		"Cached to:",
		"--- Preview (first",
		"--- Preview (last",
		"Full result available at:",
	}

	for _, marker := range requiredMarkers {
		if !strings.Contains(result, marker) {
			t.Errorf("preview should contain %q", marker)
		}
	}
}

func TestCheckAndPersistResult_HeadTailPreview(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(10)

	// 創建一個帶有明確頭尾特徵的內容
	head := "HEAD_MARKER_12345"
	tail := "TAIL_MARKER_67890"
	middle := strings.Repeat("m", 5000)
	content := head + middle + tail

	result := budget.CheckAndPersistResult("PreviewTest", content)

	// Head preview 應該包含頭部特徵
	if !strings.Contains(result, head) {
		t.Error("preview should contain head marker in head preview section")
	}
	// Tail preview 應該包含尾部特徵
	if !strings.Contains(result, tail) {
		t.Error("preview should contain tail marker in tail preview section")
	}
}

func TestCheckAndPersistResult_SetToolThresholdOverride(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// 手動覆蓋 SSHExec 的阈值
	budget.SetToolThreshold("SSHExec", 5000)

	threshold := budget.resolveThreshold("SSHExec")
	if threshold != 5000 {
		t.Errorf("overridden SSHExec threshold = %d, want 5000", threshold)
	}

	// 移除覆蓋後應恢復為 prefix match
	budget.SetToolThreshold("SSHExec", 0)
	threshold = budget.resolveThreshold("SSHExec")
	if threshold != ShellToolThreshold {
		t.Errorf("after removing override, SshExec threshold = %d, want %d", threshold, ShellToolThreshold)
	}
}

func TestCheckAndPersistResult_SetDefaultThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(500)

	threshold := budget.resolveThreshold("BrandNewTool")
	if threshold != 500 {
		t.Errorf("custom default threshold = %d, want 500", threshold)
	}
}

func TestCheckAndPersistResult_UnicodeCharacters(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())
	budget.SetDefaultThreshold(10)

	// 使用中文字符測試 rune 計數
	content := strings.Repeat("中文測試", 200) // 800 runes
	result := budget.CheckAndPersistResult("UnicodeTool", content)

	if !strings.Contains(result, "[TOOL_RESULT_TRUNCATED]") {
		t.Fatal("unicode content above threshold: should be truncated")
	}
	if !strings.Contains(result, "chars") {
		t.Error("preview should show char count correctly for unicode")
	}
}
