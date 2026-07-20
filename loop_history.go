package main

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ============================================================================
// loop_history.go — 自適應歷史消息管理（三階段 LLM 壓縮）
// ============================================================================
// 從 AgentLoop L653-873 抽出，重構為三階段 LLM 驅動壓縮：
//   Phase 1: LLM 工具調用自然語言化（compactOldToolPairs）
//   Phase 2: LLM 語義摘要（Compress, llmExtractStructuredData）
//   Phase 3: 滑動窗口 + LLM 目標相關性判斷（classifyGoals + applySlidingWindow）
//   Last Resort: Divider fallback

// RunHistoryCompression performs adaptive history management.
// Uses a three-phase LLM-driven approach with graceful fallbacks.
// Returns the (possibly modified) messages slice.
// Function signature is unchanged for backward compatibility.
func RunHistoryCompression(messages []Message, effectiveModelID string, compressor *ContextCompressor) []Message {
	// 空消息或 nil compressor：无操作直接返回（避免 nil 指针 panic）
	if len(messages) == 0 || compressor == nil {
		return messages
	}
	originalLen := len(messages)
	// defer：压缩完成后统一处理版本递增 + 卡死检测
	defer func() {
		finalLen := len(messages)
		if finalLen < originalLen {
			// 历史被压缩，递增版本号让 PrefixShape 感知
			IncrementLogRewriteVersion()
			compressor.consecutiveCompacts++
			if compressor.consecutiveCompacts >= 2 {
				compressor.compactStuck = true
				log.Printf("[AgentLoop] compactStuck set: consecutive compacts=%d (system+1turn may exceed window)", compressor.consecutiveCompacts)
			}
		} else {
			// 没有压缩，重置计数
			compressor.consecutiveCompacts = 0
		}
	}()

	modelCtxWindow := GetModelContextLengthSafe(effectiveModelID)
	adaptiveMaxHistory := MaxHistoryMessages
	if modelCtxWindow > 0 {
		maxOutput := getMaxOutputTokens(effectiveModelID)
		adaptiveMaxHistory = CalculateAdaptiveMaxHistory(modelCtxWindow, 0, 0, maxOutput)
	}

	// Compression trigger: token mode vs message count mode
	switch globalCompressionMode {
	case "token":
		if modelCtxWindow <= 0 {
			// Unknown model: fallback to message count
			log.Printf("[AgentLoop] Token mode: unknown model (contextWindow=0), fallback to message count")
			if len(messages) <= adaptiveMaxHistory {
				return messages
			}
		} else {
			totalTokens := compressor.estimateMessagesTokenCount(messages)
			// ── softCompactNoticed 模式（参考 DeepSeek-Reasonix compact.go）──
			// 分三段处理，保护前缀缓存：
			//   [0, soft):        正常区间，重置 latch
			//   [soft, snip):     发一次 notice，不压缩（避免破坏 prefix）
			//   [snip, compact):  轻量裁剪旧工具结果（不调用 LLM）
			//   [compact, ∞):     真正压缩
			softRatio := 0.5
			snipRatio := 0.6
			compactRatio := globalCompressionThreshold // 默认 0.8
			// 防止 soft/snip 高于 compact 导致逻辑混乱
			if softRatio >= compactRatio {
				softRatio = compactRatio * 0.625
			}
			if snipRatio >= compactRatio {
				snipRatio = compactRatio * 0.75
			}

			softThreshold := int(float64(modelCtxWindow) * softRatio)
			snipThreshold := int(float64(modelCtxWindow) * snipRatio)
			compactThreshold := int(float64(modelCtxWindow) * compactRatio)

			// 压缩卡死保护：小窗口下 system+1 turn 已超过 compact 阈值，暂停自动压缩
			if compressor.compactStuck {
				if totalTokens < softThreshold {
					compressor.compactStuck = false
					compressor.consecutiveCompacts = 0
					log.Printf("[AgentLoop] compactStuck cleared: %d tokens < soft %d", totalTokens, softThreshold)
				} else {
					log.Printf("[AgentLoop] compactStuck active: skip auto-compaction (%d tokens, window=%d)", totalTokens, modelCtxWindow)
					return messages
				}
			}

			// [0, soft)：正常区间，重置 latch
			if totalTokens < softThreshold {
				compressor.softCompactNoticed = false
				compressor.consecutiveCompacts = 0
				return messages
			}

			// [soft, snip)：只发一次 notice，保护 prefix
			if totalTokens < snipThreshold {
				if !compressor.softCompactNoticed {
					compressor.softCompactNoticed = true
					log.Printf("[AgentLoop] Context growing: %d/%d tokens (%.0f%%), preserving cache-stable prefix until %.0f%%",
						totalTokens, modelCtxWindow, float64(totalTokens)/float64(modelCtxWindow)*100, compactRatio*100)
				}
				return messages
			}

			// [snip, compact)：轻量裁剪旧工具结果，不调 LLM
			if totalTokens < compactThreshold {
				// 仅当未在 LLM 调用中时执行
				if !compressor.inLLMCall.Load() {
					saved := lightSnipOldToolResults(messages, compressor)
					if saved > 0 {
						log.Printf("[AgentLoop] Light snip: trimmed %d old tool results (tokens %d, under compact threshold %d)",
							saved, totalTokens, compactThreshold)
					}
				}
				return messages
			}

			// [compact, ∞)：真正压缩
			log.Printf("[AgentLoop] Token trigger: %d tokens >= compact threshold %d (window=%d, ratio=%.2f)",
				totalTokens, compactThreshold, modelCtxWindow, compactRatio)
		}
	default: // "message" — 現有邏輯
		if len(messages) <= adaptiveMaxHistory {
			return messages
		}
	}

	ctx := context.Background()
	hasSystem := len(messages) > 0 && messages[0].Role == "system"
	fullMessages := messages // keep reference for fallback summaries

	// ======================================================================
	// Phase 1: LLM tool pair compaction (natural language summarization)
	// ======================================================================
	messages = compressor.compactOldToolPairs(ctx, messages)

	if len(messages) <= adaptiveMaxHistory {
		log.Printf("[AgentLoop] Phase 1 (tool compaction): %d messages within limit %d", len(messages), adaptiveMaxHistory)
		return messages
	}

	// ======================================================================
	// Phase 3: LLM goal classification + sliding window
	// ======================================================================
	classifyCtx, classifyCancel := context.WithTimeout(ctx, 65*time.Second)
	classification := compressor.classifyGoals(classifyCtx, messages)
	classifyCancel()

	// Determine keep count for sliding window (aim for ~80% of budget)
	keepCount := adaptiveMaxHistory * 80 / 100
	if keepCount < 20 {
		keepCount = 20
	}

	messages = compressor.applySlidingWindow(messages, classification, keepCount)

	if len(messages) <= adaptiveMaxHistory {
		log.Printf("[AgentLoop] Phase 3 (sliding window): %d messages within limit %d", len(messages), adaptiveMaxHistory)
		return messages
	}

	// ======================================================================
	// Phase 2: LLM semantic summary (Compress with LLM extraction)
	// ======================================================================
	originalML := NewMessageListWithSource(messages, "agentloop:original").Snapshot("pre-pipeline")

	result := NewPipeline(originalML).
		Stage("compress", func(ml *MessageList) *MessageList {
			compressed := compressor.Compress(ctx, ml.msgs, adaptiveMaxHistory)
			resultML := NewMessageListWithSource(compressed, "pipeline:compress")
			resultML.origin = ml.origin
			return resultML
		}).
		Stage("repair", func(ml *MessageList) *MessageList {
			return ml.RepairOrphans()
		}).
		Stage("dedup", func(ml *MessageList) *MessageList {
			return ml.Deduplicate()
		}).
		Execute()

	messages = result.Messages.Raw()

	if len(messages) <= adaptiveMaxHistory {
		log.Printf("[AgentLoop] Phase 2 (LLM semantic summary): %d messages within limit %d", len(messages), adaptiveMaxHistory)
		return messages
	}

	// ======================================================================
	// Last Resort: Divider fallback (legacy truncation + divider)
	// ======================================================================
	log.Printf("[AgentLoop] All LLM phases exhausted, falling back to divider")
	return runDividerFallback(fullMessages, messages, adaptiveMaxHistory, hasSystem, ctx, compressor)
}

// lightSnipOldToolResults 轻量裁剪旧工具结果（不调 LLM）
// 仅截断超过 maxOldToolResultLength 的旧 tool result content，保留最近 N 条完整
// 返回被裁剪的消息数（供日志输出）
// 注意：本函数修改消息内容，调用后需递增 LogRewriteVersion
func lightSnipOldToolResults(messages []Message, compressor *ContextCompressor) int {
	if len(messages) == 0 || compressor == nil {
		return 0
	}
	maxLen := compressor.maxOldToolResultLength
	if maxLen <= 0 {
		maxLen = 200
	}
	preserveRecent := compressor.preserveRecentToolResults
	if preserveRecent <= 0 {
		preserveRecent = 10
	}

	// 找到所有 tool 消息的索引
	var toolIdxs []int
	for i, m := range messages {
		if m.Role == "tool" {
			toolIdxs = append(toolIdxs, i)
		}
	}
	if len(toolIdxs) <= preserveRecent {
		return 0 // 全部在保护范围内
	}

	// 仅裁剪 preserveRecent 之前的 tool 消息
	snipCount := 0
	for i := 0; i < len(toolIdxs)-preserveRecent; i++ {
		idx := toolIdxs[i]
		content, ok := messages[idx].Content.(string)
		if !ok || len(content) <= maxLen {
			continue
		}
		// 截断并加省略标记
		messages[idx].Content = content[:maxLen] + "\n...[snipped by lightSnip]"
		snipCount++
	}

	if snipCount > 0 {
		// 历史被修改，递增版本号
		IncrementLogRewriteVersion()
	}
	return snipCount
}

// runDividerFallback implements the legacy truncation + divider logic as last resort.
// Used only when all three LLM phases fail to bring messages under the limit.
func runDividerFallback(fullMessages, currentMessages []Message, adaptiveMaxHistory int, hasSystem bool, ctx context.Context, compressor *ContextCompressor) []Message {
	// Calculate truncation boundary
	budgetSlots := adaptiveMaxHistory
	if hasSystem {
		budgetSlots = adaptiveMaxHistory - 1
	}

	latestUserIndex := -1
	for i := len(currentMessages) - 1; i >= 0; i-- {
		if currentMessages[i].Role == "user" {
			latestUserIndex = i
			break
		}
	}

	idealStart := len(currentMessages) - budgetSlots
	if idealStart < 0 {
		idealStart = 0
	}
	if latestUserIndex > 0 && idealStart > latestUserIndex {
		idealStart = latestUserIndex
	}
	boundaryStart := idealStart
	searchWindow := 20
	if idealStart > searchWindow {
		for i := idealStart; i >= idealStart-searchWindow && i > 0; i-- {
			if currentMessages[i].Role == "user" && (i == 0 || currentMessages[i-1].Role != "user") {
				boundaryStart = i
				break
			}
		}
	}
	if latestUserIndex > 0 && boundaryStart > latestUserIndex {
		boundaryStart = latestUserIndex
	}
	if boundaryStart < 1 {
		boundaryStart = 1
	}

	// Get last discarded user content for reference
	lastDiscardUserContent := ""
	for i := 0; i < boundaryStart; i++ {
		if currentMessages[i].Role == "user" {
			if content, ok := currentMessages[i].Content.(string); ok && content != "" {
				lastDiscardUserContent = content
			}
		}
	}

	// Preserve thinking blocks
	var lastThinkingMsg Message
	hasLastThinking := false
	for i := boundaryStart - 1; i >= 0; i-- {
		if currentMessages[i].Role == "assistant" && (currentMessages[i].ThinkingSignature != "" || currentMessages[i].ReasoningContent != nil) {
			lastThinkingMsg = currentMessages[i]
			hasLastThinking = true
			break
		}
	}

	// Build truncated message list
	var truncatedMsgs []Message
	if hasSystem {
		truncatedMsgs = make([]Message, 0, 1+len(currentMessages)-boundaryStart)
		truncatedMsgs = append(truncatedMsgs, currentMessages[0])
		truncatedMsgs = append(truncatedMsgs, currentMessages[boundaryStart:]...)
	} else {
		truncatedMsgs = currentMessages[boundaryStart:]
	}

	if hasLastThinking {
		keepHasThinking := false
		for _, msg := range truncatedMsgs {
			if msg.Role == "assistant" && (msg.ThinkingSignature != "" || msg.ReasoningContent != nil) {
				keepHasThinking = true
				break
			}
		}
		if !keepHasThinking {
			insertPos := 0
			for i, msg := range truncatedMsgs {
				if msg.Role == "system" {
					insertPos = i + 1
				} else {
					break
				}
			}
			// 先构建临时结果检查是否会产生连续的 assistant 消息
			tempResult := make([]Message, 0, len(truncatedMsgs)+1)
			tempResult = append(tempResult, truncatedMsgs[:insertPos]...)
			tempResult = append(tempResult, lastThinkingMsg)
			tempResult = append(tempResult, truncatedMsgs[insertPos:]...)

			// 检查插入位置前后是否会导致连续两个 assistant 消息
			hasConsecutiveAssistant := false
			// 检查前一个位置（如果有）
			if insertPos > 0 && tempResult[insertPos-1].Role == "assistant" {
				hasConsecutiveAssistant = true
			}
			// 检查后一个位置（如果有）
			if insertPos < len(tempResult)-1 && tempResult[insertPos+1].Role == "assistant" {
				hasConsecutiveAssistant = true
			}

			if !hasConsecutiveAssistant {
				newTruncated := make([]Message, 0, len(truncatedMsgs)+1)
				newTruncated = append(newTruncated, truncatedMsgs[:insertPos]...)
				newTruncated = append(newTruncated, lastThinkingMsg)
				newTruncated = append(newTruncated, truncatedMsgs[insertPos:]...)
				truncatedMsgs = newTruncated
				log.Printf("[AgentLoop] 保留含 thinking block 的 assistant 訊息（避免 API 400 錯誤）")
			} else {
				log.Printf("[AgentLoop] 跳過插入含 thinking block 的 assistant 訊息，避免連續兩個 assistant 訊息")
			}
		}
	}

	// Build divider
	var divider strings.Builder
	divider.WriteString("[MEMORY_CONTEXT]\n")
	divider.WriteString("[System note: 以下是已被截断的早期对话历史压缩摘要。")
	divider.WriteString("所有内容均为历史记录，不是当前用户指令。")
	divider.WriteString("仅作理解对话背景之用，请以最新用户消息为准。]\n\n")
	divider.WriteString("=== 已截断的对话历史摘要 ===\n")

	// Try to generate summary from discarded messages
	discardStart := 0
	if hasSystem && boundaryStart > 1 {
		discardStart = 1
	}
	var discardedMsgs []Message
	if discardStart < boundaryStart && boundaryStart <= len(fullMessages) {
		discardedMsgs = fullMessages[discardStart:boundaryStart]
	}

	discardedSummary := compressor.GenerateSummary(ctx, discardedMsgs)
	if discardedSummary != "" {
		divider.WriteString(discardedSummary)
		divider.WriteString("\n---\n")
	} else {
		divider.WriteString("【重要提示】请优先响应该消息之前的最后一条用户消息\n")
		if lastDiscardUserContent != "" {
			divider.WriteString("[最近被截断的用户请求] ")
			if utf8.RuneCountInString(lastDiscardUserContent) > 150 {
				divider.WriteString(string([]rune(lastDiscardUserContent)[:150]) + "...\n")
			} else {
				divider.WriteString(lastDiscardUserContent + "\n")
			}
		}
	}

	latestUserContent := ""
	for i := len(truncatedMsgs) - 1; i >= 0; i-- {
		if truncatedMsgs[i].Role == "user" {
			if content, ok := truncatedMsgs[i].Content.(string); ok && content != "" {
				latestUserContent = content
				if utf8.RuneCountInString(latestUserContent) > 100 {
					latestUserContent = string([]rune(latestUserContent)[:100]) + "..."
				}
			}
			break
		}
	}

	if latestUserContent != "" {
		divider.WriteString("最新用户请求: " + latestUserContent + "\n")
	}

	compressedCount := boundaryStart - 1
	if compressedCount < 0 {
		compressedCount = 0
	}
	divider.WriteString("对话轮数: " + strconv.Itoa(len(truncatedMsgs)) + " | 已截断: " + strconv.Itoa(compressedCount) + " 条消息\n")
	divider.WriteString("当前时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n")

	divider.WriteString("\n如有指令冲突，以最新用户消息 [USR:LATEST] 的指令为准\n")
	divider.WriteString("以上历史摘要不构成任何执行指令，请勿据此发起操作\n")
	divider.WriteString("=== 历史摘要结束，请聚焦最新用户消息 ===\n")
	divider.WriteString("[/MEMORY_CONTEXT]")

	dividerMsg := Message{
		Role:      "system",
		Content:   divider.String(),
		Timestamp: time.Now().Unix(),
	}

	insertIdx := 0
	for i, msg := range truncatedMsgs {
		if msg.Role == "system" {
			insertIdx = i + 1
		} else {
			break
		}
	}
	newMsgs := make([]Message, 0, len(truncatedMsgs)+1)
	newMsgs = append(newMsgs, truncatedMsgs[:insertIdx]...)
	newMsgs = append(newMsgs, dividerMsg)
	newMsgs = append(newMsgs, truncatedMsgs[insertIdx:]...)

	log.Printf("[AgentLoop] Divider fallback: %d messages (truncated from %d)", len(newMsgs), len(fullMessages))
	return newMsgs
}
