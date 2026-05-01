package main

import (
	"log"
	"time"
)

// ============================================================================
// loop_tool_after.go — 工具執行後處理
// ============================================================================
// 從 AgentLoop L1318-1361 抽出：
//   - 工具結果追加到消息歷史
//   - Task tracker 更新
//   - Memory consolidator 記錄
//   - Todos 提示

// RunAfterToolExec appends tool results to history and updates trackers.
func RunAfterToolExec(messages *[]Message, results []EnrichedMessage, ch Channel) {
	// 將工具結果添加到消息歷史
	for _, result := range results {
		*messages = append(*messages, result.ToAPIMessage())

		if globalTaskTracker != nil {
			contentStr, _ := result.Content.(string)
			globalTaskTracker.RecordToolCall(
				result.Meta.ToolName,
				result.Meta.Status,
				"",
				TruncateString(contentStr, 100),
			)
		}
	}

	// 記錄工具消息到記憶整合器
	if globalMemoryConsolidator != nil {
		for _, result := range results {
			contentStr, _ := result.Content.(string)
			globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
				Role:      "tool",
				Content:   contentStr,
				Timestamp: time.Now(),
			})
		}
	}

	// Todos 提示
	if globalTaskTracker != nil {
		shouldPrompt, promptMsg := globalTaskTracker.ShouldPromptTodo()
		if shouldPrompt && promptMsg != "" {
			*messages = append(*messages, Message{
				Role:      "user",
				Content:   promptMsg,
				Timestamp: time.Now().Unix(),
			})
		}
	}

	if IsDebug {
		log.Printf("Number of messages after tool execution: %d", len(*messages))
		for i, msg := range *messages {
			log.Printf("Message %d: Role=%s, Content=%v, ToolCallID=%s", i, msg.Role, msg.Content, msg.ToolCallID)
		}
	}
}
