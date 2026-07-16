package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

// ============================================================
// Kap 容量级别工具管理系统（Kap-based Tool Management）
// ============================================================
// 问题背景：GhostClaw 当前注册约 100 个工具定义，消耗大量 token。
// 对于低上下文窗口模型（4K-8K），全量工具定义会挤占可用空间。
// 本文件实现 10 级 Kap 容量分层管理，根据模型上下文窗口大小
// 动态选择合适数量和详细程度的工具集，确保小模型也能正常工作。
//
// 10 个 Kap 级别（以 2 倍递增的上下文容量命名）：
//   4Kap → 8Kap → 16Kap → 32Kap → 64Kap → 128Kap → 256Kap → 512Kap → 1024Kap → 2048Kap
//
// 工具分配：每级工具 token 总量 <= 该级容量 × kapToolBudgetPercent（默认 1%）。
// 按工具优先级排序后贪心累加，达到预算上限则跳过该工具继续尝试更小的。
// Kap2048 为全量兜底（> 1M 上下文），不受预算限制。
// 描述密度（PromptDensity）合并到 Kap 级别，由 Density() 方法自动衍生。
// ============================================================

// ToolKap 工具容量级别枚举（10 级）
type ToolKap int

const (
	// Kap4 4K 上下文：兜底最小集
	Kap4 ToolKap = iota
	// Kap8 8K
	Kap8
	// Kap16 16K
	Kap16
	// Kap32 32K
	Kap32
	// Kap64 64K
	Kap64
	// Kap128 128K
	Kap128
	// Kap256 256K
	Kap256
	// Kap512 512K
	Kap512
	// Kap1024 1024K (1M)
	Kap1024
	// Kap2048 2048K (2M+)：全量工具（兜底，不受预算限制）
	Kap2048
)

// PromptDensity 提示密度枚举，控制工具描述的详细程度
// 现由 ToolKap.Density() 衍生，不再独立计算
type PromptDensity int

const (
	PromptDensityFull     PromptDensity = iota // 完整描述
	PromptDensityStandard                      // 标准描述（第一段）
	PromptDensityCompact                       // 精简描述（第一句，80字符）
	PromptDensityMinimal                       // 极简描述（仅工具名）
)

// Kap 容量阈值常量（单位：token）
// 语义统一用 <=：contextWindow <= kapThresholdN → KapN
// 例：contextWindow <= 16384 → Kap16；contextWindow > 1048576 → Kap2048
const (
	kapThreshold4    = 4096    // Kap4:  contextWindow <= 4K
	kapThreshold8    = 8192    // Kap8:  contextWindow <= 8K
	kapThreshold16   = 16384   // Kap16: contextWindow <= 16K
	kapThreshold32   = 32768   // Kap32: contextWindow <= 32K
	kapThreshold64   = 65536   // Kap64: contextWindow <= 64K
	kapThreshold128  = 131072  // Kap128: contextWindow <= 128K
	kapThreshold256  = 262144  // Kap256: contextWindow <= 256K
	kapThreshold512  = 524288  // Kap512: contextWindow <= 512K
	kapThreshold1024 = 1048576 // Kap1024: contextWindow <= 1024K (1M)
	// Kap2048: contextWindow > 1M（全量兜底）
)

// 估算常量
const (
	avgCharsPerToken   = 4   // 英文/代码平均每 token 字符数
	compactMaxChars    = 80  // 精简模式描述最大字符数
	safetyBufferTokens = 512 // 安全缓冲 token 数
	avgMessageTokens   = 200 // 单条消息平均 token 数
)

// kapToolBudgetPercent 控制每级工具 token 总量占该级上下文容量的百分比。
// 默认 2.0（即 128K 窗口最多用 2621 token 放工具），可全局配置覆盖。
// Kap2048 不受此限制（全量工具）。
var kapToolBudgetPercent = 2.0

// ── Kap 优先级启发式映射表 ──
// 将现有 4 桶（small/core/extended/expert）拆细到 9 级 Kap。
// key 格式 "tier:category"，value 为默认 Kap 优先级（1-9，越小越优先）。
var tierCategoryKapDefault = map[string]int{
	// small 层 → 拆分到 4Kap/8Kap/16Kap（关键工具由 override 微调）
	"small:core":   3, // 文件操作 → 16Kap（SmartShell/ReadFileLine 等由 override 调至 4Kap/8Kap）
	"small:memory": 3, // 记忆工具 → 16Kap（MemoryRecall 由 override 调至 8Kap）
	"small:web":    3, // BrowserSearch/Visit → 16Kap

	// core 层 → 拆分到 32Kap/64Kap
	"core:core":     4, // FileInfo/Todo*/Text*/SSH*/SchemeEval → 32Kap
	"core:Spawn":    4, // Spawn* → 32Kap
	"core:plan":     4, // Tasks/EnterPlanMode → 32Kap
	"core:schedule": 5, // Cron* → 64Kap（CronAdd/CronList 由 override 调至 32Kap）
	"core:skill":    5, // Skill* → 64Kap
	"core:plugin":   5, // Plugin* → 64Kap

	// extended 层 → 拆分到 128Kap/256Kap
	"extended:plan":    6, // ExitPlanMode → 128Kap
	"extended:profile": 7, // Profile*/ActorIdentity* → 256Kap

	// expert 层 → 拆分到 512Kap/1024Kap
	"expert:core": 8, // Task* → 512Kap
	"expert:web":  8, // Browser* → 512Kap（较少用的由 override 调至 1024Kap）

	// 兜底補丁：補齊 tier:category 未顯式列出的組合，避免落入 switch(tier) 兜底
	"core:misc": 4, // SchemeEval → 32Kap（與 core:core 一致）
}

// toolKapOverride 个别工具的 Kap 优先级覆盖（精细微调）
var toolKapOverride = map[string]int{
	// 4Kap：绝对最常用（2 个）
	"SmartShell":   1,
	"ReadFileLine": 1,
	// 8Kap：基本文件写入 + 记忆（4 个）
	"WriteFileLine": 2,
	"MemoryRecall":  2,
	"AppendToFile":  2,
	"ReadFileLines": 2,
	// 32Kap：高优先级 schedule 工具
	"CronAdd":  4,
	"CronList": 4,
	// 128Kap：ProfileCheck 较常用
	"ProfileCheck": 6,
	// 1024Kap：较少使用的浏览器工具
	"BrowserDoubleClick":       9,
	"BrowserRightClick":        9,
	"BrowserDrag":              9,
	"BrowserWaitSmart":         9,
	"BrowserGetCookies":        9,
	"BrowserCookieSave":        9,
	"BrowserCookieLoad":        9,
	"BrowserUploadFile":        9,
	"BrowserSelectOption":      9,
	"BrowserElementScreenshot": 9,
	"BrowserPdf":               9,
	"BrowserPdfFromFile":        9,
	"BrowserSetHeaders":        9,
	"BrowserSetUserAgent":      9,
	"BrowserEmulateDevice":     9,
	"BrowserExtractImages":     9,
	"BrowserExtractElements":   9,
}

// kapPriorityForTool 返回工具的 Kap 优先级（1-9）。
// 数值越小 = 优先级越高 = 在更小的上下文窗口中即可使用。
// 启发式：先查 toolKapOverride 精确覆盖，再按 tier:category 默认值，最后按 tier 兜底。
func kapPriorityForTool(tier, category, name string) int {
	if p, ok := toolKapOverride[name]; ok {
		return p
	}
	key := tier + ":" + category
	if p, ok := tierCategoryKapDefault[key]; ok {
		return p
	}
	switch tier {
	case "small":
		return 3
	case "core":
		return 5
	case "extended":
		return 7
	case "expert":
		return 8
	default:
		return 9
	}
}

// ── ToolKap 方法 ──

// String 返回 Kap 级别的可读名称（如 "4Kap", "1024Kap", "2048Kap"）
func (k ToolKap) String() string {
	switch k {
	case Kap4:
		return "4Kap"
	case Kap8:
		return "8Kap"
	case Kap16:
		return "16Kap"
	case Kap32:
		return "32Kap"
	case Kap64:
		return "64Kap"
	case Kap128:
		return "128Kap"
	case Kap256:
		return "256Kap"
	case Kap512:
		return "512Kap"
	case Kap1024:
		return "1024Kap"
	case Kap2048:
		return "2048Kap"
	default:
		return "Unknown"
	}
}

// Capacity 返回此 Kap 级别对应的上下文容量（单位：token）
// 用于预算计算：budget = Capacity() × kapToolBudgetPercent / 100
func (k ToolKap) Capacity() int {
	switch k {
	case Kap4:
		return kapThreshold4
	case Kap8:
		return kapThreshold8
	case Kap16:
		return kapThreshold16
	case Kap32:
		return kapThreshold32
	case Kap64:
		return kapThreshold64
	case Kap128:
		return kapThreshold128
	case Kap256:
		return kapThreshold256
	case Kap512:
		return kapThreshold512
	case Kap1024:
		return kapThreshold1024
	case Kap2048:
		return 2097152 // 2M（标称值，实际全量不受预算限制）
	default:
		return kapThreshold1024
	}
}

// Density 返回此 Kap 级别对应的提示密度
// 低 Kap → Minimal/Compact（省 token）；高 Kap → Standard/Full（完整描述）
func (k ToolKap) Density() PromptDensity {
	switch k {
	case Kap4, Kap8:
		return PromptDensityMinimal
	case Kap16, Kap32:
		return PromptDensityCompact
	case Kap64, Kap128:
		return PromptDensityStandard
	default: // Kap256, Kap512, Kap1024, Kap2048
		return PromptDensityFull
	}
}

// IsFullTools 是否包含全部工具（仅 Kap2048，全量兜底）
func (k ToolKap) IsFullTools() bool {
	return k == Kap2048
}

// ToolKapManager 工具容量级别管理器
// 根据上下文窗口大小确定 Kap 级别，并按 Kap 优先级过滤工具
type ToolKapManager struct {
	// toolKapPriority 缓存：工具名 → Kap 优先级（启动时从 registry 计算一次）
	toolKapPriority map[string]int
}

// ── 全局單例 ToolKapManager ──
var (
	globalKapManager     *ToolKapManager
	globalKapManagerOnce sync.Once
)

// getGlobalKapManager 返回全局單例 ToolKapManager
func getGlobalKapManager() *ToolKapManager {
	globalKapManagerOnce.Do(func() {
		mgr := &ToolKapManager{
			toolKapPriority: make(map[string]int, len(toolRegistry)),
		}
		for _, td := range toolRegistry {
			mgr.toolKapPriority[td.Name] = kapPriorityForTool(td.Tier, td.Category, td.Name)
		}
		globalKapManager = mgr
	})
	return globalKapManager
}

// NewToolKapManager 创建工具 Kap 管理器（委托给全局单例）
func NewToolKapManager() *ToolKapManager {
	return getGlobalKapManager()
}

// GetKapForContextWindow 根据上下文窗口大小确定 Kap 级别
// 语义统一用 <=：contextWindow <= kapThresholdN → KapN
// Kap2048 为兜底：contextWindow > 1M 时使用全量工具
func (m *ToolKapManager) GetKapForContextWindow(contextWindow int) ToolKap {
	switch {
	case contextWindow <= kapThreshold4:
		return Kap4
	case contextWindow <= kapThreshold8:
		return Kap8
	case contextWindow <= kapThreshold16:
		return Kap16
	case contextWindow <= kapThreshold32:
		return Kap32
	case contextWindow <= kapThreshold64:
		return Kap64
	case contextWindow <= kapThreshold128:
		return Kap128
	case contextWindow <= kapThreshold256:
		return Kap256
	case contextWindow <= kapThreshold512:
		return Kap512
	case contextWindow <= kapThreshold1024:
		return Kap1024
	default:
		return Kap2048
	}
}

// GetFilteredTools 根据 Kap 级别和角色权限过滤工具列表
// 自动分配算法：
//   - Kap2048（全量）：仅做角色权限检查，返回全部工具
//   - 其他级别：按优先级排序 → 裁剪描述 → 贪心累加 token
//     预算 = Capacity() × kapToolBudgetPercent / 100
//     超预算的工具跳过（continue），继续尝试后续更小的工具以最大化预算利用率
func (m *ToolKapManager) GetFilteredTools(
	allTools []map[string]interface{},
	kap ToolKap,
	role *Role,
) []map[string]interface{} {
	// Kap2048：全量工具，仅角色权限检查
	if kap.IsFullTools() {
		filtered := make([]map[string]interface{}, 0, len(allTools))
		for _, tool := range allTools {
			name := getToolName(tool)
			if name == "" {
				continue
			}
			if role != nil && !role.IsToolAllowed(name) {
				continue
			}
			filtered = append(filtered, tool)
		}
		return filtered
	}

	// 预算控制：该级容量 × 百分比
	budgetTokens := int(float64(kap.Capacity()) * kapToolBudgetPercent / 100)
	if budgetTokens < 1 {
		budgetTokens = 1
	}

	density := kap.Density()

	// 按优先级排序（priority 小的先选）
	type toolWithPriority struct {
		tool     map[string]interface{}
		priority int
	}
	queue := make([]toolWithPriority, 0, len(allTools))
	for _, tool := range allTools {
		name := getToolName(tool)
		if name == "" {
			continue
		}
		if role != nil && !role.IsToolAllowed(name) {
			continue
		}
		p, ok := m.toolKapPriority[name]
		if !ok {
			p = 9 // 未注册工具默认最低优先级
		}
		queue = append(queue, toolWithPriority{tool: tool, priority: p})
	}
	sort.SliceStable(queue, func(i, j int) bool {
		return queue[i].priority < queue[j].priority
	})

	// 贪心选择：按优先级顺序累加 token，超预算则跳过继续尝试
	filtered := make([]map[string]interface{}, 0, len(queue))
	usedTokens := 0
	for _, item := range queue {
		trimmed := m.trimToolByDensity(item.tool, density)
		toolTokens := estimateToolChars(trimmed) / avgCharsPerToken
		if toolTokens < 1 {
			toolTokens = 1
		}
		if usedTokens+toolTokens > budgetTokens {
			continue // 超预算，跳过此工具，尝试后续更小的
		}
		filtered = append(filtered, trimmed)
		usedTokens += toolTokens
	}

	return filtered
}

// trimToolByDensity 根据提示密度裁剪工具描述
// 優化：density == Full 時直接返回原始工具（零拷貝），避免不必要的 deepCopy
// 只有需要修改描述時才做深拷貝
func (m *ToolKapManager) trimToolByDensity(
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
func (m *ToolKapManager) EstimateToolTokens(tools []map[string]interface{}) int {
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
func (m *ToolKapManager) GetMaxHistoryMessages(contextWindow int, systemPromptTokens int, toolTokens int) int {
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
// Kap 工具名称查询
// ============================================================

// GetKapToolNames 返回指定 Kap 级别可用的工具名称列表
// 与 GetFilteredTools 同样的预算贪心逻辑：按优先级排序 → 贪心累加 token → 超预算跳过。
// Kap2048 返回全量工具名称。
func GetKapToolNames(kap ToolKap) []string {
	// Kap2048：全量
	if kap.IsFullTools() {
		names := make([]string, 0, len(toolRegistry))
		for _, td := range toolRegistry {
			names = append(names, td.Name)
		}
		return names
	}

	budgetTokens := int(float64(kap.Capacity()) * kapToolBudgetPercent / 100)
	if budgetTokens < 1 {
		budgetTokens = 1
	}
	density := kap.Density()

	// 按优先级排序（stable 保证同优先级按注册顺序）
	sorted := make([]*ToolDef, len(toolRegistry))
	copy(sorted, toolRegistry)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi := kapPriorityForTool(sorted[i].Tier, sorted[i].Category, sorted[i].Name)
		pj := kapPriorityForTool(sorted[j].Tier, sorted[j].Category, sorted[j].Name)
		return pi < pj
	})

	names := make([]string, 0, len(sorted))
	usedTokens := 0
	for _, td := range sorted {
		tokens := estimateToolDefTokens(td, density)
		if tokens < 1 {
			tokens = 1
		}
		if usedTokens+tokens > budgetTokens {
			continue
		}
		names = append(names, td.Name)
		usedTokens += tokens
	}
	return names
}

// GetCoreToolNames 返回 Kap64 级别可用的工具名称列表（兼容旧接口）
// 用于工具预算保护逻辑：Kap64 = 64K 上下文预算下的工具集
func GetCoreToolNames() []string {
	return GetKapToolNames(Kap64)
}

// estimateToolDefTokens 估算 ToolDef 在指定密度下的 token 消耗
// 用于 GetKapToolNames 的预算控制（不需要构建完整 map，性能优于 estimateToolChars）
func estimateToolDefTokens(td *ToolDef, density PromptDensity) int {
	// 参数 schema 的固定开销估算（平均 ~200 字符 / 4 = 50 token）
	const paramsTokens = 50

	var descChars int
	switch density {
	case PromptDensityMinimal:
		// 极简：仅工具名
		descChars = len(td.Name)
	case PromptDensityCompact:
		// 精简：截断到 80 字符
		desc := td.Description
		if len(desc) > compactMaxChars {
			desc = desc[:compactMaxChars]
		}
		descChars = len(td.Name) + len(desc)
	case PromptDensityStandard:
		// 标准：第一段（\n\n 分隔）
		desc := td.Description
		if idx := strings.Index(desc, "\n\n"); idx > 0 {
			desc = desc[:idx]
		}
		descChars = len(td.Name) + len(desc)
	default:
		// 完整：全描述
		descChars = len(td.Name) + len(td.Description)
	}

	return descChars/avgCharsPerToken + paramsTokens
}

// ============================================================
// 浏览器工具合并定义
// ============================================================
// 原始浏览器工具有 33 个独立工具，在小模型场景下消耗大量 token。
// 这里将它们合并为 5 个聚合工具，大幅减少 token 消耗。
// ============================================================

// GetConsolidatedBrowserTools 返回合并后的浏览器工具定义
// 将 33 个独立浏览器工具合并为 5 个聚合工具：
//   - BrowserNavigate: 导航/访问网页（合并 BrowserVisit, BrowserNavigate）
//   - BrowserInteract: 页面交互（合并 BrowserClick, BrowserDoubleClick,
//     BrowserHover, BrowserType, BrowserScroll, BrowserRightClick, BrowserDrag）
//   - BrowserExtract: 内容提取（合并 BrowserScreenshot, BrowserExecuteJs,
//     BrowserExtractLinks, BrowserExtractImages, BrowserExtractElements,
//     BrowserSnapshot, BrowserElementScreenshot, BrowserPdf, BrowserPdfFromFile）
//   - BrowserFormFill: 表单填写（合并 BrowserFillForm, BrowserSelectOption,
//     BrowserKeyPress, BrowserUploadFile）
//   - BrowserSearch: 搜索引擎查询（合并 BrowserSearch）
func GetConsolidatedBrowserTools() []map[string]interface{} {
	return []map[string]interface{}{
		// --- BrowserNavigate: 导航与访问 ---
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "BrowserNavigate",
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
		// --- BrowserInteract: 页面交互 ---
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "BrowserInteract",
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
							"enum":        []string{"click", "DoubleClick", "hover", "RightClick", "type", "scroll", "drag"},
							"description": "The interaction action to perform.",
						},
						"selector": map[string]interface{}{
							"type":        "string",
							"description": "CSS selector for the target element. Required for click, DoubleClick, hover, RightClick, drag. Example: 'button.submit', '#login-btn'.",
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
		// --- BrowserExtract: 内容提取 ---
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "BrowserExtract",
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
							"enum":        []string{"screenshot", "ExecuteJs", "ExtractLinks", "ExtractImages", "ExtractElements", "snapshot", "pdf"},
							"description": "The extraction mode.",
						},
						"selector": map[string]interface{}{
							"type":        "string",
							"description": "CSS selector for 'ExtractElements' mode. Example: '.article', 'div.content p'.",
						},
						"script": map[string]interface{}{
							"type":        "string",
							"description": "JavaScript code for 'ExecuteJs' mode. Must be a function expression.",
						},
						"FullPage": map[string]interface{}{
							"type":        "boolean",
							"description": "Capture full page screenshot (for 'screenshot' mode). Default: false.",
						},
						"IncludeHtml": map[string]interface{}{
							"type":        "boolean",
							"description": "Include HTML content (for 'ExtractElements' mode). Default: false.",
						},
					},
					"required":             []string{"url", "mode"},
					"additionalProperties": false,
				},
			},
		},
		// --- BrowserFormFill: 表单填写 ---
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "BrowserFormFill",
				"description": "Fill out and submit web forms. Supports multi-field input, file uploads, select options, and key press simulation.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "The URL to navigate to.",
						},
						"FormData": map[string]interface{}{
							"type":        "object",
							"description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
						},
						"SubmitSelector": map[string]interface{}{
							"type":        "string",
							"description": "CSS selector for submit button. If empty, presses Enter to submit.",
						},
						"FilePath": map[string]interface{}{
							"type":        "string",
							"description": "Path to file for file upload fields.",
						},
						"SelectValue": map[string]interface{}{
							"type":        "string",
							"description": "Value to select for dropdown menus.",
						},
					},
					"required":             []string{"url", "FormData"},
					"additionalProperties": false,
				},
			},
		},
		// --- BrowserSearch: 搜索引擎 ---
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "BrowserSearch",
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
		// --- BrowserNavigate: 导航与访问 ---
		{
			"name":        "BrowserNavigate",
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
		// --- BrowserInteract: 页面交互 ---
		{
			"name":        "BrowserInteract",
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
						"enum":        []string{"click", "DoubleClick", "hover", "RightClick", "type", "scroll", "drag"},
						"description": "The interaction action to perform.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector for the target element. Required for click, DoubleClick, hover, RightClick, drag. Example: 'button.submit', '#login-btn'.",
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
		// --- BrowserExtract: 内容提取 ---
		{
			"name":        "BrowserExtract",
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
						"enum":        []string{"screenshot", "ExecuteJs", "ExtractLinks", "ExtractImages", "ExtractElements", "snapshot", "pdf"},
						"description": "The extraction mode.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector for 'ExtractElements' mode. Example: '.article', 'div.content p'.",
					},
					"script": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript code for 'ExecuteJs' mode. Must be a function expression.",
					},
					"FullPage": map[string]interface{}{
						"type":        "boolean",
						"description": "Capture full page screenshot (for 'screenshot' mode). Default: false.",
					},
					"IncludeHtml": map[string]interface{}{
						"type":        "boolean",
						"description": "Include HTML content (for 'ExtractElements' mode). Default: false.",
					},
				},
				"required":             []string{"url", "mode"},
				"additionalProperties": false,
			},
		},
		// --- BrowserFormFill: 表单填写 ---
		{
			"name":        "BrowserFormFill",
			"description": "Fill out and submit web forms. Supports multi-field input, file uploads, select options, and key press simulation.",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to navigate to.",
					},
					"FormData": map[string]interface{}{
						"type":        "object",
						"description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
					},
					"SubmitSelector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector for submit button. If empty, presses Enter to submit.",
					},
					"FilePath": map[string]interface{}{
						"type":        "string",
						"description": "Path to file for file upload fields.",
					},
					"SelectValue": map[string]interface{}{
						"type":        "string",
						"description": "Value to select for dropdown menus.",
					},
				},
				"required":             []string{"url", "FormData"},
				"additionalProperties": false,
			},
		},
		// --- BrowserSearch: 搜索引擎 ---
		{
			"name":        "BrowserSearch",
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
	manager := NewToolKapManager()
	kap := manager.GetKapForContextWindow(modelCtx)
	density := kap.Density()

	// 從註冊中心獲取對應格式的所有工具
	var allTools []map[string]interface{}
	if apiType == "anthropic" {
		allTools = getAnthropicToolsFromRegistry()
	} else {
		allTools = getOpenAIToolsFromRegistry()
	}

	// 獲取經過 Kap 篩選、密度裁剪和角色權限檢查的工具列表
	filtered := manager.GetFilteredTools(allTools, kap, role)

	// Kap1024（全量）以外嘅級別：追加通過 menu 工具加載的額外工具
	// 低 Kap 層尤其依賴 Menu 來按需加載更多工具
	if !kap.IsFullTools() {
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

	// 低 Kap 級別且工具 token 超出預算 50% 時，合併瀏覽器工具
	if kap <= Kap64 {
		estimatedTokens := manager.EstimateToolTokens(filtered)
		budget := modelCtx / 2
		if estimatedTokens > budget {
			filtered = replaceBrowserWithConsolidated(filtered, manager, density, role, apiType)
		}
	}

	// ── 全局工具預算限制 ──────────────────────────────────────
	// 工具定義的 token 數不超過上下文容量的 1%（kapToolBudgetPercent / 100）。
	// 原因：大量工具定義（如 100 個工具 = 61KB）會導致第三方代理
	// 服務器處理延遲顯著增加（實測 75KB tools TTFB=5s vs 11KB tools TTFB=2.6s）。
	// 128K 窗口 → 1310 tokens ≈ 5KB JSON，含核心工具的精簡描述。
	maxToolTokens := int(float64(modelCtx) * kapToolBudgetPercent / 100)
	if maxToolTokens < 1 {
		maxToolTokens = 1
	}
	estimatedTokens := manager.EstimateToolTokens(filtered)
	if estimatedTokens > maxToolTokens {
		// 標記核心工具（Kap64 級別），保護它們不被移除
		coreNames := make(map[string]bool)
		for _, name := range GetCoreToolNames() {
			coreNames[name] = true
		}
		// menu 不在 toolRegistry 中（獨立定義於 tool_menu.go），
		// 但它是系統最核心的工具入口，必須受預算保護。
		coreNames["Menu"] = true
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
		log.Printf("[ToolKap] Tool budget: %d tools (%d tokens) trimmed to %d tools (budget %d tokens)",
			len(filtered), estimatedTokens, len(result), maxToolTokens)
		filtered = result
	}

	return filtered
}

// replaceBrowserWithConsolidated 将原始浏览器工具替换为合并版本
// 用于极小上下文窗口场景下进一步减少 token 消耗
func replaceBrowserWithConsolidated(
	tools []map[string]interface{},
	manager *ToolKapManager,
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
	manager *ToolKapManager,
	density PromptDensity,
	role *Role,
) []map[string]interface{} {
	return replaceBrowserWithConsolidated(tools, manager, density, role, "anthropic")
}

// ============================================================
// 调试与信息函数
// ============================================================

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

// GetKapInfo 返回工具 Kap 级别的详细调试信息
// 包括 Kap 级别名称、工具数量、提示密度等
func (m *ToolKapManager) GetKapInfo(contextWindow int, allTools []map[string]interface{}) string {
	kap := m.GetKapForContextWindow(contextWindow)
	density := kap.Density()
	toolCount := len(GetKapToolNames(kap))

	return fmt.Sprintf(
		"[ToolKap] contextWindow=%d kap=%s density=%s estimatedTools=%d",
		contextWindow, kap, density, toolCount,
	)
}
