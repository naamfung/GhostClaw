package main

import (
        "context"
        "fmt"
        "log"
        "strings"
        "time"
)

// CommandResult 斜杠命令处理结果
type CommandResult struct {
        Handled  bool   // 是否已处理
        Response string // 响应文本
        IsExit   bool   // 是否需要退出程序（/exit）
        IsQuit   bool   // 是否需要断开连接/切换模式（/quit）
        IsStop   bool   // 是否需要停止当前任务
}

// ProcessSlashCommand 统一处理斜杠命令
// 所有前端（终端、网页、邮件等）都应调用此函数
func ProcessSlashCommand(input string, rm *RoleManager, am *ActorManager, stage *Stage) CommandResult {
        input = strings.TrimSpace(input)

        if !strings.HasPrefix(input, "/") {
                return CommandResult{Handled: false}
        }

        // 移除前导斜杠
        cmdWithArgs := input[1:]

        // 解析命令
        parts := strings.SplitN(cmdWithArgs, " ", 2)
        cmd := strings.ToLower(parts[0])
        args := ""
        if len(parts) > 1 {
                args = parts[1]
        }

        switch cmd {
        case "exit":
                return CommandResult{Handled: true, Response: "青山不改，绿水长流，有缘再会！", IsExit: true}
        case "quit":
                return CommandResult{Handled: true, Response: "已断开连接，后台任务不受影响", IsQuit: true}

        case "stop":
                return CommandResult{Handled: true, Response: "任务已取消", IsStop: true}

        case "role":
                return CommandResult{Handled: true, Response: HandleRoleCommand(args, rm, am, stage)}

        case "actor":
                return CommandResult{Handled: true, Response: HandleActorCommand(args, am, rm, stage)}

        case "stage":
                return CommandResult{Handled: true, Response: HandleStageCommand(args, am, rm, stage)}

        case "next":
                return CommandResult{Handled: true, Response: HandleNextCommand(am, rm, stage)}

        case "model":
                return CommandResult{Handled: true, Response: HandleModelCommand(args, am)}

        case "skill", "skills":
                handled, resp := ProcessSkillCommand(input, globalSkillManager, rm, stage)
                return CommandResult{Handled: handled, Response: resp}

        case "session":
                return CommandResult{Handled: true, Response: HandleSessionCommand(args)}

        case "save":
                return CommandResult{Handled: true, Response: HandleSaveCommand(args, GetGlobalSession())}

        case "load":
                return CommandResult{Handled: true, Response: HandleLoadCommand(args, GetGlobalSession())}

        case "new":
                return CommandResult{Handled: true, Response: HandleNewCommand()}

        case "context":
                return CommandResult{Handled: true, Response: HandleContextCommand()}

        case "help", "?":
                return CommandResult{Handled: true, Response: GetHelpText()}

        default:
                return CommandResult{Handled: false}
        }
}

// HandleContextCommand 处理 /context 命令，显示当前上下文使用情况
func HandleContextCommand() string {
        var sb strings.Builder
        sb.WriteString("📊 上下文信息\n\n")

        session := GetGlobalSession()
        history := session.GetHistory()

        // 1. 会话信息
        sb.WriteString("会话信息:\n")
        sb.WriteString(fmt.Sprintf("  会话ID: %s\n", session.ID))
        sb.WriteString(fmt.Sprintf("  消息总数: %d 条\n", len(history)))

        // 2. 消息统计
        userMsgs, assistantMsgs, toolMsgs, systemMsgs := 0, 0, 0, 0
        for _, msg := range history {
                switch msg.Role {
                case "user":
                        userMsgs++
                case "assistant":
                        assistantMsgs++
                case "tool":
                        toolMsgs++
                case "system":
                        systemMsgs++
                }
        }
        sb.WriteString(fmt.Sprintf("  消息分布: user: %d, assistant: %d, tool: %d, system: %d\n\n", userMsgs, assistantMsgs, toolMsgs, systemMsgs))

        // 3. Token 估算
        totalTokens := 0
        for _, msg := range history {
                if content, ok := msg.Content.(string); ok {
                        totalTokens += EstimateTokens(content)
                }
                if msg.ToolCalls != nil {
                        totalTokens += 50 // tool_calls 的额外开销估算
                }
        }
        if totalTokens >= 1000 {
                sb.WriteString(fmt.Sprintf("Token 估算: ~%dk tokens\n", totalTokens/1000))
        } else {
                sb.WriteString(fmt.Sprintf("Token 估算: ~%d tokens\n", totalTokens))
        }

        // 4. 动态截断阈值
        _, _, _, effectiveModelID, _, _, _, _ := getEffectiveAPIConfig()
        modelCtxWindow := GetModelContextLengthSafe(effectiveModelID)
        maxOutput := getMaxOutputTokens(effectiveModelID)
        adaptiveMaxHistory := CalculateAdaptiveMaxHistory(modelCtxWindow, 0, 0, maxOutput)
        sb.WriteString(fmt.Sprintf("模型 Context 窗口: %d tokens\n", modelCtxWindow))
        sb.WriteString(fmt.Sprintf("动态截断阈值: %d 条消息\n", adaptiveMaxHistory))
        remaining := adaptiveMaxHistory - len(history)
        if len(history) >= adaptiveMaxHistory {
                sb.WriteString("  ⚠️  已超出阈值，下次 AgentLoop 将触发截断\n")
        } else if remaining <= 10 {
                sb.WriteString(fmt.Sprintf("  ⚠️  距离阈值仅剩 %d 条消息\n", remaining))
        } else {
                sb.WriteString(fmt.Sprintf("  距离阈值还有 %d 条消息\n", remaining))
        }

        // 5. 记忆整合器信息
        if globalMemoryConsolidator != nil {
                sb.WriteString("\n记忆整合器:\n")
                budgetInfo := globalMemoryConsolidator.GetBudgetInfo("default")
                if budget, ok := budgetInfo["budget"].(int); ok {
                        threshold, _ := budgetInfo["threshold"].(int)
                        current, _ := budgetInfo["current_tokens"].(int)
                        consolidated, _ := budgetInfo["consolidated"].(int)
                        unconsolidated, _ := budgetInfo["unconsolidated"].(int)
                        should, _ := budgetInfo["should_consolidate"].(bool)
                        ratio, _ := budgetInfo["usage_ratio"].(float64)

                        if budget >= 1000 {
                                sb.WriteString(fmt.Sprintf("  上下文预算: %dk tokens\n", budget/1000))
                        } else {
                                sb.WriteString(fmt.Sprintf("  上下文预算: %d tokens\n", budget))
                        }
                        if threshold >= 1000 {
                                sb.WriteString(fmt.Sprintf("  整合触发阈值: %dk tokens\n", threshold/1000))
                        } else {
                                sb.WriteString(fmt.Sprintf("  整合触发阈值: %d tokens\n", threshold))
                        }
                        if current >= 1000 {
                                sb.WriteString(fmt.Sprintf("  当前估算: ~%dk tokens\n", current/1000))
                        } else {
                                sb.WriteString(fmt.Sprintf("  当前估算: ~%d tokens\n", current))
                        }
                        sb.WriteString(fmt.Sprintf("  预算使用率: %.1f%%\n", ratio*100))
                        sb.WriteString(fmt.Sprintf("  已整合消息: %d 条\n", consolidated))
                        sb.WriteString(fmt.Sprintf("  未整合消息: %d 条\n", unconsolidated))
                        if should {
                                sb.WriteString("  状态: ⚠️ 需要整合\n")
                        } else {
                                sb.WriteString("  状态: ✅ 正常\n")
                        }
                } else {
                        msgCount := globalMemoryConsolidator.GetMessageCount("default")
                        sb.WriteString(fmt.Sprintf("  未整合消息: %d 条\n", msgCount))
                }
        }

        // 6. 模型配置详情
        sb.WriteString("\n模型配置:\n")
        sb.WriteString(fmt.Sprintf("  模型名称: %s\n", globalAPIConfig.Model))
        sb.WriteString(fmt.Sprintf("  MaxTokens: %d\n", globalAPIConfig.MaxTokens))
        sb.WriteString(fmt.Sprintf("  Temperature: %.2f\n", globalAPIConfig.Temperature))
        sb.WriteString(fmt.Sprintf("  API 类型: %s\n", globalAPIConfig.APIType))
        if globalAPIConfig.Thinking {
                sb.WriteString("  Thinking: 开启\n")
        } else {
                sb.WriteString("  Thinking: 关闭\n")
        }
        if globalAPIConfig.BaseURL != "" {
                sb.WriteString(fmt.Sprintf("  API 地址: %s\n", globalAPIConfig.BaseURL))
        }

        // 7. 系统状态
        sb.WriteString("\n系统状态:\n")
        sb.WriteString(fmt.Sprintf("  运行模式: %s\n", getRunningMode()))
        if globalRoleManager != nil {
                sb.WriteString(fmt.Sprintf("  可用角色: %d 个\n", globalRoleManager.Count()))
        }
        if globalSkillManager != nil {
                sb.WriteString(fmt.Sprintf("  可用技能: %d 个\n", globalSkillManager.Count()))
        }

        // 8. 最近消息摘要
        if len(history) > 0 {
                sb.WriteString("\n最近消息摘要:\n")
                startIdx := 0
                if len(history) > 3 {
                        startIdx = len(history) - 3
                }
                for i := startIdx; i < len(history); i++ {
                        msg := history[i]
                        role := msg.Role
                        if role == "system" {
                                continue // 跳过系统消息
                        }
                        content := ""
                        if c, ok := msg.Content.(string); ok {
                                content = TruncateString(c, 50)
                        }
                        sb.WriteString(fmt.Sprintf("  [%s] %s\n", role, content))
                }
        }

        return sb.String()
}

// getRunningMode 获取当前运行模式
func getRunningMode() string {
        if cmdModeActive.Load() {
                return "CMD 模式"
        }
        return "Log 模式"
}

// GetHelpText 返回帮助文本
func GetHelpText() string {
        var sb strings.Builder
        sb.WriteString("╔══════════════════════════════════════════════════════════════╗\n")
        sb.WriteString("  📖 GhostClaw 命令帮助\n")
        sb.WriteString("╚══════════════════════════════════════════════════════════════╝\n\n")

        // 系统命令
        sb.WriteString("┌─ 🎮 系统命令 ─────────────────────────────────────────────────\n")
        sb.WriteString("│  /help, /?              显示此帮助信息\n")
        sb.WriteString("│  /exit                  退出程序（所有频道）\n")
        sb.WriteString("│  /quit                  断开连接（网页模式）或切回 Log 模式（终端模式）\n")
        sb.WriteString("│  /stop                  取消当前正在执行的任务\n")
        sb.WriteString("│  /context               查看当前上下文使用情况（消息数、token估算、截断阈值等）\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 角色管理
        sb.WriteString("┌─ 🎭 角色管理 (Role) ────────────────────────────────────────────\n")
        sb.WriteString("│  角色是预设的行为模板，定义 AI 的性格、专业领域与说话风格。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /role                    显示当前使用的角色信息\n")
        sb.WriteString("│  /role list               列出所有可用角色\n")
        sb.WriteString("│  /role <角色名>           切换到指定角色\n")
        sb.WriteString("│  /role show <角色名>      显示角色的详细配置信息\n")
        sb.WriteString("│  /role default            显示当前默认角色\n")
        sb.WriteString("│  /role <角色名> default   设置默认角色（所有新会话使用）\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /role coder            切换到程序员角色\n")
        sb.WriteString("│    /role show teacher     查看教师角色的详细配置\n")
        sb.WriteString("│    /role poet default     设置诗人为默认角色\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 演员管理
        sb.WriteString("┌─ 🎬 演员管理 (Actor) ───────────────────────────────────────────\n")
        sb.WriteString("│  演员是角色的具体实例，可以绑定不同的模型与设定。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /actor                  显示当前演员信息\n")
        sb.WriteString("│  /actor list             列出所有演员\n")
        sb.WriteString("│  /actor <演员名>         切换到指定演员\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /actor list           查看所有演员\n")
        sb.WriteString("│    /actor role_coder     切换到指定演员\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 场景管理
        sb.WriteString("┌─ 🎪 场景管理 (Stage) ───────────────────────────────────────────\n")
        sb.WriteString("│  场景用于多角色协作与自动演绎模式。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /stage                       显示当前场景状态\n")
        sb.WriteString("│  /stage auto on               开启自动演绎（导演模式）\n")
        sb.WriteString("│  /stage auto off              关闭自动演绎\n")
        sb.WriteString("│  /stage auto pause            暂停自动演绎\n")
        sb.WriteString("│  /stage auto resume           恢复自动演绎\n")
        sb.WriteString("│  /stage auto mode <模式>      设置演绎模式\n")
        sb.WriteString("│                               - director: 导演模式（AI 决定谁发言）\n")
        sb.WriteString("│                               - round-robin: 轮流模式\n")
        sb.WriteString("│                               - smart: 智能模式\n")
        sb.WriteString("│  /stage present <演员...>     设置在场角色（空格分隔多个演员）\n")
        sb.WriteString("│  /stage setting <键> <值>     设置场景属性\n")
        sb.WriteString("│                               可用键: world, era, location, time, context\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /stage auto on                    开启导演模式\n")
        sb.WriteString("│    /stage present alice bob charlie  设置三个在场角色\n")
        sb.WriteString("│    /stage setting world 奇幻世界      设置世界观\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 模型管理
        sb.WriteString("┌─ 🤖 模型管理 (Model) ───────────────────────────────────────────\n")
        sb.WriteString("│  管理可用的 AI 模型配置。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /model                 列出所有可用模型\n")
        sb.WriteString("│  /model <模型名>        切换当前演员使用的模型\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /model               查看可用模型列表\n")
        sb.WriteString("│    /model main          切换到 main 模型\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 技能管理
        sb.WriteString("┌─ 🎯 技能管理 (Skill) ───────────────────────────────────────────\n")
        sb.WriteString("│  技能是可激活的提示词模板，用于增强特定任务的执行能力。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /skill, /skills            列出所有可用技能\n")
        sb.WriteString("│  /skill list                列出所有技能\n")
        sb.WriteString("│  /skill <技能名>            激活指定技能\n")
        sb.WriteString("│  /skill show <技能名>       显示技能详细配置\n")
        sb.WriteString("│  /skill create <技能名>     创建新技能模板文件\n")
        sb.WriteString("│  /skill delete <技能名>     删除技能\n")
        sb.WriteString("│  /skill reload              重新加载所有技能文件\n")
        sb.WriteString("│  /skill search <关键词>     按关键词搜索技能\n")
        sb.WriteString("│  /skill tag <标签>          按标签筛选技能\n")
        sb.WriteString("│  /skill help                显示技能命令详细帮助\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /skill translation       激活翻译技能\n")
        sb.WriteString("│    /skill search 写作       搜索与写作相关的技能\n")
        sb.WriteString("│    /skill create my_skill   创建名为 my_skill 的新技能\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 自动演绎
        sb.WriteString("┌─ ⏭️ 自动演绎控制 ───────────────────────────────────────────────\n")
        sb.WriteString("│  /next                      手动切换到下一个演员发言\n")
        sb.WriteString("│                             （仅在自动演绎模式下有效）\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 会话管理
        sb.WriteString("┌─ 💾 会话管理 (Session) ──────────────────────────────────────────\n")
        sb.WriteString("│  会话数据存储在数据库（ghostclaw.db）中，无需手动管理文件。\n")
        sb.WriteString("│\n")
        sb.WriteString("│  /save [描述]            保存当前会话\n")
        sb.WriteString("│  /load [会话ID]          加载会话（不带ID则列出所有会话）\n")
        sb.WriteString("│  /session                显示当前会话信息\n")
        sb.WriteString("│  /session list           列出所有保存的会话\n")
        sb.WriteString("│  /session delete <ID>    删除指定会话\n")
        sb.WriteString("│  /session export <文件>  导出会话到 .toon 文件（备份/迁移）\n")
        sb.WriteString("│  /session import <文件>  从 .toon 文件导入会话\n")
        sb.WriteString("│  /new                    创建新会话（清空当前历史）\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例：\n")
        sb.WriteString("│    /save 我的编程会话    保存当前会话\n")
        sb.WriteString("│    /load                 列出所有保存的会话\n")
        sb.WriteString("│    /session export backup 导出最近会话\n")
        sb.WriteString("│    /new                  开始全新对话\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 记忆系统
        sb.WriteString("┌─ 🧠 记忆系统 (Memory) ──────────────────────────────────────────\n")
        sb.WriteString("│  记忆系统通过 AI 工具调用使用，无直接命令。\n")
        sb.WriteString("│  AI 可使用以下工具管理长期记忆（数据存储在数据库中）：\n")
        sb.WriteString("│\n")
        sb.WriteString("│  memory_save        保存记忆（键值对 + 分类 + 标签）\n")
        sb.WriteString("│  memory_recall      检索记忆（按关键词搜索）\n")
        sb.WriteString("│  memory_get         按键名精确获取记忆\n")
        sb.WriteString("│  memory_forget      删除指定记忆\n")
        sb.WriteString("│  memory_list        列出所有记忆\n")
        sb.WriteString("│  memory_summarize   生成记忆摘要\n")
        sb.WriteString("│\n")
        sb.WriteString("│  记忆分类：preference(偏好), fact(事实), project(项目),\n")
        sb.WriteString("│           skill(技能), context(上下文)\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 定时任务
        sb.WriteString("┌─ ⏰ 定时任务 (Cron) ────────────────────────────────────────────\n")
        sb.WriteString("│  定时任务通过 AI 工具调用配置，无直接命令。\n")
        sb.WriteString("│  AI 可使用以下工具管理定时任务：\n")
        sb.WriteString("│\n")
        sb.WriteString("│  cron_add           添加定时任务（名称、cron表达式、消息）\n")
        sb.WriteString("│  cron_remove        删除定时任务\n")
        sb.WriteString("│  cron_list          列出所有定时任务\n")
        sb.WriteString("│  cron_status        查询任务执行状态\n")
        sb.WriteString("│\n")
        sb.WriteString("│  示例（让 AI 执行）：\n")
        sb.WriteString("│    \"每天早上9点提醒我开会\" -> AI 调用 cron_add\n")
        sb.WriteString("│    \"列出我的定时任务\"   -> AI 调用 cron_list\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 多渠道支持
        sb.WriteString("┌─ 📡 多渠道支持 ────────────────────────────────────────────────\n")
        sb.WriteString("│  GhostClaw 支持 12 个消息渠道，统一使用 GlobalSession 会话：\n")
        sb.WriteString("│\n")
        sb.WriteString("│  Telegram  /  Discord  /  Slack  /  IRC\n")
        sb.WriteString("│  飞书(Feishu)  /  Webhook  /  XMPP  /  Matrix\n")
        sb.WriteString("│  邮件(Email)  /  网页(Web)  /  命令行(CLI)\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        // 数据存储
        sb.WriteString("┌─ 🗄️ 数据存储 ──────────────────────────────────────────────────\n")
        sb.WriteString("│  所有数据统一存储在 ghostclaw.db（SQLite）：\n")
        sb.WriteString("│\n")
        sb.WriteString("│  • Memories         长期记忆（键值对 + 分类 + 标签）\n")
        sb.WriteString("│  • Sessions         会话摘要记录\n")
        sb.WriteString("│  • SessionHistories 完整会话历史（替代文件系统存储）\n")
        sb.WriteString("│  • Experiences       经验教训记录\n")
        sb.WriteString("│\n")
        sb.WriteString("│  备份只需拷贝 ghostclaw.db 文件即可。\n")
        sb.WriteString("└───────────────────────────────────────────────────────────────\n\n")

        sb.WriteString("💡 提示：直接输入自然语言即可与 AI 对话，斜杠命令仅在需要时使用。\n")

        return sb.String()
}

// HandleRoleCommand 处理 /role 命令
func HandleRoleCommand(args string, rm *RoleManager, am *ActorManager, stage *Stage) string {
        args = strings.TrimSpace(args)

        // 无参数：显示当前角色
        if args == "" {
                currentActor := stage.GetCurrentActor()
                actor, _ := am.GetActor(currentActor)
                if actor == nil {
                        return "当前未设置角色"
                }

                role, _ := rm.GetRole(actor.Role)
                icon := "🎭"
                displayName := actor.Role
                if role != nil {
                        icon = role.Icon
                        displayName = role.DisplayName
                }

                var sb strings.Builder
                sb.WriteString(fmt.Sprintf("%s **当前角色：%s**\n\n", icon, displayName))
                if role != nil {
                        sb.WriteString(fmt.Sprintf("**描述**：%s\n", role.Description))
                        sb.WriteString(fmt.Sprintf("**身份**：%s\n", role.Identity))
                        sb.WriteString(fmt.Sprintf("**性格**：%s\n", role.Personality))
                        if len(role.Expertise) > 0 {
                                sb.WriteString(fmt.Sprintf("**专业领域**：%s\n", strings.Join(role.Expertise, "、")))
                        }
                }
                return sb.String()
        }

        // /role list
        if args == "list" {
                roles := rm.ListRoles()
                var sb strings.Builder
                sb.WriteString("📋 **可用角色列表**\n\n")

                sb.WriteString("**预置角色**：\n")
                for _, r := range roles {
                        if r.IsPreset {
                                sb.WriteString(fmt.Sprintf("  %s **%s** (%s) - %s\n", r.Icon, r.DisplayName, r.Name, r.Description))
                        }
                }

                customRoles := rm.ListCustomRoles()
                if len(customRoles) > 0 {
                        sb.WriteString("\n**自定义角色**：\n")
                        for _, r := range customRoles {
                                sb.WriteString(fmt.Sprintf("  %s **%s** (%s) - %s\n", r.Icon, r.DisplayName, r.Name, r.Description))
                        }
                }

                return sb.String()
        }

        // /role show <name>
        if strings.HasPrefix(args, "show ") {
                name := strings.TrimPrefix(args, "show ")
                role, ok := rm.GetRole(name)
                if !ok {
                        return fmt.Sprintf("❌ 未找到角色：%s", name)
                }

                var sb strings.Builder
                sb.WriteString(fmt.Sprintf("%s **%s** (%s)\n\n", role.Icon, role.DisplayName, role.Name))
                sb.WriteString(fmt.Sprintf("**描述**：%s\n\n", role.Description))
                sb.WriteString("**身份定位**：\n")
                sb.WriteString(role.Identity)
                sb.WriteString("\n\n**性格特质**：")
                sb.WriteString(role.Personality)
                sb.WriteString("\n\n**说话风格**：")
                sb.WriteString(role.SpeakingStyle)

                if len(role.Expertise) > 0 {
                        sb.WriteString("\n\n**专业领域**：\n")
                        for _, e := range role.Expertise {
                                sb.WriteString(fmt.Sprintf("- %s\n", e))
                        }
                }

                if len(role.Guidelines) > 0 {
                        sb.WriteString("\n**行为准则**：\n")
                        for _, g := range role.Guidelines {
                                sb.WriteString(fmt.Sprintf("- %s\n", g))
                        }
                }

                if len(role.Forbidden) > 0 {
                        sb.WriteString("\n**禁止事项**：\n")
                        for _, f := range role.Forbidden {
                                sb.WriteString(fmt.Sprintf("- %s\n", f))
                        }
                }

                sb.WriteString(fmt.Sprintf("\n**工具权限模式**：%s\n", role.ToolPermission.Mode))
                if len(role.ToolPermission.AllowedTools) > 0 {
                        sb.WriteString(fmt.Sprintf("**允许工具**：%s\n", strings.Join(role.ToolPermission.AllowedTools, ", ")))
                }
                if len(role.ToolPermission.DeniedTools) > 0 {
                        sb.WriteString(fmt.Sprintf("**禁止工具**：%s\n", strings.Join(role.ToolPermission.DeniedTools, ", ")))
                }

                return sb.String()
        }

        // /role default - 显示当前默认角色
        if args == "default" {
                if defaultRole == "" {
                        return "⚠️ 当前未设置默认角色\n\n使用 /role <角色名> default 设置默认角色"
                }
                role, ok := rm.GetRole(defaultRole)
                if !ok {
                        return fmt.Sprintf("⚠️ 默认角色「%s」不存在", defaultRole)
                }
                return fmt.Sprintf("⭐ **默认角色**：%s %s\n\n所有新会话将默认使用此角色", role.Icon, role.DisplayName)
        }

        // /role <name> default - 设置默认角色
        if strings.HasSuffix(args, " default") {
                name := strings.TrimSuffix(args, " default")
                role, ok := rm.GetRole(name)
                if !ok {
                        return fmt.Sprintf("❌ 未找到角色：%s\n\n使用 /role list 查看可用角色", name)
                }

                // 设置默认角色
                defaultRole = name

                // 更新默认演员的角色
                if globalActorManager != nil {
                        if actor := globalActorManager.GetDefaultActor(); actor != nil {
                                actor.Role = name
                                globalActorManager.SaveToFile()
                        }
                }

                if globalStage != nil {
                        globalStage.SetUpdateSystemPrompt()
                }

                if err := globalConfigManager.UpdateDefaultRole(name); err != nil {
                        return fmt.Sprintf("⚠️ 设置成功但保存配置失败：%v", err)
                }

                return fmt.Sprintf("✅ **默认角色已设置**：%s %s\n\n所有新会话将默认使用此角色", role.Icon, role.DisplayName)
        }

        // /role <name> - 切换角色
        role, ok := rm.GetRole(args)
        if !ok {
                return fmt.Sprintf("❌ 未找到角色：%s\n\n使用 /role list 查看可用角色", args)
        }

        actorName := "role_" + args
        actor, exists := am.GetActor(actorName)
        if !exists {
                actor = &Actor{
                        Name:          actorName,
                        Role:          args,
                        Model:         "main",
                        CharacterName: role.DisplayName,
                        Description:   role.Description,
                }
                am.AddActor(actor)
        }

        stage.SetCurrentActor(actorName)
        return stage.BuildWelcomeMessage(am, rm)
}

// HandleActorCommand 处理 /actor 命令
func HandleActorCommand(args string, am *ActorManager, rm *RoleManager, stage *Stage) string {
        args = strings.TrimSpace(args)

        if args == "" {
                currentActor := stage.GetCurrentActor()
                actor, _ := am.GetActor(currentActor)
                if actor == nil {
                        return "当前未设置演员"
                }

                role, _ := rm.GetRole(actor.Role)
                model, _ := globalConfigManager.GetModel(actor.Model)

                var sb strings.Builder
                sb.WriteString(fmt.Sprintf("🎭 **当前演员：%s**\n\n", actor.Name))
                sb.WriteString(fmt.Sprintf("- **角色模板**：%s\n", actor.Role))
                if role != nil {
                        sb.WriteString(fmt.Sprintf("- **角色名**：%s %s\n", role.Icon, role.DisplayName))
                }
                if actor.CharacterName != "" {
                        sb.WriteString(fmt.Sprintf("- **扮演角色**：%s\n", actor.CharacterName))
                }
                if model != nil {
                        sb.WriteString(fmt.Sprintf("- **使用模型**：%s (%s)\n", model.Name, model.Model))
                }
                if actor.Description != "" {
                        sb.WriteString(fmt.Sprintf("- **描述**：%s\n", actor.Description))
                }
                return sb.String()
        }

        if args == "list" {
                actors := am.ListActors()
                var sb strings.Builder
                sb.WriteString("📋 **演员列表**\n\n")

                for _, a := range actors {
                        role, _ := rm.GetRole(a.Role)
                        icon := "🎭"
                        if role != nil {
                                icon = role.Icon
                        }
                        defaultMark := ""
                        if a.IsDefault {
                                defaultMark = " ⭐默认"
                        }
                        charName := a.CharacterName
                        if charName == "" {
                                charName = a.Name
                        }
                        sb.WriteString(fmt.Sprintf("  %s **%s** (%s)%s\n", icon, charName, a.Name, defaultMark))
                }
                return sb.String()
        }

        _, ok := am.GetActor(args)
        if !ok {
                return fmt.Sprintf("❌ 未找到演员：%s\n\n使用 /actor list 查看可用演员", args)
        }

        stage.SetCurrentActor(args)
        return stage.BuildWelcomeMessage(am, rm)
}

// HandleStageCommand 处理 /stage 命令
func HandleStageCommand(args string, am *ActorManager, rm *RoleManager, stage *Stage) string {
        args = strings.TrimSpace(args)

        if args == "" {
                enabled, paused, turns, maxTurns, mode := stage.GetAutoSwitchState()
                setting := stage.GetSetting()
                present := stage.GetPresentActors()
                currentActor := stage.GetCurrentActor()

                var sb strings.Builder
                sb.WriteString("🎭 **当前场景状态**\n\n")
                sb.WriteString(fmt.Sprintf("**当前演员**：%s\n", currentActor))
                sb.WriteString(fmt.Sprintf("**在场角色**：%s\n", strings.Join(present, ", ")))
                sb.WriteString("\n**自动演绎**：")
                if !enabled {
                        sb.WriteString("关闭\n")
                } else if paused {
                        sb.WriteString(fmt.Sprintf("已暂停 (%d/%d轮)\n", turns, maxTurns))
                } else {
                        sb.WriteString(fmt.Sprintf("开启 (%s模式, %d/%d轮)\n", mode, turns, maxTurns))
                }

                if setting.World != "" || setting.CurrentLocation != "" {
                        sb.WriteString("\n**场景设定**：\n")
                        if setting.World != "" {
                                sb.WriteString(fmt.Sprintf("- 世界：%s\n", setting.World))
                        }
                        if setting.Era != "" {
                                sb.WriteString(fmt.Sprintf("- 时代：%s\n", setting.Era))
                        }
                        if setting.CurrentLocation != "" {
                                sb.WriteString(fmt.Sprintf("- 地点：%s\n", setting.CurrentLocation))
                        }
                        if setting.CurrentTime != "" {
                                sb.WriteString(fmt.Sprintf("- 时间：%s\n", setting.CurrentTime))
                        }
                }
                return sb.String()
        }

        if strings.HasPrefix(args, "auto ") {
                subArgs := strings.TrimPrefix(args, "auto ")
                switch subArgs {
                case "on":
                        stage.EnableAutoSwitch(AutoSwitchDirector)
                        return "▶️ **自动演绎已开启** (导演模式)\n\n模型将根据场景自动切换角色视角"
                case "off":
                        stage.DisableAutoSwitch()
                        return "⏹️ **自动演绎已关闭**"
                case "pause":
                        stage.PauseAutoSwitch()
                        _, _, turns, maxTurns, _ := stage.GetAutoSwitchState()
                        return fmt.Sprintf("⏸️ **自动演绎已暂停** (%d/%d轮)", turns, maxTurns)
                case "resume":
                        stage.ResumeAutoSwitch()
                        return "▶️ **自动演绎已恢复**"
                default:
                        if strings.HasPrefix(subArgs, "mode ") {
                                modeStr := strings.TrimPrefix(subArgs, "mode ")
                                mode := AutoSwitchMode(modeStr)
                                if mode != AutoSwitchDirector && mode != AutoSwitchRoundRobin && mode != AutoSwitchSmart {
                                        return fmt.Sprintf("❌ 无效的模式：%s\n\n可用模式：director, round-robin, smart", modeStr)
                                }
                                stage.EnableAutoSwitch(mode)
                                return fmt.Sprintf("▶️ **自动演绎已开启** (%s模式)", mode)
                        }
                        return "用法：/stage auto on|off|pause|resume|mode <director|round-robin|smart>"
                }
        }

        if strings.HasPrefix(args, "present ") {
                actorsStr := strings.TrimPrefix(args, "present ")
                actors := strings.Fields(actorsStr)
                if len(actors) == 0 {
                        return "❌ 请指定在场角色"
                }
                for _, a := range actors {
                        if _, ok := am.GetActor(a); !ok {
                                return fmt.Sprintf("❌ 未找到演员：%s", a)
                        }
                }
                stage.SetPresentActors(actors)
                return fmt.Sprintf("✅ **在场角色已设置**：%s", strings.Join(actors, ", "))
        }

        if strings.HasPrefix(args, "setting ") {
                settingArgs := strings.TrimPrefix(args, "setting ")
                parts := strings.SplitN(settingArgs, " ", 2)
                if len(parts) < 2 {
                        return "用法：/stage setting <key> <value>\n\n可用键：world, era, location, time, context"
                }
                key := parts[0]
                value := parts[1]
                setting := stage.GetSetting()
                switch key {
                case "world":
                        setting.World = value
                case "era":
                        setting.Era = value
                case "location":
                        setting.CurrentLocation = value
                case "time":
                        setting.CurrentTime = value
                case "context":
                        setting.AdditionalContext = value
                default:
                        return fmt.Sprintf("❌ 未知的设定键：%s\n\n可用键：world, era, location, time, context", key)
                }
                stage.SetSetting(setting)
                return fmt.Sprintf("✅ **场景设定已更新**：%s = %s", key, value)
        }

        return "用法：/stage [auto|present|setting] ...\n\n" +
                "  /stage auto on          - 开启自动演绎\n" +
                "  /stage auto off         - 关闭自动演绎\n" +
                "  /stage auto pause       - 暂停自动演绎\n" +
                "  /stage auto resume      - 恢复自动演绎\n" +
                "  /stage present <actors> - 设置在场角色\n" +
                "  /stage setting <k> <v>  - 设置场景属性"
}

// HandleNextCommand 处理 /next 命令
func HandleNextCommand(am *ActorManager, rm *RoleManager, stage *Stage) string {
        if !stage.AutoSwitchEnabled() {
                return "⚠️ 自动演绎未开启，使用 /stage auto on 开启"
        }
        nextActor := stage.GetNextActorForRoundRobin()
        stage.AdvanceRoundRobin()
        stage.SetCurrentActor(nextActor)
        return stage.BuildWelcomeMessage(am, rm)
}

// HandleModelCommand 处理 /model 命令
func HandleModelCommand(args string, am *ActorManager) string {
        args = strings.TrimSpace(args)

        if args == "" {
                models := globalConfigManager.ListModels()
                var sb strings.Builder
                sb.WriteString("📋 **可用模型列表**\n\n")
                for _, m := range models {
                        sb.WriteString(fmt.Sprintf("  **%s** (%s)\n", m.Name, m.Model))
                        if m.Description != "" {
                                sb.WriteString(fmt.Sprintf("    %s\n", m.Description))
                        }
                }
                return sb.String()
        }

        currentActor := globalStage.GetCurrentActor()
        actor, ok := am.GetActor(currentActor)
        if !ok {
                return "❌ 当前演员不存在"
        }
        model, ok := globalConfigManager.GetModel(args)
        if !ok {
                return fmt.Sprintf("❌ 未找到模型：%s\n\n使用 /model 查看可用模型", args)
        }
        actor.Model = args
        return fmt.Sprintf("✅ **模型已切换**：%s → %s (%s)", currentActor, model.Name, model.Model)
}

// HandleSessionCommand 处理 /session 命令（使用全局会话）
func HandleSessionCommand(args string) string {
        args = strings.TrimSpace(args)

        if globalSessionPersist == nil {
                return "❌ 会话持久化未初始化"
        }

        session := GetGlobalSession()

        if args == "" {
                var sb strings.Builder
                sb.WriteString("📋 **当前会话信息**\n\n")
                sb.WriteString(fmt.Sprintf("- **会话ID**：%s\n", session.ID))
                sb.WriteString(fmt.Sprintf("- **消息数**：%d\n", len(session.GetHistory())))
                sb.WriteString(fmt.Sprintf("- **任务运行中**：%v\n", session.IsTaskRunning()))
                sessions, err := globalSessionPersist.ListSessions()
                if err != nil {
                        sb.WriteString(fmt.Sprintf("- **保存的会话**：读取失败 (%s)\n", err))
                } else {
                        sb.WriteString(fmt.Sprintf("- **保存的会话**：%d 个\n", len(sessions)))
                }
                sb.WriteString("\n使用 /session list 查看所有保存的会话")
                return sb.String()
        }

        if args == "list" {
                sessions, err := globalSessionPersist.ListSessions()
                if err != nil {
                        return fmt.Sprintf("❌ 读取会话列表失败：%s", err)
                }
                if len(sessions) == 0 {
                        return "📋 **保存的会话列表**\n\n暂无保存的会话\n\n使用 /save [描述] 保存当前会话"
                }
                var sb strings.Builder
                sb.WriteString("📋 **保存的会话列表**\n\n")
                for i, s := range sessions {
                        if i >= 20 {
                                sb.WriteString(fmt.Sprintf("\n... 还有 %d 个会话", len(sessions)-20))
                                break
                        }
                        desc := s.Description
                        if desc == "" {
                                desc = "无描述"
                        }
                        if len(desc) > 30 {
                                desc = desc[:30] + "..."
                        }
                        sb.WriteString(fmt.Sprintf("  **%s** - %s\n    %s (%d 条消息)\n",
                                s.ID, desc, s.UpdatedAt.Format("2006-01-02 15:04"), len(s.History)))
                }
                sb.WriteString("\n使用 /load <会话ID> 加载会话")
                return sb.String()
        }

        if strings.HasPrefix(args, "delete ") {
                sessionID := strings.TrimSpace(strings.TrimPrefix(args, "delete "))
                if err := globalSessionPersist.DeleteSession(sessionID); err != nil {
                        return fmt.Sprintf("❌ 删除会话失败：%s", err)
                }
                return fmt.Sprintf("✅ **会话已删除**：%s", sessionID)
        }

        if strings.HasPrefix(args, "export ") {
                filePath := strings.TrimSpace(strings.TrimPrefix(args, "export "))
                if filePath == "" {
                        return "用法：/session export <文件路径>\n\n示例：/session export my_session.json"
                }
                sessions, err := globalSessionPersist.ListSessions()
                if err != nil || len(sessions) == 0 {
                        return "❌ 没有可导出的会话，请先使用 /save 保存"
                }
                latestSession := sessions[0]
                if err := globalSessionPersist.ExportSession(latestSession.ID, filePath); err != nil {
                        return fmt.Sprintf("❌ 导出失败：%s", err)
                }
                return fmt.Sprintf("✅ **会话已导出**：%s\n会话ID：%s", filePath, latestSession.ID)
        }

        if strings.HasPrefix(args, "import ") {
                filePath := strings.TrimSpace(strings.TrimPrefix(args, "import "))
                if filePath == "" {
                        return "用法：/session import <文件路径>\n\n示例：/session import my_session.json"
                }
                saved, err := globalSessionPersist.ImportSession(filePath)
                if err != nil {
                        return fmt.Sprintf("❌ 导入失败：%s", err)
                }
                return fmt.Sprintf("✅ **会话已导入**\n会话ID：%s\n消息数：%d", saved.ID, len(saved.History))
        }

        return "用法：/session [list|delete|export|import]\n\n" +
                "  /session              - 显示当前会话信息\n" +
                "  /session list         - 列出所有保存的会话\n" +
                "  /session delete <ID>  - 删除指定会话\n" +
                "  /session export <文件> - 导出会话\n" +
                "  /session import <文件> - 导入会话"
}

// HandleSaveCommand 处理 /save 命令
func HandleSaveCommand(args string, session *GlobalSession) string {
        description := strings.TrimSpace(args)

        if globalSessionPersist == nil {
                return "❌ 会话持久化未初始化"
        }

        history := session.GetHistory()
        if len(history) == 0 {
                return "❌ 当前会话没有消息可保存"
        }

        saved, err := globalSessionPersist.SaveSession(session.ID, history, description)
        if err != nil {
                return fmt.Sprintf("❌ 保存会话失败：%s", err)
        }

        desc := saved.Description
        if desc == "" {
                desc = "无描述"
        }

        return fmt.Sprintf("✅ **会话已保存**\n会话ID：%s\n描述：%s\n消息数：%d",
                saved.ID, desc, len(saved.History))
}

// HandleLoadCommand 处理 /load 命令
func HandleLoadCommand(args string, session *GlobalSession) string {
        sessionID := strings.TrimSpace(args)

        if globalSessionPersist == nil {
                return "❌ 会话持久化未初始化"
        }

        if sessionID == "" {
                return HandleSessionCommand("list")
        }

        saved, err := globalSessionPersist.LoadSession(sessionID)
        if err != nil {
                return fmt.Sprintf("❌ 加载会话失败：%s\n\n使用 /load 查看可用会话", err)
        }
        if saved == nil {
                return fmt.Sprintf("📭 未找到会话：%s\n\n使用 /load 查看可用会话", sessionID)
        }

        session.SetHistory(saved.History)

        if saved.Role != "" && globalRoleManager != nil {
                if _, ok := globalRoleManager.GetRole(saved.Role); ok {
                        HandleRoleCommand(saved.Role, globalRoleManager, globalActorManager, globalStage)
                }
        }

        return fmt.Sprintf("✅ **会话已加载**\n会话ID：%s\n描述：%s\n消息数：%d\n\n会话历史已恢复，可以继续对话",
                saved.ID, saved.Description, len(saved.History))
}

// HandleNewCommand 处理 /new 命令 — 完整重置所有會話狀態
func HandleNewCommand() string {
        session := GetGlobalSession()

        // B5 修復：如果當前任務正在運行，先取消任務再重置，
        // 防止 deferred SetHistory 在任務結束後還原舊歷史覆蓋新會話
        if session.IsTaskRunning() {
                session.CancelTask()
                log.Printf("[NewCommand] Cancelled running task before session reset")
        }

        session.mu.Lock()
        session.ID = time.Now().Format("20060102_150405") + "_default"
        session.fullSessionReset("new_command")
        session.mu.Unlock()

        // 顯式持久化空 session 到 DB（使用 persistEmptySession 而非 autoSaveHistory，
        // 因爲 autoSaveHistory 對空歷史會早期返回）
        session.persistEmptySession("new_command")

        log.Printf("[NewCommand] Full session reset completed: id=%s, tracker_reset=true, input_queue_cleared, memory_consolidator_cleared, auto_turns_reset, persistID_cleared", session.ID)
        return fmt.Sprintf("✅ **新会话已创建**\n会话ID：%s\n\n可以开始新的对话", session.ID)
}

// GetCurrentWebSession 获取当前会话（返回全局会话，保持接口兼容）
func GetCurrentWebSession() *GlobalSession {
        return GetGlobalSession()
}

// handleRoleToolCall 处理角色相关的工具调用
func handleRoleToolCall(ctx context.Context, toolName string, argsMap map[string]interface{}, ch Channel, rm *RoleManager, am *ActorManager, stage *Stage) (string, bool) {
        switch toolName {
        case "role_switch":
                name, _ := argsMap["name"].(string)
                if name == "" {
                        return "Error: missing 'name' parameter", false
                }
                return HandleRoleCommand(name, rm, am, stage), false

        case "actor_switch":
                name, _ := argsMap["name"].(string)
                if name == "" {
                        return "Error: missing 'name' parameter", false
                }
                return HandleActorCommand(name, am, rm, stage), false

        case "stage_config":
                action, _ := argsMap["action"].(string)
                switch action {
                case "auto_on":
                        mode, _ := argsMap["mode"].(string)
                        if mode == "" {
                                mode = "director"
                        }
                        stage.EnableAutoSwitch(AutoSwitchMode(mode))
                        return "Auto-switch enabled", false
                case "auto_off":
                        stage.DisableAutoSwitch()
                        return "Auto-switch disabled", false
                case "pause":
                        stage.PauseAutoSwitch()
                        return "Auto-switch paused", false
                case "resume":
                        stage.ResumeAutoSwitch()
                        return "Auto-switch resumed", false
                default:
                        return HandleStageCommand("", am, rm, stage), false
                }

        default:
                return "Error: Unknown role tool", false
        }
}
