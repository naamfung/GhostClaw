package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// SessionConfig — 會話管理配置（idle 任務接續檢查）
// ============================================================================

// SessionConfig 會話管理配置
type SessionConfig struct {
	// Idle 任務接續檢查
	IdleTaskCheckEnabled bool `toon:"IdleTaskCheckEnabled" json:"IdleTaskCheckEnabled"` // 是否啟用 idle 任務接續檢查
	IdleTaskCheckMins    int  `toon:"IdleTaskCheckMins" json:"IdleTaskCheckMins"`       // idle 超時（分鐘），0=禁用，默認30
}

// DefaultSessionConfig 返回默認配置
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		IdleTaskCheckEnabled: true,
		IdleTaskCheckMins:    30,
	}
}

// EffectiveSessionConfig 從全局配置獲取會話管理配置，缺失字段用默認值填充
func EffectiveSessionConfig() SessionConfig {
	cfg := DefaultSessionConfig()
	if globalConfig.Session != nil {
		if globalConfig.Session.IdleTaskCheckEnabled {
			cfg.IdleTaskCheckEnabled = true
		}
		if globalConfig.Session.IdleTaskCheckMins > 0 {
			cfg.IdleTaskCheckMins = globalConfig.Session.IdleTaskCheckMins
		}
	}
	return cfg
}

// SessionStats — 會話級別的累計統計

// SessionStats 會話級別的累計統計數據
type SessionStats struct {
	InputTokens  int `json:"input_tokens"`  // 累計輸入 token
	OutputTokens int `json:"output_tokens"` // 累計輸出 token
	TotalTokens  int `json:"total_tokens"`  // 累計總 token

	TurnCount        int       `json:"turn_count"`         // 會話輪次（用戶消息數）
	LastPromptTokens int       `json:"last_prompt_tokens"` // 最近一次 API 調用的 prompt tokens（用於壓縮預檢）
	LastAPICallAt    time.Time `json:"last_api_call_at"`   // 最近一次 API 調用時間
}

// ============================================================================
// SessionTracker — 會話追蹤器（idle 任務接續檢查 + token 追蹤）
// ============================================================================

// SessionTracker 會話追蹤器，管理 idle 任務接續檢查和 token 追蹤
// 適配 GhostClaw 的 GlobalSession 單會話架構
type SessionTracker struct {
	mu              sync.RWMutex
	stats           SessionStats
	cfg             SessionConfig
	started         bool
	idleCheckPaused bool // COMPLETE 後暫停後續 idle check，/new 時重置
}

// NewSessionTracker 創建新的會話追蹤器
func NewSessionTracker(cfg SessionConfig) *SessionTracker {
	return &SessionTracker{
		cfg: cfg,
	}
}

// Reset 重置會話追蹤器（會話重置時調用）
func (st *SessionTracker) Reset(reason string, hadActivity bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stats = SessionStats{}
	st.started = false
	st.idleCheckPaused = false
	log.Printf("[SessionTracker] Session reset: reason=%s, had_activity=%v", reason, hadActivity)
}

// RecordAPICall 記錄一次 API 調用的 token 使用量
func (st *SessionTracker) RecordAPICall(usage TokenUsage) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.started = true
	st.stats.InputTokens += usage.PromptTokens
	st.stats.OutputTokens += usage.CompletionTokens
	st.stats.TotalTokens += usage.TotalTokens
	st.stats.LastPromptTokens = usage.PromptTokens
	st.stats.LastAPICallAt = time.Now()
}

// RecordTurn 記錄一輪對話（用戶消息）
func (st *SessionTracker) RecordTurn() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.started = true
	st.stats.TurnCount++
}

// GetStats 返回當前會話統計的副本
func (st *SessionTracker) GetStats() SessionStats {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.stats
}

// GetLastPromptTokens 返回最近一次 API 調用的 prompt tokens
// 用於 ContextCompressor 的精確壓縮預檢
func (st *SessionTracker) GetLastPromptTokens() int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.stats.LastPromptTokens
}

// ShouldCheckTaskOnIdle 檢查是否需要因 idle 超時而進行任務接續檢查
// lastActivity: 最後一次活動時間（通常是 GlobalSession.LastSeen）
// 返回是否應該檢查
func (st *SessionTracker) ShouldCheckTaskOnIdle(lastActivity time.Time) bool {
	if !st.cfg.IdleTaskCheckEnabled || st.cfg.IdleTaskCheckMins <= 0 {
		return false
	}
	if !st.started {
		return false // 從未有活動，無需檢查
	}

	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.idleCheckPaused {
		return false
	}
	idleDeadline := lastActivity.Add(time.Duration(st.cfg.IdleTaskCheckMins) * time.Minute)
	return time.Now().After(idleDeadline)
}

// GetConfig 返回會話追蹤器配置（只讀副本）
func (st *SessionTracker) GetConfig() SessionConfig {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.cfg
}

// FormatStatsForPrompt 將會話統計格式化為注入到 system prompt 的信息
// 模型可見，幫助模型了解當前會話的 token 消耗狀況
func (st *SessionTracker) FormatStatsForPrompt() string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.stats.TotalTokens == 0 && st.stats.TurnCount == 0 {
		return ""
	}

	return formatTokenStats(st.stats)
}

// formatTokenStats 格式化 token 統計信息
func formatTokenStats(stats SessionStats) string {
	var sb strings.Builder
	sb.WriteString("\n## 當前會話 Token 使用統計\n")
	sb.WriteString(fmt.Sprintf("- 累計輸入 token: %d\n", stats.InputTokens))
	sb.WriteString(fmt.Sprintf("- 累計輸出 token: %d\n", stats.OutputTokens))
	sb.WriteString(fmt.Sprintf("- 累計總 token: %d\n", stats.TotalTokens))
	sb.WriteString(fmt.Sprintf("- 對話輪次: %d\n", stats.TurnCount))
	sb.WriteString("\n")
	return sb.String()
}

// ============================================================================
// Idle Check 控制
// ============================================================================

// PauseIdleCheck 暫停後續 idle 任務接續檢查（COMPLETE 後調用）
func (st *SessionTracker) PauseIdleCheck() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.idleCheckPaused = true
	log.Printf("[SessionTracker] Idle check paused")
}

// ResumeIdleCheck 恢復 idle 任務接續檢查
func (st *SessionTracker) ResumeIdleCheck() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.idleCheckPaused = false
	log.Printf("[SessionTracker] Idle check resumed")
}

// IsIdleCheckPaused 返回 idle 檢查是否已暫停
func (st *SessionTracker) IsIdleCheckPaused() bool {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.idleCheckPaused
}
