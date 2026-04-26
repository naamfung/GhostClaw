package main

import (
        "fmt"
        "log"
        "strings"
        "sync"
)

// ============================================================
// 分层工具管理系统（Tiered Tool Management）
// ============================================================
// 问题背景：GhostClaw 当前每次 API 调用发送约 80 个工具定义，
// 消耗 8K-16K token。对于低上下文窗口模型（4K-8K），这是致命的。
// 本文件实现三级工具分层管理，根据模型上下文窗口大小动态选择
// 合适数量和详细程度的工具集，确保小模型也能正常工作。
// ============================================================

// ToolTier 工具层级枚举
type ToolTier int

const (
        // ToolTierCore 核心层：5-8 个最常用工具，始终加载
        // 适用于 <8K 上下文窗口的小模型
        ToolTierCore ToolTier = iota

        // ToolTierExtended 扩展层：15-20 个基于角色的工具
        // 适用于 8K-32K 上下文窗口的中等模型
        ToolTierExtended

        // ToolTierExpert 专家层：30+ 全部工具（含浏览器、MCP 等）
        // 适用于 >=32K 上下文窗口的大模型
        ToolTierExpert
)

// PromptDensity 提示密度枚举，控制工具描述的详细程度
type PromptDensity int

const (
        // PromptDensityFull 完整描述：返回原始描述内容
        // 适用于 >=32K 上下文窗口
        PromptDensityFull PromptDensity = iota

        // PromptDensityStandard 标准描述：仅保留第一段
        // 适用于 16K-32K 上下文窗口
        PromptDensityStandard

        // PromptDensityCompact 精简描述：仅保留第一句，最多 80 字符
        // 适用于 8K-16K 上下文窗口
        PromptDensityCompact

        // PromptDensityMinimal 极简描述：仅保留工具名称
        // 适用于 <8K 上下文窗口
        PromptDensityMinimal
)

// 上下文窗口阈值常量（单位：token）
const (
        // tierThresholdCore 小于此值使用核心层
        tierThresholdCore = 8192

        // tierThresholdExtended 小于此值使用扩展层
        tierThresholdExtended = 32768

        // densityThresholdStandard 小于此值使用标准密度
        densityThresholdStandard = 32768

        // densityThresholdCompact 小于此值使用精简密度
        densityThresholdCompact = 16384

        // densityThresholdMinimal 小于此值使用极简密度
        densityThresholdMinimal = 8192

        // avgCharsPerToken 英文/代码平均每个 token 的字符数（估算值）
        avgCharsPerToken = 4

        // compactMaxChars 精简模式下描述的最大字符数
        compactMaxChars = 80

        // safetyBuffer 安全缓冲区 token 数（为历史消息和系统提示预留）
        safetyBufferTokens = 512

        // avgMessageTokens 单条消息平均 token 数（估算值）
        avgMessageTokens = 200
)

// ToolTierManager 工具分层管理器
// 根据模型上下文窗口大小，动态决定工具数量和描述详细程度
type ToolTierManager struct {
        // coreToolNames 核心工具名称集合（用于快速查找）
        coreToolSet map[string]bool

        // extendedToolNames 扩展层工具名称集合（包含核心工具）
        extendedToolSet map[string]bool
}

// ── 全局單例 ToolTierManager（工具註冊表在啟動後不變，無需每次重建）──
var (
        globalTierManager     *ToolTierManager
        globalTierManagerOnce sync.Once
)

// getGlobalTierManager 返回全局單例 ToolTierManager
// 工具註冊表在程序啟動後不會改變，因此 tier/density 分類只需計算一次。
func getGlobalTierManager() *ToolTierManager {
        globalTierManagerOnce.Do(func() {
                mgr := &ToolTierManager{
                        coreToolSet:    make(map[string]bool),
                        extendedToolSet: make(map[string]bool),
                }
                for _, name := range GetCoreToolNames() {
                        mgr.coreToolSet[name] = true
                }
                for _, name := range GetCoreToolNames() {
                        mgr.extendedToolSet[name] = true
                }
                for _, name := range GetExtendedToolNames() {
                        mgr.extendedToolSet[name] = true
                }
                globalTierManager = mgr
        })
        return globalTierManager
}

// NewToolTierManager 创建工具分层管理器（保留舊接口，內部委托給全局單例）
func NewToolTierManager() *ToolTierManager {
        return getGlobalTierManager()
}

// GetTierForContextWindow 根据上下文窗口大小确定工具层级
// <8K → Core（仅核心工具）
// <32K → Extended（核心 + 扩展工具）
// >=32K → Expert（全部工具）
func (m *ToolTierManager) GetTierForContextWindow(contextWindow int) ToolTier {
        if contextWindow < tierThresholdCore {
                return ToolTierCore
        }
        if contextWindow < tierThresholdExtended {
                return ToolTierExtended
        }
        return ToolTierExpert
}

// GetPromptDensity 根据上下文窗口大小确定提示密度
// <8K → Minimal（仅工具名）
// <16K → Compact（第一句，80字符）
// <32K → Standard（第一段）
// >=32K → Full（完整描述）
func (m *ToolTierManager) GetPromptDensity(contextWindow int) PromptDensity {
        if contextWindow < densityThresholdMinimal {
                return PromptDensityMinimal
        }
        if contextWindow < densityThresholdCompact {
                return PromptDensityCompact
        }
        if contextWindow < densityThresholdStandard {
                return PromptDensityStandard
        }
        return PromptDensityFull
}

// GetFilteredTools 根据层级、密度和角色权限过滤工具列表
// 返回经过层级筛选、描述裁剪和角色权限检查后的工具列表
func (m *ToolTierManager) GetFilteredTools(
        allTools []map[string]interface{},
        tier ToolTier,
        density PromptDensity,
        role *Role,
) []map[string]interface{} {
        filtered := make([]map[string]interface{}, 0, len(allTools))

        for _, tool := range allTools {
                // 获取工具名称
                name := getToolName(tool)
                if name == "" {
                        // 无法识别名称的工具，跳过
                        continue
                }

                // 根据层级判断是否包含此工具
                switch tier {
                case ToolTierCore:
                        if !m.coreToolSet[name] {
                                continue
                        }
                case ToolTierExtended:
                        if !m.extendedToolSet[name] {
                                continue
                        }
                case ToolTierExpert:
                        // 专家层包含所有工具，不需要层级过滤
                }

                // 角色权限检查：如果角色不允许此工具，跳过
                if role != nil && !role.IsToolAllowed(name) {
                        continue
                }

                // 根据密度裁剪工具描述
                trimmed := m.trimToolByDensity(tool, density)
                filtered = append(filtered, trimmed)
        }

        return filtered
}

// trimToolByDensity 根据提示密度裁剪工具描述
// 優化：density == Full 時直接返回原始工具（零拷貝），避免不必要的 deepCopy
// 只有需要修改描述時才做深拷貝
func (m *ToolTierManager) trimToolByDensity(
        tool map[string]interface{},
        density PromptDensity,
) map[string]interface{} {
        // 完整密度：不需要任何裁剪，直接返回原始引用（零拷貝）
        // 這是最常見的情況（200K 上下文窗口的模型），每次請求省去 ~19 次 deepCopy
        if density == PromptDensityFull {
                return tool
        }

        // 极简密度：仅保留工具名和参数（去掉描述）
        if density == PromptDensityMinimal {
                result := deepCopyTool(tool)
                name := getToolName(result)
                if name == "" {
                        return result
                }
                // OpenAI 格式: {type: "function", function: {name, description, parameters}}
                if fn, ok := result["function"].(map[string]interface{}); ok {
                        fn["description"] = name
                } else if _, hasName := result["name"]; hasName {
                        // Anthropic 格式: {name, description, input_schema}
                        result["description"] = name
                }
                return result
        }

        // Standard / Compact 密度：需要裁剪描述文本
        // 先讀取描述，如果裁剪後與原文相同，也直接返回原始引用
        var desc string
        if fn, ok := tool["function"].(map[string]interface{}); ok {
                if d, ok := fn["description"].(string); ok {
                        desc = d
                }
        }
        if desc == "" {
                if d, ok := tool["description"].(string); ok {
                        desc = d
                }
        }
        if desc == "" {
                return tool // 無描述，無需裁剪
        }

        trimmed := TrimToolDescription(desc, density)
        if trimmed == desc {
                return tool // 裁剪後與原文相同，無需拷貝
        }

        // 描述確實需要裁剪，此時才做深拷貝
        result := deepCopyTool(tool)
        // 写回裁剪后的描述（保持原有格式不变）
        if fn, ok := result["function"].(map[string]interface{}); ok {
                fn["description"] = trimmed
        } else if _, hasName := result["name"]; hasName {
                result["description"] = trimmed
        }

        return result
}

// EstimateToolTokens 估算工具定义列表的 token 消耗
// 使用简单估算：字符数 / 平均每个 token 的字符数
// 这是近似值，实际 token 数取决于模型的分词器
func (m *ToolTierManager) EstimateToolTokens(tools []map[string]interface{}) int {
        if len(tools) == 0 {
                return 0
        }

        // 将工具定义序列化为 JSON 字符串估算长度
        totalChars := 0
        for _, tool := range tools {
                totalChars += estimateToolChars(tool)
        }

        // 字符数除以平均每个 token 的字符数
        tokens := totalChars / avgCharsPerToken
        if tokens < 1 {
                tokens = 1
        }

        return tokens
}

// estimateToolChars 估算单个工具定义的字符数
func estimateToolChars(tool map[string]interface{}) int {
        chars := 0
        for key, val := range tool {
                chars += len(key)
                chars += estimateValueChars(val)
        }
        return chars
}

// estimateValueChars 递归估算 interface{} 值的字符数
func estimateValueChars(val interface{}) int {
        switch v := val.(type) {
        case string:
                return len(v)
        case map[string]interface{}:
                n := 0
                for key, subVal := range v {
                        n += len(key)
                        n += estimateValueChars(subVal)
                }
                return n
        case []interface{}:
                n := 0
                for _, item := range v {
                        n += estimateValueChars(item)
                }
                return n
        case []string:
                n := 0
                for _, s := range v {
                        n += len(s)
                }
                return n
        default:
                // 数字、布尔等基本类型
                return len(fmt.Sprint(v))
        }
}

// GetMaxHistoryMessages 动态计算最大历史消息数
// 根据上下文窗口、系统提示 token 和工具 token，计算还能容纳多少条历史消息
// 公式：(contextWindow - systemPromptTokens - toolTokens - safetyBuffer) / avgMessageTokens
func (m *ToolTierManager) GetMaxHistoryMessages(contextWindow int, systemPromptTokens int, toolTokens int) int {
        available := contextWindow - systemPromptTokens - toolTokens - safetyBufferTokens
        if available <= 0 {
                // 没有剩余空间，至少保留 1 条历史消息
                return 1
        }

        maxMessages := available / avgMessageTokens
        if maxMessages < 1 {
                return 1
        }
        return maxMessages
}

// ============================================================
// 核心工具名称列表
// ============================================================

// GetCoreToolNames 返回核心层工具名称列表
// 这些是 AI 助手最基本、最常用的工具，在任何场景下都应该可用
// 包含：Shell 执行、文件读写、文本搜索、记忆检索
func GetCoreToolNames() []string {
        return GetCoreToolNamesFromRegistry()
}

// ============================================================
// 扩展工具名称列表（在核心工具基础上追加）
// ============================================================

// GetExtendedToolNames 返回扩展层额外追加的工具名称列表
// 这些工具在核心工具之上提供文件编辑、文本操作、记忆管理、
// 定时任务、技能管理、插件和配置文件检查等扩展能力
// 注意：此列表不包含核心工具名称，仅包含增量部分
func GetExtendedToolNames() []string {
        return GetExtendedToolNamesFromRegistry()
}

// ============================================================
// 浏览器工具合并定义
// ============================================================
// 原始浏览器工具有 33 个独立工具，在小模型场景下消耗大量 token。
// 这里将它们合并为 5 个聚合工具，大幅减少 token 消耗。
// ============================================================

// GetConsolidatedBrowserTools 返回合并后的浏览器工具定义
// 将 33 个独立浏览器工具合并为 5 个聚合工具：
//   - browser_navigate: 导航/访问网页（合并 browser_visit, browser_navigate）
//   - browser_interact: 页面交互（合并 browser_click, browser_double_click,
//     browser_hover, browser_type, browser_scroll, browser_right_click, browser_drag）
//   - browser_extract: 内容提取（合并 browser_screenshot, browser_execute_js,
//     browser_extract_links, browser_extract_images, browser_extract_elements,
//     browser_snapshot, browser_element_screenshot, browser_pdf, browser_pdf_from_file）
//   - browser_form_fill: 表单填写（合并 browser_fill_form, browser_select_option,
//     browser_key_press, browser_upload_file）
//   - browser_search: 搜索引擎查询（合并 browser_search）
func GetConsolidatedBrowserTools() []map[string]interface{} {
        return []map[string]interface{}{
                // --- browser_navigate: 导航与访问 ---
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name": "browser_navigate",
                                "description": "Navigate to a URL, visit a web page, and extract its text content. Supports optional wait time for dynamic pages.",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "url": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "The URL to navigate to.",
                                                },
                                                "wait_seconds": map[string]interface{}{
                                                        "type":        "integer",
                                                        "description": "Optional wait time in seconds after page load for dynamic content. Default: 0.",
                                                },
                                        },
                                        "required":             []string{"url"},
                                        "additionalProperties": false,
                                },
                        },
                },
                // --- browser_interact: 页面交互 ---
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name": "browser_interact",
                                "description": "Interact with elements on a web page. Supports click, double-click, hover, type text, scroll, right-click, and drag. Uses CSS selectors to target elements.",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "url": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "The URL to navigate to.",
                                                },
                                                "action": map[string]interface{}{
                                                        "type":        "string",
                                                        "enum":        []string{"click", "double_click", "hover", "right_click", "type", "scroll", "drag"},
                                                        "description": "The interaction action to perform.",
                                                },
                                                "selector": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "CSS selector for the target element. Required for click, double_click, hover, right_click, drag. Example: 'button.submit', '#login-btn'.",
                                                },
                                                "text": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "Text to type (for 'type' action).",
                                                },
                                                "submit": map[string]interface{}{
                                                        "type":        "boolean",
                                                        "description": "Whether to press Enter after typing (for 'type' action). Default: false.",
                                                },
                                                "direction": map[string]interface{}{
                                                        "type":        "string",
                                                        "enum":        []string{"up", "down"},
                                                        "description": "Scroll direction (for 'scroll' action).",
                                                },
                                                "amount": map[string]interface{}{
                                                        "type":        "integer",
                                                        "description": "Pixel amount for scroll or drag offset.",
                                                },
                                        },
                                        "required":             []string{"url", "action"},
                                        "additionalProperties": false,
                                },
                        },
                },
                // --- browser_extract: 内容提取 ---
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name": "browser_extract",
                                "description": "Extract content from a web page. Supports screenshot capture, JavaScript execution, link/image extraction, element scraping, and PDF generation.",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "url": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "The URL to navigate to.",
                                                },
                                                "mode": map[string]interface{}{
                                                        "type":        "string",
                                                        "enum":        []string{"screenshot", "execute_js", "extract_links", "extract_images", "extract_elements", "snapshot", "pdf"},
                                                        "description": "The extraction mode.",
                                                },
                                                "selector": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "CSS selector for 'extract_elements' mode. Example: '.article', 'div.content p'.",
                                                },
                                                "script": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "JavaScript code for 'execute_js' mode. Must be a function expression.",
                                                },
                                                "full_page": map[string]interface{}{
                                                        "type":        "boolean",
                                                        "description": "Capture full page screenshot (for 'screenshot' mode). Default: false.",
                                                },
                                                "include_html": map[string]interface{}{
                                                        "type":        "boolean",
                                                        "description": "Include HTML content (for 'extract_elements' mode). Default: false.",
                                                },
                                        },
                                        "required":             []string{"url", "mode"},
                                        "additionalProperties": false,
                                },
                        },
                },
                // --- browser_form_fill: 表单填写 ---
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name": "browser_form_fill",
                                "description": "Fill out and submit web forms. Supports multi-field input, file uploads, select options, and key press simulation.",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "url": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "The URL to navigate to.",
                                                },
                                                "form_data": map[string]interface{}{
                                                        "type":        "object",
                                                        "description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
                                                },
                                                "submit_selector": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "CSS selector for submit button. If empty, presses Enter to submit.",
                                                },
                                                "file_path": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "Path to file for file upload fields.",
                                                },
                                                "select_value": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "Value to select for dropdown menus.",
                                                },
                                        },
                                        "required":             []string{"url", "form_data"},
                                        "additionalProperties": false,
                                },
                        },
                },
                // --- browser_search: 搜索引擎 ---
                {
                        "type": "function",
                        "function": map[string]interface{}{
                                "name": "browser_search",
                                "description": "Search for a keyword using a search engine. Returns search results with titles and links.",
                                "parameters": map[string]interface{}{
                                        "type": "object",
                                        "properties": map[string]interface{}{
                                                "keyword": map[string]interface{}{
                                                        "type":        "string",
                                                        "description": "The keyword to search for.",
                                                },
                                        },
                                        "required":             []string{"keyword"},
                                        "additionalProperties": false,
                                },
                        },
                },
        }
}

// GetConsolidatedBrowserToolsAnthropic 返回合并后的浏览器工具定义（Anthropic 原生格式）
// 将 33 个独立浏览器工具合并为 5 个聚合工具，直接生成 Anthropic 格式
// 不再依赖 convertToolsToAnthropic 转换，消除格式转换风险
func GetConsolidatedBrowserToolsAnthropic() []map[string]interface{} {
        return []map[string]interface{}{
                // --- browser_navigate: 导航与访问 ---
                {
                        "name": "browser_navigate",
                        "description": "Navigate to a URL, visit a web page, and extract its text content. Supports optional wait time for dynamic pages.",
                        "input_schema": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "url": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The URL to navigate to.",
                                        },
                                        "wait_seconds": map[string]interface{}{
                                                "type":        "integer",
                                                "description": "Optional wait time in seconds after page load for dynamic content. Default: 0.",
                                        },
                                },
                                "required":             []string{"url"},
                                "additionalProperties": false,
                        },
                },
                // --- browser_interact: 页面交互 ---
                {
                        "name": "browser_interact",
                        "description": "Interact with elements on a web page. Supports click, double-click, hover, type text, scroll, right-click, and drag. Uses CSS selectors to target elements.",
                        "input_schema": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "url": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The URL to navigate to.",
                                        },
                                        "action": map[string]interface{}{
                                                "type":        "string",
                                                "enum":        []string{"click", "double_click", "hover", "right_click", "type", "scroll", "drag"},
                                                "description": "The interaction action to perform.",
                                        },
                                        "selector": map[string]interface{}{
                                                "type":        "string",
                                                "description": "CSS selector for the target element. Required for click, double_click, hover, right_click, drag. Example: 'button.submit', '#login-btn'.",
                                        },
                                        "text": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Text to type (for 'type' action).",
                                        },
                                        "submit": map[string]interface{}{
                                                "type":        "boolean",
                                                "description": "Whether to press Enter after typing (for 'type' action). Default: false.",
                                        },
                                        "direction": map[string]interface{}{
                                                "type":        "string",
                                                "enum":        []string{"up", "down"},
                                                "description": "Scroll direction (for 'scroll' action).",
                                        },
                                        "amount": map[string]interface{}{
                                                "type":        "integer",
                                                "description": "Pixel amount for scroll or drag offset.",
                                        },
                                },
                                "required":             []string{"url", "action"},
                                "additionalProperties": false,
                        },
                },
                // --- browser_extract: 内容提取 ---
                {
                        "name": "browser_extract",
                        "description": "Extract content from a web page. Supports screenshot capture, JavaScript execution, link/image extraction, element scraping, and PDF generation.",
                        "input_schema": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "url": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The URL to navigate to.",
                                        },
                                        "mode": map[string]interface{}{
                                                "type":        "string",
                                                "enum":        []string{"screenshot", "execute_js", "extract_links", "extract_images", "extract_elements", "snapshot", "pdf"},
                                                "description": "The extraction mode.",
                                        },
                                        "selector": map[string]interface{}{
                                                "type":        "string",
                                                "description": "CSS selector for 'extract_elements' mode. Example: '.article', 'div.content p'.",
                                        },
                                        "script": map[string]interface{}{
                                                "type":        "string",
                                                "description": "JavaScript code for 'execute_js' mode. Must be a function expression.",
                                        },
                                        "full_page": map[string]interface{}{
                                                "type":        "boolean",
                                                "description": "Capture full page screenshot (for 'screenshot' mode). Default: false.",
                                        },
                                        "include_html": map[string]interface{}{
                                                "type":        "boolean",
                                                "description": "Include HTML content (for 'extract_elements' mode). Default: false.",
                                        },
                                },
                                "required":             []string{"url", "mode"},
                                "additionalProperties": false,
                        },
                },
                // --- browser_form_fill: 表单填写 ---
                {
                        "name": "browser_form_fill",
                        "description": "Fill out and submit web forms. Supports multi-field input, file uploads, select options, and key press simulation.",
                        "input_schema": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "url": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The URL to navigate to.",
                                        },
                                        "form_data": map[string]interface{}{
                                                "type":        "object",
                                                "description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
                                        },
                                        "submit_selector": map[string]interface{}{
                                                "type":        "string",
                                                "description": "CSS selector for submit button. If empty, presses Enter to submit.",
                                        },
                                        "file_path": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Path to file for file upload fields.",
                                        },
                                        "select_value": map[string]interface{}{
                                                "type":        "string",
                                                "description": "Value to select for dropdown menus.",
                                        },
                                },
                                "required":             []string{"url", "form_data"},
                                "additionalProperties": false,
                        },
                },
                // --- browser_search: 搜索引擎 ---
                {
                        "name": "browser_search",
                        "description": "Search for a keyword using a search engine. Returns search results with titles and links.",
                        "input_schema": map[string]interface{}{
                                "type": "object",
                                "properties": map[string]interface{}{
                                        "keyword": map[string]interface{}{
                                                "type":        "string",
                                                "description": "The keyword to search for.",
                                        },
                                },
                                "required":             []string{"keyword"},
                                "additionalProperties": false,
                        },
                },
        }
}

// ============================================================
// 描述裁剪函数
// ============================================================

// TrimToolDescription 根据提示密度裁剪工具描述文本
// Full: 原样返回
// Standard: 仅保留第一段（以双换行分隔）
// Compact: 仅保留第一句，最多 80 字符
// Minimal: 仅返回工具名称（此模式下描述完全省略，由 trimToolByDensity 处理）
func TrimToolDescription(desc string, density PromptDensity) string {
        switch density {
        case PromptDensityFull:
                // 完整模式：原样返回
                return desc

        case PromptDensityStandard:
                // 标准模式：仅保留第一段
                // 以双换行（\n\n）为段落分隔符
                if idx := strings.Index(desc, "\n\n"); idx > 0 {
                        return strings.TrimSpace(desc[:idx])
                }
                return strings.TrimSpace(desc)

        case PromptDensityCompact:
                // 精简模式：仅保留第一句，最长 80 字符
                sentence := extractFirstSentence(desc)
                if len(sentence) > compactMaxChars {
                        sentence = sentence[:compactMaxChars] + "..."
                }
                return sentence

        case PromptDensityMinimal:
                // 极简模式：返回空字符串（名称由调用方处理）
                return ""

        default:
                return desc
        }
}

// extractFirstSentence 提取文本的第一句话
// 支持中英文句号（. 和 。）、感叹号（! 和 ！）、问号（? 和 ？）
// 也支持以换行符作为句子结尾
func extractFirstSentence(text string) string {
        text = strings.TrimSpace(text)
        if text == "" {
                return ""
        }

        sentenceEnders := ".。!！?？"

        for i, ch := range text {
                // 遇到句子结束符，返回到此为止的内容
                if strings.ContainsRune(sentenceEnders, ch) {
                        sentence := text[:i+1]
                        return strings.TrimSpace(sentence)
                }

                // 遇到换行符（非续行），也视为句子结束
                if ch == '\n' {
                        return strings.TrimSpace(text[:i])
                }
        }

        // 没有找到句子结束符，返回全文
        return text
}

// ============================================================
// 深拷贝辅助函数
// ============================================================

// deepCopyTool 深拷贝工具定义（map[string]interface{}）
// 确保裁剪描述时不会修改原始工具定义数据
func deepCopyTool(tool map[string]interface{}) map[string]interface{} {
        result := make(map[string]interface{}, len(tool))
        for k, v := range tool {
                result[k] = deepCopyValue(v)
        }
        return result
}

// deepCopyValue 递归深拷贝 interface{} 值
func deepCopyValue(val interface{}) interface{} {
        switch v := val.(type) {
        case map[string]interface{}:
                result := make(map[string]interface{}, len(v))
                for k, subVal := range v {
                        result[k] = deepCopyValue(subVal)
                }
                return result
        case []interface{}:
                result := make([]interface{}, len(v))
                for i, item := range v {
                        result[i] = deepCopyValue(item)
                }
                return result
        case []string:
                result := make([]string, len(v))
                copy(result, v)
                return result
        default:
                // 基本类型（string, int, float64, bool, nil）不需要深拷贝
                return v
        }
}

// ============================================================
// 集成入口函数
// ============================================================

// getFilteredOpenAITools 根据 model context window 大小和角色权限，
// 返回经过分层筛选和密度裁剪后的 OpenAI 格式工具列表
// 这是对现有 getOpenAITools() 的包装，提供智能工具管理能力
func getFilteredOpenAITools(modelCtx int, role *Role) []map[string]interface{} {
        return getFilteredToolsUnified(modelCtx, role, "openai")
}

// getFilteredAnthropicTools 根據模型上下文窗口大小和角色權限，
// 返回經過分層篩選和密度裁剪後的 Anthropic 格式工具列表
// 使用原生 Anthropic 格式工具定義作為數據源，無需格式轉換
func getFilteredAnthropicTools(modelCtx int, role *Role) []map[string]interface{} {
        return getFilteredToolsUnified(modelCtx, role, "anthropic")
}

// getFilteredToolsUnified 統一的工具過濾函數
// 根據 API 類型自動選擇對應格式的數據源，消除 OpenAI/Anthropic 重複邏輯
func getFilteredToolsUnified(modelCtx int, role *Role, apiType string) []map[string]interface{} {
        manager := NewToolTierManager()
        tier := manager.GetTierForContextWindow(modelCtx)
        density := manager.GetPromptDensity(modelCtx)

        // 從註冊中心獲取對應格式的所有工具
        var allTools []map[string]interface{}
        if apiType == "anthropic" {
                allTools = getAnthropicToolsFromRegistry()
        } else {
                allTools = getOpenAIToolsFromRegistry()
        }

        // 獲取經過層級篩選、密度裁剪和角色權限檢查的工具列表
        filtered := manager.GetFilteredTools(allTools, tier, density, role)

        // 非 Expert 層級：追加通過 menu 工具加載的額外工具
        if tier != ToolTierExpert {
                loaded := GetLoadedToolNames()
                existingNames := make(map[string]bool, len(filtered))
                for _, t := range filtered {
                        existingNames[getToolName(t)] = true
                }
                for _, tool := range allTools {
                        name := getToolName(tool)
                        if name == "" {
                                continue
                        }
                        if loaded[name] && !existingNames[name] {
                                if role != nil && !role.IsToolAllowed(name) {
                                        continue
                                }
                                trimmed := manager.trimToolByDensity(tool, density)
                                filtered = append(filtered, trimmed)
                                existingNames[name] = true
                        }
                }
        }

        // 追加 menu 工具定義
        var menuTool map[string]interface{}
        if apiType == "anthropic" {
                menuTool = GetMenuToolDefinitionAnthropic()
        } else {
                menuTool = GetMenuToolDefinition()
        }
        menuTool = manager.trimToolByDensity(menuTool, density)
        filtered = append(filtered, menuTool)

        // 核心層且工具 token 超出預算 50% 時，合併瀏覽器工具
        if tier == ToolTierCore {
                estimatedTokens := manager.EstimateToolTokens(filtered)
                budget := modelCtx / 2
                if estimatedTokens > budget {
                        filtered = replaceBrowserWithConsolidated(filtered, manager, density, role, apiType)
                }
        }

        // ── 全局工具預算限制 ──────────────────────────────────────
        // 無論 context window 多大，工具定義的 token 數不超過硬上限。
        // 原因：大量工具定義（如 100 個工具 = 61KB）會導致第三方代理
        // 服務器處理延遲顯著增加（實測 75KB tools TTFB=5s vs 11KB tools TTFB=2.6s）。
        // 3000 tokens ≈ 12KB JSON，足以包含核心工具（~15-20 個）的完整描述。
        const maxToolTokens = 3000
        estimatedTokens := manager.EstimateToolTokens(filtered)
        if estimatedTokens > maxToolTokens {
                // 標記核心工具，保護它們不被移除
                coreNames := make(map[string]bool)
                for _, name := range GetCoreToolNames() {
                        coreNames[name] = true
                }
                // menu 不在 toolRegistry 中（獨立定義於 tool_menu.go），
                // 但它是系統最核心的工具入口，必須受預算保護。
                // 與 getTools.go applyToolDistributionFilter 中的 coreTools 保持一致。
                coreNames["menu"] = true
                // 計算核心工具的 token 數
                var coreTokens int
                for _, t := range filtered {
                        if coreNames[getToolName(t)] {
                                coreTokens += manager.EstimateToolTokens([]map[string]interface{}{t})
                        }
                }
                // 如果核心工具本身就超預算，只保留核心工具（至少比全部好）
                remaining := maxToolTokens - coreTokens
                if remaining < 0 {
                        remaining = 0
                }
                // 保留核心工具 + 盡量多的非核心工具
                var result []map[string]interface{}
                var extraTokens int
                for _, t := range filtered {
                        name := getToolName(t)
                        if coreNames[name] {
                                result = append(result, t)
                        } else if extraTokens < remaining {
                                tTokens := manager.EstimateToolTokens([]map[string]interface{}{t})
                                if extraTokens+tTokens <= remaining {
                                        result = append(result, t)
                                        extraTokens += tTokens
                                }
                        }
                }
                log.Printf("[ToolTier] Tool budget: %d tools (%d tokens) trimmed to %d tools (budget %d tokens)",
                        len(filtered), estimatedTokens, len(result), maxToolTokens)
                filtered = result
        }

        return filtered
}

// replaceBrowserWithConsolidated 将原始浏览器工具替换为合并版本
// 用于极小上下文窗口场景下进一步减少 token 消耗
func replaceBrowserWithConsolidated(
        tools []map[string]interface{},
        manager *ToolTierManager,
        density PromptDensity,
        role *Role,
        apiType string,
) []map[string]interface{} {
        // 获取合并后的浏览器工具（根据格式选择）
        var consolidated []map[string]interface{}
        if apiType == "anthropic" {
                consolidated = GetConsolidatedBrowserToolsAnthropic()
        } else {
                consolidated = GetConsolidatedBrowserTools()
        }

        result := make([]map[string]interface{}, 0, len(tools))
        for _, tool := range tools {
                name := getToolName(tool)
                if strings.HasPrefix(name, "browser_") {
                        continue
                }
                result = append(result, tool)
        }

        for _, ct := range consolidated {
                name := getToolName(ct)
                if role != nil && !role.IsToolAllowed(name) {
                        continue
                }
                result = append(result, manager.trimToolByDensity(ct, density))
        }

        return result
}

// replaceBrowserWithConsolidatedAnthropic 保留旧接口兼容
func replaceBrowserWithConsolidatedAnthropic(
        tools []map[string]interface{},
        manager *ToolTierManager,
        density PromptDensity,
        role *Role,
) []map[string]interface{} {
        return replaceBrowserWithConsolidated(tools, manager, density, role, "anthropic")
}

// ============================================================
// 调试与信息函数
// ============================================================

// String 返回 ToolTier 的可读字符串表示
func (t ToolTier) String() string {
        switch t {
        case ToolTierCore:
                return "Core"
        case ToolTierExtended:
                return "Extended"
        case ToolTierExpert:
                return "Expert"
        default:
                return "Unknown"
        }
}

// String 返回 PromptDensity 的可读字符串表示
func (d PromptDensity) String() string {
        switch d {
        case PromptDensityFull:
                return "Full"
        case PromptDensityStandard:
                return "Standard"
        case PromptDensityCompact:
                return "Compact"
        case PromptDensityMinimal:
                return "Minimal"
        default:
                return "Unknown"
        }
}

// GetTierInfo 返回工具层级的详细调试信息
// 包括层级名称、工具数量、提示密度、预估 token 消耗等
func (m *ToolTierManager) GetTierInfo(contextWindow int, allTools []map[string]interface{}) string {
        tier := m.GetTierForContextWindow(contextWindow)
        density := m.GetPromptDensity(contextWindow)

        var toolCount int
        switch tier {
        case ToolTierCore:
                toolCount = len(GetCoreToolNames())
        case ToolTierExtended:
                toolCount = len(GetCoreToolNames()) + len(GetExtendedToolNames())
        case ToolTierExpert:
                toolCount = len(allTools)
        }

        return fmt.Sprintf(
                "[ToolTier] contextWindow=%d tier=%s density=%s estimatedTools=%d",
                contextWindow, tier, density, toolCount,
        )
}
