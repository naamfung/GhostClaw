package main

import (
	"context"
	"log"
	"time"
)

// ============================================================================
// loop_setup.go — Pre-loop 設置
// ============================================================================
// 從 AgentLoop L308-557 抽出：
//   - 記憶上下文注入（Prefetch）
//   - Role/model config 解析
//   - LLM 二元分類（CHAT vs TASK）
//   - System prompt 注入/更新
//   - 工作模式提示注入
//   - Token 統計信息注入
//   - 用戶消息記錄到記憶整合器

// AgentLoopConfig holds the resolved configuration for an AgentLoop run.
type AgentLoopConfig struct {
	EffectiveAPIType     string
	EffectiveBaseURL     string
	EffectiveAPIKey      string
	EffectiveModelID     string
	EffectiveTemperature float64
	EffectiveMaxTokens   int
	CurrentRole          *Role
	IsNewSession         bool
	Compressor           *ContextCompressor
}

// RunPreLoopSetup performs all pre-loop initialization.
// Modifies messages in place and returns the resolved configuration.
func RunPreLoopSetup(ctx context.Context, messages []Message, apiType, baseURL, apiKey, modelID string,
	temperature float64, maxTokens int) ([]Message, *AgentLoopConfig) {

	config := &AgentLoopConfig{
		EffectiveAPIType:     apiType,
		EffectiveBaseURL:     baseURL,
		EffectiveAPIKey:      apiKey,
		EffectiveModelID:     modelID,
		EffectiveTemperature: temperature,
		EffectiveMaxTokens:   maxTokens,
		Compressor:           NewContextCompressor(),
	}

	// 每輪 AgentLoop（用戶發新消息）重置循環檢測器
	if globalLoopDetector != nil {
		globalLoopDetector.Clear()
	}

	// ====== 注入記憶上下文 ======
	session := GetGlobalSession()
	isNewSession := session.ConsumeIsNewSession()
	config.IsNewSession = isNewSession

	if globalUnifiedMemory != nil {
		var latestUserMessage string
		var latestUserIdx int = -1
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if content, ok := messages[i].Content.(string); ok && content != "" {
					latestUserMessage = content
				}
				latestUserIdx = i
				break
			}
		}

		var memoryContext string
		if isNewSession {
			log.Printf("[AgentLoop] New session detected, injecting user context only (no experiences)")
			memoryContext = globalUnifiedMemory.GetUserContext()
		} else {
			taskDesc := getCurrentTaskDescriptionFromMessages(messages)
			if latestUserMessage != "" {
				taskDesc = latestUserMessage
			}
			memoryContext = globalUnifiedMemory.GetContextForPrompt(taskDesc)
		}

		fencedBlock := BuildMemoryContextBlock(memoryContext)
		if fencedBlock != "" && latestUserIdx >= 0 {
			insertIdx := latestUserIdx
			memMsg := Message{Role: "system", Content: fencedBlock}
			messages = append(messages[:insertIdx], append([]Message{memMsg}, messages[insertIdx:]...)...)
		}
	}

	// ====== 解析 Role 和模型配置 ======
	if globalRoleManager != nil && globalActorManager != nil && globalStage != nil {
		currentActor := globalStage.GetCurrentActor()
		if actor, ok := globalActorManager.GetActor(currentActor); ok {
			config.CurrentRole, _ = globalRoleManager.GetRole(actor.Role)
			if modelConfig := getActorModelConfig(currentActor); modelConfig != nil {
				if modelConfig.APIType != "" {
					config.EffectiveAPIType = modelConfig.APIType
				}
				if modelConfig.BaseURL != "" {
					config.EffectiveBaseURL = modelConfig.BaseURL
				}
				if modelConfig.APIKey != "" {
					config.EffectiveAPIKey = modelConfig.ResolveAPIKey()
				}
				if modelConfig.Model != "" {
					config.EffectiveModelID = modelConfig.Model
				}
				if modelConfig.Temperature > 0 {
					config.EffectiveTemperature = modelConfig.Temperature
				}
				if modelConfig.MaxTokens > 0 {
					config.EffectiveMaxTokens = modelConfig.MaxTokens
				}
			}
		}
	}

	// ====== LLM 二元分類：CHAT vs TASK ======
	if !isNewSession {
		if globalTaskTracker != nil {
			var latestQuery string
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					if content, ok := messages[i].Content.(string); ok {
						latestQuery = content
					}
					break
				}
			}
			if latestQuery != "" {
				intent, err := ClassifyUserIntent(ctx, latestQuery, config.EffectiveAPIType, config.EffectiveBaseURL, config.EffectiveAPIKey, config.EffectiveModelID)
				if err != nil {
					log.Printf("[AgentLoop] LLM classification failed: %v, defaulting to TASK", err)
					intent = IntentTask
				}
				globalTaskTracker.StartNewTask(latestQuery, intent)
				log.Printf("[AgentLoop] Intent classified as: %d (0=CHAT, 1=TASK), query: %.100s", intent, latestQuery)
			}
		}
	}

	// ====== 注入或更新系統提示 ======
	if len(messages) > 0 {
		hasSystemPrompt := false
		systemPromptIndex := -1
		for i, msg := range messages {
			if msg.Role == "system" {
				hasSystemPrompt = true
				systemPromptIndex = i
				break
			}
		}

		needUpdate := false
		if globalStage != nil {
			needUpdate = globalStage.NeedUpdateSystemPrompt()
		}

		if !hasSystemPrompt || needUpdate {
			var systemPrompt string

			if globalRoleManager != nil && globalActorManager != nil && globalStage != nil {
				currentActor := globalStage.GetCurrentActor()
				modelCtx := GetModelContextLengthSafe(config.EffectiveModelID)
				if modelCtx > 0 {
					systemPrompt = BuildAdaptiveSystemPrompt(currentActor, globalActorManager, globalRoleManager, globalStage, modelCtx, 0, 0, config.EffectiveMaxTokens)
				} else {
					systemPrompt = BuildSystemPromptForActor(currentActor, globalActorManager, globalRoleManager, globalStage)
				}
			} else {
				systemPrompt = SYSTEM_PROMPT
			}

			// 語言憲章注入
			systemPrompt = BuildLanguageCharter(globalConfig.DefaultLanguage) + systemPrompt

			// Bootstrap: 首次對話引導
			if globalUnifiedMemory != nil && IsBootstrapNeeded(globalUnifiedMemory) {
				bootstrapPrompt := GetBootstrapMissingKeysPrompt(globalUnifiedMemory)
				if bootstrapPrompt != "" {
					systemPrompt = bootstrapPrompt + "\n\n---\n\n" + systemPrompt
				}
			}

			if systemPrompt != "" {
				if needUpdate && systemPromptIndex >= 0 {
					messages[systemPromptIndex] = Message{Role: "system", Content: systemPrompt}
					globalStage.ClearUpdateSystemPrompt()
				} else {
					messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)
				}
			}
		}
	}

	// ====== 注入工作模式提示 ======
	if globalTaskTracker != nil && globalTaskTracker.IsWorkMode() {
		planModeActive := globalPlanMode != nil && globalPlanMode.IsActive()
		if !planModeActive {
			workModeHint := "\n\n[工作模式] 用戶的請求被識別為任務。\n" +
				"在開始執行前，請先向用戶確認任務的具體需求、範圍和期望結果。\n\n" +
				"根據用戶提供的詳細資訊，自行判斷任務複雜度並選擇合適的工作方式：\n" +
				"- 簡單/明確任務（1-3 步驟）→ 使用 todos 工具規劃並執行\n" +
				"- 複雜任務（涉及多檔案、多步驟、需要審慎規劃）→ 使用 EnterPlanMode 進入結構化規劃\n\n" +
				"不要基於模糊的單句請求就做重大技術決策。"
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "system" {
					if content, ok := messages[i].Content.(string); ok {
						messages[i].Content = content + workModeHint
					}
					break
				}
			}
		}
	}

	// ====== 注入會話 token 統計信息 ======
	if !isNewSession {
		if tracker := session.GetTracker(); tracker != nil {
			if tokenStats := tracker.FormatStatsForPrompt(); tokenStats != "" {
				for i := len(messages) - 1; i >= 0; i-- {
					if messages[i].Role == "system" {
						if content, ok := messages[i].Content.(string); ok {
							messages[i].Content = content + tokenStats
						}
						break
					}
				}
			}
		}
	}

	// ====== 記錄用戶消息到記憶整合器 ======
	if globalMemoryConsolidator != nil && len(messages) > 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if content, ok := messages[i].Content.(string); ok && content != "" {
					globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
						Role:      "user",
						Content:   content,
						Timestamp: time.Now(),
					})
					break
				}
			}
		}
	}

	return messages, config
}
