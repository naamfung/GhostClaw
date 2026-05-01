package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================================
// Test helpers
// ============================================================================

// setupTestSkillManagerV2 creates a SkillManagerV2 with a temp directory and empty DB.
// Returns a cleanup function that the caller should defer.
func setupTestSkillManagerV2(t *testing.T) (*SkillManagerV2, func()) {
	t.Helper()
	tmpDir := t.TempDir()

	sm, err := NewSkillManagerV2(tmpDir, 10)
	if err != nil {
		t.Fatalf("failed to create SkillManagerV2: %v", err)
	}

	cleanup := func() {
		// RebuildIndex won't find any files in empty dir, so DB is clean
	}
	return sm, cleanup
}

// insertTestSkillMeta inserts a SkillMeta record directly into the DB for testing.
func insertTestSkillMeta(t *testing.T, sm *SkillManagerV2, meta SkillMeta) {
	t.Helper()
	if err := sm.db.Create(&meta).Error; err != nil {
		t.Fatalf("failed to insert test skill meta: %v", err)
	}
}

// makeTestSkillMeta creates a SkillMeta with given parameters and sensible defaults.
func makeTestSkillMeta(name string, useCount int, lastUsedDaysAgo int, qualityScore float64, protected bool) SkillMeta {
	now := time.Now().Unix()
	lastUsed := now - int64(lastUsedDaysAgo*86400)
	createdAt := now - int64(365*86400) // 1 year ago by default

	return SkillMeta{
		Name:         name,
		DisplayName:  "Test " + name,
		Description:  "A test skill",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/skills/" + name + "/SKILL.md",
		FileSize:     100,
		ModTime:      now,
		UseCount:     useCount,
		LastUsed:     lastUsed,
		QualityScore: qualityScore,
		Protected:    protected,
		CreatedAt:    createdAt,
		UpdatedAt:    now,
	}
}

// ============================================================================
// ProtectSkill / UnprotectSkill tests
// ============================================================================

func TestProtectSkill_SetsProtectedFlag(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	insertTestSkillMeta(t, sm, makeTestSkillMeta("test_skill", 5, 10, 0.6, false))

	err := sm.ProtectSkill("test_skill")
	if err != nil {
		t.Fatalf("ProtectSkill() unexpected error: %v", err)
	}

	protected, err := sm.IsSkillProtected("test_skill")
	if err != nil {
		t.Fatalf("IsSkillProtected() unexpected error: %v", err)
	}
	if !protected {
		t.Error("IsSkillProtected() should return true after ProtectSkill()")
	}
}

func TestProtectSkill_NonExistentSkill_ReturnsError(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	err := sm.ProtectSkill("nonexistent")
	if err == nil {
		t.Error("ProtectSkill() should return error for non-existent skill")
	}
}

func TestUnprotectSkill_RemovesProtectedFlag(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	insertTestSkillMeta(t, sm, makeTestSkillMeta("test_skill", 5, 10, 0.6, true))

	err := sm.UnprotectSkill("test_skill")
	if err != nil {
		t.Fatalf("UnprotectSkill() unexpected error: %v", err)
	}

	protected, _ := sm.IsSkillProtected("test_skill")
	if protected {
		t.Error("IsSkillProtected() should return false after UnprotectSkill()")
	}
}

func TestSetSkillProtected_ToggleBothDirections(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	insertTestSkillMeta(t, sm, makeTestSkillMeta("toggle_skill", 3, 20, 0.5, false))

	// Protect
	if err := sm.SetSkillProtected("toggle_skill", true); err != nil {
		t.Fatalf("SetSkillProtected(true) unexpected error: %v", err)
	}
	protected, _ := sm.IsSkillProtected("toggle_skill")
	if !protected {
		t.Error("should be protected after SetSkillProtected(true)")
	}

	// Unprotect
	if err := sm.SetSkillProtected("toggle_skill", false); err != nil {
		t.Fatalf("SetSkillProtected(false) unexpected error: %v", err)
	}
	protected, _ = sm.IsSkillProtected("toggle_skill")
	if protected {
		t.Error("should NOT be protected after SetSkillProtected(false)")
	}
}

// ============================================================================
// IsSkillProtected tests
// ============================================================================

func TestIsSkillProtected_ReturnsFalseByDefault(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	insertTestSkillMeta(t, sm, makeTestSkillMeta("normal_skill", 10, 5, 0.7, false))

	protected, err := sm.IsSkillProtected("normal_skill")
	if err != nil {
		t.Fatalf("IsSkillProtected() unexpected error: %v", err)
	}
	if protected {
		t.Error("new skill should not be protected by default")
	}
}

func TestIsSkillProtected_NonExistentSkill_ReturnsError(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	_, err := sm.IsSkillProtected("nonexistent")
	if err == nil {
		t.Error("IsSkillProtected() should return error for non-existent skill")
	}
}

// ============================================================================
// ListProtectedSkills tests
// ============================================================================

func TestListProtectedSkills_ReturnsOnlyProtected(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	// Insert mix of protected and unprotected skills
	insertTestSkillMeta(t, sm, makeTestSkillMeta("protected_a", 5, 10, 0.7, true))
	insertTestSkillMeta(t, sm, makeTestSkillMeta("normal_b", 10, 5, 0.8, false))
	insertTestSkillMeta(t, sm, makeTestSkillMeta("protected_c", 3, 30, 0.4, true))
	insertTestSkillMeta(t, sm, makeTestSkillMeta("normal_d", 20, 2, 0.9, false))

	protected, err := sm.ListProtectedSkills()
	if err != nil {
		t.Fatalf("ListProtectedSkills() unexpected error: %v", err)
	}

	if len(protected) != 2 {
		t.Errorf("ListProtectedSkills(): got %d, want 2", len(protected))
	}

	names := make(map[string]bool)
	for _, name := range protected {
		names[name] = true
	}
	if !names["protected_a"] {
		t.Error("ListProtectedSkills() should include 'protected_a'")
	}
	if !names["protected_c"] {
		t.Error("ListProtectedSkills() should include 'protected_c'")
	}
	if names["normal_b"] || names["normal_d"] {
		t.Error("ListProtectedSkills() should NOT include unprotected skills")
	}
}

func TestListProtectedSkills_EmptyWhenNoneProtected(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	insertTestSkillMeta(t, sm, makeTestSkillMeta("skill_a", 5, 10, 0.7, false))
	insertTestSkillMeta(t, sm, makeTestSkillMeta("skill_b", 10, 5, 0.8, false))

	protected, err := sm.ListProtectedSkills()
	if err != nil {
		t.Fatalf("ListProtectedSkills() unexpected error: %v", err)
	}

	if len(protected) != 0 {
		t.Errorf("ListProtectedSkills(): got %d, want 0", len(protected))
	}
}

// ============================================================================
// DeleteSkill protected check tests
// ============================================================================

func TestDeleteSkill_ProtectedSkill_ReturnsError(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	// Need a real file on disk for DeleteSkill to find it
	skillDir := filepath.Join(sm.skillsDir, "protected_skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: protected_skill\ndescription: A protected test skill\n---\n# Test\n\n## 描述\nProtected test skill."), 0644)

	// Rebuild index to pick up the file
	sm.RebuildIndex()

	// Protect it
	sm.ProtectSkill("protected_skill")

	// Try to delete
	err := sm.DeleteSkill("protected_skill")
	if err == nil {
		t.Error("DeleteSkill() should return error for protected skill")
	}
}

func TestDeleteSkill_UnprotectedSkill_Succeeds(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	skillDir := filepath.Join(sm.skillsDir, "unprotected_skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: unprotected_skill\ndescription: A test skill for deletion\n---\n# Test\n\n## 描述\nTest skill for deletion."), 0644)

	sm.RebuildIndex()

	err := sm.DeleteSkill("unprotected_skill")
	if err != nil {
		t.Errorf("DeleteSkill() unexpected error for unprotected skill: %v", err)
	}

	_, err = sm.IsSkillProtected("unprotected_skill")
	if err == nil {
		t.Error("skill should be gone after deletion")
	}
}

// ============================================================================
// indexSkillFile - Protected from YAML frontmatter tests
// ============================================================================

func TestIndexSkillFile_ReadsProtectedFromFrontmatter(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	skillDir := filepath.Join(sm.skillsDir, "yaml_protected")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: yaml_protected
description: A protected skill from YAML
tags:
  - test
protected: true
---
# YAML Protected Skill

## 描述
This skill should be protected.
`), 0644)

	sm.RebuildIndex()

	protected, err := sm.IsSkillProtected("yaml_protected")
	if err != nil {
		t.Fatalf("IsSkillProtected() unexpected error: %v", err)
	}
	if !protected {
		t.Error("skill with protected: true in YAML frontmatter should be protected")
	}
}

func TestIndexSkillFile_DefaultsToUnprotected(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	skillDir := filepath.Join(sm.skillsDir, "yaml_normal")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: yaml_normal
description: A normal skill without protected flag
tags:
  - test
---
# Normal Skill

## 描述
This skill is not protected.
`), 0644)

	sm.RebuildIndex()

	protected, err := sm.IsSkillProtected("yaml_normal")
	if err != nil {
		t.Fatalf("IsSkillProtected() unexpected error: %v", err)
	}
	if protected {
		t.Error("skill without protected in YAML frontmatter should NOT be protected")
	}
}

func TestIndexSkillFile_UpdatePreservesDBValue_WhenYamlUnset(t *testing.T) {
	sm, cleanup := setupTestSkillManagerV2(t)
	defer cleanup()

	skillDir := filepath.Join(sm.skillsDir, "db_preserved")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: db_preserved
description: Protected via DB, not YAML
tags:
  - test
---
# DB Protected Skill

## 描述
A skill protected via DB only.
`), 0644)

	// First index: should be unprotected
	sm.RebuildIndex()

	// Manually protect via DB
	sm.ProtectSkill("db_preserved")
	protected, _ := sm.IsSkillProtected("db_preserved")
	if !protected {
		t.Fatal("ProtectSkill() should have set protected flag")
	}

	// Rebuild again: should PRESERVE the DB protected value
	sm.RebuildIndex()
	protected, err := sm.IsSkillProtected("db_preserved")
	if err != nil {
		t.Fatalf("IsSkillProtected() unexpected error: %v", err)
	}
	if !protected {
		t.Error("DB protected value should be preserved when YAML doesn't set protected")
	}
}

// ============================================================================
// GenerateCleanupSuggestions tests
// ============================================================================

func setupTestEvolutionOptimizer(t *testing.T) (*SkillEvolutionOptimizer, *SkillManagerV2, func()) {
	t.Helper()
	sm, cleanup := setupTestSkillManagerV2(t)
	return sm.EvolutionOptimizer(), sm, cleanup
}

func TestGenerateCleanupSuggestions_SkipsProtectedSkills(t *testing.T) {
	opt, sm, cleanup := setupTestEvolutionOptimizer(t)
	defer cleanup()

	now := time.Now().Unix()

	// A protected skill that SHOULD be cleaned up by Criterion 3 (100 days unused, 2 uses)
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "protected_old",
		DisplayName:  "Protected Old",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/protected_old/SKILL.md",
		UseCount:     2,
		LastUsed:     now - 100*86400,
		QualityScore: 0.2,
		Protected:    true,
		CreatedAt:    now - 200*86400,
	})

	// Same skill but unprotected — should appear in suggestions
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "unprotected_old",
		DisplayName:  "Unprotected Old",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/unprotected_old/SKILL.md",
		UseCount:     2,
		LastUsed:     now - 100*86400,
		QualityScore: 0.2,
		Protected:    false,
		CreatedAt:    now - 200*86400,
	})

	suggestions, err := opt.GenerateCleanupSuggestions()
	if err != nil {
		t.Fatalf("GenerateCleanupSuggestions() unexpected error: %v", err)
	}

	// Check that protected skill is NOT in suggestions
	for _, s := range suggestions {
		if s.SkillName == "protected_old" && s.Action == "delete" {
			t.Errorf("protected skill 'protected_old' should NOT appear in delete suggestions")
		}
	}

	// Check that unprotected skill IS in suggestions
	found := false
	for _, s := range suggestions {
		if s.SkillName == "unprotected_old" && s.Action == "delete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unprotected old skill should appear in delete suggestions")
	}
}

func TestGenerateCleanupSuggestions_Criterion3_RequiresLowQuality(t *testing.T) {
	opt, sm, cleanup := setupTestEvolutionOptimizer(t)
	defer cleanup()

	now := time.Now().Unix()

	// High quality + old + rare = should NOT be deleted (QualityScore 0.5 > 0.3)
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "high_quality_old",
		DisplayName:  "HQ Old",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/hq_old/SKILL.md",
		UseCount:     3,
		LastUsed:     now - 100*86400,
		QualityScore: 0.5,
		Protected:    false,
		CreatedAt:    now - 200*86400,
	})

	suggestions, err := opt.GenerateCleanupSuggestions()
	if err != nil {
		t.Fatalf("GenerateCleanupSuggestions() error: %v", err)
	}

	for _, s := range suggestions {
		if s.SkillName == "high_quality_old" && s.Action == "delete" {
			t.Error("high quality skill (0.5) should NOT be deleted even if old and rarely used")
		}
	}
}

func TestGenerateCleanupSuggestions_Criterion1_NeverUsedOldSkill(t *testing.T) {
	opt, sm, cleanup := setupTestEvolutionOptimizer(t)
	defer cleanup()

	now := time.Now().Unix()

	// Never used, created 60 days ago → Criterion 1
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "never_used_old",
		DisplayName:  "Never Used",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/never_used/SKILL.md",
		UseCount:     0,
		LastUsed:     0, // never used
		QualityScore: 0,
		Protected:    false,
		CreatedAt:    now - 60*86400, // 60 days ago
	})

	// Fresh skill, never used but created 5 days ago → NOT Criterion 1
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "never_used_fresh",
		DisplayName:  "Fresh",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/fresh/SKILL.md",
		UseCount:     0,
		LastUsed:     0,
		QualityScore: 0,
		Protected:    false,
		CreatedAt:    now - 5*86400, // 5 days ago
	})

	suggestions, err := opt.GenerateCleanupSuggestions()
	if err != nil {
		t.Fatalf("GenerateCleanupSuggestions() error: %v", err)
	}

	foundOld := false
	foundFresh := false
	for _, s := range suggestions {
		if s.SkillName == "never_used_old" && s.Action == "delete" {
			foundOld = true
		}
		if s.SkillName == "never_used_fresh" && s.Action == "delete" {
			foundFresh = true
		}
	}

	if !foundOld {
		t.Error("never-used skill created 60 days ago should be flagged for deletion")
	}
	if foundFresh {
		t.Error("never-used skill created 5 days ago should NOT be flagged for deletion")
	}
}

func TestGenerateCleanupSuggestions_UsesConfigurableThreshold(t *testing.T) {
	opt, sm, cleanup := setupTestEvolutionOptimizer(t)
	defer cleanup()

	// Set a very short threshold (30 days)
	restore := saveAndRestoreCompressionGlobals()
	globalSkillCleanupThresholdDays = 30
	defer func() {
		restore()
		globalSkillCleanupThresholdDays = 90
	}()

	now := time.Now().Unix()

	// Low quality, last used 60 days ago, 2 uses
	// With threshold=30 → should be deleted (60 > 30)
	// With threshold=90 → would NOT be deleted
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "borderline_skill",
		DisplayName:  "Borderline",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/borderline/SKILL.md",
		UseCount:     2,
		LastUsed:     now - 60*86400,
		QualityScore: 0.2,
		Protected:    false,
		CreatedAt:    now - 200*86400,
	})

	suggestions, err := opt.GenerateCleanupSuggestions()
	if err != nil {
		t.Fatalf("GenerateCleanupSuggestions() error: %v", err)
	}

	found := false
	for _, s := range suggestions {
		if s.SkillName == "borderline_skill" && s.Action == "delete" {
			found = true
			break
		}
	}
	if !found {
		t.Error("skill unused for 60 days should be deleted with threshold=30")
	}
}

func TestGenerateCleanupSuggestions_Criterion2_ImproveNotDelete(t *testing.T) {
	opt, sm, cleanup := setupTestEvolutionOptimizer(t)
	defer cleanup()

	now := time.Now().Unix()

	// Low quality but recently used → should only be "improve", not "delete"
	insertTestSkillMeta(t, sm, SkillMeta{
		Name:         "low_quality_active",
		DisplayName:  "Low Quality Active",
		Description:  "test",
		Tags:         `["test"]`,
		TriggerWords: `["test"]`,
		FilePath:     "/tmp/test/lowq/SKILL.md",
		UseCount:     5,
		LastUsed:     now - 2*86400, // recently used
		QualityScore: 0.15,          // below 0.2
		Protected:    false,
		CreatedAt:    now - 100*86400,
	})

	suggestions, err := opt.GenerateCleanupSuggestions()
	if err != nil {
		t.Fatalf("GenerateCleanupSuggestions() error: %v", err)
	}

	for _, s := range suggestions {
		if s.SkillName == "low_quality_active" {
			if s.Action != "improve" {
				t.Errorf("low quality active skill should be 'improve', got action=%q", s.Action)
			}
			return
		}
	}
	t.Error("low quality active skill should appear in suggestions with action='improve'")
}
