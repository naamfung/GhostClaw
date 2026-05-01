package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// ============================================================================
// ModelBase.ResolveAPIKey
// ============================================================================

func TestResolveAPIKey_RawKey(t *testing.T) {
	m := ModelBase{APIKey: "sk-abc123"}
	result := m.ResolveAPIKey()
	if result != "sk-abc123" {
		t.Errorf("ResolveAPIKey() = %q, want %q", result, "sk-abc123")
	}
}

func TestResolveAPIKey_EmptyKey(t *testing.T) {
	m := ModelBase{APIKey: ""}
	result := m.ResolveAPIKey()
	if result != "" {
		t.Errorf("ResolveAPIKey() with empty key = %q, want %q", result, "")
	}
}

func TestResolveAPIKey_EnvVarResolved(t *testing.T) {
	os.Setenv("GHOSTCLAW_TEST_KEY", "resolved-value")
	defer os.Unsetenv("GHOSTCLAW_TEST_KEY")

	m := ModelBase{APIKey: "${GHOSTCLAW_TEST_KEY}"}
	result := m.ResolveAPIKey()
	if result != "resolved-value" {
		t.Errorf("ResolveAPIKey() = %q, want %q", result, "resolved-value")
	}
}

func TestResolveAPIKey_EnvVarNotSet(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR_FOR_TEST")

	m := ModelBase{APIKey: "${NONEXISTENT_VAR_FOR_TEST}"}
	result := m.ResolveAPIKey()
	if result != "" {
		t.Errorf("ResolveAPIKey() with unset env var = %q, want %q", result, "")
	}
}

func TestResolveAPIKey_PartialEnvVarSyntax(t *testing.T) {
	// Missing closing brace — treated as literal
	tests := []string{
		"${sk-no-brace",
		"sk-${PARTIAL",
		"$OPENAI_KEY",
		"{SK_KEY}",
		"sk-abcd1234",
	}

	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			m := ModelBase{APIKey: key}
			result := m.ResolveAPIKey()
			if result != key {
				t.Errorf("ResolveAPIKey() = %q, want %q (literal)", result, key)
			}
		})
	}
}

func TestResolveAPIKey_EmptyEnvVarName(t *testing.T) {
	m := ModelBase{APIKey: "${}"}
	result := m.ResolveAPIKey()
	// os.Getenv("") returns "" on most systems
	if result != "" {
		t.Errorf("ResolveAPIKey() with ${} = %q, want %q", result, "")
	}
}

func TestResolveAPIKey_EnvVarWithSpecialChars(t *testing.T) {
	os.Setenv("GHOSTCLAW_SPECIAL_KEY", "pk-!@#$%^&*()")
	defer os.Unsetenv("GHOSTCLAW_SPECIAL_KEY")

	m := ModelBase{APIKey: "${GHOSTCLAW_SPECIAL_KEY}"}
	result := m.ResolveAPIKey()
	if result != "pk-!@#$%^&*()" {
		t.Errorf("ResolveAPIKey() = %q, want %q", result, "pk-!@#$%^&*()")
	}
}

// ============================================================================
// generateRandomPassword
// ============================================================================

func TestGenerateRandomPassword_Length(t *testing.T) {
	lengths := []int{0, 1, 8, 16, 32, 64}

	for _, l := range lengths {
		t.Run(fmt.Sprintf("length_%d", l), func(t *testing.T) {
			pw := generateRandomPassword(l)
			if len(pw) != l {
				t.Errorf("generateRandomPassword(%d) length = %d, want %d", l, len(pw), l)
			}
		})
	}
}

func TestGenerateRandomPassword_Charset(t *testing.T) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
	pw := generateRandomPassword(1000) // large sample to exercise all charset values

	for i, c := range pw {
		if !strings.ContainsRune(charset, c) {
			t.Errorf("generateRandomPassword() char at %d = %q, not in allowed charset", i, c)
		}
	}
}

func TestGenerateRandomPassword_Uniqueness(t *testing.T) {
	// Generate 10 passwords of length 32 — all should be different
	passwords := make(map[string]bool)
	for i := 0; i < 10; i++ {
		pw := generateRandomPassword(32)
		if passwords[pw] {
			t.Errorf("generateRandomPassword() produced duplicate: %q", pw)
		}
		passwords[pw] = true
	}
}

// ============================================================================
// ConfigManager.createDefaultConfig
// ============================================================================

func TestCreateDefaultConfig_HasDefaultModel(t *testing.T) {
	cm := &ConfigManager{}
	cfg := cm.createDefaultConfig()

	if len(cfg.Models) == 0 {
		t.Fatal("createDefaultConfig() should have at least one model")
	}

	defaultModel, ok := cfg.Models[DEFAULT_MODEL_ID]
	if !ok {
		t.Fatalf("createDefaultConfig() should have model %q", DEFAULT_MODEL_ID)
	}
	if !defaultModel.IsDefault {
		t.Error("default model should have IsDefault = true")
	}
	if defaultModel.Name != DEFAULT_MODEL_ID {
		t.Errorf("default model Name = %q, want %q", defaultModel.Name, DEFAULT_MODEL_ID)
	}
	if defaultModel.APIType != DEFAULT_API_TYPE {
		t.Errorf("default model APIType = %q, want %q", defaultModel.APIType, DEFAULT_API_TYPE)
	}
}

func TestCreateDefaultConfig_DefaultValues(t *testing.T) {
	cm := &ConfigManager{}
	cfg := cm.createDefaultConfig()

	tests := []struct {
		field string
		got   interface{}
		want  interface{}
	}{
		{"MaxRequestSizeBytes", cfg.MaxRequestSizeBytes, 256 * 1024},
		{"HTTPServer.Listen", cfg.HTTPServer.Listen, "0.0.0.0:10086"},
		{"Security.EnableSSRFProtection", cfg.Security.EnableSSRFProtection, true},
		{"CronConfig.MaxConcurrent", cfg.CronConfig.MaxConcurrent, 1},
		{"Timeout.Shell", cfg.Timeout.Shell, DefaultShellTimeout},
		{"Timeout.HTTP", cfg.Timeout.HTTP, DefaultHTTPTimeout},
		{"Timeout.Plugin", cfg.Timeout.Plugin, DefaultPluginTimeout},
		{"Timeout.Browser", cfg.Timeout.Browser, DefaultBrowserTimeout},
		{"Heartbeat.IntervalSeconds", cfg.Heartbeat.IntervalSeconds, 1800},
		{"Heartbeat.KeepRecentMessages", cfg.Heartbeat.KeepRecentMessages, 8},
		{"Heartbeat.MaxConcurrentChecks", cfg.Heartbeat.MaxConcurrentChecks, 3},
		{"MCP.Transport", cfg.MCP.Transport, "http"},
		{"MCP.SSEEndpoint", cfg.MCP.SSEEndpoint, "/mcp/sse"},
		{"MCP.HTTPEndpoint", cfg.MCP.HTTPEndpoint, "/mcp"},
		{"Auth.TokenExpiry", cfg.Auth.TokenExpiry, 24},
		{"Tools.SmartShell.SyncTimeout", cfg.Tools.SmartShell.SyncTimeout, 60},
		{"Tools.SmartShell.UnknownTimeout", cfg.Tools.SmartShell.UnknownTimeout, 120},
		{"Tools.SmartShell.DefaultWakeMins", cfg.Tools.SmartShell.DefaultWakeMins, 5},
		{"Tools.CompressionMode", cfg.Tools.CompressionMode, "token"},
		{"Tools.CompressionThreshold", int(cfg.Tools.CompressionThreshold * 100), 80},
		{"BrowserConfig.UserMode", cfg.BrowserConfig.UserMode, true},
		{"BrowserConfig.Headless", cfg.BrowserConfig.Headless, false},
		{"BrowserConfig.DisableGPU", cfg.BrowserConfig.DisableGPU, false},
		{"BrowserConfig.DisableDevTools", cfg.BrowserConfig.DisableDevTools, false},
		{"BrowserConfig.NoSandbox", cfg.BrowserConfig.NoSandbox, true},
		{"BrowserConfig.DisableBrowserTools", cfg.BrowserConfig.DisableBrowserTools, false},
		{"SystemInfo.IncludeCPU", cfg.SystemInfo.IncludeCPU, true},
		{"SystemInfo.IncludeMemory", cfg.SystemInfo.IncludeMemory, true},
		{"SystemInfo.IncludeGPU", cfg.SystemInfo.IncludeGPU, false},
		{"SystemInfo.IncludeOSDetails", cfg.SystemInfo.IncludeOSDetails, true},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
			}
		})
	}
}

func TestCreateDefaultConfig_DefaultModelFields(t *testing.T) {
	cm := &ConfigManager{}
	cfg := cm.createDefaultConfig()
	dm := cfg.Models[DEFAULT_MODEL_ID]

	if dm.Temperature != 0.7 {
		t.Errorf("default model Temperature = %v, want 0.7", dm.Temperature)
	}
	if dm.MaxTokens != 4096 {
		t.Errorf("default model MaxTokens = %v, want 4096", dm.MaxTokens)
	}
	if !dm.Stream {
		t.Error("default model Stream should be true")
	}
	if !dm.Thinking {
		t.Error("default model Thinking should be true")
	}
	if dm.BlockDangerousCommands {
		t.Error("default model BlockDangerousCommands should be false")
	}
	if dm.Description != "默认模型" {
		t.Errorf("default model Description = %q, want %q", dm.Description, "默认模型")
	}
}

// ============================================================================
// ConfigManager.allModelsEmpty
// ============================================================================

func TestAllModelsEmpty_EmptyMap(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: make(map[string]*ModelConfig)}
	if !cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be true for empty model map")
	}
}

func TestAllModelsEmpty_NilMap(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: nil}
	// nil map — for range over nil map iterates zero times → true
	if !cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be true for nil model map")
	}
}

func TestAllModelsEmpty_AllEmptyFields(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: map[string]*ModelConfig{
		"a": {},
		"b": {},
	}}
	if !cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be true when all models have empty fields")
	}
}

func TestAllModelsEmpty_HasName(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: map[string]*ModelConfig{
		"a": {ModelBase: ModelBase{Name: "gpt-4"}},
	}}
	if cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be false when a model has a Name")
	}
}

func TestAllModelsEmpty_HasAPIType(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: map[string]*ModelConfig{
		"a": {ModelBase: ModelBase{APIType: "openai"}},
	}}
	if cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be false when a model has an APIType")
	}
}

func TestAllModelsEmpty_HasModel(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: map[string]*ModelConfig{
		"a": {ModelBase: ModelBase{Model: "deepseek-chat"}},
	}}
	if cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be false when a model has a Model field")
	}
}

func TestAllModelsEmpty_Mixed(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{Models: map[string]*ModelConfig{
		"a": {},
		"b": {ModelBase: ModelBase{Model: "gpt-4"}},
	}}
	if cm.allModelsEmpty(cfg) {
		t.Error("allModelsEmpty() should be false when at least one model has a field set")
	}
}

// ============================================================================
// Compression defaults & applyDefaults clamping
// ============================================================================

func TestApplyDefaults_CompressionMode_EmptyStringDefaultsToToken(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{}
	cfg.Tools.CompressionMode = ""
	cfg.Tools.CompressionThreshold = 0.8 // valid threshold to avoid interference
	cm.applyDefaults(cfg)
	if cfg.Tools.CompressionMode != "token" {
		t.Errorf("CompressionMode with empty string: got %q, want %q", cfg.Tools.CompressionMode, "token")
	}
}

func TestApplyDefaults_CompressionThreshold_ZeroDefaultsTo08(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{}
	cfg.Tools.CompressionMode = "token"
	cfg.Tools.CompressionThreshold = 0
	cm.applyDefaults(cfg)
	if cfg.Tools.CompressionThreshold != 0.8 {
		t.Errorf("CompressionThreshold with 0: got %v, want 0.8", cfg.Tools.CompressionThreshold)
	}
}

func TestApplyDefaults_CompressionThreshold_ClampedToMin(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{}
	cfg.Tools.CompressionMode = "token"
	cfg.Tools.CompressionThreshold = 0.05
	cm.applyDefaults(cfg)
	if cfg.Tools.CompressionThreshold != 0.1 {
		t.Errorf("CompressionThreshold with 0.05: got %v, want 0.1", cfg.Tools.CompressionThreshold)
	}
}

func TestApplyDefaults_CompressionThreshold_ClampedToMax(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{}
	cfg.Tools.CompressionMode = "token"
	cfg.Tools.CompressionThreshold = 0.95
	cm.applyDefaults(cfg)
	if cfg.Tools.CompressionThreshold != 0.9 {
		t.Errorf("CompressionThreshold with 0.95: got %v, want 0.9", cfg.Tools.CompressionThreshold)
	}
}

func TestApplyDefaults_CompressionThreshold_WithinRange_Unchanged(t *testing.T) {
	cm := &ConfigManager{}
	cfg := &Config{}
	cfg.Tools.CompressionMode = "token"
	cfg.Tools.CompressionThreshold = 0.5
	cm.applyDefaults(cfg)
	if cfg.Tools.CompressionThreshold != 0.5 {
		t.Errorf("CompressionThreshold with 0.5: got %v, want 0.5", cfg.Tools.CompressionThreshold)
	}
}

// ============================================================================
// UpdateCompressionConfig validation
// ============================================================================

func TestUpdateCompressionConfig_ValidTokenMode(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	err := cm.UpdateCompressionConfig("token", 0.6)
	if err != nil {
		t.Fatalf("UpdateCompressionConfig(token, 0.6) unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionMode != "token" {
		t.Errorf("CompressionMode = %q, want %q", cfg.Tools.CompressionMode, "token")
	}
	if cfg.Tools.CompressionThreshold != 0.6 {
		t.Errorf("CompressionThreshold = %v, want 0.6", cfg.Tools.CompressionThreshold)
	}
}

func TestUpdateCompressionConfig_ValidMessageMode(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	err := cm.UpdateCompressionConfig("message", 0.3)
	if err != nil {
		t.Fatalf("UpdateCompressionConfig(message, 0.3) unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionMode != "message" {
		t.Errorf("CompressionMode = %q, want %q", cfg.Tools.CompressionMode, "message")
	}
}

func TestUpdateCompressionConfig_InvalidMode(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	err := cm.UpdateCompressionConfig("invalid", 0.5)
	if err == nil {
		t.Error("UpdateCompressionConfig(invalid, ...) should return error")
	}
}

func TestUpdateCompressionConfig_ThresholdBelowMin_Clamped(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	err := cm.UpdateCompressionConfig("", 0.05)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionThreshold != 0.1 {
		t.Errorf("CompressionThreshold with 0.05: got %v, want 0.1", cfg.Tools.CompressionThreshold)
	}
}

func TestUpdateCompressionConfig_ThresholdAboveMax_Clamped(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	err := cm.UpdateCompressionConfig("", 0.99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionThreshold != 0.9 {
		t.Errorf("CompressionThreshold with 0.99: got %v, want 0.9", cfg.Tools.CompressionThreshold)
	}
}

func TestUpdateCompressionConfig_ThresholdZero_KeepsCurrent(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	// First, set a known threshold
	cm.UpdateCompressionConfig("", 0.7)
	// Then send zero threshold — should keep 0.7
	err := cm.UpdateCompressionConfig("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionThreshold != 0.7 {
		t.Errorf("CompressionThreshold should stay 0.7 when 0 is passed, got %v", cfg.Tools.CompressionThreshold)
	}
}

func TestUpdateCompressionConfig_EmptyMode_KeepsCurrent(t *testing.T) {
	cm := setupTempConfigManager(t)
	defer cleanupTempConfigManager(cm)

	// First, set to "message"
	cm.UpdateCompressionConfig("message", 0)
	// Then send empty mode — should keep "message"
	err := cm.UpdateCompressionConfig("", 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := cm.GetConfig()
	if cfg.Tools.CompressionMode != "message" {
		t.Errorf("CompressionMode should stay 'message' when empty string is passed, got %q", cfg.Tools.CompressionMode)
	}
}

// setupTempConfigManager creates a ConfigManager with a temporary config file
func setupTempConfigManager(t *testing.T) *ConfigManager {
	t.Helper()
	tmpDir := t.TempDir()

	cm := &ConfigManager{
		configPath: tmpDir + "/config.toon",
		execDir:    tmpDir,
	}
	cm.config = cm.createDefaultConfig()
	// Ensure compression defaults are set so tests start from a clean state
	cm.config.Tools.CompressionMode = "token"
	cm.config.Tools.CompressionThreshold = 0.8

	return cm
}

// cleanupTempConfigManager syncs globals after a temp config manager test
func cleanupTempConfigManager(cm *ConfigManager) {
	// Reset globals to not interfere with other tests
	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8
}
