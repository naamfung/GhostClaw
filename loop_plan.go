package main

import (
	"fmt"
	"log"
	"time"
)

// ============================================================================
// loop_plan.go — Plan Mode 自動提醒與超時檢查
// ============================================================================
// 從 AgentLoop L576-604 抽出：
//   - iteration=4 時注入 Plan Mode 建議
//   - Plan Mode 單階段/總時間超時強制退出

// RunPlanModeChecks performs plan mode suggestion and timeout checks.
// Modifies messages in place.
func RunPlanModeChecks(messages *[]Message, iteration int) {
	// Plan Mode 自動提醒（僅在第 4 輪迭代時注入，且 plan mode 未激活）
	// 僅在規劃模式已啟用（配置開關打開）時才提醒
	if iteration == 4 && globalPlanMode != nil && !globalPlanMode.IsActive() {
		log.Printf("[AgentLoop] Plan Mode suggestion: iteration=%d, plan mode inactive", iteration)
		*messages = append([]Message{{
			Role:    "system",
			Content: "[系统提示] 当前任务已进行多轮工具调用。如果任务复杂、涉及多文件修改或需要仔细规划，建议使用 EnterPlanMode 工具进入结构化任务分解模式，先探索再执行。",
		}}, *messages...)
	}

	// Plan Mode 超時檢查
	if globalPlanMode != nil && globalPlanMode.IsActive() {
		if timedOut, phaseElapsed, totalElapsed := globalPlanMode.CheckPhaseTimeout(); timedOut {
			planContent := ForceExitPlanMode(fmt.Sprintf("phase elapsed=%v, total elapsed=%v", phaseElapsed, totalElapsed))
			timeoutMsg := fmt.Sprintf("[系統通知] Plan Mode 已因超時自動退出（階段耗時 %v，總耗時 %v）。\n\n", phaseElapsed.Round(time.Second), totalElapsed.Round(time.Second))
			if planContent != "" {
				timeoutMsg += fmt.Sprintf("已完成的計劃內容將作為參考：\n\n%s\n\n", planContent)
			}
			timeoutMsg += "你可以直接使用所有工具來執行任務。"
			*messages = append([]Message{{
				Role:    "system",
				Content: timeoutMsg,
			}}, *messages...)
			log.Printf("[AgentLoop] Plan Mode timed out, forced exit (phase=%v, total=%v)", phaseElapsed, totalElapsed)
		}
	}
}
