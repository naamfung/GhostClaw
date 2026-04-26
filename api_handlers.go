package main

import (
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "sync"
)

// defaultRoleMu protects concurrent access to the defaultRole global variable
var defaultRoleMu sync.RWMutex

// getDefaultRole returns the current default role (thread-safe)
func getDefaultRole() string {
        defaultRoleMu.RLock()
        defer defaultRoleMu.RUnlock()
        return defaultRole
}

// setDefaultRole updates the default role (thread-safe)
func setDefaultRole(role string) {
        defaultRoleMu.Lock()
        defer defaultRoleMu.Unlock()
        defaultRole = role
}

// setCommonHeaders 设置通用的 HTTP 响应头
func setCommonHeaders(w http.ResponseWriter) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// handleCORS 处理 CORS 预检请求
func handleCORS(w http.ResponseWriter, r *http.Request) bool {
        if r.Method == "OPTIONS" {
                setCommonHeaders(w)
                w.WriteHeader(http.StatusOK)
                return true
        }
        return false
}

// configHandler 处理配置 API 请求
// GET /api/config - 获取当前配置
// PUT /api/config - 更新配置
func (s *HTTPServer) configHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        switch r.Method {
        case http.MethodGet:
                s.getConfig(w, r)
        case http.MethodPut:
                s.updateConfig(w, r)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// getConfig 返回当前配置（动态从主模型计算）
func (s *HTTPServer) getConfig(w http.ResponseWriter, _ *http.Request) {
        // 通过 ConfigManager 获取动态 APIConfig
        apiCfg := globalConfigManager.GetAPIConfig()
        needsSetup := apiCfg.APIKey == "" && apiCfg.APIType != "ollama"

        mainModelName := globalConfigManager.GetMainModelName()
        mainModelDesc := globalConfigManager.GetMainModelDescription()

        // 构建配置响应
        configData := map[string]interface{}{
                "APIConfig": map[string]interface{}{
                        "Name":                   mainModelName,
                        "Description":            mainModelDesc,
                        "APIType":                apiCfg.APIType,
                        "BaseURL":                apiCfg.BaseURL,
                        "APIKey":                 maskAPIKey(apiCfg.APIKey),
                        "Model":                  apiCfg.Model,
                        "Temperature":            apiCfg.Temperature,
                        "MaxTokens":              apiCfg.MaxTokens,
                        "Stream":                 apiCfg.Stream,
                        "Thinking":               apiCfg.Thinking,
                        "BlockDangerousCommands": apiCfg.BlockDangerousCommands,
                },
                "DefaultRole":     getDefaultRole(),
                "NeedsSetup":      needsSetup,
                "PlanModeEnabled": globalToolsConfig.PlanModeEnabled,
                "Timeout": map[string]interface{}{
                        "Shell":   globalTimeoutConfig.Shell,
                        "HTTP":    globalTimeoutConfig.HTTP,
                        "Plugin":  globalTimeoutConfig.Plugin,
                        "Browser": globalTimeoutConfig.Browser,
                },
        }

        json.NewEncoder(w).Encode(configData)
}

// updateConfig 更新配置
func (s *HTTPServer) updateConfig(w http.ResponseWriter, r *http.Request) {
        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 解析请求
        var newConfig struct {
                APIConfig       APIConfig     `json:"APIConfig"`
                DefaultRole     string        `json:"DefaultRole"`
                Timeout         TimeoutConfig `json:"Timeout"`
                PlanModeEnabled *bool         `json:"PlanModeEnabled"` // 使用指針區分「未提供」和「提供了 false」
        }
        if err := json.Unmarshal(body, &newConfig); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 解析原始 JSON 检测哪些字段存在
        var rawMap map[string]interface{}
        json.Unmarshal(body, &rawMap)

        // 获取当前配置用于差异合并
        currentConfig := globalConfigManager.GetConfig()

        // 差异合并：只更新请求中明确指定的字段
        if _, ok := rawMap["APIConfig"]; ok {
                // 通过 ConfigManager 更新 API 配置（自动同步主模型）
                // 注意：如果 API Key 为空，UpdateAPIConfig 会保留原有密钥
                if err := globalConfigManager.UpdateAPIConfig(newConfig.APIConfig); err != nil {
                        log.Printf("Warning: failed to update API config: %v", err)
                }
        }

        // 更新默认角色
        if _, exists := rawMap["DefaultRole"]; exists {
                if err := globalConfigManager.UpdateDefaultRole(newConfig.DefaultRole); err != nil {
                        log.Printf("Warning: failed to update default role: %v", err)
                }
                // 同时更新默认演员的人格
                if globalActorManager != nil {
                        if actor := globalActorManager.GetDefaultActor(); actor != nil {
                                if newConfig.DefaultRole != "" {
                                        actor.Role = newConfig.DefaultRole
                                } else {
                                        actor.Role = "coder"
                                }
                        }
                }
                if globalStage != nil {
                        globalStage.SetUpdateSystemPrompt()
                }
        }

        // 更新超时配置
        if timeoutRaw, ok := rawMap["Timeout"]; ok {
                if timeoutMap, ok := timeoutRaw.(map[string]interface{}); ok {
                        mergedTimeout := currentConfig.Timeout // 从当前配置开始合并

                        if _, exists := timeoutMap["Shell"]; exists && newConfig.Timeout.Shell > 0 {
                                mergedTimeout.Shell = newConfig.Timeout.Shell
                        }
                        if _, exists := timeoutMap["HTTP"]; exists && newConfig.Timeout.HTTP > 0 {
                                mergedTimeout.HTTP = newConfig.Timeout.HTTP
                        }
                        if _, exists := timeoutMap["Plugin"]; exists && newConfig.Timeout.Plugin > 0 {
                                mergedTimeout.Plugin = newConfig.Timeout.Plugin
                        }
                        if _, exists := timeoutMap["Browser"]; exists && newConfig.Timeout.Browser > 0 {
                                mergedTimeout.Browser = newConfig.Timeout.Browser
                        }

                        if err := globalConfigManager.UpdateTimeout(mergedTimeout); err != nil {
                                log.Printf("Warning: failed to update timeout config: %v", err)
                        }
                }
        }

        // 更新規劃模式開關
        if _, exists := rawMap["PlanModeEnabled"]; exists && newConfig.PlanModeEnabled != nil {
                if err := globalConfigManager.UpdatePlanModeEnabled(*newConfig.PlanModeEnabled); err != nil {
                        log.Printf("Warning: failed to update plan mode config: %v", err)
                }
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "配置已更新"})
}
func maskAPIKey(key string) string {
        if len(key) <= 8 {
                return "****"
        }
        return key[:4] + "****" + key[len(key)-4:]
}

// newSessionHandler 处理创建新会话请求
// POST /api/session/new - 创建新会话（等同于 /new 命令）
func (s *HTTPServer) newSessionHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        if r.Method != http.MethodPost {
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
                return
        }

        result := HandleNewCommand()
        json.NewEncoder(w).Encode(map[string]interface{}{
                "message": result,
        })
}

// rolesHandler 处理人格列表 API 请求
// GET /api/roles - 列出所有人格
// POST /api/roles - 创建新人格
func (s *HTTPServer) rolesHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        switch r.Method {
        case http.MethodGet:
                s.listRoles(w, r)
        case http.MethodPost:
                s.createRole(w, r)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// listRoles 列出所有人格
func (s *HTTPServer) listRoles(w http.ResponseWriter, _ *http.Request) {
        if globalRoleManager == nil {
                http.Error(w, `{"error": "人格管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        roles := globalRoleManager.ListRoles()

        // 转换为简化格式
        result := make([]map[string]interface{}, 0, len(roles))
        for _, p := range roles {
                result = append(result, map[string]interface{}{
                        "Name":        p.Name,
                        "DisplayName": p.DisplayName,
                        "Description": p.Description,
                        "Icon":        p.Icon,
                        "IsPreset":    p.IsPreset,
                        "Tags":        p.Tags,
                })
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
                "Roles": result,
        })
}

// createRole 创建新人格
func (s *HTTPServer) createRole(w http.ResponseWriter, r *http.Request) {
        if globalRoleManager == nil {
                http.Error(w, `{"error": "人格管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var role Role
        if err := json.Unmarshal(body, &role); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 验证必要字段
        if role.Name == "" {
                http.Error(w, `{"error": "人格名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 检查是否已存在
        if existing, _ := globalRoleManager.GetRole(role.Name); existing != nil {
                http.Error(w, `{"error": "人格名称已存在"}`, http.StatusBadRequest)
                return
        }

        // 保存到文件
        if err := saveRoleToFile(&role); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "保存人格失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 添加到管理器
        globalRoleManager.AddRole(&role)

        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(map[string]string{
                "message": "人格创建成功",
                "name":    role.Name,
        })
}

// roleDetailHandler 处理单个人格的 API 请求
// GET /api/roles/:name - 获取人格详情
// PUT /api/roles/:name - 更新人格
// DELETE /api/roles/:name - 删除人格
func (s *HTTPServer) roleDetailHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        // 从 URL 中提取人格名称
        // URL 格式: /api/roles/:name
        name := strings.TrimPrefix(r.URL.Path, "/api/roles/")
        if name == "" {
                http.Error(w, `{"error": "人格名称不能为空"}`, http.StatusBadRequest)
                return
        }

        switch r.Method {
        case http.MethodGet:
                s.getRole(w, r, name)
        case http.MethodPut:
                s.updateRole(w, r, name)
        case http.MethodDelete:
                s.deleteRole(w, r, name)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// getRole 获取人格详情
func (s *HTTPServer) getRole(w http.ResponseWriter, _ *http.Request, name string) {
        if globalRoleManager == nil {
                http.Error(w, `{"error": "人格管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        role, ok := globalRoleManager.GetRole(name)
        if !ok {
                http.Error(w, `{"error": "人格不存在"}`, http.StatusNotFound)
                return
        }

        json.NewEncoder(w).Encode(role)
}

// updateRole 更新人格
func (s *HTTPServer) updateRole(w http.ResponseWriter, r *http.Request, name string) {
        if globalRoleManager == nil {
                http.Error(w, `{"error": "人格管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查人格是否存在
        existing, ok := globalRoleManager.GetRole(name)
        if !ok {
                http.Error(w, `{"error": "人格不存在"}`, http.StatusNotFound)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var role Role
        if err := json.Unmarshal(body, &role); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 保持原名称
        role.Name = name
        role.IsPreset = existing.IsPreset

        // 保存到文件
        if err := saveRoleToFile(&role); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "保存人格失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 更新管理器
        globalRoleManager.AddRole(&role)

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "人格更新成功"})
}

// deleteRole 删除人格
func (s *HTTPServer) deleteRole(w http.ResponseWriter, _ *http.Request, name string) {
        if globalRoleManager == nil {
                http.Error(w, `{"error": "人格管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查人格是否存在
        role, ok := globalRoleManager.GetRole(name)
        if !ok {
                http.Error(w, `{"error": "人格不存在"}`, http.StatusNotFound)
                return
        }

        // 检查是否是预置人格
        if role.IsPreset {
                http.Error(w, `{"error": "无法删除预置人格"}`, http.StatusBadRequest)
                return
        }

        // 删除文件
        if err := deleteRoleFile(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "删除人格文件失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 从管理器中移除
        globalRoleManager.RemoveRole(name)

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "人格删除成功"})
}

// saveRoleToFile 保存人格到文件
func saveRoleToFile(role *Role) error {
        execPath, err := os.Executable()
        if err != nil {
                return err
        }
        execDir := filepath.Dir(execPath)
        rolesDir := filepath.Join(execDir, "roles", "custom")

        // 确保目录存在
        if err := os.MkdirAll(rolesDir, 0755); err != nil {
                return err
        }

        // 生成 Markdown 内容
        var sb strings.Builder
        sb.WriteString("# ")
        sb.WriteString(role.DisplayName)
        sb.WriteString("\n\n")
        sb.WriteString(role.Description)
        sb.WriteString("\n\n")

        sb.WriteString("## 基本信息\n\n")
        if role.Icon != "" {
                sb.WriteString("- **图标**: ")
                sb.WriteString(role.Icon)
                sb.WriteString("\n")
        }
        sb.WriteString("- **预设**: ")
        if role.IsPreset {
                sb.WriteString("true")
        } else {
                sb.WriteString("false")
        }
        sb.WriteString("\n\n")

        if role.Identity != "" {
                sb.WriteString("## 身份\n\n")
                sb.WriteString(role.Identity)
                sb.WriteString("\n\n")
        }

        if role.Personality != "" {
                sb.WriteString("## 性格特质\n\n")
                sb.WriteString(role.Personality)
                sb.WriteString("\n\n")
        }

        if role.SpeakingStyle != "" {
                sb.WriteString("## 说话风格\n\n")
                sb.WriteString(role.SpeakingStyle)
                sb.WriteString("\n\n")
        }

        if len(role.Expertise) > 0 {
                sb.WriteString("## 专业领域\n\n")
                for _, exp := range role.Expertise {
                        sb.WriteString("- ")
                        sb.WriteString(exp)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if len(role.Guidelines) > 0 {
                sb.WriteString("## 行为准则\n\n")
                for _, g := range role.Guidelines {
                        sb.WriteString("- ")
                        sb.WriteString(g)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if len(role.Forbidden) > 0 {
                sb.WriteString("## 禁止事项\n\n")
                for _, f := range role.Forbidden {
                        sb.WriteString("- ")
                        sb.WriteString(f)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if len(role.Skills) > 0 {
                sb.WriteString("## 绑定技能\n\n")
                for _, s := range role.Skills {
                        sb.WriteString("- ")
                        sb.WriteString(s)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if len(role.Tags) > 0 {
                sb.WriteString("## 标签\n\n")
                for _, t := range role.Tags {
                        sb.WriteString("- ")
                        sb.WriteString(t)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        filePath := filepath.Join(rolesDir, role.Name+".md")
        return os.WriteFile(filePath, []byte(sb.String()), 0644)
}

// deleteRoleFile 删除人格文件
func deleteRoleFile(name string) error {
        execPath, err := os.Executable()
        if err != nil {
                return err
        }
        execDir := filepath.Dir(execPath)

        // 先检查 custom 目录
        customPath := filepath.Join(execDir, "roles", "custom", name+".md")
        if _, err := os.Stat(customPath); err == nil {
                return os.Remove(customPath)
        }

        // 再检查 roles 根目录
        rootPath := filepath.Join(execDir, "roles", name+".md")
        if _, err := os.Stat(rootPath); err == nil {
                return os.Remove(rootPath)
        }

        return fmt.Errorf("role file not found")
}

// skillsHandler 处理技能列表 API 请求
// GET /api/skills - 列出所有技能
// POST /api/skills - 创建新技能
func (s *HTTPServer) skillsHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        switch r.Method {
        case http.MethodGet:
                s.listSkills(w, r)
        case http.MethodPost:
                s.createSkill(w, r)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// listSkills 列出所有技能
func (s *HTTPServer) listSkills(w http.ResponseWriter, _ *http.Request) {
        if globalSkillManager == nil {
                http.Error(w, `{"error": "技能管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        skills := globalSkillManager.ListSkills()

        // 转换为简化格式
        result := make([]map[string]interface{}, 0, len(skills))
        for _, skill := range skills {
                result = append(result, map[string]interface{}{
                        "Name":         skill.Name,
                        "DisplayName":  skill.DisplayName,
                        "Description":  skill.Description,
                        "TriggerWords": skill.TriggerWords,
                        "Tags":         skill.Tags,
                })
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
                "Skills": result,
        })
}

// createSkill 创建新技能
func (s *HTTPServer) createSkill(w http.ResponseWriter, r *http.Request) {
        if globalSkillManager == nil {
                http.Error(w, `{"error": "技能管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var skill Skill
        if err := json.Unmarshal(body, &skill); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 验证必要字段
        if skill.Name == "" {
                http.Error(w, `{"error": "技能名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 检查是否已存在
        if existing, _ := globalSkillManager.GetSkill(skill.Name); existing != nil {
                http.Error(w, `{"error": "技能名称已存在"}`, http.StatusBadRequest)
                return
        }

        // 保存到文件
        if err := saveSkillToFile(&skill); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "保存技能失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 重新加载技能管理器
        globalSkillManager.Reload()

        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(map[string]string{
                "message": "技能创建成功",
                "name":    skill.Name,
        })
}

// skillDetailHandler 处理单个技能的 API 请求
// GET /api/skills/:name - 获取技能详情
// PUT /api/skills/:name - 更新技能
// DELETE /api/skills/:name - 删除技能
func (s *HTTPServer) skillDetailHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        // 从 URL 中提取技能名称
        name := strings.TrimPrefix(r.URL.Path, "/api/skills/")
        if name == "" {
                http.Error(w, `{"error": "技能名称不能为空"}`, http.StatusBadRequest)
                return
        }

        switch r.Method {
        case http.MethodGet:
                s.getSkill(w, r, name)
        case http.MethodPut:
                s.updateSkill(w, r, name)
        case http.MethodDelete:
                s.deleteSkill(w, r, name)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// getSkill 获取技能详情
func (s *HTTPServer) getSkill(w http.ResponseWriter, _ *http.Request, name string) {
        if globalSkillManager == nil {
                http.Error(w, `{"error": "技能管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        skill, ok := globalSkillManager.GetSkill(name)
        if !ok {
                http.Error(w, `{"error": "技能不存在"}`, http.StatusNotFound)
                return
        }

        json.NewEncoder(w).Encode(skill)
}

// updateSkill 更新技能
func (s *HTTPServer) updateSkill(w http.ResponseWriter, r *http.Request, name string) {
        if globalSkillManager == nil {
                http.Error(w, `{"error": "技能管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查技能是否存在
        _, ok := globalSkillManager.GetSkill(name)
        if !ok {
                http.Error(w, `{"error": "技能不存在"}`, http.StatusNotFound)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var skill Skill
        if err := json.Unmarshal(body, &skill); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 保持原名称
        skill.Name = name

        // 保存到文件
        if err := saveSkillToFile(&skill); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "保存技能失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 重新加载技能管理器
        globalSkillManager.Reload()

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "技能更新成功"})
}

// deleteSkill 删除技能
func (s *HTTPServer) deleteSkill(w http.ResponseWriter, _ *http.Request, name string) {
        if globalSkillManager == nil {
                http.Error(w, `{"error": "技能管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查技能是否存在
        _, ok := globalSkillManager.GetSkill(name)
        if !ok {
                http.Error(w, `{"error": "技能不存在"}`, http.StatusNotFound)
                return
        }

        // 删除技能
        if err := globalSkillManager.DeleteSkill(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "删除技能失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "技能删除成功"})
}

// saveSkillToFile 保存技能到文件
func saveSkillToFile(skill *Skill) error {
        execPath, err := os.Executable()
        if err != nil {
                return err
        }
        execDir := filepath.Dir(execPath)
        skillsDir := filepath.Join(execDir, "skills")

        // 确保目录存在
        if err := os.MkdirAll(skillsDir, 0755); err != nil {
                return err
        }

        // 生成 Markdown 内容
        var sb strings.Builder
        sb.WriteString("# ")
        sb.WriteString(skill.DisplayName)
        sb.WriteString("\n\n")

        sb.WriteString("## 描述\n\n")
        sb.WriteString(skill.Description)
        sb.WriteString("\n\n")

        if len(skill.TriggerWords) > 0 {
                sb.WriteString("## 触发关键词\n\n")
                for _, tw := range skill.TriggerWords {
                        sb.WriteString("- ")
                        sb.WriteString(tw)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if skill.SystemPrompt != "" {
                sb.WriteString("## 系统提示\n\n")
                sb.WriteString(skill.SystemPrompt)
                sb.WriteString("\n\n")
        }

        if skill.OutputFormat != "" {
                sb.WriteString("## 输出格式\n\n")
                sb.WriteString(skill.OutputFormat)
                sb.WriteString("\n\n")
        }

        if len(skill.Examples) > 0 {
                sb.WriteString("## 示例\n\n")
                for _, ex := range skill.Examples {
                        sb.WriteString("- ")
                        sb.WriteString(ex)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        if len(skill.Tags) > 0 {
                sb.WriteString("## 标签\n\n")
                for _, t := range skill.Tags {
                        sb.WriteString("- ")
                        sb.WriteString(t)
                        sb.WriteString("\n")
                }
                sb.WriteString("\n")
        }

        // 确保技能目录存在（新格式：skills/{skill.Name}/SKILL.md）
        skillDir := filepath.Join(skillsDir, skill.Name)
        if err := os.MkdirAll(skillDir, 0755); err != nil {
                return err
        }

        // 使用新格式的文件路径：skills/{skill.Name}/SKILL.md
        filePath := filepath.Join(skillDir, "SKILL.md")
        return os.WriteFile(filePath, []byte(sb.String()), 0644)
}

// ==================== 演员管理 API ====================

// actorsHandler 处理演员列表 API 请求
// GET /api/actors - 列出所有演员
// POST /api/actors - 创建新演员
func (s *HTTPServer) actorsHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        switch r.Method {
        case http.MethodGet:
                s.listActors(w, r)
        case http.MethodPost:
                s.createActor(w, r)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// listActors 列出所有演员
func (s *HTTPServer) listActors(w http.ResponseWriter, _ *http.Request) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        actors := globalActorManager.ListActors()

        // 转换为简化格式
        result := make([]map[string]interface{}, 0, len(actors))
        for _, a := range actors {
                result = append(result, map[string]interface{}{
                        "Name":                a.Name,
                        "Role":                a.Role,
                        "Model":               a.Model,
                        "CharacterName":       a.CharacterName,
                        "CharacterBackground": a.CharacterBackground,
                        "Description":         a.Description,
                        "IsDefault":           a.IsDefault,
                })
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
                "Actors": result,
        })
}

// createActor 创建新演员
func (s *HTTPServer) createActor(w http.ResponseWriter, r *http.Request) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var actor Actor
        if err := json.Unmarshal(body, &actor); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 验证必要字段
        if actor.Name == "" {
                http.Error(w, `{"error": "演员名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 设置默认值
        if actor.Role == "" {
                actor.Role = "coder"
        }
        if actor.Model == "" && globalConfigManager != nil {
                // 使用当前主模型名称作为默认值
                mainModelName := globalConfigManager.GetMainModelName()
                if mainModelName != "" {
                        actor.Model = mainModelName
                }
        }

        // 添加演员
        if err := globalActorManager.AddActor(&actor); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "创建演员失败: %s"}`, err.Error()), http.StatusBadRequest)
                return
        }

        // 保存到文件
        if err := globalActorManager.SaveToFile(); err != nil {
                log.Printf("Warning: failed to save actors: %v", err)
        }

        w.WriteHeader(http.StatusCreated)
        json.NewEncoder(w).Encode(map[string]string{
                "message": "演员创建成功",
                "name":    actor.Name,
        })
}

// actorDetailHandler 处理单个演员的 API 请求
// GET /api/actors/:name - 获取演员详情
// PUT /api/actors/:name - 更新演员
// DELETE /api/actors/:name - 删除演员
func (s *HTTPServer) actorDetailHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        // 从 URL 中提取演员名称
        name := strings.TrimPrefix(r.URL.Path, "/api/actors/")
        if name == "" || strings.Contains(name, "/") {
                http.Error(w, `{"error": "演员名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 检查是否是设置默认演员的请求
        if strings.HasSuffix(name, "/set-default") {
                actorName := strings.TrimSuffix(name, "/set-default")
                s.setDefaultActor(w, r, actorName)
                return
        }

        switch r.Method {
        case http.MethodGet:
                s.getActor(w, r, name)
        case http.MethodPut:
                s.updateActor(w, r, name)
        case http.MethodDelete:
                s.deleteActor(w, r, name)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// getActor 获取演员详情
func (s *HTTPServer) getActor(w http.ResponseWriter, _ *http.Request, name string) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        actor, ok := globalActorManager.GetActor(name)
        if !ok {
                http.Error(w, `{"error": "演员不存在"}`, http.StatusNotFound)
                return
        }

        json.NewEncoder(w).Encode(actor)
}

// updateActor 更新演员
func (s *HTTPServer) updateActor(w http.ResponseWriter, r *http.Request, name string) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查演员是否存在
        _, ok := globalActorManager.GetActor(name)
        if !ok {
                http.Error(w, `{"error": "演员不存在"}`, http.StatusNotFound)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var actor Actor
        if err := json.Unmarshal(body, &actor); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 保持原名称
        actor.Name = name

        // 更新演员（原地更新，支持默认演员）
        if err := globalActorManager.UpdateActor(&actor); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "更新演员失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 保存到文件
        if err := globalActorManager.SaveToFile(); err != nil {
                log.Printf("Warning: failed to save actors: %v", err)
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "演员更新成功"})
}

// deleteActor 删除演员
func (s *HTTPServer) deleteActor(w http.ResponseWriter, _ *http.Request, name string) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查演员是否存在
        actor, ok := globalActorManager.GetActor(name)
        if !ok {
                http.Error(w, `{"error": "演员不存在"}`, http.StatusNotFound)
                return
        }

        // 检查是否是默认演员
        if actor.IsDefault {
                http.Error(w, `{"error": "无法删除默认演员"}`, http.StatusBadRequest)
                return
        }

        // 删除演员
        if err := globalActorManager.RemoveActor(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "删除演员失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 保存到文件
        if err := globalActorManager.SaveToFile(); err != nil {
                log.Printf("Warning: failed to save actors: %v", err)
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "演员删除成功"})
}

// setDefaultActor 设置默认演员
func (s *HTTPServer) setDefaultActor(w http.ResponseWriter, r *http.Request, name string) {
        if globalActorManager == nil {
                http.Error(w, `{"error": "演员管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        if r.Method != http.MethodPost {
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
                return
        }

        // 检查演员是否存在
        actor, ok := globalActorManager.GetActor(name)
        if !ok {
                http.Error(w, `{"error": "演员不存在"}`, http.StatusNotFound)
                return
        }

        // 设置为默认演员
        if err := globalActorManager.SetDefaultActor(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "设置默认演员失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 同时更新默认人格
        setDefaultRole(actor.Role)
        if err := globalConfigManager.UpdateDefaultRole(getDefaultRole()); err != nil {
                log.Printf("Warning: failed to save config: %v", err)
        }

        // 保存演员配置
        if err := globalActorManager.SaveToFile(); err != nil {
                log.Printf("Warning: failed to save actors: %v", err)
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "已设置为默认演员"})
}

// ==================== 模型管理 API ====================

// modelsAPIHandler 处理模型列表 API 请求
// GET /api/models - 列出所有模型
// POST /api/models - 创建新模型
func (s *HTTPServer) modelsAPIHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        switch r.Method {
        case http.MethodGet:
                s.listModelsAPI(w, r)
        case http.MethodPost:
                s.createModelAPI(w, r)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// modelDetailHandler 处理单个模型 API 请求
// GET /api/models/:name - 获取模型详情
// PUT /api/models/:name - 更新模型
// DELETE /api/models/:name - 删除模型
// PATCH /api/models/:name/set-main - 设置为主模型
func (s *HTTPServer) modelDetailHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        // 提取模型名称
        path := strings.TrimPrefix(r.URL.Path, "/api/models/")
        if path == "" {
                http.Error(w, `{"error": "模型名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 检查是否是设置主模型的请求
        if strings.HasSuffix(path, "/set-main") {
                if r.Method == http.MethodPatch {
                        modelName := strings.TrimSuffix(path, "/set-main")
                        s.setMainModelAPI(w, r, modelName)
                        return
                }
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
                return
        }

        switch r.Method {
        case http.MethodGet:
                s.getModelAPI(w, r, path)
        case http.MethodPut:
                s.updateModelAPI(w, r, path)
        case http.MethodDelete:
                s.deleteModelAPI(w, r, path)
        default:
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
        }
}

// listModelsAPI 列出所有模型
func (s *HTTPServer) listModelsAPI(w http.ResponseWriter, _ *http.Request) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        mainModelName := globalConfigManager.GetMainModelName()
        models := globalConfigManager.ListModels()

        // 转换为前端期望的格式，不返回 API 密钥
        result := make([]map[string]interface{}, len(models))
        for i, m := range models {
                result[i] = map[string]interface{}{
                        "Name":                   m.Name,
                        "APIType":                m.APIType,
                        "BaseURL":                m.BaseURL,
                        "APIKey":                 "", // 不返回密钥
                        "Model":                  m.Model,
                        "Temperature":            m.Temperature,
                        "MaxTokens":              m.MaxTokens,
                        "RateLimit":              m.RateLimit,
                        "Stream":                 m.Stream,
                        "Thinking":               m.Thinking,
                        "BlockDangerousCommands": m.BlockDangerousCommands,
                        "Description":            m.Description,
                        "IsDefault":              m.Name == mainModelName,
                }
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
                "Models":    result,
                "MainModel": mainModelName,
        })
}

// createModelAPI 创建新模型
func (s *HTTPServer) createModelAPI(w http.ResponseWriter, r *http.Request) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var model ModelConfig
        if err := json.Unmarshal(body, &model); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        if model.Name == "" {
                http.Error(w, `{"error": "模型名称不能为空"}`, http.StatusBadRequest)
                return
        }

        if _, exists := globalConfigManager.GetModel(model.Name); exists {
                http.Error(w, `{"error": "模型名称已存在"}`, http.StatusBadRequest)
                return
        }

        // 设置默认值
        if model.Temperature == 0 {
                model.Temperature = 0.7
        }
        if model.MaxTokens == 0 {
                model.MaxTokens = 4096
        }

        if err := globalConfigManager.AddModel(&model); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "添加模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        w.WriteHeader(http.StatusCreated)
        // 不返回 API 密钥
        model.APIKey = ""
        json.NewEncoder(w).Encode(model)
}

// getModelAPI 获取单个模型详情
func (s *HTTPServer) getModelAPI(w http.ResponseWriter, _ *http.Request, name string) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        model, exists := globalConfigManager.GetModel(name)
        if !exists {
                http.Error(w, `{"error": "模型不存在"}`, http.StatusNotFound)
                return
        }

        // 不返回 API 密钥
        result := map[string]interface{}{
                "Name":                   model.Name,
                "APIType":                model.APIType,
                "BaseURL":                model.BaseURL,
                "APIKey":                 "",
                "Model":                  model.Model,
                "Temperature":            model.Temperature,
                "MaxTokens":              model.MaxTokens,
                "RateLimit":              model.RateLimit,
                "Stream":                 model.Stream,
                "Thinking":               model.Thinking,
                "BlockDangerousCommands": model.BlockDangerousCommands,
                "Description":            model.Description,
        }
        json.NewEncoder(w).Encode(result)
}

// updateModelAPI 更新模型
func (s *HTTPServer) updateModelAPI(w http.ResponseWriter, r *http.Request, name string) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, `{"error": "读取请求体失败"}`, http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var updates ModelConfig
        if err := json.Unmarshal(body, &updates); err != nil {
                http.Error(w, `{"error": "解析 JSON 失败"}`, http.StatusBadRequest)
                return
        }

        // 如果用户修改了模型名称，需要特殊处理
        if updates.Name != name && updates.Name != "" {
                // 检查新名称是否已经存在
                if _, exists := globalConfigManager.GetModel(updates.Name); exists {
                        http.Error(w, `{"error": "模型名称已存在"}`, http.StatusBadRequest)
                        return
                }

                // 获取原模型，保留 APIKey 和 IsDefault 标记
                if updates.APIKey == "" {
                        if oldModel, ok := globalConfigManager.GetModel(name); ok {
                                updates.APIKey = oldModel.APIKey
                                updates.IsDefault = oldModel.IsDefault
                        }
                }

                // 删除旧模型
                if err := globalConfigManager.ForceDeleteModel(name); err != nil {
                        http.Error(w, fmt.Sprintf(`{"error": "删除旧模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                        return
                }

                // 添加新模型
                if err := globalConfigManager.AddModel(&updates); err != nil {
                        http.Error(w, fmt.Sprintf(`{"error": "添加模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                        return
                }

                // 更新所有引用旧模型名称的演员
                if globalActorManager != nil {
                        for _, actor := range globalActorManager.ListActors() {
                                if actor.Model == name {
                                        actor.Model = updates.Name
                                }
                        }
                }
        } else {
                // 名称没有改变，直接更新
                updates.Name = name
                // 保留原有的 API Key（如果新配置没有提供）
                if updates.APIKey == "" {
                        if oldModel, ok := globalConfigManager.GetModel(name); ok {
                                updates.APIKey = oldModel.APIKey
                        }
                }

                if err := globalConfigManager.UpdateModel(&updates); err != nil {
                        http.Error(w, fmt.Sprintf(`{"error": "更新模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                        return
                }
        }

        // 保存演员配置（模型名称引用可能已更新）
        if globalActorManager != nil {
                if err := globalActorManager.SaveToFile(); err != nil {
                        log.Printf("Warning: failed to save actors: %v", err)
                }
        }

        updates.APIKey = "" // 不返回密钥
        json.NewEncoder(w).Encode(updates)
}

// deleteModelAPI 删除模型
func (s *HTTPServer) deleteModelAPI(w http.ResponseWriter, _ *http.Request, name string) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查模型是否存在
        if _, exists := globalConfigManager.GetModel(name); !exists {
                http.Error(w, `{"error": "模型不存在"}`, http.StatusNotFound)
                return
        }

        // 删除模型（ConfigManager 会检查是否为主模型或正在使用）
        if err := globalConfigManager.DeleteModel(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "删除模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "模型删除成功"})
}

// setMainModelAPI 设置主模型
func (s *HTTPServer) setMainModelAPI(w http.ResponseWriter, _ *http.Request, name string) {
        if globalConfigManager == nil {
                http.Error(w, `{"error": "配置管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        // 检查模型是否存在
        model, exists := globalConfigManager.GetModel(name)
        if !exists {
                http.Error(w, `{"error": "模型不存在"}`, http.StatusNotFound)
                return
        }

        // 获取旧主模型名称，用于同步演员引用
        oldMainModel := globalConfigManager.GetMainModelName()

        // 设置主模型（会自动同步全局变量）
        if err := globalConfigManager.SetMainModel(name); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "设置主模型失败: %s"}`, err.Error()), http.StatusInternalServerError)
                return
        }

        // 同步演员的模型引用：将引用旧主模型的演员更新为新主模型
        if globalActorManager != nil {
                if oldMainModel != "" && oldMainModel != name {
                        globalActorManager.UpdateActorsModelRef(oldMainModel, name)
                }
                globalActorManager.SetMainModelName(name)
                globalActorManager.SaveToFile()
        }

        log.Printf("[API] Main model set to: %s (BaseURL: %s)", model.Model, model.BaseURL)

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"message": "主模型设置成功"})
}

// ==================== Hooks 管理 API ====================

// hooksHandler 处理 Hook 列表 API 请求
// GET /api/hooks - 列出所有 Hooks
func (s *HTTPServer) hooksHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        if r.Method != http.MethodGet {
                http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
                return
        }

        hookManager := GetHookManager()
        if hookManager == nil {
                http.Error(w, `{"error": "Hook 管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        hooks := hookManager.List()
        json.NewEncoder(w).Encode(map[string]interface{}{
                "hooks": hooks,
        })
}

// hookDetailHandler 处理单个 Hook 的 API 请求
// GET /api/hooks/:name - 获取 Hook 详情
// POST /api/hooks/:name/enable - 启用 Hook
// POST /api/hooks/:name/disable - 禁用 Hook
// POST /api/hooks/reload - 重新加载 Hooks
func (s *HTTPServer) hookDetailHandler(w http.ResponseWriter, r *http.Request) {
        if handleCORS(w, r) {
                return
        }
        setCommonHeaders(w)

        // 提取 Hook 名称
        path := strings.TrimPrefix(r.URL.Path, "/api/hooks/")
        if path == "" {
                http.Error(w, `{"error": "Hook 名称不能为空"}`, http.StatusBadRequest)
                return
        }

        // 检查是否是 reload 请求
        if path == "reload" {
                if r.Method != http.MethodPost {
                        http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
                        return
                }
                s.reloadHooks(w, r)
                return
        }

        // 检查是否是 enable/disable 请求
        if strings.HasSuffix(path, "/enable") {
                name := strings.TrimSuffix(path, "/enable")
                s.setHookEnabled(w, r, name, true)
                return
        }
        if strings.HasSuffix(path, "/disable") {
                name := strings.TrimSuffix(path, "/disable")
                s.setHookEnabled(w, r, name, false)
                return
        }

        // GET 请求：获取 Hook 详情
        if r.Method == http.MethodGet {
                s.getHook(w, r, path)
                return
        }

        http.Error(w, `{"error": "方法不允许"}`, http.StatusMethodNotAllowed)
}

// getHook 获取 Hook 详情
func (s *HTTPServer) getHook(w http.ResponseWriter, _ *http.Request, name string) {
        hookManager := GetHookManager()
        if hookManager == nil {
                http.Error(w, `{"error": "Hook 管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        info := hookManager.Info(name)
        if info == nil {
                http.Error(w, `{"error": "Hook 不存在"}`, http.StatusNotFound)
                return
        }

        json.NewEncoder(w).Encode(info)
}

// setHookEnabled 设置 Hook 启用状态
func (s *HTTPServer) setHookEnabled(w http.ResponseWriter, _ *http.Request, name string, enabled bool) {
        hookManager := GetHookManager()
        if hookManager == nil {
                http.Error(w, `{"error": "Hook 管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        if err := hookManager.SetEnabled(name, enabled); err != nil {
                http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
                return
        }

        action := "启用"
        if !enabled {
                action = "禁用"
        }

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{
                "message": fmt.Sprintf("Hook '%s' 已%s", name, action),
                "name":    name,
                "enabled": fmt.Sprintf("%v", enabled),
        })
}

// reloadHooks 重新加载所有 Hooks
func (s *HTTPServer) reloadHooks(w http.ResponseWriter, _ *http.Request) {
        hookManager := GetHookManager()
        if hookManager == nil {
                http.Error(w, `{"error": "Hook 管理器未初始化"}`, http.StatusInternalServerError)
                return
        }

        hookManager.Reload()
        hooks := hookManager.List()

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]interface{}{
                "message": "Hooks 已重新加载",
                "count":   len(hooks),
        })
}
