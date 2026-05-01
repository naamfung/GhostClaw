package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "strings"
        "sync/atomic"
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

// GoalClassification holds LLM goal analysis results for sliding window decisions
type GoalClassification struct {
        HasMultipleGoals  bool     `json:"has_multiple_goals"`
        EarliestGoal      string   `json:"earliest_goal"`
        EarliestDone      bool     `json:"earliest_done"`
        CurrentGoal       string   `json:"current_goal"`
        KeyConstraints    []string `json:"key_constraints"`
        CriticalEarlyInfo string   `json:"critical_early_info"`
}

// ContextCompressor 上下文压缩器
// Implements a multi-stage compression algorithm:
//   Stage 1: LLM tool pair compaction (natural language summarization)
//   Stage 2: Protect head messages (system messages)
//   Stage 3: Token budget tail (don't split tool_call/result pairs)
//   Stage 4: Structured summary generation (LLM semantic extraction)
type ContextCompressor struct {
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
        // Recursive compression guard
        inLLMCall                 atomic.Bool  // 防止 LLM 摘要調用觸發遞迴壓縮
}

// NewContextCompressor 创建新的上下文压缩器
func NewContextCompressor() *ContextCompressor {
        return &ContextCompressor{
                protectFirstN:            3,               // 保护前 3 条消息
                tailTokenBudget:          40000,           // 尾部 40K token 预算（確保近期對話完整連貫）
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

// Compress 压缩消息列表 (multi-stage algorithm)
//   Stage 1: LLM tool pair compaction (natural language summarization)
//   Stage 2: Protect head messages (system messages)
//   Stage 3: Build token-budget tail (don't split tool_call/result pairs)
//   Stage 4: Generate structured summary of middle section (LLM semantic extraction)
// maxHistory 為當前模型的動態最大歷史消息數，由 AgentLoop 傳入
func (cc *ContextCompressor) Compress(ctx context.Context, messages []Message, maxHistory int) []Message {
        // Pre-check: recursive call guard
        if cc.inLLMCall.Load() {
                log.Printf("[ContextCompressor] Skipping compression: already inside an LLM call")
                return messages
        }

        // Pre-check: cooldown
        if cc.isInCooldown() {
                log.Printf("[ContextCompressor] Skipping compression: in cooldown (expires in %v)",
                        time.Until(cc.lastCompressionFailure.Add(cc.compressionCooldownDuration)))
                return messages
        }

        // 防止返回共享的底層切片引用
        messages = append([]Message(nil), messages...)

        // Stage 1: LLM tool pair compaction (natural language summarization)
        // Converts tool_use → tool_result pairs into natural language system notes.
        // Falls back to structured truncation if LLM times out.
        if ctx == nil {
                ctx = context.Background()
        }
        messages = cc.compactOldToolPairs(ctx, messages)

        if len(messages) <= maxHistory {
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

        // Stage 4: Generate structured summary (LLM semantic extraction, fallback)
        summary := cc.generateStructuredSummary(ctx, middle)

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

// ============================================================================
// Phase 3: LLM Goal Classification & Sliding Window
// ============================================================================

// classifyGoals uses LLM to analyze dialogue goal structure for sliding window decisions.
// Returns a GoalClassification. Falls back to HasMultipleGoals=false if LLM fails.
func (cc *ContextCompressor) classifyGoals(ctx context.Context, messages []Message) *GoalClassification {
        cc.inLLMCall.Store(true)
        defer cc.inLLMCall.Store(false)

        // Only analyze the first 40 messages (early dialogue)
        sampleCount := 40
        if len(messages) < sampleCount {
                sampleCount = len(messages)
        }
        if sampleCount == 0 {
                return &GoalClassification{HasMultipleGoals: false}
        }

        sampleMsgs := messages[:sampleCount]
        messagesText := cc.serializeMessagesForLLM(sampleMsgs)
        if strings.TrimSpace(messagesText) == "" {
                return &GoalClassification{HasMultipleGoals: false}
        }

        prompt := fmt.Sprintf(`## 你的任務
分析以下對話歷史中的任務目標結構。你僅有 60 秒處理，請簡潔回答，不要作長篇思考以免超時。

## 需要回答
1. 這段對話中是否存在多個互不相關的獨立任務？（是/否）
2. 如果存在，最早的任務目標是什麼？是否已完成？
3. 當前正在進行的任務目標是什麼？（簡述，20 字以內）
4. 是否有跨任務的重要約束需要保留？（如語言偏好、輸出格式、角色設定等，列出關鍵詞）
5. 早期消息中是否有對當前任務仍然關鍵的信息？如有請簡述。

## 輸出格式
嚴格 JSON，不要其他文字：
{"has_multiple_goals": bool, "earliest_goal": "", "earliest_done": bool, "current_goal": "", "key_constraints": [], "critical_early_info": ""}

## 對話歷史（僅分析前 40 條，後面都是最近消息，無需分析）
%s`, messagesText)

        apiType, baseURL, apiKey, modelID := cc.getAPIConfig()

        llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
        defer cancel()

        llmMessages := []Message{
                {Role: "user", Content: prompt},
        }

        resp, err := CallModelSync(llmCtx, llmMessages, apiType, baseURL, apiKey, modelID, 0, 250, false, false)
        if err != nil {
                log.Printf("[ContextCompressor] classifyGoals LLM call failed: %v", err)
                return &GoalClassification{HasMultipleGoals: false}
        }

        content, ok := resp.Content.(string)
        if !ok || strings.TrimSpace(content) == "" {
                log.Printf("[ContextCompressor] classifyGoals: empty response")
                return &GoalClassification{HasMultipleGoals: false}
        }

        // Try to extract JSON from response
        jsonStr := content
        if start := strings.Index(content, "{"); start >= 0 {
                if end := strings.LastIndex(content, "}"); end > start {
                        jsonStr = content[start : end+1]
                }
        }

        var classification GoalClassification
        if err := json.Unmarshal([]byte(jsonStr), &classification); err != nil {
                log.Printf("[ContextCompressor] classifyGoals JSON parse failed: %v (raw: %.200s...)", err, content)
                return &GoalClassification{HasMultipleGoals: false}
        }

        return &classification
}

// applySlidingWindow keeps the most recent messages while preserving critical context.
// Returns a new message slice based on the goal classification strategy.
func (cc *ContextCompressor) applySlidingWindow(messages []Message, classification *GoalClassification, keepCount int) []Message {
        if classification == nil {
                classification = &GoalClassification{HasMultipleGoals: false}
        }

        msgCount := len(messages)
        if msgCount <= keepCount {
                return messages
        }

        // Determine strategy and keep range
        var keepStart int

        switch {
        case !classification.HasMultipleGoals:
                // Simple: keep most recent messages
                keepStart = msgCount - keepCount

        case classification.HasMultipleGoals && classification.EarliestDone:
                // Old goal is done, keep recent + inject constraints
                keepStart = msgCount - keepCount

        case classification.HasMultipleGoals && !classification.EarliestDone && classification.CriticalEarlyInfo != "":
                // Old goal still active with critical info — keep more + inject
                extraKeep := 20
                if keepCount+extraKeep > msgCount {
                        extraKeep = msgCount - keepCount
                }
                keepStart = msgCount - keepCount - extraKeep

        default:
                // Conservative: keep more messages, no sliding
                extraKeep := 20
                if keepCount+extraKeep > msgCount {
                        extraKeep = msgCount - keepCount
                }
                keepStart = msgCount - keepCount - extraKeep
        }

        if keepStart < 0 {
                keepStart = 0
        }

        // Preserve system messages from the head
        var headSystems []Message
        for _, msg := range messages {
                if msg.Role == "system" {
                        headSystems = append(headSystems, msg)
                } else {
                        break
                }
        }

        // Build kept messages (preserving tool_call/result pairs)
        kept := messages[keepStart:]

        // Ensure tool_call/result pair integrity — don't leave orphan tool results
        // If the first message is a tool result without its parent assistant, remove it
        for len(kept) > 0 && kept[0].Role == "tool" {
                kept = kept[1:]
        }

        // Build result: head system messages + kept
        result := make([]Message, 0, len(headSystems)+len(kept)+1)
        result = append(result, headSystems...)

        // Build SESSION_CONSTRAINTS note if needed
        var constraintsNote string
        if len(classification.KeyConstraints) > 0 || classification.CriticalEarlyInfo != "" {
                var sb strings.Builder
                sb.WriteString("[SESSION_CONSTRAINTS]\n")
                sb.WriteString("以下為跨任務持續有效的信息，在當前及後續任務中均需遵守：\n")
                for _, c := range classification.KeyConstraints {
                        sb.WriteString(fmt.Sprintf("- %s\n", c))
                }
                if classification.CriticalEarlyInfo != "" {
                        sb.WriteString(fmt.Sprintf("- %s\n", classification.CriticalEarlyInfo))
                }
                sb.WriteString("[/SESSION_CONSTRAINTS]")
                constraintsNote = sb.String()
        }

        // Insert constraints note after head system messages
        if constraintsNote != "" {
                result = append(result, Message{
                        Role:      "system",
                        Content:   constraintsNote,
                        Timestamp: time.Now().Unix(),
                })
        }

        result = append(result, kept...)

        // Ensure at least one user message exists
        hasUser := false
        for _, msg := range result {
                if msg.Role == "user" {
                        hasUser = true
                        break
                }
        }
        if !hasUser {
                // Find the last user from original messages
                for i := len(messages) - 1; i >= 0; i-- {
                        if messages[i].Role == "user" {
                                result = append(result, messages[i])
                                break
                        }
                }
        }

        if len(result) < len(messages) {
                log.Printf("[ContextCompressor] Phase 3 sliding window: %d → %d messages (strategy: goals=%v, earliestDone=%v)",
                        len(messages), len(result), classification.HasMultipleGoals, classification.EarliestDone)
        }

        return result
}

// GenerateSummary generates a structured summary from an arbitrary list of messages.
// This is used to summarize discarded messages during history truncation,
// ensuring the model retains key context (accomplishments, decisions, tool usage)
// even when the detailed messages are no longer in the active history.
func (cc *ContextCompressor) GenerateSummary(ctx context.Context, messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	return cc.generateStructuredSummary(ctx, messages)
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
// Stage 1: Compact Old Tool Pairs (LLM natural language summarization)
// ============================================================================
// compactOldToolPairs scans for assistant(with ToolCalls) → tool(result) pairs
// and batch-summarizes old ones via LLM into natural language system notes.
// Falls back to structured truncation if LLM is unavailable or times out.

// toolPair represents a single tool_call → tool_result pair
type toolPair struct {
        assistantIdx int     // index of the assistant message with tool_calls
        toolIndices  []int   // indices of corresponding tool result messages
        toolCallIDs  []string // tool call IDs from the assistant message
}

// compactOldToolPairs replaces old tool_use → tool_result pairs with natural
// language system notes generated by LLM. Recent pairs and tail-associated
// pairs are preserved as-is.
func (cc *ContextCompressor) compactOldToolPairs(ctx context.Context, messages []Message) []Message {
        if cc.preserveRecentToolResults <= 0 {
                return messages
        }

        // Step 1: Identify all tool pairs (assistant with ToolCalls → tool results)
        pairs := cc.identifyToolPairs(messages)
        if len(pairs) == 0 {
                return messages
        }

        // Step 2: Determine which pairs to protect
        protectedSet := make(map[int]bool) // pair indices to protect
        pairIndicesWithToolIndices := make(map[int][]int)

        for i, pair := range pairs {
                var allIndices []int
                allIndices = append(allIndices, pair.assistantIdx)
                allIndices = append(allIndices, pair.toolIndices...)
                pairIndicesWithToolIndices[i] = allIndices
        }

        // Protect recent N pairs (by pair count, not message count)
        if len(pairs) > cc.preserveRecentToolResults {
                for i := len(pairs) - cc.preserveRecentToolResults; i < len(pairs); i++ {
                        protectedSet[i] = true
                }
        } else {
                // All pairs are recent, nothing to compact
                return messages
        }

        // Protect pairs associated with the last 5 assistant messages in tail
        tailAssistantCount := 0
        for i := len(messages) - 1; i >= 0 && tailAssistantCount < 5; i-- {
                if messages[i].Role == "assistant" && messages[i].ToolCalls != nil {
                        tcIDs := cc.extractToolCallIDs(messages[i])
                        // Find which pair(s) these tool calls belong to
                        for pairIdx, pair := range pairs {
                                for _, pairTCID := range pair.toolCallIDs {
                                        for _, tailTCID := range tcIDs {
                                                if pairTCID == tailTCID {
                                                        protectedSet[pairIdx] = true
                                                }
                                        }
                                }
                        }
                        tailAssistantCount++
                }
        }

        // Step 3: Collect pairs to compact
        var pairsToCompact []int
        for i := range pairs {
                if !protectedSet[i] {
                        pairsToCompact = append(pairsToCompact, i)
                }
        }

        if len(pairsToCompact) == 0 {
                return messages
        }

        // Step 4: Batch process pairs through LLM (10 pairs per batch)
        const batchSize = 10
        compactNotes := make(map[int]string) // pair index → compact note

        for start := 0; start < len(pairsToCompact); start += batchSize {
                end := start + batchSize
                if end > len(pairsToCompact) {
                        end = len(pairsToCompact)
                }
                batch := pairsToCompact[start:end]

                note, err := cc.compactToolPairsWithLLM(ctx, messages, pairs, batch)
                if err != nil {
                        log.Printf("[ContextCompressor] Stage 1 LLM compact failed: %v, using fallback", err)
                        // Fallback for each pair in the batch
                        for _, pairIdx := range batch {
                                compactNotes[pairIdx] = cc.compactToolPairFallback(messages, pairs[pairIdx])
                        }
                        continue
                }
                // The LLM returns one note that may cover multiple pairs
                compactNotes[batch[0]] = note
                for i := 1; i < len(batch); i++ {
                        compactNotes[batch[i]] = "" // subsequent pairs absorbed into first pair's note
                }
        }

        // Step 5: Build new message list with compact notes replacing old tool pairs
        return cc.applyCompactNotes(messages, pairs, protectedSet, compactNotes)
}

// identifyToolPairs scans messages and identifies all assistant→tool_result pairs
func (cc *ContextCompressor) identifyToolPairs(messages []Message) []toolPair {
        var pairs []toolPair

        for i := 0; i < len(messages); i++ {
                msg := messages[i]
                if msg.Role != "assistant" || msg.ToolCalls == nil {
                        continue
                }

                tcIDs := cc.extractToolCallIDs(msg)
                if len(tcIDs) == 0 {
                        continue
                }

                pair := toolPair{
                        assistantIdx: i,
                        toolCallIDs:  tcIDs,
                }

                // Find corresponding tool result messages (must immediately follow)
                for j := i + 1; j < len(messages); j++ {
                        if messages[j].Role == "tool" && cc.isToolCallIDInList(messages[j].ToolCallID, tcIDs) {
                                pair.toolIndices = append(pair.toolIndices, j)
                        } else if messages[j].Role != "tool" {
                                break
                        }
                }

                if len(pair.toolIndices) > 0 {
                        pairs = append(pairs, pair)
                }
        }

        return pairs
}

// compactToolPairsWithLLM sends a batch of tool pairs to LLM for summarization
func (cc *ContextCompressor) compactToolPairsWithLLM(ctx context.Context, messages []Message, pairs []toolPair, batchIndices []int) (string, error) {
        cc.inLLMCall.Store(true)
        defer cc.inLLMCall.Store(false)

        // Build prompt with serialized pairs
        var sb strings.Builder
        for _, pairIdx := range batchIndices {
                if pairIdx >= len(pairs) {
                        continue
                }
                pair := pairs[pairIdx]
                sb.WriteString(fmt.Sprintf("--- 操作 %d ---\n", pairIdx))
                if pair.assistantIdx < len(messages) {
                        assistantMsg := messages[pair.assistantIdx]
                        // Extract tool call info
                        sb.WriteString("工具調用: ")
                        tcNames := cc.extractToolCallNamesFromMessage(assistantMsg)
                        sb.WriteString(strings.Join(tcNames, ", "))
                        sb.WriteString("\n")
                }
                for _, toolIdx := range pair.toolIndices {
                        if toolIdx < len(messages) {
                                content := extractStringContent(messages[toolIdx])
                                // Truncate very long tool results for the prompt (keep enough for LLM to extract key info)
                                if utf8.RuneCountInString(content) > 1000 {
                                        runes := []rune(content)
                                        content = string(runes[:1000]) + "..."
                                }
                                sb.WriteString(fmt.Sprintf("結果: %s\n", content))
                        }
                }
                sb.WriteString("\n")
        }

        prompt := fmt.Sprintf(`## 你的任務
你係一個記憶壓縮器。以下係 agent 執行過嘅工具操作記錄。
你要為 agent 撰寫**有價值嘅記憶筆記**，等佢之後可以靠呢啲筆記回憶起做過乜、學到乜。

## 核心原則：呢啲筆記係 agent 僅存嘅記憶
- 以下工具記錄即將被永久丟棄，你寫嘅筆記係 agent 唯一可以保留嘅信息
- 如果筆記寫得含糊，agent 就會永久遺失嗰啲信息
- 所以：**關鍵信息絕不可以省略**，必須完整提取到筆記入面

## 規則
- 唔好提工具名稱（如 SmartShell, ReadFileLine），工具名冇意義
- **必須保留實際內容**：讀取檔案就要記低檔案入面嘅關鍵代碼/邏輯/結構係乜，唔好只寫「了解了某文件」
  - 例子錯：❌ 「查閱了 main.go，了解了 AgentLoop 結構」
  - 例子啱：✅ 「main.go 中 AgentLoop 主循環用 for + select 監聽 ctx.Done() 同 msgChan，每次迭代檢查 MaxIterations 上限」
- **必須保留具體結果**：編譯就要記低係成功定失敗、有冇 warning/error；搜索就要記低搵到幾多結果、關鍵匹配係乜
  - 例子錯：❌ 「編譯成功，無錯誤」
  - 例子啱：✅ 「go build ./... 編譯通過，無 warning，二進制輸出 15MB」
- 每條筆記一句話，不超過 120 字（關鍵信息優先，寧可稍長都唔好遺漏）
- 連續相關的操作合併為一條筆記
- 只輸出筆記文字，不要 JSON，不要 Markdown

## 時間限制
60 秒內完成，直接寫筆記，唔好長篇思考。

## 工具操作記錄
%s`, sb.String())

        // Get API config
        apiType, baseURL, apiKey, modelID := cc.getAPIConfig()

        llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
        defer cancel()

        llmMessages := []Message{
                {Role: "user", Content: prompt},
        }

        resp, err := CallModelSync(llmCtx, llmMessages, apiType, baseURL, apiKey, modelID, 0, 800, false, false)
        if err != nil {
                return "", fmt.Errorf("LLM call failed: %w", err)
        }

        if resp.Content == nil {
                return "", fmt.Errorf("LLM returned empty content")
        }

        content, ok := resp.Content.(string)
        if !ok || strings.TrimSpace(content) == "" {
                return "", fmt.Errorf("LLM content is empty or not a string")
        }

        return strings.TrimSpace(content), nil
}

// compactToolPairFallback generates a structured compact note without LLM
func (cc *ContextCompressor) compactToolPairFallback(messages []Message, pair toolPair) string {
        var sb strings.Builder

        if pair.assistantIdx < len(messages) {
                assistantMsg := messages[pair.assistantIdx]
                tcNames := cc.extractToolCallNamesFromMessage(assistantMsg)
                for _, name := range tcNames {
                        sb.WriteString(fmt.Sprintf("[%s] ", name))
                }
        }

        for i, toolIdx := range pair.toolIndices {
                if toolIdx >= len(messages) {
                        continue
                }
                content := extractStringContent(messages[toolIdx])
                runes := []rune(content)
                // Truncate tool result
                if len(runes) > 200 {
                        content = string(runes[:200]) + "..."
                }
                if i > 0 {
                        sb.WriteString(" | ")
                }
                sb.WriteString(content)
        }

        result := strings.TrimSpace(sb.String())
        if result == "" {
                result = "Tool operation completed."
        }
        return result
}

// extractToolCallNamesFromMessage extracts tool function names from a message's ToolCalls
func (cc *ContextCompressor) extractToolCallNamesFromMessage(msg Message) []string {
        var names []string
        if msg.ToolCalls == nil {
                return names
        }

        if tcSlice, ok := msg.ToolCalls.([]interface{}); ok {
                for _, tc := range tcSlice {
                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                        if name, ok := function["name"].(string); ok {
                                                names = append(names, name)
                                        }
                                }
                        }
                }
                return names
        }

        if tcMapSlice, ok := msg.ToolCalls.([]map[string]interface{}); ok {
                for _, tcMap := range tcMapSlice {
                        if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                if name, ok := function["name"].(string); ok {
                                        names = append(names, name)
                                }
                        }
                }
        }
        return names
}

// applyCompactNotes builds a new message list with tool pairs replaced by compact notes
func (cc *ContextCompressor) applyCompactNotes(messages []Message, pairs []toolPair, protectedSet map[int]bool, compactNotes map[int]string) []Message {
        // Determine which message indices to keep vs replace
        replaceRanges := make(map[int]string) // startIdx → compact note
        skipIndices := make(map[int]bool)

        for pairIdx, pair := range pairs {
                if protectedSet[pairIdx] {
                        continue // keep as-is
                }
                note, hasNote := compactNotes[pairIdx]
                if !hasNote || note == "" {
                        continue // no compact note generated, keep original
                }

                // Mark this pair's indices for replacement
                replaceRanges[pair.assistantIdx] = note
                skipIndices[pair.assistantIdx] = true
                for _, toolIdx := range pair.toolIndices {
                        skipIndices[toolIdx] = true
                }
        }

        // Build new message list
        result := make([]Message, 0, len(messages))
        pendingNote := ""

        for i, msg := range messages {
                if skipIndices[i] {
                        // Check if this is the assistant index where we insert the note
                        if note, ok := replaceRanges[i]; ok {
                                pendingNote = note
                        }
                        continue
                }

                // Insert compact note as system message before the next kept message
                if pendingNote != "" {
                        result = append(result, Message{
                                Role:      "system",
                                Content:   pendingNote,
                                Timestamp: time.Now().Unix(),
                        })
                        pendingNote = ""
                }

                result = append(result, msg)
        }

        // Flush any remaining pending note
        if pendingNote != "" {
                result = append(result, Message{
                        Role:      "system",
                        Content:   pendingNote,
                        Timestamp: time.Now().Unix(),
                })
        }

        compacted := len(messages) - len(result)
        if compacted > 0 {
                log.Printf("[ContextCompressor] Stage 1: compacted %d tool pair messages into natural language notes", compacted)
        }

        return result
}

// getAPIConfig retrieves the current API configuration
func (cc *ContextCompressor) getAPIConfig() (string, string, string, string) {
        if globalConfigManager != nil {
                apiCfg := globalConfigManager.GetAPIConfig()
                return apiCfg.APIType, apiCfg.BaseURL, apiCfg.APIKey, apiCfg.Model
        }
        return apiType, baseURL, apiKey, modelID
}

// ============================================================================
// Stage 1 Fallback: Trim Old Tool Results (保留作為應急方案)
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

// generateStructuredSummary 生成结构化摘要（支持增量更新，LLM semantic extraction）
func (cc *ContextCompressor) generateStructuredSummary(ctx context.Context, messages []Message) string {
        if len(messages) == 0 {
                return cc.previousSummary
        }

        // Extract structured information from messages via LLM
        extracted := cc.llmExtractStructuredData(ctx, messages)

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
// extractStructuredData is deprecated; use llmExtractStructuredData instead.
// Kept for backward compatibility as a thin wrapper.
func (cc *ContextCompressor) extractStructuredData(messages []Message) *structuredSummary {
        return cc.llmExtractStructuredData(context.Background(), messages)
}

// llmExtractStructuredData uses LLM to extract structured information from messages.
// Falls back to an empty structuredSummary if LLM fails or times out.
func (cc *ContextCompressor) llmExtractStructuredData(ctx context.Context, messages []Message) *structuredSummary {
        cc.inLLMCall.Store(true)
        defer cc.inLLMCall.Store(false)

        // Serialize messages for the LLM prompt
        messagesText := cc.serializeMessagesForLLM(messages)
        if strings.TrimSpace(messagesText) == "" {
                return &structuredSummary{}
        }

        prompt := fmt.Sprintf(`## 你的任務
分析以下對話片段，提取結構化信息。

## 時間限制
你僅有 60 秒處理，請在規定時間內完成任務，不要作長篇大論的思考，以免超時終止。

## 提取內容
1. goals: 用戶提出的任務/目標（已完成或進行中，最多 5 項，每項 ≤120 字）
2. constraints: 用戶設定的限制條件（語言偏好、格式要求、禁止事項等，最多 5 項）
3. progress: assistant 已完成的具體工作（最多 5 項，每項 ≤150 字）
4. decisions: 做出的重要選擇（最多 5 項，每項 ≤120 字）
5. pending: 尚未完成的待辦事項（最多 5 項，每項 ≤120 字）
6. tool_usage: 使用了哪些工具及其結果摘要（最多 10 項，每項 ≤150 字）

## 輸出格式
嚴格 JSON，不要 Markdown 代碼塊，不要其他文字：
{"goals": [...], "constraints": [...], "progress": [...], "decisions": [...], "pending": [...], "tool_usage": [...]}

如果某類別沒有內容，返回空陣列 []。

## 對話片段
---
%s
---`, messagesText)

        apiType, baseURL, apiKey, modelID := cc.getAPIConfig()

        llmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
        defer cancel()

        llmMessages := []Message{
                {Role: "user", Content: prompt},
        }

        resp, err := CallModelSync(llmCtx, llmMessages, apiType, baseURL, apiKey, modelID, 0, 600, false, false)
        if err != nil {
                log.Printf("[ContextCompressor] llmExtractStructuredData LLM call failed: %v", err)
                return &structuredSummary{}
        }

        content, ok := resp.Content.(string)
        if !ok || strings.TrimSpace(content) == "" {
                log.Printf("[ContextCompressor] llmExtractStructuredData: empty response")
                return &structuredSummary{}
        }

        // Parse JSON response
        var raw struct {
                Goals       []string `json:"goals"`
                Constraints []string `json:"constraints"`
                Progress    []string `json:"progress"`
                Decisions   []string `json:"decisions"`
                Pending     []string `json:"pending"`
                ToolUsage   []string `json:"tool_usage"`
        }

        // Try to extract JSON from response (model may wrap in markdown)
        jsonStr := content
        if start := strings.Index(content, "{"); start >= 0 {
                if end := strings.LastIndex(content, "}"); end > start {
                        jsonStr = content[start : end+1]
                }
        }

        if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
                log.Printf("[ContextCompressor] llmExtractStructuredData JSON parse failed: %v (raw: %.200s...)", err, content)
                return &structuredSummary{}
        }

        // Apply limits
        if len(raw.Goals) > 5 {
                raw.Goals = raw.Goals[:5]
        }
        if len(raw.Constraints) > 5 {
                raw.Constraints = raw.Constraints[:5]
        }
        if len(raw.Progress) > 5 {
                raw.Progress = raw.Progress[:5]
        }
        if len(raw.Decisions) > 5 {
                raw.Decisions = raw.Decisions[:5]
        }
        if len(raw.Pending) > 5 {
                raw.Pending = raw.Pending[:5]
        }
        if len(raw.ToolUsage) > 10 {
                raw.ToolUsage = raw.ToolUsage[:10]
        }

        return &structuredSummary{
                Goals:       raw.Goals,
                Constraints: raw.Constraints,
                Progress:    raw.Progress,
                Decisions:   raw.Decisions,
                Pending:     raw.Pending,
                ToolSummary: raw.ToolUsage,
        }
}

// serializeMessagesForLLM converts messages to a compact text representation for LLM prompts
func (cc *ContextCompressor) serializeMessagesForLLM(messages []Message) string {
        var sb strings.Builder
        maxChars := 20000 // Prevent overly large prompts
        totalChars := 0

        for _, msg := range messages {
                content := extractStringContent(msg)
                if content == "" && msg.ToolCalls == nil {
                        continue
                }

                line := fmt.Sprintf("[%s] ", msg.Role)
                if content != "" {
                        runes := []rune(content)
                        if len(runes) > 300 {
                                line += string(runes[:300]) + "..."
                        } else {
                                line += content
                        }
                }
                if msg.ToolCalls != nil {
                        names := cc.extractToolCallNamesFromMessage(msg)
                        if len(names) > 0 {
                                line += fmt.Sprintf(" [tools: %s]", strings.Join(names, ", "))
                        }
                }

                sb.WriteString(line)
                sb.WriteString("\n")

                totalChars += len(line)
                if totalChars > maxChars {
                        sb.WriteString("... [剩余消息已截断]\n")
                        break
                }
        }

        return sb.String()
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

        // Pending: merge and deduplicate
        merged.Pending = mergeStringSlices(cc.lastSummary.Pending, extracted.Pending, 5)

        // Tool Summary: merge and deduplicate
        toolMap := make(map[string]bool)
        for _, ts := range cc.lastSummary.ToolSummary {
                key := strings.TrimSpace(ts)
                if key != "" {
                        toolMap[key] = true
                }
        }
        for _, ts := range extracted.ToolSummary {
                key := strings.TrimSpace(ts)
                if key != "" {
                        toolMap[key] = true
                }
        }
        for ts := range toolMap {
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

        // 記憶圍欄：明確告訴模型呢啲係歷史背景資料，唔係當前用戶指令
        sb.WriteString("[MEMORY_CONTEXT]\n")
        sb.WriteString("(System note: 以下是已被压缩的早期对话历史摘要. " +
                "所有目标/进展/决策均为历史记录, 已经处理完毕或已过时. " +
                "请勿根据此摘要发起任何操作, 仅作理解对话背景之用. " +
                "如摘要内容与最新用户消息冲突, 以最新用户消息为准.)\n\n")

        sb.WriteString("=== 已压缩的对话历史摘要（非当前任务） ===\n")
        sb.WriteString(fmt.Sprintf("摘要版本: v%d | 压缩时间: %s\n",
                s.Version, time.Now().Format("2006-01-02 15:04:05")))

        if cc.focusTopic != "" {
                sb.WriteString(fmt.Sprintf("历史焦点主题: %s（可能已被最新消息取代）\n", cc.focusTopic))
        }

        sb.WriteString("\n")

        // Goal — 已处理的用户请求
        if len(s.Goals) > 0 {
                sb.WriteString("## 📜 已处理的用户请求 / Past User Requests\n")
                sb.WriteString("(以下请求已在对话早期处理完毕, 请勿重新执行)\n")
                for i, goal := range s.Goals {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, goal))
                }
                sb.WriteString("\n")
        }

        // Constraints — 历史约束
        if len(s.Constraints) > 0 {
                sb.WriteString("## 📜 历史约束 / Past Constraints\n")
                sb.WriteString("(对话早期用户提出的限制, 可能已被最新指令覆盖)\n")
                for i, c := range s.Constraints {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
                }
                sb.WriteString("\n")
        }

        // Progress — 已完成的工作
        if len(s.Progress) > 0 {
                sb.WriteString("## 📜 已完成的工作 / Completed Work\n")
                sb.WriteString("(对话早期已完成的步骤, 仅供参考, 无需重复)\n")
                for i, p := range s.Progress {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, p))
                }
                sb.WriteString("\n")
        }

        // Key Decisions — 历史决策
        if len(s.Decisions) > 0 {
                sb.WriteString("## 📜 历史决策 / Past Decisions\n")
                sb.WriteString("(对话早期做出的选择, 仅作背景参考)\n")
                for i, d := range s.Decisions {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, d))
                }
                sb.WriteString("\n")
        }

        // Pending Items — 可能已过时的遗留事项
        if len(s.Pending) > 0 {
                sb.WriteString("## 📜 历史遗留事项 / Historical Leftovers\n")
                sb.WriteString("(对话早期未完成的事项, 可能已过时或被后续操作解决. 除非最新用户消息明确要求继续, 否则忽略.)\n")
                for i, p := range s.Pending {
                        sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, p))
                }
                sb.WriteString("\n")
        }

        // Tool Summary — 工具使用记录
        if len(s.ToolSummary) > 0 {
                sb.WriteString("## 📜 历史工具使用记录 / Past Tool Usage\n")
                sb.WriteString("(对话早期使用过的工具及其结果摘要)\n")
                for _, ts := range s.ToolSummary {
                        sb.WriteString(fmt.Sprintf("- %s\n", ts))
                }
                sb.WriteString("\n")
        }

        // Footer guidance — 强免责声明
        sb.WriteString("## 重要免责声明\n")
        sb.WriteString("- 以上所有内容均为对话早期的历史摘要, 已压缩归档\n")
        sb.WriteString("- 历史请求/目标/待办事项均已在当时处理或已过时\n")
        sb.WriteString("- 如有指令冲突, 以最新用户消息 (标注为 [USR:LATEST]) 的指令为准\n")
        sb.WriteString("- 请勿根据此摘要的历史目标或待办事项发起任何新操作\n")
        sb.WriteString("- 此摘要仅用于帮助理解对话背景, 不构成任何执行指令\n")
        if cc.focusTopic != "" {
                sb.WriteString(fmt.Sprintf("- 注意: 历史焦点主题 %s 可能已被最新消息取代\n", cc.focusTopic))
        }
        sb.WriteString("=== 历史摘要结束, 请关注最新用户消息 ===\n")
        sb.WriteString("[/MEMORY_CONTEXT]")

        return sb.String()
}

// ============================================================================
// Helper Functions
// ============================================================================

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
