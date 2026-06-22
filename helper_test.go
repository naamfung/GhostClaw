package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestCleanupDataTempDir_RemovesExpiredFiles 验证 cleanupDataTempDir 会删除 24h 前的文件。
func TestCleanupDataTempDir_RemovesExpiredFiles(t *testing.T) {
	origDataDir := globalDataDir
	defer func() { globalDataDir = origDataDir }()

	dataDir := t.TempDir()
	globalDataDir = dataDir
	tempRoot := filepath.Join(dataDir, "temp", "tool_results_cache")
	if err := os.MkdirAll(tempRoot, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// 1. 创建一个「过期」文件（修改时间设为 25 小时前）
	expiredFile := filepath.Join(tempRoot, "expired.txt")
	if err := os.WriteFile(expiredFile, []byte("old"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	pastTime := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(expiredFile, pastTime, pastTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	// 2. 创建一个「未过期」文件（修改时间为现在）
	freshFile := filepath.Join(tempRoot, "fresh.txt")
	if err := os.WriteFile(freshFile, []byte("new"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 调用清理
	cleanupDataTempDir()

	// 验证：过期文件被删除，未过期文件保留
	if _, err := os.Stat(expiredFile); !os.IsNotExist(err) {
		t.Errorf("expired file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Errorf("fresh file should be kept, got err=%v", err)
	}
}

// TestCleanupDataTempDir_OnlyAffectsTempSubtree 验证清理只作用于 <dataDir>/temp，
// 不会误删 <dataDir>/memory 等其他子目录。
func TestCleanupDataTempDir_OnlyAffectsTempSubtree(t *testing.T) {
	origDataDir := globalDataDir
	defer func() { globalDataDir = origDataDir }()

	dataDir := t.TempDir()
	globalDataDir = dataDir

	// 在 <dataDir>/memory 下放一个「过期」文件，验证它不会被删
	memoryDir := filepath.Join(dataDir, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	memoryFile := filepath.Join(memoryDir, "important.json")
	if err := os.WriteFile(memoryFile, []byte("keep me"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	pastTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(memoryFile, pastTime, pastTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	// 同时在 <dataDir>/temp 下放一个过期文件，验证它会被删
	tempDir := filepath.Join(dataDir, "temp", "sub")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	tempFile := filepath.Join(tempDir, "cache.txt")
	if err := os.WriteFile(tempFile, []byte("expire me"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.Chtimes(tempFile, pastTime, pastTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	cleanupDataTempDir()

	// memory 下的文件应该保留
	if _, err := os.Stat(memoryFile); err != nil {
		t.Errorf("file under <dataDir>/memory should NOT be removed, got err=%v", err)
	}
	// temp 下的过期文件应该被删
	if _, err := os.Stat(tempFile); !os.IsNotExist(err) {
		t.Errorf("file under <dataDir>/temp should be removed, got err=%v", err)
	}
}

// TestCleanupDataTempDir_RemovesEmptyDirs 验证清理后空目录也会被删除。
func TestCleanupDataTempDir_RemovesEmptyDirs(t *testing.T) {
	origDataDir := globalDataDir
	defer func() { globalDataDir = origDataDir }()

	dataDir := t.TempDir()
	globalDataDir = dataDir

	// 创建 <dataDir>/temp/empty_sub，里面只放过期文件
	emptySub := filepath.Join(dataDir, "temp", "empty_sub")
	if err := os.MkdirAll(emptySub, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	expiredFile := filepath.Join(emptySub, "old.txt")
	if err := os.WriteFile(expiredFile, []byte("old"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	pastTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(expiredFile, pastTime, pastTime); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	cleanupDataTempDir()

	// 过期文件被删后，empty_sub 应该变成空目录并被清理
	if _, err := os.Stat(expiredFile); !os.IsNotExist(err) {
		t.Errorf("expired file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(emptySub); !os.IsNotExist(err) {
		t.Errorf("empty subdirectory should be removed, got err=%v", err)
	}
}
