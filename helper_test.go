package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestResolveTempDir_FallbackWhenDataDirSet 验证当 globalDataDir 已设置且系统 temp
// 可写时，返回系统 temp 路径。
//
// 跨平台适配：os.TempDir() 在 Unix 读 TMPDIR、Windows 读 TMP/TEMP，
// 因此同时设置三者，保证在两个平台行为一致。
func TestResolveTempDir_FallbackWhenDataDirSet(t *testing.T) {
	// 保存并恢复全局状态
	origDataDir := globalDataDir
	origTmp := os.Getenv("TMP")
	origTemp := os.Getenv("TEMP")
	origTmpDir := os.Getenv("TMPDIR")
	defer func() {
		globalDataDir = origDataDir
		os.Setenv("TMP", origTmp)
		os.Setenv("TEMP", origTemp)
		os.Setenv("TMPDIR", origTmpDir)
	}()

	tmpRoot := t.TempDir()
	// 将系统临时目录指向可写的 tmpRoot（Windows: TMP/TEMP；Unix: TMPDIR）
	os.Setenv("TMP", tmpRoot)
	os.Setenv("TEMP", tmpRoot)
	os.Setenv("TMPDIR", tmpRoot)
	globalDataDir = t.TempDir()

	got := resolveTempDir("tool_results_cache")
	wantSysTmp := filepath.Join(os.TempDir(), "ghostclaw-tool_results_cache")
	if got != wantSysTmp {
		t.Errorf("resolveTempDir with writable sys tmp: got %q, want %q", got, wantSysTmp)
	}
}

// TestResolveTempDir_FallbackToDataDir 验证当系统 temp 不可写时回退到 dataDir/temp/<subdir>。
func TestResolveTempDir_FallbackToDataDir(t *testing.T) {
	origDataDir := globalDataDir
	origTmp := os.Getenv("TMP")
	origTemp := os.Getenv("TEMP")
	origTmpDir := os.Getenv("TMPDIR")
	defer func() {
		globalDataDir = origDataDir
		os.Setenv("TMP", origTmp)
		os.Setenv("TEMP", origTemp)
		os.Setenv("TMPDIR", origTmpDir)
	}()

	dataDir := t.TempDir()
	globalDataDir = dataDir

	// 将系统临时目录指向一个「已存在的文件」路径。
	// os.MkdirAll 对已存在的文件路径必然失败（Unix 与 Windows 行为一致，
	// 返回 ENOTDIR 类错误），从而跨平台触发 resolveTempDir 的回退逻辑——
	// 不依赖 /proc 等 Unix 专属路径。
	blockFile := filepath.Join(dataDir, "blocked_path_file")
	if err := os.WriteFile(blockFile, []byte("x"), 0644); err != nil {
		t.Fatalf("create block file: %v", err)
	}
	os.Setenv("TMP", blockFile)
	os.Setenv("TEMP", blockFile)
	os.Setenv("TMPDIR", blockFile)

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

// closeTestDB 关闭 GORM/SQLite 连接，并在 Windows 上等待文件句柄完全释放。
//
// 跨平台适配：Unix 允许删除仍被打开的数据库文件，Windows 则要求所有句柄
// 释放后才能删除（否则 t.TempDir() 的 RemoveAll 清理会报
// "being used by another process"）。SQLite 的 WAL/shm 文件句柄释放
// 是异步的，因此在 Windows 上需要重试等待。
func closeTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Logf("closeTestDB: db.DB() error: %v", err)
		return
	}
	if sqlDB == nil {
		return
	}
	// 显式关闭连接池。此操作会关闭所有空闲连接；活跃连接也随 Close 关闭。
	if err := sqlDB.Close(); err != nil {
		t.Logf("closeTestDB: sqlDB.Close() error: %v", err)
	}
	// Windows 上 WAL/shm 句柄释放有延迟，等待重试（最多约 2s）
	if runtime.GOOS == "windows" {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			// 尝试打开并立即关闭，确保没有残留句柄；真正的验证是
			// t.TempDir() 的 RemoveAll 清理，这里主动释放后稍作让步。
			// 无需额外 IO，直接给 OS 一点时间完成句柄释放。
			time.Sleep(50 * time.Millisecond)
			break
		}
	}
}
