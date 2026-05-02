package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
        "time"
)

// ============================================================================
// SessionConfig — 會話管理配置（idle 重置）
// ============================================================================

// SessionConfig 會話管理配置
type SessionConfig struct {
        // Idle 重置
        IdleResetEnabled bool `toon:"IdleResetEnabled" json:"IdleResetEnabled"`   // 是否啟用 idle 重置
        IdleTimeoutMins  int  `toon:"IdleTimeoutMins" json:"IdleTimeoutMins"`       // idle 超時（分鐘），0=禁用，默認30
}

// DefaultSessionConfig 返回默認配置
func DefaultSessionConfig() SessionConfig {
        return SessionConfig{
                IdleResetEnabled:  true,
                IdleTimeoutMins:   30,
        }
}

// EffectiveSessionConfig 從全局配置獲取會話管理配置，缺失字段用默認值填充
func EffectiveSessionConfig() SessionConfig {
        cfg := DefaultSessionConfig()
        if globalConfig.Session != nil {
                if globalConfig.Session.IdleResetEnabled {
                        cfg.IdleResetEnabled = true
                }
                if globalConfig.Session.IdleTimeoutMins > 0 {
                        cfg.IdleTimeoutMins = globalConfig.Session.IdleTimeoutMins
                }
        }
        return cfg
}

// SessionStats — 會話級別的累計統計

// SessionStats 會話級別的累計統計數據
type SessionStats struct {
        InputTokens  int `json:"input_tokens"`   // 累計輸入 token
        OutputTokens int `json:"output_tokens"`  // 累計輸出 token
        TotalTokens  int `json:"total_tokens"`   // 累計總 token

        TurnCount          int       `json:"turn_count"`            // 會話輪次（用戶消息數）
        LastPromptTokens   int       `json:"last_prompt_tokens"`    // 最近一次 API 調用的 prompt tokens（用於壓縮預檢）
        LastAPICallAt      time.Time `json:"last_api_call_at"`       // 最近一次 API 調用時間
        AutoResetReason    string    `json:"auto_reset_reason"`     // 自動重置原因（"idle"）
        AutoResetHadActivity bool    `json:"auto_reset_had_activity"` // 被重置的會話是否曾有活動
}

// ============================================================================
// SessionTracker — 會話追蹤器（idle 重置 + token 追蹤）
// ============================================================================

// SessionTracker 會話追蹤器，管理 idle 重置和 token 追蹤
// 適配 GhostClaw 的 GlobalSession 單會話架構
type SessionTracker struct {
        mu       sync.RWMutex
        stats    SessionStats
        cfg      SessionConfig
        started  bool
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
        st.stats = SessionStats{
                AutoResetReason:      reason,
                AutoResetHadActivity: hadActivity,
        }
        st.started = false
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

// ShouldIdleReset 檢查是否需要因 idle 超時而重置會話
// lastActivity: 最後一次活動時間（通常是 GlobalSession.LastSeen）
// 返回是否應該重置
func (st *SessionTracker) ShouldIdleReset(lastActivity time.Time) bool {
        if !st.cfg.IdleResetEnabled || st.cfg.IdleTimeoutMins <= 0 {
                return false
        }
        if !st.started {
                return false // 從未有活動，無需重置
        }

        st.mu.RLock()
        defer st.mu.RUnlock()
        idleDeadline := lastActivity.Add(time.Duration(st.cfg.IdleTimeoutMins) * time.Minute)
        return time.Now().After(idleDeadline)
}

// ConsumeAutoResetReason 消費自動重置原因（調用後清空，僅消費一次）
// 用於向用戶通知會話已被重置
func (st *SessionTracker) ConsumeAutoResetReason() string {
        st.mu.Lock()
        defer st.mu.Unlock()
        reason := st.stats.AutoResetReason
        hadActivity := st.stats.AutoResetHadActivity
        st.stats.AutoResetReason = ""
        st.stats.AutoResetHadActivity = false
        if reason == "" {
                return ""
        }
        log.Printf("[SessionTracker] Consumed auto reset reason: %s (had_activity=%v)", reason, hadActivity)
        return reason
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
// Idle Reset 通知消息構建
// ============================================================================

// BuildIdleResetNotice 構建 idle 重置的通知消息
// 當會話因 idle 超時被重置時，作為 system message 注入到新會話的第一條消息前
func BuildIdleResetNotice(idleMinutes int, hadActivity bool) string {
        var sb strings.Builder
        sb.WriteString("[系統通知] 由於長時間無活動（超過 ")
        sb.WriteString(fmt.Sprintf("%d 分鐘", idleMinutes))
        sb.WriteString("），會話已自動重置。以下是新會話。\n")
        if hadActivity {
                sb.WriteString("之前的對話上下文已被清除，記憶系統仍保留重要信息。\n")
        }
        return sb.String()
}
