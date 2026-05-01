package main

import (
	"context"
	"log"
)

// ============================================================================
// loop_safety.go — 迭代安全檢查
// ============================================================================
// 從 AgentLoop L559-574 抽出：
//   - ctx.Done() 上下文取消檢查
//   - ShouldForceStop 最大迭代次數檢查
//   - globalLoopWarningInjector 迭代警告注入

// RunSafetyCheck performs iteration safety checks.
// Returns (shouldStop, error). If shouldStop is true, the caller should exit the loop.
func RunSafetyCheck(ctx context.Context, ch Channel, iteration int) (bool, error) {
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	default:
	}

	if ShouldForceStop(iteration) {
		log.Printf("[AgentLoop] 達到最大迭代次數 %d，強制停止", MaxAgentLoopIterations)
		ch.WriteChunk(StreamChunk{Content: GetIterationWarningMessage(iteration), Done: true})
		return true, nil
	}

	if globalLoopWarningInjector.ShouldInjectWarning(iteration) {
		log.Printf("[AgentLoop] 迭代警告: iteration=%d", iteration)
		ch.WriteChunk(StreamChunk{Content: GetIterationWarningMessage(iteration), Done: false})
	}

	return false, nil
}
