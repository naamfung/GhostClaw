package main

import (
        "fmt"
        "strings"
)

// ============================================================================
// 廉價模型分離策略 - 靈感來自 cc-mini 的 Buddy/Companion 系統
// ============================================================================

// CompanionModelConfig 伴侶模型配置
// 用於處理非關鍵任務，降低 API 成本
type CompanionModelConfig struct {
        Enabled   bool   `json:"Enabled"`             // 是否啟用
        ModelName string `json:"ModelName,omitempty"`  // 伴侶模型名稱
        APIType   string `json:"APIType,omitempty"`    // API 類型
        BaseURL   string `json:"BaseURL,omitempty"`    // API 基礎 URL
        APIKey    string `json:"APIKey,omitempty"`     // API Key
        // 伴侶模型的使用場景
        UseForCompression bool `json:"UseForCompression,omitempty"` // 用於上下文壓縮
        UseForMemory      bool `json:"UseForMemory,omitempty"`      // 用於記憶整合
        UseForReaction    bool `json:"UseForReaction,omitempty"`    // 用於用戶反應
}

// DefaultCompanionConfig 返回默認的伴侶模型配置
// 當主模型是高成本模型時，自動選擇同系列的低成本模型作為伴侶
func DefaultCompanionConfig(mainModel string) *CompanionModelConfig {
        mainLower := strings.ToLower(mainModel)

        // 根據主模型自動選擇伴侶模型
        switch {
        // Claude 系列伴侶 -> Haiku（成本約為 Sonnet 的 1/10-1/15）
        case strings.Contains(mainLower, "claude-3-opus"),
                strings.Contains(mainLower, "claude-3.5-sonnet"),
                strings.Contains(mainLower, "claude-3-sonnet"):
                return &CompanionModelConfig{
                        Enabled:           true,
                        ModelName:         "claude-3-haiku-20240307",
                        APIType:           "anthropic",
                        UseForCompression: true,
                        UseForMemory:      true,
                }

        // GPT-4 系列伴侶 -> GPT-4o-mini
        case strings.Contains(mainLower, "gpt-4"),
                strings.Contains(mainLower, "gpt-4o") && !strings.Contains(mainLower, "mini"):
                return &CompanionModelConfig{
                        Enabled:           true,
                        ModelName:         "gpt-4o-mini",
                        APIType:           "openai",
                        UseForCompression: true,
                        UseForMemory:      true,
                }

        // DeepSeek 系列伴侶 -> deepseek-chat（如果主模型是 reasoner）
        case strings.Contains(mainLower, "deepseek-reasoner"):
                return &CompanionModelConfig{
                        Enabled:           true,
                        ModelName:         "deepseek-chat",
                        APIType:           "openai",
                        UseForCompression: true,
                        UseForMemory:      true,
                }

        // GLM 系列伴侶 -> glm-4-flash
        case strings.Contains(mainLower, "glm-4-plus"),
                strings.Contains(mainLower, "glm-4"):
                return &CompanionModelConfig{
                        Enabled:           true,
                        ModelName:         "glm-4-flash",
                        APIType:           "openai",
                        UseForCompression: true,
                        UseForMemory:      true,
                }

        // Qwen 系列伴侶 -> qwen-turbo
        case strings.Contains(mainLower, "qwen-max"),
                strings.Contains(mainLower, "qwen-plus"):
                return &CompanionModelConfig{
                        Enabled:           true,
                        ModelName:         "qwen-turbo",
                        APIType:           "openai",
                        UseForCompression: true,
                        UseForMemory:      true,
                }

        default:
                // 對於低成本模型（本地模型等），不啟用伴侶模型
                return &CompanionModelConfig{
                        Enabled: false,
                }
        }
}

// ============================================================================
// 記憶邊界管理 - 靈感來自 cc-mini 的 KAIROS 記憶系統
// ============================================================================

// MemoryBoundaryConfig 記憶邊界配置
// 定義什麼該記住、什麼不該記住，防止記憶膨脹
type MemoryBoundaryConfig struct {
        // 應該保存的記憶類型
        SaveUserPreferences bool `json:"SaveUserPreferences"` // 用戶偏好和角色
        SaveUserFeedback    bool `json:"SaveUserFeedback"`    // 用戶糾正（最珍貴的記憶）
        SaveProjectContext  bool `json:"SaveProjectContext"`  // 無法從代碼推導的項目上下文
        SaveExternalRefs    bool `json:"SaveExternalRefs"`    // 外部系統指針

        // 不應該保存的內容（防止記憶膨脹）
        SkipCodePatterns   bool `json:"SkipCodePatterns"`   // 代碼模式（可從閱讀代碼獲得）
        SkipFilePaths      bool `json:"SkipFilePaths"`      // 文件路徑（可通過搜索獲得）
        SkipGitHistory     bool `json:"SkipGitHistory"`     // Git 歷史（git log 是權威來源）
        SkipDebugSolutions bool `json:"SkipDebugSolutions"` // 調試方案（修復已在代碼中）
}

// DefaultMemoryBoundary 返回默認的記憶邊界配置
func DefaultMemoryBoundary() *MemoryBoundaryConfig {
        return &MemoryBoundaryConfig{
                // 保存
                SaveUserPreferences: true,
                SaveUserFeedback:    true,  // 最高優先級
                SaveProjectContext:  true,
                SaveExternalRefs:    true,
                // 跳過
                SkipCodePatterns:   true,
                SkipFilePaths:      true,
                SkipGitHistory:     true,
                SkipDebugSolutions: true,
        }
}

// ShouldSaveMemory 判斷是否應該保存某條記憶
func (mc *MemoryBoundaryConfig) ShouldSaveMemory(category string) bool {
        switch category {
        case "user_preference":
                return mc.SaveUserPreferences
        case "user_feedback":
                return mc.SaveUserFeedback
        case "project_context":
                return mc.SaveProjectContext
        case "external_ref":
                return mc.SaveExternalRefs
        case "code_pattern":
                return !mc.SkipCodePatterns
        case "file_path":
                return !mc.SkipFilePaths
        case "git_history":
                return !mc.SkipGitHistory
        case "debug_solution":
                return !mc.SkipDebugSolutions
        default:
                return false // 未知類型默認不保存
        }
}

// GetMemoryFilterPrompt 獲取記憶過濾提示
// 用於指導 LLM 在整合記憶時過濾低價值內容
func GetMemoryFilterPrompt() string {
        return `記憶整合規則 - 請在整合記憶時遵守以下邊界：

## 應該保存的內容
- 用戶的個人偏好、角色設定、工作習慣
- 用戶的糾正和反饋（這是最有價值的記憶，防止重複犯錯）
- 無法從代碼中推導的項目上下文和背景知識
- 外部系統的連接信息（API 端點、服務地址等）

## 不應該保存的內容（防止記憶膨脹）
- 具體的代碼模式和實現細節（可直接閱讀代碼獲得）
- 文件路徑列表（可通過搜索工具獲得）
- Git 提交歷史（git log 是權威來源）
- 調試過程中的錯誤和修復方案（修復已體現在代碼中）
- 臨時的命令輸出和工具返回結果

## 整合原則
- 合併重複信息，不要創建重複條目
- 將相對日期轉換為絕對日期
- 刪除已被後續信息推翻的事實
- 保持記憶索引在 200 條以內
- 每條記憶應該簡潔明確，不超過 100 字`
}

// ============================================================================
// 成本追蹤器
// ============================================================================

// CostTracker API 成本追蹤器
type CostTracker struct {
        MainModelTokens    int64 // 主模型消耗的 token 數
        CompanionTokens    int64 // 伴侶模型消耗的 token 數
        MainModelCalls     int64 // 主模型調用次數
        CompanionCalls     int64 // 伴侶模型調用次數
        SavedByCompanion   int64 // 通過使用伴侶模型節省的 token 數（估算）
}

var globalCostTracker = &CostTracker{}

// RecordMainModelCall 記錄主模型調用
func (ct *CostTracker) RecordMainModelCall(tokens int64) {
        ct.MainModelTokens += tokens
        ct.MainModelCalls++
}

// RecordCompanionCall 記錄伴侶模型調用
func (ct *CostTracker) RecordCompanionCall(tokens int64, savedTokens int64) {
        ct.CompanionTokens += tokens
        ct.CompanionCalls++
        ct.SavedByCompanion += savedTokens
}

// GetSavingsRatio 計算節省比例
func (ct *CostTracker) GetSavingsRatio() float64 {
        if ct.MainModelTokens+ct.SavedByCompanion == 0 {
                return 0
        }
        return float64(ct.SavedByCompanion) / float64(ct.MainModelTokens+ct.SavedByCompanion) * 100
}

// GetReport 獲取成本報告
func (ct *CostTracker) GetReport() string {
        mainTokens := ct.MainModelTokens
        companionTokens := ct.CompanionTokens
        savingsRatio := ct.GetSavingsRatio()

        report := fmt.Sprintf(`[成本追蹤報告]
主模型: %d tokens (%d 次調用)
伴侶模型: %d tokens (%d 次調用)
估算節省: %d tokens (%.1f%%)`,
                mainTokens, ct.MainModelCalls,
                companionTokens, ct.CompanionCalls,
                ct.SavedByCompanion, savingsRatio)

        if ct.CompanionCalls > 0 {
                report += fmt.Sprintf(`
節省分析: 通過將 %d 次非關鍵任務轉移到伴侶模型，
估計節省了約 %d tokens 的主模型消耗。`, ct.CompanionCalls, ct.SavedByCompanion)
        }

        return report
}

// ShouldUseCompanion 判斷某任務是否應該使用伴侶模型
func (ct *CostTracker) ShouldUseCompanion(taskType string, config *CompanionModelConfig) bool {
        if config == nil || !config.Enabled {
                return false
        }

        switch taskType {
        case "compression":
                return config.UseForCompression
        case "memory":
                return config.UseForMemory
        case "reaction":
                return config.UseForReaction
        default:
                return false
        }
}

// NOTE: 此文件為預留架構，companion_model 功能尚待集成。
// 整合時需在 ModelConfig 中新增 CompanionModel 字段，
// 並在 MemoryConsolidator、上下文壓縮、反饋生成等模塊中調用。
//
// 已實現但待接入的組件：
//   - CompanionModelConfig + DefaultCompanionConfig() → 自動選擇伴侶模型
//   - MemoryBoundaryConfig + ShouldSaveMemory() → 記憶邊界管理
//   - GetMemoryFilterPrompt() → 記憶過濾提示
//   - CostTracker → API 成本追蹤
