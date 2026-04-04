package main

import (
        "bufio"
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "strings"
        "sync"
        "time"
)

// Skill 技能定义
type Skill struct {
        Name          string    `json:"Name"`
        DisplayName   string    `json:"DisplayName"`
        Description   string    `json:"Description"`
        TriggerWords  []string  `json:"TriggerWords,omitempty"`
        SystemPrompt  string    `json:"SystemPrompt"`
        OutputFormat  string    `json:"OutputFormat,omitempty"`
        Examples      []string  `json:"Examples,omitempty"`
        Tags          []string  `json:"Tags,omitempty"`
        FilePath      string    `json:"-"` // 源文件路径
        LastModified  time.Time `json:"-"`
}

// SkillManager 技能管理器
type SkillManager struct {
        mu          sync.RWMutex
        skills      map[string]*Skill
        skillsDir   string
}

// NewSkillManager 创建技能管理器
func NewSkillManager(skillsDir string) (*SkillManager, error) {
        sm := &SkillManager{
                skills:    make(map[string]*Skill),
                skillsDir: skillsDir,
        }

        // 确保目录存在
        if err := os.MkdirAll(skillsDir, 0755); err != nil {
                return nil, fmt.Errorf("failed to create skills directory: %w", err)
        }

        // 加载技能
        if err := sm.loadFromDirectory(); err != nil {
                return nil, fmt.Errorf("failed to load skills: %w", err)
        }

        return sm, nil
}

// loadFromDirectory 从目录加载所有技能
func (sm *SkillManager) loadFromDirectory() error {
        return filepath.Walk(sm.skillsDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return nil // 忽略错误，继续处理其他文件
                }

                if info.IsDir() {
                        return nil
                }

                // 只处理 .md 文件
                if filepath.Ext(path) != ".md" {
                        return nil
                }

                skill, err := parseSkillFile(path)
                if err != nil {
                        fmt.Printf("Warning: failed to parse skill file %s: %v\n", path, err)
                        return nil
                }

                if skill != nil {
                        sm.skills[skill.Name] = skill
                }

                return nil
        })
}

// parseSkillFile 解析技能文件
func parseSkillFile(path string) (*Skill, error) {
        data, err := os.ReadFile(path)
        if err != nil {
                return nil, err
        }

        content := string(data)
        skill := &Skill{
                FilePath:     path,
                LastModified: time.Now(),
        }

        // 解析 Markdown 格式的技能文件
        scanner := bufio.NewScanner(strings.NewReader(content))
        var currentSection string
        var sectionContent strings.Builder

        for scanner.Scan() {
                line := scanner.Text()

                // 检测标题
                if strings.HasPrefix(line, "# ") {
                        // 主标题 -> 技能名称
                        skill.DisplayName = strings.TrimPrefix(line, "# ")
                        skill.Name = sanitizeName(skill.DisplayName)
                } else if strings.HasPrefix(line, "## ") {
                        // 保存上一个 section
                        if currentSection != "" {
                                setSectionContent(skill, currentSection, sectionContent.String())
                        }
                        currentSection = strings.TrimPrefix(line, "## ")
                        sectionContent.Reset()
                } else {
                        // 添加到当前 section
                        if currentSection != "" {
                                sectionContent.WriteString(line)
                                sectionContent.WriteString("\n")
                        }
                }
        }

        // 保存最后一个 section
        if currentSection != "" {
                setSectionContent(skill, currentSection, sectionContent.String())
        }

        // 如果没有名称，使用文件名
        if skill.Name == "" {
                skill.Name = strings.TrimSuffix(filepath.Base(path), ".md")
                skill.DisplayName = skill.Name
        }

        // 验证必要字段
        if skill.SystemPrompt == "" && skill.Description == "" {
                return nil, fmt.Errorf("skill %s has no description or system_prompt", skill.Name)
        }

        return skill, nil
}

// setSectionContent 设置技能的 section 内容
func setSectionContent(skill *Skill, section, content string) {
        content = strings.TrimSpace(content)

        switch section {
        case "描述", "Description":
                skill.Description = content
        case "触发关键词", "触发词", "Trigger Words", "Keywords":
                skill.TriggerWords = parseList(content)
        case "系统提示", "系统提示注入", "System Prompt", "Prompt":
                skill.SystemPrompt = content
        case "输出格式", "Output Format":
                skill.OutputFormat = content
        case "示例对话", "示例", "Examples":
                skill.Examples = parseList(content)
        case "标签", "Tags":
                skill.Tags = parseList(content)
        }
}

// parseList 解析列表内容
func parseList(content string) []string {
        var items []string
        scanner := bufio.NewScanner(strings.NewReader(content))
        for scanner.Scan() {
                line := strings.TrimSpace(scanner.Text())
                if line == "" {
                        continue
                }
                // 移除列表标记
                if strings.HasPrefix(line, "- ") {
                        line = strings.TrimPrefix(line, "- ")
                } else if strings.HasPrefix(line, "* ") {
                        line = strings.TrimPrefix(line, "* ")
                } else if regexp.MustCompile(`^\d+\.\s`).MatchString(line) {
                        line = regexp.MustCompile(`^\d+\.\s`).ReplaceAllString(line, "")
                }
                line = strings.TrimSpace(line)
                if line != "" {
                        items = append(items, line)
                }
        }
        return items
}

// sanitizeName 将名称转换为有效的标识符
func sanitizeName(name string) string {
        // 转小写
        name = strings.ToLower(name)
        // 空格和特殊字符转为下划线（保留中文 \x{4e00}-\x{9fa5}）
        name = regexp.MustCompile(`[^a-z0-9\x{4e00}-\x{9fa5}]+`).ReplaceAllString(name, "_")
        name = strings.Trim(name, "_")
        return name
}

// BuildSkillIndexPrompt 构建技能索引提示（用于系统提示，让模型知道有哪些技能可用）
func (sm *SkillManager) BuildSkillIndexPrompt() string {
        sm.mu.RLock()
        defer sm.mu.RUnlock()

        if len(sm.skills) == 0 {
                return ""
        }

        var sb strings.Builder
        sb.WriteString("# 可用技能\n\n")
        sb.WriteString("你拥有以下专业技能，可以根据用户需求推荐或激活：\n\n")

        for _, skill := range sm.skills {
                sb.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", 
                        skill.DisplayName, skill.Name, 
                        truncateString(skill.Description, 60)))
        }

        sb.WriteString("\n**使用方式**：\n")
        sb.WriteString("- 当用户的问题适合某个技能时，可以建议用户激活\n")
        sb.WriteString("- 用户可以通过 `/skill <技能名>` 命令激活技能\n")
        sb.WriteString("- 激活后，相关技能的专业提示会注入到对话中\n")

        return sb.String()
}

// GetSkill 获取技能
func (sm *SkillManager) GetSkill(name string) (*Skill, bool) {
        sm.mu.RLock()
        defer sm.mu.RUnlock()
        skill, ok := sm.skills[name]
        return skill, ok
}

// ListSkills 列出所有技能
func (sm *SkillManager) ListSkills() []*Skill {
        sm.mu.RLock()
        defer sm.mu.RUnlock()

        skills := make([]*Skill, 0, len(sm.skills))
        for _, skill := range sm.skills {
                skills = append(skills, skill)
        }
        return skills
}

// Count 返回技能数量
func (sm *SkillManager) Count() int {
        sm.mu.RLock()
        defer sm.mu.RUnlock()
        return len(sm.skills)
}

// MatchTrigger 匹配触发词，返回匹配的技能
func (sm *SkillManager) MatchTrigger(input string) *Skill {
        sm.mu.RLock()
        defer sm.mu.RUnlock()

        inputLower := strings.ToLower(input)

        for _, skill := range sm.skills {
                for _, trigger := range skill.TriggerWords {
                        if strings.Contains(inputLower, strings.ToLower(trigger)) {
                                return skill
                        }
                }
        }
        return nil
}

// BuildSkillPrompt 构建技能的系统提示
func (s *Skill) BuildSkillPrompt() string {
        var sb strings.Builder

        sb.WriteString(fmt.Sprintf("## 技能：%s\n\n", s.DisplayName))

        if s.Description != "" {
                sb.WriteString(fmt.Sprintf("**描述**：%s\n\n", s.Description))
        }

        if s.SystemPrompt != "" {
                sb.WriteString(s.SystemPrompt)
                sb.WriteString("\n\n")
        }

        if s.OutputFormat != "" {
                sb.WriteString("**输出格式**：\n")
                sb.WriteString(s.OutputFormat)
                sb.WriteString("\n\n")
        }

        return sb.String()
}

// Reload 重新加载技能
func (sm *SkillManager) Reload() error {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        sm.skills = make(map[string]*Skill)
        return sm.loadFromDirectory()
}

// GetSkillsByTag 按标签获取技能
func (sm *SkillManager) GetSkillsByTag(tag string) []*Skill {
        sm.mu.RLock()
        defer sm.mu.RUnlock()

        var skills []*Skill
        for _, skill := range sm.skills {
                for _, t := range skill.Tags {
                        if strings.EqualFold(t, tag) {
                                skills = append(skills, skill)
                                break
                        }
                }
        }
        return skills
}

// CreateSkillFile 创建技能文件模板
func (sm *SkillManager) CreateSkillFile(name string) (string, error) {
        filename := sanitizeName(name) + ".md"
        path := filepath.Join(sm.skillsDir, filename)

        // 检查是否已存在
        if _, err := os.Stat(path); err == nil {
                return "", fmt.Errorf("skill file already exists: %s", path)
        }

        template := fmt.Sprintf(`# %s

## 描述
在这里填写技能的描述...

## 触发关键词
- 触发词1
- 触发词2

## 系统提示
当用户触发此技能时，系统会注入以下提示：

请在这里填写详细的系统提示...

## 输出格式
（可选）定义输出的格式要求

## 示例
（可选）提供示例对话

## 标签
- 标签1
- 标签2
`, name)

        if err := os.WriteFile(path, []byte(template), 0644); err != nil {
                return "", err
        }

        return path, nil
}

// DeleteSkill 删除技能
func (sm *SkillManager) DeleteSkill(name string) error {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        skill, ok := sm.skills[name]
        if !ok {
                return fmt.Errorf("skill not found: %s", name)
        }

        if err := os.Remove(skill.FilePath); err != nil {
                return err
        }

        delete(sm.skills, name)
        return nil
}

// parseSkillContent 解析技能内容（字符串版本，用于 SkillManagerV2）
func parseSkillContent(content string, path string) (*Skill, error) {
	skill := &Skill{
		FilePath:     path,
		LastModified: time.Now(),
	}

	// 解析 Markdown 格式的技能文件
	scanner := bufio.NewScanner(strings.NewReader(content))
	var currentSection string
	var sectionContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// 检测标题
		if strings.HasPrefix(line, "# ") {
			// 主标题 -> 技能名称
			skill.DisplayName = strings.TrimPrefix(line, "# ")
			skill.Name = sanitizeName(skill.DisplayName)
		} else if strings.HasPrefix(line, "## ") {
			// 保存上一个 section
			if currentSection != "" {
				setSectionContent(skill, currentSection, sectionContent.String())
			}
			currentSection = strings.TrimPrefix(line, "## ")
			sectionContent.Reset()
		} else {
			// 添加到当前 section
			if currentSection != "" {
				sectionContent.WriteString(line)
				sectionContent.WriteString("\n")
			}
		}
	}

	// 保存最后一个 section
	if currentSection != "" {
		setSectionContent(skill, currentSection, sectionContent.String())
	}

	// 如果没有名称，使用文件名
	if skill.Name == "" {
		skill.Name = strings.TrimSuffix(filepath.Base(path), ".md")
		skill.DisplayName = skill.Name
	}

	// 验证必要字段
	if skill.SystemPrompt == "" && skill.Description == "" {
		return nil, fmt.Errorf("skill %s has no description or system_prompt", skill.Name)
	}

	return skill, nil
}

// parseJSONArray 解析 JSON 数组字符串
func parseJSONArray(jsonStr string) []string {
	// 简单解析，移除 [ ] 和引号
	jsonStr = strings.Trim(jsonStr, "[]")
	if jsonStr == "" {
		return []string{}
	}

	parts := strings.Split(jsonStr, "\",\"")
	var result []string
	for _, part := range parts {
		part = strings.Trim(part, "\"")
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
