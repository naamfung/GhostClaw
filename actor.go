package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/toon-format/toon-go"
)

// ModelConfig 模型配置
type ModelConfig struct {
	Name        string  `json:"Name"`
	APIType     string  `json:"APIType"`
	BaseURL     string  `json:"BaseURL"`
	APIKey      string  `json:"APIKey"` // 支持环境变量 ${VAR}
	Model       string  `json:"Model"`
	Temperature float64 `json:"Temperature,omitempty"`
	MaxTokens   int     `json:"MaxTokens,omitempty"`
	Description string  `json:"Description,omitempty"`
}

// ResolveAPIKey 解析 API Key（支持环境变量）
func (m *ModelConfig) ResolveAPIKey() string {
	key := m.APIKey
	// 检查是否是环境变量引用 ${VAR}
	if strings.HasPrefix(key, "${") && strings.HasSuffix(key, "}") {
		envVar := key[2 : len(key)-1]
		return os.Getenv(envVar)
	}
	return key
}

// Actor 演员实例
type Actor struct {
	Name                string `json:"Name"`                // 实例名：hero_lin
	Role                string `json:"Role"`                // 角色模板：protagonist
	Model               string `json:"Model"`               // 模型配置：main
	CharacterName       string `json:"CharacterName"`       // 角色名：林风
	CharacterBackground string `json:"CharacterBackground"` // 角色背景
	Description         string `json:"Description,omitempty"`
	IsDefault           bool   `json:"IsDefault,omitempty"`
}

// ActorManager 演员管理器
type ActorManager struct {
	mu        sync.RWMutex
	actors    map[string]*Actor
	models    map[string]*ModelConfig
	filePath  string
	mainModel string // 主模型名称
}

// NewActorManager 创建演员管理器
func NewActorManager(filePath string, defaultAPIType, defaultBaseURL, defaultAPIKey, defaultModel string, defaultTemperature float64, defaultMaxTokens int, defaultRole string) (*ActorManager, error) {
	am := &ActorManager{
		actors:    make(map[string]*Actor),
		models:    make(map[string]*ModelConfig),
		filePath:  filePath,
		mainModel: "main",
	}

	// 创建默认主模型配置
	am.models["main"] = &ModelConfig{
		Name:        "main",
		APIType:     defaultAPIType,
		BaseURL:     defaultBaseURL,
		APIKey:      defaultAPIKey,
		Model:       defaultModel,
		Temperature: defaultTemperature,
		MaxTokens:   defaultMaxTokens,
		Description: "主模型 - 默认配置",
	}

	// 确定默认人格
	roleName := "coder"
	if defaultRole != "" {
		roleName = defaultRole
	}

	// 创建默认演员
	am.actors["default"] = &Actor{
		Name:          "default",
		Role:          roleName,
		Model:         "main",
		CharacterName: "助手",
		Description:   "默认助手角色",
		IsDefault:     true,
	}

	// 尝试从文件加载配置
	if _, err := os.Stat(filePath); err == nil {
		if err := am.loadFromFile(); err != nil {
			log.Printf("Warning: failed to load actors from file: %v", err)
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
		Models    map[string]*ModelConfig `json:"models"`
		Actors    map[string]*Actor       `json:"actors"`
		MainModel string                  `json:"main_model"`
	}

	if err := toon.Unmarshal(data, &fileData); err != nil {
		return err
	}

	// 加载模型配置
	for name, m := range fileData.Models {
		am.models[name] = m
	}

	// 加载演员配置
	for name, a := range fileData.Actors {
		am.actors[name] = a
	}

	// 设置主模型
	if fileData.MainModel != "" {
		am.mainModel = fileData.MainModel
	}

	return nil
}

// SaveToFile 保存配置到文件
func (am *ActorManager) SaveToFile() error {
	am.mu.RLock()
	defer am.mu.RUnlock()

	// 只保存非默认配置
	customModels := make(map[string]*ModelConfig)
	for name, m := range am.models {
		if name != "main" {
			customModels[name] = m
		}
	}

	customActors := make(map[string]*Actor)
	for name, a := range am.actors {
		if name != "default" {
			customActors[name] = a
		}
	}

	fileData := struct {
		Models    map[string]*ModelConfig `json:"models,omitempty"`
		Actors    map[string]*Actor       `json:"actors,omitempty"`
		MainModel string                  `json:"main_model,omitempty"`
	}{
		Models:    customModels,
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

// GetModel 获取模型配置
func (am *ActorManager) GetModel(name string) (*ModelConfig, bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	m, ok := am.models[name]
	return m, ok
}

// GetMainModel 获取主模型配置
func (am *ActorManager) GetMainModel() *ModelConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.models[am.mainModel]
}

// AddModel 添加模型配置
func (am *ActorManager) AddModel(m *ModelConfig) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.models[m.Name]; exists {
		return fmt.Errorf("model already exists: %s", m.Name)
	}

	am.models[m.Name] = m
	return nil
}

// RemoveModel 移除模型配置
func (am *ActorManager) RemoveModel(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if name == am.mainModel {
		return fmt.Errorf("cannot remove main model: %s", name)
	}

	if _, exists := am.models[name]; !exists {
		return fmt.Errorf("model not found: %s", name)
	}

	// 检查是否有演员正在使用此模型
	for _, a := range am.actors {
		if a.Model == name {
			return fmt.Errorf("model is in use by actor: %s", a.Name)
		}
	}

	delete(am.models, name)
	return nil
}

// ListModels 列出所有模型
func (am *ActorManager) ListModels() []*ModelConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make([]*ModelConfig, 0, len(am.models))
	for _, m := range am.models {
		result = append(result, m)
	}
	return result
}

// SetMainModel 设置主模型
func (am *ActorManager) SetMainModel(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.models[name]; !exists {
		return fmt.Errorf("model not found: %s", name)
	}

	am.mainModel = name
	return nil
}

// GetActorModel 获取演员使用的模型配置
func (am *ActorManager) GetActorModel(actorName string) *ModelConfig {
	am.mu.RLock()
	defer am.mu.RUnlock()

	a, ok := am.actors[actorName]
	if !ok {
		return am.models[am.mainModel]
	}

	m, ok := am.models[a.Model]
	if !ok {
		return am.models[am.mainModel]
	}

	return m
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

// UpdateMainModel 更新主模型配置（内存缓存）
func (am *ActorManager) UpdateMainModel(m *ModelConfig) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// 保留原有的 API Key（如果新配置没有提供）
	if m.APIKey == "" && am.models["main"] != nil {
		m.APIKey = am.models["main"].APIKey
	}

	am.models["main"] = m
}
