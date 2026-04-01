package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "strings"
    "time"
)

const MaxHistoryMessages = 100

// 工具调用配额（每个会话/任务）
const MaxToolCallsPerSession = 50

// AGENTIC_TAGS 用于前端解析工具调用的标记
const (
    AgenticToolCallStart   = "<<<AGENTIC_TOOL_CALL_START>>>"
    AgenticToolCallEnd     = "<<<AGENTIC_TOOL_CALL_END>>>"
    AgenticToolNamePrefix  = "<<<TOOL_NAME:"
    AgenticToolArgsStart   = "<<<TOOL_ARGS_START>>>"
    AgenticToolArgsEnd     = "<<<TOOL_ARGS_END>>>"
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

// AgentLoop 核心循环
func AgentLoop(ctx context.Context, ch Channel, messages []Message, apiType, baseURL, apiKey, modelID string,
    temperature float64, maxTokens int, stream bool, thinking bool) ([]Message, error) {

    // 工具调用配额计数器
    toolCallCount := 0

    // 注入记忆上下文
    if globalUnifiedMemory != nil {
        taskDesc := getCurrentTaskDescriptionFromMessages(messages)
        memoryContext := globalUnifiedMemory.GetContextForPrompt(taskDesc)
        if memoryContext != "" {
            if len(messages) > 0 && messages[0].Role == "system" {
                if content, ok := messages[0].Content.(string); ok {
                    messages[0].Content = content + "\n\n" + memoryContext
                }
            } else {
                messages = append([]Message{{Role: "system", Content: memoryContext}}, messages...)
            }
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
            if modelConfig := globalActorManager.GetActorModel(currentActor); modelConfig != nil {
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
                systemPrompt = BuildSystemPromptForActor(currentActor, globalActorManager, globalRoleManager, globalStage)
            } else {
                systemPrompt = SYSTEM_PROMPT
            }

            // === Bootstrap: 首次对话引导 ===
            // 检查记忆中是否存在必要字段（user.name, user.birth_year, user.gender, assistant.name）
            // 如果缺失，强制注入引导提示，要求模型主动询问用户收集信息
            if globalUnifiedMemory != nil && IsBootstrapNeeded(globalUnifiedMemory) {
                bootstrapPrompt := GetBootstrapMissingKeysPrompt(globalUnifiedMemory)
                if bootstrapPrompt != "" {
                    // Prepend bootstrap instruction to the system prompt
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

    hookManager := GetHookManager()
    iteration := 0

    // 记录用户消息到记忆整合器（使用原始内容，无隐式总结）
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

    for {
        iteration++
        select {
        case <-ctx.Done():
            return messages, ctx.Err()
        default:
        }

        // ========== 新增：每次循环开始前截断历史 ==========
        if len(messages) > MaxHistoryMessages {
            keep := MaxHistoryMessages
            if messages[0].Role == "system" {
                newMessages := make([]Message, 0, keep)
                newMessages = append(newMessages, messages[0])
                start := len(messages) - (keep - 1)
                if start < 1 {
                    start = 1
                }
                newMessages = append(newMessages, messages[start:]...)
                messages = newMessages
            } else {
                messages = messages[len(messages)-keep:]
            }
            log.Printf("[AgentLoop] History truncated to %d messages", len(messages))
        }
        // ===============================================

        if hookManager != nil && hookManager.IsEnabled() {
            hookResult := hookManager.RunBeforeModel(ctx, 0, "", iteration, "", len(messages), 0)
            if hookResult.Action == HookOutcomeBlock {
                ch.WriteChunk(StreamChunk{Content: hookResult.Reason, Done: true})
                return messages, fmt.Errorf("blocked by hook: %s", hookResult.Reason)
            }
        }

        chunkChan, err := CallModel(ctx, messages, effectiveAPIType, effectiveBaseURL, effectiveAPIKey, effectiveModelID, effectiveTemperature, effectiveMaxTokens, stream, thinking, currentRole)
        if err != nil {
            if writeErr := ch.WriteChunk(StreamChunk{Error: err.Error()}); writeErr != nil {
                log.Printf("Failed to write error chunk: %v", writeErr)
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
                ch.WriteChunk(StreamChunk{Error: ctx.Err().Error()})
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
                // 对内容应用字符串替换（过滤累赘表达等）
                // 确保 respContent（落盘/历史）和前端显示使用相同的过滤后内容
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

        if stopReason == "tool_use" || stopReason == "function_call" || stopReason == "tool_calls" {
            messages = append(messages, Message{
                Role:      "assistant",
                ToolCalls: toolCalls,
            })
        } else {
            messages = append(messages, Message{
                Role:             "assistant",
                Content:          respContent,
                ReasoningContent: reasoningContent,
            })
        }

        // 记录助手消息到记忆整合器（使用原始内容，无隐式总结）
        if globalMemoryConsolidator != nil {
            contentStr, _ := respContent.(string)
            globalMemoryConsolidator.AddMessage("default", ConsolidationMessage{
                Role:      "assistant",
                Content:   contentStr,
                Timestamp: time.Now(),
            })
        }

        if stopReason != "tool_use" && stopReason != "function_call" && stopReason != "tool_calls" {
            if globalStage != nil && globalStage.AutoSwitchEnabled() {
                contentStr, _ := respContent.(string)
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

                        newSystemPrompt := BuildSystemPromptForActor(targetActor, globalActorManager, globalRoleManager, globalStage)
                        if globalUnifiedMemory != nil {
                            memoryContext := globalUnifiedMemory.GetContextForPrompt("")
                            if memoryContext != "" {
                                newSystemPrompt += "\n\n" + memoryContext
                            }
                        }
                        newMessages = append([]Message{{Role: "system", Content: newSystemPrompt}}, newMessages...)
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
            break
        }

        var results []EnrichedMessage

        if IsDebug {
            fmt.Println("===================== Executing tool calls =====================")
            fmt.Printf("API type: %s\n", apiType)
            fmt.Printf("Response content type: %T\n", respContent)
            fmt.Printf("Response content: %v\n", respContent)
        }

        if apiType == "openai" {
            var toolCallsSlice []interface{}
            for _, m := range toolCalls {
                toolCallsSlice = append(toolCallsSlice, m)
            }

            if len(toolCallsSlice) == 0 {
                if IsDebug {
                    fmt.Printf("Warning: no tool calls to process\n")
                }
                continue
            }

            validToolCalls := []interface{}{}
            type callInfo struct {
                ID       string
                Name     string
                ArgsJSON string
            }
            var callsToProcess []callInfo

            for _, item := range toolCallsSlice {
                toolUse, ok := item.(map[string]interface{})
                if !ok {
                    if IsDebug {
                        fmt.Printf("Warning: invalid tool call item: %v\n", item)
                    }
                    continue
                }

                toolID, ok := toolUse["id"].(string)
                if !ok {
                    if idVal, exists := toolUse["id"]; exists {
                        toolID = fmt.Sprint(idVal)
                    } else {
                        if IsDebug {
                            fmt.Printf("Warning: tool call missing id: %v\n", toolUse)
                        }
                        continue
                    }
                }
                if toolID == "" {
                    if IsDebug {
                        fmt.Printf("Warning: tool call has empty id: %v\n", toolUse)
                    }
                    continue
                }

                if toolUse["type"] != "function" {
                    validToolCalls = append(validToolCalls, toolUse)
                    callsToProcess = append(callsToProcess, callInfo{
                        ID:       toolID,
                        Name:     "",
                        ArgsJSON: "",
                    })
                    continue
                }
                function, ok := toolUse["function"].(map[string]interface{})
                if !ok {
                    validToolCalls = append(validToolCalls, toolUse)
                    callsToProcess = append(callsToProcess, callInfo{
                        ID:       toolID,
                        Name:     "",
                        ArgsJSON: "",
                    })
                    continue
                }
                toolName, _ := function["name"].(string)
                argsStr, _ := function["arguments"].(string)

                validToolCalls = append(validToolCalls, toolUse)
                callsToProcess = append(callsToProcess, callInfo{
                    ID:       toolID,
                    Name:     toolName,
                    ArgsJSON: argsStr,
                })
            }

            messages = messages[:len(messages)-1]
            messages = append(messages, Message{
                Role:      "assistant",
                ToolCalls: validToolCalls,
            })

            for _, call := range callsToProcess {
                select {
                case <-ctx.Done():
                    log.Printf("[AgentLoop] Context cancelled, stopping tool execution")
                    return messages, ctx.Err()
                default:
                }

                // ========== 工具调用配额检查 ==========
                toolCallCount++
                if toolCallCount > MaxToolCallsPerSession {
                    errMsg := fmt.Sprintf("⚠️ 已达到工具调用上限（%d次），任务已自动停止。请考虑简化任务或使用 /new 开始新对话。", MaxToolCallsPerSession)
                    ch.WriteChunk(StreamChunk{Error: errMsg, Done: true})
                    return messages, fmt.Errorf("tool call quota exceeded")
                }
                // ===================================

                if call.Name == "" {
                    results = append(results, NewToolResultMessage(call.ID, "Error: Invalid tool type or function field", TaskStatusFailed, ""))
                    continue
                }

                var argsMap map[string]interface{}
                if err := json.Unmarshal([]byte(call.ArgsJSON), &argsMap); err != nil {
                    if IsDebug {
                        fmt.Printf("Failed to parse arguments: %v\n", err)
                    }
                    results = append(results, NewToolResultMessage(call.ID, "Error: Failed to parse arguments", TaskStatusFailed, call.Name))
                    continue
                }

                if hookManager != nil && hookManager.IsEnabled() {
                    hookResult := hookManager.RunBeforeTool(ctx, 0, "", iteration, call.Name, argsMap)
                    if hookResult.Action == HookOutcomeBlock {
                        results = append(results, NewToolResultMessage(call.ID, hookResult.Reason, TaskStatusFailed, call.Name))
                        continue
                    } else if hookResult.Action == HookOutcomeModify && hookResult.ModifiedInput != nil {
                        argsMap = hookResult.ModifiedInput
                    }
                }

                result := executeTool(ctx, call.ID, call.Name, argsMap, ch, currentRole)

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
                    // 如果检测到循环且需要中断，则终止整个任务
                    if loopResult.ShouldInterrupt {
                        errMsg := fmt.Sprintf("\n\n🚫 %s\n\n任务已被系统终止，因为检测到重复循环。", loopResult.WarningMessage)
                        ch.WriteChunk(StreamChunk{Error: errMsg, Done: true})
                        return messages, fmt.Errorf("loop detected: %s", loopResult.WarningMessage)
                    }
                    // 否则只添加警告
                    contentStr = contentStr + "\n\n" + loopResult.WarningMessage
                    if loopResult.Suggestion != "" {
                        contentStr = contentStr + "\n\n💡 建议：" + loopResult.Suggestion
                    }
                    result.Content = contentStr
                    log.Printf("[AgentLoop] Loop detected: %s (count: %d)", call.Name, loopResult.LoopCount)
                }

                if hookManager != nil && hookManager.IsEnabled() {
                    contentStr, _ := result.Content.(string)
                    toolResultInfo := &ToolResultInfo{
                        Content: contentStr,
                        IsError: result.Meta.Status == TaskStatusFailed,
                    }
                    hookResult := hookManager.RunAfterTool(ctx, 0, "", iteration, call.Name, argsMap, toolResultInfo)
                    if hookResult.Action == HookOutcomeBlock {
                        result = NewToolResultMessage(call.ID, hookResult.Reason, TaskStatusFailed, call.Name)
                    } else if hookResult.Action == HookOutcomeModify {
                        if warning, ok := hookResult.Patch["warning"].(string); ok {
                            contentStr = contentStr + "\n\n" + warning
                            result.Content = contentStr
                        }
                    }
                }

                results = append(results, result)
            }
        } else {
            if contentArray, ok := respContent.([]interface{}); ok {
                for _, item := range contentArray {
                    select {
                    case <-ctx.Done():
                        log.Printf("[AgentLoop] Context cancelled, stopping tool execution (anthropic format)")
                        return messages, ctx.Err()
                    default:
                    }

                    if toolUse, ok := item.(map[string]interface{}); ok && toolUse["type"] == "tool_use" {
                        toolName, nameOk := toolUse["name"].(string)
                        input, inputOk := toolUse["input"].(map[string]interface{})

                        toolID, ok := toolUse["id"].(string)
                        if !ok {
                            if idVal, exists := toolUse["id"]; exists {
                                toolID = fmt.Sprint(idVal)
                            } else {
                                if IsDebug {
                                    fmt.Printf("Warning: tool call missing id: %v\n", toolUse)
                                }
                                continue
                            }
                        }
                        if toolID == "" {
                            if IsDebug {
                                fmt.Printf("Warning: tool call has empty id: %v\n", toolUse)
                            }
                            continue
                        }

                        if !nameOk || !inputOk {
                            results = append(results, NewToolResultMessage(toolID, "Error: Invalid tool use fields", TaskStatusFailed, toolName))
                            continue
                        }

                        // ========== 工具调用配额检查 ==========
                        toolCallCount++
                        if toolCallCount > MaxToolCallsPerSession {
                            errMsg := fmt.Sprintf("⚠️ 已达到工具调用上限（%d次），任务已自动停止。请考虑简化任务或使用 /new 开始新对话。", MaxToolCallsPerSession)
                            ch.WriteChunk(StreamChunk{Error: errMsg, Done: true})
                            return messages, fmt.Errorf("tool call quota exceeded")
                        }
                        // ===================================

                        if hookManager != nil && hookManager.IsEnabled() {
                            hookResult := hookManager.RunBeforeTool(ctx, 0, "", iteration, toolName, input)
                            if hookResult.Action == HookOutcomeBlock {
                                results = append(results, NewToolResultMessage(toolID, hookResult.Reason, TaskStatusFailed, toolName))
                                continue
                            } else if hookResult.Action == HookOutcomeModify && hookResult.ModifiedInput != nil {
                                input = hookResult.ModifiedInput
                            }
                        }

                        result := executeTool(ctx, toolID, toolName, input, ch, currentRole)

                        contentStr, _ := result.Content.(string)
                        isErr := result.Meta.Status == TaskStatusFailed
                        if loopResult := CheckLoop(toolName, input, contentStr, isErr); loopResult != nil {
                            if globalUnifiedMemory != nil {
                                exps := globalUnifiedMemory.RetrieveExperiences(toolName, 2)
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
                                ch.WriteChunk(StreamChunk{Error: errMsg, Done: true})
                                return messages, fmt.Errorf("loop detected: %s", loopResult.WarningMessage)
                            }
                            contentStr = contentStr + "\n\n" + loopResult.WarningMessage
                            if loopResult.Suggestion != "" {
                                contentStr = contentStr + "\n\n💡 建议：" + loopResult.Suggestion
                            }
                            result.Content = contentStr
                            log.Printf("[AgentLoop] Loop detected: %s (count: %d)", toolName, loopResult.LoopCount)
                        }

                        if hookManager != nil && hookManager.IsEnabled() {
                            contentStr, _ := result.Content.(string)
                            toolResultInfo := &ToolResultInfo{
                                Content: contentStr,
                                IsError: result.Meta.Status == TaskStatusFailed,
                            }
                            hookResult := hookManager.RunAfterTool(ctx, 0, "", iteration, toolName, input, toolResultInfo)
                            if hookResult.Action == HookOutcomeBlock {
                                result = NewToolResultMessage(toolID, hookResult.Reason, TaskStatusFailed, toolName)
                            } else if hookResult.Action == HookOutcomeModify {
                                if warning, ok := hookResult.Patch["warning"].(string); ok {
                                    contentStr = contentStr + "\n\n" + warning
                                    result.Content = contentStr
                                }
                            }
                        }

                        results = append(results, result)
                    }
                }
            }
        }

        for _, result := range results {
            messages = append(messages, result.ToAPIMessage())

            if globalTaskTracker != nil {
                contentStr, _ := result.Content.(string)
                globalTaskTracker.RecordToolCall(
                    result.Meta.ToolName,
                    result.Meta.Status,
                    "",
                    truncateString(contentStr, 100),
                )
            }
        }

        if globalTaskTracker != nil {
            shouldPrompt, promptMsg := globalTaskTracker.ShouldPromptTodo()
            if shouldPrompt && promptMsg != "" {
                messages = append(messages, Message{
                    Role:    "user",
                    Content: promptMsg,
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

    ch.WriteChunk(StreamChunk{Done: true})

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
                if err := globalMemoryConsolidator.MaybeConsolidate(ctx, sessionKey); err != nil {
                    log.Printf("[MemoryConsolidator] Consolidation failed: %v", err)
                }
            }
        }()
    }

    return messages, nil
}

