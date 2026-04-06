package main

import (
        "context"
        "fmt"
        "os"
        "path/filepath"
        "strings"

        "github.com/toon-format/toon-go"
)

// 插件工具处理函数

func handlePluginList(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        if globalPluginManager == nil {
                return "Error: plugin manager not initialized. Please restart the application.", false
        }
        plugins := globalPluginManager.ListPlugins()
        if len(plugins) == 0 {
                return "No plugins loaded.", false
        }
        data, err := toon.Marshal(plugins)
        if err != nil {
                return fmt.Sprintf("Error marshaling plugin list: %v", err), false
        }
        return string(data), false
}

func handlePluginCreate(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_create(name=\"my_plugin\")", false
        }
        description, _ := argsMap["description"].(string)

        if globalPluginManager == nil {
                return "Error: plugin manager not initialized", false
        }
        // 检查是否已存在
        plugins := globalPluginManager.ListPlugins()
        for _, p := range plugins {
                if p["name"] == name {
                        return fmt.Sprintf("Error: plugin '%s' already exists. Use plugin_reload or plugin_delete first.", name), false
                }
        }

        // 生成模板
        template := `-- Plugin: ` + name + "\n"
        if description != "" {
                template += `-- Description: ` + description + "\n"
        }
        template += `
-- This is a template for a GhostClaw Lua plugin.
-- You can define any number of functions, and call them via plugin_call.
-- Use ghostclaw.log(level, msg) to log messages.
-- Use ghostclaw.call_tool(tool_name, args_table) to invoke GhostClaw tools.

-- Example function:
function hello(name)
    ghostclaw.log("info", "hello called with name: " .. name)
    return "Hello, " .. name .. " from plugin " .. "` + name + `"
end

-- Add your own functions below.
-- function my_function(param1, param2)
--     -- your code here
--     return result
-- end
`

        if err := globalPluginManager.LoadPlugin(name, template, ""); err != nil {
                return fmt.Sprintf("Error creating plugin: %v\nPlease check the plugin name and try again.", err), false
        }

        return fmt.Sprintf("Plugin '%s' created successfully and loaded. You can now call its functions via plugin_call.\nFile location: %s\nExample: plugin_call(plugin=\"%s\", function=\"hello\", args=[\"World\"])",
                name, filepath.Join(globalPluginManager.pluginsDir, name, name+".lua"), name), false
}

func handlePluginLoad(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_load(name=\"my_plugin\", code=\"...\")", false
        }
        code, ok := argsMap["code"].(string)
        if !ok || code == "" {
                return "Error: missing or invalid 'code' parameter. Provide the Lua code as a string.", false
        }

        if err := globalPluginManager.LoadPlugin(name, code, ""); err != nil {
                return fmt.Sprintf("Error loading plugin: %v\nCheck the Lua code for syntax errors.", err), false
        }
        return fmt.Sprintf("Plugin '%s' loaded successfully.", name), false
}

func handlePluginUnload(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_unload(name=\"my_plugin\")", false
        }
        if err := globalPluginManager.UnloadPlugin(name); err != nil {
                return fmt.Sprintf("Error unloading plugin: %v\nMake sure the plugin is loaded.", err), false
        }
        return fmt.Sprintf("Plugin '%s' unloaded (files remain).", name), false
}

// handlePluginDelete 完全删除插件（包括文件夹和文件）
func handlePluginDelete(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_delete(name=\"my_plugin\")", false
        }
        if err := globalPluginManager.DeletePlugin(name); err != nil {
                return fmt.Sprintf("Error deleting plugin: %v", err), false
        }
        return fmt.Sprintf("Plugin '%s' deleted successfully (folder removed).", name), false
}

func handlePluginReload(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_reload(name=\"my_plugin\")", false
        }
        if err := globalPluginManager.ReloadPlugin(name); err != nil {
                return fmt.Sprintf("Error reloading plugin: %v\nMake sure the plugin exists and the file is readable.", err), false
        }
        return fmt.Sprintf("Plugin '%s' reloaded.", name), false
}

func handlePluginCall(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["plugin"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'plugin' parameter. Example: plugin_call(plugin=\"my_plugin\", function=\"hello\", args=[\"World\"])", false
        }
        funcName, ok := argsMap["function"].(string)
        if !ok || funcName == "" {
                return "Error: missing or invalid 'function' parameter. Provide the Lua function name.", false
        }

        var args []interface{}
        if argsRaw, exists := argsMap["args"]; exists {
                switch v := argsRaw.(type) {
                case []interface{}:
                        args = v
                case map[string]interface{}:
                        args = []interface{}{v}
                case string:
                        var parsed interface{}
                        if err := toon.Unmarshal([]byte(v), &parsed); err == nil {
                                if arr, ok := parsed.([]interface{}); ok {
                                        args = arr
                                } else {
                                        args = []interface{}{parsed}
                                }
                        } else {
                                args = []interface{}{v}
                        }
                default:
                        args = []interface{}{v}
                }
        }

        result, err := globalPluginManager.CallPluginFunction(ctx, name, funcName, args...)
        if err != nil {
                return fmt.Sprintf("Error calling plugin function: %v\nCheck that the function exists and arguments are correct.", err), false
        }
        return result, false
}

// handlePluginCompile 编译Lua代码（语法检查）
func handlePluginCompile(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        name, ok := argsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing or invalid 'name' parameter. Example: plugin_compile(name=\"my_plugin\")", false
        }
        code, hasCode := argsMap["code"].(string)

        var source string
        if hasCode && code != "" {
                source = code
        } else {
                // 从已存在的插件读取源代码
                pluginPath := filepath.Join(globalPluginManager.pluginsDir, name, name+".lua")
                data, err := os.ReadFile(pluginPath)
                if err != nil {
                        return fmt.Sprintf("Error reading plugin source: %v\nMake sure the plugin exists.", err), false
                }
                source = string(data)
        }

        // 编译检查
        err := globalPluginManager.CompilePlugin(name, source)
        if err != nil {
                return fmt.Sprintf("Compilation error: %v\nFix the syntax errors and try again.", err), false
        }
        return fmt.Sprintf("Plugin '%s' compiled successfully (syntax OK).", name), false
}

// handlePluginAPIs 处理plugin_apis工具调用，返回插件系统的内部接口信息
func handlePluginAPIs(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	// 构建插件系统内部接口文档
	apiDocs := map[string]interface{}{
		"title": "GhostClaw Plugin System API Documentation",
		"version": "1.0.0",
		"description": "This document provides information about the internal APIs available to plugins in GhostClaw.",
		"apis": map[string]interface{}{
			"Lua Standard Library": "All standard Lua 5.4 libraries are available, including string, table, math, etc.",
			"GhostClaw Core APIs": map[string]interface{}{
				"print(...)" : "Print messages to the console.",
				"log(...)" : "Log messages to the system log.",
				"error(...)" : "Throw an error.",
				"upload_multipart(path, url, method, file_field)": "Upload a file using multipart/form-data format. Returns (success, response).",
				"upload_raw(path, url, method, content_type)": "Upload a file using raw data format. Returns (success, response).",
				"upload_file(path, url, method, content_type, file_field)": "Unified upload function that automatically selects upload method based on content_type. Returns (success, response).",
				"download_file(url, save_path, headers)": "Download a file from URL to local path. Uses GET method. Optional headers table for custom headers like User-Agent. Returns (success, message).",
				"toon_encode(table)": "Encode a Lua table to TOON format. Returns TOON string or (nil, error).",
				"toon_decode(str)": "Decode TOON format string to Lua table. Returns Lua table or (nil, error).",
				"toon_read_file(path)": "Read TOON file and decode to Lua table. Returns Lua table or (nil, error).",
				"toon_write_file(path, table)": "Encode Lua table to TOON format and write to file. Returns (true) or (false, error).",
			},
			"Plugin Return Format": "Plugins should return a table containing all exported functions.",
			"Function Call Convention": "Functions can accept multiple arguments and return multiple values.",
			"File Operations": "Use standard Lua io library for file operations.",
			"System Commands": "Use io.popen() to execute system commands.",
			"HTTP Requests": "Use curl or other command-line tools via io.popen() for HTTP requests.",
			"Error Handling": "Return error information as part of the function return values.",
			"Best Practices": []string{
				"Keep plugins focused on a single task",
				"Document all functions with comments",
				"Handle errors gracefully",
				"Test plugins thoroughly",
				"Use descriptive function names",
				"Limit external dependencies",
			},
			"Example Plugin Structure": `-- Example Plugin Structure
local function hello(name)
    return "Hello, " .. name .. "!"
end

local function add(a, b)
    return a + b
end

return {
    hello = hello,
    add = add
}`,
		},
		"plugin_calls": map[string]interface{}{
			"plugin_list": "List all available plugins.",
			"plugin_create": "Create a new plugin with the given name and code.",
			"plugin_load": "Load or reload a plugin from code.",
			"plugin_unload": "Unload a plugin from memory.",
			"plugin_reload": "Reload a plugin from its file.",
			"plugin_call": "Call a function in a plugin with arguments.",
			"plugin_compile": "Compile a plugin for syntax checking.",
			"plugin_delete": "Delete a plugin and its files.",
			"plugin_apis": "Show this API documentation.",
			"plugin_detail": "Get detailed information about a specific plugin.",
		},
	}
	
	// 将API文档转换为TOON格式
	apiDocsTOON, err := toon.Marshal(apiDocs)
	if err != nil {
		return "Error: failed to generate API documentation", false
	}
	
	return string(apiDocsTOON), false
}

// handlePluginDetail 处理plugin_detail工具调用，返回插件的详细信息
func handlePluginDetail(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
	name, ok := argsMap["name"].(string)
	if !ok || name == "" {
		return "Error: missing or invalid 'name' parameter. Example: plugin_detail(name=\"temp_uploader\")", false
	}
	includeSource, _ := argsMap["include_source"].(bool)

	if globalPluginManager == nil {
		return "Error: plugin manager not initialized. Please restart the application.", false
	}

	// 检查插件是否存在
	plugins := globalPluginManager.ListPlugins()
	var targetPlugin map[string]interface{}
	for _, p := range plugins {
		if p["name"] == name {
			targetPlugin = p
			break
		}
	}

	if targetPlugin == nil {
		return fmt.Sprintf("Error: plugin '%s' not found. Use plugin_list to see available plugins.", name), false
	}

	// 构建插件详情
	detail := map[string]interface{}{
		"name":        name,
		"description": targetPlugin["description"],
		"path":        targetPlugin["path"],
		"functions":   []string{},
	}

	// 尝试读取插件源代码以获取函数列表
	pluginPath := filepath.Join(globalPluginManager.pluginsDir, name, name+".lua")
	data, err := os.ReadFile(pluginPath)
	if err == nil {
		source := string(data)
		
		// 简单解析Lua函数定义
		var functions []string
		lines := strings.Split(source, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "function ") {
				funcName := strings.TrimPrefix(line, "function ")
				funcName = strings.Split(funcName, "(")[0]
				funcName = strings.TrimSpace(funcName)
				if funcName != "" {
					functions = append(functions, funcName)
				}
			} else if strings.HasPrefix(line, "local function ") {
				funcName := strings.TrimPrefix(line, "local function ")
				funcName = strings.Split(funcName, "(")[0]
				funcName = strings.TrimSpace(funcName)
				if funcName != "" {
					functions = append(functions, funcName)
				}
			}
		}
		detail["functions"] = functions

		if includeSource {
			detail["source"] = source
		}
	}

	// 转换为TOON格式
	detailTOON, err := toon.Marshal(detail)
	if err != nil {
		return fmt.Sprintf("Error: failed to generate plugin details: %v", err), false
	}

	return string(detailTOON), false
}

// callToolInternal 执行一个工具并返回结果字符串（无流式输出）
func callToolInternal(ctx context.Context, toolName string, argsMap map[string]interface{}) (string, error) {
	dummyCh := &dummyChannel{}
	result := executeTool(ctx, "", toolName, argsMap, dummyCh, nil) // nil = 插件内部调用，跳过权限检查
	contentStr, _ := result.Content.(string)
	if result.Meta.Status == TaskStatusFailed {
		return "", fmt.Errorf("%s", contentStr)
	}
	return contentStr, nil
}

// dummyChannel 实现 Channel 接口，忽略所有写入
type dummyChannel struct{}

func (d *dummyChannel) WriteChunk(chunk StreamChunk) error { return nil }
func (d *dummyChannel) ID() string                         { return "dummy" }
func (d *dummyChannel) Close() error                       { return nil }
func (d *dummyChannel) GetSessionID() string               { return "" }
func (d *dummyChannel) HealthCheck() map[string]interface{} {
	return map[string]interface{}{
		"id":      "dummy",
		"status":  "operational",
		"message": "Dummy channel health check",
	}
}

