package main

import (
	"fmt"
	"log"
	"time"
)

// ============================================================================
// loop_plan.go — Tasks Mode 自動提醒與超時檢查
// ============================================================================

// RunPlanModeChecks performs Tasks Mode suggestion and timeout checks.
// Modifies messages in place.
//
// 前缀缓存说明（inx 字节级前缀缓存移植）：
// 注入使用 appendBeforeLatestUser 插到「最新 user 消息之前」而不是 prepend 到头部，
// 避免破坏 system + tools + 早期历史的前缀字节。
// 注意：若最新 user 是第一条 user 消息（工具循环中常见），本次注入会使插入点
// 之后的历史字节一次性错位（该轮 cache miss），但此后消息在插入点之后继续
// append-only 增长，后续轮次前缀稳定——属低频一次性代价，换取模型能及时看到提醒。
func RunPlanModeChecks(messages *[]Message, iteration int) {
	tasksActive := globalTasksMode != nil && globalTasksMode.IsActive()

	// Tasks Mode 自動提醒（僅在第 4 輪迭代時注入）
	if iteration == 4 && !tasksActive {
		log.Printf("[AgentLoop] Tasks Mode suggestion: iteration=%d, tasks mode inactive", iteration)
		*messages = appendBeforeLatestUser(*messages, Message{
			Role:    "system",
			Content: "[系统提示] 当前任务已进行多轮工具调用。如果任务复杂、涉及多文件修改或需要仔细规划，建议使用 Tasks 工具进入结构化任务分解模式（Tasks({\"PlanPhase\": \"explore\"}) 先探索 → Tasks({\"PlanPhase\": \"design\"}) 再設計 → Tasks({\"PlanPhase\": \"execute\"}) 退出執行）。",
		})
	}

	// Tasks Mode 超時檢查
	if tasksActive {
		if timedOut, phaseElapsed, totalElapsed := checkTasksTimeout(); timedOut {
			content := forceExitTasks(fmt.Sprintf("phase elapsed=%v, total elapsed=%v", phaseElapsed, totalElapsed))
			timeoutMsg := fmt.Sprintf("[系統通知] Tasks Mode 已因超時自動退出（階段耗時 %v，總耗時 %v）。\n\n", phaseElapsed.Round(time.Second), totalElapsed.Round(time.Second))
			if content != "" {
				timeoutMsg += fmt.Sprintf("已完成的計劃內容：\n\n%s\n\n", content)
			}
			timeoutMsg += "你可以直接使用所有工具來執行任務。"
			*messages = appendBeforeLatestUser(*messages, Message{
				Role:    "system",
				Content: timeoutMsg,
			})
			log.Printf("[AgentLoop] Tasks Mode timed out, forced exit (phase=%v, total=%v)", phaseElapsed, totalElapsed)
		}
	}
}

// appendBeforeLatestUser 把 msg 插入到 messages 中最新一条 user 消息之前。
// 若没有 user 消息则追加到尾部。返回新切片，不修改原切片。
// 用途：所有「指令性 system 提示」都应在最新 user 轮附近注入，
// 避免 prepend 到头部破坏 provider prompt cache 前缀。
func appendBeforeLatestUser(messages []Message, msg Message) []Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			out := make([]Message, 0, len(messages)+1)
			out = append(out, messages[:i]...)
			out = append(out, msg)
			out = append(out, messages[i:]...)
			return out
		}
	}
	return append(messages, msg)
}
