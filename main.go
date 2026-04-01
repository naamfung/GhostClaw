package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/chzyer/readline"
)

// 全局配置变量
var (
	apiType     string
	baseURL     string
	apiKey      string
	modelID     string
	temperature float64
	maxTokens   int
	stream      bool
	thinking    bool

	defaultRole string

	BlockDangerousCommands bool
	UserModeBrowser        bool
	IsDebug                bool

	globalAPIConfig      APIConfig
	globalTimeoutConfig  TimeoutConfig
	globalToolsConfig    ToolsConfig
	globalEmailConfig    *EmailConfig
	globalTelegramConfig *TelegramConfig
	globalDiscordConfig  *DiscordConfig
	globalSlackConfig    *SlackConfig
	globalFeishuConfig   *FeishuConfig
	globalIRCConfig      *IRCConfig
	globalWebhookConfig  *WebhookConfig
	globalXMPPConfig     *XMPPConfig
	globalMatrixConfig   *MatrixConfig

	globalCronManager      *CronManager
	globalTaskTracker      *TaskTracker
	globalRoleManager      *RoleManager
	globalActorManager     *ActorManager
	globalStage            *Stage
	globalSkillManager     *SkillManager
	globalTaskManager      *TaskManager
	globalMCPServer        *MCPServer
	globalUnifiedMemory    *UnifiedMemory
	globalProfileLoader    *ProfileLoader
	globalGroupChatConfig  *GroupChatConfig
	globalPluginManager    *PluginManager
	globalMCPClientManager *MCPClientManager
	globalSessionPersist   *SessionPersistManager
	globalHeartbeatService *HeartbeatService
	globalSubagentManager  *SubagentManager
	globalAuthManager      *AuthManager
	globalAuthConfig       AuthConfig
	globalCancel           context.CancelFunc
	globalExecDir          string
	globalUploadDir        string

	// cmdModeActive 控制日志是否输出到终端
	// true = CMD REPL 模式，日志静默（只显示模型对话流）
	// false = Monitor 模式，正常输出所有日志
	cmdModeActive bool = false
)

func init() {
	globalUploadDir = filepath.Join(globalExecDir, "uploads")
}

// cliLogWriter 是自定义的日志写入器
// 在 CMD 模式下丢弃日志输出，在 Monitor 模式下正常输出
type cliLogWriter struct {
	underlying io.Writer
}

func (w *cliLogWriter) Write(p []byte) (n int, err error) {
	if cmdModeActive {
		return len(p), nil // 静默丢弃
	}
	return w.underlying.Write(p)
}

// enterCMDMode 从 Monitor 模式切换到 CMD REPL 模式
// 设置 terminal raw mode 以捕获键盘输入，然后创建 readline 实例
func enterCMDMode(ctx context.Context, session *GlobalSession) {
	cmdModeActive = true
	fmt.Println("\n╔══════════════════════════════════════╗")
	fmt.Println("║  GhostClaw CMD 模式                   ║")
	fmt.Println("║  输入消息与模型对话                     ║")
	fmt.Println("║  /quit → 退出 CMD 模式，返回 Monitor   ║")
	fmt.Println("║  /exit → 退出程序                      ║")
	fmt.Println("╚══════════════════════════════════════╝")

	rl, err := readline.New("GhostClaw /> ")
	if err != nil {
		log.Printf("Failed to create readline: %v", err)
		cmdModeActive = false
		return
	}

	cmdChan := NewCmdChannel()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF || errors.Is(err, readline.ErrInterrupt) {
				break
			}
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// /quit: 退出 CMD 模式，回到 Monitor
		if line == "/quit" {
			fmt.Println("\n↩ 退出 CMD 模式，返回 Monitor...")
			rl.Close()
			cmdModeActive = false
			return
		}

		// /exit: 直接退出程序
		if line == "/exit" {
			fmt.Println("\n✋ 正在退出程序...")
			rl.Close()
			session.autoSaveHistory()
			os.Exit(0)
		}

		// 处理其他斜杠命令
		if HandleSlashCommandWithDefaults(line,
			func(resp string) {
				fmt.Println(resp)
			},
			func() {
				if globalCancel != nil {
					globalCancel()
					globalCancel = nil
				}
			},
			func() {
				fmt.Println("✋ 正在退出程序...")
				session.autoSaveHistory()
				os.Exit(0)
			}) {
			continue
		}

		// 普通对话：读取全局会话历史并发送给模型
		history := session.GetHistory()
		history = append(history, Message{Role: "user", Content: line})
		if globalTaskTracker != nil {
			globalTaskTracker.StartNewTask(line)
		}
		newHistory, err := AgentLoop(ctx, cmdChan, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
		if err != nil {
			fmt.Printf("Agent error: %v\n", err)
		} else {
			history = newHistory
			session.SetHistory(history)
		}
		if globalTaskTracker != nil {
			globalTaskTracker.MarkCompleted()
		}
		fmt.Println()
	}

	rl.Close()
	cmdModeActive = false
}

// runMonitorMode Monitor 模式：程序正常运行，终端只显示日志
// 监听用户按下 '/' 键，按下后切换到 CMD REPL 模式
func runMonitorMode(ctx context.Context, cancel context.CancelFunc, session *GlobalSession, emailPoller *EmailPoller) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║  GhostClaw Monitor 模式（默认）       ║")
	fmt.Println("║  按 / 键进入 CMD 模式与模型对话       ║")
	fmt.Println("║  Ctrl+C 退出程序                      ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println()

	// 将 stdin 设为 raw mode 以捕获单字符按键
	oldState, err := setRawMode()
	if err != nil {
		log.Printf("Warning: cannot set raw mode: %v, falling back to readline mode", err)
		// 回退：直接进入 readline 模式
		rl, rlErr := readline.New("GhostClaw /> ")
		if rlErr != nil {
			log.Fatalf("Failed to create readline: %v", rlErr)
		}
		cmdModeActive = true
		cmdChan := NewCmdChannel()
		for {
			line, err := rl.Readline()
			if err != nil {
				break
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if line == "/quit" {
				fmt.Println("\n↩ (已在单频道模式)")
				continue
			}
			if line == "/exit" || line == "exit" {
				fmt.Println("\n✋ 正在退出程序...")
				rl.Close()
				session.autoSaveHistory()
				os.Exit(0)
			}
			if HandleSlashCommandWithDefaults(line,
				func(resp string) { fmt.Println(resp) },
				func() {
					if globalCancel != nil {
						globalCancel()
						globalCancel = nil
					}
				},
				func() {
					fmt.Println("✋ 正在退出程序...")
					session.autoSaveHistory()
					os.Exit(0)
				}) {
				continue
			}
			history := session.GetHistory()
			history = append(history, Message{Role: "user", Content: line})
			newHistory, err := AgentLoop(ctx, cmdChan, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
			if err != nil {
				fmt.Printf("Agent error: %v\n", err)
			} else {
				session.SetHistory(newHistory)
			}
			fmt.Println()
		}
		rl.Close()
		return
	}
	defer restoreTerminal(oldState)

	reader := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 检查是否有数据可读
		var buf [1]byte
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			if err == io.EOF {
				continue
			}
			continue
		}
		if n == 0 {
			continue
		}

		ch := buf[0]

		// 如果用户按下了 '/'
		if ch == '/' {
			// 回显 / 让用户知道已识别
			fmt.Print("/")
			// 恢复 terminal 到 cooked mode（readline 需要）
			restoreTerminal(oldState)
			// 读取行剩余部分（用户可能按了 / 后面跟了其他字符）
			rest, _ := reader.ReadString('\n')
			rest = strings.TrimSpace(rest)
			// 如果用户只是单独按了 / 没有其他字符
			if rest == "" {
				enterCMDMode(ctx, session)
				// CMD 模式退出后，重新设置 raw mode
				newState, err := setRawMode()
				if err != nil {
					log.Printf("Warning: cannot re-enter raw mode: %v", err)
					return
				}
				defer restoreTerminal(newState) // 重新 defer
				fmt.Println()
				fmt.Println("↩ 返回 Monitor 模式，按 / 键进入 CMD 模式")
				fmt.Println()
			} else {
				// 用户输入了 /quit /exit 或其他命令，直接处理
				fullCmd := "/" + rest
				if fullCmd == "/quit" {
					fmt.Println("\n↩ (已在 Monitor 模式)")
				} else if fullCmd == "/exit" {
					fmt.Println("\n✋ 正在退出程序...")
					session.autoSaveHistory()
					os.Exit(0)
				} else {
					fmt.Printf("\nMonitor 模式下输入了命令: %s\n", fullCmd)
					fmt.Println("提示: 按 / 进入 CMD 模式后可以执行命令")
				}
			}
		}
		// 其他按键在 Monitor 模式下被忽略（日志由后台 goroutine 输出到 stderr）
	}
}

func main() {
	// 命令行参数
	promptFlag := flag.String("p", "", "调试模式：直接传入提示词，模型输出完成后自动退出")
	promptFlagLong := flag.String("prompt", "", "调试模式：直接传入提示词，模型输出完成后自动退出（长格式）")
	debugFlag := flag.Bool("debug", false, "启用调试输出")
	flag.Parse()

	prompt := *promptFlag
	if prompt == "" {
		prompt = *promptFlagLong
	}

	if *debugFlag {
		IsDebug = true
	}

	// 初始化程序所在目录（必须在其他初始化之前）
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: cannot get executable path: %v", err)
		execPath = "."
	}
	globalExecDir = filepath.Dir(execPath)

	// 加载配置
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	// 检查是否需要配置向导
	if NeedsSetup(config) {
		result := RunConfigWizard(config)
		if !result.IsCompleted {
			fmt.Println("配置未完成，程序退出。")
			os.Exit(0)
		}
		config = result.Config
	}

	// 从配置中赋值全局变量
	apiType = config.APIConfig.APIType
	globalAPIConfig = config.APIConfig
	baseURL = config.APIConfig.BaseURL
	apiKey = config.APIConfig.APIKey
	modelID = config.APIConfig.Model
	temperature = config.APIConfig.Temperature
	maxTokens = config.APIConfig.MaxTokens
	stream = config.APIConfig.Stream
	thinking = config.APIConfig.Thinking
	BlockDangerousCommands = config.APIConfig.BlockDangerousCommands
	UserModeBrowser = config.BrowserConfig.UserMode
	globalEmailConfig = config.EmailConfig
	globalTelegramConfig = config.TelegramConfig
	globalDiscordConfig = config.DiscordConfig
	globalSlackConfig = config.SlackConfig
	globalFeishuConfig = config.FeishuConfig
	globalIRCConfig = config.IRCConfig
	globalWebhookConfig = config.WebhookConfig
	globalXMPPConfig = config.XMPPConfig
	globalMatrixConfig = config.MatrixConfig
	globalTimeoutConfig = config.Timeout
	globalToolsConfig = config.Tools
	defaultRole = config.DefaultRole
	globalAuthConfig = config.Auth
	globalGroupChatConfig = config.GroupChatConfig

	// 初始化安全配置
	SetSecurityConfig(config.Security)
	if config.Security.EnableSSRFProtection {
		log.Println("SSRF protection is ENABLED.")
	} else {
		log.Println("WARNING: SSRF protection is DISABLED. This is not recommended for production.")
	}

	fmt.Printf("Using model: %s\n", modelID)
	if !BlockDangerousCommands {
		fmt.Println("Dangerous command blocking is DISABLED. The model can execute any command.")
	}
	if UserModeBrowser {
		fmt.Println("Browser user mode is ENABLED. Using existing browser session.")
	}

	// 初始化数据库
	if err := InitDB(globalExecDir); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	log.Println("Database initialized.")

	// 初始化插件管理器
	pluginsDir := config.PluginsDir
	globalPluginManager = NewPluginManager(pluginsDir)
	globalPluginManager.SetToolExecutor(callToolInternal)
	if err := globalPluginManager.LoadPluginsFromDir(); err != nil {
		log.Printf("Warning: failed to load plugins: %v", err)
	}
	plugins := globalPluginManager.ListPlugins()
	if len(plugins) > 0 {
		log.Printf("Loaded %d plugin(s):", len(plugins))
		for _, p := range plugins {
			log.Printf("  - %s (%s)", p["name"], p["file"])
		}
	} else {
		log.Println("No plugins loaded. Plugins directory:", pluginsDir)
	}
	defer func() {
		if globalPluginManager != nil {
			globalPluginManager.Close()
		}
	}()

	// 初始化 CronManager
	cronFilePath := filepath.Join(globalExecDir, "cron.toon")
	globalCronManager, err = NewCronManager(cronFilePath, &config.CronConfig)
	if err != nil {
		log.Printf("Warning: failed to start cron manager: %v", err)
	} else {
		defer globalCronManager.Stop()
		log.Println("Cron manager started.")
	}

	// 初始化统一记忆系统
	globalUnifiedMemory, err = NewUnifiedMemory(globalExecDir)
	if err != nil {
		log.Printf("Warning: failed to start unified memory: %v", err)
	} else {
		log.Println("Unified memory system started.")
	}

	// 初始化任务进度追踪器
	globalTaskTracker = NewTaskTracker()

	// 初始化循环检测器
	InitGlobalLoopDetector()
	log.Println("Loop detector initialized.")

	// 初始化后台任务管理器
	globalTaskManager = NewTaskManager()
	globalTaskManager.SetWakeHandler(func(task *BackgroundTask) {
		log.Printf("[TaskManager] Task %s wake up, status: %s", task.ID, task.Status)

		task.mu.RLock()
		output := truncateTaskOutput(task.Stdout.String())
		_ = truncateTaskOutput(task.Stderr.String())
		task.mu.RUnlock()

		wakeMsg := GetTaskWakeMessage(task)

		if task.SessionID != "" {
			GetBus().NotifyDelayedTask(
				task.ID,
				task.Command,
				string(task.Status),
				output,
				task.SessionID,
			)
			log.Printf("[TaskManager] Wake notification sent for task %s to session %s", task.ID, task.SessionID)

			session := GetGlobalSession()
			if !session.IsTaskRunning() {
				session.AddToHistory("user", wakeMsg)
				log.Printf("[TaskManager] Triggering model call for global session")
			} else {
				session.EnqueueOutput(StreamChunk{
					Content: "\n\n" + wakeMsg + "\n\n",
				})
			}
		} else {
			log.Printf("[TaskManager] Task %s has no session ID, cannot send wake notification", task.ID)
		}
	})
	log.Println("Task manager started.")
	defer func() {
		if globalTaskManager != nil {
			globalTaskManager.Stop()
		}
	}()

	// 初始化消息总线
	initMessageBus()
	log.Println("Message bus initialized.")

	// 初始化子代理管理器
	globalSubagentManager = NewSubagentManager()
	globalSubagentManager.SetResultHandler(func(task *SubagentTask) {
		log.Printf("[Subagent] Task %s completed: %s", task.ID, task.Status)
		if task.SessionID != "" {
			GetBus().NotifySubagent(task.ID, string(task.Status), task.Result, task.SessionID)
		}
	})
	log.Println("Subagent manager started.")
	defer func() {
		if globalSubagentManager != nil {
			globalSubagentManager.Stop()
		}
	}()

	// 初始化心跳服务
	if config.Heartbeat.Enabled {
		globalHeartbeatService = NewHeartbeatService(config.Heartbeat, globalExecDir)
		SetHeartbeatNotifier(NewBusHeartbeatNotifier())
		if err := globalHeartbeatService.Start(); err != nil {
			log.Printf("Warning: failed to start heartbeat service: %v", err)
		}
		defer func() {
			if globalHeartbeatService != nil {
				globalHeartbeatService.Stop()
			}
		}()
	} else {
		log.Println("Heartbeat service is disabled")
	}

	// 初始化角色模板管理器
	roleFilePath := filepath.Join(globalExecDir, "role.toon")
	globalRoleManager, err = NewRoleManager(roleFilePath)
	if err != nil {
		log.Printf("Warning: failed to start role manager: %v", err)
	} else {
		log.Printf("Role manager started. %d roles available.", globalRoleManager.Count())
	}

	// 初始化演员管理器
	actorFilePath := filepath.Join(globalExecDir, "actor.toon")
	globalActorManager, err = NewActorManager(actorFilePath, apiType, baseURL, apiKey, modelID, temperature, maxTokens, config.DefaultRole)
	if err != nil {
		log.Printf("Warning: failed to start actor manager: %v", err)
	} else {
		log.Printf("Actor manager started. %d actors available.", len(globalActorManager.ListActors()))
	}

	// 初始化场景管理器
	globalStage = NewStage()

	// 初始化 ProfileLoader
	profilesDir := filepath.Join(globalExecDir, "profiles")
	globalProfileLoader, err = NewProfileLoader(profilesDir)
	if err != nil {
		log.Printf("Warning: failed to start profile loader: %v", err)
	} else {
		defer globalProfileLoader.Stop()
		log.Println("Profile loader started.")
	}

	// 加载工具别名（tools.toon）
	toolsAliasPath := filepath.Join(globalExecDir, "tools.toon")
	globalToolsAliases, err = LoadToolsAliases(toolsAliasPath)
	if err != nil {
		log.Printf("Tools aliases not loaded: %v", err)
	} else {
		log.Printf("Tools aliases loaded: %d entries", len(globalToolsAliases))
	}

	// 初始化技能管理器
	skillsDir := filepath.Join(globalExecDir, "skills")
	globalSkillManager, err = NewSkillManager(skillsDir)
	if err != nil {
		log.Printf("Warning: failed to start skill manager: %v", err)
	} else {
		log.Printf("Skill manager started. %d skills available.", globalSkillManager.Count())
	}

	// 初始化 MCP 服务器
	if config.MCP.Enabled {
		globalMCPServer = NewMCPServer("GhostClaw", "1.0.0")
		initMCPTools(globalMCPServer)
		log.Printf("MCP server started (transport: %s)", config.MCP.Transport)

		if config.MCP.Transport == "stdio" {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			log.Println("MCP server running in stdio mode")
			if err := globalMCPServer.StartStdio(ctx); err != nil {
				log.Fatalf("MCP stdio error: %v", err)
			}
			return
		}
	}

	// 初始化 MCP 客户端管理器
	if err := InitMCPClients(globalExecDir); err != nil {
		log.Printf("Warning: failed to init MCP clients: %v", err)
	} else if globalMCPClientManager != nil && globalMCPClientManager.Count() > 0 {
		log.Printf("MCP client manager started with %d server(s)", globalMCPClientManager.Count())
	}
	defer func() {
		if globalMCPClientManager != nil {
			globalMCPClientManager.DisconnectAll()
		}
	}()

	// 初始化记忆整合器
	consolidatorConfig := DefaultMemoryConsolidatorConfig()
	if config.Memory != nil {
		if config.Memory.MinMessagesToConsolidate > 0 {
			consolidatorConfig.MinMessagesToConsolidate = config.Memory.MinMessagesToConsolidate
		}
		if config.Memory.ConsolidationRatio > 0 {
			consolidatorConfig.ConsolidationRatio = config.Memory.ConsolidationRatio
		}
		if config.Memory.ContextWindowTokens > 0 {
			consolidatorConfig.ContextWindowTokens = config.Memory.ContextWindowTokens
		}
	}
	InitMemoryConsolidator(consolidatorConfig, globalUnifiedMemory)
	log.Printf("Memory consolidator initialized (MinMsgs: %d, Ratio: %.2f%%)",
		consolidatorConfig.MinMessagesToConsolidate,
		consolidatorConfig.ConsolidationRatio*100)

	// 初始化会话持久化管理器（基于数据库）
	InitSessionPersist()
	log.Println("Session persistence initialized (database-backed)")

	// 初始化 Hook 管理器
	InitHookManager(&config)
	if globalHookManager != nil {
		hooks := globalHookManager.List()
		enabledCount := 0
		for _, h := range hooks {
			if h.Enabled {
				enabledCount++
			}
		}
		log.Printf("Hook manager started. %d hooks found, %d enabled", len(hooks), enabledCount)
	}

	// 启动 HTTP 服务器
	if config.HTTPServer.Listen != "" {
		httpServer := NewHTTPServer(config.HTTPServer.Listen)
		go func() {
			httpServer.Start()
		}()
	}

	// 启动邮件轮询
	var emailPoller *EmailPoller
	if config.EmailConfig != nil {
		emailPoller = &EmailPoller{config: config.EmailConfig, stop: make(chan struct{})}
		emailPoller.Start()
		log.Println("Email polling started")
	}

	// 启动各渠道 Bot
	startChannels(&config)

	// 获取全局会话
	session := GetGlobalSession()

	// 调试模式：直接执行提示词并退出
	if prompt != "" {
		runDebugMode(prompt, session)
		return
	}

	// 安装自定义日志 writer：CMD 模式下静默日志输出
	logWriter := &cliLogWriter{underlying: os.Stderr}
	log.SetOutput(logWriter)

	ctx, cancel := context.WithCancel(context.Background())
	globalCancel = cancel

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n✋ 收到终止信号，正在关闭...")
		cancel()
		session.autoSaveHistory()
		if emailPoller != nil {
			emailPoller.Stop()
		}
		os.Exit(0)
	}()

	// 启动 Monitor 模式（默认）
	// 终端只显示日志，按 / 键进入 CMD REPL 模式
	runMonitorMode(ctx, cancel, session, emailPoller)

	session.autoSaveHistory()
	if emailPoller != nil {
		emailPoller.Stop()
	}
}

// startChannels 启动所有启用的渠道
func startChannels(config *Config) {
	// 启动 Telegram Bot
	if config.TelegramConfig != nil && config.TelegramConfig.Enabled {
		telegramChannel, err := NewTelegramChannel(config.TelegramConfig)
		if err != nil {
			log.Printf("Warning: failed to create Telegram channel: %v", err)
		} else {
			err = telegramChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Telegram message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "telegram")
			})
			if err != nil {
				log.Printf("Warning: failed to start Telegram bot: %v", err)
			} else {
				log.Println("Telegram bot started")
				telegramChannel.RegisterToBus()
			}
		}
	}

	// 启动 Discord Bot
	if config.DiscordConfig != nil && config.DiscordConfig.Enabled {
		discordChannel, err := NewDiscordChannel(config.DiscordConfig)
		if err != nil {
			log.Printf("Warning: failed to create Discord channel: %v", err)
		} else {
			err = discordChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Discord message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "discord")
			})
			if err != nil {
				log.Printf("Warning: failed to start Discord bot: %v", err)
			} else {
				log.Println("Discord bot started")
				discordChannel.RegisterToBus()
			}
		}
	}

	// 启动 Slack Bot
	if config.SlackConfig != nil && config.SlackConfig.Enabled {
		slackChannel, err := NewSlackChannel(config.SlackConfig)
		if err != nil {
			log.Printf("Warning: failed to create Slack channel: %v", err)
		} else {
			err = slackChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Slack message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "slack")
			})
			if err != nil {
				log.Printf("Warning: failed to start Slack bot: %v", err)
			} else {
				log.Println("Slack bot started")
				slackChannel.RegisterToBus()
			}
		}
	}

	// 启动 Feishu Bot
	if config.FeishuConfig != nil && config.FeishuConfig.Enabled {
		feishuChannel, err := NewFeishuChannel(config.FeishuConfig)
		if err != nil {
			log.Printf("Warning: failed to create Feishu channel: %v", err)
		} else {
			err = feishuChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Feishu message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "feishu")
			})
			if err != nil {
				log.Printf("Warning: failed to start Feishu bot: %v", err)
			} else {
				log.Println("Feishu bot started")
				feishuChannel.RegisterToBus()
			}
		}
	}

	// 启动 IRC Bot
	if config.IRCConfig != nil && config.IRCConfig.Enabled {
		ircChannel, err := NewIRCChannel(config.IRCConfig)
		if err != nil {
			log.Printf("Warning: failed to create IRC channel: %v", err)
		} else {
			err = ircChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("IRC message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "irc")
			})
			if err != nil {
				log.Printf("Warning: failed to start IRC bot: %v", err)
			} else {
				log.Println("IRC bot started")
				ircChannel.RegisterToBus()
			}
		}
	}

	// 启动 Webhook 服务
	if config.WebhookConfig != nil && config.WebhookConfig.Enabled {
		webhookChannel, err := NewWebhookChannel(config.WebhookConfig)
		if err != nil {
			log.Printf("Warning: failed to create Webhook channel: %v", err)
		} else {
			err = webhookChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Webhook message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "webhook")
			})
			if err != nil {
				log.Printf("Warning: failed to start Webhook server: %v", err)
			} else {
				log.Println("Webhook server started")
				webhookChannel.RegisterToBus()
			}
		}
	}

	// 启动 XMPP Bot
	if config.XMPPConfig != nil && config.XMPPConfig.Enabled {
		xmppChannel, err := NewXMPPChannel(config.XMPPConfig)
		if err != nil {
			log.Printf("Warning: failed to create XMPP channel: %v", err)
		} else {
			err = xmppChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("XMPP message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "xmpp")
			})
			if err != nil {
				log.Printf("Warning: failed to start XMPP bot: %v", err)
			} else {
				log.Println("XMPP bot started")
				xmppChannel.RegisterToBus()
			}
		}
	}

	// 启动 Matrix Bot
	if config.MatrixConfig != nil && config.MatrixConfig.Enabled {
		matrixChannel, err := NewMatrixChannel(config.MatrixConfig)
		if err != nil {
			log.Printf("Warning: failed to create Matrix channel: %v", err)
		} else {
			err = matrixChannel.Start(func(chatID, senderID, content string, metadata map[string]interface{}) {
				log.Printf("Matrix message from %s: %s", senderID, content)
				GetBus().RegisterUserChannel(senderID, "matrix")
			})
			if err != nil {
				log.Printf("Warning: failed to start Matrix bot: %v", err)
			} else {
				log.Println("Matrix bot started")
				matrixChannel.RegisterToBus()
			}
		}
	}
}

func runDebugMode(prompt string, session *GlobalSession) {
	log.Println("[Debug Mode] Starting...")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmdChan := NewCmdChannel()
	history := []Message{{Role: "user", Content: prompt}}
	if globalTaskTracker != nil {
		globalTaskTracker.StartNewTask(prompt)
	}
	ok, taskID := session.TryStartTask()
	if !ok {
		log.Println("[Debug Mode] Another task already running.")
		os.Exit(1)
	}
	taskCtx := session.GetTaskCtx()
	newHistory, err := AgentLoop(taskCtx, cmdChan, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
	session.SetTaskRunning(false, taskID)
	if err != nil {
		log.Printf("[Debug Mode] Agent error: %v", err)
		os.Exit(1)
	}
	if globalTaskTracker != nil {
		globalTaskTracker.MarkCompleted()
	}
	if len(newHistory) > 0 {
		if content, ok := newHistory[len(newHistory)-1].Content.(string); ok && content != "" {
			fmt.Println("\n[Debug Mode] Final response:")
			fmt.Println(content)
		}
	}
	log.Println("[Debug Mode] Completed.")
}
