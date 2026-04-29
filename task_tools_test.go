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

func TestGetMaxDirectOutput_Default(t *testing.T) {
	// 保存並恢復全局狀態
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	globalToolsConfig.SmartShell.MaxDirectOutput = 0

	if got := getMaxDirectOutput(); got != DefaultMaxDirectOutput {
		t.Errorf("getMaxDirectOutput() = %d, want %d (DefaultMaxDirectOutput)", got, DefaultMaxDirectOutput)
	}
}

func TestGetMaxDirectOutput_ConfigOverride(t *testing.T) {
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	globalToolsConfig.SmartShell.MaxDirectOutput = 500

	if got := getMaxDirectOutput(); got != 500 {
		t.Errorf("getMaxDirectOutput() = %d, want 500", got)
	}
}

func TestGetMaxDirectOutput_ExplorePhase(t *testing.T) {
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	defer resetGlobalPlanMode()

	// 進入 Plan Mode Explore Phase
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseExplore
	globalPlanMode.mu.Unlock()

	globalToolsConfig.SmartShell.MaxDirectOutput = 0

	if got := getMaxDirectOutput(); got != PlanModeExploreMaxDirectOutput {
		t.Errorf("getMaxDirectOutput() in Explore Phase = %d, want %d", got, PlanModeExploreMaxDirectOutput)
	}
}

func TestGetMaxDirectOutput_ExplorePhaseOverridesConfig(t *testing.T) {
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	defer resetGlobalPlanMode()

	// 進入 Plan Mode Explore Phase
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseExplore
	globalPlanMode.mu.Unlock()

	// 配置設置為 500，但探索階段應該返回 2000
	globalToolsConfig.SmartShell.MaxDirectOutput = 500

	if got := getMaxDirectOutput(); got != PlanModeExploreMaxDirectOutput {
		t.Errorf("getMaxDirectOutput() in Explore Phase with config=500 = %d, want %d", got, PlanModeExploreMaxDirectOutput)
	}
}

func TestGetMaxDirectOutput_DesignPhase(t *testing.T) {
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	defer resetGlobalPlanMode()

	// 進入 Plan Mode Design Phase (Phase 2)
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseDesign
	globalPlanMode.mu.Unlock()

	globalToolsConfig.SmartShell.MaxDirectOutput = 0

	if got := getMaxDirectOutput(); got != DefaultMaxDirectOutput {
		t.Errorf("getMaxDirectOutput() in Design Phase = %d, want %d (default)", got, DefaultMaxDirectOutput)
	}
}

func TestGetMaxDirectOutput_ExecutePhase(t *testing.T) {
	old := globalToolsConfig
	defer func() { globalToolsConfig = old }()
	defer resetGlobalPlanMode()

	// 進入 Plan Mode Execute Phase (Phase 3)
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseExecute
	globalPlanMode.mu.Unlock()

	globalToolsConfig.SmartShell.MaxDirectOutput = 0

	if got := getMaxDirectOutput(); got != DefaultMaxDirectOutput {
		t.Errorf("getMaxDirectOutput() in Execute Phase = %d, want %d (default)", got, DefaultMaxDirectOutput)
	}
}

// ============================================================================
// saveOutputToFile
// ============================================================================

func TestSaveOutputToFile_BelowThreshold(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 1000
	globalExecDir = t.TempDir()

	content := strings.Repeat("a", 500) // 500 < 1000 threshold
	filePath, err := saveOutputToFile(content, "test", "echo hello")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath != "" {
		t.Errorf("saveOutputToFile() should return empty path for short content, got: %s", filePath)
	}
}

func TestSaveOutputToFile_AboveThreshold(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 100
	globalExecDir = t.TempDir()

	content := strings.Repeat("b", 200) // 200 > 100 threshold
	filePath, err := saveOutputToFile(content, "stdout", "ls -la")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath == "" {
		t.Fatal("saveOutputToFile() should return a file path for long content")
	}

	// 驗證文件存在且內容正確
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content = %q, want %q", string(data), content)
	}
}

func TestSaveOutputToFile_ExactThreshold(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 100
	globalExecDir = t.TempDir()

	// 正好等於門檻 → 不應該保存到文件
	content := strings.Repeat("c", 100)
	filePath, err := saveOutputToFile(content, "stdout", "cat file")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath != "" {
		t.Errorf("saveOutputToFile() should return empty for content at exact threshold, got: %s", filePath)
	}
}

func TestSaveOutputToFile_JustAboveThreshold(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 100
	globalExecDir = t.TempDir()

	// 比門檻多 1 個字符 → 應該保存到文件
	content := strings.Repeat("d", 101)
	filePath, err := saveOutputToFile(content, "stderr", "go build")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath == "" {
		t.Fatal("saveOutputToFile() should save file when content exceeds threshold")
	}
}

func TestSaveOutputToFile_PrefixInFilename(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 0
	globalExecDir = t.TempDir()

	content := strings.Repeat("e", 2000)
	filePath, err := saveOutputToFile(content, "my_prefix", "cmd")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if !strings.Contains(filepath.Base(filePath), "my_prefix") {
		t.Errorf("filename should contain prefix: %s", filePath)
	}
}

func TestSaveOutputToFile_OutputDirCreated(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()

	globalToolsConfig.SmartShell.MaxDirectOutput = 0
	globalExecDir = t.TempDir()

	// output 目錄唔存在，函數應該自動創建
	content := strings.Repeat("f", 2000)
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

func TestSaveOutputToFile_ExplorePhaseHigherThreshold(t *testing.T) {
	old := globalToolsConfig
	oldExecDir := globalExecDir
	defer func() {
		globalToolsConfig = old
		globalExecDir = oldExecDir
	}()
	defer resetGlobalPlanMode()

	globalToolsConfig.SmartShell.MaxDirectOutput = 1000
	globalExecDir = t.TempDir()

	// 進入 Plan Mode Explore Phase
	globalPlanMode.mu.Lock()
	globalPlanMode.Phase = PlanPhaseExplore
	globalPlanMode.mu.Unlock()

	// 1500 字：超過普通門檻 1000，但低於探索階段門檻 2000，應該不會保存
	content := strings.Repeat("g", 1500)
	filePath, err := saveOutputToFile(content, "stdout", "echo")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath != "" {
		t.Errorf("saveOutputToFile() in Explore Phase with 1500 chars should not save (threshold=2000), got: %s", filePath)
	}

	// 2500 字：超過探索階段門檻 2000，應該保存
	content2 := strings.Repeat("h", 2500)
	filePath2, err := saveOutputToFile(content2, "stdout", "echo")
	if err != nil {
		t.Fatalf("saveOutputToFile() error: %v", err)
	}
	if filePath2 == "" {
		t.Error("saveOutputToFile() in Explore Phase with 2500 chars should save (threshold=2000)")
	}
}
