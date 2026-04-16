package main

import (
        "bufio"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "os"
        "strings"
        "time"
)

// StreamChunk 流式响应块，添加 JSON tag 以便前端使用小写字段名
type StreamChunk struct {
        Content          string                   `json:"content"`
        ToolCalls        []map[string]interface{} `json:"tool_calls,omitempty"`
        Done             bool                     `json:"done"`
        Error            string                   `json:"error,omitempty"`
        FinishReason     string                   `json:"finish_reason,omitempty"`
        ReasoningContent string                   `json:"reasoning_content,omitempty"`
        SessionID        string                   `json:"session_id,omitempty"`    // 会话 ID
        TaskRunning      bool                     `json:"task_running,omitempty"`  // 任务是否在运行
        HistorySync      []Message                `json:"history_sync,omitempty"`  // 重连时同步的历史消息

        // Token 使用量（僅在 Done=true 時填充，來自 API 響應的 usage 字段）
        Usage *TokenUsage `json:"usage,omitempty"`
}

// getStreamChunks 从响应体中获取流式响应块，根据 apiType 选择解析方式
func getStreamChunks(body io.ReadCloser, apiType string) (<-chan StreamChunk, error) {
        chunkChan := make(chan StreamChunk, 100)

        go func() {
                defer close(chunkChan)
                defer body.Close()

                var debugLines []string
                scanner := bufio.NewScanner(body)
                scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // 10MB max

                // 根据 apiType 选择解析模式
                var parser *anthropicSSEState // Anthropic 狀態機，scanner error 時需 flush
                switch apiType {
                case "openai":
                        // OpenAI SSE 模式：处理 data: 前缀的行
                        for scanner.Scan() {
                                line := scanner.Text()
                                if IsDebug {
                                        debugLines = append(debugLines, line)
                                }

                                if strings.HasPrefix(line, "data:") {
                                        data := strings.TrimPrefix(line, "data:")
                                        data = strings.TrimSpace(data)

                                        if data == "[DONE]" {
                                                chunkChan <- StreamChunk{Done: true}
                                                saveDebugLines(debugLines)
                                                return
                                        }

                                        // 解析 JSON
                                        var response map[string]interface{}
                                        if err := json.Unmarshal([]byte(data), &response); err != nil {
                                                log.Printf("Failed to parse SSE JSON: %v, line: %s", err, line)
                                                chunkChan <- StreamChunk{Error: fmt.Sprintf("parse error: %v", err)}
                                                continue
                                        }

                                        chunk := parseSSEChunk(response)
                                        chunkChan <- chunk
                                        if chunk.Done {
                                                saveDebugLines(debugLines)
                                                return
                                        }
                                } else if line != "" {
                                        // SSE 规范中 event:、id:、retry: 等字段是合法的，不打印警告
                                        if !(strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") || strings.HasPrefix(line, ":")) {
                                                log.Printf("Unexpected SSE line: %s", line)
                                        }
                                }
                        }

                case "anthropic":
                        // Anthropic 原生 SSE 格式：event: + data: 成对出现
                        // 需要狀態機處理 tool_use（跨多個 chunk 累積 JSON 參數）
                        var currentEvent string
                        parser = &anthropicSSEState{chunkChan: chunkChan}
                        for scanner.Scan() {
                                line := scanner.Text()
                                if IsDebug {
                                        debugLines = append(debugLines, line)
                                }

                                // 空行跳过（SSE 事件分隔符）
                                if line == "" {
                                        currentEvent = ""
                                        continue
                                }

                                if strings.HasPrefix(line, "event:") {
                                        currentEvent = strings.TrimPrefix(line, "event:")
                                        currentEvent = strings.TrimSpace(currentEvent)
                                        continue
                                }

                                if strings.HasPrefix(line, "data:") {
                                        data := strings.TrimPrefix(line, "data:")
                                        data = strings.TrimSpace(data)

                                        if data == "[DONE]" {
                                                // 先 flush 累積的 tool calls
                                                parser.flushToolCall()
                                                chunkChan <- StreamChunk{Done: true}
                                                saveDebugLines(debugLines)
                                                return
                                        }

                                        var response map[string]interface{}
                                        if err := json.Unmarshal([]byte(data), &response); err != nil {
                                                log.Printf("Failed to parse Anthropic SSE JSON: %v, line: %s", err, line)
                                                chunkChan <- StreamChunk{Error: fmt.Sprintf("parse error: %v", err)}
                                                continue
                                        }

                                        done := parser.process(response, currentEvent)
                                        if done {
                                                saveDebugLines(debugLines)
                                                return
                                        }
                                        continue
                                }

                                // 其他非標準行忽略
                        }

                case "ollama":
                        // Ollama 模式：每一行都是一个完整的 JSON 对象
                        for scanner.Scan() {
                                line := scanner.Text()
                                if IsDebug {
                                        debugLines = append(debugLines, line)
                                }
                                if line == "" {
                                        continue
                                }

                                var ollamaChunk struct {
                                        Message struct {
                                                Content string `json:"content"`
                                        } `json:"message"`
                                        Done bool `json:"done"`
                                }

                                if err := json.Unmarshal([]byte(line), &ollamaChunk); err != nil {
                                        log.Printf("Failed to parse Ollama JSON: %v, line: %s", err, line)
                                        chunkChan <- StreamChunk{Error: fmt.Sprintf("parse error: %v", err)}
                                        continue
                                }

                                chunk := StreamChunk{
                                        Content: ollamaChunk.Message.Content,
                                        Done:    ollamaChunk.Done,
                                }
                                chunkChan <- chunk
                                if ollamaChunk.Done {
                                        saveDebugLines(debugLines)
                                        return
                                }
                        }

                default:
                        chunkChan <- StreamChunk{Error: fmt.Sprintf("unsupported API type for streaming: %s", apiType)}
                        return
                }

                if err := scanner.Err(); err != nil {
                        // 連接被代理或遠端中斷是常見情況（尤其在第三方代理環境下），
                        // 不需要打印完整堆棧，簡潔提示即可。
                        log.Printf("[StreamChunk] Scanner error (%s): %v", apiType, err)
                        // Anthropic 模式下可能還有未完成的 tool call，flush 它們
                        if apiType == "anthropic" && parser != nil {
                                parser.flushToolCall()
                        }
                        chunkChan <- StreamChunk{Error: fmt.Sprintf("stream connection lost: %v", err)}
                }
        }()

        return chunkChan, nil
}

// parseSSEChunk 解析 OpenAI 格式的 SSE JSON 块
func parseSSEChunk(response map[string]interface{}) StreamChunk {
        chunk := StreamChunk{}

        if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
                choice := choices[0]
                if choiceMap, ok := choice.(map[string]interface{}); ok {
                        if delta, ok := choiceMap["delta"].(map[string]interface{}); ok {
                                if content, ok := delta["content"].(string); ok {
                                        chunk.Content = content
                                }
                                if reasoningContent, ok := delta["reasoning_content"].(string); ok {
                                        chunk.ReasoningContent = reasoningContent
                                }
                                if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
                                        var tcs []map[string]interface{}
                                        for _, tc := range toolCalls {
                                                if tcMap, ok := tc.(map[string]interface{}); ok {
                                                        if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                                                if args, ok := function["arguments"]; ok {
                                                                        if argsMap, ok := args.(map[string]interface{}); ok {
                                                                                if argsJSON, err := json.Marshal(argsMap); err == nil {
                                                                                        function["arguments"] = string(argsJSON)
                                                                                }
                                                                        }
                                                                }
                                                        }
                                                        tcs = append(tcs, tcMap)
                                                }
                                        }
                                        chunk.ToolCalls = tcs
                                }
                        }
                        if finishReason, ok := choiceMap["finish_reason"].(string); ok && finishReason != "" {
                                chunk.Done = true
                                chunk.FinishReason = finishReason
                        }
                }
        }

        // 提取 token usage（OpenAI/兼容 API 在最後一個 chunk 返回 usage）
        chunk.Usage = extractAPIUsageFromResponse(response)

        return chunk
}

// extractAPIUsageFromResponse 從 API 響應中提取 token 使用量
func extractAPIUsageFromResponse(response map[string]interface{}) *TokenUsage {
        usageRaw, ok := response["usage"]
        if !ok || usageRaw == nil {
                return nil
        }
        usageMap, ok := usageRaw.(map[string]interface{})
        if !ok {
                return nil
        }
        promptTokens := intFromInterface(usageMap["prompt_tokens"])
        completionTokens := intFromInterface(usageMap["completion_tokens"])
        totalTokens := intFromInterface(usageMap["total_tokens"])
        if promptTokens == 0 && completionTokens == 0 && totalTokens == 0 {
                return nil
        }
        return &TokenUsage{
                PromptTokens:     promptTokens,
                CompletionTokens: completionTokens,
                TotalTokens:      totalTokens,
        }
}

// intFromInterface 安全地將 interface{} 轉為 int（支持 float64 和 int）
// 使用不同的函數名避免與 lisp_eval.go 的 toInt(types.MalType) 衝突
func intFromInterface(v interface{}) int {
        if v == nil {
                return 0
        }
        switch val := v.(type) {
        case float64:
                return int(val)
        case int:
                return val
        case int64:
                return int(val)
        default:
                return 0
        }
}

// anthropicSSEState 維護 Anthropic SSE 流式解析的狀態機
// 用於跨多個 chunk 累積 tool_use 的 JSON 參數
type anthropicSSEState struct {
        chunkChan    chan StreamChunk
        inToolUse    bool
        toolID       string
        toolName     string
        argsBuilder  strings.Builder
}

// process 處理一個 Anthropic SSE data JSON 對象
// 返回 true 表示流結束（message_stop）
func (s *anthropicSSEState) process(response map[string]interface{}, eventType string) bool {
        eventType = coalesce(eventType, toString(response["type"]))

        switch eventType {
        case "message_start":

        case "content_block_start":
                if contentBlock, ok := response["content_block"].(map[string]interface{}); ok {
                        if toString(contentBlock["type"]) == "tool_use" {
                                s.inToolUse = true
                                s.toolID = toString(contentBlock["id"])
                                s.toolName = toString(contentBlock["name"])
                                s.argsBuilder.Reset()
                        }
                }

        case "content_block_delta":
                if delta, ok := response["delta"].(map[string]interface{}); ok {
                        switch toString(delta["type"]) {
                        case "text_delta":
                                if text, ok := delta["text"].(string); ok {
                                        s.chunkChan <- StreamChunk{Content: text}
                                }
                        case "thinking_delta":
                                if thinking, ok := delta["thinking"].(string); ok {
                                        s.chunkChan <- StreamChunk{ReasoningContent: thinking}
                                }
                        case "input_json_delta":
                                if partialJSON, ok := delta["partial_json"].(string); ok {
                                        s.argsBuilder.WriteString(partialJSON)
                                }
                        }
                }

        case "content_block_stop":
                if s.inToolUse {
                        s.flushToolCall()
                }

        case "message_delta":
                if delta, ok := response["delta"].(map[string]interface{}); ok {
                        stopReason := toString(delta["stop_reason"])
                        if stopReason != "" {
                                s.flushToolCall()
                                if stopReason == "tool_use" {
                                        s.chunkChan <- StreamChunk{Done: true, FinishReason: "function_call"}
                                } else {
                                        s.chunkChan <- StreamChunk{Done: true, FinishReason: stopReason}
                                }
                        }
                }

        case "message_stop":
                s.flushToolCall()
                s.chunkChan <- StreamChunk{Done: true, FinishReason: "stop"}
                return true

        case "ping":

        case "error":
                if errMsg, ok := response["error"].(map[string]interface{}); ok {
                        s.chunkChan <- StreamChunk{Error: fmt.Sprintf("Anthropic API error: %v", errMsg)}
                }
        }

        return false
}

// flushToolCall 將累積的 tool call 作為完整 chunk 發送
func (s *anthropicSSEState) flushToolCall() {
        if !s.inToolUse || s.toolName == "" {
                s.inToolUse = false
                return
        }

        argsStr := s.argsBuilder.String()
        var args interface{} = argsStr
        if argsStr != "" {
                var parsed map[string]interface{}
                if json.Unmarshal([]byte(argsStr), &parsed) == nil {
                        args = parsed
                }
        }

        toolCall := map[string]interface{}{
                "id":   s.toolID,
                "type": "function",
                "function": map[string]interface{}{
                        "name":      s.toolName,
                        "arguments": args,
                },
        }
        s.chunkChan <- StreamChunk{ToolCalls: []map[string]interface{}{toolCall}}

        s.inToolUse = false
        s.toolID = ""
        s.toolName = ""
        s.argsBuilder.Reset()
}

// toString 安全地將 interface{} 轉為 string
func toString(v interface{}) string {
        if v == nil {
                return ""
        }
        if s, ok := v.(string); ok {
                return s
        }
        return fmt.Sprintf("%v", v)
}

// coalesce 返回第一個非空字符串
func coalesce(a, b string) string {
        if a != "" {
                return a
        }
        return b
}

// saveDebugLines 保存调试行到文件
func saveDebugLines(lines []string) {
        if IsDebug && len(lines) > 0 {
                debugFile := fmt.Sprintf("debug_stream_response_%d.json", time.Now().Unix())
                debugContent := strings.Join(lines, "\n")
                if err := os.WriteFile(debugFile, []byte(debugContent), 0644); err == nil {
                        fmt.Printf("Debug stream response data written to: %s\n", debugFile)
                }
        }
}
