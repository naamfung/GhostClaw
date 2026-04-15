package main

import (
        "fmt"
        "log"
        "os"
        "strings"
        "sync"

        "github.com/toon-format/toon-go"
)

// Actor 演员实例
type Actor struct {
        Name                string `json:"Name"`                // 实例名：hero_lin
        Role                string `json:"Role"`                // 角色模板：protagonist
        Model               string `json:"Model"`               // 模型名称引用：main
        CharacterName       string `json:"CharacterName"`       // 角色名：林风
        CharacterBackground string `json:"CharacterBackground"` // 角色背景
        Description         string `json:"Description,omitempty"`
        IsDefault           bool   `json:"IsDefault,omitempty"`
}

// ActorManager 演员管理器
type ActorManager struct {
        mu        sync.RWMutex
        actors    map[string]*Actor
        mainModel string // 主模型名称（仅引用，不存储模型配置数据）
        filePath  string
}

// NewActorManager 创建演员管理器
func NewActorManager(filePath string, defaultRole string) (*ActorManager, error) {
        am := &ActorManager{
                actors:    make(map[string]*Actor),
                filePath:  filePath,
                mainModel: "",
        }

        // 尝试从文件加载配置
        if _, err := os.Stat(filePath); err == nil {
                if err := am.loadFromFile(); err != nil {
                        log.Printf("Warning: failed to load actors from file: %v", err)
                }
        }

        // 确定默认人格
        roleName := "coder"
        if defaultRole != "" {
                roleName = defaultRole
        }

        // 如果没有从文件加载到演员，创建默认演员
        if _, exists := am.actors["default"]; !exists {
                am.actors["default"] = &Actor{
                        Name:          "default",
                        Role:          roleName,
                        Model:         "",
                        CharacterName: "助手",
                        Description:   "默认助手角色",
                        IsDefault:     true,
                }
        }

        return am, nil
}

// loadFromFile 从文件加载配置
func (am *ActorManager) loadFromFile() error {
        data, err := os.ReadFile(am.filePath)
        if err != nil {
                return err
        }

        var fileData struct {
                Actors    map[string]*Actor `json:"actors"`
                MainModel string            `json:"main_model"`
        }

        if err := toon.Unmarshal(data, &fileData); err != nil {
                return err
        }

        // 加载演员配置
        for name, a := range fileData.Actors {
                am.actors[name] = a
        }

        // 设置主模型名称
        if fileData.MainModel != "" {
                am.mainModel = fileData.MainModel
        }

        return nil
}

// SaveToFile 保存配置到文件
func (am *ActorManager) SaveToFile() error {
        am.mu.RLock()
        defer am.mu.RUnlock()

        customActors := make(map[string]*Actor)
        for name, a := range am.actors {
                customActors[name] = a
        }

        fileData := struct {
                Actors    map[string]*Actor `json:"actors,omitempty"`
                MainModel string            `json:"main_model,omitempty"`
        }{
                Actors:    customActors,
                MainModel: am.mainModel,
        }

        data, err := toon.Marshal(fileData)
        if err != nil {
                return err
        }

        return os.WriteFile(am.filePath, data, 0644)
}

// GetActor 获取演员
func (am *ActorManager) GetActor(name string) (*Actor, bool) {
        am.mu.RLock()
        defer am.mu.RUnlock()
        a, ok := am.actors[name]
        return a, ok
}

// GetDefaultActor 获取默认演员
func (am *ActorManager) GetDefaultActor() *Actor {
        am.mu.RLock()
        defer am.mu.RUnlock()
        for _, a := range am.actors {
                if a.IsDefault {
                        return a
                }
        }
        return am.actors["default"]
}

// ListActors 列出所有演员
func (am *ActorManager) ListActors() []*Actor {
        am.mu.RLock()
        defer am.mu.RUnlock()

        result := make([]*Actor, 0, len(am.actors))
        for _, a := range am.actors {
                result = append(result, a)
        }
        return result
}

// AddActor 添加演员
func (am *ActorManager) AddActor(a *Actor) error {
        am.mu.Lock()
        defer am.mu.Unlock()

        if _, exists := am.actors[a.Name]; exists {
                return fmt.Errorf("actor already exists: %s", a.Name)
        }

        am.actors[a.Name] = a
        return nil
}

// RemoveActor 移除演员
func (am *ActorManager) RemoveActor(name string) error {
        am.mu.Lock()
        defer am.mu.Unlock()

        a, exists := am.actors[name]
        if !exists {
                return fmt.Errorf("actor not found: %s", name)
        }

        if a.IsDefault {
                return fmt.Errorf("cannot remove default actor: %s", name)
        }

        delete(am.actors, name)
        return nil
}

// UpdateActor 更新演员（原地更新，支持默认演员）
func (am *ActorManager) UpdateActor(a *Actor) error {
        am.mu.Lock()
        defer am.mu.Unlock()

        existing, exists := am.actors[a.Name]
        if !exists {
                return fmt.Errorf("actor not found: %s", a.Name)
        }

        // 保留原有的 IsDefault 标记（前端不发送此字段）
        a.IsDefault = existing.IsDefault

        am.actors[a.Name] = a
        return nil
}

// UpdateActorsModelRef 将所有引用 oldModel 的演员更新为引用 newModel
// 用于主模型切换时同步演员的模型引用
func (am *ActorManager) UpdateActorsModelRef(oldModel, newModel string) {
        am.mu.Lock()
        defer am.mu.Unlock()

        for _, a := range am.actors {
                if a.Model == oldModel {
                        a.Model = newModel
                }
        }
}

// SetDefaultActor 设置默认演员
func (am *ActorManager) SetDefaultActor(name string) error {
        am.mu.Lock()
        defer am.mu.Unlock()

        a, exists := am.actors[name]
        if !exists {
                return fmt.Errorf("actor not found: %s", name)
        }

        // 移除其他演员的默认标记
        for _, actor := range am.actors {
                actor.IsDefault = false
        }

        a.IsDefault = true
        return nil
}

// GetMainModelName 返回主模型名称
func (am *ActorManager) GetMainModelName() string {
        am.mu.RLock()
        defer am.mu.RUnlock()
        return am.mainModel
}

// SetMainModelName 设置主模型名称（仅名称，不涉及模型配置）
func (am *ActorManager) SetMainModelName(name string) {
        am.mu.Lock()
        defer am.mu.Unlock()
        am.mainModel = name
}

// BuildActorContext 构建演员的完整上下文
func (am *ActorManager) BuildActorContext(actorName string, rm *RoleManager) string {
        am.mu.RLock()
        actor, actorExists := am.actors[actorName]
        am.mu.RUnlock()

        if !actorExists {
                return ""
        }

        // 获取角色模板
        role, ok := rm.GetRole(actor.Role)
        if !ok {
                return ""
        }

        var sb strings.Builder

        // 角色身份
        sb.WriteString("## 当前身份\n\n")

        if actor.CharacterName != "" {
                sb.WriteString(fmt.Sprintf("**角色名**：%s\n\n", actor.CharacterName))
        }

        if actor.CharacterBackground != "" {
                sb.WriteString("**角色背景**：\n")
                sb.WriteString(actor.CharacterBackground)
                sb.WriteString("\n\n")
        }

        // 角色模板的系统提示
        sb.WriteString(role.BuildSystemPrompt())

        return sb.String()
}

// getActorModelConfig 获取演员的模型配置
// 通过 ActorManager 获取演员的模型名称引用，再通过 ConfigManager 获取实际的 ModelConfig
// 如果演员没有指定模型或模型不存在，回退到主模型
func getActorModelConfig(actorName string) *ModelConfig {
        if globalActorManager == nil {
                return nil
        }
        actor, ok := globalActorManager.GetActor(actorName)
        if !ok {
                if globalConfigManager != nil {
                        return globalConfigManager.GetMainModel()
                }
                return nil
        }
        if actor.Model != "" && globalConfigManager != nil {
                if m, exists := globalConfigManager.GetModel(actor.Model); exists {
                        return m
                }
        }
        if globalConfigManager != nil {
                return globalConfigManager.GetMainModel()
        }
        return nil
}
