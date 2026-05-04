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
  CHAT - casual chat, factual knowledge question ("what is X?", "how does Y work?")
  TASK - user wants you to investigate, check, verify, build, fix, create, modify, install, run, or take ANY action

Key distinction: asking "can X work?" or "does X support Y?" implies the user wants you to CHECK — that is a TASK.
Only pure factual questions ("what is X?") or casual conversation are CHAT.

Examples:
  "what is Go?" → CHAT
  "how does Docker work?" → CHAT
  "can my system run with bun?" → TASK
  "is FreeBSD good for servers?" → CHAT
  "check if bun is installed" → TASK
  "fix the login bug" → TASK
  "add a delete button" → TASK
  "帮我重構個模組" → TASK
  "你係邊個" → CHAT`,
		},
		{Role: "user", Content: "User message: \"\"\"" + sanitized + "\"\"\"\n\nClassification:"},
	}

	resp, err := CallModelSync(ctx, messages, apiType, baseURL, apiKey, modelID, 0, 10, false, false)
	if err != nil {
		// 安全默認：API 失敗時假設為任務，觸發工作模式
		return IntentTask, err
	}

	content := strings.TrimSpace(extractContentString(resp.Content))
	// 移除標點、換行等干擾字符
	upper := strings.ToUpper(content)
	upper = strings.TrimRight(upper, ".\n\r,;:!? \t")
	// 精確匹配 "TASK"，避免 "TASKING" / "TASKED" 誤判
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
