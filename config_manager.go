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

// ConfigManager 统一配置管理器
// 作为所有配置数据的唯一权威数据源（Single Source of Truth）
// 所有配置修改都必须通过 ConfigManager.Save() 写入
type ConfigManager struct {
        mu         sync.RWMutex
        config     Config // 完整配置对象（权威数据源）
        configPath string // config.toon 文件路径
        execDir    string // 程序目录
}

// NewConfigManager 从配置文件加载并返回初始化的管理器
func NewConfigManager(execDir string) (*ConfigManager, error) {
        cm := &ConfigManager{
                execDir:    execDir,
                configPath: filepath.Join(execDir, CONFIG_FILE),
        }

        config, err := cm.loadConfig()
        if err != nil {
                return nil, fmt.Errorf("failed to load config: %v", err)
        }

        cm.config = config

        // 加载完成后，同步全局变量
        cm.syncGlobals()

        return cm, nil
}

// loadConfig 加载配置文件（内部方法，不持锁）
func (cm *ConfigManager) loadConfig() (Config, error) {
        var config Config

        // 读取配置文件
        data, err := os.ReadFile(cm.configPath)
        if err != nil {
                // 配置文件不存在，生成默认配置
                defaultConfig := cm.createDefaultConfig()
                toonData, err := toon.Marshal(defaultConfig)
                if err == nil {
                        os.WriteFile(cm.configPath, toonData, 0600)
                        fmt.Printf("Generated default config file at: %s\n", cm.configPath)
                }
                return defaultConfig, nil
        }

        // 使用 toon 直接解析到结构体
        if err := toon.Unmarshal(data, &config); err != nil {
                // 尝试处理旧的 Models 数组格式（兼容旧版）
                if strings.Contains(err.Error(), "Models: toon: expected object for map, got []interface {}") {
                        var tempConfig struct {
                                Models []ModelConfig `toon:"Models"`
                        }
                        if tempErr := toon.Unmarshal(data, &tempConfig); tempErr == nil {
                                config.Models = make(map[string]*ModelConfig)
                                for _, model := range tempConfig.Models {
                                        config.Models[model.Name] = &model
                                }
                        } else {
                                return config, fmt.Errorf("error parsing TOON config: %v", err)
                        }
                } else {
                        return config, fmt.Errorf("error parsing TOON config: %v", err)
                }
        }

        // 设置默认值
        cm.applyDefaults(&config)

        // 处理环境变量覆盖
        cm.applyEnvOverrides(&config)

        // 验证模型数据完整性：如果所有模型的 ModelBase 字段均为零值，
        // 说明配置文件格式不兼容（如旧版扁平格式），使用默认配置替代
        if len(config.Models) > 0 && cm.allModelsEmpty(&config) {
                if IsDebug {
                        fmt.Printf("Warning: all models have empty fields, resetting to default config\n")
                }
                config = cm.createDefaultConfig()
                if toonData, err := toon.Marshal(config); err == nil {
                        os.WriteFile(cm.configPath, toonData, 0600)
                }
        }

        if IsDebug {
                fmt.Printf("Loaded config: %+v\n", config)
        }

        return config, nil
}

// createDefaultConfig 创建默认配置
func (cm *ConfigManager) createDefaultConfig() Config {
        config := Config{}

        // 创建默认主模型
        defaultModel := &ModelConfig{
                ModelBase: ModelBase{
                        Name:                   DEFAULT_MODEL_ID,
                        APIType:                DEFAULT_API_TYPE,
                        BaseURL:                "", // 留空，由 API 类型自动补全
                        Model:                  DEFAULT_MODEL_ID,
                        Temperature:            0.7,
                        MaxTokens:              4096,
                        Stream:                 true,
                        Thinking:               true,
                        BlockDangerousCommands: false,
                        Description:            "默认模型",
                        IsDefault:              true,
                },
        }
        config.Models = make(map[string]*ModelConfig)
        config.Models[DEFAULT_MODEL_ID] = defaultModel

        config.MaxRequestSizeBytes = 256 * 1024 // 256KB
        config.HTTPServer.Listen = "0.0.0.0:10086"
        config.DataDir = ""
        config.Security.EnableSSRFProtection = true
        config.CronConfig.MaxConcurrent = 1
        config.Timeout.Shell = DefaultShellTimeout
        config.Timeout.HTTP = DefaultHTTPTimeout
        config.Timeout.Plugin = DefaultPluginTimeout
        config.Timeout.Browser = DefaultBrowserTimeout
        config.Heartbeat.IntervalSeconds = 1800
        config.Heartbeat.KeepRecentMessages = 8
        config.Heartbeat.MaxConcurrentChecks = 3
        config.MCP.Transport = "http"
        config.MCP.SSEEndpoint = "/mcp/sse"
        config.MCP.HTTPEndpoint = "/mcp"
        config.Auth.TokenExpiry = 24
        config.Tools.SmartShell.SyncTimeout = 60
        config.Tools.SmartShell.UnknownTimeout = 120
        config.Tools.SmartShell.DefaultWakeMins = 5
        config.BrowserConfig.UserMode = true
        config.BrowserConfig.Headless = false
        config.BrowserConfig.DisableGPU = false
        config.BrowserConfig.DisableDevTools = false
        config.BrowserConfig.NoSandbox = true
        config.BrowserConfig.DisableBrowserTools = false
        config.SystemInfo.IncludeCPU = true
        config.SystemInfo.IncludeMemory = true
        config.SystemInfo.IncludeGPU = false
        config.SystemInfo.IncludeOSDetails = true
        config.Tools.PlanModeEnabled = false // 規劃模式默認關閉
        return config
}

// applyDefaults 设置配置默认值
func (cm *ConfigManager) applyDefaults(config *Config) {
        // HTTPServer 默认值
        if config.HTTPServer.Listen == "" {
                config.HTTPServer.Listen = "0.0.0.0:10086"
        }

        // DataDir: 读取时将相对路径转为绝对路径
        if config.DataDir != "" {
                if !filepath.IsAbs(config.DataDir) {
                        config.DataDir = filepath.Join(cm.execDir, config.DataDir)
                }
        } else {
                config.DataDir = cm.execDir
        }

        // Models map 初始化
        if config.Models == nil {
                config.Models = make(map[string]*ModelConfig)
        }

        // 确保至少有一个默认模型
        hasDefault := false
        for _, m := range config.Models {
                if m.IsDefault {
                        hasDefault = true
                        break
                }
        }
        if !hasDefault && len(config.Models) > 0 {
                // 将第一个模型设为默认
                for _, m := range config.Models {
                        m.IsDefault = true
                        break
                }
        }

        // MaxRequestSizeBytes 默认值
        if config.MaxRequestSizeBytes == 0 {
                config.MaxRequestSizeBytes = 256 * 1024
        }

        // CronConfig
        if config.CronConfig.MaxConcurrent == 0 {
                config.CronConfig.MaxConcurrent = 1
        }

        // Timeout 默认值
        if config.Timeout.Shell == 0 {
                config.Timeout.Shell = DefaultShellTimeout
        }
        if config.Timeout.HTTP == 0 {
                config.Timeout.HTTP = DefaultHTTPTimeout
        }
        if config.Timeout.Plugin == 0 {
                config.Timeout.Plugin = DefaultPluginTimeout
        }
        if config.Timeout.Browser == 0 {
                config.Timeout.Browser = DefaultBrowserTimeout
        }

        // Heartbeat 默认值
        if config.Heartbeat.IntervalSeconds == 0 {
                config.Heartbeat.IntervalSeconds = 1800
        }
        if config.Heartbeat.KeepRecentMessages == 0 {
                config.Heartbeat.KeepRecentMessages = 8
        }
        if config.Heartbeat.MaxConcurrentChecks == 0 {
                config.Heartbeat.MaxConcurrentChecks = 3
        }

        // MCP 默认值
        if config.MCP.Transport == "" {
                config.MCP.Transport = "http"
        }
        if config.MCP.SSEEndpoint == "" {
                config.MCP.SSEEndpoint = "/mcp/sse"
        }
        if config.MCP.HTTPEndpoint == "" {
                config.MCP.HTTPEndpoint = "/mcp"
        }

        // Auth 默认值
        if config.Auth.TokenExpiry == 0 {
                config.Auth.TokenExpiry = 24
        }

        // Tools 默认值
        if config.Tools.SmartShell.Enabled == nil {
                defaultEnabled := true
                config.Tools.SmartShell.Enabled = &defaultEnabled
        }
        if config.Tools.SmartShell.SyncTimeout == 0 {
                config.Tools.SmartShell.SyncTimeout = 60
        }
        if config.Tools.SmartShell.UnknownTimeout == 0 {
                config.Tools.SmartShell.UnknownTimeout = 120
        }
        if config.Tools.SmartShell.DefaultWakeMins == 0 {
                config.Tools.SmartShell.DefaultWakeMins = 5
        }

        // BrowserConfig 默认值
        config.BrowserConfig.UserMode = true
        config.BrowserConfig.Headless = false
        config.BrowserConfig.DisableGPU = false
        config.BrowserConfig.DisableDevTools = false
        config.BrowserConfig.NoSandbox = true
        config.BrowserConfig.DisableBrowserTools = false

        // PlanModeEnabled 默認值（bool 零值為 false，無需額外處理）

        // SystemInfo 默认值
        if !config.SystemInfo.Enabled {
                config.SystemInfo.IncludeCPU = true
                config.SystemInfo.IncludeMemory = true
                config.SystemInfo.IncludeGPU = false
                config.SystemInfo.IncludeOSDetails = true
        }

        // 如果启用了认证但没有设置密码，生成随机密码
        if config.Auth.Enabled && config.Auth.Password == "" {
                randomPassword := generateRandomPassword(12)
                config.Auth.Password = randomPassword
                fmt.Printf("\n========================================\n")
                fmt.Printf("认证已启用，自动生成密码: %s\n", randomPassword)
                fmt.Printf("请在配置文件中设置自定义密码: Auth.Password\n")
                fmt.Printf("========================================\n\n")
        }
}

// applyEnvOverrides 环境变量覆盖
func (cm *ConfigManager) applyEnvOverrides(config *Config) {
        // 环境变量覆盖将作用于主模型（稍后在 syncGlobals 之前完成）
        // 但此处暂不实现，因为需要在运行时处理，较为复杂。
        // 可以后续增强：通过环境变量设置主模型的 APIKey 等。
}

// GetConfig 返回当前配置的副本（线程安全读取）
func (cm *ConfigManager) GetConfig() Config {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.config
}

// GetModel 按名称获取模型
func (cm *ConfigManager) GetModel(name string) (*ModelConfig, bool) {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        m, ok := cm.config.Models[name]
        if !ok {
                return nil, false
        }
        // 返回副本以避免外部修改
        copy := *m
        return &copy, true
}

// GetMainModelName 获取主模型名称
// 优先使用 IsDefault: true 标记，否则回退到第一个模型
func (cm *ConfigManager) GetMainModelName() string {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        // 查找标记为 IsDefault 的模型
        for name, m := range cm.config.Models {
                if m.IsDefault {
                        return name
                }
        }

        // 回退到第一个可用模型
        for name := range cm.config.Models {
                return name
        }

        return ""
}

// GetMainModel 获取主模型的 ModelConfig
func (cm *ConfigManager) GetMainModel() *ModelConfig {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        // 查找标记为 IsDefault 的模型
        for _, m := range cm.config.Models {
                if m.IsDefault {
                        copy := *m
                        return &copy
                }
        }

        // 回退到第一个可用模型
        for _, m := range cm.config.Models {
                copy := *m
                return &copy
        }

        return nil
}

// GetMainModelDescription 获取主模型的描述
func (cm *ConfigManager) GetMainModelDescription() string {
        mainModel := cm.GetMainModel()
        if mainModel != nil {
                return mainModel.Description
        }
        return ""
}

// ListModels 列出所有模型
func (cm *ConfigManager) ListModels() []*ModelConfig {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        result := make([]*ModelConfig, 0, len(cm.config.Models))
        for _, m := range cm.config.Models {
                copy := *m
                result = append(result, &copy)
        }
        return result
}

// AddModel 添加新模型，保存
func (cm *ConfigManager) AddModel(m *ModelConfig) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        if _, exists := cm.config.Models[m.Name]; exists {
                return fmt.Errorf("model already exists: %s", m.Name)
        }

        copy := *m
        cm.config.Models[m.Name] = &copy
        return cm.saveLocked()
}

// UpdateModel 更新现有模型，保存
func (cm *ConfigManager) UpdateModel(m *ModelConfig) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        existing, exists := cm.config.Models[m.Name]
        if !exists {
                return fmt.Errorf("model not found: %s", m.Name)
        }

        // 保留原有的 APIKey（如果新配置没有提供）
        if m.APIKey == "" && existing.APIKey != "" {
                m.APIKey = existing.APIKey
        }

        copy := *m
        cm.config.Models[m.Name] = &copy

        return cm.saveLocked()
}

// DeleteModel 删除模型（检查是否正在使用），保存
func (cm *ConfigManager) DeleteModel(name string) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        if _, exists := cm.config.Models[name]; !exists {
                return fmt.Errorf("model not found: %s", name)
        }

        // 检查是否为主模型
        mainName := cm.findMainModelNameLocked()
        if mainName == name {
                return fmt.Errorf("cannot delete main model: %s", name)
        }

        // 检查是否有 actor 正在使用此模型
        if globalActorManager != nil {
                for _, a := range globalActorManager.ListActors() {
                        if a.Model == name {
                                return fmt.Errorf("model is in use by actor: %s", a.Name)
                        }
                }
        }

        delete(cm.config.Models, name)
        return cm.saveLocked()
}

// ForceDeleteModel 强制删除模型（跳过主模型和 actor 使用检查）
func (cm *ConfigManager) ForceDeleteModel(name string) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        if _, exists := cm.config.Models[name]; !exists {
                return fmt.Errorf("model not found: %s", name)
        }

        delete(cm.config.Models, name)
        return cm.saveLocked()
}

// SetMainModel 设置主模型（通过 IsDefault 标记），保存
func (cm *ConfigManager) SetMainModel(name string) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        if _, exists := cm.config.Models[name]; !exists {
                return fmt.Errorf("model not found: %s", name)
        }

        // 清除所有现有 IsDefault 标记
        for _, m := range cm.config.Models {
                m.IsDefault = false
        }

        // 设置新主模型
        cm.config.Models[name].IsDefault = true

        return cm.saveLocked()
}

// GetAPIConfig 动态从主模型生成 APIConfig（公开方法）
func (cm *ConfigManager) GetAPIConfig() APIConfig {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.getAPIConfigLocked()
}

// getAPIConfigLocked 内部实现（要求持有读锁）
func (cm *ConfigManager) getAPIConfigLocked() APIConfig {
        mainModel := cm.findMainModelLocked()
        if mainModel == nil {
                return APIConfig{}
        }
        return APIConfig{
                ModelBase:           mainModel.ModelBase,
                MaxRequestSizeBytes: cm.config.MaxRequestSizeBytes,
        }
}

// UpdateAPIConfig 更新 API 配置（通过修改主模型和独立字段）
func (cm *ConfigManager) UpdateAPIConfig(apiConfig APIConfig) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        // 确定目标模型名称
        modelName := apiConfig.Name
        if modelName == "" {
                modelName = cm.findMainModelNameLocked()
        }
        if modelName == "" {
                return fmt.Errorf("no model to update")
        }

        model, exists := cm.config.Models[modelName]
        if !exists {
                return fmt.Errorf("model not found: %s", modelName)
        }

        // 更新模型字段（保留原有 APIKey 若未提供）
        model.APIType = apiConfig.APIType
        model.BaseURL = apiConfig.BaseURL
        if apiConfig.APIKey != "" {
                model.APIKey = apiConfig.APIKey
        }
        model.Model = apiConfig.Model
        model.Temperature = apiConfig.Temperature
        model.MaxTokens = apiConfig.MaxTokens
        model.Stream = apiConfig.Stream
        model.Thinking = apiConfig.Thinking
        model.BlockDangerousCommands = apiConfig.BlockDangerousCommands
        if apiConfig.Description != "" {
                model.Description = apiConfig.Description
        }

        // 更新 MaxRequestSizeBytes 独立字段
        cm.config.MaxRequestSizeBytes = apiConfig.MaxRequestSizeBytes

        return cm.saveLocked()
}

// UpdateDefaultRole 更新默认角色，保存
func (cm *ConfigManager) UpdateDefaultRole(role string) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        cm.config.DefaultRole = role
        return cm.saveLocked()
}

// UpdateTimeout 更新超时配置，保存
func (cm *ConfigManager) UpdateTimeout(timeout TimeoutConfig) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        cm.config.Timeout = timeout
        return cm.saveLocked()
}

// UpdatePlanModeEnabled 更新規劃模式開關，保存
func (cm *ConfigManager) UpdatePlanModeEnabled(enabled bool) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        cm.config.Tools.PlanModeEnabled = enabled
        return cm.saveLocked()
}

// ReplaceConfig 替换整个配置对象（用于配置向导等场景），保存
func (cm *ConfigManager) ReplaceConfig(config Config) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        cm.config = config
        return cm.saveLocked()
}

// Save 唯一的保存方法
func (cm *ConfigManager) Save() error {
        cm.mu.Lock()
        defer cm.mu.Unlock()
        return cm.saveLocked()
}

// saveLocked 内部保存方法（要求已持写锁）
func (cm *ConfigManager) saveLocked() error {
        // 1. 保存前将 DataDir 转为相对路径（保证可移植性）
        normalizeConfigForSave(&cm.config)

        // 2. 序列化
        data, err := toon.Marshal(cm.config)
        if err != nil {
                return fmt.Errorf("failed to marshal config: %v", err)
        }

        // 3. 写入文件
        if err := os.WriteFile(cm.configPath, data, 0600); err != nil {
                return fmt.Errorf("failed to write config file: %v", err)
        }

        // 4. 恢复 DataDir 为绝对路径（供内存使用）
        if cm.config.DataDir != "" {
                if !filepath.IsAbs(cm.config.DataDir) {
                        cm.config.DataDir = filepath.Join(cm.execDir, cm.config.DataDir)
                }
        } else {
                cm.config.DataDir = cm.execDir
        }

        // 5. 更新全局变量
        cm.syncGlobalsLocked()

        return nil
}

// syncGlobals 将配置同步到全局变量（向后兼容）
func (cm *ConfigManager) syncGlobals() {
        cm.mu.Lock()
        defer cm.mu.Unlock()
        cm.syncGlobalsLocked()
}

// syncGlobalsLocked 将配置同步到全局变量（内部方法，要求已持写锁）
func (cm *ConfigManager) syncGlobalsLocked() {
        globalConfig = cm.config

        // 从主模型获取 APIConfig 并同步全局变量
        apiCfg := cm.getAPIConfigLocked()
        globalAPIConfig = apiCfg
        apiType = apiCfg.APIType
        baseURL = apiCfg.BaseURL
        apiKey = apiCfg.APIKey
        modelID = apiCfg.Model
        temperature = apiCfg.Temperature
        maxTokens = apiCfg.MaxTokens
        stream = apiCfg.Stream
        thinking = apiCfg.Thinking
        BlockDangerousCommands = apiCfg.BlockDangerousCommands

        UserModeBrowser = cm.config.BrowserConfig.UserMode
        HeadlessBrowser = cm.config.BrowserConfig.Headless
        DisableGPUBrowser = cm.config.BrowserConfig.DisableGPU
        DisableDevToolsBrowser = cm.config.BrowserConfig.DisableDevTools
        NoSandboxBrowser = cm.config.BrowserConfig.NoSandbox
        DisableBrowserTools = cm.config.BrowserConfig.DisableBrowserTools
        globalTimeoutConfig = cm.config.Timeout
        globalToolsConfig = cm.config.Tools
        globalPlanModeEnabled = cm.config.Tools.PlanModeEnabled
        setDefaultRole(cm.config.DefaultRole)

        // 热重载：应用用户配置的 Agent Loop 迭代上限（0 = 不限制）
        if cm.config.Tools.MaxAgentIterations > 0 {
                MaxAgentLoopIterations = cm.config.Tools.MaxAgentIterations
                IterationWarningThreshold = MaxAgentLoopIterations * 80 / 100
                log.Printf("Agent Loop 最大迭代次数(热重载): %d, 警告阈值: %d", MaxAgentLoopIterations, IterationWarningThreshold)
        } else {
                MaxAgentLoopIterations = 0
                IterationWarningThreshold = 0
        }
        globalAuthConfig = cm.config.Auth
        globalGroupChatConfig = cm.config.GroupChatConfig

        // ── 同步 Provider Failover Chain 的 default provider ──
        // 當主模型被切換時（SetMainModel → saveLocked → syncGlobalsLocked），
        // globalProviderFailover 中的 “default” provider 必須同步更新，
        // 否則 sendRequestAndGetChunks 會用舊的 baseURL/apiKey 覆蓋 session 級別的配置。
        if globalProviderFailover != nil && baseURL != "" && apiKey != "" {
                globalProviderFailover.RegisterProvider(ProviderConfig{
                        Name:    "default",
                        BaseURL: baseURL,
                        APIKey:  apiKey,
                        Priority: 1,
                        Enabled:  true,
                })
                log.Printf("[ConfigSync] Provider failover 'default' synced: BaseURL=%s", baseURL)
        }
}

// allModelsEmpty 检查所有模型的 ModelBase 字段是否均为零值
// 用于检测配置文件格式不兼容的情况
func (cm *ConfigManager) allModelsEmpty(config *Config) bool {
        for _, m := range config.Models {
                if m.Name != "" || m.APIType != "" || m.Model != "" {
                        return false
                }
        }
        return true
}

// findMainModelLocked 查找主模型配置（内部方法，要求已持锁）
func (cm *ConfigManager) findMainModelLocked() *ModelConfig {
        for _, m := range cm.config.Models {
                if m.IsDefault {
                        return m
                }
        }
        // 回退到第一个可用模型
        for _, m := range cm.config.Models {
                return m
        }
        return nil
}

// findMainModelNameLocked 查找主模型名称（内部方法，要求已持锁）
func (cm *ConfigManager) findMainModelNameLocked() string {
        for name, m := range cm.config.Models {
                if m.IsDefault {
                        return name
                }
        }
        // 回退到第一个可用模型
        for name := range cm.config.Models {
                return name
        }
        return ""
}

// GetConfigPath 返回配置文件路径
func (cm *ConfigManager) GetConfigPath() string {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.configPath
}

// GetExecDir 返回程序目录
func (cm *ConfigManager) GetExecDir() string {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.execDir
}

// getEffectiveAPIConfig 統一的模型配置獲取函數。
// 優先通過 ConfigManager 線程安全讀取（內部持 RLock），
// ConfigManager 未初始化時退回全局變量。
// 所有頻道（session/webhook/telegram/discord/slack/email/cron/debug）
// 調用 AgentLoop 前應使用此函數，而非直接讀裸全局變量。
func getEffectiveAPIConfig() (string, string, string, string, float64, int, bool, bool) {
        if globalConfigManager != nil {
                apiCfg := globalConfigManager.GetAPIConfig()
                return apiCfg.APIType, apiCfg.BaseURL, apiCfg.APIKey, apiCfg.Model,
                        apiCfg.Temperature, apiCfg.MaxTokens, apiCfg.Stream, apiCfg.Thinking
        }
        // ConfigManager 未初始化的後備方案：返回全局變量
        return apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking
}
