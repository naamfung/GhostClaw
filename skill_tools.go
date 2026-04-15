package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
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
	case "patch":
		return handleSkillPatch(args[1:], sm)
	case "update":
		return handleSkillUpdate(args[1:], sm)
	case "repair":
		return handleSkillRepair(args[1:], sm)
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
			sb.WriteString(fmt.Sprintf("   %s\n", skill.Description))
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
		sb.WriteString("\n\n")
	}

	// 显示关联文件
	if len(skill.LinkedFiles) > 0 {
		sb.WriteString("📁 **关联文件**\n")
		for dir, files := range skill.LinkedFiles {
			sb.WriteString(fmt.Sprintf("- %s：\n", dir))
			for _, file := range files {
				sb.WriteString(fmt.Sprintf("  - %s\n", file))
			}
		}
		sb.WriteString("\n")
	}

	// 显示文件路径
	sb.WriteString(fmt.Sprintf("📄 **文件路径**\n%s\n", skill.FilePath))

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
			sb.WriteString(fmt.Sprintf("   %s\n", skill.Description))
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

// ---------------------------------------------------------------------------
// Skill self-repair: patch, update, repair commands
// ---------------------------------------------------------------------------

// sectionAliases maps normalized English section names to Chinese markdown headings
// used in SKILL.md files.
var sectionAliases = map[string]string{
	"description":   "描述",
	"system_prompt": "系统提示",
	"output_format": "输出格式",
	"triggers":      "触发关键词",
	"tags":          "标签",
	"examples":      "示例",
}

// parseFlags parses positional arguments and --flag value pairs from a raw arg slice.
func parseFlags(args []string) (positional []string, flags map[string]string) {
	positional = []string{}
	flags = make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = ""
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return
}

// resolveSectionHeading returns the Chinese markdown heading for a normalized section
// name, or the raw name itself if no alias is found.
func resolveSectionHeading(section string) string {
	if heading, ok := sectionAliases[strings.ToLower(section)]; ok {
		return heading
	}
	return section
}

// ---------------------------------------------------------------------------
// handleSkillPatch — patch specific sections of a skill
// Usage: /skill patch <skill_name> --section <section> --content <content>
// ---------------------------------------------------------------------------

func handleSkillPatch(args []string, sm *SkillManager) (bool, string) {
	positional, flags := parseFlags(args)

	if len(positional) == 0 {
		return true, "用法: /skill patch <技能名> --section <节名> --content <内容>\n\n" +
			"可用节名: description, system_prompt, output_format, triggers, tags, examples"
	}

	skillName := positional[0]
	section, hasSection := flags["section"]
	content, hasContent := flags["content"]

	if !hasSection || !hasContent {
		return true, "❌ 必须同时指定 --section 和 --content\n\n" +
			"用法: /skill patch <技能名> --section <节名> --content <内容>\n\n" +
			"可用节名: description, system_prompt, output_format, triggers, tags, examples"
	}

	result, err := PatchSkill(skillName, section, content)
	if err != nil {
		return true, fmt.Sprintf("❌ 修补失败: %v", err)
	}

	return true, result
}

// ---------------------------------------------------------------------------
// handleSkillUpdate — full skill content update from a file
// Usage: /skill update <skill_name> --file <path> [--append]
// ---------------------------------------------------------------------------

func handleSkillUpdate(args []string, sm *SkillManager) (bool, string) {
	positional, flags := parseFlags(args)

	if len(positional) == 0 {
		return true, "用法: /skill update <技能名> --file <文件路径> [--append]"
	}

	skillName := positional[0]
	filePath, hasFile := flags["file"]
	_, appendMode := flags["append"]

	if !hasFile {
		return true, "❌ 必须指定 --file <文件路径>\n\n用法: /skill update <技能名> --file <文件路径> [--append]"
	}

	// Verify skill exists
	if globalSkillManager == nil {
		return true, "❌ 技能管理器未初始化"
	}
	skill, ok := globalSkillManager.GetSkill(skillName)
	if !ok {
		return true, fmt.Sprintf("❌ 技能不存在: %s", skillName)
	}

	// Read the source file
	srcData, err := os.ReadFile(filePath)
	if err != nil {
		return true, fmt.Sprintf("❌ 无法读取文件 '%s': %v", filePath, err)
	}
	srcContent := string(srcData)

	if appendMode {
		// Append to existing skill file
		existingData, err := os.ReadFile(skill.FilePath)
		if err != nil {
			return true, fmt.Sprintf("❌ 无法读取技能文件: %v", err)
		}
		combined := string(existingData) + "\n\n" + srcContent
		if err := os.WriteFile(skill.FilePath, []byte(combined), 0644); err != nil {
			return true, fmt.Sprintf("❌ 写入失败: %v", err)
		}
	} else {
		// Replace entire skill file
		if err := os.WriteFile(skill.FilePath, []byte(srcContent), 0644); err != nil {
			return true, fmt.Sprintf("❌ 写入失败: %v", err)
		}
	}

	// Reload
	if err := globalSkillManager.Reload(); err != nil {
		return true, fmt.Sprintf("⚠️ 文件已更新但重新加载失败: %v", err)
	}

	mode := "替换"
	if appendMode {
		mode = "追加"
	}
	return true, fmt.Sprintf("✅ 已%s技能 '%s' 的内容（来源: %s），已自动重新加载。", mode, skillName, filePath)
}

// ---------------------------------------------------------------------------
// handleSkillRepair — auto-repair a broken skill
// Usage: /skill repair <skill_name>
// ---------------------------------------------------------------------------

func handleSkillRepair(args []string, sm *SkillManager) (bool, string) {
	if len(args) == 0 {
		return true, "用法: /skill repair <技能名>"
	}

	skillName := args[0]
	result, err := RepairSkill(skillName)
	if err != nil {
		return true, fmt.Sprintf("❌ 修复失败: %v", err)
	}

	return true, result
}

// ---------------------------------------------------------------------------
// Programmatic API (for agent use via tools)
// ---------------------------------------------------------------------------

// PatchSkill programmatically patches a section of a skill's SKILL.md file.
// It reads the file from disk, replaces (or appends) the targeted section, writes
// back, and calls sm.Reload() so the in-memory state is refreshed.
//
// Parameters:
//   - skillName: the skill identifier (matches Skill.Name)
//   - section:   normalized English section name (description, system_prompt, etc.)
//   - newContent: the replacement content for that section
//
// Returns a human-readable summary of changes, or an error.
func PatchSkill(skillName string, section string, newContent string) (string, error) {
	if globalSkillManager == nil {
		return "", fmt.Errorf("skill manager not initialized")
	}

	skill, ok := globalSkillManager.GetSkill(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}

	filePath := skill.FilePath
	heading := resolveSectionHeading(section)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read skill file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	targetMarker := "## " + heading

	// Locate the section boundaries.
	sectionStart := -1
	sectionEnd := len(lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == targetMarker || trimmed == "## "+heading {
			sectionStart = i + 1 // content starts on the line after the heading
		} else if sectionStart != -1 && sectionStart < i && strings.HasPrefix(trimmed, "## ") {
			sectionEnd = i
			break
		}
	}

	var action string
	var newLines []string

	if sectionStart == -1 {
		// Section not found — append it before any trailing empty lines.
		action = "新增"
		contentLines := strings.Split(newContent, "\n")
		newLines = append(lines, "")
		newLines = append(newLines, targetMarker)
		newLines = append(newLines, contentLines...)
	} else {
		// Replace the section content in-place.
		action = "替换"
		contentLines := strings.Split(newContent, "\n")
		newLines = append(lines[:sectionStart], contentLines...)
		newLines = append(newLines, lines[sectionEnd:]...)
	}

	result := strings.Join(newLines, "\n")
	if err := os.WriteFile(filePath, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("failed to write skill file: %w", err)
	}

	// Reload so in-memory state is refreshed.
	if err := globalSkillManager.Reload(); err != nil {
		return "", fmt.Errorf("file written but reload failed: %w", err)
	}

	return fmt.Sprintf("✅ 已%s技能 '%s' 的 [%s] 章节，已自动重新加载。", action, skillName, heading), nil
}

// RepairSkill programmatically inspects a skill's SKILL.md for common issues and
// auto-fixes them.  It checks for:
//
//  1. Missing YAML frontmatter → adds a minimal default
//  2. Empty description → adds a placeholder
//  3. Missing triggers section → adds name-based trigger words
//  4. Invalid markdown headers (e.g. # with no space) → normalises
//
// After fixes are applied the file is written back and the skill manager is reloaded.
// Returns a human-readable report of what was fixed, or an error.
func RepairSkill(skillName string) (string, error) {
	if globalSkillManager == nil {
		return "", fmt.Errorf("skill manager not initialized")
	}

	skill, ok := globalSkillManager.GetSkill(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}

	filePath := skill.FilePath

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read skill file: %w", err)
	}

	content := string(data)
	var fixes []string

	// ---- Fix 1: Normalise markdown headers (ensure "## " has a trailing space) ----
	headerRe := regexp.MustCompile(`^(#{1,3})([^ #\s])`)
	if headerRe.MatchString(content) {
		content = headerRe.ReplaceAllString(content, "$1 $2")
		fixes = append(fixes, "• 修复了 Markdown 标题格式（补全空格）")
	}

	// ---- Fix 2: Missing YAML frontmatter ----
	if !strings.HasPrefix(content, "---") {
		defaultFM := fmt.Sprintf("---\nname: %s\n---\n\n", skill.Name)
		content = defaultFM + content
		fixes = append(fixes, fmt.Sprintf("• 添加了缺失的 YAML frontmatter（name: %s）", skill.Name))
	}

	// ---- Fix 3: Empty or missing description ----
	if skill.Description == "" {
		// Find or create the 描述 section
		if !strings.Contains(content, "## 描述") {
			// Insert after the first H1 heading
			idx := strings.Index(content, "\n")
			insert := fmt.Sprintf("\n\n## 描述\n%s的技能描述待补充。\n", skill.DisplayName)
			if idx == -1 {
				content = content + insert
			} else {
				content = content[:idx] + insert + content[idx:]
			}
			fixes = append(fixes, "• 添加了缺失的描述章节")
		} else {
			// Section exists but is empty — fill it
			replaced := false
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) == "## 描述" {
					// Find the next section or end
					end := len(lines)
					for j := i + 1; j < len(lines); j++ {
						if strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
							end = j
							break
						}
					}
					sectionContent := strings.TrimSpace(strings.Join(lines[i+1:end], "\n"))
					if sectionContent == "" || isPlaceholderContent(sectionContent) {
						placeholder := fmt.Sprintf("%s的技能描述待补充。", skill.DisplayName)
						newLines := make([]string, 0, len(lines)+1)
						newLines = append(newLines, lines[:i+1]...)
						newLines = append(newLines, placeholder)
						newLines = append(newLines, lines[end:]...)
						content = strings.Join(newLines, "\n")
						replaced = true
					}
					break
				}
			}
			if replaced {
				fixes = append(fixes, "• 填充了空的描述章节")
			}
		}
	}

	// ---- Fix 4: Missing triggers ----
	if len(skill.TriggerWords) == 0 {
		if !strings.Contains(content, "## 触发关键词") && !strings.Contains(content, "## 触发词") {
			triggerLine := fmt.Sprintf("\n## 触发关键词\n- %s\n", skill.Name)
			content = strings.TrimRight(content, "\n") + triggerLine + "\n"
			fixes = append(fixes, fmt.Sprintf("• 添加了基于名称的触发关键词: %s", skill.Name))
		} else {
			// Section exists but empty — add name-based triggers
			heading := "## 触发关键词"
			if idx := strings.Index(content, "## 触发词"); idx != -1 {
				heading = "## 触发词"
			}
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				if strings.TrimSpace(line) == heading {
					end := len(lines)
					for j := i + 1; j < len(lines); j++ {
						if strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
							end = j
							break
						}
					}
					sectionContent := strings.TrimSpace(strings.Join(lines[i+1:end], "\n"))
					if sectionContent == "" {
						triggerLine := fmt.Sprintf("- %s\n", skill.Name)
						newLines := make([]string, 0, len(lines)+1)
						newLines = append(newLines, lines[:i+1]...)
						newLines = append(newLines, triggerLine)
						newLines = append(newLines, lines[end:]...)
						content = strings.Join(newLines, "\n")
						fixes = append(fixes, fmt.Sprintf("• 填充了空的触发关键词: %s", skill.Name))
					}
					break
				}
			}
		}
	}

	// ---- Fix 5: Remove consecutive blank lines (>2) ----
	blankRe := regexp.MustCompile(`\n{3,}`)
	if blankRe.MatchString(content) {
		content = blankRe.ReplaceAllString(content, "\n\n")
		fixes = append(fixes, "• 清理了多余的连续空行")
	}

	// Write back
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write repaired file: %w", err)
	}

	// Reload
	if err := globalSkillManager.Reload(); err != nil {
		return "", fmt.Errorf("file written but reload failed: %w", err)
	}

	if len(fixes) == 0 {
		return fmt.Sprintf("✅ 技能 '%s' 未发现问题，无需修复。", skillName), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔧 技能 '%s' 已自动修复 (%d 项):\n\n", skillName, len(fixes)))
	for _, fix := range fixes {
		sb.WriteString(fix)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\n✅ 已自动重新加载。"))

	return sb.String(), nil
}

// isPlaceholderContent returns true if the content looks like a template placeholder.
func isPlaceholderContent(s string) bool {
	lower := strings.ToLower(s)
	placeholders := []string{
		"在这里填写", "待补充", "placeholder", "todo", "tbd",
		"在这里", "请填写", "填写技能的描述",
	}
	for _, p := range placeholders {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Help text
// ---------------------------------------------------------------------------

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
  /skill patch <名称> --section <节> --content <内容> 修补技能的特定章节
  /skill update <名称> --file <路径> [--append]  从文件更新技能内容
  /skill repair <名称>  自动修复技能的常见问题

📁 技能文件存储在 skills/ 目录，采用层次化结构:

# 技能目录结构:
  skills/
  ├── coding/                    # 技能分类
  │   ├── code_review/           # 技能目录
  │   │   ├── SKILL.md           # 主技能文件
  │   │   ├── references/        # 参考资料
  │   │   ├── templates/         # 模板文件
  │   │   └── scripts/           # 辅助脚本

# SKILL.md 文件格式:
  ---  # YAML frontmatter (推荐)
  name: code_review              # 技能标识符
  description: 专业的代码审查技能  # 技能描述
  tags:                          # 技能标签
    - coding
    - review
  platforms:                     # 支持的平台
    - windows
    - linux
    - macos
  ---  

  # 技能显示名称                 # 技能的友好名称
  
  ## 描述                        # 详细描述
  技能的详细说明...
  
  ## 触发关键词                  # 自动触发的词汇
  - 关键词1
  - 关键词2
  
  ## 系统提示                    # 激活时注入的提示词
  系统提示内容...
  
  ## 输出格式                    # 输出格式要求
  输出格式说明...
  
  ## 示例                        # 示例对话
  - 示例1
  - 示例2
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

// ---------------------------------------------------------------------------
// Internal helpers for file-level skill content manipulation
// ---------------------------------------------------------------------------

// readSkillFileLines reads a SKILL.md file and returns its lines, handling
// both frontmatter and body content.
func readSkillFileLines(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// findSectionBounds locates the start (first content line) and end indices of
// a given ## heading within a file's lines.  Returns (-1, -1) if not found.
func findSectionBounds(lines []string, heading string) (contentStart int, sectionEnd int) {
	target := "## " + heading
	contentStart = -1
	sectionEnd = -1

	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			contentStart = i + 1
			// Find the next ## heading or EOF
			sectionEnd = len(lines)
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
					sectionEnd = j
					break
				}
			}
			break
		}
	}
	return contentStart, sectionEnd
}

// writeLines writes a slice of lines to a file joined by newlines with a
// trailing newline.
func writeLines(filePath string, lines []string) error {
	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
