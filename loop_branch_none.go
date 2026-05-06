package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// lastWorkModeTodoDigest 記錄上一次 exit guard 檢查時嘅 todos 指紋。
// 用於進展感知 resume：指紋變咗 = 有進展 → reset counter；指紋唔變 = 卡死 → counter++。
var lastWorkModeTodoDigest string

// turnsSinceLastTaskTool 距離上次調用 task 工具（TodoWrite/TodoCreate/TodoUpdate/TodoList）嘅 turns 數。
// executeSingleToolCall 喺每次 task 工具調用時重置為 0；RunBranchNone 每 turn +1。
// 用於 Stale Task Reminder：>3 turns 冇用 task 工具 → inject reminder。
var turnsSinceLastTaskTool int

// turnsSinceLastReminder 距離上次 inject stale reminder 嘅 turns 數。
// 避免連續 inject：>5 turns 先會再次 remind。
var turnsSinceLastReminder int

// getMaxWorkModeResumeRounds returns the max resume rounds for work mode exit guard.
// Default is 3 if not configured.
func getMaxWorkModeResumeRounds() int {
	if globalConfig.MaxWorkModeResumeRounds > 0 {
		return globalConfig.MaxWorkModeResumeRounds
	}
	return 3
}

// ============================================================================
// loop_branch_none.go — Branch A: 無工具調用時的分支邏輯
// ============================================================================
// 從 AgentLoop L1054-1235 抽出：
//   - XML 工具調用偵測 + 直接解析執行（不再 re-prompt）
//   - Auto-switch 標記處理
//   - 工作模式退出守衛（基於 todo 狀態）
//   - 子代理運行守衛
//   - todos 使用提醒守衛

// BranchNoneResult holds the result of the no-tool-calls branch logic.
type BranchNoneResult struct {
	ShouldContinue bool     // continue the main loop
	ShouldBreak    bool     // break out of the main loop
	Messages       []Message // (possibly) modified messages
}

// RunBranchNone handles the branch when the model returned no tool calls.
func RunBranchNone(messages []Message, respContent interface{},
	reasoningContent, thinkingSignature string,
	xmlRePromptCount *int, resumeCount *int, subagentResumeCount *int,
	todoReminderCount *int, loopExitedNaturally *bool,
	ch Channel, iteration int, effectiveMaxTokens int) BranchNoneResult {

	contentStr, _ := respContent.(string)

	// ========== Inline XML 工具調用偵測 + 直接解析執行 ==========
	// 當模型以文字形式輸出 XML/DSML 格式工具調用（非原生 API tool_calls），
	// 唔再 re-prompt，而係直接 parse 出 tool name + args，行正常執行路徑。
	// 支援格式：<invoke>, <tool_call>, <DSML_invoke>, <DSML_tool_calls>
	if contentStr != "" && detectXMLToolInvocation(contentStr) {
		parsedCalls := parseInlineXMLToolCalls(contentStr)
		if len(parsedCalls) > 0 && *xmlRePromptCount < maxXMLRePromptRounds {
			*xmlRePromptCount++
			log.Printf("[AgentLoop] Parsed %d inline XML tool call(s) from text response (%d/%d)",
				len(parsedCalls), *xmlRePromptCount, maxXMLRePromptRounds)

			for _, call := range parsedCalls {
				result := executeSingleToolCall(context.TODO(), call, ch, nil, iteration)
				messages = append(messages, Message{
					Role:       "tool",
					Content:    result.Content,
					ToolCallID: call.ID,
					Timestamp:  time.Now().Unix(),
				})
			}
			return BranchNoneResult{ShouldContinue: true, Messages: messages}
		}
	}

	// ========== Auto-switch 標記處理 ==========
	if globalStage != nil && globalStage.AutoSwitchEnabled() {
		hasMarker, targetActor, isEnd := ParseSwitchMarker(contentStr)

		if hasMarker && !isEnd && targetActor != "" && globalStage.CanAutoSwitch() {
			if _, ok := globalActorManager.GetActor(targetActor); ok {
				cleanedContent := StripSwitchMarker(contentStr)

				messages[len(messages)-1] = Message{
					Role:              "assistant",
					Content:           cleanedContent,
					ReasoningContent:  reasoningContent,
					ThinkingSignature: thinkingSignature,
				}

				globalStage.SetCurrentActor(targetActor)
				turns := globalStage.IncrementAutoTurns()

				switchMsg := fmt.Sprintf("\n═══════════════════════════════════════════════════════════════\n[Auto Switch → %s | Turns: %d/%d]\n═══════════════════════════════════════════════════════════════\n", targetActor, turns, 20)
				ch.WriteChunk(StreamChunk{Content: switchMsg})

				newMessages := make([]Message, 0)
				for _, msg := range messages {
					if msg.Role != "system" {
						newMessages = append(newMessages, msg)
					}
				}

				modelCtxWindow := GetModelContextLengthSafe("")
				newSystemPrompt := BuildAdaptiveSystemPrompt(targetActor, globalActorManager, globalRoleManager, globalStage, modelCtxWindow, 0, 0, effectiveMaxTokens)
				newMessages = append([]Message{{Role: "system", Content: newSystemPrompt}}, newMessages...)

				if globalUnifiedMemory != nil {
					memoryContext := globalUnifiedMemory.GetContextForPrompt("")
					fencedBlock := BuildMemoryContextBlock(memoryContext)
					if fencedBlock != "" {
						userInsertIdx := -1
						for i := len(newMessages) - 1; i >= 0; i-- {
							if newMessages[i].Role == "user" {
								userInsertIdx = i
								break
							}
						}
						if userInsertIdx >= 0 {
							memMsg := Message{Role: "system", Content: fencedBlock}
							newMessages = append(newMessages[:userInsertIdx], append([]Message{memMsg}, newMessages[userInsertIdx:]...)...)
						}
					}
				}
				messages = newMessages
				return BranchNoneResult{ShouldContinue: true, Messages: messages}
			}
		} else if isEnd {
			ch.WriteChunk(StreamChunk{Content: "\n═══════════════════════════════════════════════════════════════\n[Auto Stopped: END marker]\n═══════════════════════════════════════════════════════════════\n"})
			cleanedContent := StripSwitchMarker(contentStr)
			messages[len(messages)-1] = Message{
				Role:              "assistant",
				Content:           cleanedContent,
				ReasoningContent:  reasoningContent,
				ThinkingSignature: thinkingSignature,
			}
		}
	}

	// ========== Stale Task Reminder（仿 Claude Code） ==========
	// 模型已有一段時間冇用 task 工具 → inject reminder 連同當前任務列表
	// 同 todoReminderCount（強制規劃）唔同：呢個係溫和提醒，只喺已有任務時觸發
	if !TODO.IsEmpty() && turnsSinceLastTaskTool >= 3 && turnsSinceLastReminder >= 5 {
		turnsSinceLastReminder = 0
		currentTasks := TODO.Render()
		reminderMsg := fmt.Sprintf(
			"[SYSTEM_REMINDER] 你已有一段時間未更新任務狀態。以下係當前任務列表：\n\n%s\n\n如有任務已完成或狀態有變，請使用 TodoWrite 更新。",
			currentTasks,
		)
		messages = append(messages, Message{
			Role:      "user",
			Content:   reminderMsg,
			Timestamp: time.Now().Unix(),
		})
		log.Printf("[AgentLoop] Stale task reminder injected (turns=%d, tasks_present=true)", turnsSinceLastTaskTool)
		return BranchNoneResult{ShouldContinue: true, Messages: messages}
	}

	// 追蹤距離上次 task 工具調用嘅 turns
	turnsSinceLastTaskTool++
	turnsSinceLastReminder++

	// ========== 工作模式退出守衛（進展感知） ==========
	// 每次 resume 前比較 todos 指紋：有進展（指紋變咗）→ reset counter；
	// 冇進展（指紋一樣）→ counter++。卡死先強制退出，唔係計 resume 次數。
	if TODO.HasUnfinishedItems() {
		if !TODO.AllUnfinishedAreWaiting() {
			currentDigest := TODO.GetUnfinishedDigest()
			if currentDigest != lastWorkModeTodoDigest {
				// 有進展：reset counter，更新 snapshot
				*resumeCount = 0
				lastWorkModeTodoDigest = currentDigest
			}
			*resumeCount++
			if *resumeCount <= getMaxWorkModeResumeRounds() {
				unfinished := TODO.GetUnfinishedSummary()
				resumePrompt := fmt.Sprintf(
					"[SYSTEM_RESUME] 你的任務尚未完成。以下待辦事項仍需處理：\n%s\n\n請繼續執行未完成的任務。如果某個任務已通過 SmartShell（異步模式）或 CronAdd 提交為後台操作，請使用 todos 工具將其狀態更新為 waiting，然後等待系統通知結果，切勿重複調用同步模式。",
					unfinished,
				)
				messages = append(messages, Message{
					Role:      "user",
					Content:   resumePrompt,
					Timestamp: time.Now().Unix(),
				})
				log.Printf("[AgentLoop] Work mode exit guard: resume #%d (consecutive no-progress=%d), unfinished todos detected", *resumeCount, *resumeCount)
				return BranchNoneResult{ShouldContinue: true, Messages: messages}
			}
			log.Printf("[AgentLoop] Work mode: max consecutive no-progress rounds (%d) reached, allowing exit", getMaxWorkModeResumeRounds())
		} else {
			log.Printf("[AgentLoop] Work mode: all remaining todos are waiting, allowing exit")
		}
	}

	// ========== 子代理運行守衛 ==========
	if globalSubagentManager != nil {
		var runningSubagentIDs []string
		for _, task := range globalSubagentManager.List() {
			task.mu.RLock()
			if task.Status == SubagentRunning {
				runningSubagentIDs = append(runningSubagentIDs, task.ID)
			}
			task.mu.RUnlock()
		}
		if len(runningSubagentIDs) > 0 && *subagentResumeCount < getMaxWorkModeResumeRounds() {
			*subagentResumeCount++
			resumePrompt := fmt.Sprintf(
				"[SYSTEM_RESUME] 你有 %d 個子代理仍在後台運行（%s）。\n"+
					"請繼續使用 SpawnCheck 檢查它們的進度，直到所有子代理完成。\n"+
					"不要回覆文字給用戶，繼續執行工具調用。",
				len(runningSubagentIDs), strings.Join(runningSubagentIDs, ", "))
			messages = append(messages, Message{
				Role:      "user",
				Content:   resumePrompt,
				Timestamp: time.Now().Unix(),
			})
			log.Printf("[AgentLoop] Subagent running guard: resume #%d, %d subagents still running: %v", *subagentResumeCount, len(runningSubagentIDs), runningSubagentIDs)
			return BranchNoneResult{ShouldContinue: true, Messages: messages}
		} else if len(runningSubagentIDs) > 0 {
			log.Printf("[AgentLoop] Subagent running guard: max resume rounds (%d) reached, allowing exit despite %d running subagents", getMaxWorkModeResumeRounds(), len(runningSubagentIDs))
		}
	}

	// ========== todos 使用提醒守衛 ==========
	// 工作模式 + 未規劃 → 死循環，強制模型使用 Todos 或 EnterPlanMode
	// plan mode active 時唔觸發 — 模型已選擇 EnterPlanMode 路徑
	planModeActive := globalTasksMode != nil && globalTasksMode.IsActive()
	if globalTaskTracker != nil && globalTaskTracker.IsWorkMode() && TODO.IsEmpty() && !planModeActive {
		*todoReminderCount++
		reminderHint := "[SYSTEM_REMINDER] 你正處於工作模式但尚未規劃任務。\n" +
			"你可以繼續使用讀取/搜索類工具蒐集資訊，但必須使用 Todos 或 EnterPlanMode 進行規劃。\n" +
			"系統已攔截寫入/執行類工具，完成規劃後先會放行。"
		messages = append(messages, Message{
			Role:      "user",
			Content:   reminderHint,
			Timestamp: time.Now().Unix(),
		})
		log.Printf("[AgentLoop] Todos reminder #%d injected (indefinite guard)", *todoReminderCount)
		return BranchNoneResult{ShouldContinue: true, Messages: messages}
	}

	*loopExitedNaturally = true
	return BranchNoneResult{ShouldBreak: true, Messages: messages}
}

// parseInlineXMLToolCalls 從文字內容中提取 XML/DSML 格式嘅工具調用，
// 轉換為 ParsedToolCall 格式，兼容現有工具執行路徑。
// 支援格式：
//   <invoke name="ToolName"><parameter name="k">v</parameter></invoke>
//   <DSML_invoke name="ToolName"><DSML_parameter name="k">v</DSML_parameter></DSML_invoke>
func parseInlineXMLToolCalls(content string) []ParsedToolCall {
	var calls []ParsedToolCall

	// 提取所有 invoke / DSML_invoke block（(?s) 令 . 匹配換行）
	invokePattern := regexp.MustCompile(`(?s)<(?:DSML_)?invoke\s+name\s*=\s*["']([^"']+)["'][^>]*>(.*?)</(?:DSML_)?invoke>`)

	// 如果內容包含 tool_calls / DSML_tool_calls wrapper，只 parse wrapper 內部
	toolCallPattern := regexp.MustCompile(`(?s)<(?:DSML_)?tool_calls[^>]*>(.*?)</(?:DSML_)?tool_calls>`)
	var matches [][]string
	if tcMatches := toolCallPattern.FindStringSubmatch(content); len(tcMatches) > 1 {
		matches = invokePattern.FindAllStringSubmatch(tcMatches[1], -1)
	} else {
		matches = invokePattern.FindAllStringSubmatch(content, -1)
	}

	for i, match := range matches {
		if len(match) < 3 {
			continue
		}
		toolName := strings.TrimSpace(match[1])
		paramBlock := match[2]

		// parse parameters
		args := make(map[string]interface{})
		paramPattern := regexp.MustCompile(`(?s)<(?:DSML_)?parameter\s+name\s*=\s*["']([^"']+)["'][^>]*>(.*?)</(?:DSML_)?parameter>`)
		paramMatches := paramPattern.FindAllStringSubmatch(paramBlock, -1)
		for _, pm := range paramMatches {
			if len(pm) >= 3 {
				key := strings.TrimSpace(pm[1])
				val := strings.TrimSpace(pm[2])
				// 嘗試解析 JSON value
				var jsonVal interface{}
				if err := json.Unmarshal([]byte(val), &jsonVal); err == nil {
					args[key] = jsonVal
				} else {
					args[key] = val
				}
			}
		}

		argsJSON, _ := json.Marshal(args)
		calls = append(calls, ParsedToolCall{
			ID:       fmt.Sprintf("inline_xml_%d", i),
			Name:     toolName,
			ArgsJSON: string(argsJSON),
		})
	}

	return calls
}

// detectXMLToolInvocation 检測模型文本回覆中是否包含 XML 格式的工具調用
func detectXMLToolInvocation(content string) bool {
	checkContent := content
	runes := []rune(checkContent)
	if len(runes) > 500 {
		checkContent = string(runes[:500])
	}
	lower := strings.ToLower(checkContent)

	if strings.Contains(lower, "<invoke") && strings.Contains(lower, "name=") {
		for toolName := range toolRegistryMap {
			lt := strings.ToLower(toolName)
			if strings.Contains(lower, "name=\""+lt+"\"") || strings.Contains(lower, "name='"+lt+"'") {
				return true
			}
		}
	}

	// DSML-prefixed variants (<DSML_invoke name="ToolName"> / <DSML_tool_calls>)
	if (strings.Contains(lower, "<dsml_invoke") || strings.Contains(lower, "<dsml_tool_call")) && strings.Contains(lower, "name=") {
		for toolName := range toolRegistryMap {
			lt := strings.ToLower(toolName)
			if strings.Contains(lower, "name=\""+lt+"\"") || strings.Contains(lower, "name='"+lt+"'") {
				return true
			}
		}
		// Even without a specific known tool name, any DSML invoke/tool_call block should be caught
		return true
	}

	if strings.Contains(lower, "<tool_call") && strings.Contains(lower, "name=") {
		for toolName := range toolRegistryMap {
			lt := strings.ToLower(toolName)
			if strings.Contains(lower, "name=\""+lt+"\"") || strings.Contains(lower, "name='"+lt+"'") {
				return true
			}
		}
	}

	if strings.Contains(lower, "<function_call>") {
		return true
	}

	if strings.Contains(lower, "<parameter") && strings.Contains(lower, "</parameter") &&
		strings.Contains(lower, "name=") &&
		(strings.Contains(lower, "command") || strings.Contains(lower, "filename") ||
			strings.Contains(lower, "query") || strings.Contains(lower, "url") ||
			strings.Contains(lower, "content") || strings.Contains(lower, "path")) {
		return true
	}

	return false
}
