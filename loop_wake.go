package main

import (
	"log"
	"strings"
	"time"
)

// ============================================================================
// loop_wake.go — 即時喚醒通知注入
// ============================================================================
// 從 AgentLoop L607-652 抽出：
//   - 將 ShellDelayed/spawn 後台任務的完成/失敗通知注入到 user 消息中
//   - 安全注入策略：合併到現有 user 消息，避免破壞 tool_use→tool_result 鄰接性

// RunWakeInjection checks InputMessages queue for pending wake notifications
// and injects them into the last user message (or appends a new one).
// Modifies messages in place.
func RunWakeInjection(messages *[]Message, iteration int) {
	session := GetGlobalSession()
	if session == nil {
		return
	}

	session.inputMu.Lock()
	var remaining []string
	for _, input := range session.InputMessages {
		if strings.Contains(input, "任务唤醒通知") {
			// 從尾部向前找最後一條 user 消息，將喚醒通知合併進去
			// 這樣不會破壞 tool_use→tool_result 的順序
			merged := false
			for i := len(*messages) - 1; i >= 0; i-- {
				if (*messages)[i].Role == "user" {
					if contentStr, ok := (*messages)[i].Content.(string); ok {
						(*messages)[i].Content = contentStr + "\n\n" + input
					} else {
						(*messages)[i].Content = input
					}
					merged = true
					log.Printf("[AgentLoop] Merged wake notification into existing user message (index=%d, iteration=%d)", i, iteration)
					break
				}
			}
			if !merged {
				// 極端情況：沒有任何 user 消息（不應該發生，I2 保證至少一條）
				*messages = append(*messages, Message{
					Role:      "user",
					Content:   input,
					Timestamp: time.Now().Unix(),
				})
				log.Printf("[AgentLoop] Injected pending wake notification as new user message (iteration=%d)", iteration)
			}
		} else {
			remaining = append(remaining, input)
		}
	}
	session.InputMessages = remaining
	session.inputMu.Unlock()
}
