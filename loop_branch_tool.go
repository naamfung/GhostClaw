package main

import (
	"context"
	"log"
)

// ============================================================================
// loop_branch_tool.go — Branch B: 工具調用執行
// ============================================================================
// 從 AgentLoop L1237-1269 抽出：
//   - 解析 tool calls
//   - 逐一執行 executeSingleToolCall
//   - 收集結果

// RunBranchTool executes all tool calls from the model response.
func RunBranchTool(ctx context.Context, toolCalls []map[string]interface{},
	ch Channel, currentRole *Role, iteration int) []EnrichedMessage {

	// 所有 API 類型的工具調用均已由流式/非流式處理器統一轉為 OpenAI 兼容格式
	parsedCalls := parseToolCallsFromOpenAI(toolCalls)

	if len(parsedCalls) == 0 {
		if IsDebug {
			log.Printf("Warning: no tool calls to process")
		}
		return nil
	}

	var results []EnrichedMessage
	for _, call := range parsedCalls {
		select {
		case <-ctx.Done():
			log.Printf("[AgentLoop] Context cancelled, stopping tool execution")
			return results
		default:
		}

		if call.Name == "" {
			errMsg := "Error: Invalid tool type or missing function"
			emitToolCallTags(ch, "unknown", nil, errMsg, TaskStatusFailed)
			results = append(results, NewToolResultMessage(call.ID, errMsg, TaskStatusFailed, ""))
			continue
		}

		result := executeSingleToolCall(ctx, call, ch, currentRole, iteration)
		results = append(results, result)
	}

	return results
}
