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
// 必须配置项：主模型的 API Key
// 如果主模型不存在或 API Key 为空，则需要配置
func NeedsSetup(config Config) bool {
        // 检查是否有主模型且其 APIKey 不为空
        if config.Models == nil || len(config.Models) == 0 {
                return true
        }
        // 查找默认模型
        var mainModel *ModelConfig
        for _, m := range config.Models {
                if m.IsDefault {
                        mainModel = m
                        break
                }
        }
        if mainModel == nil {
                // 没有默认模型，取第一个
                for _, m := range config.Models {
                        mainModel = m
                        break
                }
        }
        if mainModel == nil {
                return true
        }
        // Ollama 不需要 API Key
        if mainModel.APIType == "ollama" {
                return false
        }
        return mainModel.APIKey == ""
}

// RunConfigWizard 运行配置向导
// 通过终端交互式地收集必要的配置参数
func RunConfigWizard(existingConfig Config) ConfigWizardResult {
        reader := bufio.NewReader(os.Stdin)
        config := existingConfig

        // 确保 Models 已初始化
        if config.Models == nil {
                config.Models = make(map[string]*ModelConfig)
        }

        // 尝试获取现有主模型作为默认值
        var currentModel *ModelConfig
        for _, m := range config.Models {
                if m.IsDefault {
                        currentModel = m
                        break
                }
        }
        if currentModel == nil && len(config.Models) > 0 {
                // 取第一个作为临时主模型
                for _, m := range config.Models {
                        currentModel = m
                        break
                }
        }
        if currentModel == nil {
                // 创建新的空模型配置
                currentModel = &ModelConfig{
                        ModelBase: ModelBase{
                                Name:        DEFAULT_MODEL_ID,
                                APIType:     DEFAULT_API_TYPE,
                                Temperature: 0.7,
                                MaxTokens:   4096,
                                Stream:      true,
                                Thinking:    true,
                        },
                }
        }

        fmt.Println()
        fmt.Println("╔══════════════════════════════════════════════════════════════╗")
        fmt.Println("  欢迎使用 GhostClaw - 首次启动配置向导")
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

        currentType := currentModel.APIType
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
                        // 保留原值
                        if currentModel.APIType == "" {
                                currentModel.APIType = currentType
                        }
                        break
                }

                switch input {
                case "1":
                        currentModel.APIType = "openai"
                case "2":
                        currentModel.APIType = "anthropic"
                case "3":
                        currentModel.APIType = "ollama"
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
        switch currentModel.APIType {
        case "openai":
                defaultURL = OPENAI_BASE_URL
        case "anthropic":
                defaultURL = ANTHROPIC_BASE_URL
        case "ollama":
                defaultURL = OLLAMA_BASE_URL
        }

        currentURL := currentModel.BaseURL
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
                currentModel.BaseURL = input
        } else if currentModel.BaseURL == "" {
                currentModel.BaseURL = defaultURL
        }

        // 3. API Key
        fmt.Println()
        fmt.Println("【步骤 3/4】配置 API 密钥 (API Key)")

        if currentModel.APIType == "ollama" {
                fmt.Println("  Ollama 模式无需 API Key，直接回车跳过。")
                fmt.Printf("API Key (当前: %s): ", maskAPIKey(currentModel.APIKey))
                reader.ReadString('\n') // 消耗输入
        } else {
                for {
                        currentKey := currentModel.APIKey
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
                                currentModel.APIKey = input
                                break
                        } else if currentModel.APIKey != "" {
                                break // 保留原值
                        }
                        fmt.Println("  API Key 为必填项，请输入或按 Ctrl+C 退出。")
                }
        }

        // 4. Model ID
        fmt.Println()
        fmt.Println("【步骤 4/4】配置模型标识 (Model ID)")

        currentModelID := currentModel.Model
        if currentModelID == "" {
                currentModelID = DEFAULT_MODEL_ID
        }

        fmt.Printf("模型名称 (当前: %s): ", currentModelID)
        input, err = reader.ReadString('\n')
        if err != nil {
                fmt.Println("\n配置已取消。")
                return ConfigWizardResult{Config: config, IsCompleted: false}
        }

        input = strings.TrimSpace(input)
        if input != "" {
                currentModel.Model = input
                // 如果模型名称变化，更新 Name 字段（保持一致）
                currentModel.Name = input
        } else if currentModel.Model == "" {
                currentModel.Model = DEFAULT_MODEL_ID
                currentModel.Name = DEFAULT_MODEL_ID
        }

        // 确保 Name 字段与 Model 字段一致（若用户未显式设置）
        if currentModel.Name == "" {
                currentModel.Name = currentModel.Model
        }

        // 设置默认值
        if currentModel.Temperature == 0 {
                currentModel.Temperature = 0.7
        }
        if currentModel.MaxTokens == 0 {
                currentModel.MaxTokens = 4096
        }
        currentModel.Stream = true
        currentModel.Thinking = true
        currentModel.Description = "默认模型（向导配置）"
        currentModel.IsDefault = true

        // 检查是否需要删除旧模型（当模型名称改变时）
        oldModelName := ""
        for name, m := range config.Models {
                if m == currentModel {
                        oldModelName = name
                        break
                }
        }
        
        // 如果模型名称改变了，删除旧模型
        if oldModelName != "" && oldModelName != currentModel.Name {
                delete(config.Models, oldModelName)
        }
        
        // 将当前模型存入 Models 映射，并清除其他模型的默认标记
        for name, m := range config.Models {
                if name != currentModel.Name {
                        m.IsDefault = false
                }
        }
        config.Models[currentModel.Name] = currentModel

        // 设置其他默认值
        if config.HTTPServer.Listen == "" {
                config.HTTPServer.Listen = "0.0.0.0:10086"
        }
        if config.DefaultRole == "" {
                config.DefaultRole = "coder"
        }
        if config.MaxRequestSizeBytes == 0 {
                config.MaxRequestSizeBytes = 256 * 1024
        }

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
        fmt.Println("配置完成！正在启动 GhostClaw...")
        fmt.Println("═══════════════════════════════════════════════════════════════")
        fmt.Println()

        return ConfigWizardResult{Config: config, IsCompleted: true}
}

// saveConfigWizardResult 保存配置向导结果
// 确保不写入 APIConfig 段，所有模型配置存储在 Models 中
func saveConfigWizardResult(config *Config) error {
        // 使用 ConfigManager 保存（如果已初始化）
        if globalConfigManager != nil {
                return globalConfigManager.ReplaceConfig(*config)
        }

        // Fallback: 直接写入文件（向导在 ConfigManager 初始化之前调用的情况）
        execPath, err := os.Executable()
        if err != nil {
                return err
        }
        execDir := filepath.Dir(execPath)
        configPath := filepath.Join(execDir, CONFIG_FILE)

        // 确保不包含 APIConfig 字段（结构体中已移除，但防止意外）
        // 直接序列化即可
        normalizeConfigForSave(config)
        data, err := toon.Marshal(*config)
        if err != nil {
                return err
        }

        return os.WriteFile(configPath, data, 0644)
}

