package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

const MaxHistoryMessages = 128

// maxWorkModeResumeRounds 工作模式退出守衛最大續行次數
// 當模型停止但 todo 有未完成項時，程序注入提示強制續行，最多觸發此次數
const maxWorkModeResumeRounds = 3

// maxXMLRePromptRounds XML 工具調用偵測最大重新提示次數
// 防止模型反覆輸出 XML 格式的工具調用導致無限循環
const maxXMLRePromptRounds = 3

// AGENTIC_TAGS 用于前端解析工具调用的标记
const (
	AgenticToolCallStart  = "<<<AGENTIC_TOOL_CALL_START>>>"
	AgenticToolCallEnd    = "<<<AGENTIC_TOOL_CALL_END>>>"
	AgenticToolNamePrefix = "<<<TOOL_NAME:"
	AgenticToolArgsStart  = "<<<TOOL_ARGS_START>>>"
	AgenticToolArgsEnd    = "<<<TOOL_ARGS_END>>>"
	AgenticToolStatusTag  = "<<<TOOL_STATUS:"
	AgenticTagSuffix      = ">>>"
)

// sanitizeContent 清理内容中的非法控制字符
func sanitizeContent(content string) string {
	var builder strings.Builder
	builder.Grow(len(content))

	for _, r := range content {
		switch r {
		case '\n', '\t':
			builder.WriteRune(r)
		case '\r':
			continue
		default:
			if r < 0x20 || r == 0x7F {
				continue
			}
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// sendToolCallStart 发送工具调用开始标记
func sendToolCallStart(ch Channel, toolName string, argsJSON string) {
	var sb strings.Builder
	sb.WriteString(AgenticToolCallStart)
	sb.WriteString("\n")
	sb.WriteString(AgenticToolNamePrefix)
	sb.WriteString(toolName)
	sb.WriteString(AgenticTagSuffix)
	sb.WriteString("\n")
	sb.WriteString(AgenticToolArgsStart)
	sb.WriteString(argsJSON)
	sb.WriteString(AgenticToolArgsEnd)
	sb.WriteString("\n")
	ch.WriteChunk(StreamChunk{Content: sb.String()})
}

// sendToolCallStatus 发送工具调用状态标记（仅在非成功时发送，供前端以警告色渲染）
func sendToolCallStatus(ch Channel, status TaskStatus) {
	if status == TaskStatusFailed || status == TaskStatusCancelled {
		ch.WriteChunk(StreamChunk{Content: AgenticToolStatusTag + string(status) + AgenticTagSuffix + "\n"})
	}
}

// sendToolCallEnd 发送工具调用结束标记
func sendToolCallEnd(ch Channel) {
	ch.WriteChunk(StreamChunk{Content: AgenticToolCallEnd + "\n"})
}

// getCurrentTaskDescriptionFromMessages 从消息历史中提取最后一条用户消息作为任务描述
func getCurrentTaskDescriptionFromMessages(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if content, ok := messages[i].Content.(string); ok && content != "" {
				return content
			}
		}
	}
	return ""
}

func getAllowedToolsList(role *Role) string {
	if role == nil {
		return "所有工具"
	}
	switch role.ToolPermission.Mode {
	case ToolPermissionAll:
		return "所有工具"
	case ToolPermissionAllowlist:
		if len(role.ToolPermission.AllowedTools) == 0 {
			return "无"
		}
		return strings.Join(role.ToolPermission.AllowedTools, ", ")
	case ToolPermissionDenylist:
		return "除 " + strings.Join(role.ToolPermission.DeniedTools, ", ") + " 以外的工具"
	default:
		return "所有工具"
	}
}

// ParsedToolCall 统一的工具调用结构
type ParsedToolCall struct {
	ID       string
	Name     string
	ArgsJSON string
}

// parseToolCallsFromOpenAI 从 OpenAI 格式响应中提取工具调用
func parseToolCallsFromOpenAI(rawToolCalls interface{}) []ParsedToolCall {
	var calls []ParsedToolCall

	switch v := rawToolCalls.(type) {
	case []interface{}:
		for _, item := range v {
			toolUse, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			call := parseSingleOpenAIToolCall(toolUse)
			if call != nil {
				calls = append(calls, *call)
			}
		}
	case []map[string]interface{}:
		for _, toolUse := range v {
			call := parseSingleOpenAIToolCall(toolUse)
			if call != nil {
				calls = append(calls, *call)
			}
		}
	}
	return calls
}

// parseSingleOpenAIToolCall 解析单个 OpenAI 工具调用
func parseSingleOpenAIToolCall(toolUse map[string]interface{}) *ParsedToolCall {
	toolID, ok := toolUse["id"].(string)
	if !ok {
		if idVal, exists := toolUse["id"]; exists {
			toolID = fmt.Sprint(idVal)
		} else {
			return nil
		}
	}
	if toolID == "" {
		return nil
	}

	if toolUse["type"] != "function" {
		return &ParsedToolCall{ID: toolID, Name: "", ArgsJSON: ""}
	}

	function, ok := toolUse["function"].(map[string]interface{})
	if !ok {
		return &ParsedToolCall{ID: toolID, Name: "", ArgsJSON: ""}
	}

	toolName, _ := function["name"].(string)
	argsStr, _ := function["arguments"].(string)

	return &ParsedToolCall{
		ID:       toolID,
		Name:     toolName,
		ArgsJSON: argsStr,
	}
}

// parseToolCallsFromAnthropic 从 Anthropic 格式响应中提取工具调用
func parseToolCallsFromAnthropic(content interface{}) []ParsedToolCall {
	var calls []ParsedToolCall
	contentArray, ok := content.([]interface{})
	if !ok {
		return calls
	}

	for _, item := range contentArray {
		toolUse, ok := item.(map[string]interface{})
		if !ok || toolUse["type"] != "tool_use" {
			continue
		}

		toolName, nameOk := toolUse["name"].(string)
		input, inputOk := toolUse["input"].(map[string]interface{})
		toolID, idOk := toolUse["id"].(string)
		if !idOk {
			if idVal, exists := toolUse["id"]; exists {
				toolID = fmt.Sprint(idVal)
			} else {
				continue
			}
		}
		if !nameOk || !inputOk || toolID == "" {
			continue
		}

		argsJSON, _ := json.Marshal(input)
		calls = append(calls, ParsedToolCall{
			ID:       toolID,
			Name:     toolName,
			ArgsJSON: string(argsJSON),
		})
	}
	return calls
}

// executeSingleToolCall 执行单个工具调用，包含钩子、循环检测
func executeSingleToolCall(ctx context.Context, call ParsedToolCall, ch Channel, role *Role, iteration int) EnrichedMessage {
	// 解析参数
	var argsMap map[string]interface{}
	if err := json.Unmarshal([]byte(call.ArgsJSON), &argsMap); err != nil {
		if IsDebug {
			fmt.Printf("Failed to parse arguments: %v\n", err)
		}
		errMsg := "Error: Failed to parse arguments"
		emitToolCallTags(ch, call.Name, nil, errMsg, TaskStatusFailed)
		return NewToolResultMessage(call.ID, errMsg, TaskStatusFailed, call.Name)
	}

	// 执行前钩子
	hookManager := GetHookManager()
	if hookManager != nil && hookManager.IsEnabled() {
		hookResult := hookManager.RunBeforeTool(ctx, 0, "", iteration, call.Name, argsMap)
		if hookResult.Action == HookOutcomeBlock {
			emitToolCallTags(ch, call.Name, argsMap, hookResult.Reason, TaskStatusFailed)
			return NewToolResultMessage(call.ID, hookResult.Reason, TaskStatusFailed, call.Name)
		} else if hookResult.Action == HookOutcomeModify && hookResult.ModifiedInput != nil {
			argsMap = hookResult.ModifiedInput
		}
	}

	// 执行工具
	result := SafeExecuteTool(ctx, call.ID, call.Name, argsMap, ch, role)

	// 循环检测
	contentStr, _ := result.Content.(string)
	isErr := result.Meta.Status == TaskStatusFailed
	if loopResult := CheckLoop(call.Name, argsMap, contentStr, isErr); loopResult != nil {
		// 主动学习：注入历史经验
		if globalUnifiedMemory != nil {
			exps := globalUnifiedMemory.RetrieveExperiences(call.Name, 2)
			if len(exps) > 0 {
				var expMsg strings.Builder
				expMsg.WriteString("\n\n## 📚 历史经验参考\n")
				for _, exp := range exps {
					expMsg.WriteString(fmt.Sprintf("- %s (评分: %.2f)\n", exp.Summary, exp.Score))
				}
				expMsg.WriteString("建议参考上述成功经验，避免重复错误。")
				loopResult.WarningMessage += expMsg.String()
			}
		}
		if loopResult.ShouldInterrupt {
			errMsg := fmt.Sprintf("\n\n🚫 %s\n\n任务已被系统终止，因为检测到重复循环。", loopResult.WarningMessage)
			ch.WriteChunk(StreamChunk{Error: errMsg})
			return NewToolResultMessage(call.ID, errMsg, TaskStatusFailed, call.Name)
		}
		// 否则只添加警告
		contentStr = contentStr + "\n\n" + loopResult.WarningMessage
		if loopResult.Suggestion != "" {
			contentStr = contentStr + "\n\n💡 建议：" + loopResult.Suggestion
		}
		result.Content = contentStr
		log.Printf("[AgentLoop] Loop detected: %s (count: %d)", call.Name, loopResult.LoopCount)
	}

	// 执行后钩子
	if hookManager != nil && hookManager.IsEnabled() {
		contentStr, _ := result.Content.(string)
		toolResultInfo := &ToolResultInfo{
			Content: contentStr,
			IsError: result.Meta.Status == TaskStatusFailed,
		}
		hookResult := hookManager.RunAfterTool(ctx, 0, "", iteration, call.Name, argsMap, toolResultInfo)
		if hookResult.Action == HookOutcomeBlock {
			emitToolCallTags(ch, call.Name, argsMap, hookResult.Reason, TaskStatusFailed)
			return NewToolResultMessage(call.ID, hookResult.Reason, TaskStatusFailed, call.Name)
		} else if hookResult.Action == HookOutcomeModify {
			if warning, ok := hookResult.Patch["warning"].(string); ok {
				contentStr = contentStr + "\n\n" + warning
				result.Content = contentStr
			}
		}
	}

	return result
}

// ============================================================================
// AgentLoop — 核心循環（排程器架構）
// ============================================================================
//
// 依賴組件（按執行順序）：
//   - loop_setup.go:    Pre-loop 設置（記憶注入、模型配置、意圖分類、系統提示）
//   - loop_safety.go:   迭代安全檢查（ctx.Done、最大迭代、警告）
//   - loop_plan.go:     Plan Mode 自動提醒與超時檢查
//   - loop_wake.go:     即時喚醒通知注入
//   - loop_history.go:  自適應歷史壓縮
//   - loop_call.go:     CallModel 封裝（hooks + API + 流式累積）
//   - loop_branch_none.go: 無工具調用分支（XML/AutoSwitch/WorkGuard/SubagentGuard）
//   - loop_branch_tool.go: 工具調用執行
//   - loop_escalate.go:    錯誤升級檢測
//   - loop_tool_after.go:  工具執行後處理
//   - loop_post.go:     Post-loop 清理（反饋/Token/日誌/學習）
//   - scheduler.go:     排程器核心（ReadyQueue/TCB/優先級）

func AgentLoop(ctx context.Context, ch Channel, messages []Message, apiType, baseURL, apiKey, modelID string,
	temperature float64, maxTokens int, stream bool, thinking bool) ([]Message, error) {

	// ========== Phase 1: Pre-loop 設置 ==========
	messages, config := RunPreLoopSetup(ctx, messages, apiType, baseURL, apiKey, modelID, temperature, maxTokens)

	// 建立排程器並註冊任務
	sched := NewScheduler()
	registerLoopTasks(sched)

	// ========== Phase 2: 迭代主循環 ==========
	iteration := 0
	resumeCount := 0
	subagentResumeCount := 0
	xmlRePromptCount := 0
	todoReminderCount := 0
	loopExitedNaturally := false
	var lastTokenUsage *TokenUsage

	for {
		iteration++

		// ---- 安全檢查 (P0: CRITICAL) ----
		if stop, err := RunSafetyCheck(ctx, ch, iteration); stop {
			if err != nil {
				return messages, err
			}
			return messages, nil
		}

		// ---- Plan Mode 檢查 (P1: HIGH) ----
		RunPlanModeChecks(&messages, iteration)

		// ---- 喚醒通知注入 (P1: HIGH) ----
		RunWakeInjection(&messages, iteration)

		// ---- 歷史壓縮 (P3: LOW) ----
		messages = RunHistoryCompression(messages, config.EffectiveModelID, config.Compressor)

		// ---- CallModel (P2: NORMAL) ----
		callResult, err := RunCallModel(ctx, &messages, ch,
			config.EffectiveAPIType, config.EffectiveBaseURL, config.EffectiveAPIKey,
			config.EffectiveModelID, config.EffectiveTemperature, config.EffectiveMaxTokens,
			stream, thinking, config.CurrentRole, iteration)
		if err != nil {
			return messages, err
		}
		if callResult.LastTokenUsage != nil {
			lastTokenUsage = callResult.LastTokenUsage
		}
		_ = sched // 排程器用於狀態追蹤

		// ---- 分支：無工具調用 vs 有工具調用 ----
		if !isToolUseStopReason(callResult.StopReason) {
			// Branch A: 無工具調用
			branchResult := RunBranchNone(messages, callResult.RespContent,
				callResult.ReasoningContent, callResult.ThinkingSignature,
				&xmlRePromptCount, &resumeCount, &subagentResumeCount,
				&todoReminderCount, &loopExitedNaturally,
				ch, iteration, config.EffectiveMaxTokens)

			messages = branchResult.Messages
			if branchResult.ShouldContinue {
				continue
			}
			if branchResult.ShouldBreak {
				break
			}
		} else {
			// Branch B: 有工具調用
			results := RunBranchTool(ctx, callResult.ToolCalls, ch, config.CurrentRole, iteration)
			if len(results) == 0 {
				continue
			}

			// ---- 錯誤升級檢測 (P1: HIGH) ----
			if RunEscalateCheck(&messages, results, callResult.ToolCalls) {
				continue
			}

			// ---- 工具執行後處理 (P3: LOW) ----
			RunAfterToolExec(&messages, results, ch)
		}
	}

	// ========== Phase 3: Post-loop 清理 ==========
	RunPostLoop(ch, messages, iteration, loopExitedNaturally, lastTokenUsage,
		config.EffectiveModelID, config.EffectiveAPIType, config.EffectiveBaseURL, config.EffectiveAPIKey)

	return messages, nil
}

// registerLoopTasks 註冊排程器任務（用於未來非同步排程擴展）
func registerLoopTasks(sched *Scheduler) {
	// 預留：當 handler 改為非同步模型時，透過 Scheduler.Tick() 進行優先級排程
	// 目前所有步驟以同步函數調用方式執行，排程器負責狀態追蹤
	_ = sched
}
