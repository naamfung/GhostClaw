package main

import (
        "fmt"
        "strings"
)

// ProcessSkillCommand 处理技能相关的斜杠命令
func ProcessSkillCommand(line string, sm *SkillManager, rm *RoleManager, stage *Stage) (handled bool, response string) {
        parts := strings.Fields(line)
        if len(parts) == 0 {
                return false, ""
        }

        switch parts[0] {
        case "/skill", "/skills":
                return handleSkillCommand(parts[1:], sm)
        }

        return false, ""
}

// handleSkillCommand 处理 /skill 命令
func handleSkillCommand(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                // 显示技能列表
                return handleSkillList(args, sm)
        }

        subCmd := args[0]
        switch subCmd {
        case "list", "ls", "":
                return handleSkillList(args[1:], sm)
        case "show", "get":
                return handleSkillShow(args[1:], sm)
        case "create", "new":
                return handleSkillCreate(args[1:], sm)
        case "delete", "rm", "remove":
                return handleSkillDelete(args[1:], sm)
        case "reload":
                return handleSkillReload(args[1:], sm)
        case "search", "find":
                return handleSkillSearch(args[1:], sm)
        case "tag":
                return handleSkillTag(args[1:], sm)
        case "help":
                return true, GetSkillCommandsHelp()
        default:
                // 尝试作为技能名称激活
                return handleSkillActivate(args, sm)
        }
}

// handleSkillList 列出所有技能
func handleSkillList(args []string, sm *SkillManager) (bool, string) {
        skills := sm.ListSkills()

        if len(skills) == 0 {
                return true, `📭 没有可用的技能

使用 /skill create <技能名> 创建新技能
技能文件存储在 skills/ 目录`
        }

        var sb strings.Builder
        sb.WriteString("🎯 可用技能:\n\n")

        for i, skill := range skills {
                icon := "📄"
                if len(skill.Tags) > 0 {
                        icon = "🔧"
                }
                sb.WriteString(fmt.Sprintf("%d. %s **%s** (`%s`)\n", i+1, icon, skill.DisplayName, skill.Name))
                if skill.Description != "" {
                        desc := skill.Description
                        if len(desc) > 60 {
                                desc = desc[:57] + "..."
                        }
                        sb.WriteString(fmt.Sprintf("   %s\n", desc))
                }
                if len(skill.Tags) > 0 {
                        sb.WriteString(fmt.Sprintf("   🏷️ %s\n", strings.Join(skill.Tags, ", ")))
                }
                sb.WriteString("\n")
        }

        sb.WriteString("💡 使用 /skill <技能名> 激活技能")
        return true, sb.String()
}

// handleSkillShow 显示技能详情
func handleSkillShow(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                return true, "用法: /skill show <技能名>"
        }

        skillName := args[0]
        skill, ok := sm.GetSkill(skillName)
        if !ok {
                return true, fmt.Sprintf("❌ 技能不存在: %s", skillName)
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("🎯 **%s** (`%s`)\n\n", skill.DisplayName, skill.Name))

        if skill.Description != "" {
                sb.WriteString("📝 **描述**\n")
                sb.WriteString(skill.Description)
                sb.WriteString("\n\n")
        }

        if len(skill.TriggerWords) > 0 {
                sb.WriteString("🔑 **触发关键词**\n")
                for _, tw := range skill.TriggerWords {
                        sb.WriteString(fmt.Sprintf("- %s\n", tw))
                }
                sb.WriteString("\n")
        }

        if skill.SystemPrompt != "" {
                sb.WriteString("📜 **系统提示**\n")
                sb.WriteString("```\n")
                if len(skill.SystemPrompt) > 500 {
                        sb.WriteString(skill.SystemPrompt[:497] + "...")
                } else {
                        sb.WriteString(skill.SystemPrompt)
                }
                sb.WriteString("\n```\n\n")
        }

        if skill.OutputFormat != "" {
                sb.WriteString("📋 **输出格式**\n")
                sb.WriteString(skill.OutputFormat)
                sb.WriteString("\n\n")
        }

        if len(skill.Tags) > 0 {
                sb.WriteString("🏷️ **标签**\n")
                sb.WriteString(strings.Join(skill.Tags, ", "))
                sb.WriteString("\n")
        }

        return true, sb.String()
}

// handleSkillCreate 创建新技能
func handleSkillCreate(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                return true, "用法: /skill create <技能名>"
        }

        name := args[0]
        path, err := sm.CreateSkillFile(name)
        if err != nil {
                return true, fmt.Sprintf("❌ 创建失败: %v", err)
        }

        return true, fmt.Sprintf(`✅ 已创建技能模板

📁 文件: %s

请编辑该文件，填写技能定义。完成后使用 /skill reload 重新加载。

💡 提示:
- 主标题 (# ) 是技能显示名称
- ## 描述 - 技能简介
- ## 触发关键词 - 自动触发的词汇
- ## 系统提示 - 激活时注入的提示词
- ## 输出格式 - 输出格式要求
- ## 标签 - 用于分类`, path)
}

// handleSkillDelete 删除技能
func handleSkillDelete(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                return true, "用法: /skill delete <技能名>"
        }

        skillName := args[0]
        if err := sm.DeleteSkill(skillName); err != nil {
                return true, fmt.Sprintf("❌ 删除失败: %v", err)
        }

        return true, fmt.Sprintf("✅ 已删除技能: %s", skillName)
}

// handleSkillReload 重新加载技能
func handleSkillReload(args []string, sm *SkillManager) (bool, string) {
        if err := sm.Reload(); err != nil {
                return true, fmt.Sprintf("❌ 重新加载失败: %v", err)
        }

        return true, fmt.Sprintf("✅ 已重新加载技能，共 %d 个", sm.Count())
}

// handleSkillSearch 搜索技能
func handleSkillSearch(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                return true, "用法: /skill search <关键词>"
        }

        keyword := strings.ToLower(strings.Join(args, " "))
        skills := sm.ListSkills()

        var matched []*Skill
        for _, skill := range skills {
                // 搜索名称、描述、标签
                if strings.Contains(strings.ToLower(skill.Name), keyword) ||
                        strings.Contains(strings.ToLower(skill.DisplayName), keyword) ||
                        strings.Contains(strings.ToLower(skill.Description), keyword) {
                        matched = append(matched, skill)
                        continue
                }
                for _, tag := range skill.Tags {
                        if strings.Contains(strings.ToLower(tag), keyword) {
                                matched = append(matched, skill)
                                break
                        }
                }
        }

        if len(matched) == 0 {
                return true, fmt.Sprintf("🔍 没有找到匹配 '%s' 的技能", keyword)
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("🔍 搜索结果 (%d):\n\n", len(matched)))

        for i, skill := range matched {
                sb.WriteString(fmt.Sprintf("%d. **%s** (`%s`)\n", i+1, skill.DisplayName, skill.Name))
                if skill.Description != "" {
                        desc := skill.Description
                        if len(desc) > 50 {
                                desc = desc[:47] + "..."
                        }
                        sb.WriteString(fmt.Sprintf("   %s\n", desc))
                }
        }

        return true, sb.String()
}

// handleSkillTag 按标签查找技能
func handleSkillTag(args []string, sm *SkillManager) (bool, string) {
        if len(args) == 0 {
                return true, "用法: /skill tag <标签名>"
        }

        tag := args[0]
        skills := sm.GetSkillsByTag(tag)

        if len(skills) == 0 {
                return true, fmt.Sprintf("🏷️ 没有找到标签为 '%s' 的技能", tag)
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("🏷️ 标签 '%s' 下的技能:\n\n", tag))

        for i, skill := range skills {
                sb.WriteString(fmt.Sprintf("%d. **%s** (`%s`)\n", i+1, skill.DisplayName, skill.Name))
        }

        return true, sb.String()
}

// handleSkillActivate 激活技能（返回技能提示）
func handleSkillActivate(args []string, sm *SkillManager) (bool, string) {
        skillName := args[0]
        skill, ok := sm.GetSkill(skillName)
        if !ok {
                return true, fmt.Sprintf("❌ 技能不存在: %s\n\n使用 /skill list 查看可用技能", skillName)
        }

        // 返回激活提示
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("🎯 **已激活技能: %s**\n\n", skill.DisplayName))

        if skill.Description != "" {
                sb.WriteString(skill.Description)
                sb.WriteString("\n\n")
        }

        if len(skill.TriggerWords) > 0 {
                sb.WriteString("🔑 触发关键词: ")
                sb.WriteString(strings.Join(skill.TriggerWords, ", "))
                sb.WriteString("\n\n")
        }

        sb.WriteString("---\n")
        sb.WriteString("技能提示已注入到当前对话上下文。\n")
        sb.WriteString("接下来你可以开始与此技能相关的对话。")

        return true, sb.String()
}

// GetSkillCommandsHelp 获取技能命令帮助
func GetSkillCommandsHelp() string {
        return `🎯 技能管理命令:

  /skill                列出所有技能
  /skill list           列出所有技能
  /skill <技能名>       激活指定技能
  /skill show <技能名>  显示技能详情
  /skill create <名称>  创建新技能模板
  /skill delete <名称>  删除技能
  /skill reload         重新加载所有技能
  /skill search <关键词> 搜索技能
  /skill tag <标签>     按标签查找技能

📁 技能文件存储在 skills/ 目录，格式为 Markdown

💡 技能文件结构:
  # 技能名称           -> 显示名称
  ## 描述              -> 技能简介
  ## 触发关键词        -> 自动触发的词汇列表
  ## 系统提示          -> 激活时注入的提示词
  ## 输出格式          -> 输出格式要求
  ## 标签              -> 分类标签
`
}

// ActiveSkill 当前激活的技能（用于构建系统提示）
var ActiveSkill *Skill

// SetActiveSkill 设置当前激活的技能
func SetActiveSkill(skill *Skill) {
        ActiveSkill = skill
}

// GetActiveSkillPrompt 获取当前激活技能的提示
func GetActiveSkillPrompt() string {
        if ActiveSkill == nil {
                return ""
        }
        return ActiveSkill.BuildSkillPrompt()
}

// ClearActiveSkill 清除当前激活的技能
func ClearActiveSkill() {
        ActiveSkill = nil
}
