package main

import (
	"context"
	"fmt"
	"strings"
)

// CommandResult 斜杠命令处理结果
type CommandResult struct {
	Handled  bool   // 是否已处理
	Response string // 响应文本
	IsExit   bool   // 是否需要退出
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
	case "exit", "quit", "q":
		return CommandResult{Handled: true, Response: "再见！", IsExit: true}

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

	case "help", "?":
		return CommandResult{Handled: true, Response: GetHelpText()}

	default:
		return CommandResult{Handled: false}
	}
}

// GetHelpText 返回帮助文本（与原始版本相同，省略...）
func GetHelpText() string {
	// 保持原有完整帮助文本，省略重复内容
	// 实际使用时请保留原文件中的完整实现
	return "Help text here"
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
			sb.WriteString(fmt.Sprintf("**身份**：%s\n", truncateString(role.Identity, 100)))
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

		if err := saveConfigToFile(); err != nil {
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
		model, _ := am.GetModel(actor.Model)

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
		models := am.ListModels()
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
	model, ok := am.GetModel(args)
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

	session.SetHistory(saved.History)

	if saved.Role != "" && globalRoleManager != nil {
		if _, ok := globalRoleManager.GetRole(saved.Role); ok {
			HandleRoleCommand(saved.Role, globalRoleManager, globalActorManager, globalStage)
		}
	}

	return fmt.Sprintf("✅ **会话已加载**\n会话ID：%s\n描述：%s\n消息数：%d\n\n会话历史已恢复，可以继续对话",
		saved.ID, saved.Description, len(saved.History))
}

// HandleNewCommand 处理 /new 命令
func HandleNewCommand() string {
	session := GetGlobalSession()
	session.SetHistory([]Message{})
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
