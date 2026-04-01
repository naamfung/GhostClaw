package main

import (
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"

        "github.com/toon-format/toon-go"
)

// ToolPermissionMode 工具权限模式
type ToolPermissionMode string

const (
        ToolPermissionAll       ToolPermissionMode = "all"       // 允许所有工具
        ToolPermissionAllowlist ToolPermissionMode = "allowlist" // 仅允许白名单
        ToolPermissionDenylist  ToolPermissionMode = "denylist"  // 禁止黑名单
)

// ToolPermission 工具权限配置
type ToolPermission struct {
        Mode         ToolPermissionMode `json:"mode"`
        AllowedTools []string           `json:"allowed_tools,omitempty"`
        DeniedTools  []string           `json:"denied_tools,omitempty"`
}

// RoleExample 示例对话
type RoleExample struct {
        User      string `json:"user"`
        Assistant string `json:"assistant"`
        Context   string `json:"context,omitempty"`
}

// Role 角色模板
type Role struct {
        // 基础信息
        Name        string `json:"Name"`        // 内部标识：novelist, coder
        DisplayName string `json:"DisplayName"` // 显示名称：小说家、程序员
        Description string `json:"Description"` // 简短描述
        Icon        string `json:"Icon"`        // 图标（可选）

        // 核心人设
        Identity      string `json:"Identity"`      // 身份定位
        Personality   string `json:"Personality"`   // 性格特质
        SpeakingStyle string `json:"SpeakingStyle"` // 说话风格

        // 专业能力
        Expertise []string `json:"Expertise,omitempty"` // 专业领域

        // 行为约束
        Guidelines []string `json:"Guidelines,omitempty"` // 应遵循的准则
        Forbidden  []string `json:"Forbidden,omitempty"`  // 禁止的行为

        // 技能绑定
        Skills []string `json:"Skills,omitempty"` // 绑定的技能名称列表

        // 工具权限配置
        ToolPermission ToolPermission `json:"ToolPermission"`

        // 示例对话
        Examples []RoleExample `json:"Examples,omitempty"`

        // 元数据
        Tags     []string `json:"Tags,omitempty"`
        IsPreset bool     `json:"IsPreset"` // 是否预置角色
        Author   string   `json:"Author,omitempty"`
}

// BuildSystemPrompt 构建该角色的系统提示
func (r *Role) BuildSystemPrompt() string {
        var sb strings.Builder

        // 身份定位
        if r.Identity != "" {
                sb.WriteString(r.Identity)
                sb.WriteString("\n\n")
        }

        // 性格特质
        if r.Personality != "" {
                sb.WriteString("## 性格特质\n")
                sb.WriteString(r.Personality)
                sb.WriteString("\n\n")
        }

        // 说话风格
        if r.SpeakingStyle != "" {
                sb.WriteString("## 说话风格\n")
                sb.WriteString(r.SpeakingStyle)
                sb.WriteString("\n\n")
        }

        // 专业领域
        if len(r.Expertise) > 0 {
                sb.WriteString("## 专业领域\n")
                for _, exp := range r.Expertise {
                        sb.WriteString("- ")
                        sb.WriteString(exp)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        // 行为准则
        if len(r.Guidelines) > 0 {
                sb.WriteString("## 行为准则\n")
                for _, g := range r.Guidelines {
                        sb.WriteString("- ")
                        sb.WriteString(g)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        // 禁止事项
        if len(r.Forbidden) > 0 {
                sb.WriteString("## 禁止事项\n")
                for _, f := range r.Forbidden {
                        sb.WriteString("- ")
                        sb.WriteString(f)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        // 示例对话
        if len(r.Examples) > 0 {
                sb.WriteString("## 示例对话\n\n")
                for i, ex := range r.Examples {
                        sb.WriteString(fmt.Sprintf("### 示例 %d\n", i+1))
                        sb.WriteString("用户：")
                        sb.WriteString(ex.User)
                        sb.WriteString("\n\n助手：")
                        sb.WriteString(ex.Assistant)
                        sb.WriteString("\n\n")
                }
        }

        return sb.String()
}

// BuildSystemPromptWithSkills 构建包含技能的系统提示
func (r *Role) BuildSystemPromptWithSkills(sm *SkillManager) string {
        basePrompt := r.BuildSystemPrompt()

        // 如果没有绑定技能，直接返回基础提示
        if len(r.Skills) == 0 || sm == nil {
                return basePrompt
        }

        var sb strings.Builder
        sb.WriteString(basePrompt)

        // 添加技能提示
        sb.WriteString("## 已激活技能\n\n")
        for _, skillName := range r.Skills {
                skill, ok := sm.GetSkill(skillName)
                if !ok {
                        continue
                }
                sb.WriteString(skill.BuildSkillPrompt())
                sb.WriteString("\n---\n\n")
        }

        return sb.String()
}

// IsToolAllowed 检查工具是否被允许
func (r *Role) IsToolAllowed(toolName string) bool {
        switch r.ToolPermission.Mode {
        case ToolPermissionAll:
                return true
        case ToolPermissionAllowlist:
                for _, allowed := range r.ToolPermission.AllowedTools {
                        if allowed == toolName {
                                return true
                        }
                }
                return false
        case ToolPermissionDenylist:
                for _, denied := range r.ToolPermission.DeniedTools {
                        if denied == toolName {
                                return false
                        }
                }
                return true
        default:
                return true
        }
}

// RoleManager 角色模板管理器
type RoleManager struct {
        mu       sync.RWMutex
        roles    map[string]*Role
        filePath string // 单文件配置路径（兼容旧模式）
        rolesDir string // 角色目录路径
}

// NewRoleManager 创建角色模板管理器（兼容模式）
func NewRoleManager(filePath string) (*RoleManager, error) {
        // 从文件路径推断目录路径
        dir := filepath.Dir(filePath)
        rolesDir := filepath.Join(dir, "roles")

        return NewRoleManagerWithDir(filePath, rolesDir)
}

// NewRoleManagerWithDir 创建角色模板管理器（指定目录）
func NewRoleManagerWithDir(filePath string, rolesDir string) (*RoleManager, error) {
        rm := &RoleManager{
                roles:    make(map[string]*Role),
                filePath: filePath,
                rolesDir: rolesDir,
        }

        // 1. 加载预置角色（硬编码，可被文件覆盖）
        presets := GetPresetRoles()
        for _, r := range presets {
                rm.roles[r.Name] = r
        }

        // 2. 从目录加载角色文件（优先级高于预置角色）
        if _, err := os.Stat(rolesDir); err == nil {
                if err := rm.loadFromDirectory(); err != nil {
                        log.Printf("Warning: failed to load roles from directory: %v", err)
                }
        }

        // 3. 尝试从单文件加载自定义角色（兼容旧模式）
        if _, err := os.Stat(filePath); err == nil {
                if err := rm.loadFromFile(); err != nil {
                        log.Printf("Warning: failed to load roles from file: %v", err)
                }
        }

        log.Printf("Role manager: %d roles loaded (%d from directory)", rm.Count(), rm.countFromDirectory())
        return rm, nil
}

// loadFromFile 从文件加载角色（兼容旧模式）
func (rm *RoleManager) loadFromFile() error {
        data, err := os.ReadFile(rm.filePath)
        if err != nil {
                return err
        }

        var fileData struct {
                Roles map[string]*Role `json:"roles"`
        }

        if err := toon.Unmarshal(data, &fileData); err != nil {
                return err
        }

        for name, p := range fileData.Roles {
                // 预置角色不能被文件覆盖
                if existing, exists := rm.roles[name]; exists && existing.IsPreset {
                        continue
                }
                // 设置 name 字段（文件中的 key 即为角色名）
                p.Name = name
                rm.roles[name] = p
        }

        return nil
}

// loadFromDirectory 从目录加载角色文件
// 扫描 rolesDir 下的所有 .toon 文件，文件名即为角色标识符
// 支持子目录扫描（如 custom/ 子目录）
func (rm *RoleManager) loadFromDirectory() error {
        return filepath.Walk(rm.rolesDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }

                // 跳过目录
                if info.IsDir() {
                        return nil
                }

                // 支持 .md 和 .toon 两种格式
                ext := strings.ToLower(filepath.Ext(path))
                if ext != ".md" && ext != ".toon" {
                        return nil
                }

                // 获取相对路径，用于确定角色名
                relPath, err := filepath.Rel(rm.rolesDir, path)
                if err != nil {
                        return err
                }

                // 角色名 = 相对路径去掉扩展名，路径分隔符替换为 /
                // 例如: custom/my_role.md -> custom/my_role
                roleName := strings.TrimSuffix(relPath, filepath.Ext(relPath))
                roleName = strings.ReplaceAll(roleName, string(filepath.Separator), "/")

                // 读取并解析文件
                data, err := os.ReadFile(path)
                if err != nil {
                        log.Printf("Warning: failed to read role file %s: %v", path, err)
                        return nil // 继续处理其他文件
                }

                var role Role
                if ext == ".md" {
                        // 解析 Markdown 格式
                        if err := parseMarkdownRole(data, &role); err != nil {
                                log.Printf("Warning: failed to parse role file %s: %v", path, err)
                                return nil // 继续处理其他文件
                        }
                } else {
                        // 解析 TOON/JSON 格式
                        if err := toon.Unmarshal(data, &role); err != nil {
                                log.Printf("Warning: failed to parse role file %s: %v", path, err)
                                return nil // 继续处理其他文件
                        }
                }

                // 设置角色名（文件名优先，允许文件内指定 name 字段作为覆盖）
                if role.Name == "" {
                        role.Name = roleName
                }

                // 文件加载的角色默认为非预置角色
                // 但如果文件内明确标记为 is_preset: true，则视为预置角色
                // 这允许用户创建自己的"稳定角色"

                // 添加到管理器
                // 注意：目录文件可以覆盖预置角色（用于更新预置角色的配置）
                rm.roles[role.Name] = &role

                return nil
        })
}

// parseMarkdownRole 解析 Markdown 格式的角色文件
func parseMarkdownRole(data []byte, role *Role) error {
        content := string(data)
        lines := strings.Split(content, "\n")

        var currentSection string
        var sectionContent strings.Builder

        finishSection := func() {
                text := strings.TrimSpace(sectionContent.String())
                if text == "" {
                        return
                }

                switch currentSection {
                case "身份", "identity":
                        role.Identity = text
                case "性格", "性格特质", "personality":
                        role.Personality = text
                case "说话风格", "speaking_style", "说话方式":
                        role.SpeakingStyle = text
                case "专业领域", "expertise":
                        role.Expertise = parseMarkdownList(text)
                case "行为准则", "准则", "guidelines":
                        role.Guidelines = parseMarkdownList(text)
                case "禁止事项", "forbidden":
                        role.Forbidden = parseMarkdownList(text)
                case "示例对话", "示例", "examples":
                        role.Examples = parseMarkdownExamples(text)
                case "标签", "tags":
                        role.Tags = parseMarkdownList(text)
                case "工具权限", "tool_permission":
                        parseToolPermission(text, role)
                case "绑定技能", "技能", "skills":
                        role.Skills = parseMarkdownList(text)
                }
                sectionContent.Reset()
        }

        for _, line := range lines {
                trimmed := strings.TrimSpace(line)

                // 解析标题作为显示名称
                if strings.HasPrefix(trimmed, "# ") && role.DisplayName == "" {
                        role.DisplayName = strings.TrimPrefix(trimmed, "# ")
                        continue
                }

                // 解析二级标题作为节
                if strings.HasPrefix(trimmed, "## ") {
                        finishSection()
                        currentSection = strings.TrimPrefix(trimmed, "## ")
                        continue
                }

                // 解析描述（标题后的第一段非空内容）
                if role.DisplayName != "" && role.Description == "" && trimmed != "" && !strings.HasPrefix(trimmed, "#") && currentSection == "" {
                        // 检查是否是"基本信息"节之前的内容
                        if !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") {
                                role.Description = trimmed
                        }
                        continue
                }

                // 解析基本信息中的字段
                if currentSection == "基本信息" || currentSection == "" {
                        if strings.HasPrefix(trimmed, "- **") || strings.HasPrefix(trimmed, "* **") {
                                // 解析 key: value 格式
                                idx := strings.Index(trimmed, "**:")
                                if idx > 0 {
                                        key := strings.TrimPrefix(trimmed[:idx], "- **")
                                        key = strings.TrimPrefix(key, "* **")
                                        value := strings.TrimSpace(trimmed[idx+3:])

                                        switch key {
                                        case "图标", "icon":
                                                role.Icon = strings.Trim(value, "\"")
                                        case "预设", "is_preset":
                                                role.IsPreset = value == "true" || value == "是"
                                        case "描述", "description":
                                                role.Description = strings.Trim(value, "\"")
                                        }
                                }
                                continue
                        }
                }

                // 累积节内容
                if currentSection != "" && currentSection != "基本信息" {
                        if sectionContent.Len() > 0 {
                                sectionContent.WriteString("\n")
                        }
                        sectionContent.WriteString(trimmed)
                }
        }

        // 处理最后一个节
        finishSection()

        return nil
}

// parseMarkdownList 解析 Markdown 列表
func parseMarkdownList(text string) []string {
        var items []string
        lines := strings.Split(text, "\n")
        for _, line := range lines {
                trimmed := strings.TrimSpace(line)
                if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
                        item := strings.TrimPrefix(trimmed, "- ")
                        item = strings.TrimPrefix(item, "* ")
                        item = strings.TrimSpace(item)
                        if item != "" {
                                items = append(items, item)
                        }
                }
        }
        return items
}

// parseMarkdownExamples 解析 Markdown 示例对话
func parseMarkdownExamples(text string) []RoleExample {
        var examples []RoleExample
        var currentExample *RoleExample
        var currentUser, currentAssistant strings.Builder

        lines := strings.Split(text, "\n")
        for _, line := range lines {
                trimmed := strings.TrimSpace(line)

                if strings.HasPrefix(trimmed, "**用户**") || strings.HasPrefix(trimmed, "**User**") {
                        // 保存之前的示例
                        if currentExample != nil && currentUser.Len() > 0 {
                                currentExample.User = strings.TrimSpace(currentUser.String())
                                currentExample.Assistant = strings.TrimSpace(currentAssistant.String())
                                examples = append(examples, *currentExample)
                        }
                        currentExample = &RoleExample{}
                        currentUser.Reset()
                        currentAssistant.Reset()

                        // 提取用户内容
                        content := trimmed
                        content = strings.TrimPrefix(content, "**用户**")
                        content = strings.TrimPrefix(content, "**User**")
                        content = strings.TrimPrefix(content, ":")
                        content = strings.TrimPrefix(content, "：")
                        currentUser.WriteString(strings.TrimSpace(content))
                } else if strings.HasPrefix(trimmed, "**助手**") || strings.HasPrefix(trimmed, "**Assistant**") {
                        // 提取助手内容
                        content := trimmed
                        content = strings.TrimPrefix(content, "**助手**")
                        content = strings.TrimPrefix(content, "**Assistant**")
                        content = strings.TrimPrefix(content, ":")
                        content = strings.TrimPrefix(content, "：")
                        currentAssistant.WriteString(strings.TrimSpace(content))
                } else if currentExample != nil {
                        // 继续累积内容
                        if currentAssistant.Len() > 0 {
                                if currentAssistant.String() != "" {
                                        currentAssistant.WriteString("\n")
                                }
                                currentAssistant.WriteString(trimmed)
                        } else if currentUser.Len() > 0 {
                                if currentUser.String() != "" {
                                        currentUser.WriteString("\n")
                                }
                                currentUser.WriteString(trimmed)
                        }
                }
        }

        // 保存最后一个示例
        if currentExample != nil && currentUser.Len() > 0 {
                currentExample.User = strings.TrimSpace(currentUser.String())
                currentExample.Assistant = strings.TrimSpace(currentAssistant.String())
                examples = append(examples, *currentExample)
        }

        return examples
}

// parseToolPermission 解析工具权限
func parseToolPermission(text string, role *Role) {
        lines := strings.Split(text, "\n")
        for _, line := range lines {
                trimmed := strings.TrimSpace(line)
                if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
                        item := strings.TrimPrefix(trimmed, "- ")
                        item = strings.TrimPrefix(item, "* ")
                        item = strings.TrimSpace(item)

                        // 检查是否是模式设置
                        if strings.HasPrefix(item, "模式:") || strings.HasPrefix(item, "mode:") {
                                mode := strings.TrimPrefix(item, "模式:")
                                mode = strings.TrimPrefix(mode, "mode:")
                                mode = strings.TrimSpace(mode)
                                role.ToolPermission.Mode = ToolPermissionMode(mode)
                        } else {
                                // 否则是工具列表
                                if role.ToolPermission.Mode == ToolPermissionAllowlist {
                                        role.ToolPermission.AllowedTools = append(role.ToolPermission.AllowedTools, item)
                                } else if role.ToolPermission.Mode == ToolPermissionDenylist {
                                        role.ToolPermission.DeniedTools = append(role.ToolPermission.DeniedTools, item)
                                }
                        }
                } else if strings.HasPrefix(trimmed, "模式:") || strings.HasPrefix(trimmed, "mode:") {
                        mode := strings.TrimPrefix(trimmed, "模式:")
                        mode = strings.TrimPrefix(mode, "mode:")
                        role.ToolPermission.Mode = ToolPermissionMode(strings.TrimSpace(mode))
                }
        }
}

// SaveToFile 保存到文件（仅保存非预置角色）
func (rm *RoleManager) SaveToFile() error {
        rm.mu.RLock()
        defer rm.mu.RUnlock()

        customRoles := make(map[string]*Role)
        for name, p := range rm.roles {
                if !p.IsPreset {
                        customRoles[name] = p
                }
        }

        fileData := struct {
                Roles map[string]*Role `json:"roles"`
        }{
                Roles: customRoles,
        }

        data, err := toon.Marshal(fileData)
        if err != nil {
                return err
        }

        return os.WriteFile(rm.filePath, data, 0644)
}

// GetRole 获取角色模板
func (rm *RoleManager) GetRole(name string) (*Role, bool) {
        rm.mu.RLock()
        defer rm.mu.RUnlock()
        p, ok := rm.roles[name]
        return p, ok
}

// ListRoles 列出所有角色模板
func (rm *RoleManager) ListRoles() []*Role {
        rm.mu.RLock()
        defer rm.mu.RUnlock()

        result := make([]*Role, 0, len(rm.roles))
        for _, p := range rm.roles {
                result = append(result, p)
        }
        return result
}

// ListPresetRoles 列出预置角色
func (rm *RoleManager) ListPresetRoles() []*Role {
        rm.mu.RLock()
        defer rm.mu.RUnlock()

        result := make([]*Role, 0)
        for _, p := range rm.roles {
                if p.IsPreset {
                        result = append(result, p)
                }
        }
        return result
}

// ListCustomRoles 列出自定义角色
func (rm *RoleManager) ListCustomRoles() []*Role {
        rm.mu.RLock()
        defer rm.mu.RUnlock()

        result := make([]*Role, 0)
        for _, p := range rm.roles {
                if !p.IsPreset {
                        result = append(result, p)
                }
        }
        return result
}

// AddRole 添加角色模板
func (rm *RoleManager) AddRole(p *Role) error {
        rm.mu.Lock()
        defer rm.mu.Unlock()

        // 检查是否与预置角色冲突
        if existing, exists := rm.roles[p.Name]; exists && existing.IsPreset {
                return fmt.Errorf("cannot override preset role: %s", p.Name)
        }

        rm.roles[p.Name] = p
        return nil
}

// RemoveRole 移除角色模板（仅限自定义角色）
func (rm *RoleManager) RemoveRole(name string) error {
        rm.mu.Lock()
        defer rm.mu.Unlock()

        p, exists := rm.roles[name]
        if !exists {
                return fmt.Errorf("role not found: %s", name)
        }

        if p.IsPreset {
                return fmt.Errorf("cannot remove preset role: %s", name)
        }

        delete(rm.roles, name)
        return nil
}

// Count 获取角色数量
func (rm *RoleManager) Count() int {
        rm.mu.RLock()
        defer rm.mu.RUnlock()
        return len(rm.roles)
}

// countFromDirectory 统计从目录加载的角色数量（用于日志）
func (rm *RoleManager) countFromDirectory() int {
        rm.mu.RLock()
        defer rm.mu.RUnlock()

        count := 0
        for name := range rm.roles {
                // 检查是否来自目录（名称包含 / 或对应文件存在）
                if strings.Contains(name, "/") {
                        count++
                        continue
                }
                // 检查文件是否存在
                if rm.rolesDir != "" {
                        filePath := filepath.Join(rm.rolesDir, name+".toon")
                        if _, err := os.Stat(filePath); err == nil {
                                count++
                        }
                }
        }
        return count
}

// GetRolesDir 获取角色目录路径
func (rm *RoleManager) GetRolesDir() string {
        return rm.rolesDir
}
