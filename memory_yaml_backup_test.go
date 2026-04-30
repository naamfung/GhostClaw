package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ghostclaw.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	if err := db.AutoMigrate(&Memories{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db, tmpDir
}

// insertMem 插入一條測試記憶，自動生成唯一 ID
func insertMem(db *gorm.DB, category, key, value, scope string, score float64) {
	db.Create(&Memories{
		ID:       fmt.Sprintf("id-%s-%s-%d", category, key, time.Now().UnixNano()),
		Category: category,
		Key:      key,
		Value:    value,
		Scope:    scope,
		Score:    score,
	})
}

// ============================================================================
// BackupMemoriesToYAML
// ============================================================================

func TestBackupMemoriesToYAML_EmptyDB(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	err := BackupMemoriesToYAML()
	if err != nil {
		t.Fatalf("backup empty DB should succeed: %v", err)
	}

	backupPath := filepath.Join(MemoryDir(), "memories_backup.yaml")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup file should exist: %v", err)
	}

	if !strings.Contains(string(data), "memories: []") && !strings.Contains(string(data), "memories:") {
		t.Errorf("backup should contain empty memories list, got: %s", string(data))
	}
}

func TestBackupMemoriesToYAML_WithData(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	insertMem(db, "fact", "user_name", "张三", "user", 0.85)
	insertMem(db, "preference", "language", "简体中文", "user", 0.9)

	err := BackupMemoriesToYAML()
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	backupPath := filepath.Join(MemoryDir(), "memories_backup.yaml")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup file should exist: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "user_name") {
		t.Error("backup should contain 'user_name'")
	}
	if !strings.Contains(content, "张三") {
		t.Error("backup should contain '张三'")
	}
	if !strings.Contains(content, "简体中文") {
		t.Error("backup should contain '简体中文'")
	}
	if !strings.Contains(content, "backup_date") {
		t.Error("backup should contain backup_date")
	}
}

// ============================================================================
// RecoverMemoriesFromYAML
// ============================================================================

func TestRecoverMemoriesFromYAML_NoFile(t *testing.T) {
	_, tmpDir := setupTestDB(t)
	oldDataDir := globalDataDir
	globalDataDir = tmpDir
	defer func() { globalDataDir = oldDataDir }()

	_, err := RecoverMemoriesFromYAML()
	if err == nil {
		t.Error("recovery without backup file should return error")
	}
}

func TestRecoverMemoriesFromYAML_RestoreData(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	insertMem(db, "fact", "user_name", "张三", "user", 0.85)
	insertMem(db, "fact", "user_age", "30", "user", 0.5)

	if err := BackupMemoriesToYAML(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// 清空 DB
	db.Where("1 = 1").Delete(&Memories{})
	var count int64
	db.Model(&Memories{}).Count(&count)
	if count != 0 {
		t.Fatalf("DB should be empty after delete, got %d", count)
	}

	recovered, err := RecoverMemoriesFromYAML()
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if recovered != 2 {
		t.Errorf("should recover 2 memories, got %d", recovered)
	}

	var mem Memories
	db.Where("key = ?", "user_name").First(&mem)
	if mem.Value != "张三" {
		t.Errorf("recovered user_name = %q, want '张三'", mem.Value)
	}
	if mem.Score != 0.85 {
		t.Errorf("recovered score = %v, want 0.85", mem.Score)
	}
	if mem.Category != "fact" {
		t.Errorf("recovered category = %q, want 'fact'", mem.Category)
	}
}

func TestRecoverMemoriesFromYAML_Deduplication(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	insertMem(db, "fact", "existing", "already_here", "user", 1.0)
	insertMem(db, "fact", "new_one", "not_in_db_yet", "user", 0.5)

	if err := BackupMemoriesToYAML(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	// 只刪除 existing
	db.Where("key = ?", "existing").Delete(&Memories{})

	recovered, err := RecoverMemoriesFromYAML()
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if recovered != 1 {
		t.Errorf("should recover only 1 memory (existing was lost), got %d", recovered)
	}

	var count int64
	db.Model(&Memories{}).Where("key = ?", "new_one").Count(&count)
	if count != 1 {
		t.Errorf("new_one should still have exactly 1 record, got %d", count)
	}
}

// ============================================================================
// Round-trip
// ============================================================================

func TestMemoryBackupRoundTrip(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	type testMem struct {
		cat, key, val, scope string
		score                float64
		tags                 string
	}
	original := []testMem{
		{"fact", "k1", "v1", "user", 0.8, `["a","b"]`},
		{"fact", "k2", "v2", "user", 0.6, ""},
		{"preference", "k3", "v3", "user", 0.9, `["x"]`},
		{"project", "k4", "v4", "global", 0.3, ""},
		{"skill", "k5", "v5", "user", 0.7, ""},
	}
	for _, m := range original {
		insertMem(db, m.cat, m.key, m.val, m.scope, m.score)
	}

	if err := BackupMemoriesToYAML(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	db.Where("1 = 1").Delete(&Memories{})

	recovered, err := RecoverMemoriesFromYAML()
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if recovered != len(original) {
		t.Errorf("recovered %d, want %d", recovered, len(original))
	}

	for _, orig := range original {
		var restored Memories
		result := db.Where("key = ? AND category = ?", orig.key, orig.cat).First(&restored)
		if result.Error != nil {
			t.Errorf("missing record: key=%s category=%s", orig.key, orig.cat)
			continue
		}
		if restored.Value != orig.val {
			t.Errorf("key=%s: value=%q, want %q", orig.key, restored.Value, orig.val)
		}
		if restored.Scope != orig.scope {
			t.Errorf("key=%s: scope=%q, want %q", orig.key, restored.Scope, orig.scope)
		}
		if restored.Score != orig.score {
			t.Errorf("key=%s: score=%v, want %v", orig.key, restored.Score, orig.score)
		}
	}

	var count int64
	db.Model(&Memories{}).Count(&count)
	if count != int64(len(original)) {
		t.Errorf("total records = %d, want %d", count, len(original))
	}
}

// ============================================================================
// YAML format validation
// ============================================================================

func TestMemoryBackupYAML_ValidFormat(t *testing.T) {
	db, tmpDir := setupTestDB(t)
	oldDB := globalDB
	oldDataDir := globalDataDir
	globalDB = db
	globalDataDir = tmpDir
	defer func() { globalDB = oldDB; globalDataDir = oldDataDir }()

	insertMem(db, "fact", "test", "hello", "user", 0.5)

	if err := BackupMemoriesToYAML(); err != nil {
		t.Fatalf("backup failed: %v", err)
	}

	backupPath := filepath.Join(MemoryDir(), "memories_backup.yaml")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}

	var backup memoryBackup
	if err := yaml.Unmarshal(data, &backup); err != nil {
		t.Fatalf("invalid YAML format: %v", err)
	}

	if len(backup.Memories) != 1 {
		t.Errorf("unexpected memory count: %d", len(backup.Memories))
	}
	if backup.BackupDate == "" {
		t.Error("backup_date should not be empty")
	}
	if backup.Memories[0].Key != "test" {
		t.Errorf("key = %q, want 'test'", backup.Memories[0].Key)
	}
}

func TestBackupMemoriesToYAML_DBNotInitialized(t *testing.T) {
	oldDB := globalDB
	globalDB = nil
	defer func() { globalDB = oldDB }()

	err := BackupMemoriesToYAML()
	if err == nil {
		t.Error("should fail when DB is nil")
	}
}
