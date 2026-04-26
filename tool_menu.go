package main

import (
        "fmt"
        "strings"
        "sync"
)

// ============================================================
// 工具菜单系统（Tool Menu System）
// ============================================================
// 为低上下文窗口模型提供可折叠的分类式工具浏览和按需加载功能。
// 替代传统的截断式工具列表，使模型能按需发现和使用工具。
//
// 设计理念：
// - 所有工具按功能分类（core, file, web, memory 等）
// - 模型通过 menu 工具按分类浏览、按需加载/卸载
// - 加载的工具在会话内常驻（后续 API 调用自动包含）
// - SSH 工具归入 core 分类
// ============================================================

// ToolCategory 工具分类定义
type ToolCategory struct {
        Name        string   // 分类名称（英文标识符）
        DisplayName string   // 显示名称
        Description string   // 分类描述
        Tools       []string // 该分类下的所有工具名称
}

// 全局工具分类注册表
// 从工具注册中心自动派生，确保与注册中心定义保持一致
var toolCategoryRegistry []ToolCategory

// initMenuCategories 从注册中心生成工具分类表
// 注意：不使用 init() 調用，因為 Go 按 filename 字母序執行 init()，
// tool_menu.go 排在 tool_registry.go 之前，此時 registry 爲空。
// 改爲在 tool_registry.go 的 init() 末尾顯式調用。
func initMenuCategories() {
        toolCategoryRegistry = GetCategoryRegistry()
}

// ============================================================
// 已加载工具的全局状态（会话级持久化）
// ============================================================

var (
        loadedToolCategories = make(map[string]bool) // 已加载的分类名称集合
        loadedToolNames      = make(map[string]bool) // 所有已加载的工具名称集合（含单独加载的）
        loadedToolsMu        sync.RWMutex
)

// GetLoadedToolNames 获取所有已加载的工具名称集合（线程安全副本）
func GetLoadedToolNames() map[string]bool {
        loadedToolsMu.RLock()
        defer loadedToolsMu.RUnlock()
        result := make(map[string]bool, len(loadedToolNames))
        for k, v := range loadedToolNames {
                result[k] = v
        }
        return result
}

// LoadToolCategory 加载指定分类的所有工具
// 返回新增加载的工具列表
func LoadToolCategory(category string) []string {
        loadedToolsMu.Lock()
        defer loadedToolsMu.Unlock()

        cat := findCategory(category)
        if cat == nil {
                return nil
        }

        loadedToolCategories[cat.Name] = true
        newlyLoaded := make([]string, 0)
        for _, tool := range cat.Tools {
                if !loadedToolNames[tool] {
                        loadedToolNames[tool] = true
                        newlyLoaded = append(newlyLoaded, tool)
                }
        }
        return newlyLoaded
}

// UnloadToolCategory 卸载指定分类的所有工具
// 返回被卸载的工具列表（排除仍被其他已加载分类包含的工具）
func UnloadToolCategory(category string) []string {
        loadedToolsMu.Lock()
        defer loadedToolsMu.Unlock()

        cat := findCategory(category)
        if cat == nil {
                return nil
        }

        delete(loadedToolCategories, cat.Name)
        unloaded := make([]string, 0)
        for _, tool := range cat.Tools {
                if !isToolInOtherLoadedCategory(tool, category) {
                        delete(loadedToolNames, tool)
                        unloaded = append(unloaded, tool)
                }
        }
        return unloaded
}

// LoadSingleTool 加载单个工具（按工具名）
func LoadSingleTool(toolName string) bool {
        loadedToolsMu.Lock()
        defer loadedToolsMu.Unlock()

        if !isToolInAnyCategory(toolName) {
                return false
        }
        loadedToolNames[toolName] = true
        return true
}

// UnloadSingleTool 卸载单个工具
func UnloadSingleTool(toolName string) bool {
        loadedToolsMu.Lock()
        defer loadedToolsMu.Unlock()

        if !loadedToolNames[toolName] {
                return false
        }
        delete(loadedToolNames, toolName)
        return true
}

// ResetLoadedTools 重置所有已加载的工具状态
// 在新会话（/new 命令）时调用
func ResetLoadedTools() {
        loadedToolsMu.Lock()
        defer loadedToolsMu.Unlock()
        loadedToolCategories = make(map[string]bool)
        loadedToolNames = make(map[string]bool)
}

// ============================================================
// 内部辅助函数
// ============================================================

// findCategory 根据名称查找分类（支持英文名和中文显示名，不区分大小写）
func findCategory(name string) *ToolCategory {
        nameLower := strings.ToLower(name)
        for i := range toolCategoryRegistry {
                cat := &toolCategoryRegistry[i]
                if strings.ToLower(cat.Name) == nameLower ||
                        strings.ToLower(cat.DisplayName) == nameLower {
                        return cat
                }
        }
        return nil
}

// isToolInAnyCategory 检查工具是否存在于任何分类中
func isToolInAnyCategory(toolName string) bool {
        for _, cat := range toolCategoryRegistry {
                for _, t := range cat.Tools {
                        if t == toolName {
                                return true
                        }
                }
        }
        return false
}

// isToolInOtherLoadedCategory 检查工具是否存在于除指定分类外的其他已加载分类中
// 需要在已持有 loadedToolsMu 写锁时调用
func isToolInOtherLoadedCategory(toolName string, excludeCategory string) bool {
        for _, cat := range toolCategoryRegistry {
                if cat.Name == excludeCategory {
                        continue
                }
                if !loadedToolCategories[cat.Name] {
                        continue
                }
                for _, t := range cat.Tools {
                        if t == toolName {
                                return true
                        }
                }
        }
        return false
}

// buildToolDescriptionMap 从注册中心构建「工具名→描述」映射
func buildToolDescriptionMap() map[string]string {
        tools := GetRegistryTools()
        result := make(map[string]string, len(tools))
        for _, td := range tools {
                result[td.Name] = td.Description
        }
        return result
}

// ============================================================
// Menu 工具定义（OpenAI 格式）
// ============================================================

// GetMenuToolDefinition 返回 menu 工具的 OpenAI 格式定义
func GetMenuToolDefinition() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name": "menu",
                        "description": `分层浏览和加载可用工具。

使用方式（类似目录导航）：
1. 首次调用不带参数或 action="root"，查看所有工具分类（根目录）
2. 使用 action="show" target="<分类名>" 展开某个分类，查看该分类下的具体工具列表
3. 使用 action="load" target="<分类名或工具名>" 将分类或单个工具加载到当前会话
4. 使用 action="unload" target="<分类名或工具名>" 从会话中移除

典型工作流：
- menu() → 查看有哪些分类
- menu(action="show", target="web") → 查看 web 分类下有哪些工具
- menu(action="load", target="web") → 加载整个 web 分类
- menu(action="load", target="browser_search") → 仅加载单个工具`,
                        "parameters": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "action": map[string]interface{}{
                                                "type":        "string",
                                                "enum":        []string{"root", "show", "load", "unload"},
                                                "description": "操作类型。root: 查看根分类列表（默认）；show: 展开分类查看工具；load: 加载分类/工具；unload: 卸载分类/工具。",
                                        },
                                        "target": map[string]interface{}{
                                                "type":        "string",
                                                "description": "分类名或工具名（show/load/unload 时使用）。例如 'web', 'file', 'browser_search'。",
                                        },
                                },
                                "required":             []string{},
                                "additionalProperties": false,
                        },
                },
        }
}

// GetMenuToolDefinitionAnthropic 返回 menu 工具的 Anthropic 原生格式定义
func GetMenuToolDefinitionAnthropic() map[string]interface{} {
        return map[string]interface{}{
                "name": "menu",
                "description": `分层浏览和加载可用工具。

使用方式（类似目录导航）：
1. 首次调用不带参数或 action="root"，查看所有工具分类（根目录）
2. 使用 action="show" target="<分类名>" 展开某个分类，查看该分类下的具体工具列表
3. 使用 action="load" target="<分类名或工具名>" 将分类或单个工具加载到当前会话
4. 使用 action="unload" target="<分类名或工具名>" 从会话中移除

典型工作流：
- menu() → 查看有哪些分类
- menu(action="show", target="web") → 查看 web 分类下有哪些工具
- menu(action="load", target="web") → 加载整个 web 分类
- menu(action="load", target="browser_search") → 仅加载单个工具`,
                "input_schema": map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "action": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"root", "show", "load", "unload"},
                                        "description": "操作类型。root: 查看根分类列表（默认）；show: 展开分类查看工具；load: 加载分类/工具；unload: 卸载分类/工具。",
                                },
                                "target": map[string]interface{}{
                                        "type":        "string",
                                        "description": "分类名或工具名（show/load/unload 时使用）。例如 'web', 'file', 'browser_search'。",
                                },
                        },
                        "required":             []string{},
                        "additionalProperties": false,
                },
        }
}

// ============================================================
// Menu 工具执行逻辑
// ============================================================

// executeMenuTool 执行 menu 工具调用，返回文本结果
func executeMenuTool(argsMap map[string]interface{}) string {
        action, _ := argsMap["action"].(string)
        target, _ := argsMap["target"].(string)

        // 无参数或 action 为空/root，显示根分类列表
        if action == "" || action == "root" {
                return menuRoot()
        }

        switch strings.ToLower(action) {
        case "show":
                return menuShow(target)
        case "load":
                return menuLoad(target)
        case "unload":
                return menuUnload(target)
        default:
                return fmt.Sprintf("Error: Unknown action '%s'. Use 'root', 'show', 'load', or 'unload'.", action)
        }
}

// menuRoot 显示所有根分类（工具大类），类似目录浏览的顶层
func menuRoot() string {
        var sb strings.Builder
        sb.WriteString("=== 工具分类（根目录） ===\n\n")
        sb.WriteString("以下是可用的工具大类，使用 menu(action=\"show\", target=\"<分类名>\") 展开查看具体工具。\n")

        loadedToolsMu.RLock()
        defer loadedToolsMu.RUnlock()

        for _, cat := range toolCategoryRegistry {
                status := "  "
                if loadedToolCategories[cat.Name] {
                        status = "[L]"
                }

                // 构建显示名称：优先使用 DisplayName，否则用 Name
                displayName := cat.DisplayName
                if displayName == "" {
                        displayName = cat.Name
                }

                sb.WriteString(fmt.Sprintf("\n  %s %-12s %-20s (%2d 工具)  %s\n",
                        status, cat.Name, displayName, len(cat.Tools), cat.Description))

                // 列出该分类下的代表性工具（最多 8 个），帮助模型快速定位
                previewCount := len(cat.Tools)
                if previewCount > 8 {
                        previewCount = 8
                }
                sb.WriteString("       工具: ")
                for i := 0; i < previewCount; i++ {
                        if i > 0 {
                                sb.WriteString(", ")
                        }
                        sb.WriteString(cat.Tools[i])
                }
                if len(cat.Tools) > 8 {
                        sb.WriteString(fmt.Sprintf(", ... 共 %d 个", len(cat.Tools)))
                }
                sb.WriteString("\n")
        }

        sb.WriteString("\n指令说明:\n")
        sb.WriteString("  menu(action=\"show\", target=\"<分类名>\")  展开分类，查看其中包含的工具\n")
        sb.WriteString("  menu(action=\"load\", target=\"<分类名>\")  加载整个分类到会话\n")
        sb.WriteString("  menu(action=\"load\", target=\"<工具名>\")  加载单个工具\n")
        sb.WriteString("  menu(action=\"unload\", target=\"<分类名>\") 卸载分类\n")
        sb.WriteString("\n[L]=已加载  空白=未加载. 加载的工具在后续对话中常驻可用。\n")

        return sb.String()
}

// menuShow 展示指定分类的所有工具及简短描述（展开子菜单）
func menuShow(target string) string {
        if target == "" {
                return "Error: 'target' parameter is required for 'show' action. Example: menu(action=\"show\", target=\"web\")."
        }

        cat := findCategory(target)
        if cat == nil {
                available := make([]string, 0, len(toolCategoryRegistry))
                for _, c := range toolCategoryRegistry {
                        available = append(available, c.Name)
                }
                return fmt.Sprintf("Error: Unknown category '%s'. Available categories: %s",
                        target, strings.Join(available, ", "))
        }

        // 构建工具描述映射
        descMap := buildToolDescriptionMap()

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("=== %s (%s) 工具列表 ===\n", cat.DisplayName, cat.Name))
        sb.WriteString(fmt.Sprintf("描述: %s\n\n", cat.Description))

        loadedToolsMu.RLock()
        defer loadedToolsMu.RUnlock()

        loadedCount := 0
        for _, toolName := range cat.Tools {
                status := "  "
                if loadedToolNames[toolName] || loadedToolCategories[cat.Name] {
                        status = "[L]"
                        loadedCount++
                }

                desc := "(无描述)"
                if d, ok := descMap[toolName]; ok {
                        firstSentence := extractFirstSentence(d)
                        if len(firstSentence) > 60 {
                                desc = firstSentence[:57] + "..."
                        } else {
                                desc = firstSentence
                        }
                }

                sb.WriteString(fmt.Sprintf("  %s %-35s %s\n", status, toolName, desc))
        }

        sb.WriteString(fmt.Sprintf("\n已加载: %d/%d 个工具\n", loadedCount, len(cat.Tools)))
        sb.WriteString(fmt.Sprintf("如需加载此分类的全部工具: menu(action=\"load\", target=\"%s\")\n", cat.Name))
        sb.WriteString(fmt.Sprintf("或加载单个工具: menu(action=\"load\", target=\"<工具名>\")\n"))

        return sb.String()
}

// menuLoad 加载指定分类或单个工具
func menuLoad(target string) string {
        if target == "" {
                return "Error: 'target' parameter is required for 'load' action. Example: menu(action=\"load\", target=\"web\")."
        }

        // 优先匹配分类名
        cat := findCategory(target)
        if cat != nil {
                newlyLoaded := LoadToolCategory(cat.Name)
                if len(newlyLoaded) == 0 {
                        return fmt.Sprintf("分类「%s」(%s) 的所有工具已加载，无需重复操作。",
                                cat.DisplayName, cat.Name)
                }
                return fmt.Sprintf("已加载分类「%s」(%s)，新增 %d 个工具:\n  %s\n\n这些工具将在后续对话中可用。",
                        cat.DisplayName, cat.Name, len(newlyLoaded), strings.Join(newlyLoaded, ", "))
        }

        // 尝试作为单个工具加载
        if LoadSingleTool(target) {
                return fmt.Sprintf("已加载工具「%s」。该工具将在后续对话中可用。", target)
        }

        return fmt.Sprintf("Error: 未找到分类或工具「%s」。使用 menu(action=\"root\") 查看可用分类，或 menu(action=\"show\", target=\"<分类>\") 查看分类下的工具。", target)
}

// menuUnload 卸载指定分类或单个工具
func menuUnload(target string) string {
        if target == "" {
                return "Error: 'target' parameter is required for 'unload' action. Example: menu(action=\"unload\", target=\"web\")."
        }

        // 优先匹配分类名
        cat := findCategory(target)
        if cat != nil {
                unloaded := UnloadToolCategory(cat.Name)
                if len(unloaded) == 0 {
                        return fmt.Sprintf("分类「%s」(%s) 当前未加载。", cat.DisplayName, cat.Name)
                }
                return fmt.Sprintf("已卸载分类「%s」(%s)，移除 %d 个工具:\n  %s",
                        cat.DisplayName, cat.Name, len(unloaded), strings.Join(unloaded, ", "))
        }

        // 尝试作为单个工具卸载
        if UnloadSingleTool(target) {
                return fmt.Sprintf("已卸载工具「%s」。", target)
        }

        return fmt.Sprintf("Error: 工具「%s」当前未加载。使用 menu(action=\"root\") 查看状态。", target)
}
