package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/toon-format/toon-go"
)

// ============================================================
// MCP 客户端配置加载
// ============================================================

// MCPClientConfigs MCP 客户端配置列表
type MCPClientConfigs struct {
	Servers map[string]MCPClientConfig `json:"servers"`
}

// LoadMCPClientConfigs 加载 MCP 客户端配置
func LoadMCPClientConfigs(execDir string) (*MCPClientConfigs, error) {
	configPath := filepath.Join(execDir, "mcp_servers.toon")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，返回空配置
			return &MCPClientConfigs{Servers: make(map[string]MCPClientConfig)}, nil
		}
		return nil, fmt.Errorf("failed to read MCP config: %w", err)
	}

	// 解析 TOON 格式
	parsed, err := toon.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}

	configs := &MCPClientConfigs{
		Servers: make(map[string]MCPClientConfig),
	}

	// 解析服务器配置
	if servers, ok := parsed.(map[string]interface{})["servers"]; ok {
		if sm, ok := servers.(map[string]interface{}); ok {
			for name, serverCfg := range sm {
				if m, ok := serverCfg.(map[string]interface{}); ok {
					cfg := parseMCPClientConfig(m)
					cfg.Name = name
					configs.Servers[name] = cfg
				}
			}
		}
	}

	return configs, nil
}

// parseMCPClientConfig 解析单个 MCP 客户端配置
func parseMCPClientConfig(m map[string]interface{}) MCPClientConfig {
	cfg := MCPClientConfig{}

	// 传输类型
	cfg.Type = getStringVal(m, "type")
	cfg.Command = getStringVal(m, "command")
	cfg.URL = getStringVal(m, "url")

	// 解析 args 数组
	if args, ok := m["args"].([]interface{}); ok {
		for _, arg := range args {
			if s, ok := arg.(string); ok {
				cfg.Args = append(cfg.Args, s)
			}
		}
	}

	// 解析 env
	if env, ok := m["env"].(map[string]interface{}); ok {
		cfg.Env = make(map[string]string)
		for k, v := range env {
			if s, ok := v.(string); ok {
				cfg.Env[k] = s
			}
		}
	}

	// 解析 headers
	if headers, ok := m["headers"].(map[string]interface{}); ok {
		cfg.Headers = make(map[string]string)
		for k, v := range headers {
			if s, ok := v.(string); ok {
				cfg.Headers[k] = s
			}
		}
	}

	// 解析 enabled_tools
	if tools, ok := m["enabled_tools"].([]interface{}); ok {
		for _, t := range tools {
			if s, ok := t.(string); ok {
				cfg.EnabledTools = append(cfg.EnabledTools, s)
			}
		}
	}

	// 超时设置
	if v, ok := m["tool_timeout"]; ok {
		switch val := v.(type) {
		case int:
			cfg.ToolTimeout = val
		case int64:
			cfg.ToolTimeout = int(val)
		case float64:
			cfg.ToolTimeout = int(val)
		}
	}

	return cfg
}

// getStringVal 从 map 中获取字符串值
func getStringVal(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// ============================================================
// MCP 客户端初始化
// ============================================================

// InitMCPClients 初始化 MCP 客户端
func InitMCPClients(execDir string) error {
	initMCPClientManager()

	configs, err := LoadMCPClientConfigs(execDir)
	if err != nil {
		return fmt.Errorf("failed to load MCP client configs: %w", err)
	}

	if len(configs.Servers) == 0 {
		log.Println("[MCP Client] No MCP servers configured")
		return nil
	}

	// 添加所有客户端
	for name, cfg := range configs.Servers {
		if err := globalMCPClientManager.AddClient(name, &cfg); err != nil {
			log.Printf("[MCP Client] Failed to add client %s: %v", name, err)
		}
	}

	// 连接所有客户端
	ctx := context.Background()
	if err := globalMCPClientManager.ConnectAll(ctx); err != nil {
		log.Printf("[MCP Client] Some clients failed to connect: %v", err)
	}

	return nil
}

// GetMCPClientManager 获取全局 MCP 客户端管理器
func GetMCPClientManager() *MCPClientManager {
	if globalMCPClientManager == nil {
		initMCPClientManager()
	}
	return globalMCPClientManager
}

// GetMCPToolDefinitions 获取所有 MCP 工具定义
func GetMCPToolDefinitions() []map[string]interface{} {
	if globalMCPClientManager == nil {
		return nil
	}
	return globalMCPClientManager.GetAllTools()
}

// CallMCPTool 调用 MCP 工具
func CallMCPTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if globalMCPClientManager == nil {
		return "", fmt.Errorf("MCP client manager not initialized")
	}
	return globalMCPClientManager.CallTool(ctx, name, args)
}
