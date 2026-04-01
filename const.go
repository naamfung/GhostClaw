package main

import (
        "os"
        "fmt"
        "time"
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

// 通用系统规则（不含角色身份）
var baseSystemRules = `请遵循以下原则：

1. **在调用任何工具之前，先回顾整个对话历史。如果回答用户当前问题所需的信息已在历史中（包括你之前的回答或工具结果），请直接回答，不要调用工具。**

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
        SYSTEM_PROMPT = baseSystemRules
}

// BuildGeneralSystemPrompt 构建通用系统规则（不含角色身份）
func BuildGeneralSystemPrompt(enableImplicitSummary bool) string {
    // 获取系统环境信息
    hostname := getHostname()
    currentTime := time.Now().Format("2006-01-02 15:04:05")
    osInfo := runtime.GOOS + "/" + runtime.GOARCH
    programName := "GarClaw"

    envInfo := fmt.Sprintf(`# 系统环境信息
- **操作系统**：%s
- **宿主程序**：%s
- **当前系统时间**：%s
- **主机名**：%s
`, osInfo, programName, currentTime, hostname)
    return envInfo + baseSystemRules
}

// getHostname 获取主机名（失败时返回 "unknown"）
func getHostname() string {
    hostname, err := os.Hostname()
    if err != nil {
        return "unknown"
    }
    return hostname
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
                name        string
                tools       []struct {
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
                }},
                {"文件操作", []struct {
                        name        string
                        description string
                }{
                        {"read_file_line", "读取文件指定行"},
                        {"write_file_line", "写入文件指定行"},
                        {"read_all_lines", "读取文件所有行"},
                        {"write_all_lines", "覆盖写入文件"},
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
                        {"browser_visit", "访问网页"},
                        {"browser_search", "搜索引擎搜索"},
                        {"browser_download", "下载文件"},
                        {"browser_screenshot", "截图"},
                        {"browser_click", "点击元素"},
                        {"browser_type", "输入文本"},
                        {"browser_scroll", "滚动页面"},
                        {"browser_execute_js", "执行 JavaScript"},
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
                }},
                {"任务调度", []struct {
                        name        string
                        description string
                }{
                        {"cron_add", "添加定时任务"},
                        {"cron_list", "列出定时任务"},
                        {"todo", "管理待办事项"},
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

        sb.WriteString("**提示**：每个工具都有详细的参数说明。调用时系统会显示具体用法。\n")

        return sb.String()
}
