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
        DefaultBrowserTimeout     = 90  // 浏览器每次操作默认超时（适应慢速网络 + JS 渲染）
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
                        TruncateString(skill.Description, 50)))
        }

        return sb.String()
}

// BuildToolSectionForRole 根据角色权限构建工具说明
// 从工具注册中心动态生成，彻底消除硬编码维护
func BuildToolSectionForRole(role *Role) string {
        var sb strings.Builder

        sb.WriteString("# 可用工具\n\n")
        sb.WriteString("你拥有丰富的工具来完成各种任务。工具按类别组织：\n\n")

        // 获取所有工具定义
        allTools := GetRegistryTools()

        // 按分类收集允许的工具
        categoryMap := make(map[string][]*ToolDef)

        for _, td := range allTools {
                // 权限检查：如果角色不允许该工具，跳过
                if role != nil && !role.IsToolAllowed(td.Name) {
                        continue
                }
                categoryMap[td.Category] = append(categoryMap[td.Category], td)
        }

        // 定义分类的显示顺序和友好名称
        categoryOrder := []struct {
                name  string
                title string
        }{
                {"core", "命令执行"},
                {"file", "文件操作"},
                {"web", "浏览器操作"},
                {"memory", "记忆管理"},
                {"plugin", "插件管理"},
                {"schedule", "任务调度"},
                {"ssh", "SSH 管理"},
                {"skill", "技能管理"},
                {"profile", "配置管理"},
                {"plan", "规划模式"},
                {"spawn", "子代理"},
                {"misc", "其他工具"},
        }

        // 按预定义顺序输出
        for _, cat := range categoryOrder {
                tools, exists := categoryMap[cat.name]
                if !exists || len(tools) == 0 {
                        continue
                }
                sb.WriteString(fmt.Sprintf("## %s\n", cat.title))
                for _, td := range tools {
                        sb.WriteString(fmt.Sprintf("- **%s**：%s\n", td.Name, td.Description))
                }
                sb.WriteString("\n")
        }

        // 处理未预定义顺序的分类（追加到末尾）
        for catName, tools := range categoryMap {
                found := false
                for _, cat := range categoryOrder {
                        if cat.name == catName {
                                found = true
                                break
                        }
                }
                if found {
                        continue
                }
                // 未预定义的分类直接使用分类名作为标题
                sb.WriteString(fmt.Sprintf("## %s\n", catName))
                for _, td := range tools {
                        sb.WriteString(fmt.Sprintf("- **%s**：%s\n", td.Name, td.Description))
                }
                sb.WriteString("\n")
        }

        sb.WriteString("**提示**：每个工具都有详细的参数说明。调用时系统会显示具体用法。\n\n")

        // 关于 curl 的提示（仅在 opencli 可用时显示）
        if isOpenCLIAvailable() {
                sb.WriteString("## 关于 curl 使用\n\n")
                sb.WriteString("⚠️ **重要**：如果并非下载文件，而是为了访问网页、搜索信息或进行网页自动化操作，**请优先使用 opencli 工具**或通过 shell 运行 opencli 命令，而不是使用 curl！\n\n")
                sb.WriteString("curl/wget 仅适用于：\n")
                sb.WriteString("- 直接下载文件\n")
                sb.WriteString("- 简单的 HTTP API 请求（非网页浏览）\n\n")
                sb.WriteString("所有网页浏览、搜索和交互任务请使用 opencli 工具。\n\n")
        }

        // 添加 OpenCLI 工具优先级说明
        sb.WriteString("## 工具使用优先级\n\n")

        if isOpenCLIAvailable() {
                sb.WriteString("**重要提示**：系统检测到 OpenCLI 已可用！\n\n")
                sb.WriteString("**网页操作类任务**：\n")
                sb.WriteString("- **强制要求**：所有网页操作类任务必须使用 OpenCLI（通过 shell 工具执行 `opencli` 命令）\n")
                sb.WriteString("- **禁用说明**：内置 browser 工具已被禁用，请勿调用\n\n")
                sb.WriteString("OpenCLI 使用示例：\n")
                sb.WriteString("- 访问网页：`shell: \"opencli web read --url https://example.com\"`\n")
                sb.WriteString("- 咇站搜索：`shell: \"opencli bilibili search \\\"关键词\\\"\"`\n")
                sb.WriteString("- 强烈建议先睇命令帮助：`shell: \"opencli --help\"` 或 `shell: \"opencli <子命令> --help\"`\n")
                sb.WriteString("- 或使用 skill 中的opencli指南。\n\n")
                sb.WriteString("OpenCLI 优势：支持浏览器会话重用、更好的登录状态保持、更丰富的适配器生态。\n")
        } else {
                sb.WriteString("**网页操作类任务**：\n")
                sb.WriteString("1. 优先使用 OpenCLI（通过 shell 工具执行 `opencli` 命令）\n")
                sb.WriteString("2. 仅当 OpenCLI 不可用时，才使用内置 browser 工具\n\n")
                sb.WriteString("OpenCLI 优势：支持浏览器会话重用、更好的登录状态保持、更丰富的适配器生态。\n\n")
                sb.WriteString("**检查 OpenCLI 是否可用**：`shell: \"which opencli\"` 或 `shell: \"opencli doctor\"`\n")
        }

        return sb.String()
}
