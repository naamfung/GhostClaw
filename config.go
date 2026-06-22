package main

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
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
	UserMode            bool `toon:"UserMode" json:"UserMode"`
	Headless            bool `toon:"Headless" json:"Headless"`
	DisableGPU          bool `toon:"DisableGPU" json:"DisableGPU"`
	DisableDevTools     bool `toon:"DisableDevTools" json:"DisableDevTools"`
	NoSandbox           bool `toon:"NoSandbox" json:"NoSandbox"`
	DisableBrowserTools bool `toon:"DisableBrowserTools" json:"DisableBrowserTools"`
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

// ModelBase 模型通用配置（嵌入到 ModelConfig 和 APIConfig 中，避免重复定义）
type ModelBase struct {
	Name                   string  `json:"Name"`
	Description            string  `json:"Description,omitempty"`
	APIType                string  `json:"APIType"`
	BaseURL                string  `json:"BaseURL"`
	APIKey                 string  `json:"APIKey"` // 支持环境变量 ${VAR}
	Model                  string  `json:"Model"`
	Temperature            float64 `json:"Temperature,omitempty"`
	MaxTokens              int     `json:"MaxTokens,omitempty"`
	ContextLength          int     `json:"ContextLength,omitempty"` // 上下文窗口大小（輸入+輸出總上限），獨立於 MaxTokens
	RateLimit              int     `json:"RateLimit,omitempty"`     // 请求速率限制（次/分钟），0 表示不限制
	Stream                 bool    `json:"Stream,omitempty"`
	Thinking               bool    `json:"Thinking,omitempty"`
	ThinkingFormat         string  `json:"ThinkingFormat,omitempty"` // ""=auto(URL檢測), "object", "bool", "disabled"
	BlockDangerousCommands bool    `json:"BlockDangerousCommands,omitempty"`
	IsDefault              bool    `json:"IsDefault,omitempty"`
}

// ResolveAPIKey 解析 API Key（支持环境变量）
func (m ModelBase) ResolveAPIKey() string {
	key := m.APIKey
	// 检查是否是环境变量引用 ${VAR}
	if strings.HasPrefix(key, "${") && strings.HasSuffix(key, "}") {
		envVar := key[2 : len(key)-1]
		return os.Getenv(envVar)
	}
	return key
}

// APIConfig 仅用于 API 传输和动态计算，不再持久化到配置文件
// 嵌入 Model，额外包含 MaxRequestSizeBytes（API 层面独立配置）
type APIConfig struct {
	ModelBase
	MaxRequestSizeBytes int `json:"MaxRequestSizeBytes,omitempty"` // 请求体最大字节数（独立于模型）
}

// 超时配置（单位：秒）
type TimeoutConfig struct {
	MinTimeout int `toon:"MinTimeout" json:"MinTimeout"` // 全局最低超时（秒），0=不限制
	Shell      int `toon:"Shell" json:"Shell"`
	HTTP       int `toon:"HTTP" json:"HTTP"`
	Plugin     int `toon:"Plugin" json:"Plugin"`
	Browser    int `toon:"Browser" json:"Browser"`
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

// SmartShellConfig SmartShell 工具配置
type SmartShellConfig struct {
	Enabled         *bool `toon:"Enabled,omitempty" json:"Enabled,omitempty"`
	SyncTimeout     int   `toon:"SyncTimeout" json:"SyncTimeout"`         // 快速命令超时（秒），默认60
	UnknownTimeout  int   `toon:"UnknownTimeout" json:"UnknownTimeout"`   // 未知命令超时（秒），默认120
	DefaultWakeMins int   `toon:"DefaultWakeMins" json:"DefaultWakeMins"` // 默认唤醒时间（分钟），默认5
}

// MemoryConfig 记忆整合配置
type MemoryConfig struct {
	MinMessagesToConsolidate int     `toon:"MinMessagesToConsolidate" json:"MinMessagesToConsolidate"` // 最小整合消息数
	ConsolidationRatio       float64 `toon:"ConsolidationRatio" json:"ConsolidationRatio"`             // 整合比例
	ContextWindowTokens      int     `toon:"ContextWindowTokens" json:"ContextWindowTokens"`           // 上下文窗口大小
}

// ToolsConfig 工具开关配置
type ToolsConfig struct {
	SmartShell                SmartShellConfig `toon:"SmartShell" json:"SmartShell"`
	MaxAgentIterations        int              `toon:"MaxAgentIterations,omitempty" json:"MaxAgentIterations,omitempty"`               // Agent Loop 最大迭代次数（0=使用默认值100）
	CompressionMode           string           `toon:"CompressionMode,omitempty" json:"CompressionMode,omitempty"`                     // "token"（預設）或 "message"
	CompressionThreshold      float64          `toon:"CompressionThreshold,omitempty" json:"CompressionThreshold,omitempty"`           // 0.1-0.9，預設 0.8
	SkillCleanupThresholdDays int              `toon:"SkillCleanupThresholdDays,omitempty" json:"SkillCleanupThresholdDays,omitempty"` // Skill 自動清理天數，預設 90，範圍 30-365
	EscalationThreshold       int              `toon:"EscalationThreshold,omitempty" json:"EscalationThreshold,omitempty"`             // 工具連續失敗升級閾值，預設 3，範圍 1-5
	EnableParallelTools       bool             `toon:"EnableParallelTools" json:"EnableParallelTools"`                                 // 啟用並行工具調用（部分 provider 唔支援，預設關閉）
	DeferExtendedTools        bool             `toon:"DeferExtendedTools" json:"DeferExtendedTools"`                                   // 延遲加載非核心工具（Extended/Expert tier 只顯示名稱，通過 Menu 按需加載完整 schema）
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

// SystemInfoConfig 系统信息配置
type SystemInfoConfig struct {
	Enabled          bool `toon:"Enabled" json:"Enabled"`                   // 是否启用系统信息注入
	IncludeMemory    bool `toon:"IncludeMemory" json:"IncludeMemory"`       // 包含内存信息
	IncludeCPU       bool `toon:"IncludeCPU" json:"IncludeCPU"`             // 包含 CPU 信息
	IncludeGPU       bool `toon:"IncludeGPU" json:"IncludeGPU"`             // 包含 GPU 信息
	IncludeOSDetails bool `toon:"IncludeOSDetails" json:"IncludeOSDetails"` // 包含详细操作系统信息
}

// ResilienceConfig 網絡韌性配置
type ResilienceConfig struct {
	EnableFailover        bool    `toon:"EnableFailover" json:"EnableFailover"`               // 啟用故障轉移
	EnableTimeoutScaling  bool    `toon:"EnableTimeoutScaling" json:"EnableTimeoutScaling"`   // 啟用超時自動放寬
	MaxRetries            int     `toon:"MaxRetries" json:"MaxRetries"`                       // 最大重試次數，0=無限（當無failover時）
	TimeoutScaleFactor    float64 `toon:"TimeoutScaleFactor" json:"TimeoutScaleFactor"`       // 超時放寬倍率，預設 1.5
	MaxTimeoutSeconds     int     `toon:"MaxTimeoutSeconds" json:"MaxTimeoutSeconds"`         // 超時上限（秒），預設 600
	InitialBackoffSeconds int     `toon:"InitialBackoffSeconds" json:"InitialBackoffSeconds"` // 初始重試間隔（秒），預設 5
	MaxBackoffSeconds     int     `toon:"MaxBackoffSeconds" json:"MaxBackoffSeconds"`         // 最大重試間隔（秒），預設 300
	BackoffMultiplier     float64 `toon:"BackoffMultiplier" json:"BackoffMultiplier"`         // 退避倍率，預設 2.0
}

// PromptCacheConfig 提示词缓存配置
type PromptCacheConfig struct {
	Enabled     bool `toon:"Enabled" json:"Enabled"`         // 啟用 提示词缓存（cache_control breakpoints + anthropic-version header）
	StableTools bool `toon:"StableTools" json:"StableTools"` // 穩定工具集：請求之間不改變工具列表以保持 cache prefix 一致
}

// ModelConfig 模型配置（持久化到 config.toon）
// 嵌入 ModelBase，toon-go 按嵌套格式序列化/反序列化 ModelBase 字段
type ModelConfig struct {
	ModelBase
}

// 主配置结构
// APIConfig 字段已移除，运行时通过 ConfigManager.GetAPIConfig() 从主模型动态获取。
// MaxRequestSizeBytes 作为独立字段保留。
type Config struct {
	// APIConfig 不再持久化，仅用于运行时
	Models                  map[string]*ModelConfig `toon:"Models" json:"Models"`
	MaxRequestSizeBytes     int                     `toon:"MaxRequestSizeBytes" json:"MaxRequestSizeBytes"` // 请求体最大字节数
	HTTPServer              HTTPServerConfig        `toon:"HTTPServer" json:"HTTPServer"`
	EmailConfig             *EmailConfig            `toon:"EmailConfig,omitempty" json:"EmailConfig,omitempty"`
	TelegramConfig          *TelegramConfig         `toon:"TelegramConfig,omitempty" json:"TelegramConfig,omitempty"`
	DiscordConfig           *DiscordConfig          `toon:"DiscordConfig,omitempty" json:"DiscordConfig,omitempty"`
	SlackConfig             *SlackConfig            `toon:"SlackConfig,omitempty" json:"SlackConfig,omitempty"`
	FeishuConfig            *FeishuConfig           `toon:"FeishuConfig,omitempty" json:"FeishuConfig,omitempty"`
	IRCConfig               *IRCConfig              `toon:"IRCConfig,omitempty" json:"IRCConfig,omitempty"`
	WebhookConfig           *WebhookConfig          `toon:"WebhookConfig,omitempty" json:"WebhookConfig,omitempty"`
	XMPPConfig              *XMPPConfig             `toon:"XMPPConfig,omitempty" json:"XMPPConfig,omitempty"`
	MatrixConfig            *MatrixConfig           `toon:"MatrixConfig,omitempty" json:"MatrixConfig,omitempty"`
	BrowserConfig           BrowserConfig           `toon:"BrowserConfig" json:"BrowserConfig"`
	DataDir                 string                  `toon:"DataDir" json:"DataDir,omitempty"`
	CronConfig              CronConfig              `toon:"CronConfig" json:"CronConfig"`
	DefaultRole             string                  `toon:"DefaultRole" json:"DefaultRole"`
	DefaultLanguage         string                  `toon:"DefaultLanguage,omitempty" json:"DefaultLanguage,omitempty"` // 輸出語言（自然語言描述），如「广东简体粤语」「台湾繁体中文」「English」
	Timeout                 TimeoutConfig           `toon:"Timeout" json:"Timeout"`
	Security                SecurityConfig          `toon:"Security" json:"Security"`
	Heartbeat               HeartbeatConfig         `toon:"Heartbeat" json:"Heartbeat"`
	MCP                     MCPConfig               `toon:"MCP" json:"MCP"`
	Auth                    AuthConfig              `toon:"Auth" json:"Auth"`
	Hooks                   *HooksConfig            `toon:"Hooks,omitempty" json:"Hooks,omitempty"`
	Tools                   ToolsConfig             `toon:"Tools" json:"Tools"`
	Memory                  *MemoryConfig           `toon:"Memory,omitempty" json:"Memory,omitempty"`
	ProfileConfig           ProfileConfig           `toon:"Profile,omitempty" json:"Profile,omitempty"`
	GroupChatConfig         *GroupChatConfig        `toon:"GroupChat,omitempty" json:"GroupChat,omitempty"`
	SystemInfo              SystemInfoConfig        `toon:"SystemInfo" json:"SystemInfo"`
	Session                 *SessionConfig          `toon:"Session,omitempty" json:"Session,omitempty"`
	MaxWorkModeResumeRounds int                     `toon:"MaxWorkModeResumeRounds" json:"MaxWorkModeResumeRounds"` // 工作模式退出守衛最大續行次數，默認3
	Resilience              ResilienceConfig        `toon:"Resilience" json:"Resilience"`                           // 網絡韌性配置
	PromptCache             PromptCacheConfig       `toon:"PromptCache" json:"PromptCache"`                         // 提示词缓存配置
}

// normalizeConfigForSave 在保存配置前将 DataDir 转为相对路径
// 确保配置文件的可移植性——方便用户转移数据目录
// 如果 DataDir 在 execDir 之下，转为相对于 execDir 的路径
// 如果 DataDir 等于 execDir，清空（省略写入，读取时自动使用 execDir）
func normalizeConfigForSave(config *Config) {
	if config.DataDir == "" {
		return
	}
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	execDir := filepath.Dir(execPath)

	// 清理路径（解析 .. 和符号链接）
	absDataDir, err := filepath.Abs(config.DataDir)
	if err != nil {
		return
	}
	absExecDir, err := filepath.Abs(execDir)
	if err != nil {
		return
	}

	// 如果 DataDir 就是 execDir，清空（读取时默认使用 execDir）
	if absDataDir == absExecDir {
		config.DataDir = ""
		return
	}

	// 如果 DataDir 是 execDir 的子目录或可转为相对路径，使用相对路径
	rel, err := filepath.Rel(absExecDir, absDataDir)
	if err != nil {
		return
	}
	// 避免产生深层 ../ 逃逸路径（如 ../../../etc/data），保持原值
	if strings.HasPrefix(rel, "..") {
		// 用户显式设置了外部目录，保持绝对路径不变
		return
	}
	config.DataDir = rel
}

// generateRandomPassword 生成随机密码
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
	b := make([]byte, length)
	rand.Read(b)
	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b)
}
