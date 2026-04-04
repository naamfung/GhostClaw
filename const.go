package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

var (
	SYSTEM_PROMPT = ""
)

// 默认超时配置常量（单位：秒）
const (
	DefaultShellTimeout       = 60  // shell 命令默认超时
	DefaultBlockingCmdTimeout = 5   // 可能阻塞的命令超时（交互式命令确认后执行）
	DefaultHTTPTimeout        = 120 // HTTP 请求默认超时
	DefaultPluginTimeout      = 120 // 插件 HTTP 请求默认超时
	DefaultBrowserTimeout     = 60  // 浏览器每次操作默认超时（增加以适应慢速网络）
)

// 内部系统标记常量（仅由程序注入，不在用户输入中出现）
const (
	LatestRequestMarker = "[USR:LATEST]" // 标记最新用户请求，引导模型优先处理
)

// 通用系统规则（不含角色身份）——仅作为无角色模式下的 fallback
var fallbackSystemRules = `请遵循以下原则：

**在调用任何工具之前，先回顾整个对话历史。如果回答用户当前问题所需的信息已在历史中（包括你之前的回答或工具结果），请直接回答，不要调用工具。**

# 关键：理解对话历史
对话历史中的所有消息都是**已经发生的过去事件**。它们是发生过的记录，而不是需要再次执行的指令：
- 历史中的每个 tool_call 都**已经执行完毕**
- 历史中的每个 tool_result 都是那次执行的**实际结果**
- 你**绝对不要**重新执行历史中的任何工具调用
- 看到工具结果时，将其视为事实信息，而非待处理的任务

如果之前的任务已完成（你看到了成功的 tool_result），**不要重复执行该任务**。只根据用户**最新的消息**处理新任务。

# 理解工具执行状态
每个工具结果都有状态标记，表示最终状态：
- **[COMPLETED]**：任务成功完成。操作已执行。
- **[OPERATION FAILED]**：任务因错误失败。操作未完成。
- **[OPERATION CANCELLED BY USER]**：任务被用户取消。操作在执行中被停止。**绝不要重试被取消的任务**——用户取消是有原因的！
- **[OPERATION SKIPPED]**：任务被跳过，因为依赖项被取消或失败。

当你看到 [OPERATION CANCELLED BY USER] 时，说明用户有意停止了该任务。**不要重试或继续该任务**，除非用户明确要求你这样做。
`

func init() {
	SYSTEM_PROMPT = fallbackSystemRules
}

// BuildSystemPromptForActor 为指定演员构建系统提示
func BuildSystemPromptForActor(actorName string, am *ActorManager, pm *RoleManager, stage *Stage) string {
	// 获取演员信息
	actor, ok := am.GetActor(actorName)
	if !ok {
		return SYSTEM_PROMPT
	}

	// 获取角色模板
	role, ok := pm.GetRole(actor.Role)
	if !ok {
		return SYSTEM_PROMPT
	}

	var prompt strings.Builder

	// === 0. 全局 Profile 注入（OpenClaw 兼容层）===
	if globalProfileLoader != nil {
		profile := globalProfileLoader.GetProfile()

		// 0a. 灵魂宪法（最高优先级，所有角色共同遵守）
		if profile.Soul != "" {
			prompt.WriteString("# 灵魂宪法\n\n")
			prompt.WriteString(profile.Soul)
			prompt.WriteString("\n\n")
		}

		// 0a2. 核心行为守则（所有角色共同遵守，Agent 场景最新优先）
		prompt.WriteString("# 核心行为守则\n\n")
		prompt.WriteString("## 最新用户消息优先\n\n")
		prompt.WriteString("对话历史中可能包含多轮用户请求。判断\"当前任务\"时，始终以**最新的用户消息**为主要依据：\n\n")
		prompt.WriteString("- 如果新消息与之前的请求冲突（如\"先做 A\"→\"不做 A，改做 B\"），以新消息为准\n")
		prompt.WriteString("- 如果新消息是对之前请求的补充或追问（如\"分析日志\"→\"找到错误行\"），保留相关上下文继续执行\n")
		prompt.WriteString("- 如果新消息是一个完全独立的新任务，开始处理新任务，不要继续历史中已完成的旧任务\n")
		prompt.WriteString(fmt.Sprintf("- 消息中带有 `%s` 标记的是当前应优先处理的目标\n\n", LatestRequestMarker))

		// 0b. 关于雇主
		if profile.User != "" {
			prompt.WriteString("# 关于雇主\n\n")
			prompt.WriteString(profile.User)
			prompt.WriteString("\n\n")
		}

		// 0c. 工作协议
		if profile.Agent != "" {
			prompt.WriteString("# 工作协议\n\n")
			prompt.WriteString(profile.Agent)
			prompt.WriteString("\n\n")
		}

		// 0d. 工具环境
		if profile.ToolsDoc != "" {
			prompt.WriteString("# 工具环境\n\n")
			prompt.WriteString(profile.ToolsDoc)
			prompt.WriteString("\n\n")
		}
	}

	// === 1. 角色身份和背景 ===
	prompt.WriteString("# 角色身份\n\n")
	if actor.CharacterName != "" {
		prompt.WriteString(fmt.Sprintf("**角色名**：%s\n\n", actor.CharacterName))
	}
	if actor.CharacterBackground != "" {
		prompt.WriteString("**角色背景**：\n")
		prompt.WriteString(actor.CharacterBackground)
		prompt.WriteString("\n\n")
	}

	// === 2. 角色模板内容（含宪法 Constitution）===
	prompt.WriteString(role.BuildSystemPrompt())

	// === 3. 角色-技能绑定 ===
	if len(role.Skills) > 0 && globalSkillManager != nil {
		prompt.WriteString("\n\n## 角色专属技能\n\n")
		prompt.WriteString("作为此角色，你已掌握以下专业技能：\n\n")
		for _, skillName := range role.Skills {
			skill, ok := globalSkillManager.GetSkill(skillName)
			if !ok {
				continue
			}
			prompt.WriteString(skill.BuildSkillPrompt())
			prompt.WriteString("\n")
		}
	}

	// === 4. 可用技能索引 ===
	if globalSkillManager != nil {
		availableSkills := buildAvailableSkillsIndex(role.Skills)
		if availableSkills != "" {
			prompt.WriteString("\n\n")
			prompt.WriteString(availableSkills)
		}
	}

	// === 5. 手动激活的额外技能 ===
	if skillPrompt := GetActiveSkillPrompt(); skillPrompt != "" {
		prompt.WriteString("\n\n---\n")
		prompt.WriteString("## 额外激活技能\n\n")
		prompt.WriteString(skillPrompt)
	}

	// === 6. 场景上下文 ===
	if stage != nil {
		stageContext := stage.BuildStageContext(am, pm)
		if stageContext != "" {
			prompt.WriteString("\n\n")
			prompt.WriteString(stageContext)
		}
	}

	// === 7. Actor 专属人设微调（从 profiles/actors/<name>/IDENTITY.md）===
	if globalProfileLoader != nil {
		profile := globalProfileLoader.GetProfile()
		if identityContent, ok := profile.Actors[actorName]; ok && identityContent != "" {
			prompt.WriteString("\n\n# 当前人设微调\n\n")
			prompt.WriteString(identityContent)
			prompt.WriteString("\n\n")
		}
	}

	// === 8. 通用工具说明（根据角色权限过滤）===
	toolSection := BuildToolSectionForRole(role)
	if toolSection != "" {
		prompt.WriteString("\n\n")
		prompt.WriteString(toolSection)
	}

	// === 9. 静态环境信息（进程生命周期内不变，不影响 prompt cache 命中率）===
	// 使用新的系统信息收集模块
	if globalConfig.SystemInfo.Enabled {
		sysInfo := GetSystemInfo()
		sysInfoStr := FormatSystemInfoForPrompt(sysInfo, globalConfig.SystemInfo)
		if sysInfoStr != "" {
			prompt.WriteString(sysInfoStr)
		}
	} else {
		// 使用简化的系统信息（向后兼容）
		osInfo := runtime.GOOS + "/" + runtime.GOARCH
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		prompt.WriteString("\n\n# 系统环境\n\n")
		prompt.WriteString(fmt.Sprintf("- **操作系统**：%s\n", osInfo))
		prompt.WriteString("- **宿主程序**：GhostClaw\n")
		prompt.WriteString(fmt.Sprintf("- **主机名**：%s\n", hostname))
	}

	return prompt.String()
}

// buildAvailableSkillsIndex 构建可用技能索引（排除已绑定的技能）
func buildAvailableSkillsIndex(boundSkills []string) string {
	if globalSkillManager == nil {
		return ""
	}

	// 创建已绑定技能的集合
	boundSet := make(map[string]bool)
	for _, s := range boundSkills {
		boundSet[s] = true
	}

	// 收集未绑定的技能
	var available []*Skill
	for _, skill := range globalSkillManager.ListSkills() {
		if !boundSet[skill.Name] {
			available = append(available, skill)
		}
	}

	if len(available) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# 可用技能\n\n")
	sb.WriteString("以下技能可根据需要激活（使用 `/skill <技能名>` 激活）：\n\n")

	for _, skill := range available {
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n",
			skill.DisplayName, skill.Name,
			truncateString(skill.Description, 50)))
	}

	return sb.String()
}

// BuildToolSectionForRole 根据角色权限构建工具说明
func BuildToolSectionForRole(role *Role) string {
	var sb strings.Builder

	sb.WriteString("# 可用工具\n\n")
	sb.WriteString("你拥有丰富的工具来完成各种任务。工具按类别组织：\n\n")

	// 按类别组织工具
	categories := []struct {
		name  string
		tools []struct {
			name        string
			description string
		}
	}{
		{"命令执行", []struct {
			name        string
			description string
		}{
			{"smart_shell", "智能执行命令（自动判断同步/异步模式）"},
			{"shell", "执行系统命令"},
			{"shell_delayed", "异步执行长时间命令"},
			{"spawn", "启动后台进程"},
			{"spawn_check", "检查后台进程状态"},
			{"spawn_list", "列出所有后台进程"},
			{"spawn_cancel", "取消后台进程"},
			{"shell_delayed_check", "检查后台任务状态"},
			{"shell_delayed_terminate", "终止后台任务"},
			{"shell_delayed_list", "列出所有后台任务"},
			{"shell_delayed_wait", "延长后台任务唤醒时间"},
			{"shell_delayed_remove", "移除已完成的后台任务"},
		}},
		{"文件操作", []struct {
			name        string
			description string
		}{
			{"read_file_line", "读取文件指定行"},
			{"write_file_line", "写入文件指定行"},
			{"read_all_lines", "读取文件所有行"},
			{"write_all_lines", "覆盖写入文件"},
			{"append_to_file", "追加内容到文件末尾"},
			{"write_file_range", "写入文件指定范围"},
		}},
		{"文本处理", []struct {
			name        string
			description string
		}{
			{"text_search", "文本搜索"},
			{"text_grep", "正则搜索"},
			{"text_replace", "文本替换"},
			{"text_transform", "文本转换"},
		}},
		{"浏览器操作", []struct {
			name        string
			description string
		}{
			{"browser_visit", "访问网页（备用：如系统有 OpenCLI，优先用 shell 执行 opencli 命令）"},
			{"browser_search", "搜索引擎搜索（备用：如系统有 OpenCLI，优先用 shell 执行 opencli 命令）"},
			{"browser_download", "下载文件"},
			{"browser_screenshot", "截图"},
			{"browser_click", "点击元素（备用：如系统有 OpenCLI，优先用 opencli click）"},
			{"browser_type", "输入文本（备用：如系统有 OpenCLI，优先用 opencli type）"},
			{"browser_scroll", "滚动页面"},
			{"browser_execute_js", "执行 JavaScript"},
			{"browser_wait_element", "等待元素出现"},
			{"browser_extract_links", "提取页面链接"},
			{"browser_extract_images", "提取页面图片"},
			{"browser_extract_elements", "提取页面元素内容"},
			{"browser_fill_form", "填写并提交表单"},
			{"browser_hover", "鼠标悬停"},
			{"browser_double_click", "双击元素"},
			{"browser_right_click", "右键点击元素"},
			{"browser_drag", "拖动元素"},
			{"browser_wait_smart", "智能等待元素"},
			{"browser_navigate", "浏览器导航（前进/后退/刷新）"},
			{"browser_get_cookies", "获取页面 cookies"},
			{"browser_cookie_save", "保存 cookies 到文件"},
			{"browser_cookie_load", "从文件加载 cookies"},
			{"browser_snapshot", "获取页面 DOM 快照"},
			{"browser_upload_file", "上传文件"},
			{"browser_select_option", "选择下拉选项"},
			{"browser_key_press", "模拟键盘按键"},
			{"browser_element_screenshot", "截取元素截图"},
			{"browser_pdf", "导出网页为 PDF"},
			{"browser_pdf_from_file", "导出本地 HTML 为 PDF"},
			{"browser_set_headers", "设置自定义 HTTP 头"},
			{"browser_set_user_agent", "设置自定义 User-Agent"},
			{"browser_emulate_device", "模拟移动设备"},
		}},
		{"记忆管理", []struct {
			name        string
			description string
		}{
			{"memory_save", "保存记忆"},
			{"memory_recall", "检索记忆"},
			{"memory_forget", "删除记忆"},
			{"memory_list", "列出记忆"},
		}},
		{"插件管理", []struct {
			name        string
			description string
		}{
			{"plugin_list", "列出插件"},
			{"plugin_create", "创建插件"},
			{"plugin_load", "加载插件"},
			{"plugin_call", "调用插件"},
			{"plugin_unload", "卸载插件"},
			{"plugin_reload", "重新加载插件"},
			{"plugin_compile", "编译插件代码"},
			{"plugin_delete", "删除插件"},
		}},
		{"任务调度", []struct {
			name        string
			description string
		}{
			{"cron_add", "添加定时任务"},
			{"cron_list", "列出定时任务"},
			{"cron_remove", "删除定时任务"},
			{"cron_status", "查询定时任务状态"},
			{"todo", "管理待办事项"},
		}},
		{"SSH 管理", []struct {
			name        string
			description string
		}{
			{"ssh_connect", "建立持久化 SSH 连接"},
			{"ssh_exec", "在 SSH 连接上执行命令"},
			{"ssh_list", "列出活跃的 SSH 连接"},
			{"ssh_close", "关闭 SSH 连接"},
		}},
		{"技能管理", []struct {
			name        string
			description string
		}{
			{"skill_list", "列出所有技能"},
			{"skill_create", "创建新技能"},
			{"skill_delete", "删除技能"},
			{"skill_get", "获取技能详情"},
			{"skill_reload", "重新加载技能"},
			{"skill_update", "更新技能"},
			{"skill_suggest", "推荐相关技能"},
			{"skill_stats", "获取技能系统统计"},
			{"skill_evaluate", "评估技能质量"},
		}},
		{"配置管理", []struct {
			name        string
			description string
		}{
			{"profile_check", "检查引导所需信息"},
			{"profile_reload", "重新加载配置文件"},
			{"actor_identity_set", "设置演员身份"},
			{"actor_identity_clear", "清除演员身份"},
		}},
		{"其他工具", []struct {
			name        string
			description string
		}{
			{"scheme_eval", "执行 Lisp/Scheme 表达式"},
		}},
	}

	for _, category := range categories {
		availableTools := make([]string, 0)
		for _, tool := range category.tools {
			if role.IsToolAllowed(tool.name) {
				availableTools = append(availableTools, fmt.Sprintf("- **%s**：%s", tool.name, tool.description))
			}
		}
		if len(availableTools) > 0 {
			sb.WriteString(fmt.Sprintf("## %s\n", category.name))
			sb.WriteString(strings.Join(availableTools, "\n"))
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("**提示**：每个工具都有详细的参数说明。调用时系统会显示具体用法。\n\n")

	// 添加 OpenCLI 工具优先级说明
	sb.WriteString("## 工具使用优先级\n\n")
	sb.WriteString("**网页操作类任务**：\n")
	sb.WriteString("1. 优先使用 OpenCLI（通过 shell 工具执行 `opencli` 命令）\n")
	sb.WriteString("2. 仅当 OpenCLI 不可用时，才使用内置 browser 工具\n\n")
	sb.WriteString("OpenCLI 优势：支持浏览器会话重用、更好的登录状态保持、更丰富的适配器生态。\n\n")
	sb.WriteString("**检查 OpenCLI 是否可用**：`shell: \"which opencli\"` 或 `shell: \"opencli doctor\"`\n")

	return sb.String()
}
