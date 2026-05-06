package main

import (
	"strings"
	"testing"
)

// ============================================================================
// filterDeferredTools — Core tier 保留，非 core 排除
// ============================================================================

func TestFilterDeferredTools_ReducesNonCore(t *testing.T) {
	// 用 registry 入面嘅真實工具構建輸入（OpenAI format）
	toolList := getOpenAIToolsFromRegistry()
	if len(toolList) == 0 {
		t.Fatal("getOpenAIToolsFromRegistry returned empty")
	}

	allCount := len(toolList)
	result := filterDeferredTools("openai", interface{}(toolList))
	filtered, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatal("expected []map[string]interface{}")
	}

	if len(filtered) >= allCount {
		t.Errorf("should reduce tool count: all=%d filtered=%d", allCount, len(filtered))
	}
	if len(filtered) == 0 {
		t.Error("should keep at least some core tools")
	}

	// 確保所有保留嘅工具都係 core tier
	for _, ft := range filtered {
		name := getToolName(ft)
		td, exists := toolRegistryMap[name]
		if !exists {
			t.Errorf("tool %s not in registry", name)
			continue
		}
		if td.Tier != "core" {
			t.Errorf("tool %s has tier '%s', should be 'core'", name, td.Tier)
		}
	}
}

func TestFilterDeferredTools_MixedFormatsPreserved(t *testing.T) {
	// Anthropic format (直接 "name" key)
	anthList := getAnthropicToolsFromRegistry()
	if len(anthList) == 0 {
		t.Fatal("getAnthropicToolsFromRegistry returned empty")
	}
	result := filterDeferredTools("anthropic", interface{}(anthList))
	filtered, ok := result.([]map[string]interface{})
	if !ok {
		t.Fatal("expected []map[string]interface{}")
	}
	if len(filtered) >= len(anthList) {
		t.Errorf("should reduce tool count: all=%d filtered=%d", len(anthList), len(filtered))
	}
}

func TestFilterDeferredTools_NotMapSlice(t *testing.T) {
	result := filterDeferredTools("openai", "not_a_slice")
	if result != "not_a_slice" {
		t.Error("should return input unchanged for non-slice")
	}
}

// ============================================================================
// GetDeferredToolNames
// ============================================================================

func TestGetDeferredToolNames_ReturnsNames(t *testing.T) {
	result := GetDeferredToolNames()
	if result == "" {
		t.Log("no deferred tools configured")
		return
	}
	if !strings.Contains(result, "Menu") {
		t.Errorf("expected 'Menu' reference, got: %.80s", result)
	}
	if !strings.Contains(result, "個") {
		t.Errorf("expected count suffix '個', got: %.80s", result)
	}
}

// ============================================================================
// hasConfigDisabledTools
// ============================================================================

func TestHasConfigDisabledTools_Default(t *testing.T) {
	// 默認配置（SmartShell enabled, browser tools enabled）
	if hasConfigDisabledTools() {
		t.Log("some tools are disabled by config (expected in certain environments)")
	}
}

func TestHasConfigDisabledTools_NoPanic(t *testing.T) {
	// 確保函數唔會 panic（access nil pointer etc）
	_ = hasConfigDisabledTools()
}
