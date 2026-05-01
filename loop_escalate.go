package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

// ============================================================================
// loop_escalate.go — 錯誤升級檢測
// ============================================================================
// 從 AgentLoop L1271-1317 抽出：
//   - Sentinel prefix 檢測（__ESCALATE__:）
//   - 重複工具失敗模式匹配
//   - 用戶消息注入

// RunEscalateCheck checks tool results for error escalation signals.
// Returns true if escalation was triggered (caller should continue the loop).
func RunEscalateCheck(messages *[]Message, results []EnrichedMessage, toolCalls []map[string]interface{}) bool {
	var escalateInjected bool

	for i, result := range results {
		contentStr, _ := result.Content.(string)

		// (1) Sentinel prefix 檢測
		if strings.HasPrefix(contentStr, escalatePrefix) {
			userMsg := strings.TrimPrefix(contentStr, escalatePrefix)
			*messages = append(*messages, Message{
				Role:      "user",
				Content:   userMsg,
				Timestamp: time.Now().Unix(),
			})
			log.Printf("[AgentLoop] Error escalated via sentinel: injecting user message")
			escalateInjected = true
			break
		}

		// (2) 重複工具失敗檢測
		if result.Meta.Status == TaskStatusFailed && result.Meta.ToolName != "" {
			errorKey := result.Meta.ToolName
			parsedCalls := parseToolCallsFromOpenAI(toolCalls)
			if i < len(parsedCalls) && parsedCalls[i].ArgsJSON != "" {
				var argsMap map[string]interface{}
				if json.Unmarshal([]byte(parsedCalls[i].ArgsJSON), &argsMap) == nil {
					errorKey = generateFingerprint(result.Meta.ToolName, argsMap)
				}
			}
			shouldStop, userMsg := globalErrorEscalator.RecordEscalation(
				EscalateRepeatedFailure, errorKey, contentStr,
			)
			if shouldStop {
				*messages = append(*messages, Message{
					Role:      "user",
					Content:   userMsg,
					Timestamp: time.Now().Unix(),
				})
				log.Printf("[AgentLoop] Repeated tool failure escalated: tool=%s", errorKey)
				escalateInjected = true
				break
			}
		}
	}

	return escalateInjected
}
