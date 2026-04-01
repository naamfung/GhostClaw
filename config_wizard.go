package main

import (
        "bufio"
        "fmt"
        "os"
        "path/filepath"
        "strings"

        "github.com/toon-format/toon-go"
)

// ConfigWizardResult 配置向导结果
type ConfigWizardResult struct {
        Config      Config
        IsCompleted bool // 是否完成配置（用户主动退出则为 false）
}

// NeedsSetup 检查是否需要配置向导
// 必须配置项：API Key
// 如果 API Key 为空，则需要配置
func NeedsSetup(config Config) bool {
        return config.APIConfig.APIKey == ""
}

// RunConfigWizard 运行配置向导
// 通过终端交互式地收集必要的配置参数
func RunConfigWizard(existingConfig Config) ConfigWizardResult {
        reader := bufio.NewReader(os.Stdin)
        config := existingConfig

        fmt.Println()
        fmt.Println("╔══════════════════════════════════════════════════════════════╗")
        fmt.Println("║          欢迎使用 GarClaw - 首次启动配置向导                  ║")
        fmt.Println("╚══════════════════════════════════════════════════════════════╝")
        fmt.Println()
        fmt.Println("请配置必要的模型连接参数。按 Ctrl+C 可随时退出。")
        fmt.Println("已有值会显示在方括号中，直接回车保留原值。")
        fmt.Println()

        // 1. API 类型
        fmt.Println("【步骤 1/4】选择 API 类型")
        fmt.Println("  1. openai     - OpenAI / 兼容 API (如 DeepSeek, 通义千问等)")
        fmt.Println("  2. anthropic  - Anthropic Claude API")
        fmt.Println("  3. ollama     - Ollama 本地模型")
        fmt.Println()

        currentType := config.APIConfig.APIType
        if currentType == "" {
                currentType = DEFAULT_API_TYPE
        }

        for {
                fmt.Printf("请选择 [1-3] (当前: %s): ", currentType)
                input, err := reader.ReadString('\n')
                if err != nil {
                        fmt.Println("\n配置已取消。")
                        return ConfigWizardResult{Config: config, IsCompleted: false}
                }

                input = strings.TrimSpace(input)
                if input == "" {
                        // 保留原值，如果原值为空则设置默认值
                        if config.APIConfig.APIType == "" {
                                config.APIConfig.APIType = currentType
                        }
                        break
                }

                switch input {
                case "1":
                        config.APIConfig.APIType = "openai"
                case "2":
                        config.APIConfig.APIType = "anthropic"
                case "3":
                        config.APIConfig.APIType = "ollama"
                default:
                        fmt.Println("  无效选择，请输入 1、2 或 3")
                        continue
                }
                break
        }

        // 2. Base URL
        fmt.Println()
        fmt.Println("【步骤 2/4】配置 API 基础地址 (Base URL)")

        defaultURL := ""
        switch config.APIConfig.APIType {
        case "openai":
                defaultURL = OPENAI_BASE_URL
        case "anthropic":
                defaultURL = ANTHROPIC_BASE_URL
        case "ollama":
                defaultURL = OLLAMA_BASE_URL
        }

        currentURL := config.APIConfig.BaseURL
        if currentURL == "" {
                currentURL = defaultURL
        }

        fmt.Printf("请输入 Base URL (当前: %s): ", currentURL)
        input, err := reader.ReadString('\n')
        if err != nil {
                fmt.Println("\n配置已取消。")
                return ConfigWizardResult{Config: config, IsCompleted: false}
        }

        input = strings.TrimSpace(input)
        if input != "" {
                config.APIConfig.BaseURL = input
        } else if config.APIConfig.BaseURL == "" {
                config.APIConfig.BaseURL = defaultURL
        }

        // 3. API Key
        fmt.Println()
        fmt.Println("【步骤 3/4】配置 API 密钥 (API Key)")

        if config.APIConfig.APIType == "ollama" {
                fmt.Println("  Ollama 模式无需 API Key，直接回车跳过。")
                fmt.Printf("API Key (当前: %s): ", maskAPIKey(config.APIConfig.APIKey))
                reader.ReadString('\n') // 消耗输入
        } else {
                for {
                        currentKey := config.APIConfig.APIKey
                        if currentKey != "" {
                                fmt.Printf("API Key (当前: %s): ", maskAPIKey(currentKey))
                        } else {
                                fmt.Print("API Key (必填): ")
                        }

                        input, err := reader.ReadString('\n')
                        if err != nil {
                                fmt.Println("\n配置已取消。")
                                return ConfigWizardResult{Config: config, IsCompleted: false}
                        }

                        input = strings.TrimSpace(input)
                        if input != "" {
                                config.APIConfig.APIKey = input
                                break
                        } else if config.APIConfig.APIKey != "" {
                                break // 保留原值
                        }
                        fmt.Println("  API Key 为必填项，请输入或按 Ctrl+C 退出。")
                }
        }

        // 4. Model ID
        fmt.Println()
        fmt.Println("【步骤 4/4】配置模型标识 (Model ID)")

        currentModel := config.APIConfig.Model
        if currentModel == "" {
                currentModel = DEFAULT_MODEL_ID
        }

        fmt.Printf("模型名称 (当前: %s): ", currentModel)
        input, err = reader.ReadString('\n')
        if err != nil {
                fmt.Println("\n配置已取消。")
                return ConfigWizardResult{Config: config, IsCompleted: false}
        }

        input = strings.TrimSpace(input)
        if input != "" {
                config.APIConfig.Model = input
        } else if config.APIConfig.Model == "" {
                config.APIConfig.Model = DEFAULT_MODEL_ID
        }

        // 设置默认值
        if config.APIConfig.Temperature == 0 {
                config.APIConfig.Temperature = 0.7
        }
        if config.APIConfig.MaxTokens == 0 {
                config.APIConfig.MaxTokens = 4096
        }
        config.APIConfig.Stream = true
        config.APIConfig.Thinking = true

        // 保存配置
        fmt.Println()
        fmt.Println("正在保存配置...")

        if err := saveConfigWizardResult(&config); err != nil {
                fmt.Printf("警告: 保存配置失败: %v\n", err)
        } else {
                fmt.Println("✓ 配置已保存")
        }

        fmt.Println()
        fmt.Println("═══════════════════════════════════════════════════════════════")
        fmt.Println("配置完成！正在启动 GarClaw...")
        fmt.Println("═══════════════════════════════════════════════════════════════")
        fmt.Println()

        return ConfigWizardResult{Config: config, IsCompleted: true}
}

// saveConfigWizardResult 保存配置向导结果
// 返回更新后的配置（包含默认值）
func saveConfigWizardResult(config *Config) error {
        execPath, err := os.Executable()
        if err != nil {
                return err
        }
        execDir := filepath.Dir(execPath)
        configPath := filepath.Join(execDir, CONFIG_FILE)

        // 设置默认值
        if config.HTTPServer.Listen == "" {
                config.HTTPServer.Listen = "0.0.0.0:10086"
        }
        // 设置默认角色
        if config.DefaultRole == "" {
                config.DefaultRole = "coder"
        }

        data, err := toon.Marshal(*config)
        if err != nil {
                return err
        }

        return os.WriteFile(configPath, data, 0644)
}
