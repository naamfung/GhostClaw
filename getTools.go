package main

import (
	"log"
	"strings"
)

// getTools 根据 API 类型返回对应格式的工具定义
func getTools(apiType string) interface{} {
	switch apiType {
	case "openai", "ollama":
		return getOpenAITools()
	default: // anthropic 及其他兼容格式
		return getAnthropicTools()
	}
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
	tools := getTools(apiType)

	// 首先根据工具配置过滤
	tools = filterToolsByConfig(apiType, tools)

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

	// 检查 opencli 是否可用，如果可用且配置为禁用浏览器工具，才禁用所有 browser_ 前缀的工具
	if isOpenCLIAvailable() && DisableBrowserTools {
		log.Println("[Tools] opencli is available, disabling browser_* tools")
		// 这里我们会在后续的过滤逻辑中处理 browser_ 前缀的工具
	}

	// 如果未有需要过滤的工具，直接返回
	if len(disabledTools) == 0 && !(isOpenCLIAvailable() && DisableBrowserTools) {
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
		// 检查是否需要禁用
		shouldDisable := disabledTools[name] || (isOpenCLIAvailable() && DisableBrowserTools && strings.HasPrefix(name, "browser_") && name != "browser_search")
		if !shouldDisable {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// appendDynamicTools 添加动态工具（MCP 客户端工具与记忆整合工具）
func appendDynamicTools(apiType string, tools interface{}) interface{} {
	toolList, ok := tools.([]map[string]interface{})
	if !ok {
		return tools
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

	return toolList
}
