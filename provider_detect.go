package main

import (
	"fmt"
	"net/url"
	"strings"
)

// ============================================================
// 提供商自动检测
// 根据模型名/API Key/Base URL 自动匹配提供商配置
// ============================================================

// ProviderInfo 提供商信息
type ProviderInfo struct {
	Name       string // 提供商名称
	APIType    string // API 类型 (openai, anthropic, gemini 等)
	BaseURL    string // 默认 Base URL
	ModelHint  string // 模型名前缀提示
	KeyPrefix  string // API Key 前缀
	DocURL     string // 文档链接
}

// 已知提供商配置
var knownProviders = []ProviderInfo{
	{
		Name:      "OpenAI",
		APIType:   "openai",
		BaseURL:   "https://api.openai.com/v1",
		ModelHint: "gpt-",
		KeyPrefix: "sk-",
		DocURL:    "https://platform.openai.com/docs",
	},
	{
		Name:      "Anthropic",
		APIType:   "anthropic",
		BaseURL:   "https://api.anthropic.com/v1",
		ModelHint: "claude-",
		KeyPrefix: "sk-ant-",
		DocURL:    "https://docs.anthropic.com",
	},
	{
		Name:      "DeepSeek",
		APIType:   "openai",
		BaseURL:   "https://api.deepseek.com/v1",
		ModelHint: "deepseek-",
		KeyPrefix: "sk-",
		DocURL:    "https://platform.deepseek.com/docs",
	},
	{
		Name:      "Google Gemini",
		APIType:   "gemini",
		BaseURL:   "https://generativelanguage.googleapis.com/v1",
		ModelHint: "gemini-",
		KeyPrefix: "AIza",
		DocURL:    "https://ai.google.dev/docs",
	},
	{
		Name:      "Mistral",
		APIType:   "openai",
		BaseURL:   "https://api.mistral.ai/v1",
		ModelHint: "mistral-",
		KeyPrefix: "",
		DocURL:    "https://docs.mistral.ai",
	},
	{
		Name:      "Groq",
		APIType:   "openai",
		BaseURL:   "https://api.groq.com/openai/v1",
		ModelHint: "llama-",
		KeyPrefix: "gsk_",
		DocURL:    "https://console.groq.com/docs",
	},
	{
		Name:      "Groq Mixtral",
		APIType:   "openai",
		BaseURL:   "https://api.groq.com/openai/v1",
		ModelHint: "mixtral-",
		KeyPrefix: "gsk_",
		DocURL:    "https://console.groq.com/docs",
	},
	{
		Name:      "OpenRouter",
		APIType:   "openai",
		BaseURL:   "https://openrouter.ai/api/v1",
		ModelHint: "",
		KeyPrefix: "sk-or-",
		DocURL:    "https://openrouter.ai/docs",
	},
	{
		Name:      "Together AI",
		APIType:   "openai",
		BaseURL:   "https://api.together.xyz/v1",
		ModelHint: "",
		KeyPrefix: "",
		DocURL:    "https://docs.together.ai",
	},
	{
		Name:      "Replicate",
		APIType:   "openai",
		BaseURL:   "https://api.replicate.com/v1",
		ModelHint: "",
		KeyPrefix: "r8_",
		DocURL:    "https://replicate.com/docs",
	},
	{
		Name:      "Moonshot (月之暗面)",
		APIType:   "openai",
		BaseURL:   "https://api.moonshot.cn/v1",
		ModelHint: "moonshot-",
		KeyPrefix: "sk-",
		DocURL:    "https://platform.moonshot.cn/docs",
	},
	{
		Name:      "Qwen (通义千问)",
		APIType:   "openai",
		BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
		ModelHint: "qwen-",
		KeyPrefix: "sk-",
		DocURL:    "https://help.aliyun.com/zh/dashscope",
	},
	{
		Name:      "GLM (智谱)",
		APIType:   "openai",
		BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
		ModelHint: "glm-",
		KeyPrefix: "",
		DocURL:    "https://open.bigmodel.cn/dev/api",
	},
	{
		Name:      "Yi (零一万物)",
		APIType:   "openai",
		BaseURL:   "https://api.lingyiwanwu.com/v1",
		ModelHint: "yi-",
		KeyPrefix: "",
		DocURL:    "https://platform.lingyiwanwu.com/docs",
	},
	{
		Name:      "Baichuan (百川)",
		APIType:   "openai",
		BaseURL:   "https://api.baichuan-ai.com/v1",
		ModelHint: "Baichuan-",
		KeyPrefix: "",
		DocURL:    "https://platform.baichuan-ai.com/docs",
	},
	{
		Name:      "MiniMax",
		APIType:   "openai",
		BaseURL:   "https://api.minimax.chat/v1",
		ModelHint: "",
		KeyPrefix: "",
		DocURL:    "https://api.minimax.chat/document/guides",
	},
	{
		Name:      "SiliconFlow",
		APIType:   "openai",
		BaseURL:   "https://api.siliconflow.cn/v1",
		ModelHint: "",
		KeyPrefix: "sk-",
		DocURL:    "https://siliconflow.cn/docs",
	},
	{
		Name:      "Ollama (本地)",
		APIType:   "ollama",
		BaseURL:   "http://localhost:11434/api",
		ModelHint: "llama",
		KeyPrefix: "",
		DocURL:    "https://ollama.ai/docs",
	},
	{
		Name:      "vLLM (本地)",
		APIType:   "openai",
		BaseURL:   "http://localhost:8000/v1",
		ModelHint: "",
		KeyPrefix: "",
		DocURL:    "https://vllm.readthedocs.io",
	},
	{
		Name:      "LM Studio (本地)",
		APIType:   "openai",
		BaseURL:   "http://localhost:1234/v1",
		ModelHint: "",
		KeyPrefix: "",
		DocURL:    "https://lmstudio.ai/docs",
	},
}

// DetectProvider 自动检测提供商
// 根据模型名、API Key、Base URL 综合判断
func DetectProvider(model, apiKey, baseURL string) *ProviderInfo {
	// 1. 先通过 Base URL 检测（最准确）
	if baseURL != "" {
		if provider := detectByBaseURL(baseURL); provider != nil {
			return provider
		}
	}

	// 2. 通过 API Key 前缀检测
	if apiKey != "" {
		if provider := detectByAPIKey(apiKey); provider != nil {
			return provider
		}
	}

	// 3. 通过模型名前缀检测
	if model != "" {
		if provider := detectByModel(model); provider != nil {
			return provider
		}
	}

	return nil
}

// detectByBaseURL 通过 Base URL 检测提供商
func detectByBaseURL(baseURL string) *ProviderInfo {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	host := strings.ToLower(parsedURL.Host)

	for i := range knownProviders {
		provider := &knownProviders[i]
		providerURL, err := url.Parse(provider.BaseURL)
		if err != nil {
			continue
		}

		// 检查主机名匹配
		if strings.Contains(host, strings.ToLower(providerURL.Host)) {
			return provider
		}

		// 特殊处理
		if strings.Contains(host, "deepseek") {
			return &ProviderInfo{
				Name:      "DeepSeek",
				APIType:   "openai",
				BaseURL:   "https://api.deepseek.com/v1",
				ModelHint: "deepseek-",
			}
		}
		if strings.Contains(host, "anthropic") {
			return &ProviderInfo{
				Name:      "Anthropic",
				APIType:   "anthropic",
				BaseURL:   "https://api.anthropic.com/v1",
				ModelHint: "claude-",
			}
		}
		if strings.Contains(host, "moonshot") {
			return &ProviderInfo{
				Name:      "Moonshot",
				APIType:   "openai",
				BaseURL:   "https://api.moonshot.cn/v1",
				ModelHint: "moonshot-",
			}
		}
		if strings.Contains(host, "dashscope") || strings.Contains(host, "aliyuncs") {
			return &ProviderInfo{
				Name:      "Qwen",
				APIType:   "openai",
				BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
				ModelHint: "qwen-",
			}
		}
		if strings.Contains(host, "bigmodel") {
			return &ProviderInfo{
				Name:      "GLM",
				APIType:   "openai",
				BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
				ModelHint: "glm-",
			}
		}
		if strings.Contains(host, "localhost") || strings.Contains(host, "127.0.0.1") {
			// 本地服务
			if strings.Contains(baseURL, "11434") {
				return &ProviderInfo{
					Name:      "Ollama",
					APIType:   "ollama",
					BaseURL:   "http://localhost:11434/api",
				}
			}
			if strings.Contains(baseURL, "1234") {
				return &ProviderInfo{
					Name:      "LM Studio",
					APIType:   "openai",
					BaseURL:   "http://localhost:1234/v1",
				}
			}
			if strings.Contains(baseURL, "8000") {
				return &ProviderInfo{
					Name:      "vLLM",
					APIType:   "openai",
					BaseURL:   "http://localhost:8000/v1",
				}
			}
		}
	}

	return nil
}

// detectByAPIKey 通过 API Key 检测提供商
func detectByAPIKey(apiKey string) *ProviderInfo {
	key := strings.ToLower(apiKey)

	for i := range knownProviders {
		provider := &knownProviders[i]
		if provider.KeyPrefix != "" && strings.HasPrefix(key, strings.ToLower(provider.KeyPrefix)) {
			return provider
		}
	}

	// 特殊检测
	if strings.HasPrefix(key, "sk-ant-") {
		return &ProviderInfo{
			Name:      "Anthropic",
			APIType:   "anthropic",
			BaseURL:   "https://api.anthropic.com/v1",
			KeyPrefix: "sk-ant-",
		}
	}
	if strings.HasPrefix(key, "gsk_") {
		return &ProviderInfo{
			Name:      "Groq",
			APIType:   "openai",
			BaseURL:   "https://api.groq.com/openai/v1",
			KeyPrefix: "gsk_",
		}
	}
	if strings.HasPrefix(key, "sk-or-") {
		return &ProviderInfo{
			Name:      "OpenRouter",
			APIType:   "openai",
			BaseURL:   "https://openrouter.ai/api/v1",
			KeyPrefix: "sk-or-",
		}
	}
	if strings.HasPrefix(key, "aiza") {
		return &ProviderInfo{
			Name:      "Google Gemini",
			APIType:   "gemini",
			BaseURL:   "https://generativelanguage.googleapis.com/v1",
			KeyPrefix: "AIza",
		}
	}
	if strings.HasPrefix(key, "r8_") {
		return &ProviderInfo{
			Name:      "Replicate",
			APIType:   "openai",
			BaseURL:   "https://api.replicate.com/v1",
			KeyPrefix: "r8_",
		}
	}

	return nil
}

// detectByModel 通过模型名检测提供商
func detectByModel(model string) *ProviderInfo {
	modelLower := strings.ToLower(model)

	for i := range knownProviders {
		provider := &knownProviders[i]
		if provider.ModelHint != "" && strings.HasPrefix(modelLower, strings.ToLower(provider.ModelHint)) {
			return provider
		}
	}

	// 特殊模型检测
	if strings.HasPrefix(modelLower, "gpt-") || strings.HasPrefix(modelLower, "gpt4") || strings.HasPrefix(modelLower, "gpt4o") {
		return &ProviderInfo{
			Name:      "OpenAI",
			APIType:   "openai",
			BaseURL:   "https://api.openai.com/v1",
			ModelHint: "gpt-",
		}
	}
	if strings.HasPrefix(modelLower, "claude-") {
		return &ProviderInfo{
			Name:      "Anthropic",
			APIType:   "anthropic",
			BaseURL:   "https://api.anthropic.com/v1",
			ModelHint: "claude-",
		}
	}
	if strings.HasPrefix(modelLower, "gemini-") {
		return &ProviderInfo{
			Name:      "Google Gemini",
			APIType:   "gemini",
			BaseURL:   "https://generativelanguage.googleapis.com/v1",
			ModelHint: "gemini-",
		}
	}
	if strings.HasPrefix(modelLower, "deepseek-") {
		return &ProviderInfo{
			Name:      "DeepSeek",
			APIType:   "openai",
			BaseURL:   "https://api.deepseek.com/v1",
			ModelHint: "deepseek-",
		}
	}
	if strings.HasPrefix(modelLower, "llama-") || strings.HasPrefix(modelLower, "mixtral-") {
		// 可能是 Groq、Together、或本地
		return &ProviderInfo{
			Name:      "Llama/Mixtral",
			APIType:   "openai",
			BaseURL:   "", // 需要用户指定
			ModelHint: "llama-",
		}
	}
	if strings.HasPrefix(modelLower, "mistral-") {
		return &ProviderInfo{
			Name:      "Mistral",
			APIType:   "openai",
			BaseURL:   "https://api.mistral.ai/v1",
			ModelHint: "mistral-",
		}
	}
	if strings.HasPrefix(modelLower, "qwen-") {
		return &ProviderInfo{
			Name:      "Qwen",
			APIType:   "openai",
			BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
			ModelHint: "qwen-",
		}
	}
	if strings.HasPrefix(modelLower, "glm-") {
		return &ProviderInfo{
			Name:      "GLM",
			APIType:   "openai",
			BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
			ModelHint: "glm-",
		}
	}
	if strings.HasPrefix(modelLower, "moonshot-") {
		return &ProviderInfo{
			Name:      "Moonshot",
			APIType:   "openai",
			BaseURL:   "https://api.moonshot.cn/v1",
			ModelHint: "moonshot-",
		}
	}
	if strings.HasPrefix(modelLower, "yi-") {
		return &ProviderInfo{
			Name:      "Yi",
			APIType:   "openai",
			BaseURL:   "https://api.lingyiwanwu.com/v1",
			ModelHint: "yi-",
		}
	}
	if strings.HasPrefix(modelLower, "baichuan-") {
		return &ProviderInfo{
			Name:      "Baichuan",
			APIType:   "openai",
			BaseURL:   "https://api.baichuan-ai.com/v1",
			ModelHint: "Baichuan-",
		}
	}

	return nil
}

// AutoConfigureProvider 自动配置提供商
// 返回推荐的 APIType 和 BaseURL
func AutoConfigureProvider(model, apiKey, baseURL string) (apiType string, recommendedBaseURL string, provider *ProviderInfo) {
	provider = DetectProvider(model, apiKey, baseURL)

	if provider != nil {
		apiType = provider.APIType
		recommendedBaseURL = provider.BaseURL

		// 如果用户已提供 Base URL，保留用户配置
		if baseURL != "" {
			recommendedBaseURL = baseURL
		}
	} else {
		// 无法检测，使用默认配置
		apiType = "openai"
		if baseURL == "" {
			recommendedBaseURL = "https://api.openai.com/v1"
		} else {
			recommendedBaseURL = baseURL
		}
	}

	return apiType, recommendedBaseURL, provider
}

// GetProviderInfo 获取提供商信息字符串
func GetProviderInfo(model, apiKey, baseURL string) string {
	_, _, provider := AutoConfigureProvider(model, apiKey, baseURL)

	if provider == nil {
		return "Unknown provider"
	}

	info := fmt.Sprintf("Provider: %s\nAPI Type: %s\nDefault Base URL: %s",
		provider.Name, provider.APIType, provider.BaseURL)

	if provider.DocURL != "" {
		info += fmt.Sprintf("\nDocumentation: %s", provider.DocURL)
	}

	return info
}

// ListProviders 列出所有已知提供商
func ListProviders() []ProviderInfo {
	return knownProviders
}
