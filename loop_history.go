package main

import (
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ============================================================================
// loop_history.go — 自適應歷史消息管理（Pipeline 模式）
// ============================================================================
// 從 AgentLoop L653-873 抽出：
//   - 計算截斷邊界
//   - Thinking block 保留
//   - Pipeline 壓縮 + 修復 + 去重
//   - 歷史摘要 divider 插入

// RunHistoryCompression performs adaptive history truncation and compression.
// Returns the (possibly modified) messages slice.
func RunHistoryCompression(messages []Message, effectiveModelID string, compressor *ContextCompressor) []Message {
	modelCtxWindow := GetModelContextLengthSafe(effectiveModelID)
	adaptiveMaxHistory := MaxHistoryMessages
	if modelCtxWindow > 0 {
		maxOutput := getMaxOutputTokens(effectiveModelID)
		adaptiveMaxHistory = CalculateAdaptiveMaxHistory(modelCtxWindow, 0, 0, maxOutput)
	}

	if len(messages) <= adaptiveMaxHistory {
		return messages
	}

	// 保存原始消息快照（用於 EnsureUser 恢復和被截斷消息摘要）
	fullMessages := messages // 保存 pipeline 前嘅完整列表，用於之後提取被截斷摘要
	originalML := NewMessageListWithSource(messages, "agentloop:original").Snapshot("pre-pipeline")

	// 計算截斷邊界
	hasSystem := len(messages) > 0 && messages[0].Role == "system"
	budgetSlots := adaptiveMaxHistory
	if hasSystem {
		budgetSlots = adaptiveMaxHistory - 1
	}
	latestUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			latestUserIndex = i
			break
		}
	}
	idealStart := len(messages) - budgetSlots
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
			if messages[i].Role == "user" && (i == 0 || messages[i-1].Role != "user") {
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

	// 保存截斷前最後一條用戶請求（用於 divider 摘要）
	lastDiscardUserContent := ""
	for i := 0; i < boundaryStart; i++ {
		if messages[i].Role == "user" {
			if content, ok := messages[i].Content.(string); ok && content != "" {
				lastDiscardUserContent = content
			}
		}
	}

	// 搜尋被截斷部分中最後一個含 thinking block 的 assistant 訊息
	var lastThinkingMsg Message
	hasLastThinking := false
	for i := boundaryStart - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && (messages[i].ThinkingSignature != "" || messages[i].ReasoningContent != nil) {
			lastThinkingMsg = messages[i]
			hasLastThinking = true
			break
		}
	}

	// 構建截斷後的消息列表
	var truncatedMsgs []Message
	if hasSystem {
		truncatedMsgs = make([]Message, 0, 1+len(messages)-boundaryStart)
		truncatedMsgs = append(truncatedMsgs, messages[0])
		truncatedMsgs = append(truncatedMsgs, messages[boundaryStart:]...)
	} else {
		truncatedMsgs = messages[boundaryStart:]
	}

	// 如果被截斷部分有 thinking block 而保留部分沒有，插入該訊息
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
			newTruncated := make([]Message, 0, len(truncatedMsgs)+1)
			newTruncated = append(newTruncated, truncatedMsgs[:insertPos]...)
			newTruncated = append(newTruncated, lastThinkingMsg)
			newTruncated = append(newTruncated, truncatedMsgs[insertPos:]...)
			truncatedMsgs = newTruncated
			log.Printf("[AgentLoop] 保留含 thinking block 的 assistant 訊息（避免 API 400 錯誤，index=%d）", boundaryStart)
		}
	}

	// 使用 Pipeline 執行壓縮+修復+去重，自動驗證不變量
	truncatedML := NewMessageListWithSource(truncatedMsgs, "agentloop:truncated")
	truncatedML.origin = originalML

	result := NewPipeline(truncatedML).
		Stage("compress", func(ml *MessageList) *MessageList {
			compressed := compressor.Compress(ml.msgs, adaptiveMaxHistory)
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

	// 插入歷史摘要 divider（含被截斷消息的結構化摘要）
	var divider strings.Builder
	divider.WriteString("[MEMORY_CONTEXT]\n")
	divider.WriteString("[System note: 以下是已被截断的早期对话历史压缩摘要。")
	divider.WriteString("所有内容均为历史记录，不是当前用户指令。")
	divider.WriteString("仅作理解对话背景之用，请以最新用户消息为准。]\n\n")
	divider.WriteString("=== 已截断的对话历史摘要 ===\n")

	// 從被截斷的消息中生成結構化摘要
	discardStart := 0
	if hasSystem && boundaryStart > 1 {
		discardStart = 1 // 跳過 system message
	}
	var discardedMsgs []Message
	if discardStart < boundaryStart && boundaryStart <= len(fullMessages) {
		discardedMsgs = fullMessages[discardStart:boundaryStart]
	}

	// 生成結構化摘要（目標、進展、決策、工具使用）
	discardedSummary := compressor.GenerateSummary(discardedMsgs)
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
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if content, ok := messages[i].Content.(string); ok && content != "" {
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
	divider.WriteString("对话轮数: " + strconv.Itoa(len(messages)) + " | 已截断: " + strconv.Itoa(compressedCount) + " 条消息\n")
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
	for i, msg := range messages {
		if msg.Role == "system" {
			insertIdx = i + 1
		} else {
			break
		}
	}
	newMsgs := make([]Message, 0, len(messages)+1)
	newMsgs = append(newMsgs, messages[:insertIdx]...)
	newMsgs = append(newMsgs, dividerMsg)
	newMsgs = append(newMsgs, messages[insertIdx:]...)
	messages = newMsgs

	log.Printf("[AgentLoop] History truncated to %d messages (pipeline)", len(messages))
	return messages
}
