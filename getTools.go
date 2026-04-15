package main

import (
        "log"
        "strings"
        "sync"
)

// getTools 根据 API 类型返回对应格式的工具定义
// 现在从统一的工具注册中心生成，消除了双份定义和参数漂移
func getTools(apiType string) interface{} {
        switch apiType {
        case "openai", "ollama":
                return getOpenAIToolsFromRegistry()
        default: // anthropic 及其他兼容格式
                return getAnthropicToolsFromRegistry()
        }
}

// ── 工具定義緩存（啟動時生成一次，之後直接復用）─────────────
// 原來每次請求都從 toolRegistry 重新生成 106 個 map（ToOpenAI/ToAnthropic），
// 這是 prepareRequestData 6-7s 延遲的主要來源之一。

var (
        cachedOpenAITools    []map[string]interface{}
        cachedAnthropicTools []map[string]interface{}
        toolCacheOnce        sync.Once
)

// initToolCache 初始化工具定義緩存（僅在首次調用時執行）
func initToolCache() {
        tools := GetRegistryTools()
        openaiResult := make([]map[string]interface{}, len(tools))
        anthropicResult := make([]map[string]interface{}, len(tools))
        for i, td := range tools {
                openaiResult[i] = td.ToOpenAI()
                anthropicResult[i] = td.ToAnthropic()
        }
        cachedOpenAITools = openaiResult
        cachedAnthropicTools = anthropicResult
}

// getOpenAIToolsFromRegistry 从注册中心生成 OpenAI 格式工具列表（带缓存）
func getOpenAIToolsFromRegistry() []map[string]interface{} {
        toolCacheOnce.Do(initToolCache)
        return cachedOpenAITools
}

// getAnthropicToolsFromRegistry 从注册中心生成 Anthropic 格式工具列表（带缓存）
func getAnthropicToolsFromRegistry() []map[string]interface{} {
        toolCacheOnce.Do(initToolCache)
        return cachedAnthropicTools
}

// getOpenAITools 保留旧接口兼容（内部现在委托给注册中心）
func getOpenAITools() []map[string]interface{} {
        return getOpenAIToolsFromRegistry()
}

// getAnthropicTools 保留旧接口兼容（内部现在委托给注册中心）
func getAnthropicTools() []map[string]interface{} {
        return getAnthropicToolsFromRegistry()
}

// getToolName 从工具定义中提取工具名称
func getToolName(tool map[string]interface{}) string {
        // OpenAI/Ollama 格式: {"type": "function", "function": {"name": "xxx"}}
        if function, ok := tool["function"].(map[string]interface{}); ok {
                if name, ok := function["name"].(string); ok {
                        return name
                }
        }
        // Anthropic 格式: {"name": "xxx", "input_schema": {...}}
        if name, ok := tool["name"].(string); ok {
                return name
        }
        return ""
}

// convertOpenAIToolToAnthropic 将 OpenAI 格式的工具转换为 Anthropic 格式
func convertOpenAIToolToAnthropic(tool map[string]interface{}) map[string]interface{} {
        function, _ := tool["function"].(map[string]interface{})
        if function == nil {
                return tool
        }

        name, _ := function["name"].(string)
        description, _ := function["description"].(string)
        parameters, _ := function["parameters"].(map[string]interface{})

        // 构建 Anthropic 格式工具
        anthropicTool := map[string]interface{}{
                "name":         name,
                "description":  description,
                "input_schema": parameters,
        }

        return anthropicTool
}

// convertToolsToAnthropic 将工具列表从 OpenAI 格式转换为 Anthropic 格式
func convertToolsToAnthropic(tools []map[string]interface{}) []map[string]interface{} {
        result := make([]map[string]interface{}, len(tools))
        for i, tool := range tools {
                result[i] = convertOpenAIToolToAnthropic(tool)
        }
        return result
}

// getFilteredTools 根据角色权限与工具配置过滤工具列表
// role 为 nil 时返回所有工具（但仍受工具配置限制）
func getFilteredTools(apiType string, role *Role) interface{} {
        return getFilteredToolsWithContext(apiType, role, 0)
}

// getFilteredToolsWithContext 带上下文窗口感知的工具过滤
// contextWindow > 0 时启用分层工具管理和提示密度裁剪（所有 API 类型统一支持）
// contextWindow == 0 时行为与 getFilteredTools 完全一致（向后兼容，返回全部工具）
// Anthropic 使用原生格式数据源，不做 OpenAI→Anthropic 格式转换
func getFilteredToolsWithContext(apiType string, role *Role, contextWindow int) interface{} {
        var tools interface{}

        // ── P3: 工具分發 — 若已配置分發規則，從中抽樣工具子集 ──────
        var sampledToolNames []string
        if globalToolDistributionMgr != nil {
                if sampled := globalToolDistributionMgr.SampleToolset(); sampled != nil && len(sampled.ToolNames) > 0 {
                        sampledToolNames = sampled.ToolNames
                }
        }

        // 如果提供了上下文窗口信息，使用分层工具管理
        // 根據 API 類型選擇對應格式的原生數據源，避免格式轉換
        if contextWindow > 0 {
                if apiType == "anthropic" {
                        tools = getFilteredAnthropicTools(contextWindow, role)
                } else {
                        tools = getFilteredOpenAITools(contextWindow, role)
                }
        } else {
                // 向後兼容：無上下文窗口信息時返回全部工具
                tools = getTools(apiType)
        }

        // 首先根据工具配置过滤
        tools = filterToolsByConfig(apiType, tools)

        // ── P3: 應用工具分發抽樣結果 ──────────────────────────────────
        if len(sampledToolNames) > 0 {
                tools = applyToolDistributionFilter(apiType, tools, sampledToolNames)
        }

        // 如果没有角色或权限模式为 all，返回过滤后的工具
        if role == nil || role.ToolPermission.Mode == ToolPermissionAll {
                // 添加 MCP 客户端工具与记忆整合工具
                return appendDynamicTools(apiType, tools)
        }

        // 处理工具过滤（两种格式逻辑相同）
        toolList, ok := tools.([]map[string]interface{})
        if !ok {
                return tools
        }
        filtered := make([]map[string]interface{}, 0)
        for _, tool := range toolList {
                name := getToolName(tool)
                if role.IsToolAllowed(name) {
                        filtered = append(filtered, tool)
                }
        }
        // 添加 MCP 客户端工具与记忆整合工具
        return appendDynamicTools(apiType, filtered)
}

// filterToolsByConfig 根据工具配置过滤工具列表
func filterToolsByConfig(apiType string, tools interface{}) interface{} {
        // 需要过滤的工具名称
        disabledTools := make(map[string]bool)

        // 检查 smart_shell 是否启用
        if globalToolsConfig.SmartShell.Enabled != nil && !*globalToolsConfig.SmartShell.Enabled {
                disabledTools["smart_shell"] = true
        }

        // 检查 shell 是否启用
        if !globalToolsConfig.Shell.Enabled {
                disabledTools["shell"] = true
        }

        // 检查 shell_delayed 及相关工具是否启用
        if !globalToolsConfig.ShellDelayed.Enabled {
                disabledTools["shell_delayed"] = true
                disabledTools["shell_delayed_check"] = true
                disabledTools["shell_delayed_wait"] = true
                disabledTools["shell_delayed_terminate"] = true
                disabledTools["shell_delayed_list"] = true
                disabledTools["shell_delayed_remove"] = true
        }

        // 预先计算 opencli 状态（isOpenCLIAvailable 已有 sync.Once 缓存，但此处短路避免重复调用）
        opencliAvail := isOpenCLIAvailable()
        disableBrowser := opencliAvail && DisableBrowserTools

        if disableBrowser {
                log.Println("[Tools] opencli is available, disabling browser_* tools")
        }

        // 如果未有需要过滤的工具，直接返回
        if len(disabledTools) == 0 && !disableBrowser {
                return tools
        }

        // 处理工具过滤（两种格式逻辑相同）
        toolList, ok := tools.([]map[string]interface{})
        if !ok {
                return tools
        }
        filtered := make([]map[string]interface{}, 0, len(toolList))
        for _, tool := range toolList {
                name := getToolName(tool)
                // 检查是否需要禁用（短路求值：先检查 disabledTools map，O(1)）
                shouldDisable := disabledTools[name] || (disableBrowser && strings.HasPrefix(name, "browser_") && name != "browser_search")
                if !shouldDisable {
                        filtered = append(filtered, tool)
                }
        }
        return filtered
}

// appendDynamicTools 添加动态工具（MCP 客户端工具、记忆整合工具、Plan Mode 工具）
func appendDynamicTools(apiType string, tools interface{}) interface{} {
        toolList, ok := tools.([]map[string]interface{})
        if !ok {
                return tools
        }

        // 如果 Plan Mode 已激活，只注入 Plan Mode 专用工具（替代所有其他动态工具）
        if globalPlanMode != nil && globalPlanMode.IsActive() {
                planTools := getPlanModeToolDefinitions()

                // 過濾掉與 Plan Mode 動態工具同名的靜態工具，避免重複定義
                planToolNames := make(map[string]bool, len(planTools))
                for _, pt := range planTools {
                        if name := getToolName(pt); name != "" {
                                planToolNames[name] = true
                        }
                }

                // 【Plan A】根據當前 Phase 物理移除禁止的工具
                // 核心思路：模型看不到被禁止的工具就不會嘗試調用
                phase := globalPlanMode.CurrentPhase()
                phaseBlocked := getBlockedToolsForPlanPhase(phase)

                filtered := make([]map[string]interface{}, 0, len(toolList))
                for _, t := range toolList {
                        name := getToolName(t)
                        if planToolNames[name] {
                                continue // 動態工具已覆蓋，跳過同名靜態工具
                        }
                        if phaseBlocked[name] {
                                continue // 當前 Phase 禁止的工具
                        }
                        if strings.HasPrefix(name, "browser_") {
                                continue // 瀏覽器工具在 Plan Mode 中不需要
                        }
                        filtered = append(filtered, t)
                }
                log.Printf("[PlanMode] Phase %d: 過濾後工具數 %d（原始 %d，移除 %d）",
                        phase, len(filtered), len(toolList), len(toolList)-len(filtered))

                if apiType == "anthropic" {
                        planTools = convertToolsToAnthropic(planTools)
                }
                filtered = append(filtered, planTools...)
                return filtered
        }

        // 获取动态工具（OpenAI 格式）
        var dynamicTools []map[string]interface{}
        if globalMCPClientManager != nil {
                dynamicTools = append(dynamicTools, globalMCPClientManager.GetAllTools()...)
        }
        dynamicTools = append(dynamicTools, GetConsolidationTools()...)

        // 如果是 Anthropic 格式，需要转换工具格式
        if apiType == "anthropic" {
                dynamicTools = convertToolsToAnthropic(dynamicTools)
        }

        // 添加动态工具
        toolList = append(toolList, dynamicTools...)

        // 如果規劃模式未啟用，從工具列表中移除 Plan Mode 相關靜態工具
        // 動態工具（next_phase, plan_write, plan_read）僅在 Plan Mode 激活時注入，此處無需處理
        if !globalPlanModeEnabled && !(globalPlanMode != nil && globalPlanMode.IsActive()) {
                planModeToolNames := map[string]bool{
                        "enter_plan_mode": true,
                        "exit_plan_mode":  true,
                }
                filtered := make([]map[string]interface{}, 0, len(toolList))
                for _, t := range toolList {
                        name := getToolName(t)
                        if !planModeToolNames[name] {
                                filtered = append(filtered, t)
                        }
                }
                toolList = filtered
        }

        return toolList
}

// applyToolDistributionFilter 根據工具分發抽樣結果過濾工具列表。
// 僅保留 sampledToolNames 中包含的工具。
func applyToolDistributionFilter(apiType string, tools interface{}, sampledToolNames []string) interface{} {
        // 構建快速查找集合
        allowed := make(map[string]bool, len(sampledToolNames))
        for _, name := range sampledToolNames {
                allowed[name] = true
        }

        toolList, ok := tools.([]map[string]interface{})
        if !ok {
                return tools
        }

        filtered := make([]map[string]interface{}, 0, len(toolList))
        for _, tool := range toolList {
                name := getToolName(tool)
                if allowed[name] {
                        filtered = append(filtered, tool)
                }
        }

        // 至少保留核心工具（shell, smart_shell），避免工具集為空
        coreTools := []string{"smart_shell", "shell", "menu"}
        hasCore := false
        for _, name := range coreTools {
                if allowed[name] {
                        hasCore = true
                        break
                }
        }
        if !hasCore {
                for _, tool := range toolList {
                        name := getToolName(tool)
                        for _, core := range coreTools {
                                if name == core {
                                        filtered = append(filtered, tool)
                                        break
                                }
                        }
                }
        }

        return filtered
}

// ============================================================================
// Plan Mode 工具過濾（Plan A: 物理移除被禁止的工具）
// ============================================================================

// getBlockedToolsForPlanPhase 返回 Plan Mode 指定 Phase 中應從靜態工具列表物理移除的工具集合
// 核心設計：模型看不到被禁止的工具就不會嘗試調用，從根源消除誤調用
//
// 保留的工具：read_file_line, read_all_lines, text_search, text_grep,
//
//      memory_recall, memory_list, plugin_list, skill_list, cron_list, ssh_list,
//      profile_check, menu, exit_plan_mode, 以及各 Phase 的動態工具
func getBlockedToolsForPlanPhase(phase PlanPhase) map[string]bool {
        blocked := make(map[string]bool)

        // ── 所有 Phase 禁止 ──

        // 已在 Plan Mode 中，無需重複進入
        blocked["enter_plan_mode"] = true

        // Shell 類工具（Plan Mode 中使用 spawn 執行只讀探索任務）
        blocked["smart_shell"] = true
        blocked["shell"] = true
        blocked["shell_delayed"] = true
        blocked["shell_delayed_check"] = true
        blocked["shell_delayed_wait"] = true
        blocked["shell_delayed_terminate"] = true
        blocked["shell_delayed_list"] = true
        blocked["shell_delayed_remove"] = true

        // 文件寫入工具（Plan Mode 僅允許通過 plan_write 寫計劃文件）
        blocked["write_file_line"] = true
        blocked["write_all_lines"] = true
        blocked["append_to_file"] = true
        blocked["write_file_range"] = true
        blocked["text_replace"] = true
        blocked["text_transform"] = true

        // 記憶寫入工具
        blocked["memory_save"] = true
        blocked["memory_forget"] = true

        // 插件寫入工具（保留 plugin_list / plugin_detail / plugin_apis 等只讀工具）
        blocked["plugin_create"] = true
        blocked["plugin_load"] = true
        blocked["plugin_call"] = true
        blocked["plugin_unload"] = true
        blocked["plugin_reload"] = true
        blocked["plugin_compile"] = true
        blocked["plugin_delete"] = true

        // 技能寫入工具（保留 skill_list / skill_get / skill_stats 等只讀工具）
        blocked["skill_create"] = true
        blocked["skill_delete"] = true
        blocked["skill_load"] = true
        blocked["skill_reload"] = true
        blocked["skill_update"] = true
        blocked["skill_evaluate"] = true

        // SSH 寫入工具（保留 ssh_list）
        blocked["ssh_connect"] = true
        blocked["ssh_exec"] = true
        blocked["ssh_close"] = true

        // Cron 寫入工具（保留 cron_list / cron_status）
        blocked["cron_add"] = true
        blocked["cron_remove"] = true

        // 其他有副作用的工具
        blocked["consolidate_memory"] = true
        blocked["opencli"] = true
        blocked["spawn_cancel"] = true

        return blocked
}
