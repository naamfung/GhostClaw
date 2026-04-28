package main

import (
	"context"
	"strings"
)

// ClassifyUserIntent 使用 LLM 對用戶訊息做二元分類（CHAT / TASK）
// 僅在 AgentLoop 入口處調用一次，成本極低（~5 tokens 輸出）
// 分類失敗時默認返回 IntentTask（安全默認：寧可觸發工作模式都唔好漏判）
func ClassifyUserIntent(ctx context.Context, query string, apiType, baseURL, apiKey, modelID string) (TaskIntent, error) {
	// 防止用戶輸入包含 """ 破壞 prompt 結構
	sanitized := strings.ReplaceAll(query, `"""`, `\"\"\"`)

	messages := []Message{
		{
			Role: "system",
			Content: `Classify this user message. Reply with EXACTLY ONE WORD:
  CHAT - casual conversation, question, information lookup
  TASK - user wants you to DO something (build, fix, create, modify, install, run, etc.)

Examples:
  "what is Go?" → CHAT
  "how does Docker work?" → CHAT
  "fix the login bug" → TASK
  "add a delete button" → TASK
  "帮我重構個模組" → TASK
  "你係邊個" → CHAT`,
		},
		{Role: "user", Content: "User message: \"\"\"" + sanitized + "\"\"\"\n\nClassification:"},
	}

	resp, err := CallModelSync(ctx, messages, apiType, baseURL, apiKey, modelID, 0, 5, false, false)
	if err != nil {
		// 安全默認：API 失敗時假設為任務，觸發工作模式
		return IntentTask, err
	}

	content := strings.TrimSpace(extractContentString(resp.Content))
	// 用精確匹配取代前綴匹配，避免 "TASKING" / "TASKED" 誤判
	upper := strings.ToUpper(content)
	if upper == "TASK" || strings.HasPrefix(upper, "TASK\n") || strings.HasPrefix(upper, "TASK ") {
		return IntentTask, nil
	}
	return IntentChat, nil
}

// extractContentString 從 Response.Content (interface{}) 提取字符串
func extractContentString(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	return ""
}
