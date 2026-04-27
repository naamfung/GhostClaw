package main

import (
        "fmt"
        "log"
        "strings"
        "time"
        "unicode/utf8"
)

// structuredSummary holds the rich structured summary fields
type structuredSummary struct {
        Version     int      // summary version counter
        Goals       []string // main user objectives
        Constraints []string // limitations or requirements from the user
        Progress    []string // what has been accomplished so far
        Decisions   []string // important decisions made during conversation
        Pending     []string // outstanding tasks or questions needing attention
        ToolSummary []string // summary of tools used with outcomes
}

// ContextCompressor 上下文压缩器
// Implements a 4-stage compression algorithm:
//   Stage 1: Trim old tool results (pure text processing, no LLM)
//   Stage 2: Protect head messages
//   Stage 3: Token budget tail (don't split tool_call/result pairs)
//   Stage 4: LLM structured summary (heuristic extraction)
type ContextCompressor struct {
        thresholdPercent         float64       // 触发压缩的阈值（默认 50%）
        protectFirstN            int           // 保护前 N 条消息
        tailTokenBudget          int           // 尾部 token 预算
        previousSummary          string        // 上次摘要，用于迭代更新
        compressionCount         int           // 压缩次数
        focusTopic               string        // 焦点主题，优先保留相关消息
        summaryVersion           int           // 摘要版本计数器
        lastSummary              *structuredSummary // 上一次的结构化摘要（用于增量更新）
        // Stage 1: Old tool result trimming
        preserveRecentToolResults int          // 保护最近 N 条工具结果（默认 10）
        maxOldToolResultLength    int          // 旧工具结果截断长度（默认 200 字符）
        // Compression failure cooldown
        lastCompressionFailure      time.Time   // 上次压缩失败的时间
        compressionCooldownDuration time.Duration // 压缩失败冷却期（默认 600s）
}

// NewContextCompressor 创建新的上下文压缩器
func NewContextCompressor() *ContextCompressor {
        return &ContextCompressor{
                thresholdPercent:         0.5,             // 50% 阈值
                protectFirstN:            3,               // 保护前 3 条消息
                tailTokenBudget:          20000,           // 尾部 20K token 预算
                compressionCount:         0,
                preserveRecentToolResults: 10,             // 保护最近 10 条工具结果
                maxOldToolResultLength:   200,             // 旧工具结果截断为 200 字符
                compressionCooldownDuration: 10 * time.Minute, // 10 分钟冷却
        }
}

// SetFocusTopic 设置焦点主题，压缩时优先保留与此主题相关的消息
func (cc *ContextCompressor) SetFocusTopic(topic string) {
        cc.focusTopic = strings.ToLower(strings.TrimSpace(topic))
        if cc.focusTopic != "" {
                log.Printf("[ContextCompressor] Focus topic set: %q", cc.focusTopic)
        }
}

// ClearFocusTopic 清除焦点主题
func (cc *ContextCompressor) ClearFocusTopic() {
        cc.focusTopic = ""
}

// topicRelevanceScore 计算消息与焦点主题的相关性得分
// 使用简单的关键词匹配：返回匹配的关键词数量
func (cc *ContextCompressor) topicRelevanceScore(msg Message) int {
        if cc.focusTopic == "" {
                return 0
        }

        content := extractStringContent(msg)
        if content == "" {
                return 0
        }

        lowerContent := strings.ToLower(content)
        score := 0

        // Split focus topic into keywords for matching
        topicKeywords := strings.Fields(cc.focusTopic)
        for _, keyword := range topicKeywords {
                if strings.Contains(lowerContent, keyword) {
                        score++
                }
        }

        return score
}

// extractStringContent safely extracts a string from Message.Content
func extractStringContent(msg Message) string {
        if msg.Content == nil {
                return ""
        }
        switch v := msg.Content.(type) {
        case string:
                return v
        default:
                return fmt.Sprintf("%v", v)
        }
}

// Compress 压缩消息列表 (4-stage algorithm)
//   Stage 1: Trim old tool results (pure text processing, no LLM)
//   Stage 2: Protect head messages (system + first N)
//   Stage 3: Build token-budget tail (don't split tool_call/result pairs)
//   Stage 4: Generate structured summary of middle section (heuristic extraction)
func (cc *ContextCompressor) Compress(messages []Message) []Message {
        // Pre-check: cooldown
        if cc.isInCooldown() {
                log.Printf("[ContextCompressor] Skipping compression: in cooldown (expires in %v)",
                        time.Until(cc.lastCompressionFailure.Add(cc.compressionCooldownDuration)))
                return messages
        }

        // 防止返回共享的底層切片引用
        messages = append([]Message(nil), messages...)

        if len(messages) <= MaxHistoryMessages {
                return messages
        }

        // Stage 1: Trim old tool results (pure text replacement, no LLM)
        messages = cc.trimOldToolResults(messages)

        // After Stage 1, check again — maybe we're under the limit now
        if len(messages) <= MaxHistoryMessages {
                log.Printf("[ContextCompressor] Stage 1 sufficient: trimmed from original count to %d messages", len(messages))
                cc.recordCompressionSuccess()
                return messages
        }

        // Stage 2: Protect head (system + first N messages)
        head := cc.buildHead(messages)

        // Stage 3: Build token-budget tail (don't split tool_call/result pairs)
        tail := cc.buildTail(messages, head)

        // Determine the middle section (what gets summarized)
        middle := make([]Message, 0)
        headEnd := len(head)
        tailStart := len(messages) - len(tail)
        if tailStart > headEnd {
                middle = messages[headEnd:tailStart]
        }

        // 如果中間區域為空，無需摘要，直接返回 head+tail
        if len(middle) == 0 {
                log.Printf("[ContextCompressor] Middle section empty, returning head+tail directly")
                return append(head, tail...)
        }

        // Stage 4: Generate structured summary (heuristic extraction, no LLM)
        summary := cc.generateStructuredSummary(middle)

        // Treat empty summary as compression failure
        if strings.TrimSpace(summary) == "" {
                log.Printf("[ContextCompressor] Compression failure: structured summary was empty")
                cc.recordCompressionFailure()
                return messages
        }

        // Build compressed result using MessageList for invariant protection
        compressedML := NewMessageListWithSource(head, "compress:head")

        // Add summary message
        summaryMsg := Message{
                Role:      "system",
                Content:   summary,
                Timestamp: time.Now().Unix(),
                ToolCalls: nil,
        }
        compressedML = compressedML.Append(summaryMsg)
        compressedML = compressedML.Append(tail...)

        // 檢查 middle 區域是否有 thinking block，而 tail 沒有
        // thinking blocks 必須回傳 API（DeepSeek/Anthropic），不可丟失
        if !cc.messagesContainThinkingBlock(compressedML.msgs) {
                if middleThinking := cc.findLastThinkingInMiddle(middle); middleThinking != nil {
                        // 將含 thinking block 的訊息插入到 summary 之後、tail 之前
                        insertPos := len(head) + 1 // after head + summary
                        newMsgs := make([]Message, 0, len(compressedML.msgs)+1)
                        newMsgs = append(newMsgs, compressedML.msgs[:insertPos]...)
                        newMsgs = append(newMsgs, *middleThinking)
                        newMsgs = append(newMsgs, compressedML.msgs[insertPos:]...)
                        compressedML.SetMsgs(newMsgs)
                        log.Printf("[ContextCompressor] 保留 middle 中最後一個含 thinking block 的 assistant 訊息（避免 API 400）")
                }
        }

        // 安全驗證：壓縮結果必須包含至少一條用戶消息
        // 利用 MessageList 的 EnsureUser 從原始消息恢復
        if !compressedML.HasUser() {
                // 從原始消息構建 MessageList 並設置為 origin
                originML := NewMessageListWithSource(messages, "compress:original")
                compressedML.origin = originML
                compressedML = compressedML.EnsureUser()
        }

        cc.compressionCount++
        cc.summaryVersion++
        cc.recordCompressionSuccess()

        log.Printf("[ContextCompressor] Compressed from %d to %d messages (count: %d, version: %d, stages: 1→2→3→4)",
                len(messages), compressedML.Len(), cc.compressionCount, cc.summaryVersion)

        return compressedML.msgs
}

// CompressWithContextWindow compresses messages only if estimated tokens exceed contextWindow * thresholdPercent.
// This provides a more accurate trigger than message count alone.
func (cc *ContextCompressor) CompressWithContextWindow(messages []Message, contextWindow int) []Message {
        if contextWindow <= 0 {
                return cc.Compress(messages)
        }

        totalTokens := cc.estimateMessagesTokenCount(messages)
        threshold := float64(contextWindow) * cc.thresholdPercent

        if float64(totalTokens) <= threshold {
                return messages
        }

        log.Printf("[ContextCompressor] CompressWithContextWindow: %d tokens > %.0f threshold (window=%d)",
                totalTokens, threshold, contextWindow)

        return cc.Compress(messages)
}

// buildHead extracts the protected head messages.
// 只保護 system 消息（含 system prompt、MEMORY_CONTEXT 圍欄、token 統計等），
// 不再無條件保護最初的 user/assistant 消息。
// 之前的實現保護 system + 前 N 條消息，導致最初對話（如名稱設定）永遠留在 head 中，
// 壓縮後模型看到最初對話碎片就會「回到原點」回覆舊內容。
func (cc *ContextCompressor) buildHead(messages []Message) []Message {
        head := make([]Message, 0)
        for _, msg := range messages {
                if msg.Role == "system" {
                        head = append(head, msg)
                } else if len(head) > 0 {
                        // system 之後遇到第一條非 system 消息就停止
                        break
                }
        }
        return head
}

// buildTail constructs the tail messages with token budget awareness.
// Key invariant: never splits an assistant tool_call from its corresponding tool result messages.
// Uses MessageList internally to guarantee the tail always contains a user message.
func (cc *ContextCompressor) buildTail(messages []Message, head []Message) []Message {
        headEnd := len(head)
        msgCount := len(messages)

        // Find the latest user message (tail must include at least this)
        latestUserIndex := -1
        for i := msgCount - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        latestUserIndex = i
                        break
                }
        }

        // Determine initial tail start position
        var tailStart int
        if latestUserIndex >= 0 {
                tailStart = latestUserIndex
        } else {
                tailStart = msgCount - 5
        }

        if tailStart <= headEnd {
                tailStart = headEnd + 1
                if tailStart >= msgCount {
                        return []Message{messages[msgCount-1]}
                }
        }

        // Collect tool_call IDs that appear in messages[tailStart:] so we can
        // protect their corresponding tool result messages in the middle.
        // We build the tail from tailStart backwards, respecting the token budget.
        // Track which tool_call IDs are "claimed" by included assistant messages,
        // so orphan tool-role messages (without a parent assistant) can be skipped.
        claimedToolCallIDs := make(map[string]bool)
        tail := make([]Message, 0)
        budgetRemaining := cc.tailTokenBudget

        for i := msgCount - 1; i >= tailStart; i-- {
                msg := messages[i]
                content := extractStringContent(msg)
                msgTokens := cc.estimateTokenCount(content)
                // Add overhead for role, tool_calls, etc.
                msgTokens += 20

                // If this assistant message has tool_calls, we MUST include all
                // corresponding tool result messages too (they follow immediately).
                if msg.Role == "assistant" && msg.ToolCalls != nil {
                        toolCallIDs := cc.extractToolCallIDs(msg)
                        pairTokens := msgTokens
                        // Scan forward to collect matching tool results
                        for j := i + 1; j < msgCount; j++ {
                                if messages[j].Role == "tool" && cc.isToolCallIDInList(messages[j].ToolCallID, toolCallIDs) {
                                        pairTokens += cc.estimateTokenCount(extractStringContent(messages[j])) + 20
                                } else {
                                        break
                                }
                        }
                        if pairTokens <= budgetRemaining {
                                budgetRemaining -= pairTokens
                                // Build a contiguous block: [assistant, tool_result, tool_result, ...]
                                pairBlock := []Message{msg}
                                for j := i + 1; j < msgCount; j++ {
                                        if messages[j].Role == "tool" && cc.isToolCallIDInList(messages[j].ToolCallID, toolCallIDs) {
                                                pairBlock = append(pairBlock, messages[j])
                                        } else {
                                                break
                                        }
                                }
                                for _, tcID := range toolCallIDs {
                                        claimedToolCallIDs[tcID] = true
                                }
                                tail = append(pairBlock, tail...)
                                continue
                        }
                        continue
                }

                // Skip orphan tool-role messages whose ToolCallID was not claimed
                if msg.Role == "tool" && msg.ToolCallID != "" && !claimedToolCallIDs[msg.ToolCallID] {
                        continue
                }

                if msgTokens <= budgetRemaining {
                        budgetRemaining -= msgTokens
                        tail = append([]Message{msg}, tail...)
                }
        }

        // 構建 MessageList 利用其內建保護
        tailML := NewMessageList(tail)

        // 安全保護 1：如果 tail 為空但 tailStart 之後有消息，強制包含最後幾條
        if tailML.IsEmpty() && msgCount > tailStart {
                forceCount := 2
                if msgCount-tailStart < forceCount {
                        forceCount = msgCount - tailStart
                }
                tailML = NewMessageList(messages[msgCount-forceCount:])
        }

        // 安全保護 2：確保 tail 中包含至少一條用戶消息
        // 場景：預算被尾部大量 assistant+tool_call pair 消耗，backward walk
        // 從 msgCount-1 到 tailStart 期間耗盡預算，導致 tailStart 處的用戶消息未被加入
        if !tailML.HasUser() && latestUserIndex >= 0 {
                userMsg := messages[latestUserIndex]
                tailML = tailML.Prepend(userMsg)
                log.Printf("[ContextCompressor] buildTail: 預算耗盡後強制包含用戶消息 (index %d)", latestUserIndex)
        }

        // When there's a focus topic, also pull in related messages from the middle
        if cc.focusTopic != "" {
                focusMessages := cc.collectFocusMessages(messages, headEnd, tailStart)
                if len(focusMessages) > 0 {
                        merged := make([]Message, 0, len(focusMessages)+len(tailML.msgs))
                        merged = append(merged, focusMessages...)
                        merged = append(merged, tailML.msgs...)
                        mergedML := NewMessageList(merged)
                        mergedML.ValidateOrLog("buildTail:focus-merge")
                        return mergedML.msgs
                }
        }

        return tailML.msgs
}

// extractToolCallIDs extracts tool call IDs from an assistant message's ToolCalls field.
func (cc *ContextCompressor) extractToolCallIDs(msg Message) []string {
        var ids []string
        if msg.ToolCalls == nil {
                return ids
        }
        // Handle []interface{} type
        if tcSlice, ok := msg.ToolCalls.([]interface{}); ok {
                for _, tc := range tcSlice {
                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                if id, ok := tcMap["id"].(string); ok {
                                        ids = append(ids, id)
                                }
                        }
                }
                return ids
        }
        // Handle []map[string]interface{} type
        if tcMapSlice, ok := msg.ToolCalls.([]map[string]interface{}); ok {
                for _, tcMap := range tcMapSlice {
                        if id, ok := tcMap["id"].(string); ok {
                                ids = append(ids, id)
                        }
                }
        }
        return ids
}

// isToolCallIDInList checks if a tool call ID matches any ID in the list.
func (cc *ContextCompressor) isToolCallIDInList(toolCallID string, idList []string) bool {
        if toolCallID == "" || len(idList) == 0 {
                return false
        }
        for _, id := range idList {
                if id == toolCallID {
                        return true
                }
        }
        return false
}

// collectFocusMessages 从中间区域收集与焦点主题相关的消息（去重）
func (cc *ContextCompressor) collectFocusMessages(messages []Message, headEnd, tailStart int) []Message {
        if cc.focusTopic == "" {
                return nil
        }

        var result []Message
        seen := make(map[int]bool) // avoid duplicates by index

        // Scan from tailStart backwards to find relevant messages near the tail
        for i := tailStart - 1; i >= headEnd && len(result) < 4; i-- {
                if cc.topicRelevanceScore(messages[i]) > 0 {
                        if !seen[i] {
                                result = append([]Message{messages[i]}, result...) // prepend to maintain order
                                seen[i] = true
                        }
                }
        }

        return result
}

// ============================================================================
// Stage 1: Trim Old Tool Results (pure text processing, no LLM)
// ============================================================================

// trimOldToolResults truncates old tool-role messages that are beyond the
// preserveRecentToolResults threshold. This is purely text replacement — no LLM call.
// Messages within a protected tool_call/result pair in the tail are NOT truncated.
func (cc *ContextCompressor) trimOldToolResults(messages []Message) []Message {
        if cc.preserveRecentToolResults <= 0 || cc.maxOldToolResultLength <= 0 {
                return messages
        }

        // Collect indices of all tool-role messages
        toolIndices := make([]int, 0)
        for i, msg := range messages {
                if msg.Role == "tool" {
                        toolIndices = append(toolIndices, i)
                }
        }

        toolCount := len(toolIndices)
        if toolCount <= cc.preserveRecentToolResults {
                return messages // nothing to trim
        }

        // Mark which tool result indices are protected (in the most recent N)
        protectedSet := make(map[int]bool)
        for i := toolCount - cc.preserveRecentToolResults; i < toolCount; i++ {
                protectedSet[toolIndices[i]] = true
        }

        // Also protect tool results that are part of a tool_call/result pair
        // where the assistant message with tool_calls is in the tail region.
        // We protect the last ~5 assistant messages with tool_calls and their results.
        tailAssistantCount := 0
        for i := len(messages) - 1; i >= 0 && tailAssistantCount < 5; i-- {
                if messages[i].Role == "assistant" && messages[i].ToolCalls != nil {
                        tcIDs := cc.extractToolCallIDs(messages[i])
                        for j := i + 1; j < len(messages); j++ {
                                if messages[j].Role == "tool" && cc.isToolCallIDInList(messages[j].ToolCallID, tcIDs) {
                                        protectedSet[j] = true
                                } else if messages[j].Role != "tool" {
                                        break
                                }
                        }
                        tailAssistantCount++
                }
        }

        // Truncate old (non-protected) tool results
        truncated := 0
        result := make([]Message, len(messages))
        copy(result, messages)
        for _, idx := range toolIndices[:toolCount-cc.preserveRecentToolResults] {
                if protectedSet[idx] {
                        continue
                }
                content := extractStringContent(result[idx])
                if content == "" {
                        continue
                }
                runeCount := utf8.RuneCountInString(content)
                if runeCount > cc.maxOldToolResultLength {
                        runes := []rune(content)
                        result[idx].Content = string(runes[:cc.maxOldToolResultLength]) + "... [truncated by context compressor]"
                        truncated++
                }
        }

        if truncated > 0 {
                log.Printf("[ContextCompressor] Stage 1: truncated %d old tool results (kept %d recent)", truncated, cc.preserveRecentToolResults)
        }

        return result
}

// ============================================================================
// Token Estimation Helpers
// ============================================================================

// estimateTokenCount estimates the number of tokens in a string.
// Uses a simple heuristic: ~4 characters per token for mixed content,
// ~2 characters per token for CJK-heavy content.
func (cc *ContextCompressor) estimateTokenCount(content string) int {
        if content == "" {
                return 0
        }
        // Count CJK runes vs ASCII runes for better estimation
        cjkRunes := 0
        asciiRunes := 0
        for _, r := range content {
                if r >= 0x4E00 && r <= 0x9FFF || // CJK Unified Ideographs
                        r >= 0x3040 && r <= 0x30FF || // Hiragana + Katakana
                        r >= 0xAC00 && r <= 0xD7AF || // Hangul Syllables
                        r >= 0xFF00 && r <= 0xFFEF { // Fullwidth Forms
                        cjkRunes++
                } else {
                        asciiRunes++
                }
        }
        // CJK: ~1.5 tokens per rune, ASCII: ~4 chars per token
        cjkTokens := (cjkRunes*3 + 1) / 2
        asciiTokens := asciiRunes / 4
        if asciiTokens == 0 && asciiRunes > 0 {
                asciiTokens = 1
        }
        return cjkTokens + asciiTokens
}

// estimateMessagesTokenCount estimates total token count across all messages.
func (cc *ContextCompressor) estimateMessagesTokenCount(messages []Message) int {
        total := 0
        for _, msg := range messages {
                content := extractStringContent(msg)
                total += cc.estimateTokenCount(content)
                // Add overhead per message for role, formatting, tool_calls, etc.
                total += 10
                if msg.ToolCalls != nil {
                        total += 50 // approximate overhead for tool call structures
                }
                if msg.ReasoningContent != nil {
                        rc := fmt.Sprintf("%v", msg.ReasoningContent)
                        total += cc.estimateTokenCount(rc)
                }
        }
        return total
}

// ============================================================================
// Compression Failure Cooldown
// ============================================================================

// isInCooldown returns true if we're within the cooldown period after a compression failure.
func (cc *ContextCompressor) isInCooldown() bool {
        if cc.lastCompressionFailure.IsZero() {
                return false
        }
        return time.Since(cc.lastCompressionFailure) < cc.compressionCooldownDuration
}

// recordCompressionSuccess clears the failure timestamp.
func (cc *ContextCompressor) recordCompressionSuccess() {
        cc.lastCompressionFailure = time.Time{} // zero value = no failure
}

// recordCompressionFailure records the current time as a compression failure.
func (cc *ContextCompressor) recordCompressionFailure() {
        cc.lastCompressionFailure = time.Now()
        log.Printf("[ContextCompressor] Compression failure recorded, cooldown active for %v", cc.compressionCooldownDuration)
}

// ============================================================================
// Rich Structured Summary Generation
// ============================================================================

// generateStructuredSummary 生成结构化摘要（支持增量更新）
func (cc *ContextCompressor) generateStructuredSummary(messages []Message) string {
        if len(messages) == 0 {
                return cc.previousSummary
        }

        // Extract structured information from messages
        extracted := cc.extractStructuredData(messages)

        // Merge with previous summary if available (iterative update)
        merged := cc.mergeWithPrevious(extracted)

        // Store for future incremental updates
        cc.lastSummary = merged

        // Render to text
        rendered := cc.renderStructuredSummary(merged)

        // Store the rendered text as previousSummary for backward compat
        cc.previousSummary = rendered

        return rendered
}

// extractStructuredData 从消息列表中提取结构化数据
func (cc *ContextCompressor) extractStructuredData(messages []Message) *structuredSummary {
        s := &structuredSummary{}

        // Track tool call outcomes: toolName -> list of short outcome descriptions
        toolOutcomes := make(map[string][]string)

        for _, msg := range messages {
                switch msg.Role {
                case "user":
                        content := extractStringContent(msg)
                        if content == "" {
                                continue
                        }
                        // Heuristic classification of user messages
                        lowerContent := strings.ToLower(content)
                        if containsAny(lowerContent, "不要", "不能", "不允许", "限制", "必须", "禁止", "约束", "要求",
                                "don't", "must not", "cannot", "only", "never", "constraint", "require", "limit") {
                                s.Constraints = append(s.Constraints, truncateString(content, 120))
                        } else if containsAny(lowerContent, "决定", "选择", "用这个", "就用", "确认", "好的用",
                                "decided", "go with", "use this", "confirmed") {
                                s.Decisions = append(s.Decisions, truncateString(content, 120))
                        } else if containsAny(lowerContent, "还没", "待办", "接下来", "还需要", "下一步", "pending",
                                "todo", "next", "still need", "remaining") {
                                s.Pending = append(s.Pending, truncateString(content, 120))
                        } else {
                                s.Goals = append(s.Goals, truncateString(content, 120))
                        }

                case "assistant":
                        content := extractStringContent(msg)
                        if content != "" {
                                // Detect progress indicators in assistant responses
                                lowerContent := strings.ToLower(content)
                                if containsAny(lowerContent, "已完成", "完成", "成功", "done", "completed",
                                        "finished", "successfully", "created", "wrote", "built", "implemented") {
                                        s.Progress = append(s.Progress, truncateString(content, 150))
                                }
                        }

                        // Extract tool calls
                        cc.extractToolCallsFromMessage(msg, toolOutcomes)

                case "tool":
                        // Tool results can indicate progress
                        content := extractStringContent(msg)
                        if content == "" {
                                continue
                        }
                        // Infer tool name from ToolCallID context (if available)
                        toolName := inferToolName(msg)
                        if len(content) > 20 {
                                toolOutcomes[toolName] = append(toolOutcomes[toolName], truncateString(content, 100))
                        }
                }
        }

        // Deduplicate and limit goals
        s.Goals = deduplicateStrings(s.Goals)
        if len(s.Goals) > 5 {
                s.Goals = s.Goals[:5]
        }

        s.Constraints = deduplicateStrings(s.Constraints)
        if len(s.Constraints) > 5 {
                s.Constraints = s.Constraints[:5]
        }

        s.Progress = deduplicateStrings(s.Progress)
        if len(s.Progress) > 5 {
                s.Progress = s.Progress[:5]
        }

        s.Decisions = deduplicateStrings(s.Decisions)
        if len(s.Decisions) > 5 {
                s.Decisions = s.Decisions[:5]
        }

        s.Pending = deduplicateStrings(s.Pending)
        if len(s.Pending) > 5 {
                s.Pending = s.Pending[:5]
        }

        // Build tool summary
        for toolName, outcomes := range toolOutcomes {
                count := len(outcomes)
                summary := fmt.Sprintf("%s (called %d times)", toolName, count)
                if len(outcomes) > 0 {
                        summary += fmt.Sprintf(": %s", outcomes[len(outcomes)-1])
                }
                s.ToolSummary = append(s.ToolSummary, truncateString(summary, 150))
        }

        return s
}

// extractToolCallsFromMessage extracts tool call names from a message
func (cc *ContextCompressor) extractToolCallsFromMessage(msg Message, toolOutcomes map[string][]string) {
        if msg.ToolCalls == nil {
                return
        }

        // Handle []interface{} type
        if tcSlice, ok := msg.ToolCalls.([]interface{}); ok && len(tcSlice) > 0 {
                for _, tc := range tcSlice {
                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                        if name, ok := function["name"].(string); ok {
                                                toolOutcomes[name] = toolOutcomes[name] // ensure key exists
                                        }
                                }
                        }
                }
                return
        }

        // Handle []map[string]interface{} type
        if tcMapSlice, ok := msg.ToolCalls.([]map[string]interface{}); ok && len(tcMapSlice) > 0 {
                for _, tcMap := range tcMapSlice {
                        if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                if name, ok := function["name"].(string); ok {
                                        toolOutcomes[name] = toolOutcomes[name] // ensure key exists
                                }
                        }
                }
        }
}

// inferToolName attempts to infer a tool name from tool result content
func inferToolName(msg Message) string {
        content := extractStringContent(msg)
        if msg.ToolCallID != "" {
                // Try to extract from content markers like [COMPLETED | Tool: xxx]
                markers := []string{"[COMPLETED | Tool: ", "[OPERATION FAILED] ", "[Tool: "}
                for _, marker := range markers {
                        idx := strings.Index(content, marker)
                        if idx >= 0 {
                                rest := content[idx+len(marker):]
                                endIdx := strings.Index(rest, "]")
                                if endIdx > 0 {
                                        return rest[:endIdx]
                                }
                        }
                }
        }
        return "unknown_tool"
}

// ============================================================================
// Iterative Summary Update
// ============================================================================

// mergeWithPrevious merges newly extracted data with the previous structured summary
func (cc *ContextCompressor) mergeWithPrevious(extracted *structuredSummary) *structuredSummary {
        // First compression: just use extracted data
        if cc.lastSummary == nil {
                extracted.Version = cc.summaryVersion + 1
                return extracted
        }

        merged := &structuredSummary{
                Version: cc.summaryVersion + 1,
        }

        // Goals: keep previous unique goals, append new ones
        merged.Goals = mergeStringSlices(cc.lastSummary.Goals, extracted.Goals, 5)

        // Constraints: accumulate (constraints are important to keep)
        merged.Constraints = mergeStringSlices(cc.lastSummary.Constraints, extracted.Constraints, 5)

        // Progress: new progress items are appended; old ones preserved
        merged.Progress = mergeStringSlices(cc.lastSummary.Progress, extracted.Progress, 5)

        // Decisions: accumulate
        merged.Decisions = mergeStringSlices(cc.lastSummary.Decisions, extracted.Decisions, 5)

        // Pending: merge, but remove items that appear in progress (they're done)
        pendingSet := make(map[string]bool)
        for _, p := range extracted.Pending {
                pendingSet[p] = true
        }
        for _, p := range cc.lastSummary.Pending {
                // Check if this pending item has been addressed by progress
                if !isAddressedIn(p, extracted.Progress) {
                        pendingSet[p] = true
                }
        }
        for p := range pendingSet {
                merged.Pending = append(merged.Pending, p)
        }
        if len(merged.Pending) > 5 {
                merged.Pending = merged.Pending[:5]
        }

        // Tool Summary: merge tool call counts
        toolMap := make(map[string]string)
        // Parse previous tool summaries
        for _, ts := range cc.lastSummary.ToolSummary {
                name := parseToolNameFromSummary(ts)
                toolMap[name] = ts
        }
        // New tool summaries override old ones (they have more up-to-date counts)
        for _, ts := range extracted.ToolSummary {
                name := parseToolNameFromSummary(ts)
                toolMap[name] = ts
        }
        for _, ts := range toolMap {
                merged.ToolSummary = append(merged.ToolSummary, ts)
        }
        if len(merged.ToolSummary) > 10 {
                merged.ToolSummary = merged.ToolSummary[:10]
        }

        return merged
}

// ============================================================================
// Rendering
// ============================================================================

// renderStructuredSummary renders the structured summary into a formatted string
func (cc *ContextCompressor) renderStructuredSummary(s *structuredSummary) string {
        var sb strings.Builder

        sb.WriteString("=== 对话历史摘要 ===\n")
        sb.WriteString(fmt.Sprintf("摘要版本: v%d | 压缩时间: %s | 压缩消息数: 原始摘要覆盖\n",
                s.Version, time.Now().Format("2006-01-02 15:04:05")))

        if cc.focusTopic != "" {
                sb.WriteString(fmt.Sprintf("焦点主题: %s\n", cc.focusTopic))
        }

        sb.WriteString("\n")

        // Goal
        if len(s.Goals) > 0 {
                sb.WriteString("## 🎯 目标 (Goal)\n")
                for i, goal := range s.Goals {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, goal))
                }
                sb.WriteString("\n")
        }

        // Constraints
        if len(s.Constraints) > 0 {
                sb.WriteString("## ⚠️ 约束 (Constraints)\n")
                for i, c := range s.Constraints {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
                }
                sb.WriteString("\n")
        }

        // Progress
        if len(s.Progress) > 0 {
                sb.WriteString("## ✅ 进展 (Progress)\n")
                for i, p := range s.Progress {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, p))
                }
                sb.WriteString("\n")
        }

        // Key Decisions
        if len(s.Decisions) > 0 {
                sb.WriteString("## 🔑 关键决策 (Key Decisions)\n")
                for i, d := range s.Decisions {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, d))
                }
                sb.WriteString("\n")
        }

        // Pending Items
        if len(s.Pending) > 0 {
                sb.WriteString("## 📋 待办事项 (Pending Items)\n")
                for i, p := range s.Pending {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, p))
                }
                sb.WriteString("\n")
        }

        // Tool Summary
        if len(s.ToolSummary) > 0 {
                sb.WriteString("## 🔧 工具摘要 (Tool Summary)\n")
                for _, ts := range s.ToolSummary {
                        sb.WriteString(fmt.Sprintf("- %s\n", ts))
                }
                sb.WriteString("\n")
        }

        // Footer guidance
        sb.WriteString("## ⚡ 重要提示\n")
        sb.WriteString("- 此摘要包含了被压缩的对话历史（支持增量更新）\n")
        sb.WriteString("- 请优先响应最新的用户消息\n")
        sb.WriteString("- 如有指令冲突，以最新用户消息为准\n")
        if cc.focusTopic != "" {
                sb.WriteString(fmt.Sprintf("- 当前焦点主题: %s，请保持上下文连贯\n", cc.focusTopic))
        }
        sb.WriteString("=== 摘要结束 ===")

        return sb.String()
}

// ============================================================================
// Helper Functions
// ============================================================================

// containsAny checks if the content string contains any of the given substrings
func containsAny(content string, substrings ...string) bool {
        for _, sub := range substrings {
                if strings.Contains(content, sub) {
                        return true
                }
        }
        return false
}

// truncateString truncates a string to maxLen characters, appending "..." if truncated
func truncateString(s string, maxLen int) string {
        if len(s) <= maxLen {
                return s
        }
        // Truncate at rune boundary
        runes := []rune(s)
        if len(runes) <= maxLen {
                return s
        }
        return string(runes[:maxLen]) + "..."
}

// deduplicateStrings removes duplicate strings from a slice while preserving order
func deduplicateStrings(items []string) []string {
        seen := make(map[string]bool)
        result := make([]string, 0, len(items))
        for _, item := range items {
                key := strings.TrimSpace(item)
                if key == "" {
                        continue
                }
                if !seen[key] {
                        seen[key] = true
                        result = append(result, item)
                }
        }
        return result
}

// mergeStringSlices merges two string slices: keeps items from `previous` first,
// then appends new items from `current` that aren't already present. Limits to maxLen.
func mergeStringSlices(previous, current []string, maxLen int) []string {
        seen := make(map[string]bool)
        result := make([]string, 0, maxLen)

        // Add previous items first
        for _, item := range previous {
                key := strings.TrimSpace(item)
                if key != "" && !seen[key] {
                        seen[key] = true
                        result = append(result, item)
                }
        }

        // Append new current items
        for _, item := range current {
                key := strings.TrimSpace(item)
                if key != "" && !seen[key] {
                        seen[key] = true
                        result = append(result, item)
                }
        }

        if len(result) > maxLen {
                result = result[:maxLen]
        }

        return result
}

// isAddressedIn checks if a pending item appears to have been addressed by any progress item
// Uses simple substring matching
func isAddressedIn(pendingItem string, progressItems []string) bool {
        pendingLower := strings.ToLower(pendingItem)
        // Extract key nouns from pending item (simple heuristic: split and take words > 2 chars)
        words := strings.Fields(pendingLower)
        for _, word := range words {
                if len(word) <= 2 {
                        continue
                }
                // Skip common stop words
                if isStopWord(word) {
                        continue
                }
                for _, progress := range progressItems {
                        if strings.Contains(strings.ToLower(progress), word) {
                                return true
                        }
                }
        }
        return false
}

// isStopWord returns true for common stop words that shouldn't be used for matching
func isStopWord(word string) bool {
        stopWords := map[string]bool{
                "the": true, "and": true, "for": true, "are": true, "but": true,
                "not": true, "you": true, "all": true, "can": true, "had": true,
                "her": true, "was": true, "one": true, "our": true, "out": true,
                "has": true, "have": true, "been": true, "will": true, "with": true,
                "this": true, "that": true, "from": true, "they": true,
                "的": true, "了": true, "在": true, "是": true, "我": true,
                "有": true, "和": true, "就": true, "不": true, "人": true,
                "都": true, "一": true, "一个": true, "上": true, "也": true,
                "到": true, "说": true, "要": true, "去": true, "你": true,
                "会": true, "着": true, "没有": true, "看": true, "好": true,
                "自己": true, "这": true, "他": true, "她": true, "它": true,
        }
        return stopWords[word]
}

// parseToolNameFromSummary extracts the tool name from a tool summary string
// e.g., "shell (called 3 times): ..." -> "shell"
func parseToolNameFromSummary(summary string) string {
        summary = strings.TrimSpace(summary)
        parenIdx := strings.Index(summary, " (")
        if parenIdx > 0 {
                return summary[:parenIdx]
        }
        // If no parenthesis, take first word
        parts := strings.Fields(summary)
        if len(parts) > 0 {
                return parts[0]
        }
        return summary
}

// messagesContainThinkingBlock 檢查消息列表中是否有 assistant 訊息包含 thinking block
// thinking blocks (含 signature) 必須回傳 API，否則 DeepSeek/Anthropic 會返回 400
func (cc *ContextCompressor) messagesContainThinkingBlock(messages []Message) bool {
        for _, msg := range messages {
                if msg.Role == "assistant" && msg.ThinkingSignature != "" {
                        return true
                }
                if msg.Role == "assistant" && msg.ReasoningContent != nil {
                        if reasoning, ok := msg.ReasoningContent.(string); ok && reasoning != "" {
                                return true
                        }
                }
        }
        return false
}

// findLastThinkingInMiddle 在 middle 區域中搜尋最後一個含 thinking block 的 assistant 訊息
func (cc *ContextCompressor) findLastThinkingInMiddle(middle []Message) *Message {
        for i := len(middle) - 1; i >= 0; i-- {
                if middle[i].Role == "assistant" && middle[i].ThinkingSignature != "" {
                        m := middle[i]
                        return &m
                }
                if middle[i].Role == "assistant" && middle[i].ReasoningContent != nil {
                        if reasoning, ok := middle[i].ReasoningContent.(string); ok && reasoning != "" {
                                m := middle[i]
                                return &m
                        }
                }
        }
        return nil
}
