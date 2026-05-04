package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// getMaxDirectOutput
// ============================================================================

// ============================================================================
// getMaxDirectOutput
// ============================================================================

func TestGetMaxDirectOutput_AlwaysDynamic(t *testing.T) {
	// getMaxDirectOutput 始終返回 DynamicToolThreshold()，與 ReadFileLines 一致
	got := getMaxDirectOutput()
	want := DynamicToolThreshold()
	if got != want {
		t.Errorf("getMaxDirectOutput() = %d, want %d (DynamicToolThreshold)", got, want)
	}
}

func TestGetMaxDirectOutput_MinThreshold(t *testing.T) {
	// 動態閾值不應低於 DynamicToolThreshold 的下限 (10000)
	if got := getMaxDirectOutput(); got < 10000 {
		t.Errorf("getMaxDirectOutput() = %d, want >= 10000", got)
	}
}


// ============================================================================
// saveOutputToFile
// ============================================================================

func TestSaveOutputToFile_BelowDynamicThreshold(t *testing.T) {
	oldExecDir := globalExecDir
	defer func() { globalExecDir = oldExecDir }()

	globalExecDir = t.TempDir()

	// 5000 字遠小於動態閾值下限 (10000)，不應保存到文件
	content := strings.Repeat("a", 5000)
	filePath, err := saveOutputToFile(content, "test", "echo hello")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath != "" {
		t.Errorf("saveOutputToFile() should return empty path for content below dynamic threshold, got: %s", filePath)
	}
}

func TestSaveOutputToFile_AboveDynamicThreshold(t *testing.T) {
	oldExecDir := globalExecDir
	defer func() { globalExecDir = oldExecDir }()

	globalExecDir = t.TempDir()

	// 15000 字超過動態閾值下限 (10000)，應保存到文件
	content := strings.Repeat("b", 15000)
	filePath, err := saveOutputToFile(content, "stdout", "ls -la")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath == "" {
		t.Fatal("saveOutputToFile() should return a file path for content exceeding dynamic threshold")
	}

	// 驗證文件存在且內容正確
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch, got len=%d, want len=%d", len(data), len(content))
	}
}

func TestSaveOutputToFile_PrefixInFilename(t *testing.T) {
	oldExecDir := globalExecDir
	defer func() { globalExecDir = oldExecDir }()

	globalExecDir = t.TempDir()

	content := strings.Repeat("e", 15000)
	filePath, err := saveOutputToFile(content, "my_prefix", "cmd")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if !strings.Contains(filepath.Base(filePath), "my_prefix") {
		t.Errorf("filename should contain prefix: %s", filePath)
	}
}

func TestSaveOutputToFile_OutputDirCreated(t *testing.T) {
	oldExecDir := globalExecDir
	defer func() { globalExecDir = oldExecDir }()

	globalExecDir = t.TempDir()

	// output 目錄唔存在，函數應該自動創建
	content := strings.Repeat("f", 15000)
	filePath, err := saveOutputToFile(content, "test", "echo")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}

	// 確保輸出目錄被創建
	outputDir := filepath.Join(globalExecDir, "output")
	if info, err := os.Stat(outputDir); err != nil || !info.IsDir() {
		t.Error("output directory should be created")
	}

	// 文件在 output 目錄入面
	if !strings.Contains(filePath, "output") {
		t.Errorf("file should be in output dir: %s", filePath)
	}
}
