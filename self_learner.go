package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// SelfLearner LLM 驅動的自學習器。
// 在任務完成後調用 LLM 進行自省，從對話中提取可復用的經驗教訓，
// 並自動保存為記憶，形成真正的閉環自學習。
type SelfLearner struct {
	mu             sync.Mutex
	lastReflection time.Time
	minInterval    time.Duration // 最小自省間隔
}

var globalSelfLearner = &SelfLearner{
	minInterval: 10 * time.Minute,
}

// Reflect 在任務完成後進行 LLM 自省。
func (sl *SelfLearner) Reflect(ctx context.Context, taskDesc string, messages []Message) {
	sl.mu.Lock()
	if time.Since(sl.lastReflection) < sl.minInterval {
		sl.mu.Unlock()
		return
	}
	sl.lastReflection = time.Now()
	sl.mu.Unlock()

	prompt := sl.buildReflectionPrompt(taskDesc, messages)
	if prompt == "" {
		return
	}

	// 使用帶超時的 context 防止 goroutine 洩漏
	reflectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go func() {
		defer cancel()
		result, err := sl.callLLM(reflectCtx, prompt)
		if err != nil {
			log.Printf("[SelfLearner] Reflection LLM call failed: %v", err)
			return
		}
		sl.processReflectionResult(result)
	}()
}

// buildReflectionPrompt 從對話歷史中提取最近消息構建自省 prompt
func (sl *SelfLearner) buildReflectionPrompt(taskDesc string, messages []Message) string {
	var recent []string
	count := 0
	for i := len(messages) - 1; i >= 0 && count < 10; i-- {
		msg := messages[i]
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content, ok := msg.Content.(string)
		if !ok || content == "" {
			continue
		}
		recent = append([]string{fmt.Sprintf("[%s] %s", msg.Role, TruncateRunes(content, 300))}, recent...)
		count++
	}

	if len(recent) == 0 {
		return ""
	}

	if taskDesc == "" {
		taskDesc = "未指定任务的对话"
	}

	return fmt.Sprintf("## 任务描述\n%s\n\n## 最近对话\n%s", taskDesc, strings.Join(recent, "\n"))
}

// callLLM 調用 LLM（復用現有 sendRequest 基礎設施）
func (sl *SelfLearner) callLLM(ctx context.Context, userPrompt string) (string, error) {
	messages := []Message{
		{Role: "system", Content: reflectionSystemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// 使用與主聊天相同的 apiType/baseURL/apiKey/modelID（全局變量）
	reqBaseURL := baseURL
	if reqBaseURL == "" {
		if apiType == "anthropic" {
			reqBaseURL = ANTHROPIC_BASE_URL
		} else {
			reqBaseURL = OPENAI_BASE_URL
		}
	}

	data, endpoint, _, err := prepareRequestData(messages, apiType, reqBaseURL, modelID, 0, 200, false, false, nil)
	if err != nil {
		return "", fmt.Errorf("prepare request: %w", err)
	}

	resp, err := sendRequest(ctx, data, endpoint, apiKey, apiType)
	if err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	// OpenAI 格式
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					return content, nil
				}
			}
		}
	}

	// Anthropic 格式
	if contentList, ok := result["content"].([]interface{}); ok {
		for _, c := range contentList {
			if cm, ok := c.(map[string]interface{}); ok {
				if text, ok := cm["text"].(string); ok {
					return text, nil
				}
			}
		}
	}

	return "", fmt.Errorf("unexpected response format")
}

// processReflectionResult 解析 LLM 自省結果並保存
func (sl *SelfLearner) processReflectionResult(result string) {
	if globalUnifiedMemory == nil {
		return
	}

	saved := 0
	lines := strings.Split(result, "\n")
	var currentCategory MemoryCategory
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 檢測 category header
		switch {
		case strings.HasPrefix(line, "### Facts") || strings.HasPrefix(line, "## Facts"):
			currentCategory = MemoryCategoryFact
			continue
		case strings.HasPrefix(line, "### Preferences") || strings.HasPrefix(line, "## Preferences"):
			currentCategory = MemoryCategoryPreference
			continue
		case strings.HasPrefix(line, "### Experiences") || strings.HasPrefix(line, "## Experiences"):
			currentCategory = MemoryCategoryExperience
			continue
		case strings.HasPrefix(line, "###") || strings.HasPrefix(line, "##"):
			currentCategory = ""
			continue
		}

		if currentCategory == "" || !strings.HasPrefix(line, "- ") {
			continue
		}

		// 解析 "- key: value" 格式
		entry := strings.TrimPrefix(line, "- ")
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}

		if err := globalUnifiedMemory.SaveEntry(currentCategory, key, value, nil, MemoryScopeUser); err != nil {
			log.Printf("[SelfLearner] Failed to save %s/%s: %v", currentCategory, key, err)
			continue
		}
		saved++
	}

	if saved > 0 {
		log.Printf("[SelfLearner] Saved %d learnings from self-reflection", saved)
	} else {
		log.Printf("[SelfLearner] No learnings extracted from reflection")
	}
}

// reflectionSystemPrompt 自省系統提示
var reflectionSystemPrompt = `你是一個自學習分析器。根據任務描述和對話歷史，分析這次交互並提取可復用的經驗。

嚴格按以下格式輸出（不要輸出其他內容）：

### Facts
- 事實key: 簡潔的事實描述

### Preferences
- 偏好key: 簡潔的偏好描述

### Experiences
- 學到的經驗: 簡潔的經驗總結

每條必須是 "key: value" 格式。只記錄有長期價值的內容，不要記錄一次性事務信息。如果沒有值得記錄的內容，對應區塊留空。`
