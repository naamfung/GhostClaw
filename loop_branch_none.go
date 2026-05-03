package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// ============================================================================
// loop_branch_none.go — Branch A: 無工具調用時的分支邏輯
// ============================================================================
// 從 AgentLoop L1054-1235 抽出：
//   - XML 工具調用偵測 + 重新提示
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
// It manages XML re-prompt, auto-switch, work mode exit guard,
// subagent running guard, and todos reminder.
func RunBranchNone(messages []Message, respContent interface{},
	reasoningContent, thinkingSignature string,
	xmlRePromptCount *int, resumeCount *int, subagentResumeCount *int,
	todoReminderCount *int, loopExitedNaturally *bool,
	ch Channel, iteration int, effectiveMaxTokens int) BranchNoneResult {

	contentStr, _ := respContent.(string)

	// ========== XML 工具調用偵測 ==========
	if contentStr != "" && detectXMLToolInvocation(contentStr) {
		*xmlRePromptCount++
		if *xmlRePromptCount > maxXMLRePromptRounds {
			log.Printf("[AgentLoop] XML re-prompt limit reached (%d), stopping re-prompt", *xmlRePromptCount)
		} else {
			log.Printf("[AgentLoop] Detected XML tool invocation in text response, re-prompting model (%d/%d)", *xmlRePromptCount, maxXMLRePromptRounds)
			rePromptMsg := "[系统提示] 你的回复包含了 XML 格式的工具调用，但该工具当前不可用或未被正确识别。" +
				"请使用下方工具列表中可用的工具，通过标准的 tool_calls 机制调用。" +
				"不要在文本中手动编写工具调用 XML。如果需要的工具不在可用列表中，" +
				"请使用其他可用工具完成任务，或向用户说明情况。"
			messages = append(messages, Message{
				Role:      "user",
				Content:   rePromptMsg,
				Timestamp: time.Now().Unix(),
			})
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

	// ========== 工作模式退出守衛 ==========
	if TODO.HasUnfinishedItems() {
		if !TODO.AllUnfinishedAreWaiting() {
			if *resumeCount < maxWorkModeResumeRounds {
				*resumeCount++
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
				log.Printf("[AgentLoop] Work mode exit guard: resume #%d, unfinished todos detected", *resumeCount)
				return BranchNoneResult{ShouldContinue: true, Messages: messages}
			}
			log.Printf("[AgentLoop] Work mode: max resume rounds (%d) reached, allowing exit", maxWorkModeResumeRounds)
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
		if len(runningSubagentIDs) > 0 && *subagentResumeCount < maxWorkModeResumeRounds {
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
			log.Printf("[AgentLoop] Subagent running guard: max resume rounds (%d) reached, allowing exit despite %d running subagents", maxWorkModeResumeRounds, len(runningSubagentIDs))
		}
	}

	// ========== todos 使用提醒守衛 ==========
	if globalTaskTracker != nil && globalTaskTracker.IsWorkMode() && TODO.IsEmpty() && *todoReminderCount < 2 {
		*todoReminderCount++
		reminderHint := fmt.Sprintf(
			"[SYSTEM_REMINDER] 你正處於工作模式但尚未使用 todos 工具規劃任務。\n"+
				"請使用 todos 將任務分解為可追蹤的子步驟。\n"+
				"如果任務非常簡單（1 步驟可完成），請直接執行並完成。\n"+
				"(此提醒不會再出現超過 %d 次)",
			2-*todoReminderCount,
		)
		messages = append(messages, Message{
			Role:      "user",
			Content:   reminderHint,
			Timestamp: time.Now().Unix(),
		})
		log.Printf("[AgentLoop] Todos reminder #%d injected", *todoReminderCount)
		return BranchNoneResult{ShouldContinue: true, Messages: messages}
	}

	*loopExitedNaturally = true
	return BranchNoneResult{ShouldBreak: true, Messages: messages}
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
