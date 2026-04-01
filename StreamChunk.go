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
                switch apiType {
                case "openai", "anthropic":
                        // SSE 模式：处理 data: 前缀的行
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
                                                // 解析失败，可能是非标准格式，发送错误块
                                                log.Printf("Failed to parse SSE JSON: %v, line: %s", err, line)
                                                chunkChan <- StreamChunk{Error: fmt.Sprintf("parse error: %v", err)}
                                                continue
                                        }

                                        chunk := parseSSEChunk(response, apiType)
                                        chunkChan <- chunk
                                        if chunk.Done {
                                                saveDebugLines(debugLines)
                                                return
                                        }
                                } else if line != "" {
                                        // 非 data 开头的行，可能是错误信息或空行
                                        log.Printf("Unexpected SSE line: %s", line)
                                }
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
                        log.Printf("Scanner error: %v", err)
                        chunkChan <- StreamChunk{Error: fmt.Sprintf("scanner error: %v", err)}
                }
        }()

        return chunkChan, nil
}

// parseSSEChunk 解析 SSE 格式的 JSON 块，支持 OpenAI 和 Anthropic
func parseSSEChunk(response map[string]interface{}, apiType string) StreamChunk {
        chunk := StreamChunk{}

        if apiType == "openai" {
                // OpenAI 格式
                if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
                        choice := choices[0]
                        if choiceMap, ok := choice.(map[string]interface{}); ok {
                                // 提取 delta 内容
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
                                                                // 确保 arguments 是字符串格式
                                                                // 某些 API（如 MiniMax）可能返回对象而不是字符串
                                                                if function, ok := tcMap["function"].(map[string]interface{}); ok {
                                                                        if args, ok := function["arguments"]; ok {
                                                                                // 如果 arguments 是 map，转换为 JSON 字符串
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
                                // 检查结束标记
                                if finishReason, ok := choiceMap["finish_reason"].(string); ok && finishReason != "" {
                                        chunk.Done = true
                                        chunk.FinishReason = finishReason
                                }
                        }
                }
        } else if apiType == "anthropic" {
                // Anthropic 格式
                if typ, ok := response["type"].(string); ok {
                        switch typ {
                        case "content_block_delta":
                                if delta, ok := response["delta"].(map[string]interface{}); ok {
                                        // 处理文本内容
                                        if text, ok := delta["text"].(string); ok {
                                                chunk.Content = text
                                        }
                                        // 处理思考内容（Anthropic thinking_delta）
                                        if thinking, ok := delta["thinking"].(string); ok {
                                                chunk.ReasoningContent = thinking
                                        }
                                }
                        case "content_block_start":
                                // 内容块开始，可能包含 thinking 类型
                                // 不需要特殊处理，后续会有 delta
                        case "message_stop":
                                chunk.Done = true
                                chunk.FinishReason = "stop"
                        }
                }
        }

        return chunk
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
