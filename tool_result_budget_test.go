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

	// 所有工具使用動態閾值，在測試環境中為 10000（下限）
	dynamicThreshold := DynamicToolThreshold()

	// SSHExec 匹配 "ssh" prefix，使用動態閾值
	threshold := budget.resolveThreshold("SSHExec")
	if threshold != dynamicThreshold {
		t.Errorf("SSHExec threshold = %d, want dynamic %d", threshold, dynamicThreshold)
	}

	// 輸出 9999 chars — 應該不截斷（低於 10000）
	content := strings.Repeat("e", 9999)
	result := budget.CheckAndPersistResult("SSHExec", content)
	if result != content {
		t.Error("SSHExec 9999 chars: should return unchanged (below dynamic threshold)")
	}

	// 輸出 10001 chars — 應該截斷並存檔
	content2 := strings.Repeat("f", 10001)
	result2 := budget.CheckAndPersistResult("SSHExec", content2)
	if !strings.Contains(result2, "[TOOL_RESULT_TRUNCATED]") {
		t.Error("SSHExec 10001 chars: should be truncated (above dynamic threshold)")
	}
}

func TestCheckAndPersistResult_ShellToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// SmartShell 匹配 "SmartShell" prefix，使用動態閾值
	dynamicThreshold := DynamicToolThreshold()
	for _, tool := range []string{"SmartShell"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != dynamicThreshold {
			t.Errorf("%s threshold = %d, want dynamic %d", tool, threshold, dynamicThreshold)
		}
	}
}

func TestCheckAndPersistResult_SSHAliasThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	dynamicThreshold := DynamicToolThreshold()
	for _, tool := range []string{"ssh", "SSHConnect", "SSHExec"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != dynamicThreshold {
			t.Errorf("%s threshold = %d, want dynamic %d", tool, threshold, dynamicThreshold)
		}
	}
}

func TestCheckAndPersistResult_FileToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	dynamicThreshold := DynamicToolThreshold()
	tools := []string{
		"ReadFileLines", "ReadFileLine", "ReadFileRange",
		"WriteFileLines", "WriteFileLine", "WriteFileRange",
		"AppendToFile",
	}
	for _, tool := range tools {
		got := budget.resolveThreshold(tool)
		if got != dynamicThreshold {
			t.Errorf("%s threshold = %d, want dynamic %d", tool, got, dynamicThreshold)
		}
	}
}

func TestCheckAndPersistResult_BrowserToolThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	dynamicThreshold := DynamicToolThreshold()
	for _, tool := range []string{"browser", "BrowserVisit", "BrowserScreenshot"} {
		threshold := budget.resolveThreshold(tool)
		if threshold != dynamicThreshold {
			t.Errorf("%s threshold = %d, want dynamic %d", tool, threshold, dynamicThreshold)
		}
	}
}

func TestCheckAndPersistResult_DefaultThreshold(t *testing.T) {
	budget := NewToolResultBudget(t.TempDir())

	// 未知工具也使用動態閾值
	dynamicThreshold := DynamicToolThreshold()
	threshold := budget.resolveThreshold("SomeUnknownTool")
	if threshold != dynamicThreshold {
		t.Errorf("unknown tool threshold = %d, want dynamic %d", threshold, dynamicThreshold)
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

	// 移除覆蓋後應恢復為動態閾值
	budget.SetToolThreshold("SSHExec", 0)
	threshold = budget.resolveThreshold("SSHExec")
	dynamicThreshold := DynamicToolThreshold()
	if threshold != dynamicThreshold {
		t.Errorf("after removing override, SshExec threshold = %d, want dynamic %d", threshold, dynamicThreshold)
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
