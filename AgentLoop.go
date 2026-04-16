package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "strconv"
    "strings"
    "time"
    "unicode/utf8"
)

const MaxHistoryMessages = 30

// maxWorkModeResumeRounds 工作模式退出守衛最大續行次數
// 當模型停止但 todo 有未完成項時，程序注入提示強制續行，最多觸發此次數
const maxWorkModeResumeRounds = 3

// maxXMLRePromptRounds XML 工具調用偵測最大重新提示次數
// 防止模型反覆輸出 XML 格式的工具調用導致無限循環
const maxXMLRePromptRounds = 3

// AGENTIC_TAGS 用于前端解析工具调用的标记
const (
    AgenticToolCallStart   = "<<<AGENTIC_TOOL_CALL_START>>>"
    AgenticToolCallEnd     = "<<<AGENTIC_TOOL_CALL_END>>>"
    AgenticToolNamePrefix  = "<<<TOOL_NAME:"
    AgenticToolArgsStart   = "<<<TOOL_ARGS_START>>>"
    AgenticToolArgsEnd     = "<<<TOOL_ARGS_END>>>"
    AgenticToolStatusTag   = "<<<TOOL_STATUS:"
    AgenticTagSuffix       = ">>>"
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

    // 支持 []interface{} 或 []map[string]interface{}
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
            // 返回一个包含错误信息的工具结果，并标记失败
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

// AgentLoop 核心循环
func AgentLoop(ctx context.Context, ch Channel, messages []Message, apiType, baseURL, apiKey, modelID string,
    temperature float64, maxTokens int, stream bool, thinking bool) ([]Message, error) {

    // 初始化上下文压缩器
    compressor := NewContextCompressor()

    // 每轮 AgentLoop（用户发新消息）重置循环检测器
    if globalLoopDetector != nil {
        globalLoopDetector.Clear()
    }

    // 注入记忆上下文（基于最新用户消息的 Prefetch 机制）
    // 使用 [MEMORY_CONTEXT] 方括號围栏包裹，作为独立 system message 插入到
    // 最新 user message 之前。不追加到 system prompt 尾部，避免：
    //   1. 每次改動記憶都要重建 system prompt（破壞 prompt caching）
    //   2. 模型將記憶內容誤認為用戶當前指令
    //   3. 記憶中的惡意內容造成 prompt injection
    //
    // 重要：新會話首輪（/new 或 idle 重置後）跳過記憶注入，
    // 防止舊會話的記憶上下文洩漏到新會話，導致模型「記住過去的事」。
    session := GetGlobalSession()
    isNewSession := session.ConsumeIsNewSession()
    if isNewSession {
        log.Printf("[AgentLoop] New session detected, skipping memory context injection for first turn")
    }

    if globalUnifiedMemory != nil && !isNewSession {
        // 找到最新的用户消息
        var latestUserMessage string
        var latestUserIdx int = -1
        for i := len(messages) - 1; i >= 0; i-- {
            if messages[i].Role == "user" {
                if content, ok := messages[i].Content.(string); ok && content != "" {
                    latestUserMessage = content
                }
                latestUserIdx = i
                break
            }
        }

        taskDesc := getCurrentTaskDescriptionFromMessages(messages)
        if latestUserMessage != "" {
            taskDesc = latestUserMessage
        }

        memoryContext := globalUnifiedMemory.GetContextForPrompt(taskDesc)
        fencedBlock := BuildMemoryContextBlock(memoryContext)
        if fencedBlock != "" && latestUserIdx > 0 {
            // 插入到最新 user message 之前（紧跟上一条消息之后）
            insertIdx := latestUserIdx
            memMsg := Message{Role: "system", Content: fencedBlock}
            messages = append(messages[:insertIdx], append([]Message{memMsg}, messages[insertIdx:]...)...)
        }
    }

    // 获取当前角色的 Role（用于工具权限过滤）和模型配置
    var currentRole *Role
    var effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID string
    var effectiveTemperature float64
    var effectiveMaxTokens int

    effectiveAPIType = apiType
    effectiveBaseURL = baseURL
    effectiveAPIKey = apiKey
    effectiveModelID = modelID
    effectiveTemperature = temperature
    effectiveMaxTokens = maxTokens

    if globalRoleManager != nil && globalActorManager != nil && globalStage != nil {
        currentActor := globalStage.GetCurrentActor()
        if actor, ok := globalActorManager.GetActor(currentActor); ok {
            currentRole, _ = globalRoleManager.GetRole(actor.Role)
            if modelConfig := getActorModelConfig(currentActor); modelConfig != nil {
                if modelConfig.APIType != "" {
                    effectiveAPIType = modelConfig.APIType
                }
                if modelConfig.BaseURL != "" {
                    effectiveBaseURL = modelConfig.BaseURL
                }
                if modelConfig.APIKey != "" {
                    effectiveAPIKey = modelConfig.ResolveAPIKey()
                }
                if modelConfig.Model != "" {
                    effectiveModelID = modelConfig.Model
                }
                if modelConfig.Temperature > 0 {
                    effectiveTemperature = modelConfig.Temperature
                }
                if modelConfig.MaxTokens > 0 {
                    effectiveMaxTokens = modelConfig.MaxTokens
                }
            }
        }
    }

    // 注入或更新系统提示
    if len(messages) > 0 {
        hasSystemPrompt := false
        systemPromptIndex := -1
        for i, msg := range messages {
            if msg.Role == "system" {
                hasSystemPrompt = true
                systemPromptIndex = i
                break
            }
        }

        needUpdate := false
        if globalStage != nil {
            needUpdate = globalStage.NeedUpdateSystemPrompt()
        }

        if !hasSystemPrompt || needUpdate {
            var systemPrompt string

            if globalRoleManager != nil && globalActorManager != nil && globalStage != nil {
                currentActor := globalStage.GetCurrentActor()

                // 获取模型上下文窗口大小，用于自适应系统提示
                modelCtx := GetModelContextLengthSafe(effectiveModelID)
                if modelCtx > 0 {
                    systemPrompt = BuildAdaptiveSystemPrompt(currentActor, globalActorManager, globalRoleManager, globalStage, modelCtx, 0, 0, effectiveMaxTokens)
                } else {
                    systemPrompt = BuildSystemPromptForActor(currentActor, globalActorManager, globalRoleManager, globalStage)
                }
            } else {
                systemPrompt = SYSTEM_PROMPT
            }

            // === Bootstrap: 首次对话引导 ===
            if globalUnifiedMemory != nil && IsBootstrapNeeded(globalUnifiedMemory) {
                bootstrapPrompt := GetBootstrapMissingKeysPrompt(globalUnifiedMemory)
                if bootstrapPrompt != "" {
                    systemPrompt = bootstrapPrompt + "\n\n---\n\n" + systemPrompt
                }
            }

            if systemPrompt != "" {
                if needUpdate && systemPromptIndex >= 0 {
                    messages[systemPromptIndex] = Message{Role: "system", Content: systemPrompt}
                    globalStage.ClearUpdateSystemPrompt()
                } else {
                    messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)
                }
            }
        }
    }

    // === 注入工作模式提示 ===
    // 當意圖為 task 且複雜度為 moderate/complex 時，提醒模型使用 todos 管理進度
    // 這使退出守衛能基於 todo 狀態進行程序化判斷，而非依賴模型自評
    // 注意：如果 Plan Mode 已激活，不注入工作模式提示（Plan Mode 有自己的 todo 系統）
    if globalTaskTracker != nil && globalTaskTracker.IsWorkMode() && !isNewSession {
        planModeActive := globalPlanMode != nil && globalPlanMode.IsActive()
        if !planModeActive {
            workModeHint := "\n\n[工作模式] 你正在執行一個結構化任務。\n" +
                "請使用 todos 工具管理你的子任務進度：\n" +
                "- 開始前先用 todos 創建任務列表\n" +
                "- 完成子任務後更新為 completed\n" +
                "- 對於異步操作（如 cron_add、shell_delayed），將項目標記為 waiting\n" +
                "- 所有任務完成或等待中後再總結結果"
            for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "system" {
                    if content, ok := messages[i].Content.(string); ok {
                        messages[i].Content = content + workModeHint
                    }
                    break
                }
            }
        }
    }

    // === 注入會話 token 統計信息到 system prompt ===
    // 僅在 tracker 啟用且有累計數據時注入，幫助模型了解 token 消耗
    // 新會話首輪跳過（剛重置，無統計數據可顯示）
    if !isNewSession {
        if tracker := session.GetTracker(); tracker != nil {
            if tokenStats := tracker.FormatStatsForPrompt(); tokenStats != "" {
                // 附加到現有 system prompt 末尾
                for i := len(messages) - 1; i >= 0; i-- {
                    if messages[i].Role == "system" {
                        if content, ok := messages[i].Content.(string); ok {
                            messages[i].Content = content + tokenStats
                        }
                        break
                    }
                }
            }
        }
    }

    hookManager := GetHookManager()
    iteration := 0
    resumeCount := 0              // 工作模式退出守衛續行計數器
    subagentResumeCount := 0      // 子代理運行守衛續行計數器（獨立計數，避免互相消耗配額）
    xmlRePromptCount := 0         // XML 工具調用偵測重新提示計數器

    // 记录用户消息到记忆整合器
    if globalMemoryConsolidator != nil && len(messages) > 0 {
        for i := len(messages) - 1; i >= 0; i-- {
            if messages[i].Role == "user" {
                if content, ok := messages[i].Content.(string); ok && content != "" {
                    globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
                        Role:      "user",
                        Content:   content,
                        Timestamp: time.Now(),
                    })
                    break
                }
            }
        }
    }

    loopExitedNaturally := false
    var lastTokenUsage *TokenUsage // 收集所有迭代中 API 返回的 token 使用量
    for {
        iteration++
        select {
        case <-ctx.Done():
            return messages, ctx.Err()
        default:
        }

        // ========== 迭代安全检查 ==========
        if ShouldForceStop(iteration) {
            log.Printf("[AgentLoop] 达到最大迭代次数 %d，强制停止", MaxAgentLoopIterations)
            ch.WriteChunk(StreamChunk{Content: GetIterationWarningMessage(iteration), Done: true})
            return messages, nil
        }
        if globalLoopWarningInjector.ShouldInjectWarning(iteration) {
            log.Printf("[AgentLoop] 迭代警告: iteration=%d", iteration)
            ch.WriteChunk(StreamChunk{Content: GetIterationWarningMessage(iteration), Done: false})
        }

        // ========== Plan Mode 自动提醒 ==========
        // 如果迭代次数较多且 Plan Mode 未激活，注入一次性提醒
        // 僅在規劃模式已啟用（配置開關打開）時才提醒，否則模型無法使用此功能
        if iteration == 4 && globalPlanModeEnabled && globalPlanMode != nil && !globalPlanMode.IsActive() {
            log.Printf("[AgentLoop] Plan Mode suggestion: iteration=%d, plan mode inactive", iteration)
            // 注入到消息歷史中（僅模型可見），而非 ch.WriteChunk（前端可見）
            messages = append([]Message{{
                Role:    "system",
                Content: "[系统提示] 当前任务已进行多轮工具调用。如果任务复杂、涉及多文件修改或需要仔细规划，建议使用 enter_plan_mode 工具进入结构化任务分解模式，先探索再执行。",
            }}, messages...)
        }

        // ========== Plan Mode 超時檢查 ==========
        // 如果 Plan Mode 單階段或總時間超時，強制退出並注入提醒
        if globalPlanMode != nil && globalPlanMode.IsActive() {
            if timedOut, phaseElapsed, totalElapsed := globalPlanMode.CheckPhaseTimeout(); timedOut {
                planContent := ForceExitPlanMode(fmt.Sprintf("phase elapsed=%v, total elapsed=%v", phaseElapsed, totalElapsed))
                timeoutMsg := fmt.Sprintf("[系統通知] Plan Mode 已因超時自動退出（階段耗時 %v，總耗時 %v）。\n\n", phaseElapsed.Round(time.Second), totalElapsed.Round(time.Second))
                if planContent != "" {
                    timeoutMsg += fmt.Sprintf("已完成的計劃內容將作為參考：\n\n%s\n\n", planContent)
                }
                timeoutMsg += "你可以直接使用所有工具來執行任務。"
                messages = append([]Message{{
                    Role:    "system",
                    Content: timeoutMsg,
                }}, messages...)
                log.Printf("[AgentLoop] Plan Mode timed out, forced exit (phase=%v, total=%v)", phaseElapsed, totalElapsed)
            }
        }

        // ========== 自适应历史消息管理（Pipeline 模式）==========
        modelCtxWindow := GetModelContextLengthSafe(effectiveModelID)
        adaptiveMaxHistory := MaxHistoryMessages
        if modelCtxWindow > 0 {
            adaptiveMaxHistory = CalculateAdaptiveMaxHistory(modelCtxWindow, 0, 0, effectiveMaxTokens)
            if adaptiveMaxHistory > MaxHistoryMessages {
                adaptiveMaxHistory = MaxHistoryMessages
            }
        }

        if len(messages) > adaptiveMaxHistory {
            // 保存原始消息快照（用於 EnsureUser 恢復）
            originalML := NewMessageListWithSource(messages, "agentloop:original").Snapshot("pre-pipeline")

            // 計算截斷邊界
            hasSystem := len(messages) > 0 && messages[0].Role == "system"
            budgetSlots := adaptiveMaxHistory
            if hasSystem {
                budgetSlots = adaptiveMaxHistory - 1
            }
            latestUserIndex := -1
            for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                    latestUserIndex = i
                    break
                }
            }
            idealStart := len(messages) - budgetSlots
            if idealStart < 0 {
                idealStart = 0
            }
            if latestUserIndex > 0 && idealStart > latestUserIndex {
                idealStart = latestUserIndex
            }
            boundaryStart := idealStart
            searchWindow := 20
            if idealStart > searchWindow {
                for i := idealStart; i >= idealStart-searchWindow && i > 0; i-- {
                    if messages[i].Role == "user" && (i == 0 || messages[i-1].Role != "user") {
                        boundaryStart = i
                        break
                    }
                }
            }
            if latestUserIndex > 0 && boundaryStart > latestUserIndex {
                boundaryStart = latestUserIndex
            }
            if boundaryStart < 1 {
                boundaryStart = 1
            }

            // 保存截斷前最後一條用戶請求（用於 divider 摘要）
            lastDiscardUserContent := ""
            for i := 0; i < boundaryStart; i++ {
                if messages[i].Role == "user" {
                    if content, ok := messages[i].Content.(string); ok && content != "" {
                        lastDiscardUserContent = content
                    }
                }
            }

            // 構建截斷後的消息列表
            var truncatedMsgs []Message
            if hasSystem {
                truncatedMsgs = make([]Message, 0, 1+len(messages)-boundaryStart)
                truncatedMsgs = append(truncatedMsgs, messages[0])
                truncatedMsgs = append(truncatedMsgs, messages[boundaryStart:]...)
            } else {
                truncatedMsgs = messages[boundaryStart:]
            }

            // 使用 Pipeline 執行壓縮+修復+去重，自動驗證不變量
            truncatedML := NewMessageListWithSource(truncatedMsgs, "agentloop:truncated")
            truncatedML.origin = originalML

            result := NewPipeline(truncatedML).
                Stage("compress", func(ml *MessageList) *MessageList {
                    compressed := compressor.Compress(ml.msgs)
                    resultML := NewMessageListWithSource(compressed, "pipeline:compress")
                    resultML.origin = ml.origin // 保持 origin 鏈，確保 EnsureUser 可恢復
                    return resultML
                }).
                Stage("repair", func(ml *MessageList) *MessageList {
                    return ml.RepairOrphans()
                }).
                Stage("dedup", func(ml *MessageList) *MessageList {
                    return ml.Deduplicate()
                }).
                Execute()

            messages = result.Messages.Raw()
            _ = result // Pipeline 已記錄日誌

            // 插入歷史摘要 divider
            var divider strings.Builder
            divider.WriteString("=== 历史对话摘要 ===\n")
            divider.WriteString("【重要提示】请优先响应该消息之前的最后一条用户消息\n")

            latestUserContent := ""
            for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                    if content, ok := messages[i].Content.(string); ok && content != "" {
                        latestUserContent = content
                        if utf8.RuneCountInString(latestUserContent) > 100 {
                            latestUserContent = string([]rune(latestUserContent)[:100]) + "..."
                        }
                    }
                    break
                }
            }

            if latestUserContent != "" {
                divider.WriteString("用户最新请求: " + latestUserContent + "\n")
            }

            compressedCount := boundaryStart - 1
            if compressedCount < 0 {
                compressedCount = 0
            }
            divider.WriteString("对话轮数: " + strconv.Itoa(len(messages)) + " | 已压缩: " + strconv.Itoa(compressedCount) + " 条消息\n")
            divider.WriteString("当前时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n")

            if lastDiscardUserContent != "" {
                divider.WriteString("\n[最近被截断的用户请求]\n")
                if utf8.RuneCountInString(lastDiscardUserContent) > 150 {
                    divider.WriteString(string([]rune(lastDiscardUserContent)[:150]) + "...\n")
                } else {
                    divider.WriteString(lastDiscardUserContent + "\n")
                }
            }

            divider.WriteString("\n请注意：如有指令冲突，以最新用户消息的指令为准\n")
            divider.WriteString("=== 摘要结束，以下继续对话 ===")

            dividerMsg := Message{
                Role:      "system",
                Content:   divider.String(),
                Timestamp: time.Now().Unix(),
            }

            insertIdx := 0
            for i, msg := range messages {
                if msg.Role == "system" {
                    insertIdx = i + 1
                } else {
                    break
                }
            }
            newMsgs := make([]Message, 0, len(messages)+1)
            newMsgs = append(newMsgs, messages[:insertIdx]...)
            newMsgs = append(newMsgs, dividerMsg)
            newMsgs = append(newMsgs, messages[insertIdx:]...)
            messages = newMsgs

            log.Printf("[AgentLoop] History truncated to %d messages (pipeline)", len(messages))
        }

        if hookManager != nil && hookManager.IsEnabled() {
            hookResult := hookManager.RunBeforeModel(ctx, 0, "", iteration, "", len(messages), 0)
            if hookResult.Action == HookOutcomeBlock {
                ch.WriteChunk(StreamChunk{Content: hookResult.Reason, Done: true})
                return messages, fmt.Errorf("blocked by hook: %s", hookResult.Reason)
            }
        }

        chunkChan, err := CallModel(ctx, messages, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, effectiveTemperature, effectiveMaxTokens, stream, thinking, currentRole)
        if err != nil {
            // 用戶主動取消或超時不發送 Error chunk，避免前端彈錯誤彈窗
            // 使用 errors.Is 支持 wrapped error（如 fmt.Errorf("...: %w", err)）
            if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
                if writeErr := ch.WriteChunk(StreamChunk{Error: err.Error()}); writeErr != nil {
                    log.Printf("Failed to write error chunk: %v", writeErr)
                }
            } else {
                log.Printf("[AgentLoop] CallModel cancelled/timeout, skipping error chunk")
            }
            return messages, err
        }

        var respContent interface{}
        var reasoningContent string
        var toolCalls []map[string]interface{}
        var stopReason string
        toolCallsMap := make(map[int]map[string]interface{})

        for chunk := range chunkChan {
            select {
            case <-ctx.Done():
                // 用戶主動取消，不發送 Error chunk（避免前端彈錯誤彈窗）
                // 直接返回 ctx.Err() 即可，前端通過 WebSocket 關閉感知取消
                return messages, ctx.Err()
            default:
            }

            if chunk.Error != "" {
                if writeErr := ch.WriteChunk(chunk); writeErr != nil {
                    log.Printf("Failed to write error chunk: %v", writeErr)
                    return messages, fmt.Errorf("%s", chunk.Error)
                }
                return messages, fmt.Errorf("%s", chunk.Error)
            }

            chunkToSend := chunk
            chunkToSend.Done = false
            if writeErr := ch.WriteChunk(chunkToSend); writeErr != nil {
                log.Printf("WebSocket write failed: %v, stopping AgentLoop", writeErr)
                return messages, writeErr
            }

            if chunk.Content != "" {
                filteredContent := applyReplacements(chunk.Content)
                if str, ok := respContent.(string); ok {
                    respContent = str + filteredContent
                } else {
                    respContent = filteredContent
                }
            }
            if chunk.ReasoningContent != "" {
                reasoningContent += chunk.ReasoningContent
            }
            if len(chunk.ToolCalls) > 0 {
                for _, tc := range chunk.ToolCalls {
                    idx := 0
                    if idxFloat, ok := tc["index"].(float64); ok {
                        idx = int(idxFloat)
                    } else if idxInt, ok := tc["index"].(int); ok {
                        idx = idxInt
                    }

                    existing, exists := toolCallsMap[idx]
                    if !exists {
                        existing = make(map[string]interface{})
                        toolCallsMap[idx] = existing
                    }

                    for k, v := range tc {
                        if k == "function" {
                            funcMap, ok := v.(map[string]interface{})
                            if !ok {
                                existing[k] = v
                                continue
                            }
                            existingFunc, funcOk := existing["function"].(map[string]interface{})
                            if !funcOk {
                                existingFunc = make(map[string]interface{})
                                existing["function"] = existingFunc
                            }
                            for fk, fv := range funcMap {
                                if fk == "arguments" {
                                    if argStr, ok := fv.(string); ok {
                                        if existingArgs, argsOk := existingFunc["arguments"].(string); argsOk {
                                            existingFunc["arguments"] = existingArgs + argStr
                                        } else {
                                            existingFunc["arguments"] = argStr
                                        }
                                    } else if argMap, ok := fv.(map[string]interface{}); ok {
                                        // Anthropic flushToolCall 傳入完整 map，序列化為 JSON 字符串
                                        if j, err := json.Marshal(argMap); err == nil {
                                            existingFunc["arguments"] = string(j)
                                        }
                                    }
                                } else {
                                    existingFunc[fk] = fv
                                }
                            }
                        } else {
                            if v != nil {
                                if str, ok := v.(string); ok && str == "" {
                                    continue
                                }
                                existing[k] = v
                            }
                        }
                    }
                }
            }
            if chunk.Done {
                stopReason = chunk.FinishReason
                // 收集 token 使用量（API 在最後一個 chunk 返回 usage）
                if chunk.Usage != nil {
                    lastTokenUsage = chunk.Usage
                }
                break
            }
        }

        if len(toolCallsMap) > 0 {
            maxIdx := 0
            for idx := range toolCallsMap {
                if idx > maxIdx {
                    maxIdx = idx
                }
            }
            toolCalls = make([]map[string]interface{}, 0, maxIdx+1)
            for i := 0; i <= maxIdx; i++ {
                if tc, exists := toolCallsMap[i]; exists {
                    delete(tc, "index")
                    toolCalls = append(toolCalls, tc)
                }
            }
        }

        // 将助手消息加入历史
        if stopReason == "tool_use" || stopReason == "function_call" || stopReason == "tool_calls" {
            messages = append(messages, Message{
                Role:      "assistant",
                ToolCalls: toolCalls,
                Timestamp: time.Now().Unix(),
            })
        } else {
            messages = append(messages, Message{
                Role:             "assistant",
                Content:          respContent,
                ReasoningContent: reasoningContent,
                Timestamp:        time.Now().Unix(),
            })
        }

        // 记录助手消息到记忆整合器
        if globalMemoryConsolidator != nil {
            contentStr, _ := respContent.(string)
            globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
                Role:      "assistant",
                Content:   contentStr,
                Timestamp: time.Now(),
            })
        }

        // 如果没有工具调用，检查自动切换并跳出循环
        if stopReason != "tool_use" && stopReason != "function_call" && stopReason != "tool_calls" {
            // ========== XML 工具調用偵測 ==========
            // 當模型嘗試使用不可用的工具時，可能輸出 XML 格式的工具調用作為文本
            // 例如：<invoke name="smart_shell">... 或 <INVOKE NAME="SMART_SHELL">...
            // 此時應重新提示模型使用可用工具，而非直接退出循環
            contentStr, _ := respContent.(string)
            if contentStr != "" && detectXMLToolInvocation(contentStr) {
                xmlRePromptCount++
                if xmlRePromptCount > maxXMLRePromptRounds {
                    log.Printf("[AgentLoop] XML re-prompt limit reached (%d), stopping re-prompt", xmlRePromptCount)
                    // 不再重新提示，讓模型輸出作為最終回覆繼續流程
                } else {
                    log.Printf("[AgentLoop] Detected XML tool invocation in text response, re-prompting model (%d/%d)", xmlRePromptCount, maxXMLRePromptRounds)
                    // assistant 消息已在上方 L879 添加，此處直接注入重新提示
                    rePromptMsg := "[系统提示] 你的回复包含了 XML 格式的工具调用，但该工具当前不可用或未被正确识别。" +
                        "请使用下方工具列表中可用的工具，通过标准的 tool_calls 机制调用。" +
                        "不要在文本中手动编写工具调用 XML。如果需要的工具不在可用列表中，" +
                        "请使用其他可用工具完成任务，或向用户说明情况。"
                    messages = append(messages, Message{
                        Role:      "user",
                        Content:   rePromptMsg,
                        Timestamp: time.Now().Unix(),
                    })
                    continue
                }
            }

            if globalStage != nil && globalStage.AutoSwitchEnabled() {
                hasMarker, targetActor, isEnd := ParseSwitchMarker(contentStr)

                if hasMarker && !isEnd && targetActor != "" && globalStage.CanAutoSwitch() {
                    if _, ok := globalActorManager.GetActor(targetActor); ok {
                        cleanedContent := StripSwitchMarker(contentStr)

                        messages[len(messages)-1] = Message{
                            Role:             "assistant",
                            Content:          cleanedContent,
                            ReasoningContent: reasoningContent,
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

                        newSystemPrompt := BuildAdaptiveSystemPrompt(targetActor, globalActorManager, globalRoleManager, globalStage, modelCtxWindow, 0, 0, effectiveMaxTokens)
                        newMessages = append([]Message{{Role: "system", Content: newSystemPrompt}}, newMessages...)

                        // 壓縮後重新注入記憶上下文（使用 XML 圍欄，獨立 system message）
                        if globalUnifiedMemory != nil {
                            memoryContext := globalUnifiedMemory.GetContextForPrompt("")
                            fencedBlock := BuildMemoryContextBlock(memoryContext)
                            if fencedBlock != "" {
                                // 找到最新 user message 並在其前插入
                                userInsertIdx := -1
                                for i := len(newMessages) - 1; i >= 0; i-- {
                                    if newMessages[i].Role == "user" {
                                        userInsertIdx = i
                                        break
                                    }
                                }
                                if userInsertIdx > 0 {
                                    memMsg := Message{Role: "system", Content: fencedBlock}
                                    newMessages = append(newMessages[:userInsertIdx], append([]Message{memMsg}, newMessages[userInsertIdx:]...)...)
                                }
                            }
                        }
                        messages = newMessages

                        continue
                    }
                } else if isEnd {
                    ch.WriteChunk(StreamChunk{Content: "\n═══════════════════════════════════════════════════════════════\n[Auto Stopped: END marker]\n═══════════════════════════════════════════════════════════════\n"})
                    cleanedContent := StripSwitchMarker(contentStr)
                    messages[len(messages)-1] = Message{
                        Role:             "assistant",
                        Content:          cleanedContent,
                        ReasoningContent: reasoningContent,
                    }
                }
            }
            // ========== 工作模式退出守衛（基於 todo 狀態的程序化判斷） ==========
            // 取代模型自評：程序直接檢查 todo 列表中是否有未完成項目
            // - 有 in_progress/pending 項目 → 強制續行（模型不能停）
            // - 所有非 completed 項目都是 waiting → 允許退出（異步等待中）
            // - 無活躍 todo 項目 → 放行（簡單對話或模型未使用 todos）
            if TODO.HasUnfinishedItems() {
                if !TODO.AllUnfinishedAreWaiting() {
                    // 有 in_progress 或 pending 項目，不允許退出
                    if resumeCount < maxWorkModeResumeRounds {
                        resumeCount++
                        unfinished := TODO.GetUnfinishedSummary()
                        // 使用 [SYSTEM_RESUME] 標記區分系統注入的續行提示與真實用戶消息
                        // 這確保 FeedbackCollector 的 WakeNotification 檢測和隱式反饋採集
                        // 能正確跳過系統消息，找到真正的觸發用戶消息
                        resumePrompt := fmt.Sprintf(
                            "[SYSTEM_RESUME] 你的任務尚未完成。以下待辦事項仍需處理：\n%s\n\n請繼續執行未完成的任務。如果某個任務已提交為異步操作（如 cron_add），請使用 todos 工具將其狀態更新為 waiting。",
                            unfinished,
                        )
                        messages = append(messages, Message{
                            Role:      "user",
                            Content:   resumePrompt,
                            Timestamp: time.Now().Unix(),
                        })
                        log.Printf("[AgentLoop] Work mode exit guard: resume #%d, unfinished todos detected", resumeCount)
                        continue
                    }
                    log.Printf("[AgentLoop] Work mode: max resume rounds (%d) reached, allowing exit", maxWorkModeResumeRounds)
                } else {
                    // 所有未完成項目都是 waiting（異步等待中），允許退出
                    log.Printf("[AgentLoop] Work mode: all remaining todos are waiting, allowing exit")
                }
            }

            // ========== 子代理運行守衛 ==========
            // 如果有子代理仍在後台運行，模型不應停止——必須繼續 spawn_check 或等待結果。
            // 防止模型在子代理執行期間回覆無意義的文字（如 "I see your message appears empty"）
            // 然後直接退出循環，導致用戶看到異常的系統級回覆。
            // 使用獨立的 subagentResumeCount，避免與工作模式守衛互相消耗配額。
            if globalSubagentManager != nil {
                var runningSubagentIDs []string
                for _, task := range globalSubagentManager.List() {
                    task.mu.RLock()
                    if task.Status == SubagentRunning {
                        runningSubagentIDs = append(runningSubagentIDs, task.ID)
                    }
                    task.mu.RUnlock()
                }
                if len(runningSubagentIDs) > 0 && subagentResumeCount < maxWorkModeResumeRounds {
                    subagentResumeCount++
                    resumePrompt := fmt.Sprintf(
                        "[SYSTEM_RESUME] 你有 %d 個子代理仍在後台運行（%s）。\n"+
                            "請繼續使用 spawn_check 檢查它們的進度，直到所有子代理完成。\n"+
                            "不要回覆文字給用戶，繼續執行工具調用。",
                        len(runningSubagentIDs), strings.Join(runningSubagentIDs, ", "))
                    messages = append(messages, Message{
                        Role:      "user",
                        Content:   resumePrompt,
                        Timestamp: time.Now().Unix(),
                    })
                    log.Printf("[AgentLoop] Subagent running guard: resume #%d, %d subagents still running: %v", subagentResumeCount, len(runningSubagentIDs), runningSubagentIDs)
                    continue
                } else if len(runningSubagentIDs) > 0 {
                    log.Printf("[AgentLoop] Subagent running guard: max resume rounds (%d) reached, allowing exit despite %d running subagents", maxWorkModeResumeRounds, len(runningSubagentIDs))
                }
            }

            loopExitedNaturally = true
            break
        }

        // ========== 统一的工具调用处理 ==========
        var parsedCalls []ParsedToolCall

        // 所有 API 類型的工具調用均已由流式/非流式處理器統一轉為 OpenAI 兼容格式（存於 toolCalls）
        parsedCalls = parseToolCallsFromOpenAI(toolCalls)

        if len(parsedCalls) == 0 {
            if IsDebug {
                fmt.Printf("Warning: no tool calls to process\n")
            }
            continue
        }

        // 执行所有工具调用
        var results []EnrichedMessage
        for _, call := range parsedCalls {
            select {
            case <-ctx.Done():
                log.Printf("[AgentLoop] Context cancelled, stopping tool execution")
                return messages, ctx.Err()
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

        // 将工具结果添加到消息历史
        for _, result := range results {
            messages = append(messages, result.ToAPIMessage())

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

        // 记录工具消息到记忆整合器
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

        if globalTaskTracker != nil {
            shouldPrompt, promptMsg := globalTaskTracker.ShouldPromptTodo()
            if shouldPrompt && promptMsg != "" {
                messages = append(messages, Message{
                    Role:      "user",
                    Content:   promptMsg,
                    Timestamp: time.Now().Unix(),
                })
            }
        }

        if IsDebug {
            fmt.Printf("Number of messages before second call: %d\n", len(messages))
            for i, msg := range messages {
                fmt.Printf("Message %d: Role=%s, Content=%v, ToolCallID=%s\n", i, msg.Role, msg.Content, msg.ToolCallID)
            }
        }
    }

    // 隐式任务完成检测 — 私下询问模型，不向用户发送任何消息
    // 守卫条件：循环自然退出 + 冷却期已过 + 输入非系统唤醒通知
    if globalFeedbackCollector != nil && iteration > 0 && loopExitedNaturally {
        // 查找触发本次 AgentLoop 的用户消息（用于检测是否为唤醒通知）
        // 跳過系統注入的續行提示（[SYSTEM_RESUME] 前綴），找到真正的用戶消息
        var triggerUserMsg string
        for i := len(messages) - 1; i >= 0; i-- {
            if messages[i].Role == "user" {
                if content, ok := messages[i].Content.(string); ok && content != "" {
                    // 跳過系統注入的續行提示
                    if strings.HasPrefix(content, "[SYSTEM_RESUME]") {
                        continue
                    }
                    triggerUserMsg = content
                }
                break
            }
        }

        // 降級守衛：有活躍的非計劃 todo 項目 → 程序化退出守衛已取代模型自評
        if TODO.HasUnfinishedItems() {
            log.Printf("[FeedbackCollector] Skipping: active todo items exist, programmatic exit guard takes precedence")
        } else if IsWakeNotification(triggerUserMsg) {
            log.Printf("[FeedbackCollector] Skipping task completion check: input is a wake notification")
        } else if !globalFeedbackCollector.CanAskCompletion() {
            log.Printf("[FeedbackCollector] Skipping task completion check: cooldown active")
        } else {
            // 提取最后的用户消息和助手回复
            var lastUserMsg, lastAssistantMsg string
            for i := len(messages) - 1; i >= 0; i-- {
                if lastUserMsg == "" && messages[i].Role == "user" {
                    if content, ok := messages[i].Content.(string); ok {
                        lastUserMsg = content
                    }
                }
                if lastAssistantMsg == "" && messages[i].Role == "assistant" {
                    if content, ok := messages[i].Content.(string); ok {
                        lastAssistantMsg = content
                    }
                }
                if lastUserMsg != "" && lastAssistantMsg != "" {
                    break
                }
            }
            if lastUserMsg != "" && lastAssistantMsg != "" {
                apiConfig := TaskCompletionQuery{
                    APIType: effectiveAPIType,
                    BaseURL: effectiveBaseURL,
                    APIKey:  effectiveAPIKey,
                    ModelID: effectiveModelID,
                }
                // 记录调用时间（进入冷却期）
                globalFeedbackCollector.RecordCompletionAsk()
                // 使用独立 context + 短超时，保持同步执行（不脱离当前任务上下文）
                askCtx, askCancel := context.WithTimeout(context.Background(), 10*time.Second)
                completed := globalFeedbackCollector.AskModelTaskCompletion(askCtx, lastUserMsg, lastAssistantMsg, apiConfig)
                askCancel()

                if completed {
                    // 静默标记任务完成，等待用户下次发消息时采集隐式反馈
                    globalFeedbackCollector.MarkTaskCompleted(lastUserMsg, lastAssistantMsg)
                    log.Printf("[FeedbackCollector] Task marked as completed (implicit, no user prompt)")
                }
            }
        }
    }

    ch.WriteChunk(StreamChunk{Done: true})

    // === Token 追蹤：記錄 API 返回的 token 使用量 ===
    if lastTokenUsage != nil && lastTokenUsage.TotalTokens > 0 {
        session := GetGlobalSession()
        if tracker := session.GetTracker(); tracker != nil {
            tracker.RecordAPICall(*lastTokenUsage)
            stats := tracker.GetStats()
            log.Printf("[AgentLoop] Token usage recorded: prompt=%d, completion=%d, total=%d (session_total=%d)",
                lastTokenUsage.PromptTokens, lastTokenUsage.CompletionTokens,
                lastTokenUsage.TotalTokens, stats.TotalTokens)

            // Token 不足警告
            if tracker.ShouldWarnTokenBudget() {
                tracker.MarkTokenWarningSent()
                cfg := EffectiveSessionConfig()
                warnMsg := fmt.Sprintf("\n[系統提醒] 當前會話 token 使用量已達上限的 %.0f%% (%d/%d)。\n如需繼續長時間對話，建議使用 /new 開始新會話，或等待系統自動重置。\n",
                    cfg.TokenWarningRatio*100, stats.TotalTokens, cfg.SessionTokenLimit)
                ch.WriteChunk(StreamChunk{Content: warnMsg})
            }
        }
    }

    // 写入每日日志
    if globalMemoryConsolidator != nil {
        sessionID := ch.GetSessionID()
        if sessionID != "" {
            if err := globalMemoryConsolidator.WriteDailyLog(sessionID, messages); err != nil {
                log.Printf("[MemoryConsolidator] WriteDailyLog error: %v", err)
            }
        }
    }

    if globalMemoryConsolidator != nil {
        go func() {
            sessionKey := "default"
            if should, _ := globalMemoryConsolidator.ShouldConsolidate(sessionKey); should {
                log.Println("[MemoryConsolidator] Triggering automatic consolidation...")
                if err := globalMemoryConsolidator.MaybeConsolidate(context.Background(), sessionKey); err != nil {
                    log.Printf("[MemoryConsolidator] Consolidation failed: %v", err)
                }
            }
        }()
    }

    // 轨迹记录
    if globalTrajectoryManager != nil {
        go func() {
            modelUsed := effectiveModelID
            tokenUsage := TokenUsage{}
            success := true
            globalTrajectoryManager.RecordTrajectory(messages, success, modelUsed, tokenUsage)
        }()
    }

    // 策略优化
    if globalStrategyOptimizer != nil {
        go func() {
            if iteration%10 == 0 {
                if result, err := globalStrategyOptimizer.Optimize(); err == nil && result != nil {
                    log.Printf("[StrategyOptimizer] Optimization completed with score: %.2f", result.ImprovementScore)
                }
            }
        }()
    }

    // 记忆重构
    if globalMemoryRefactorManager != nil {
        go func() {
            if iteration%20 == 0 {
                if result, err := globalMemoryRefactorManager.Refactor(); err == nil && result != nil {
                    log.Printf("[MemoryRefactorManager] Refactoring completed with score: %.2f", result.ImprovementScore)
                }
            }
        }()
    }

    return messages, nil
}

// detectXMLToolInvocation 检測模型文本回覆中是否包含 XML 格式的工具調用
// 當模型嘗試使用不可用的工具時，可能輸出類似以下格式：
//   <invoke name="smart_shell"><parameter name="command">...</parameter></invoke>
//   <INVOKE NAME="SMART_SHELL"><PARAMETER NAME="COMMAND">...</PARAMETER></INVOKE>
//   <function_call>{"name": "tool_name", ...}</function_call>
//
// 為了避免誤報（用戶正常討論 XML/HTML 時觸發），採用以下策略：
//   1. 只檢測回覆前 500 字符（模型通常在開頭輸出工具調用）
//   2. <invoke> 和 <tool_call必須引用已知工具名稱
//   3. <parameter> 必須包含常見工具參數名（command/filename/query/url）
func detectXMLToolInvocation(content string) bool {
    // 只檢查前 500 字符（rune 級別），避免用戶在長回覆中間討論 XML 時誤觸發
    checkContent := content
    runes := []rune(checkContent)
    if len(runes) > 500 {
        checkContent = string(runes[:500])
    }
    lower := strings.ToLower(checkContent)

    // 已知 GhostClaw 工具名稱（用於 <invoke name="..."> 驗證）
    knownToolNames := []string{
        "smart_shell", "shell", "shell_delayed", "read_all_lines", "read_file_line",
        "write_file", "write_file_line", "write_all_lines", "search_files",
        "enter_plan_mode", "spawn", "spawn_check", "spawn_list", "spawn_batch",
        "menu", "todo", "todos", "grep", "list_directory", "web_search",
        "browser_navigate", "browser_click", "browser_type", "browser_snapshot",
        "mcp_call", "replace", "batch_replace", "file_exists",
    }

    // 檢測 <invoke name="..."> — 必須引用已知工具名稱
    if strings.Contains(lower, "<invoke") && strings.Contains(lower, "name=") {
        for _, toolName := range knownToolNames {
            if strings.Contains(lower, "name=\""+toolName+"\"") || strings.Contains(lower, "name='"+toolName+"'") {
                return true
            }
        }
    }

    // 檢測 <tool_call name="..."> — 同樣必須引用已知工具名稱
    if strings.Contains(lower, "<tool_call") && strings.Contains(lower, "name=") {
        for _, toolName := range knownToolNames {
            if strings.Contains(lower, "name=\""+toolName+"\"") || strings.Contains(lower, "name='"+toolName+"'") {
                return true
            }
        }
    }

    // 檢測 <function_call> 模式（通用函數調用，通常不誤報）
    if strings.Contains(lower, "<function_call>") {
        return true
    }

    // 檢測裸 <parameter> + 常見工具參數名的組合
    // 限定：必須同時出現 <parameter 和 </parameter>（閉合標籤），減少誤報
    if strings.Contains(lower, "<parameter") && strings.Contains(lower, "</parameter") &&
        strings.Contains(lower, "name=") &&
        (strings.Contains(lower, "command") || strings.Contains(lower, "filename") ||
            strings.Contains(lower, "query") || strings.Contains(lower, "url") ||
            strings.Contains(lower, "content") || strings.Contains(lower, "path")) {
        return true
    }

    return false
}
