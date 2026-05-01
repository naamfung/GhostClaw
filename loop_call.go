package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// ============================================================================
// loop_call.go — CallModel 封裝器
// ============================================================================
// 從 AgentLoop L875-1052 抽出：
//   - before-model hooks
//   - CallModel 調用
//   - 流式 chunk 累積
//   - tool_calls 合併
//   - assistant 消息追加到歷史

// CallModelResult holds the result of a CallModel invocation.
type CallModelResult struct {
	RespContent       interface{}
	ReasoningContent  string
	ThinkingSignature string
	ToolCalls         []map[string]interface{}
	StopReason        string
	LastTokenUsage    *TokenUsage
}

// RunCallModel invokes the model API and processes the streaming response.
// Appends the assistant message to messages on success.
func RunCallModel(ctx context.Context, messages *[]Message, ch Channel,
	effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID string,
	effectiveTemperature float64, effectiveMaxTokens int,
	stream, thinking bool, currentRole *Role, iteration int) (*CallModelResult, error) {

	hookManager := GetHookManager()
	if hookManager != nil && hookManager.IsEnabled() {
		hookResult := hookManager.RunBeforeModel(ctx, 0, "", iteration, "", len(*messages), 0)
		if hookResult.Action == HookOutcomeBlock {
			ch.WriteChunk(StreamChunk{Content: hookResult.Reason, Done: true})
			return nil, fmt.Errorf("blocked by hook: %s", hookResult.Reason)
		}
	}

	chunkChan, err := CallModel(ctx, *messages, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, effectiveTemperature, effectiveMaxTokens, stream, thinking, currentRole)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			if writeErr := ch.WriteChunk(StreamChunk{Error: err.Error()}); writeErr != nil {
				log.Printf("Failed to write error chunk: %v", writeErr)
			}
		} else {
			log.Printf("[AgentLoop] CallModel cancelled/timeout, skipping error chunk")
		}
		return nil, err
	}

	var respContent interface{}
	var reasoningContent string
	var thinkingSignature string
	var toolCalls []map[string]interface{}
	var stopReason string
	var lastTokenUsage *TokenUsage
	toolCallsMap := make(map[int]map[string]interface{})

	for chunk := range chunkChan {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if chunk.Error != "" {
			if writeErr := ch.WriteChunk(chunk); writeErr != nil {
				log.Printf("Failed to write error chunk: %v", writeErr)
				return nil, fmt.Errorf("%s", chunk.Error)
			}
			return nil, fmt.Errorf("%s", chunk.Error)
		}

		chunkToSend := chunk
		chunkToSend.Done = false
		if writeErr := ch.WriteChunk(chunkToSend); writeErr != nil {
			log.Printf("WebSocket write failed: %v, stopping AgentLoop", writeErr)
			return nil, writeErr
		}

		if chunk.Content != "" {
			if str, ok := respContent.(string); ok {
				respContent = str + chunk.Content
			} else {
				respContent = chunk.Content
			}
		}
		if chunk.ReasoningContent != "" {
			reasoningContent += chunk.ReasoningContent
		}
		if chunk.ThinkingSignature != "" {
			thinkingSignature = chunk.ThinkingSignature
		}
		if len(chunk.ToolCalls) > 0 {
			for _, tc := range chunk.ToolCalls {
				idx := 0
				if idxFloat, ok := tc["index"].(float64); ok {
					idx = int(idxFloat)
				} else if idxInt, ok := tc["index"].(int); ok {
					idx = idxInt
				}

				existing, exists := toolCallsMap[idx]
				if !exists {
					existing = make(map[string]interface{})
					toolCallsMap[idx] = existing
				}

				for k, v := range tc {
					if k == "function" {
						funcMap, ok := v.(map[string]interface{})
						if !ok {
							existing[k] = v
							continue
						}
						existingFunc, funcOk := existing["function"].(map[string]interface{})
						if !funcOk {
							existingFunc = make(map[string]interface{})
							existing["function"] = existingFunc
						}
						for fk, fv := range funcMap {
							if fk == "arguments" {
								if argStr, ok := fv.(string); ok {
									if existingArgs, argsOk := existingFunc["arguments"].(string); argsOk {
										existingFunc["arguments"] = existingArgs + argStr
									} else {
										existingFunc["arguments"] = argStr
									}
								} else if argMap, ok := fv.(map[string]interface{}); ok {
									if j, err := json.Marshal(argMap); err == nil {
										existingFunc["arguments"] = string(j)
									}
								}
							} else {
								existingFunc[fk] = fv
							}
						}
					} else {
						if v != nil {
							if str, ok := v.(string); ok && str == "" {
								continue
							}
							existing[k] = v
						}
					}
				}
			}
		}
		if chunk.Done {
			stopReason = chunk.FinishReason
			if chunk.Usage != nil {
				lastTokenUsage = chunk.Usage
			}
			break
		}
	}

	// 發送 final Done 信號，令 CMD 等頻道可以 flush 緩衝區並換行
	ch.WriteChunk(StreamChunk{Done: true})

	// 對完整累積內容行一次 applyReplacements，而非逐 chunk 處理
	// 確保跨 chunk 邊界嘅 replacement key 都能正確匹配
	if str, ok := respContent.(string); ok && str != "" {
		respContent = applyReplacements(str)
	}
	// ReasoningContent 同理
	if reasoningContent != "" {
		reasoningContent = applyReplacements(reasoningContent)
	}

	if len(toolCallsMap) > 0 {
		maxIdx := 0
		for idx := range toolCallsMap {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		toolCalls = make([]map[string]interface{}, 0, maxIdx+1)
		for i := 0; i <= maxIdx; i++ {
			if tc, exists := toolCallsMap[i]; exists {
				delete(tc, "index")
				toolCalls = append(toolCalls, tc)
			}
		}
	}

	// 將助手消息加入歷史
	if stopReason == "tool_use" || stopReason == "function_call" || stopReason == "tool_calls" {
		*messages = append(*messages, Message{
			Role:              "assistant",
			ToolCalls:         toolCalls,
			Content:           respContent,
			ReasoningContent:  reasoningContent,
			ThinkingSignature: thinkingSignature,
			Timestamp:         time.Now().Unix(),
		})
	} else {
		*messages = append(*messages, Message{
			Role:              "assistant",
			Content:           respContent,
			ReasoningContent:  reasoningContent,
			ThinkingSignature: thinkingSignature,
			Timestamp:         time.Now().Unix(),
		})
	}

	// 記錄助手消息到記憶整合器
	if globalMemoryConsolidator != nil {
		contentStr, _ := respContent.(string)
		globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
			Role:      "assistant",
			Content:   contentStr,
			Timestamp: time.Now(),
		})
	}

	return &CallModelResult{
		RespContent:       respContent,
		ReasoningContent:  reasoningContent,
		ThinkingSignature: thinkingSignature,
		ToolCalls:         toolCalls,
		StopReason:        stopReason,
		LastTokenUsage:    lastTokenUsage,
	}, nil
}

// isToolUseStopReason is a helper to check if stop reason indicates tool calls
func isToolUseStopReason(stopReason string) bool {
	return stopReason == "tool_use" || stopReason == "function_call" || stopReason == "tool_calls"
}
