package main

import (
	"log"
	"strings"
)

// ============================================================================
// normalize_session.go — 会话归一化（保证不修改历史消息）
// ============================================================================
// 参考 DeepSeek-Reasonix 的 normalize.go 设计。
// 目标：在 session 加载或发送前，修复旧版/中断/格式不全的历史消息，
// 使其能被 provider 正确重放，同时严格保证「历史消息只读」语义——
// 不修改历史消息的 content/role/timestamp，避免破坏 prompt cache 前缀。
//
// 与 validateAndCleanMessages 的区别：
//   validateAndCleanMessages 是底层格式修复（在 prepareRequestData 内每次调用）
//   NormalizeSession 是会话级归一化（session 加载时调用一次 + 每次发送前轻量检查）
//
// 集成点：
//   - prepareRequestData 内 extractSystemPrompt 之后调用（保证发送前消息格式正确）
//   - session_persist 加载历史后调用（修复旧版数据）
//
// 重要：本函数对历史消息只做「补全」不做「重写」：
//   - 不修改已有 content
//   - 不修改已有 role
//   - 不修改 timestamp
//   - 只补充缺失字段（如 tool_call_id）和移除空角色消息
// ============================================================================

// NormalizeSession 对会话消息做归一化修复，保证历史消息不被修改
// 返回新切片，原切片不变（除非无任何修改时直接返回原切片以零分配）
func NormalizeSession(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	// 快速路径：检查是否需要修复（避免无谓分配）
	needsFix := false
	for i := range messages {
		if needsNormalization(&messages[i], i) {
			needsFix = true
			break
		}
	}
	if !needsFix {
		// 良好格式的历史直接返回，零分配
		return messages
	}

	// 慢路径：复制并修复
	cleaned := make([]Message, 0, len(messages))
	fixedCount := 0
	for i := range messages {
		msg := messages[i] // 值拷贝

		// 1. 跳过空角色消息（不复制到结果）
		if msg.Role == "" {
			fixedCount++
			continue
		}

		// 2. 补全 tool 消息缺失的 tool_call_id（不修改已有 id）
		if msg.Role == "tool" && msg.ToolCallID == "" {
			msg.ToolCallID = formatAutoToolCallID(i)
			fixedCount++
		}

		// 3. 补全 user/assistant 缺失的 content（不修改已有 content）
		if (msg.Role == "user" || msg.Role == "assistant") && msg.Content == nil {
			msg.Content = ""
			fixedCount++
		}

		// 4. assistant 带 tool_calls 时 content 应为 nil（API 要求）
		//    仅当 content 是空字符串时改为 nil，非空内容保留
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			if s, ok := msg.Content.(string); ok && s == "" {
				msg.Content = nil
				fixedCount++
			}
		}

		cleaned = append(cleaned, msg)
	}

	// 5. 修复连续 assistant 消息（在尾部追加一条 user 占位符会破坏历史，
	//    改为合并连续的 assistant 消息）
	cleaned = mergeConsecutiveAssistants(cleaned, &fixedCount)

	// 6. 修复孤立的 tool 消息（无对应 assistant tool_calls）：补一条占位 assistant
	cleaned = fixOrphanedToolMessages(cleaned, &fixedCount)

	if fixedCount > 0 {
		log.Printf("[NormalizeSession] 修复 %d 处问题，消息数 %d → %d",
			fixedCount, len(messages), len(cleaned))
		// 历史被重写（即使是补全），递增版本号让 PrefixShape 能感知
		IncrementLogRewriteVersion()
	}

	return cleaned
}

// needsNormalization 快速检查单条消息是否需要修复
func needsNormalization(msg *Message, idx int) bool {
	if msg.Role == "" {
		return true
	}
	if msg.Role == "tool" && msg.ToolCallID == "" {
		return true
	}
	if (msg.Role == "user" || msg.Role == "assistant") && msg.Content == nil {
		return true
	}
	if msg.Role == "assistant" && msg.ToolCalls != nil {
		if s, ok := msg.Content.(string); ok && s == "" {
			return true
		}
	}
	return false
}

// formatAutoToolCallID 生成自动 tool_call_id
func formatAutoToolCallID(idx int) string {
	return "auto_norm_" + itoaSimple(idx)
}

// itoaSimple 简易整数转字符串（避免引入 strconv 仅为这一处）
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// mergeConsecutiveAssistants 合并连续的 assistant 消息
// 某些 provider（如 OpenAI）不允许连续 assistant 消息，需合并为一条
// 只合并 content（拼接字符串），tool_calls 合并到同一条
func mergeConsecutiveAssistants(msgs []Message, fixedCount *int) []Message {
	if len(msgs) <= 1 {
		return msgs
	}

	out := make([]Message, 0, len(msgs))
	i := 0
	for i < len(msgs) {
		if msgs[i].Role != "assistant" {
			out = append(out, msgs[i])
			i++
			continue
		}

		// 收集连续的 assistant 消息
		merged := msgs[i]
		j := i + 1
		for j < len(msgs) && msgs[j].Role == "assistant" {
			// 合并 content
			merged.Content = mergeContent(merged.Content, msgs[j].Content)
			// 合并 tool_calls（如果都是 []map[string]interface{}）
			merged.ToolCalls = mergeToolCalls(merged.ToolCalls, msgs[j].ToolCalls)
			*fixedCount++
			j++
		}
		out = append(out, merged)
		i = j
	}
	return out
}

// mergeContent 合并两条消息的 content
func mergeContent(a, b interface{}) interface{} {
	aStr, aOK := a.(string)
	bStr, bOK := b.(string)
	if aOK && bOK {
		if aStr == "" {
			return bStr
		}
		if bStr == "" {
			return aStr
		}
		return aStr + "\n" + bStr
	}
	// 非字符串：优先返回非 nil 的
	if a != nil {
		return a
	}
	return b
}

// mergeToolCalls 合并两个 tool_calls 列表
func mergeToolCalls(a, b interface{}) interface{} {
	aSlice, aOK := a.([]map[string]interface{})
	bSlice, bOK := b.([]map[string]interface{})
	if !aOK && !bOK {
		return a // 保持原样
	}
	if !aOK {
		return b
	}
	if !bOK {
		return a
	}
	merged := make([]map[string]interface{}, 0, len(aSlice)+len(bSlice))
	merged = append(merged, aSlice...)
	merged = append(merged, bSlice...)
	return merged
}

// fixOrphanedToolMessages 为孤立的 tool 消息补一条占位 assistant
// 孤立：tool 消息前一条不是 assistant 或 assistant 无 tool_calls
// 这种情况通常是 session 保存时丢失了 assistant 消息
func fixOrphanedToolMessages(msgs []Message, fixedCount *int) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	out := make([]Message, 0, len(msgs)+2)
	for i := range msgs {
		// 检查是否是孤立的 tool 消息
		if msgs[i].Role == "tool" {
			needPlaceholder := false
			if len(out) == 0 {
				// tool 消息在开头：需要占位
				needPlaceholder = true
			} else if out[len(out)-1].Role != "assistant" {
				// 前一条不是 assistant：需要占位
				needPlaceholder = true
			} else if out[len(out)-1].ToolCalls == nil {
				// 前一条是 assistant 但无 tool_calls：需要占位
				needPlaceholder = true
			}

			if needPlaceholder {
				// 插入占位 assistant（content 为空，tool_calls 用占位）
				placeholder := Message{
					Role:    "assistant",
					Content: nil,
					ToolCalls: []map[string]interface{}{
						{
							"id":   msgs[i].ToolCallID,
							"type": "function",
							"function": map[string]interface{}{
								"name":      "recovered",
								"arguments": "{}",
							},
						},
					},
				}
				out = append(out, placeholder)
				*fixedCount++
			}
		}
		out = append(out, msgs[i])
	}
	return out
}

// IsCompactionSummary 检测消息是否是 compaction 摘要（供其他模块使用）
// 摘要消息以特定 tag 开头，区分于普通用户消息
const (
	compactionSummaryTagOpen  = "<compaction-summary>"
	compactionSummaryTagClose = "</compaction-summary>"
)

// IsCompactionSummary 判断消息是否为 compaction 摘要
func IsCompactionSummary(msg Message) bool {
	if msg.Role != "user" {
		return false
	}
	s, ok := msg.Content.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(s, "\n "), compactionSummaryTagOpen)
}
