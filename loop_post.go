package main

import (
	"context"
	"log"
	"strings"
	"time"
)

// ============================================================================
// loop_post.go — Post-loop 清理
// ============================================================================
// 從 AgentLoop L1363-1514 抽出：
//   - 隱式任務完成檢測（FeedbackCollector）
//   - Done 信號發送
//   - Token 追蹤與警告
//   - 每日日誌寫入
//   - LLM 自省學習
//   - 記憶整合
//   - 軌跡記錄
//   - 策略優化
//   - 記憶重構

// RunPostLoop performs all post-loop cleanup operations.
func RunPostLoop(ch Channel, messages []Message, iteration int,
	loopExitedNaturally bool, lastTokenUsage *TokenUsage,
	effectiveModelID, effectiveAPIType, effectiveBaseURL, effectiveAPIKey string) {

	// ====== 隱式任務完成檢測 ======
	if globalFeedbackCollector != nil && iteration > 0 && loopExitedNaturally {
		var triggerUserMsg string
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if content, ok := messages[i].Content.(string); ok && content != "" {
					if strings.HasPrefix(content, "[SYSTEM_RESUME]") {
						continue
					}
					triggerUserMsg = content
				}
				break
			}
		}

		if TODO.HasUnfinishedItems() {
			log.Printf("[FeedbackCollector] Skipping: active todo items exist, programmatic exit guard takes precedence")
		} else if IsWakeNotification(triggerUserMsg) {
			log.Printf("[FeedbackCollector] Skipping task completion check: input is a wake notification")
		} else if !globalFeedbackCollector.CanAskCompletion() {
			log.Printf("[FeedbackCollector] Skipping task completion check: cooldown active")
		} else {
			var lastUserMsg, lastAssistantMsg string
			for i := len(messages) - 1; i >= 0; i-- {
				if lastUserMsg == "" && messages[i].Role == "user" {
					if content, ok := messages[i].Content.(string); ok {
						lastUserMsg = content
					}
				}
				if lastAssistantMsg == "" && messages[i].Role == "assistant" {
					if content, ok := messages[i].Content.(string); ok {
						lastAssistantMsg = content
					}
				}
				if lastUserMsg != "" && lastAssistantMsg != "" {
					break
				}
			}
			if lastUserMsg != "" && lastAssistantMsg != "" {
				apiConfig := TaskCompletionQuery{
					APIType: effectiveAPIType,
					BaseURL: effectiveBaseURL,
					APIKey:  effectiveAPIKey,
					ModelID: effectiveModelID,
				}
				globalFeedbackCollector.RecordCompletionAsk()
				// 使用獨立 goroutine 異步執行，避免 FeedbackCollector timeout
				// 阻擋 done=true 發送，導致前端長時間等待後模型看似「無故終止」。
				go func(userMsg, assistantMsg string, cfg TaskCompletionQuery) {
					askCtx, askCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer askCancel()
					completed := globalFeedbackCollector.AskModelTaskCompletion(askCtx, userMsg, assistantMsg, cfg)
					if completed {
						globalFeedbackCollector.MarkTaskCompleted(userMsg, assistantMsg)
						log.Printf("[FeedbackCollector] Task marked as completed (implicit, no user prompt)")
					}
				}(lastUserMsg, lastAssistantMsg, apiConfig)
			}
		}
	}

	// ====== Done 信號 ======
	ch.WriteChunk(StreamChunk{Done: true})

	// ====== Token 追蹤 ======
	if lastTokenUsage != nil && lastTokenUsage.TotalTokens > 0 {
		session := GetGlobalSession()
		if tracker := session.GetTracker(); tracker != nil {
			tracker.RecordAPICall(*lastTokenUsage)
			stats := tracker.GetStats()
			log.Printf("[AgentLoop] Token usage recorded: prompt=%d, completion=%d, total=%d (session_total=%d)",
				lastTokenUsage.PromptTokens, lastTokenUsage.CompletionTokens,
				lastTokenUsage.TotalTokens, stats.TotalTokens)

		}
	}

	// ====== 每日日誌 ======
	if globalMemoryConsolidator != nil {
		sessionID := ch.GetSessionID()
		if sessionID != "" {
			if err := globalMemoryConsolidator.WriteDailyLog(sessionID, messages); err != nil {
				log.Printf("[MemoryConsolidator] WriteDailyLog error: %v", err)
			}
		}
	}

	// ====== LLM 自省學習 ======
	if globalSelfLearner != nil {
		taskDesc := getCurrentTaskDescriptionFromMessages(messages)
		sessionID := GetGlobalSession().ID
		go globalSelfLearner.Reflect(context.Background(), taskDesc, sessionID)
	}

	// ====== 自進化引擎（跨會話分析） ======
	if globalSelfEvolver != nil && ch.GetSessionID() != "" {
		sessionID := ch.GetSessionID()
		go globalSelfEvolver.AnalyzePromptEffectiveness(context.Background(), sessionID)
		go globalSelfEvolver.AnalyzeToolPatterns(context.Background(), sessionID)
		go globalSelfEvolver.AnalyzeErrorRecovery(context.Background(), sessionID)
		go globalSelfEvolver.SynthesizeCrossSession(context.Background())
	}

	// ====== 記憶整合 ======
	if globalMemoryConsolidator != nil {
		go func() {
			sessionKey := "default"
			if should, _ := globalMemoryConsolidator.ShouldConsolidate(sessionKey); should {
				log.Println("[MemoryConsolidator] Triggering automatic consolidation...")
				if err := globalMemoryConsolidator.MaybeConsolidate(context.Background(), sessionKey); err != nil {
					log.Printf("[MemoryConsolidator] Consolidation failed: %v", err)
				}
			}
		}()
	}

	// ====== 軌跡記錄 ======
	if globalTrajectoryManager != nil {
		go func() {
			modelUsed := effectiveModelID
			tokenUsage := TokenUsage{}
			success := true
			globalTrajectoryManager.RecordTrajectory(messages, success, modelUsed, tokenUsage)
		}()
	}

	// ====== 策略優化 ======
	if globalStrategyOptimizer != nil {
		go func() {
			if iteration%10 == 0 {
				if result, err := globalStrategyOptimizer.Optimize(); err == nil && result != nil {
					log.Printf("[StrategyOptimizer] Optimization completed with score: %.2f", result.ImprovementScore)
				}
			}
		}()
	}

	// ====== 記憶重構 ======
	if globalMemoryRefactorManager != nil {
		go func() {
			if iteration%20 == 0 {
				if result, err := globalMemoryRefactorManager.Refactor(); err == nil && result != nil {
					log.Printf("[MemoryRefactorManager] Refactoring completed with score: %.2f", result.ImprovementScore)
				}
			}
		}()
	}
}
