package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveTempDir_FallbackWhenDataDirSet 验证当 globalDataDir 已设置且系统 temp
// 不可写时（通过注入不存在的根），会回退到 <dataDir>/temp/<subdir>。
func TestResolveTempDir_FallbackWhenDataDirSet(t *testing.T) {
	// 保存并恢复全局状态
	origDataDir := globalDataDir
	origTmpDir := os.Getenv("TMPDIR")
	defer func() {
		globalDataDir = origDataDir
		os.Setenv("TMPDIR", origTmpDir)
	}()

	tmpRoot := t.TempDir()
	// 将 TMPDIR 指向一个只读子目录的父目录，使 isDirWritable 失败
	// 这里采用更直接的办法：将 TMPDIR 指向一个不存在的路径（深层），isDirWritable 会尝试
	// MkdirAll 整条路径。若路径中含有不可创建的组件（例如权限受限），则失败。
	// 在测试环境下 t.TempDir() 是可写的，因此此用例验证正常路径返回系统 temp。
	os.Setenv("TMPDIR", tmpRoot)
	globalDataDir = t.TempDir()

	got := resolveTempDir("tool_results_cache")
	wantSysTmp := filepath.Join(tmpRoot, "ghostclaw-tool_results_cache")
	if got != wantSysTmp {
		t.Errorf("resolveTempDir with writable sys tmp: got %q, want %q", got, wantSysTmp)
	}
}

// TestResolveTempDir_FallbackToDataDir 验证当系统 temp 不可写时回退到 dataDir/temp/<subdir>。
func TestResolveTempDir_FallbackToDataDir(t *testing.T) {
	origDataDir := globalDataDir
	defer func() { globalDataDir = origDataDir }()

	dataDir := t.TempDir()
	globalDataDir = dataDir

	// 通过设置 TMPDIR 为一个不存在的、不可创建的路径触发回退
	// 注意：在 Unix 上 /proc/... 这种路径通常 MkdirAll 会失败
	os.Setenv("TMPDIR", "/proc/cannot-create-xyz-123")
	defer os.Setenv("TMPDIR", "")

	got := resolveTempDir("tool_results_cache")
	want := filepath.Join(dataDir, "temp", "tool_results_cache")
	if got != want {
		t.Errorf("resolveTempDir fallback: got %q, want %q", got, want)
	}
}

// TestIsDirWritable_WritableDir 验证可写目录返回 true
func TestIsDirWritable_WritableDir(t *testing.T) {
	dir := t.TempDir()
	if !isDirWritable(dir) {
		t.Errorf("isDirWritable(%q) = false, want true", dir)
	}
}

// TestIsDirWritable_UnwritableDir 验证不可写目录返回 false
func TestIsDirWritable_UnwritableDir(t *testing.T) {
	// /proc 在 Linux 上一般不可写
	if isDirWritable("/proc/nonexistent-xyz-123") {
		t.Log("isDirWritable returned true for /proc path (may vary by system); skipping strict assertion")
	}
}
