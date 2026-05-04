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
func RunPlanModeChecks(messages *[]Message, iteration int) {
	tasksActive := globalTasksMode != nil && globalTasksMode.IsActive()

	// Tasks Mode 自動提醒（僅在第 4 輪迭代時注入）
	if iteration == 4 && !tasksActive {
		log.Printf("[AgentLoop] Tasks Mode suggestion: iteration=%d, tasks mode inactive", iteration)
		*messages = append([]Message{{
			Role:    "system",
			Content: "[系统提示] 当前任务已进行多轮工具调用。如果任务复杂、涉及多文件修改或需要仔细规划，建议使用 Tasks 工具进入结构化任务分解模式（Tasks(PlanPhase=\"explore\") 先探索 → Tasks(PlanPhase=\"design\") 再設計 → Tasks(PlanPhase=\"execute\") 退出執行）。",
		}}, *messages...)
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
			*messages = append([]Message{{
				Role:    "system",
				Content: timeoutMsg,
			}}, *messages...)
			log.Printf("[AgentLoop] Tasks Mode timed out, forced exit (phase=%v, total=%v)", phaseElapsed, totalElapsed)
		}
	}
}
