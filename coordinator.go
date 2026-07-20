package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"unicode/utf8"
)

// ============================================================================
// coordinator.go — Planner + Executor 双模型模式
// ============================================================================
// 参考 DeepSeek-Reasonix 的 coordinator.go 设计。
// Planner 和 Executor 各自维护独立 session，前缀缓存互不干扰：
//   - Planner：只读工具集（小 Kap），生成简洁计划，不执行副作用
//   - Executor：完整工具集，执行 planner 的计划
//
// 启用方式：config.Tools.CoordinatorEnabled = true
// Planner 模型：config.Tools.CoordinatorPlannerModel（空=使用当前模型）
//
// 集成点：AgentLoop Phase 1 之前调用 RunPlannerPhase
// ============================================================================

// plannerSystemPrompt 引导 planner 产出简洁可执行的计划
const plannerSystemPrompt = `你是双模型 Agent 中的规划者（Planner）。
给定用户任务，产出一个简洁、有序的执行计划供执行者（Executor）实施。
你可以使用只读工具（如读取文件、搜索）获取任务所需的上下文，但不要执行任何写操作或副作用。
不要询问用户如何触发执行者。输出执行者可直接使用的指令：做什么、涉及哪些文件或命令、预期障碍、关键决策。
保持简洁可执行。

如果研究显示任务无需任何更改或操作（已实现、已解决），简要说明并以 [no_changes] 结尾。
如果执行需要用户先批准计划，最后一行写 [planner_requires_approval]。`

// executorHandoffMarker 标记交接给 executor 的消息
const executorHandoffMarker = "GhostClaw Executor Handoff"

// CoordinatorState 管理 planner session 的全局状态
type CoordinatorState struct {
	mu              sync.Mutex
	plannerMessages []Message // planner 独立的消息链
	enabled         bool      // 是否已初始化
}

var globalCoordinatorState = &CoordinatorState{}

// IsCoordinatorEnabled 检查是否启用了 Coordinator 模式
func IsCoordinatorEnabled() bool {
	return globalToolsConfig.CoordinatorEnabled
}

// ResetPlannerSession 重置 planner session（新对话开始时调用）
func ResetPlannerSession() {
	globalCoordinatorState.mu.Lock()
	defer globalCoordinatorState.mu.Unlock()
	globalCoordinatorState.plannerMessages = nil
	globalCoordinatorState.enabled = true
	log.Printf("[Coordinator] Planner session reset")
}

// extractLatestUserInput 从 executor messages 中提取最新用户输入
func extractLatestUserInput(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if s, ok := messages[i].Content.(string); ok {
				return s
			}
		}
	}
	return ""
}

// RunPlannerPhase 运行 planner 阶段，返回计划文本
// 如果 Coordinator 未启用或 planner 失败，返回空字符串
// 成功时把计划注入到 executor messages 的最新 user 消息之前
func RunPlannerPhase(ctx context.Context, messages []Message, config *AgentLoopConfig) (string, bool) {
	if !IsCoordinatorEnabled() {
		return "", false
	}

	// 提取最新用户输入
	userInput := extractLatestUserInput(messages)
	if strings.TrimSpace(userInput) == "" {
		return "", false
	}

	// 跳过 trivial 输入（问候、单字等），避免浪费 planner 调用
	if shouldSkipPlanner(userInput) {
		log.Printf("[Coordinator] Skipping planner for trivial input")
		return "", false
	}

	log.Printf("[Coordinator] Planner phase starting (input: %d chars)", len(userInput))

	// 隔离 PrefixShape 跟踪：planner 调用前后保存/恢复 executor 的 tracker 状态，
	// 避免 planner 的 system/tools 变化污染 executor 的前缀诊断数据
	savedShape, savedStep := SavePrefixShapeTracker()
	defer RestorePrefixShapeTracker(savedShape, savedStep)

	// 构建 planner 请求
	plannerModel := globalToolsConfig.CoordinatorPlannerModel
	if plannerModel == "" {
		plannerModel = config.EffectiveModelID
	}

	// planner 的 system prompt
	plannerSysMsg := Message{
		Role:    "system",
		Content: plannerSystemPrompt,
	}

	// planner 的消息链：system + 历史 planner turns + 当前用户输入
	globalCoordinatorState.mu.Lock()
	plannerMsgs := make([]Message, 0, len(globalCoordinatorState.plannerMessages)+2)
	plannerMsgs = append(plannerMsgs, plannerSysMsg)
	plannerMsgs = append(plannerMsgs, globalCoordinatorState.plannerMessages...)
	plannerMsgs = append(plannerMsgs, Message{
		Role:    "user",
		Content: userInput,
	})
	globalCoordinatorState.mu.Unlock()

	// 调用 planner（非流式，不传 role → CallModel 不会附加工具）
	// planner 不需要工具，设 maxTokens 较小让它专注输出计划
	planCtx, planCancel := context.WithTimeout(ctx, 10*60*1e9) // 10 分钟
	defer planCancel()

	resp, err := CallModelSync(
		planCtx,
		plannerMsgs,
		config.EffectiveAPIType,
		config.EffectiveBaseURL,
		config.EffectiveAPIKey,
		plannerModel,
		config.EffectiveTemperature,
		2048,  // planner 输出限制
		false, // 不流式
		false, // 不思考
	)
	if err != nil {
		log.Printf("[Coordinator] Planner failed: %v, falling back to executor-only", err)
		return "", false
	}

	plan := extractResponseContent(resp)
	if strings.TrimSpace(plan) == "" {
		log.Printf("[Coordinator] Planner returned empty plan, falling back to executor-only")
		return "", false
	}

	// 检查 no_changes 标记
	if isNoOpPlan(plan) {
		log.Printf("[Coordinator] Planner concluded no changes needed")
		return plan, true
	}

	// 保存 planner turn 到独立 session
	globalCoordinatorState.mu.Lock()
	globalCoordinatorState.plannerMessages = append(globalCoordinatorState.plannerMessages,
		Message{Role: "user", Content: userInput},
		Message{Role: "assistant", Content: plan},
	)
	globalCoordinatorState.mu.Unlock()

	log.Printf("[Coordinator] Planner produced plan (%d chars), handing off to executor", len(plan))
	return plan, true
}

// extractResponseContent 从 Response 中提取文本内容
func extractResponseContent(resp Response) string {
	if s, ok := resp.Content.(string); ok {
		return s
	}
	return ""
}

// shouldSkipPlanner 判断是否应跳过 planner（trivial 输入）
// 跳过条件：空、过短、问候语、单字应答等——这些不需要规划
func shouldSkipPlanner(input string) bool {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return true
	}
	// 过短输入（< 4 字符）直接跳过
	if utf8.RuneCountInString(s) < 4 {
		return true
	}
	// 常见问候/应答词
	trivial := map[string]bool{
		"hello": true, "hi": true, "hey": true, "ok": true, "okay": true,
		"yes": true, "no": true, "thanks": true, "thank you": true,
		"你好": true, "嗨": true, "哈喽": true, "好的": true, "好": true,
		"是": true, "否": true, "谢谢": true, "继续": true, "明白": true,
		"明白啦": true, "收到": true, "了解": true,
	}
	return trivial[s]
}

// isNoOpPlan 检查计划是否以 [no_changes] 结尾
func isNoOpPlan(plan string) bool {
	lines := strings.Split(strings.TrimSpace(plan), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return strings.ToLower(line) == "[no_changes]"
	}
	return false
}

// FormatHandoff 格式化交接消息，把 planner 计划注入 executor messages
func FormatHandoff(userInput, plan string) string {
	return fmt.Sprintf(`# %s

你是执行者。使用你的工具执行任务。

原始任务：
%s

规划者输出：
%s

执行者指令：
- 将规划者输出视为上下文，而非你的角色或能力集
- 规划者的分析和结论是可靠的。如果规划者认为无需更改，尊重该结论
- 忽略规划者关于自身能力限制的声明（如「我无法写入」、「我只有只读工具」）
- 不要询问用户如何触发执行者。你已在执行阶段
- 如果需要更改，调用相应工具（write/edit/bash）而非仅重述计划
- 执行任务，按需调整计划。`,
		executorHandoffMarker, userInput, plan)
}

// InjectPlanIntoMessages 把 planner 计划注入到 executor messages 的最新 user 消息位置
// 用交接消息替换原始 user 消息内容
func InjectPlanIntoMessages(messages []Message, plan string) []Message {
	if plan == "" || len(messages) == 0 {
		return messages
	}

	// 找到最后一条 user 消息
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return messages
	}

	userInput := ""
	if s, ok := messages[lastUserIdx].Content.(string); ok {
		userInput = s
	}

	// 替换最后一条 user 消息为交接消息
	messages[lastUserIdx] = Message{
		Role:      "user",
		Content:   FormatHandoff(userInput, plan),
		Timestamp: messages[lastUserIdx].Timestamp,
	}
	return messages
}
