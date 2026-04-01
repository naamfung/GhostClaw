package main

import (
    "fmt"
    "os"
    "path/filepath"
    "strconv"

    "github.com/toon-format/toon-go"
)

// 配置常量
const (
    DEFAULT_API_TYPE   = "openai"
    ANTHROPIC_BASE_URL = "https://api.anthropic.com/v1"
    OLLAMA_BASE_URL    = "http://localhost:11434/api"
    OPENAI_BASE_URL    = "https://api.openai.com/v1"
    DEFAULT_MODEL_ID   = "deepseek-chat"
    CONFIG_FILE        = "config.toon"
)

// HTTP服务器配置
type HTTPServerConfig struct {
    Listen string `toon:"Listen" json:"Listen"`
}

// 邮件配置
type EmailConfig struct {
    IMAPServer   string `toon:"IMAPServer" json:"IMAPServer"`
    IMAPPort     int    `toon:"IMAPPort" json:"IMAPPort"`
    IMAPUseTLS   bool   `toon:"IMAPUseTLS" json:"IMAPUseTLS"`
    IMAPUser     string `toon:"IMAPUser" json:"IMAPUser"`
    IMAPPassword string `toon:"IMAPPassword" json:"IMAPPassword"`
    SMTPServer   string `toon:"SMTPServer" json:"SMTPServer"`
    SMTPPort     int    `toon:"SMTPPort" json:"SMTPPort"`
    SMTPUseTLS   bool   `toon:"SMTPUseTLS" json:"SMTPUseTLS"`
    SMTPUser     string `toon:"SMTPUser" json:"SMTPUser"`
    SMTPPassword string `toon:"SMTPPassword" json:"SMTPPassword"`
    PollInterval int    `toon:"PollInterval" json:"PollInterval"`
}

// 浏览器配置
type BrowserConfig struct {
    UserMode bool `toon:"UserMode" json:"UserMode"`
}

// Telegram配置
type TelegramConfig struct {
    Enabled      bool     `toon:"Enabled" json:"Enabled"`
    Token        string   `toon:"Token" json:"Token"`
    AllowFrom    []string `toon:"AllowFrom" json:"AllowFrom"`
    Proxy        string   `toon:"Proxy" json:"Proxy"`
    ReplyToMsg   bool     `toon:"ReplyToMsg" json:"ReplyToMsg"`
    ReactEmoji   string   `toon:"ReactEmoji" json:"ReactEmoji"`
    GroupPolicy  string   `toon:"GroupPolicy" json:"GroupPolicy"`
    Streaming    bool     `toon:"Streaming" json:"Streaming"`
    PollInterval int      `toon:"PollInterval" json:"PollInterval"`
}

type APIConfig struct {
    APIType                string  `toon:"APIType" json:"APIType"`
    BaseURL                string  `toon:"BaseURL" json:"BaseURL"`
    APIKey                 string  `toon:"APIKey" json:"APIKey"`
    Model                  string  `toon:"Model" json:"Model"`
    Temperature            float64 `toon:"Temperature" json:"Temperature"`
    MaxTokens              int     `toon:"MaxTokens" json:"MaxTokens"`
    Stream                 bool    `toon:"Stream" json:"Stream"`
    Thinking               bool    `toon:"Thinking" json:"Thinking"`
    BlockDangerousCommands bool    `toon:"BlockDangerousCommands" json:"BlockDangerousCommands"`
    MaxRequestSizeBytes    int     `toon:"MaxRequestSizeBytes" json:"MaxRequestSizeBytes"` // 新增：请求体最大字节数
}

// 超时配置（单位：秒）
type TimeoutConfig struct {
    Shell   int `toon:"Shell" json:"Shell"`
    HTTP    int `toon:"HTTP" json:"HTTP"`
    Plugin  int `toon:"Plugin" json:"Plugin"`
    Browser int `toon:"Browser" json:"Browser"`
}

// 安全配置
type SecurityConfig struct {
    EnableSSRFProtection bool     `toon:"EnableSSRFProtection" json:"EnableSSRFProtection"`
    AllowPrivateIPs      bool     `toon:"AllowPrivateIPs" json:"AllowPrivateIPs"`
    AllowedHosts         []string `toon:"AllowedHosts" json:"AllowedHosts"`
    BlockedHosts         []string `toon:"BlockedHosts" json:"BlockedHosts"`
}

// 心跳服务配置
type HeartbeatConfig struct {
    Enabled             bool `toon:"Enabled" json:"Enabled"`
    IntervalSeconds     int  `toon:"IntervalSeconds" json:"IntervalSeconds"`
    KeepRecentMessages  int  `toon:"KeepRecentMessages" json:"KeepRecentMessages"`
    MaxConcurrentChecks int  `toon:"MaxConcurrentChecks" json:"MaxConcurrentChecks"`
}

// MCP 服务配置
type MCPConfig struct {
    Enabled      bool   `toon:"Enabled" json:"Enabled"`
    Transport    string `toon:"Transport" json:"Transport"`
    SSEEndpoint  string `toon:"SSEEndpoint" json:"SSEEndpoint"`
    HTTPEndpoint string `toon:"HTTPEndpoint" json:"HTTPEndpoint"`
}

// 认证配置
type AuthConfig struct {
    Enabled      bool   `toon:"Enabled" json:"Enabled"`
    Password     string `toon:"Password" json:"Password"`
    SessionToken string `toon:"SessionToken" json:"SessionToken"`
    TokenExpiry  int    `toon:"TokenExpiry" json:"TokenExpiry"`
}

// Hooks 配置
type HooksConfig struct {
    Enabled        *bool `toon:"Enabled,omitempty" json:"Enabled,omitempty"`
    MaxInputBytes  int   `toon:"MaxInputBytes,omitempty" json:"MaxInputBytes,omitempty"`
    MaxOutputBytes int   `toon:"MaxOutputBytes,omitempty" json:"MaxOutputBytes,omitempty"`
}

// CronConfig 定时任务配置
type CronConfig struct {
    MaxConcurrent int `toon:"MaxConcurrent" json:"MaxConcurrent"`
}

// SmartShellConfig smart_shell 工具配置
type SmartShellConfig struct {
    Enabled         *bool `toon:"Enabled,omitempty" json:"Enabled,omitempty"`
    SyncTimeout     int   `toon:"SyncTimeout" json:"SyncTimeout"`           // 快速命令超时（秒），默认60
    UnknownTimeout  int   `toon:"UnknownTimeout" json:"UnknownTimeout"`     // 未知命令超时（秒），默认120
    DefaultWakeMins int   `toon:"DefaultWakeMins" json:"DefaultWakeMins"`   // 默认唤醒时间（分钟），默认5
}

// ShellToolConfig shell 工具配置
type ShellToolConfig struct {
    Enabled bool `toon:"Enabled" json:"Enabled"`
}

// ShellDelayedConfig shell_delayed 工具配置
type ShellDelayedConfig struct {
    Enabled bool `toon:"Enabled" json:"Enabled"`
}

// MemoryConfig 记忆整合配置
type MemoryConfig struct {
    MinMessagesToConsolidate int     `toon:"MinMessagesToConsolidate" json:"MinMessagesToConsolidate"` // 最小整合消息数
    ConsolidationRatio       float64 `toon:"ConsolidationRatio" json:"ConsolidationRatio"`             // 整合比例
    ContextWindowTokens      int     `toon:"ContextWindowTokens" json:"ContextWindowTokens"`           // 上下文窗口大小
}

// ToolsConfig 工具开关配置
type ToolsConfig struct {
    SmartShell   SmartShellConfig   `toon:"SmartShell" json:"SmartShell"`
    Shell        ShellToolConfig    `toon:"Shell" json:"Shell"`
    ShellDelayed ShellDelayedConfig `toon:"ShellDelayed" json:"ShellDelayed"`
}

// ProfileConfig 个人资料配置
type ProfileConfig struct {
    ReloadMode string `toon:"ReloadMode" json:"ReloadMode"` // "once" or "per_session"
}

// GroupChatConfig 群聊配置
type GroupChatConfig struct {
    DefaultPolicy string   `toon:"DefaultPolicy" json:"DefaultPolicy"` // "open", "mention", "allowlist"
    AllowList     []string `toon:"AllowList" json:"AllowList"`
}

// 主配置结构
type Config struct {
    APIConfig      APIConfig        `toon:"APIConfig" json:"APIConfig"`
    HTTPServer     HTTPServerConfig `toon:"HTTPServer" json:"HTTPServer"`
    EmailConfig    *EmailConfig     `toon:"EmailConfig,omitempty" json:"EmailConfig,omitempty"`
    TelegramConfig *TelegramConfig  `toon:"TelegramConfig,omitempty" json:"TelegramConfig,omitempty"`
    DiscordConfig  *DiscordConfig   `toon:"DiscordConfig,omitempty" json:"DiscordConfig,omitempty"`
    SlackConfig    *SlackConfig     `toon:"SlackConfig,omitempty" json:"SlackConfig,omitempty"`
    FeishuConfig   *FeishuConfig    `toon:"FeishuConfig,omitempty" json:"FeishuConfig,omitempty"`
    IRCConfig      *IRCConfig       `toon:"IRCConfig,omitempty" json:"IRCConfig,omitempty"`
    WebhookConfig  *WebhookConfig   `toon:"WebhookConfig,omitempty" json:"WebhookConfig,omitempty"`
    XMPPConfig     *XMPPConfig      `toon:"XMPPConfig,omitempty" json:"XMPPConfig,omitempty"`
    MatrixConfig   *MatrixConfig    `toon:"MatrixConfig,omitempty" json:"MatrixConfig,omitempty"`
    BrowserConfig  BrowserConfig    `toon:"BrowserConfig" json:"BrowserConfig"`
    DataDir        string           `toon:"DataDir" json:"DataDir,omitempty"`
    CronConfig     CronConfig       `toon:"CronConfig" json:"CronConfig"`
    DefaultRole    string           `toon:"DefaultRole" json:"DefaultRole"`
    Timeout        TimeoutConfig    `toon:"Timeout" json:"Timeout"`
    Security       SecurityConfig   `toon:"Security" json:"Security"`
    Heartbeat      HeartbeatConfig  `toon:"Heartbeat" json:"Heartbeat"`
    MCP            MCPConfig        `toon:"MCP" json:"MCP"`
    Auth           AuthConfig       `toon:"Auth" json:"Auth"`
    Hooks          *HooksConfig     `toon:"Hooks,omitempty" json:"Hooks,omitempty"`
    Tools          ToolsConfig      `toon:"Tools" json:"Tools"`
    Memory         *MemoryConfig    `toon:"Memory,omitempty" json:"Memory,omitempty"`
    ProfileConfig  ProfileConfig    `toon:"Profile,omitempty" json:"Profile,omitempty"`
    GroupChatConfig *GroupChatConfig `toon:"GroupChat,omitempty" json:"GroupChat,omitempty"`
}

// 加载配置文件
func loadConfig() (Config, error) {
    var config Config

    // 获取程序自身路径
    execPath, err := os.Executable()
    if err != nil {
        return config, fmt.Errorf("error getting executable path: %v", err)
    }
    execDir := filepath.Dir(execPath)
    configPath := filepath.Join(execDir, CONFIG_FILE)

    // 读取配置文件
    data, err := os.ReadFile(configPath)
    if err != nil {
        // 生成默认配置
        defaultConfig := Config{}
        defaultConfig.APIConfig.APIType = DEFAULT_API_TYPE
        defaultConfig.APIConfig.Model = DEFAULT_MODEL_ID
        defaultConfig.APIConfig.Temperature = 0.7
        defaultConfig.APIConfig.MaxTokens = 4096
        defaultConfig.APIConfig.Stream = true
        defaultConfig.APIConfig.Thinking = true
        defaultConfig.APIConfig.BlockDangerousCommands = false
        defaultConfig.APIConfig.MaxRequestSizeBytes = 256 * 1024 // 256KB
        defaultConfig.HTTPServer.Listen = "0.0.0.0:10086"
        defaultConfig.DataDir = ""    // 默认为程序自身目录，由 main.go 处理
        defaultConfig.Security.EnableSSRFProtection = true // 默认启用 SSRF 防护

        toonData, err := toon.Marshal(defaultConfig)
        if err == nil {
            os.WriteFile(configPath, toonData, 0644)
            fmt.Printf("Generated default config file at: %s\n", configPath)
        }
        return config, fmt.Errorf("error reading config file: %v", err)
    }

    // 使用 toon 直接解析到结构体
    if err := toon.Unmarshal(data, &config); err != nil {
        return config, fmt.Errorf("error parsing TOON config: %v", err)
    }

    // 设置默认值
    if config.APIConfig.APIType == "" {
        config.APIConfig.APIType = DEFAULT_API_TYPE
    }
    if config.APIConfig.Model == "" {
        config.APIConfig.Model = DEFAULT_MODEL_ID
    }
    if config.APIConfig.MaxTokens == 0 {
        config.APIConfig.MaxTokens = 4096
    }
    if config.APIConfig.MaxRequestSizeBytes == 0 {
        config.APIConfig.MaxRequestSizeBytes = 256 * 1024
    }
    if config.HTTPServer.Listen == "" {
        config.HTTPServer.Listen = "0.0.0.0:10086"
    }
    // DataDir: 数据目录（插件、技能、角色、记忆、数据库等）
    // 默认为程序自身目录，用户可自定义
    if config.DataDir == "" {
        config.DataDir = execDir
    }
    if config.CronConfig.MaxConcurrent == 0 {
        config.CronConfig.MaxConcurrent = 1
    }

    // 设置超时默认值
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

    // 设置心跳配置默认值
    if config.Heartbeat.IntervalSeconds == 0 {
        config.Heartbeat.IntervalSeconds = 1800
    }
    if config.Heartbeat.KeepRecentMessages == 0 {
        config.Heartbeat.KeepRecentMessages = 8
    }
    if config.Heartbeat.MaxConcurrentChecks == 0 {
        config.Heartbeat.MaxConcurrentChecks = 3
    }

    // 设置 MCP 配置默认值
    if config.MCP.Transport == "" {
        config.MCP.Transport = "http"
    }
    if config.MCP.SSEEndpoint == "" {
        config.MCP.SSEEndpoint = "/mcp/sse"
    }
    if config.MCP.HTTPEndpoint == "" {
        config.MCP.HTTPEndpoint = "/mcp"
    }

    // 设置认证配置默认值
    if config.Auth.TokenExpiry == 0 {
        config.Auth.TokenExpiry = 24
    }

    // 设置工具配置默认值
    // SmartShell 默认启用，同步超时60秒，异步唤醒5分钟
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
    // Shell 和 ShellDelayed 默认禁用（零值 false 即为禁用）

    // 如果启用了认证但没有设置密码，生成随机密码并提示
    if config.Auth.Enabled && config.Auth.Password == "" {
        randomPassword := generateRandomPassword(12)
        config.Auth.Password = randomPassword
        fmt.Printf("\n========================================\n")
        fmt.Printf("认证已启用，自动生成密码: %s\n", randomPassword)
        fmt.Printf("请在配置文件中设置自定义密码: Auth.Password\n")
        fmt.Printf("========================================\n\n")
    }

    // 环境变量覆盖
    if v := os.Getenv("API_TYPE"); v != "" {
        config.APIConfig.APIType = v
    }
    if v := os.Getenv("BASE_URL"); v != "" {
        config.APIConfig.BaseURL = v
    }
    if v := os.Getenv("API_KEY"); v != "" {
        config.APIConfig.APIKey = v
    }
    if v := os.Getenv("MODEL_ID"); v != "" {
        config.APIConfig.Model = v
    }
    if v := os.Getenv("TEMPERATURE"); v != "" {
        if f, err := strconv.ParseFloat(v, 64); err == nil {
            config.APIConfig.Temperature = f
        }
    }
    if v := os.Getenv("MAX_TOKENS"); v != "" {
        if i, err := strconv.Atoi(v); err == nil {
            config.APIConfig.MaxTokens = i
        }
    }
    if v := os.Getenv("STREAM"); v != "" {
        if b, err := strconv.ParseBool(v); err == nil {
            config.APIConfig.Stream = b
        }
    }
    if v := os.Getenv("THINKING"); v != "" {
        if b, err := strconv.ParseBool(v); err == nil {
            config.APIConfig.Thinking = b
        }
    }
    if v := os.Getenv("BLOCK_DANGEROUS_COMMANDS"); v != "" {
        if b, err := strconv.ParseBool(v); err == nil {
            config.APIConfig.BlockDangerousCommands = b
        }
    }
    if v := os.Getenv("DEFAULT_ROLE"); v != "" {
        config.DefaultRole = v
    }

    if IsDebug {
        fmt.Printf("Loaded config: %+v\n", config)
    }

    return config, nil
}

// generateRandomPassword 生成随机密码
func generateRandomPassword(length int) string {
    const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
    b := make([]byte, length)
    for i := range b {
        b[i] = charset[i%len(charset)]
    }
    return string(b)
}
