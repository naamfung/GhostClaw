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

// RefreshAPIConfig 從配置管理器刷新 API 相關配置欄位。
// 當用戶喺 AgentLoop 運行期間切換模型時，下次迭代會自動使用新配置，
// 避免請求繼續發送去舊嘅模型服務商。
func (c *AgentLoopConfig) RefreshAPIConfig() {
	useAPIType, useBaseURL, useAPIKey, useModelID, useTemp, useMaxTokens, _, _ := getEffectiveAPIConfig()
	c.EffectiveAPIType = useAPIType
	c.EffectiveBaseURL = useBaseURL
	c.EffectiveAPIKey = useAPIKey
	c.EffectiveModelID = useModelID
	c.EffectiveTemperature = useTemp
	c.EffectiveMaxTokens = useMaxTokens
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
	// 注意：新 session 第一條消息都要做分類 — 否則模型會繞過工作模式直接執行
	// 但如果當前任務已經結構化（已用 todos 或已入 plan mode），跳過重新分類，
	// 避免同一任務內嘅連續對話被重複要求做選擇
	taskAlreadyStructured := globalTaskTracker != nil && globalTaskTracker.IsWorkMode() &&
		(!TODO.IsEmpty() || (globalTasksMode != nil && globalTasksMode.IsActive()))

	if globalTaskTracker != nil && !taskAlreadyStructured {
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
			// 升級 CHAT → TASK：如有未完成 todo，說明用戶可能係延續之前嘅工作
			// 短句如「繼續」、「搞掂？」會被 LLM 判為 CHAT，但應該以工作模式處理
			if intent == IntentChat && !TODO.IsEmpty() && TODO.HasUnfinishedItems() {
				log.Printf("[AgentLoop] Intent upgraded CHAT→TASK: unfinished todos exist")
				intent = IntentTask
			}
			globalTaskTracker.StartNewTask(latestQuery, intent)
			log.Printf("[AgentLoop] Intent classified as: %d (0=CHAT, 1=TASK), query: %.100s", intent, latestQuery)
		}
	} else if taskAlreadyStructured {
		log.Printf("[AgentLoop] Task already structured (todos=%v, plan=%v), skipping re-classification",
			!TODO.IsEmpty(), globalTasksMode != nil && globalTasksMode.IsActive())
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
	// 只在任務未結構化時注入（即模型尚未選擇 todos 或 EnterPlanMode）
	if globalTaskTracker != nil && globalTaskTracker.IsWorkMode() && !taskAlreadyStructured {
		workModeHint := "\n\n[工作模式] 用戶的請求被識別為任務。你**必須**嚴格按照以下流程執行，不可跳過：\n\n" +
			"1. 若需要了解任務背景，可先用讀取/搜索類工具蒐集所需資訊\n" +
			"2. 充分了解後，**強制選擇**以下兩種方式之一進行規劃（不可跳過此步直接執行）：\n" +
			"   - 簡單/明確任務（1-3 步驟）→ **必須使用 Todos 工具**設定待辦事項，逐項執行\n" +
			"   - 複雜任務（涉及多步驟、需要審慎規劃）→ **必須使用 EnterPlanMode** 進入結構化規劃\n\n" +
			"**嚴禁**：在未完成規劃（Todos 或 EnterPlanMode）之前，調用任何寫入/執行類工具。系統層面已強制攔截此類調用。"
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "system" {
				if content, ok := messages[i].Content.(string); ok {
					messages[i].Content = content + workModeHint
				}
				break
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
