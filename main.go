package main

import (
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

    globalCronManager        *CronManager
    globalTaskTracker        *TaskTracker
    globalRoleManager        *RoleManager
    globalActorManager       *ActorManager
    globalStage              *Stage
    globalSkillManager       *SkillManager
    globalTaskManager        *TaskManager
    globalMCPServer          *MCPServer
    globalUnifiedMemory      *UnifiedMemory
    globalProfileLoader      *ProfileLoader
    globalGroupChatConfig    *GroupChatConfig
    globalPluginManager      *PluginManager
    globalMCPClientManager   *MCPClientManager
    globalSessionPersist     *SessionPersistManager
    globalHeartbeatService   *HeartbeatService
    globalSubagentManager    *SubagentManager
    globalAuthManager        *AuthManager
    globalAuthConfig         AuthConfig
    globalCancel             context.CancelFunc
    globalExecDir            string
    globalUploadDir            string
)

func init() {
	globalUploadDir = filepath.Join(globalExecDir, "uploads")
}

func main() {
	promptFlag := flag.String("p", "", "调试模式：直接传入提示词")
	promptFlagLong := flag.String("prompt", "", "调试模式：直接传入提示词（长格式）")
	debugFlag := flag.Bool("debug", false, "启用调试输出")
	flag.Parse()
	prompt := *promptFlag
	if prompt == "" {
		prompt = *promptFlagLong
	}
	if *debugFlag {
		IsDebug = true
	}
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Warning: cannot get executable path: %v", err)
		execPath = "."
	}
	globalExecDir = filepath.Dir(execPath)
	config, err := loadConfig()
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
	}
	if NeedsSetup(config) {
		result := RunConfigWizard(config)
		if !result.IsCompleted {
			fmt.Println("配置未完成，程序退出。")
			os.Exit(0)
		}
		config = result.Config
	}
	// 初始化全局变量（省略，与原代码相同）
	// ...
	SetSecurityConfig(config.Security)
	// 初始化数据库
	if err := InitDB(globalExecDir); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}
	// 初始化其他组件...
	// ...
	session := GetGlobalSession()
	if prompt != "" {
		runDebugMode(prompt, session)
		return
	}
	rl, err := readline.New("GarClaw /> ")
	if err != nil {
		log.Fatalf("Failed to create readline: %v", err)
	}
	defer rl.Close()
	cmdChan := NewCmdChannel()
	var history []Message
	_, cancel := context.WithCancel(context.Background())
	globalCancel = cancel
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		session.autoSaveHistory()
		rl.Close()
	}()
	for {
		line, err := rl.Readline()
		if err != nil {
			if err == io.EOF || errors.Is(err, readline.ErrInterrupt) {
				break
			}
			fmt.Printf("Readline error: %v\n", err)
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
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
				fmt.Println("Exiting...")
				session.autoSaveHistory()
				os.Exit(0)
			}) {
			continue
		}
		history = append(history, Message{Role: "user", Content: line})
		if globalTaskTracker != nil {
			globalTaskTracker.StartNewTask(line)
		}
		ok, taskID := session.TryStartTask()
		if !ok {
			fmt.Println("已有任务在执行中，请使用 /stop 取消后再试")
			continue
		}
		taskCtx := session.GetTaskCtx()
		newHistory, err := AgentLoop(taskCtx, cmdChan, history, apiType, baseURL, apiKey, modelID, temperature, maxTokens, stream, thinking)
		session.SetTaskRunning(false, taskID)
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
	session.autoSaveHistory()
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
