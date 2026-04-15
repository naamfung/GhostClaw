package main

import (
        "bytes"
        "context"
        "crypto/tls"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "strings"
        "sync"
        "time"
)

// rateLimiter 基于令牌桶算法的速率限制器
// 以 endpoint（BaseURL）为粒度，限制每分钟请求数
type rateLimiter struct {
        mu       sync.Mutex
        buckets  map[string]*rateBucket
}

type rateBucket struct {
        tokens    int       // 剩余令牌数
        maxTokens int       // 每分钟最大令牌数（即每分钟最大请求数）
        lastRefill time.Time // 上次补充时间
}

var globalRateLimiter = &rateLimiter{
        buckets: make(map[string]*rateBucket),
}

// waitIfNeeded 等待直到可以发送请求（如果需要限速）
// endpoint 用于标识不同的 API 服务商
// rateLimit 为每分钟请求数限制，0 表示不限制
func (rl *rateLimiter) waitIfNeeded(ctx context.Context, endpoint string, rateLimit int) error {
        if rateLimit <= 0 {
                return nil
        }

        rl.mu.Lock()
        bucket, exists := rl.buckets[endpoint]
        if !exists {
                bucket = &rateBucket{
                        tokens:     rateLimit,
                        maxTokens:  rateLimit,
                        lastRefill: time.Now(),
                }
                rl.buckets[endpoint] = bucket
        }

        // 补充令牌：每分钟补充 maxTokens 个
        now := time.Now()
        elapsed := now.Sub(bucket.lastRefill)
        if elapsed >= time.Minute {
                bucket.tokens = bucket.maxTokens
                bucket.lastRefill = now
        } else {
                // 按比例补充
                refillTokens := int(float64(bucket.maxTokens) * elapsed.Minutes())
                if refillTokens > 0 {
                        bucket.tokens += refillTokens
                        if bucket.tokens > bucket.maxTokens {
                                bucket.tokens = bucket.maxTokens
                        }
                        bucket.lastRefill = now
                }
        }

        if bucket.tokens > 0 {
                bucket.tokens--
                rl.mu.Unlock()
                return nil
        }

        // 没有可用令牌，计算需要等待的时间
        waitDuration := time.Minute - elapsed
        rl.mu.Unlock()

        if IsDebug {
                log.Printf("[RateLimit] %s: 达到速率限制 (%d/min)，等待 %v", endpoint, rateLimit, waitDuration.Round(time.Second))
        }

        select {
        case <-time.After(waitDuration):
                // 等待后重试获取令牌
                return rl.waitIfNeeded(ctx, endpoint, rateLimit)
        case <-ctx.Done():
                return ctx.Err()
        }
}

// 全局 HTTP 客户端
var httpClient = &http.Client{
        Timeout: 0, // 取消默认超时，由 Context 控制
        Transport: &http.Transport{
                // 禁用 HTTP/2：SSE (Server-Sent Events) 基於 HTTP/1.1 長連接，
                // 許多 API 代理對 HTTP/2 的 SSE 支持不佳，會導致
                // "http2: response body closed" 錯誤。
                // 設置空的 TLSNextProto map 可以阻止 Go 自動協商 HTTP/2。
                TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
        },
}

// StreamReplacer 用于流式文本替换（最长匹配）
type StreamReplacer struct {
        buffer             []rune
        maxKeyLen          int
        sortedReplacements []StringReplacement
        out                func(r rune)
}

// NewStreamReplacer 创建流式替换器
func NewStreamReplacer(out func(r rune)) *StreamReplacer {
        sr := &StreamReplacer{
                buffer:             make([]rune, 0),
                sortedReplacements: sortedStringsReplacements.Replacements,
                out:                out,
        }
        // 计算最长键的字符数
        for _, rep := range sr.sortedReplacements {
                if len([]rune(rep.Key)) > sr.maxKeyLen {
                        sr.maxKeyLen = len([]rune(rep.Key))
                }
        }
        return sr
}

// Write 处理新文本
func (sr *StreamReplacer) Write(text string) {
        runes := []rune(text)
        for _, r := range runes {
                sr.buffer = append(sr.buffer, r)
                sr.flushSafe()
        }
}

// Flush 输出缓冲区剩余内容
func (sr *StreamReplacer) Flush() {
        for _, r := range sr.buffer {
                sr.out(r)
        }
        sr.buffer = sr.buffer[:0]
}

// flushSafe 处理缓冲区，输出安全字符
func (sr *StreamReplacer) flushSafe() {
        for {
                if len(sr.buffer) == 0 {
                        break
                }
                // 尝试从起始位置匹配最长键
                matched := false
                for _, rep := range sr.sortedReplacements {
                        keyRunes := []rune(rep.Key)
                        if len(keyRunes) <= len(sr.buffer) {
                                eq := true
                                for i := 0; i < len(keyRunes); i++ {
                                        if sr.buffer[i] != keyRunes[i] {
                                                eq = false
                                                break
                                        }
                                }
                                if eq {
                                        // 输出替换值
                                        for _, r := range []rune(rep.Value) {
                                                sr.out(r)
                                        }
                                        // 移除匹配部分
                                        sr.buffer = sr.buffer[len(keyRunes):]
                                        matched = true
                                        break
                                }
                        }
                }
                if matched {
                        continue
                }

                // 检查起始位置是否是某个键的前缀
                isPrefix := false
                for _, rep := range sr.sortedReplacements {
                        keyRunes := []rune(rep.Key)
                        if len(keyRunes) > 0 && len(sr.buffer) < len(keyRunes) {
                                eq := true
                                for i := 0; i < len(sr.buffer); i++ {
                                        if sr.buffer[i] != keyRunes[i] {
                                                eq = false
                                                break
                                        }
                                }
                                if eq {
                                        isPrefix = true
                                        break
                                }
                        }
                }
                if isPrefix {
                        // 是某个键的前缀，等待更多字符
                        break
                }

                // 不是前缀，输出第一个字符
                sr.out(sr.buffer[0])
                sr.buffer = sr.buffer[1:]
                // 继续循环
        }
}

// applyReplacements 对字符串应用替换（最长匹配，非递归）
func applyReplacements(text string) string {
        runes := []rune(text)
        result := make([]rune, 0, len(runes))
        i := 0
        for i < len(runes) {
                matched := false
                for _, rep := range sortedStringsReplacements.Replacements {
                        keyRunes := []rune(rep.Key)
                        if i+len(keyRunes) <= len(runes) {
                                eq := true
                                for j := 0; j < len(keyRunes); j++ {
                                        if runes[i+j] != keyRunes[j] {
                                                eq = false
                                                break
                                        }
                                }
                                if eq {
                                        // 替换
                                        result = append(result, []rune(rep.Value)...)
                                        i += len(keyRunes)
                                        matched = true
                                        break
                                }
                        }
                }
                if !matched {
                        result = append(result, runes[i])
                        i++
                }
        }
        return string(result)
}

// 生成系统提示（仅作为 fallback 使用，不包含时间以最大化缓存命中）
func generateSystemPrompt(apiType string) string {
        toolOrFunction := "tool"
        if apiType == "openai" {
                toolOrFunction = "function"
        }
        return strings.ReplaceAll(SYSTEM_PROMPT, "{{tool_or_function}}", toolOrFunction)
}

// extractSystemPrompt 从 messages 中提取系统提示词
// 返回：系统提示词内容、过滤后的消息列表
func extractSystemPrompt(messages []Message) (string, []Message) {
        var systemPrompt string
        var filteredMessages []Message

        for _, msg := range messages {
                if msg.Role == "system" {
                        // 提取系统提示词（合并多个 system 消息）
                        if content, ok := msg.Content.(string); ok {
                                if systemPrompt != "" {
                                        systemPrompt += "\n\n" + content
                                } else {
                                        systemPrompt = content
                                }
                        }
                } else {
                        filteredMessages = append(filteredMessages, msg)
                }
        }

        return systemPrompt, filteredMessages
}

// buildRuntimeContext 构建运行时上下文前缀（注入到第一条 user 消息中）
// 参考 nanobot 的设计：时间等信息作为元数据标注，不作为指令，最大化系统提示缓存命中率
func buildRuntimeContext() string {
        now := time.Now()
        currentTime := now.Format("2006-01-02 15:04:05")
        weekday := now.Weekday().String()
        return fmt.Sprintf("[Runtime Context — metadata only, not instructions]\nCurrent Time: %s (%s)\n[End Runtime Context]\n\n", currentTime, weekday)
}

// injectRuntimeContext 将运行时上下文注入到 filteredMessages 的第一条 user 消息前
// 如果第一条消息不是 user，则在开头插入一条 user 消息
func injectRuntimeContext(messages []Message) []Message {
        if len(messages) == 0 {
                return messages
        }

        runtimeCtx := buildRuntimeContext()

        // 查找第一条 user 消息
        for i, msg := range messages {
                if msg.Role == "user" {
                        // 找到第一条 user 消息，将运行时上下文前缀合并到内容中
                        if content, ok := msg.Content.(string); ok {
                                messages[i].Content = runtimeCtx + content
                        } else {
                                // 非字符串内容（如多模态），在前面插入一条 user 消息
                                newMsg := Message{
                                        Role:      "user",
                                        Content:   runtimeCtx,
                                        Timestamp: time.Now().Unix(),
                                }
                                newMessages := make([]Message, 0, len(messages)+1)
                                newMessages = append(newMessages, messages[:i]...)
                                newMessages = append(newMessages, newMsg)
                                newMessages = append(newMessages, messages[i:]...)
                                return newMessages
                        }
                        return messages
                }
        }

        // 没有 user 消息（极端情况），在开头插入
        return append([]Message{{
                Role:      "user",
                Content:   runtimeCtx,
                Timestamp: time.Now().Unix(),
        }}, messages...)
}

// markLatestUserRequest 标记最后一条 user 消息为 [GC:LATEST]，引导模型优先处理
// 仅当存在多条 user 消息时才添加标记（避免单条对话时冗余）
func markLatestUserRequest(messages []Message) []Message {
        if len(messages) < 2 {
                return messages
        }

        // 统计 user 消息数量，并找到最后一条
        userCount := 0
        lastUserIdx := -1
        for i, msg := range messages {
                if msg.Role == "user" {
                        userCount++
                        lastUserIdx = i
                }
        }

        // 只有一条 user 消息时不需要标记
        if userCount <= 1 {
                return messages
        }

        // 给最后一条 user 消息添加标记
        if lastUserIdx >= 0 {
                if content, ok := messages[lastUserIdx].Content.(string); ok {
                        messages[lastUserIdx].Content = LatestRequestMarker + " " + content
                }
        }

        return messages
}

// convertToAnthropicFormat 將內部 Message 轉換為 Anthropic API 要求的格式
// 注意：Anthropic API 使用单独的 system 参数，不将 system 消息放在 messages 中
func convertToAnthropicFormat(messages []Message) []map[string]interface{} {
        anthropicMessages := make([]map[string]interface{}, 0, len(messages))
        for _, msg := range messages {
                switch msg.Role {
                case "system":
                        // Anthropic 使用单独的 system 参数，跳过 messages 中的 system 消息
                        continue
                case "user":
                        anthropicMessages = append(anthropicMessages, map[string]interface{}{
                                "role":    "user",
                                "content": msg.Content,
                        })
                case "assistant":
                        if msg.ToolCalls != nil {
                                // 构建 content 数组，包含 text 和 tool_use
                                content := []map[string]interface{}{}
                                if msg.Content != nil {
                                        if txt, ok := msg.Content.(string); ok && txt != "" {
                                                content = append(content, map[string]interface{}{
                                                        "type": "text",
                                                        "text": txt,
                                                })
                                        }
                                }
                                // 處理 []interface{} 和 []map[string]interface{} 兩種格式
                                var toolCallSlice []map[string]interface{}
                                switch v := msg.ToolCalls.(type) {
                                case []interface{}:
                                        for _, tc := range v {
                                                if tcMap, ok := tc.(map[string]interface{}); ok {
                                                        toolCallSlice = append(toolCallSlice, tcMap)
                                                }
                                        }
                                case []map[string]interface{}:
                                        toolCallSlice = v
                                }
                                for _, tcMap := range toolCallSlice {
                                        if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                                toolUse := map[string]interface{}{
                                                        "type": "tool_use",
                                                        "id":   tcMap["id"],
                                                        "name": function["name"],
                                                }
                                                // arguments 可能是字符串，尝试解析为对象
                                                if argsStr, ok := function["arguments"].(string); ok {
                                                        var args map[string]interface{}
                                                        if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
                                                                toolUse["input"] = args
                                                        } else {
                                                                toolUse["input"] = argsStr
                                                        }
                                                }
                                                content = append(content, toolUse)
                                        }
                                }
                                anthropicMessages = append(anthropicMessages, map[string]interface{}{
                                        "role":    "assistant",
                                        "content": content,
                                })
                        } else {
                                anthropicMessages = append(anthropicMessages, map[string]interface{}{
                                        "role":    "assistant",
                                        "content": msg.Content,
                                })
                        }
                case "tool":
                        // 确保 tool_use_id 不为空
                        toolUseID := msg.ToolCallID
                        if toolUseID == "" {
                                toolUseID = "unknown_tool_use"
                        }
                        // 确保 content 是字符串
                        var contentStr string
                        switch v := msg.Content.(type) {
                        case string:
                                contentStr = v
                        case nil:
                                contentStr = ""
                        default:
                                if jsonBytes, err := json.Marshal(v); err == nil {
                                        contentStr = string(jsonBytes)
                                } else {
                                        contentStr = fmt.Sprintf("%v", v)
                                }
                        }
                        toolResult := map[string]interface{}{
                                "type":        "tool_result",
                                "tool_use_id": toolUseID,
                                "content":     contentStr,
                        }
                        anthropicMessages = append(anthropicMessages, map[string]interface{}{
                                "role": "user",
                                "content": []map[string]interface{}{
                                        toolResult,
                                },
                        })
                }
        }
        return anthropicMessages
}

// 转换为Ollama格式（支持工具消息）
// 注意：Ollama API 使用单独的 system 参数，不将 system 消息放在 messages 中
func convertToOllamaFormat(messages []Message) []map[string]interface{} {
        ollamaMessages := make([]map[string]interface{}, 0, len(messages))
        for _, msg := range messages {
                // 跳过 system 消息，Ollama 使用单独的 system 参数
                if msg.Role == "system" {
                        continue
                }
                ollamaMsg := map[string]interface{}{
                        "role": msg.Role,
                }
                if msg.Role == "assistant" && msg.ToolCalls != nil {
                        ollamaMsg["tool_calls"] = msg.ToolCalls
                        if msg.Content != nil {
                                ollamaMsg["content"] = msg.Content
                        }
                } else if msg.Role == "tool" {
                        // tool 消息的 content 必须是字符串
                        switch v := msg.Content.(type) {
                        case string:
                                ollamaMsg["content"] = v
                        case nil:
                                ollamaMsg["content"] = ""
                        default:
                                if jsonBytes, err := json.Marshal(v); err == nil {
                                        ollamaMsg["content"] = string(jsonBytes)
                                } else {
                                        ollamaMsg["content"] = fmt.Sprintf("%v", v)
                                }
                        }
                } else {
                        ollamaMsg["content"] = msg.Content
                }
                ollamaMessages = append(ollamaMessages, ollamaMsg)
        }
        return ollamaMessages
}

// 转换为OpenAI格式
func convertToOpenAIFormat(messages []Message) []map[string]interface{} {
        openaiMessages := make([]map[string]interface{}, len(messages))
        for i, msg := range messages {
                openaiMsg := map[string]interface{}{
                        "role": msg.Role,
                }

                if msg.Role == "tool" {
                        // tool 消息必须有 tool_call_id 和 content
                        // 如果 tool_call_id 为空，生成一个占位符（避免 API 报错）
                        toolCallID := msg.ToolCallID
                        if toolCallID == "" {
                                toolCallID = "unknown_tool_call"
                        }
                        openaiMsg["tool_call_id"] = toolCallID

                        // content 必须是字符串，不能是 nil
                        switch v := msg.Content.(type) {
                        case string:
                                openaiMsg["content"] = v
                        case nil:
                                openaiMsg["content"] = ""
                        default:
                                // 其他类型转换为 JSON 字符串
                                if jsonBytes, err := json.Marshal(v); err == nil {
                                        openaiMsg["content"] = string(jsonBytes)
                                } else {
                                        openaiMsg["content"] = fmt.Sprintf("%v", v)
                                }
                        }
                } else if msg.Role == "assistant" && msg.ToolCalls != nil {
                        // 确保 tool_calls 中的 arguments 是字符串格式
                        var normalizedToolCalls []interface{}

                        // 处理不同类型的 ToolCalls
                        switch v := msg.ToolCalls.(type) {
                        case []interface{}:
                                for j, tc := range v {
                                        normalizedToolCalls = append(normalizedToolCalls, normalizeToolCall(tc))
                                        _ = j // unused
                                }
                        case []map[string]interface{}:
                                for _, tc := range v {
                                        normalizedToolCalls = append(normalizedToolCalls, normalizeToolCall(tc))
                                }
                        default:
                                // 未知类型，直接使用原始值
                                normalizedToolCalls = nil
                                openaiMsg["tool_calls"] = msg.ToolCalls
                        }

                        if len(normalizedToolCalls) > 0 {
                                openaiMsg["tool_calls"] = normalizedToolCalls
                        }
                        // 处理 content：当有 tool_calls 时，空字符串会导致某些 API（如 SiliconFlow）报错
                        // 必须是 null 或不设置该字段
                        if msg.Content != nil {
                                if contentStr, ok := msg.Content.(string); ok && contentStr == "" {
                                        // 空字符串，不设置 content 字段（某些 API 不接受空字符串 + tool_calls）
                                        // 不设置 content 字段
                                } else {
                                        openaiMsg["content"] = msg.Content
                                }
                        }
                        // 如果 content 是 nil，不设置该字段
                } else {
                        openaiMsg["content"] = msg.Content
                }

                openaiMessages[i] = openaiMsg
        }
        return openaiMessages
}

// normalizeToolCall 确保单个 tool call 的 arguments 是字符串格式
func normalizeToolCall(tc interface{}) interface{} {
        tcMap, ok := tc.(map[string]interface{})
        if !ok {
                return tc
        }

        normalizedTC := make(map[string]interface{})
        for k, v := range tcMap {
                normalizedTC[k] = v
        }

        // 深拷貝 function 子 map，防止修改原始消息的 ToolCalls
        if function, ok := normalizedTC["function"].(map[string]interface{}); ok {
                functionCopy := make(map[string]interface{}, len(function))
                for k, v := range function {
                        functionCopy[k] = v
                }
                normalizedTC["function"] = functionCopy
                function = functionCopy

                if args, exists := function["arguments"]; exists {
                        switch v := args.(type) {
                        case string:
                                // 已经是字符串，无需处理
                        case map[string]interface{}:
                                if argsJSON, err := json.Marshal(v); err == nil {
                                        function["arguments"] = string(argsJSON)
                                }
                        default:
                                if argsJSON, err := json.Marshal(v); err == nil {
                                        function["arguments"] = string(argsJSON)
                                }
                        }
                }
        }

        return normalizedTC
}

// validateAndCleanMessages 验证并清理消息，确保符合 API 要求
func validateAndCleanMessages(messages []Message) []Message {
        if len(messages) == 0 {
                return messages
        }

        cleaned := make([]Message, 0, len(messages))

        for i, msg := range messages {
                // 跳过完全空的消息
                if msg.Role == "" {
                        if IsDebug {
                                log.Printf("Warning: skipping message with empty role at index %d", i)
                        }
                        continue
                }

                // 创建消息副本
                cleanedMsg := msg

                // 确保 content 不为 nil（对于需要 content 的消息类型）
                if msg.Role == "user" || msg.Role == "assistant" {
                        if msg.Content == nil {
                                cleanedMsg.Content = ""
                        }
                        // 对于 assistant 且有 tool_calls 的情况，某些 API 要求 content 为 null 或空字符串
                        // 但为了安全，如果 content 是空字符串，我们设置为 nil
                        if msg.Role == "assistant" && msg.ToolCalls != nil {
                                if contentStr, ok := msg.Content.(string); ok && contentStr == "" {
                                        cleanedMsg.Content = nil
                                }
                        }
                }

                // 对于 tool 消息，确保 tool_call_id 存在且 content 是字符串
                if msg.Role == "tool" {
                        if msg.ToolCallID == "" {
                                cleanedMsg.ToolCallID = fmt.Sprintf("auto_id_%d", i)
                                if IsDebug {
                                        log.Printf("Warning: tool message missing tool_call_id, assigned: %s", cleanedMsg.ToolCallID)
                                }
                        }
                        if msg.Content == nil {
                                cleanedMsg.Content = ""
                        } else if _, ok := msg.Content.(string); !ok {
                                // 如果不是字符串，尝试转换为 JSON 字符串
                                if jsonBytes, err := json.Marshal(msg.Content); err == nil {
                                        cleanedMsg.Content = string(jsonBytes)
                                } else {
                                        cleanedMsg.Content = fmt.Sprintf("%v", msg.Content)
                                }
                        }
                }

                // 检查是否与上一条消息角色相同（特殊情况：连续的 tool 消息是允许的）
                if len(cleaned) > 0 {
                        lastMsg := cleaned[len(cleaned)-1]
                        // 允许连续的 tool 消息
                        if lastMsg.Role == msg.Role && msg.Role != "tool" {
                                if IsDebug {
                                        log.Printf("Warning: consecutive messages with same role: %s at index %d", msg.Role, i)
                                }
                                // 如果是连续两个 assistant 且都没有 tool_calls，可以合并 content
                                if msg.Role == "assistant" && lastMsg.ToolCalls == nil && msg.ToolCalls == nil {
                                        lastContent, _ := lastMsg.Content.(string)
                                        thisContent, _ := msg.Content.(string)
                                        if lastContent != "" && thisContent != "" {
                                                cleaned[len(cleaned)-1].Content = lastContent + "\n" + thisContent
                                        } else if thisContent != "" {
                                                cleaned[len(cleaned)-1].Content = thisContent
                                        }
                                        continue
                                }
                                // 如果是连续两个 user 消息，合并
                                if msg.Role == "user" {
                                        lastContent, _ := lastMsg.Content.(string)
                                        thisContent, _ := msg.Content.(string)
                                        if lastContent != "" && thisContent != "" {
                                                cleaned[len(cleaned)-1].Content = lastContent + "\n" + thisContent
                                        } else if thisContent != "" {
                                                cleaned[len(cleaned)-1].Content = thisContent
                                        }
                                        continue
                                }
                                // 其他情况保留，但记录警告
                        }
                }

                cleaned = append(cleaned, cleanedMsg)
        }

        // ==================== 最终检查与修复 ====================

        // 1. 移除孤立 tool 消息：tool result 没有前置的 assistant+tool_calls
        cleaned = removeOrphanedToolMessages(cleaned)

        // 2. 移除孤立 tool_calls：assistant 有 tool_calls 但后续没有 tool result
        cleaned = removeOrphanedToolCalls(cleaned)

        // 3. 合并连续同角色消息（compressMessages 可能产生）
        cleaned = mergeConsecutiveSameRole(cleaned)

        // 4. 确保消息序列以 user 开头（不能以 assistant/tool 开头）
        if len(cleaned) > 0 && cleaned[0].Role != "user" && cleaned[0].Role != "system" {
                log.Printf("[validateAndCleanMessages] Fixing: inserting synthetic user message before %s-first sequence", cleaned[0].Role)
                cleaned = append([]Message{{
                        Role:    "user",
                        Content: "continue",
                }}, cleaned...)
        }

        // 5. 移除空的 user/assistant 消息（content 为 nil 或空字符串且无 tool_calls）
        finalCleaned := make([]Message, 0, len(cleaned))
        for _, msg := range cleaned {
                if msg.Role == "user" || msg.Role == "assistant" {
                        contentStr, _ := msg.Content.(string)
                        if contentStr == "" && msg.ToolCalls == nil {
                                if IsDebug {
                                        log.Printf("Warning: removing empty %s message (no content, no tool_calls)", msg.Role)
                                }
                                continue
                        }
                }
                finalCleaned = append(finalCleaned, msg)
        }

        return finalCleaned
}


// findLegalStart 前向扫描算法，确保消息序列开头不会有孤儿工具结果
// 参考 nanobot 的 _find_legal_start：从前往后扫描，遇到没有对应 assistant tool_calls 的
// tool 消息时，从该消息之后重新开始。同时处理连续多个孤儿的情况。
func findLegalStart(messages []Message) []Message {
        if len(messages) == 0 {
                return messages
        }

        // 收集所有 assistant 消息中声明的 tool_call_id
        declared := make(map[string]bool)
        start := 0

        for i, msg := range messages {
                switch msg.Role {
                case "assistant":
                        // 收集此 assistant 消息声明的所有 tool_call ID
                        declared = make(map[string]bool) // 每遇到新的 assistant，重置声明集合
                        if msg.ToolCalls != nil {
                                switch v := msg.ToolCalls.(type) {
                                case []interface{}:
                                        for _, tc := range v {
                                                if tcMap, ok := tc.(map[string]interface{}); ok {
                                                        if id, ok := tcMap["id"].(string); ok {
                                                                declared[id] = true
                                                        }
                                                }
                                        }
                                case []map[string]interface{}:
                                        for _, tc := range v {
                                                if id, ok := tc["id"].(string); ok {
                                                        declared[id] = true
                                                }
                                        }
                                }
                        }
                case "tool":
                        // 检查此 tool 消息是否有对应的声明
                        if !declared[msg.ToolCallID] {
                                // 孤儿工具结果！从下一条消息重新开始
                                start = i + 1
                                declared = make(map[string]bool)
                        }
                case "user":
                        // user 消息重置声明集合（新的对话轮次）
                        declared = make(map[string]bool)
                case "system":
                        // system 消息不重置 declared（避免打破 assistant→tool_result 配对）
                        // 但如果 system 消息出现在截断边界处，不应被当作孤儿跳过
                }
        }

        if start > 0 {
                // 安全保护：绝不能截断所有消息
                if start >= len(messages) {
                        // 從末尾向前找最近一條非 tool 消息（避免返回孤兒 tool result）
                        if IsDebug {
                                log.Printf("[findLegalStart] WARNING: start(%d) >= len(%d), searching for safe fallback", start, len(messages))
                        }
                        for i := len(messages) - 1; i >= 0; i-- {
                                if messages[i].Role != "tool" {
                                        return []Message{messages[i]}
                                }
                        }
                        // 全部都是 tool 消息的極端情況，返回最後一條
                        return []Message{messages[len(messages)-1]}
                }

                // 保留前导 system 消息（包含 system prompt 等关键信息）
                systemEnd := 0
                for i, msg := range messages {
                        if msg.Role == "system" {
                                systemEnd = i + 1
                        } else {
                                break
                        }
                }

                if systemEnd > 0 && start >= systemEnd {
                        // 截断点在 system 消息块之后，保留 system 头部 + 非孤儿尾部
                        if IsDebug {
                                log.Printf("[findLegalStart] Removed %d orphaned messages, preserving %d system prefix", start-systemEnd, systemEnd)
                        }
                        return append(messages[:systemEnd], messages[start:]...)
                }

                if IsDebug {
                        log.Printf("[findLegalStart] Removed %d orphaned leading messages", start)
                }
                return messages[start:]
        }
        return messages
}

// removeOrphanedToolMessages 移除孤立的 tool 消息（没有前置 assistant+tool_calls）
func removeOrphanedToolMessages(messages []Message) []Message {
        if len(messages) == 0 {
                return messages
        }
        result := make([]Message, 0, len(messages))
        for i, msg := range messages {
                if msg.Role == "tool" {
                        // 查找前面是否有 assistant 消息带有匹配的 tool_calls
                        hasMatchingAssistant := false
                        for j := i - 1; j >= 0; j-- {
                                prevMsg := messages[j]
                                if prevMsg.Role == "assistant" && prevMsg.ToolCalls != nil {
                                        if hasToolCallID(prevMsg.ToolCalls, msg.ToolCallID) {
                                                hasMatchingAssistant = true
                                                break
                                        }
                                }
                                // 如果遇到 user 或 system 消息，停止向前搜索
                                if prevMsg.Role == "user" || prevMsg.Role == "system" {
                                        break
                                }
                        }
                        if !hasMatchingAssistant {
                                if IsDebug {
                                        log.Printf("Warning: removing orphaned tool message at index %d (tool_call_id: %s)", i, msg.ToolCallID)
                                }
                                continue
                        }
                }
                result = append(result, msg)
        }
        return result
}

// hasToolCallID 檢查 ToolCalls 中是否包含指定的 tool_call_id
func hasToolCallID(toolCalls interface{}, toolCallID string) bool {
        if toolCallID == "" {
                return false
        }
        switch v := toolCalls.(type) {
        case []interface{}:
                for _, tc := range v {
                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                if id, ok := tcMap["id"].(string); ok && id == toolCallID {
                                        return true
                                }
                        }
                }
        case []map[string]interface{}:
                for _, tc := range v {
                        if id, ok := tc["id"].(string); ok && id == toolCallID {
                                return true
                        }
                }
        }
        return false
}

// removeOrphanedToolCalls 移除孤立的 tool_calls（assistant 有 tool_calls 但后续没有 tool result）
func removeOrphanedToolCalls(messages []Message) []Message {
        if len(messages) == 0 {
                return messages
        }
        // 首先收集所有存在的 tool_call_id（来自 tool 消息）
        existingToolResults := make(map[string]bool)
        for _, msg := range messages {
                if msg.Role == "tool" && msg.ToolCallID != "" {
                        existingToolResults[msg.ToolCallID] = true
                }
        }

        result := make([]Message, 0, len(messages))
        for i, msg := range messages {
                if msg.Role == "assistant" && msg.ToolCalls != nil {
                        // 检查是否所有 tool_calls 都有对应的 tool result
                        hasAnyResult := false
                        remainingToolCalls := filterToolCallsWithResults(msg.ToolCalls, existingToolResults, &hasAnyResult)

                        if !hasAnyResult {
                                // 所有 tool_calls 都是孤立的，移除 tool_calls，保留 content
                                if IsDebug {
                                        log.Printf("Warning: removing all orphaned tool_calls from assistant message at index %d", i)
                                }
                                newMsg := msg
                                newMsg.ToolCalls = nil
                                if newMsg.Content == nil {
                                        newMsg.Content = ""
                                }
                                result = append(result, newMsg)
                                continue
                        } else if len(remainingToolCalls) > 0 {
                                // 部分有结果，只保留有结果的 tool_calls
                                newMsg := msg
                                newMsg.ToolCalls = remainingToolCalls
                                result = append(result, newMsg)
                                continue
                        }
                }
                result = append(result, msg)
        }
        return result
}

// filterToolCallsWithResults 过滤出有对应 tool result 的 tool_calls
func filterToolCallsWithResults(toolCalls interface{}, existingResults map[string]bool, hasAnyResult *bool) []interface{} {
        var remaining []interface{}
        switch v := toolCalls.(type) {
        case []interface{}:
                for _, tc := range v {
                        if tcMap, ok := tc.(map[string]interface{}); ok {
                                if id, ok := tcMap["id"].(string); ok && existingResults[id] {
                                        remaining = append(remaining, tc)
                                        *hasAnyResult = true
                                }
                        }
                }
        case []map[string]interface{}:
                for _, tc := range v {
                        if id, ok := tc["id"].(string); ok && existingResults[id] {
                                remaining = append(remaining, tc)
                                *hasAnyResult = true
                        }
                }
        }
        return remaining
}

// mergeConsecutiveSameRole 合并连续同角色的消息（排除 tool 消息和带 tool_calls 的 assistant）
func mergeConsecutiveSameRole(messages []Message) []Message {
        if len(messages) <= 1 {
                return messages
        }
        result := make([]Message, 0, len(messages))
        for _, msg := range messages {
                if len(result) > 0 {
                        lastMsg := result[len(result)-1]
                        if lastMsg.Role == msg.Role && msg.Role != "tool" {
                                // 不合并有 tool_calls 的 assistant
                                if msg.Role == "assistant" && (lastMsg.ToolCalls != nil || msg.ToolCalls != nil) {
                                        result = append(result, msg)
                                        continue
                                }
                                // 合并 content（僅限字串類型，跳過多模態內容）
                                lastContent, lastIsStr := lastMsg.Content.(string)
                                thisContent, thisIsStr := msg.Content.(string)
                                if lastIsStr && thisIsStr && lastContent != "" && thisContent != "" {
                                        result[len(result)-1].Content = lastContent + "\n" + thisContent
                                } else if thisIsStr && thisContent != "" {
                                        result[len(result)-1].Content = thisContent
                                } else {
                                        // 非字串內容（多模態等），不合并，直接追加
                                        result = append(result, msg)
                                        continue
                                }
                                continue
                        }
                }
                result = append(result, msg)
        }
        return result
}

// 准备请求数据
// role 参数用于工具权限过滤，为 nil 时返回所有工具
// 系统提示词从 messages 中的 system 消息提取，根据 API 类型正确处理
func prepareRequestData(messages []Message, apiType, baseURL, modelID string, temperature float64, maxTokens int, stream bool, thinking bool, role *Role) (map[string]interface{}, string, string, error) {
        var data map[string]interface{}
        var endpoint string
        t0 := time.Now()

        // 限制 maxTokens 不超过模型的最大输出 token 限制，避免 API 拒绝请求
        if maxTokens > 0 {
                if maxOutputLimit := getMaxOutputTokens(modelID); maxOutputLimit > 0 && maxTokens > maxOutputLimit {
                        maxTokens = maxOutputLimit
                }
        }
        t1 := time.Now()

        // 验证并清理消息
        messages = validateAndCleanMessages(messages)
        t2 := time.Now()

        // 从 messages 中提取系统提示词
        systemPromptFromMessages, filteredMessages := extractSystemPrompt(messages)
        t3 := time.Now()

        // 确定最终使用的系统提示词（不含时间，最大化缓存命中率）
        var finalSystemPrompt string
        if systemPromptFromMessages != "" {
                finalSystemPrompt = systemPromptFromMessages
        } else {
                finalSystemPrompt = generateSystemPrompt(apiType)
        }
        t4 := time.Now()

        filteredMessages = injectRuntimeContext(filteredMessages)
        filteredMessages = markLatestUserRequest(filteredMessages)
        t5 := time.Now()

        switch apiType {
        case "anthropic":
                if baseURL == "" {
                        baseURL = ANTHROPIC_BASE_URL
                }
                anthropicMessages := convertToAnthropicFormat(filteredMessages)
                t6 := time.Now()
                tools := getFilteredToolsWithContext(apiType, role, getModelContextLength(modelID))
                t7 := time.Now()
                data = map[string]interface{}{
                        "model":       modelID,
                        "system":      finalSystemPrompt,
                        "messages":    anthropicMessages,
                        "tools":       tools,
                        "temperature": temperature,
                        "stream":      stream,
                }
                // Anthropic API 要求 max_tokens 为必填字段，未设置时使用默认值 4096
                // OpenAI 兼容 API（如 BigModel）会拒绝 max_tokens:0，因此在 openai 分支中按条件发送
                if maxTokens > 0 {
                        data["max_tokens"] = maxTokens
                } else {
                        data["max_tokens"] = 4096
                }
                if thinking {
                        data["thinking"] = map[string]interface{}{
                                "type": "enabled",
                        }
                }
                endpoint = "/messages"
                log.Printf("[Perf] prepareRequestData breakdown: maxOutput=%v validate=%v extractSys=%v sysPrompt=%v inject=%v convert=%v tools=%v", t1.Sub(t0), t2.Sub(t1), t3.Sub(t2), t4.Sub(t3), t5.Sub(t4), t6.Sub(t5), t7.Sub(t6))

        case "ollama":
                baseURL = OLLAMA_BASE_URL
                // Ollama 使用单独的 system 参数，messages 中不应包含 system 消息
                ollamaMessages := convertToOllamaFormat(filteredMessages)
                data = map[string]interface{}{
                        "model":       modelID,
                        "messages":    ollamaMessages,
                        "tools":       getFilteredToolsWithContext(apiType, role, getModelContextLength(modelID)),
                        "stream":      stream,
                        "system":      finalSystemPrompt,
                        "temperature": temperature,
                }
                endpoint = "/chat"

        case "openai":
                if baseURL == "" {
                        baseURL = OPENAI_BASE_URL
                }
                // OpenAI API 期望 system 消息在 messages 数组中
                // 需要将系统提示词作为第一条 system 消息
                var openaiMessages []map[string]interface{}

                // 构建包含 system 消息的 messages 列表
                openaiMessages = append(openaiMessages, map[string]interface{}{
                        "role":    "system",
                        "content": finalSystemPrompt,
                })
                // 添加其他消息
                openaiMessages = append(openaiMessages, convertToOpenAIFormat(filteredMessages)...)

                data = map[string]interface{}{
                        "model":       modelID,
                        "messages":    openaiMessages,
                        "tools":       getFilteredToolsWithContext(apiType, role, getModelContextLength(modelID)),
                        "temperature": temperature,
                        "stream":      stream,
                }
                // 仅在 maxTokens > 0 时发送 max_tokens，避免部分提供商（如 BigModel）拒绝 max_tokens:0
                if maxTokens > 0 {
                        data["max_tokens"] = maxTokens
                }
                // 思考模式：根据提供商支持情况发送对应格式
                // DeepSeek → "thinking": true
                // GLM/智谱、Qwen/通义 → "thinking": {"type":"enabled"}
                // Anthropic 由上方 anthropic 分支单独处理
                if thinking {
                        if supported, format := isThinkingSupported(baseURL); supported {
                                if format == "bool" {
                                        data["thinking"] = true
                                } else {
                                        data["thinking"] = map[string]interface{}{
                                                "type": "enabled",
                                        }
                                }
                        }
                }
                endpoint = "/chat/completions"

        default:
                return nil, "", "", fmt.Errorf("unsupported API type: %s", apiType)
        }

        // 清理空值字段，避免部分提供商拒绝空 tools 数组或 nil 值
        if tools, ok := data["tools"]; ok {
                if tools == nil {
                        delete(data, "tools")
                } else if arr, ok := tools.([]interface{}); ok && len(arr) == 0 {
                        delete(data, "tools")
                } else if arr, ok := tools.([]map[string]interface{}); ok && len(arr) == 0 {
                        delete(data, "tools")
                }
        }

        return data, endpoint, baseURL, nil
}

// isThinkingSupported 判断该 OpenAI 兼容提供商是否支持 thinking 模式及对应格式
// 返回值：
//   - supported: 是否支持 thinking 模式
//   - format: "bool" 表示发送布尔值 true，"object" 表示发送 {"type":"enabled"}
//
// 已知支持：
//   - DeepSeek  → bool:   "thinking": true
//   - GLM/智谱  → object: "thinking": {"type":"enabled"}
//   - Qwen/通义  → object: "thinking": {"type":"enabled"}
//
// 注意：Anthropic 的 thinking 由 prepareRequestData 的 anthropic 分支单独处理
func isThinkingSupported(baseURL string) (supported bool, format string) {
        lower := strings.ToLower(baseURL)
        // DeepSeek 使用布尔值格式
        if strings.Contains(lower, "deepseek.com") || strings.Contains(lower, "deepseek") {
                return true, "bool"
        }
        // GLM/智谱、Qwen/通义 使用对象格式
        if strings.Contains(lower, "bigmodel.cn") ||
                strings.Contains(lower, "dashscope.aliyuncs.com") ||
                strings.Contains(lower, "aliyuncs.com") {
                return true, "object"
        }
        return false, ""
}

// isContextLengthError 检测错误消息是否为上下文长度超过限制的错误
func isContextLengthError(errorBody string) bool {
        lowerError := strings.ToLower(errorBody)
        // 常见的上下文长度错误关键词
        contextLengthKeywords := []string{
                "context length",
                "token limit",
                "context window",
                "max tokens",
                "token count",
                "context size",
                "input length",
                "message length",
                "tokens exceed",
                "context exceeds",
                "exceeds the maximum",
        }
        for _, keyword := range contextLengthKeywords {
                if strings.Contains(lowerError, keyword) {
                        return true
                }
        }
        return false
}

// 发送请求（支持 Context）
func sendRequest(ctx context.Context, data map[string]interface{}, endpoint, apiKey, apiType string) (*http.Response, error) {
        t0 := time.Now()
        jsonData, err := json.Marshal(data)
        if err != nil {
                return nil, fmt.Errorf("failed to marshal request data: %w", err)
        }
        log.Printf("[CallModel] Request to %s: body=%d bytes, apiType=%s", endpoint, len(jsonData), apiType)

        req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
        if err != nil {
                return nil, fmt.Errorf("failed to create request: %w", err)
        }

        req.Header.Set("Content-Type", "application/json")
        if apiKey != "" {
                if apiType == "openai" || apiType == "ollama" {
                        req.Header.Set("Authorization", "Bearer "+apiKey)
                } else if apiType == "anthropic" {
                        req.Header.Set("x-api-key", apiKey)
                }
        }

        if IsDebug {
                fmt.Printf("Sending request to: %s\n", endpoint)
                fmt.Printf("Request data: %v\n", data)
        }

        resp, err := httpClient.Do(req)
        if err != nil {
                if IsDebug {
                        fmt.Printf("Error sending request: %v\n", err)
                }
                return nil, fmt.Errorf("failed to send request: %w", err)
        }

        log.Printf("[CallModel] TTFB: %v (status=%d)", time.Since(t0), resp.StatusCode)

        if resp.StatusCode != http.StatusOK {
                errorBody, _ := io.ReadAll(resp.Body)
                resp.Body.Close()
                errorBodyStr := string(errorBody)
                if IsDebug {
                        fmt.Printf("Error response status: %d\n", resp.StatusCode)
                        fmt.Printf("Error response body: %s\n", errorBodyStr)
                        // 记录发送的消息，帮助诊断问题
                        if messagesData, ok := data["messages"]; ok {
                                fmt.Printf("Messages that caused error: %v\n", messagesData)
                        }
                }
                // 检测上下文长度超过限制的错误
                if isContextLengthError(errorBodyStr) {
                        return nil, fmt.Errorf("context_length_exceeded: %s", errorBodyStr)
                }
                return nil, fmt.Errorf("API returned error status: %d, body: %s", resp.StatusCode, errorBodyStr)
        }

        return resp, nil
}

// 处理OpenAI响应
func handleOpenAIResponse(resp *http.Response) (Response, error) {
        var result Response
        // 使用 map 来解析，因为 MiniMax 等 API 可能返回不同格式的 arguments
        var openaiResp struct {
                Choices []struct {
                        Message struct {
                                Role      string      `json:"role"`
                                Content   interface{} `json:"content"`
                                ToolCalls []struct {
                                        ID       string `json:"id"`
                                        Type     string `json:"type"`
                                        Function struct {
                                                Name      string      `json:"name"`
                                                Arguments interface{} `json:"arguments"` // 改为 interface{} 以支持对象或字符串
                                        } `json:"function"`
                                } `json:"tool_calls"`
                                FunctionCall struct {
                                        Name      string      `json:"name"`
                                        Arguments interface{} `json:"arguments"` // 改为 interface{} 以支持对象或字符串
                                } `json:"function_call"`
                                ReasoningContent interface{} `json:"reasoning_content,omitempty"`
                        } `json:"message"`
                        FinishReason string `json:"finish_reason"`
                } `json:"choices"`
        }

        err := json.NewDecoder(resp.Body).Decode(&openaiResp)
        if err != nil {
                return Response{}, fmt.Errorf("failed to decode OpenAI response: %w", err)
        }

        if len(openaiResp.Choices) > 0 {
                choice := openaiResp.Choices[0]
                result.StopReason = choice.FinishReason

                if IsDebug {
                        messageJson, _ := json.Marshal(choice.Message)
                        fmt.Printf("Message structure: %s\n", string(messageJson))
                }

                if len(choice.Message.ToolCalls) > 0 {
                        var content []map[string]interface{}
                        for _, toolCall := range choice.Message.ToolCalls {
                                // 将 arguments 转换为 JSON 字符串
                                var argsStr string
                                switch v := toolCall.Function.Arguments.(type) {
                                case string:
                                        argsStr = v
                                case map[string]interface{}:
                                        if argsJSON, err := json.Marshal(v); err == nil {
                                                argsStr = string(argsJSON)
                                        }
                                default:
                                        if argsJSON, err := json.Marshal(v); err == nil {
                                                argsStr = string(argsJSON)
                                        }
                                }

                                toolUse := map[string]interface{}{
                                        "id":   toolCall.ID,
                                        "type": "function",
                                        "function": map[string]interface{}{
                                                "name":      toolCall.Function.Name,
                                                "arguments": argsStr,
                                        },
                                }
                                content = append(content, toolUse)
                        }
                        result.Content = content
                        result.StopReason = "function_call"
                } else {
                        if choice.Message.FunctionCall.Name != "" {
                                // 将 arguments 转换为 JSON 字符串
                                var argsStr string
                                switch v := choice.Message.FunctionCall.Arguments.(type) {
                                case string:
                                        argsStr = v
                                case map[string]interface{}:
                                        if argsJSON, err := json.Marshal(v); err == nil {
                                                argsStr = string(argsJSON)
                                        }
                                default:
                                        if argsJSON, err := json.Marshal(v); err == nil {
                                                argsStr = string(argsJSON)
                                        }
                                }

                                var args map[string]interface{}
                                json.Unmarshal([]byte(argsStr), &args)

                                toolUse := map[string]interface{}{
                                        "type":  "function",
                                        "id":    "1",
                                        "name":  choice.Message.FunctionCall.Name,
                                        "input": args,
                                }
                                result.Content = []map[string]interface{}{toolUse}
                                result.StopReason = "function_call"
                        } else {
                                if contentStr, ok := choice.Message.Content.(string); ok {
                                        result.Content = applyReplacements(contentStr)
                                } else {
                                        result.Content = choice.Message.Content
                                }
                                if reasoningStr, ok := choice.Message.ReasoningContent.(string); ok {
                                        result.ReasoningContent = applyReplacements(reasoningStr)
                                } else {
                                        result.ReasoningContent = choice.Message.ReasoningContent
                                }
                        }
                }
        }

        return result, nil
}

// 处理Ollama响应
func handleOllamaResponse(resp *http.Response) (Response, error) {
        var result Response
        var ollamaResp struct {
                Message struct {
                        Role    string      `json:"role"`
                        Content interface{} `json:"content"`
                } `json:"message"`
                Done bool `json:"done"`
        }

        err := json.NewDecoder(resp.Body).Decode(&ollamaResp)
        if err != nil {
                return Response{}, fmt.Errorf("failed to decode Ollama response: %w", err)
        }

        result.Content = ollamaResp.Message.Content
        if contentStr, ok := result.Content.(string); ok {
                result.Content = applyReplacements(contentStr)
        }
        if ollamaResp.Done {
                result.StopReason = "stop"
        } else {
                result.StopReason = "tool_use"
        }

        return result, nil
}

// 处理Anthropic响应
func handleAnthropicResponse(resp *http.Response) (Response, error) {
        var result Response
        var anthropicResp struct {
                Content []struct {
                        Type    string `json:"type"`
                        Text    string `json:"text,omitempty"`
                        ToolUse struct {
                                ID    string                 `json:"id"`
                                Name  string                 `json:"name"`
                                Input map[string]interface{} `json:"input"`
                        } `json:"tool_use,omitempty"`
                        Thinking string `json:"thinking,omitempty"`
                } `json:"content"`
                StopReason string `json:"stop_reason"`
        }

        err := json.NewDecoder(resp.Body).Decode(&anthropicResp)
        if err != nil {
                return Response{}, fmt.Errorf("failed to decode Anthropic response: %w", err)
        }

        var content interface{}
        var hasToolUse bool
        var toolCalls []map[string]interface{}
        var reasoningContent strings.Builder

        for _, item := range anthropicResp.Content {
                if item.Type == "text" && item.Text != "" {
                        if content == nil {
                                content = item.Text
                        } else if str, ok := content.(string); ok {
                                content = str + "\n" + item.Text
                        }
                } else if item.Type == "tool_use" {
                        hasToolUse = true
                        toolCall := map[string]interface{}{
                                "id":   item.ToolUse.ID,
                                "type": "function",
                                "function": map[string]interface{}{
                                        "name":      item.ToolUse.Name,
                                        "arguments": item.ToolUse.Input,
                                },
                        }
                        toolCalls = append(toolCalls, toolCall)
                } else if item.Type == "thinking" && item.Thinking != "" {
                        reasoningContent.WriteString(item.Thinking)
                        reasoningContent.WriteString("\n")
                }
        }

        if reasoningContent.Len() > 0 {
                result.ReasoningContent = reasoningContent.String()
        }

        if hasToolUse {
                result.Content = toolCalls
                result.StopReason = "function_call"
        } else {
                result.StopReason = anthropicResp.StopReason
                if str, ok := content.(string); ok {
                        result.Content = applyReplacements(str)
                } else {
                        result.Content = content
                }
        }

        return result, nil
}

// 处理非流式响应
func handleNonStreamResponse(resp *http.Response, apiType string) (Response, error) {
        responseBody, err := io.ReadAll(resp.Body)
        if err != nil {
                if IsDebug {
                        fmt.Printf("Error reading response body: %v\n", err)
                }
                return Response{}, fmt.Errorf("failed to read response body: %w", err)
        }

        if IsDebug {
                fmt.Printf("Response body: %s\n", string(responseBody))
                debugFile := fmt.Sprintf("debug_response_%d.json", time.Now().Unix())
                if err := os.WriteFile(debugFile, responseBody, 0600); err == nil {
                        fmt.Printf("Debug response data written to: %s\n", debugFile)
                }
        }

        r := bytes.NewReader(responseBody)
        resp.Body = io.NopCloser(r)

        switch apiType {
        case "openai":
                return handleOpenAIResponse(resp)
        case "ollama":
                return handleOllamaResponse(resp)
        default:
                return handleAnthropicResponse(resp)
        }
}

// compressMessages 根据级别压缩消息列表
// level: 0-简化工具消息（提取原始命令+后200字符），1-移除所有工具消息，2-保留最近20条
func compressMessages(messages []Message, level int) []Message {
        if level < 0 {
                level = 0
        }
        if level > 2 {
                level = 2
        }

        // 复制一份，避免修改原切片
        newMsgs := make([]Message, len(messages))
        copy(newMsgs, messages)

        switch level {
        case 0:
                // 构建从 tool_call_id 到 (命令, 工具名) 的映射
                cmdMap := make(map[string]struct {
                        cmd  string
                        tool string
                })
                for _, msg := range newMsgs {
                        if msg.Role == "assistant" && msg.ToolCalls != nil {
                                // 遍历 tool_calls
                                if tcSlice, ok := msg.ToolCalls.([]interface{}); ok {
                                        for _, tc := range tcSlice {
                                                if tcMap, ok := tc.(map[string]interface{}); ok {
                                                        if id, ok := tcMap["id"].(string); ok && id != "" {
                                                                // 提取工具名称和命令
                                                                toolName := ""
                                                                command := ""
                                                                if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                                                        if name, ok := function["name"].(string); ok {
                                                                                toolName = name
                                                                        }
                                                                        if args, ok := function["arguments"]; ok {
                                                                                var argsMap map[string]interface{}
                                                                                switch v := args.(type) {
                                                                                case string:
                                                                                        json.Unmarshal([]byte(v), &argsMap)
                                                                                case map[string]interface{}:
                                                                                        argsMap = v
                                                                                }
                                                                                if argsMap != nil {
                                                                                        if cmd, ok := argsMap["command"].(string); ok {
                                                                                                command = cmd
                                                                                        } else if cmd, ok := argsMap["script"].(string); ok {
                                                                                                command = cmd
                                                                                        } else if cmd, ok := argsMap["expression"].(string); ok {
                                                                                                command = cmd
                                                                                        } else if cmd, ok := argsMap["query"].(string); ok {
                                                                                                command = cmd
                                                                                        }
                                                                                }
                                                                        }
                                                                }
                                                                cmdMap[id] = struct {
                                                                        cmd  string
                                                                        tool string
                                                                }{cmd: command, tool: toolName}
                                                        }
                                                }
                                        }
                                }
                        }
                }

                // 简化 tool 消息的内容
                for i, msg := range newMsgs {
                        if msg.Role == "tool" {
                                contentStr, ok := msg.Content.(string)
                                if !ok {
                                        contentStr = fmt.Sprintf("%v", msg.Content)
                                }
                                // 获取命令信息
                                cmdInfo := cmdMap[msg.ToolCallID]
                                command := cmdInfo.cmd
                                toolName := cmdInfo.tool
                                // 判断是否失败
                                isError := strings.HasPrefix(contentStr, "Error:") || strings.HasPrefix(contentStr, "error:")
                                status := "成功"
                                if isError {
                                        status = "失败"
                                }
                                // 取后200字符作为摘要
                                runes := []rune(contentStr)
                                tail := contentStr
                                if len(runes) > 200 {
                                        tail = string(runes[len(runes)-200:])
                                }
                                var prefix string
                                if command != "" {
                                        prefix = fmt.Sprintf("[%s: %s] [%s] ", toolName, command, status)
                                } else {
                                        prefix = fmt.Sprintf("[工具执行%s] ", status)
                                }
                                newMsgs[i].Content = prefix + tail
                        }
                }
        case 1:
                // 移除所有 tool 消息，并清除 assistant 中的 tool_calls
                filtered := make([]Message, 0, len(newMsgs))
                for _, msg := range newMsgs {
                        if msg.Role == "tool" {
                                continue
                        }
                        if msg.Role == "assistant" && msg.ToolCalls != nil {
                                // 创建新消息，移除 tool_calls
                                newMsg := msg
                                newMsg.ToolCalls = nil
                                if msg.Content == nil {
                                        newMsg.Content = ""
                                }
                                // 只有 content 非空时才保留，避免产生空 assistant 消息
                                if contentStr, ok := newMsg.Content.(string); ok && contentStr == "" {
                                        continue
                                }
                                filtered = append(filtered, newMsg)
                        } else {
                                filtered = append(filtered, msg)
                        }
                }
                newMsgs = filtered
                // 合并连续同角色消息（移除 tool 消息后可能产生连续 assistant）
                newMsgs = mergeConsecutiveSameRole(newMsgs)
        case 2:
                // 保留最近20条消息，但保留系统消息（如果有）
                const keepRecent = 20
                if len(newMsgs) <= keepRecent {
                        break
                }
                var systemMsg *Message
                if len(newMsgs) > 0 && newMsgs[0].Role == "system" {
                        systemMsg = &newMsgs[0]
                        newMsgs = newMsgs[1:]
                }
                if len(newMsgs) > keepRecent {
                        newMsgs = newMsgs[len(newMsgs)-keepRecent:]
                }
                if systemMsg != nil {
                        newMsgs = append([]Message{*systemMsg}, newMsgs...)
                }
                // 截断后可能产生孤立 tool 消息或非法序列，进行清理
                newMsgs = removeOrphanedToolMessages(newMsgs)
                newMsgs = removeOrphanedToolCalls(newMsgs)
                newMsgs = mergeConsecutiveSameRole(newMsgs)
        }
        return newMsgs
}

// resolveEndpoint 是 URL 拼接的唯一入口。
// 將 baseURL 與 apiPath 拼接為完整的請求 URL。
// 所有 URL 構建邏輯必須經過此函數，確保不會出現雙重拼接。
func resolveEndpoint(baseURL, apiPath string) string {
        return strings.TrimRight(baseURL, "/") + apiPath
}

// sendRequestAndGetChunks 发送请求并返回流式数据块通道
// baseURL: API 基地址（可能被 session 級別覆蓋）
// apiPath: API 路徑（如 /messages、/chat/completions、/chat）
func sendRequestAndGetChunks(ctx context.Context, data map[string]interface{}, baseURL, apiPath, apiKey, apiType string, stream bool) (<-chan StreamChunk, error) {
        // 速率限制：基于 BaseURL 粒度，使用当前全局 API 配置的 RateLimit
        if globalAPIConfig.RateLimit > 0 {
                if err := globalRateLimiter.waitIfNeeded(ctx, globalAPIConfig.BaseURL, globalAPIConfig.RateLimit); err != nil {
                        return nil, fmt.Errorf("rate limit wait cancelled: %w", err)
                }
        }

        // ── P4: 從憑證池獲取 API Key（若池中有可用憑證）───────────
        effectiveAPIKey := apiKey
        activeCredentialID := ""
        if globalCredentialPool != nil && globalCredentialPool.GetHealthyCredentialCount() > 0 {
                if globalCredentialPool.PoolSize() >= 2 {
                        // 多憑證：預取 fallback，減少 429 時的切換延遲
                        best, _, err := globalCredentialPool.GetCredentialWithFallback()
                        if err == nil && best != nil {
                                effectiveAPIKey = best.Key
                                activeCredentialID = best.ID
                        }
                } else {
                        if cred, err := globalCredentialPool.GetCredential(); err == nil && cred != nil {
                                effectiveAPIKey = cred.Key
                                activeCredentialID = cred.ID
                        }
                }
        }

        // ── P3: 使用 Provider Failover（若有多個 Provider 且當前不可用）──
        // effectiveBaseURL：最終使用的 baseURL，默認為 session 傳入的 baseURL，
        // 若 Provider Failover 有可用 provider，則使用 failover provider 的 BaseURL。
        effectiveBaseURL := baseURL
        effectiveKey := effectiveAPIKey
        if globalProviderFailover != nil && globalProviderFailover.ProviderCount() > 0 {
                if active, err := globalProviderFailover.GetActiveProvider(); err == nil && active != nil {
                        effectiveBaseURL = active.BaseURL
                        effectiveKey = active.APIKey
                }
        }

        // 唯一 URL 拼接點：resolveEndpoint(effectiveBaseURL, apiPath)
        effectiveEndpoint := resolveEndpoint(effectiveBaseURL, apiPath)

        resp, err := sendRequest(ctx, data, effectiveEndpoint, effectiveKey, apiType)

        if err != nil {
                // ── P1: 錯誤分類 + Provider Failover 報告失敗 ─────────
                if globalErrorClassifier != nil {
                        classified := globalErrorClassifier.Classify(err)
                        if classified != nil {
                                log.Printf("[ErrorClassifier] Request failed: %s (type: %s)",
                                        err.Error(), ErrorTypeString(classified.Type))
                                // 報告失敗到 Provider Failover
                                if globalProviderFailover != nil {
                                        globalProviderFailover.ReportFailure("default", err)
                                }
                                // 報告失敗到憑證池
                                if globalCredentialPool != nil && activeCredentialID != "" {
                                        globalCredentialPool.ReportFailure(activeCredentialID, err.Error())
                                }
                                // ── P4-RateLimit: 429 時立即切換到另一個憑證重試 ──
                                if classified.Type == ErrorRateLimit && globalCredentialPool != nil && activeCredentialID != "" {
                                        retryAfter := classified.RetryAfter
                                        if retryAfter <= 0 {
                                                retryAfter = globalErrorClassifier.GetRetryDelay(err)
                                        }
                                        globalCredentialPool.ReportRateLimit(activeCredentialID, retryAfter)
                                        log.Printf("[CredentialPool] Rate limit on %s (cooldown %v), switching credential...", MaskAPIKey(activeCredentialID), retryAfter)

                                        maxSwitchAttempts := globalCredentialPool.PoolSize()
                                        for attempt := 0; attempt < maxSwitchAttempts; attempt++ {
                                                altCred, altErr := globalCredentialPool.GetCredentialForRetry(activeCredentialID)
                                                if altErr != nil {
                                                        log.Printf("[CredentialPool] No alternate credential available: %v", altErr)
                                                        break
                                                }
                                                log.Printf("[CredentialPool] Retrying with alternate credential %s (attempt %d/%d)",
                                                        MaskAPIKey(altCred.ID), attempt+1, maxSwitchAttempts)
                                                activeCredentialID = altCred.ID
                                                effectiveKey = altCred.Key
                                                resp, err = sendRequest(ctx, data, effectiveEndpoint, effectiveKey, apiType)
                                                if err == nil {
                                                        // 成功：跳出重試循環，讓下面的成功路徑處理
                                                        break
                                                }
                                                // 檢查新的錯誤是否也是 429
                                                if globalErrorClassifier != nil {
                                                        newClassified := globalErrorClassifier.Classify(err)
                                                        if newClassified != nil && newClassified.Type == ErrorRateLimit {
                                                                newRetryAfter := newClassified.RetryAfter
                                                                if newRetryAfter <= 0 {
                                                                        newRetryAfter = globalErrorClassifier.GetRetryDelay(err)
                                                                }
                                                                globalCredentialPool.ReportRateLimit(activeCredentialID, newRetryAfter)
                                                                log.Printf("[CredentialPool] Alternate credential %s also rate limited (cooldown %v)",
                                                                        MaskAPIKey(activeCredentialID), newRetryAfter)
                                                                continue // 嘗試下一個憑證
                                                        }
                                                }
                                                // 非速率限制錯誤：不再切換憑證，讓下方通用重試邏輯處理
                                                break
                                        }
                                }
                                // 如果仍然有錯誤且可重試，使用分類器的延遲
                                if err != nil && classified.Retryable {
                                        delay := globalErrorClassifier.GetRetryDelay(err)
                                        log.Printf("[ErrorClassifier] Retryable error, retrying after %v...", delay)
                                        select {
                                        case <-ctx.Done():
                                                return nil, ctx.Err()
                                        case <-time.After(delay):
                                        }
                                        // 重試一次
                                        resp, err = sendRequest(ctx, data, effectiveEndpoint, effectiveKey, apiType)
                                        if err != nil {
                                                return nil, err
                                        }
                                }
                        }
                } else {
                        return nil, err
                }
                // ── 錯誤分類完成但仍未成功（classified==nil 或不可重試）──
                if err != nil {
                        return nil, err
                }
                // err == nil 表示重試成功，fall-through 到下方成功路徑
        } else {
                // ── P1/P3/P4: 報告成功 ───────────────────────────────
                if globalProviderFailover != nil {
                        globalProviderFailover.ReportSuccess("default")
                }
                if globalCredentialPool != nil && activeCredentialID != "" {
                        globalCredentialPool.ReportSuccess(activeCredentialID)
                        globalCredentialPool.RecordRequest(activeCredentialID)
                }
        }

        chunkChan := make(chan StreamChunk, 100)

        go func() {
                defer close(chunkChan)
                defer resp.Body.Close()

                if stream {
                        // 流式：直接使用 getStreamChunks 并将数据转发
                        innerChan, err := getStreamChunks(resp.Body, apiType)
                        if err != nil {
                                chunkChan <- StreamChunk{Error: err.Error()}
                                return
                        }
                        chunkCount := 0
                        for chunk := range innerChan {
                                chunkCount++
                                select {
                                case <-ctx.Done():
                                        chunkChan <- StreamChunk{Error: ctx.Err().Error()}
                                        return
                                case chunkChan <- chunk:
                                }
                                if chunk.Done {
                                        break
                                }
                        }
                        if chunkCount == 0 {
                                log.Printf("No stream chunks received from API")
                                chunkChan <- StreamChunk{Error: "no valid stream data received"}
                        }
                } else {
                        // 非流式：读取完整响应，解析后构造一个包含所有内容的块，并标记 Done
                        bodyBytes, err := io.ReadAll(resp.Body)
                        if err != nil {
                                chunkChan <- StreamChunk{Error: err.Error()}
                                return
                        }
                        if IsDebug {
                                debugFile := fmt.Sprintf("debug_response_%d.json", time.Now().Unix())
                                os.WriteFile(debugFile, bodyBytes, 0600)
                                fmt.Printf("Debug response data written to: %s\n", debugFile)
                        }
                        r := bytes.NewReader(bodyBytes)
                        resp.Body = io.NopCloser(r)
                        response, err := handleNonStreamResponse(resp, apiType)
                        if err != nil {
                                chunkChan <- StreamChunk{Error: err.Error()}
                                return
                        }
                        if str, ok := response.Content.(string); ok && str != "" {
                                chunkChan <- StreamChunk{Content: str}
                        }
                        if reasoning, ok := response.ReasoningContent.(string); ok && reasoning != "" {
                                chunkChan <- StreamChunk{ReasoningContent: reasoning}
                        }
                        if toolCalls, ok := response.Content.([]map[string]interface{}); ok {
                                chunkChan <- StreamChunk{ToolCalls: toolCalls}
                        }
                        chunkChan <- StreamChunk{Done: true, FinishReason: response.StopReason}
                }
        }()

        return chunkChan, nil
}

// estimateMessagesTokens 估算消息列表的词元数
func estimateMessagesTokens(messages []Message) int {
        total := 0
        for _, msg := range messages {
                if content, ok := msg.Content.(string); ok {
                        total += EstimateTokens(content)
                }
                if msg.ToolCalls != nil {
                        total += 50 // tool_calls 的额外开销估算
                }
                total += 10 // 每条消息的基础开销
        }
        return total
}

// getModelContextLength 获取模型的上下文长度限制
// 委托给 context_manager.go 的 GetModelContextLengthSafe（统一数据源），
// 确保新模型（如 claude-sonnet-4-6）能正确匹配上下文窗口。
func getModelContextLength(modelID string) int {
        return GetModelContextLengthSafe(modelID)
}

// getMaxOutputTokens 返回模型的最大输出 token 限制（max_tokens 上限）
// 与 getModelContextLength（上下文窗口 = 输入 + 输出）不同，
// 此函数返回的是单次回复允许的最大 token 数。
// 返回 0 表示不限制（使用调用方指定的值）。
func getMaxOutputTokens(modelID string) int {
        // 常见模型的最大输出 token 限制
        maxOutputLimits := map[string]int{
                // OpenAI
                "gpt-4":                  4096,
                "gpt-4-1106-preview":     4096,
                "gpt-4-0125-preview":     4096,
                "gpt-4-0613":             4096,
                "gpt-4-32k":              4096,
                "gpt-4o":                 16384,
                "gpt-4o-mini":            16384,
                "gpt-3.5-turbo":          4096,
                "gpt-3.5-turbo-1106":     4096,
                "gpt-3.5-turbo-0125":     4096,
                "gpt-3.5-turbo-16k":      4096,
                "gpt-3.5-turbo-instruct": 4096,
                // Anthropic
                "claude-3-opus-20240229":  4096,
                "claude-3-sonnet-20240229": 4096,
                "claude-3-haiku-20240307":  4096,
                "claude-2.1":              4096,
                "claude-2":                4096,
                // DeepSeek
                "deepseek-chat":  8192,
                "deepseek-llm":   8192,
                // GLM/智谱 — 注意：GLM-4.6V 输出上限为 32768，而非上下文窗口大小
                "GLM-4.6V":    32768,
                "glm-4.6v":    32768,
                "glm-4v":      4096,
                "GLM-4-Plus":  4096,
                "GLM-4-0520":  4096,
                "glm-4":       4096,
                "glm-3-turbo": 4096,
                "GLM-4-Flash": 4096,
                "GLM-4-Air":  4096,
                "GLM-4-Long": 4096,
        }

        // 检查精确匹配（大小写不敏感）
        lowerID := strings.ToLower(modelID)
        if limit, ok := maxOutputLimits[modelID]; ok {
                return limit
        }
        if limit, ok := maxOutputLimits[lowerID]; ok {
                return limit
        }

        // 检查模糊匹配（模型名称包含关键词）
        for key, limit := range maxOutputLimits {
                if strings.Contains(lowerID, strings.ToLower(key)) {
                        return limit
                }
        }

        // 返回 0 表示不限制，使用调用方指定的值
        return 0
}

// CallModel 调用 LLM API，返回流式数据块通道
// role 参数用于工具权限过滤，为 nil 时返回所有工具
func CallModel(ctx context.Context, messages []Message, apiType, baseURL, apiKey, modelID string,
        temperature float64, maxTokens int, stream bool, thinking bool, role *Role) (<-chan StreamChunk, error) {

        if apiType == "" {
                apiType = DEFAULT_API_TYPE
        }
        if modelID == "" {
                modelID = DEFAULT_MODEL_ID
        }

        // ── Prompt Cache 檢查 + Prompt Loop 偵測 + 存儲 ────────
        // 優化：先計算一次 tokenCount，避免 estimateMessagesTokens 重複調用
        tokenCount := estimateMessagesTokens(messages)

        if globalPromptCache != nil {
                if cached, found := globalPromptCache.Lookup(messages); found {
                        log.Printf("[PromptCache] Cache HIT for %d messages (hit_count: %d)",
                                len(messages), cached.HitCount)
                        // ── Prompt Loop 偵測：相同提示被重複發送 ─────────
                        // 如果同一消息序列被發送超過 3 次，可能是模型陷入死循環
                        if cached.HitCount >= 3 {
                                log.Printf("[PromptCache] WARNING: Same prompt sent %d times consecutively, possible prompt loop detected!",
                                        cached.HitCount)
                                // 返回一個帶有警告的 Done chunk，讓調用者知道
                                errCh := make(chan StreamChunk, 1)
                                close(errCh)
                                return errCh, fmt.Errorf("prompt loop detected: same prompt sent %d times", cached.HitCount)
                        }
                } else {
                        log.Printf("[PromptCache] Cache MISS for %d messages", len(messages))
                }
                // 存儲本次請求的消息到緩存（異步，不阻塞）
                // 優化：直接使用已計算的 tokenCount，避免第二次 estimateMessagesTokens
                go func(msgs []Message, tokCount int) {
                        globalPromptCache.Store(msgs, tokCount)
                }(messages, tokenCount)
        }

        // 获取模型的上下文长度限制
        contextLimit := getModelContextLength(modelID)
        log.Printf("[CallModel] Estimated tokens: %d, Context limit: %d", tokenCount, contextLimit)

        // 检查是否接近或超过上下文限制
        if tokenCount >= contextLimit*9/10 { // 90% 阈值
                // 上下文长度接近或超过限制，尝试自动创建新会话
                return handleContextLengthExceeded(ctx, messages, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking, role)
        }

        // 准备请求数据（初始尝试）
        prepStart := time.Now()
        data, apiPath, resolvedBaseURL, err := prepareRequestData(messages, apiType, baseURL, modelID, temperature, maxTokens, stream, thinking, role)
        log.Printf("[CallModel] prepareRequestData took %v", time.Since(prepStart))
        if err != nil {
                return nil, err
        }

        // 检查请求体大小
        reqBody, _ := json.Marshal(data)
        maxSize := globalAPIConfig.MaxRequestSizeBytes
        if maxSize == 0 || len(reqBody) <= maxSize || IsDebug {
                // 大小合适或调试模式，直接发送
                chunkChan, err := sendRequestAndGetChunks(ctx, data, resolvedBaseURL, apiPath, apiKey, apiType, stream)
                if err != nil {
                        // 检查是否是上下文长度超过限制的错误
                        if strings.Contains(err.Error(), "context_length_exceeded") {
                                // 上下文长度超过限制，尝试自动创建新会话
                                return handleContextLengthExceeded(ctx, messages, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking, role)
                        }
                        return nil, err
                }
                return chunkChan, nil
        }

        // 压缩重试
        compressLevels := []int{0, 1, 2}
        for _, level := range compressLevels {
                compressedMsgs := compressMessages(messages, level)
                // 估算压缩后消息的词元数
                compressedTokenCount := estimateMessagesTokens(compressedMsgs)
                log.Printf("[CallModel] Compressed tokens: %d, Context limit: %d", compressedTokenCount, contextLimit)
                // 检查压缩后是否仍接近或超过上下文限制
                if compressedTokenCount >= contextLimit*9/10 {
                        // 压缩后仍接近或超过限制，尝试自动创建新会话
                        return handleContextLengthExceeded(ctx, compressedMsgs, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking, role)
                }
                data, apiPath, resolvedBaseURL, err = prepareRequestData(compressedMsgs, apiType, baseURL, modelID, temperature, maxTokens, stream, thinking, role)
                if err != nil {
                        continue
                }
                reqBody, _ = json.Marshal(data)
                if maxSize == 0 || len(reqBody) <= maxSize {
                        chunkChan, err := sendRequestAndGetChunks(ctx, data, resolvedBaseURL, apiPath, apiKey, apiType, stream)
                        if err != nil {
                                // 检查是否是上下文长度超过限制的错误
                                if strings.Contains(err.Error(), "context_length_exceeded") {
                                        // 上下文长度超过限制，尝试自动创建新会话
                                        return handleContextLengthExceeded(ctx, compressedMsgs, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking, role)
                                }
                                return nil, err
                        }
                        return chunkChan, nil
                }
        }

        // 所有压缩都失败
        errMsg := fmt.Sprintf("🚫 请求体过大（%d bytes），超过配置限制（%d bytes）。即使经过压缩过滤仍然无法满足大小限制。请考虑：\n"+
                "• 使用 /new 开始新对话\n"+
                "• 减少不必要的工具调用\n"+
                "• 调整配置中的 MaxRequestSizeBytes 值（设置为 0 表示不限制）\n"+
                "任务已停止。", len(reqBody), maxSize)
        log.Printf("[CallModel] Request size still too large after compression: %d > %d", len(reqBody), maxSize)

        errChan := make(chan StreamChunk, 1)
        errChan <- StreamChunk{Error: errMsg, Done: true}
        close(errChan)
        return errChan, nil
}

// handleContextLengthExceeded 处理上下文长度超过限制的情况，自动创建新会话并使用现有压缩策略初始化新会话
func handleContextLengthExceeded(ctx context.Context, messages []Message, apiType, baseURL, apiKey, modelID string,
        temperature float64, maxTokens int, stream bool, thinking bool, role *Role) (<-chan StreamChunk, error) {
        log.Printf("[CallModel] Context length exceeded, creating new session with compressed context")

        // 压缩消息，保留头尾中间摘要
        // 使用最高级别的压缩，确保消息大小在限制范围内
        compressedMsgs := compressMessages(messages, 2)

        // 准备新会话的请求数据
        data, apiPath, resolvedBaseURL, err := prepareRequestData(compressedMsgs, apiType, baseURL, modelID, temperature, maxTokens, stream, thinking, role)
        if err != nil {
                return nil, err
        }

        // 发送请求到新会话
        chunkChan, err := sendRequestAndGetChunks(ctx, data, resolvedBaseURL, apiPath, apiKey, apiType, stream)
        if err != nil {
                return nil, err
        }

        return chunkChan, nil
}

// CallModelSync 同步调用 LLM API，返回完整响应（用于子代理）
func CallModelSync(ctx context.Context, messages []Message, apiType, baseURL, apiKey, modelID string,
        temperature float64, maxTokens int, stream bool, thinking bool) (Response, error) {

        var response Response

        // 使用流式接口但同步等待结果
        chunkChan, err := CallModel(ctx, messages, apiType, baseURL, apiKey, modelID, temperature, maxTokens, false, thinking, nil)
        if err != nil {
                return response, err
        }

        var content strings.Builder
        var reasoning strings.Builder
        var toolCalls []map[string]interface{}
        var finishReason string

        for chunk := range chunkChan {
                if chunk.Error != "" {
                        return response, fmt.Errorf("model error: %s", chunk.Error)
                }
                if chunk.Content != "" {
                        content.WriteString(chunk.Content)
                }
                if chunk.ReasoningContent != "" {
                        reasoning.WriteString(chunk.ReasoningContent)
                }
                if chunk.ToolCalls != nil {
                        toolCalls = chunk.ToolCalls
                }
                if chunk.Done {
                        finishReason = chunk.FinishReason
                        break
                }
        }

        // 构建响应
        if toolCalls != nil {
                response.Content = toolCalls
        } else {
                response.Content = content.String()
        }

        if reasoning.Len() > 0 {
                response.ReasoningContent = reasoning.String()
        }

        response.StopReason = finishReason

        return response, nil
}
