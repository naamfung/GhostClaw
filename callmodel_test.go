package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// ============================================================================
// Helper Functions
// ============================================================================

// makeMessage 快速创建 Message
func makeMessage(role string, content interface{}) Message {
	return Message{Role: role, Content: content}
}

// makeToolCall 快速创建单个 tool call entry
func makeToolCall(id, name string, args interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id": id,
		"function": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
}

// makeToolCallSlice 将多个 tool call 打包成 []interface{} 切片（标准接口格式）
func makeToolCallSlice(calls ...map[string]interface{}) []interface{} {
	result := make([]interface{}, len(calls))
	for i, c := range calls {
		result[i] = c
	}
	return result
}

// assertJSONEqual 比较两个值为 JSON 后相等
func assertJSONEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("%s\n  got:  %s\n  want: %s", msg, string(gotJSON), string(wantJSON))
	}
}

// assertMessageSliceEqual 比较两个 Message 切片 JSON 相等
func assertMessageSliceEqual(t *testing.T, got, want []Message, msg string) {
	t.Helper()
	assertJSONEqual(t, got, want, msg)
}

// ============================================================================
// 1. Anthropic 格式转换 — TestConvertToAnthropicFormat
// ============================================================================

func TestConvertToAnthropicFormat(t *testing.T) {
	t.Run("普通 user", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hello"},
		}
		result := convertToAnthropicFormat(msgs)
		want := []map[string]interface{}{
			{"role": "user", "content": "hello"},
		}
		assertJSONEqual(t, result, want, "user message")
	})

	t.Run("普通 assistant", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "hi there"},
		}
		result := convertToAnthropicFormat(msgs)
		want := []map[string]interface{}{
			{
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "text", "text": "hi there"},
				},
			},
		}
		assertJSONEqual(t, result, want, "assistant message")
	})

	t.Run("system 跳过", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "hi"},
		}
		result := convertToAnthropicFormat(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result))
		}
		if result[0]["role"] != "user" {
			t.Errorf("expected user role, got %v", result[0]["role"])
		}
	})

	t.Run("tool 消息转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "result text", ToolCallID: "toolu_123"},
		}
		result := convertToAnthropicFormat(msgs)
		want := []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_123",
						"content":     "result text",
					},
				},
			},
		}
		assertJSONEqual(t, result, want, "tool message")
	})

	t.Run("tool 无 content fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: nil, ToolCallID: "toolu_x"},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if content[0]["content"] != "" {
			t.Errorf("expected empty string content, got %v", content[0]["content"])
		}
	})

	t.Run("tool 无 ToolCallID fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "result", ToolCallID: ""},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if content[0]["tool_use_id"] != "unknown_tool_use" {
			t.Errorf("expected 'unknown_tool_use', got %v", content[0]["tool_use_id"])
		}
	})

	// Thinking block 6 种组合

	t.Run("thinking: 仅有 ReasoningContent 无签名", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "I think..."},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 2 {
			t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(content))
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("first block should be thinking, got %v", content[0]["type"])
		}
		if content[0]["thinking"] != "I think..." {
			t.Errorf("thinking text mismatch: %v", content[0]["thinking"])
		}
		_, hasSig := content[0]["signature"]
		if hasSig {
			t.Error("signature should not be present when ThinkingSignature is empty")
		}
	})

	t.Run("thinking: 仅有 ThinkingSignature 无文字 (redacted thinking)", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: nil, ThinkingSignature: "sig_abc"},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 2 {
			t.Fatalf("expected 2 content blocks (thinking + text), got %d: %v", len(content), content)
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("first block should be thinking, got %v", content[0]["type"])
		}
		if content[0]["thinking"] != "" {
			t.Errorf("thinking text should be empty for redacted, got %v", content[0]["thinking"])
		}
		if content[0]["signature"] != "sig_abc" {
			t.Errorf("signature mismatch: %v", content[0]["signature"])
		}
	})

	t.Run("thinking: ReasoningContent + ThinkingSignature 均有", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "deep think", ThinkingSignature: "sig_xyz"},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if content[0]["thinking"] != "deep think" {
			t.Errorf("thinking text mismatch: %v", content[0]["thinking"])
		}
		if content[0]["signature"] != "sig_xyz" {
			t.Errorf("signature mismatch: %v", content[0]["signature"])
		}
	})

	t.Run("thinking: 均无", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "plain answer", ReasoningContent: nil, ThinkingSignature: ""},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 1 {
			t.Fatalf("expected 1 content block (text only), got %d", len(content))
		}
		if content[0]["type"] != "text" {
			t.Errorf("expected text block, got %v", content[0]["type"])
		}
	})

	t.Run("thinking: ReasoningContent 为空字符串", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "", ThinkingSignature: ""},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		// "" 是 string 类型非 nil，会触发 thinking block 创建但 reasoning 文字为空
		if len(content) != 2 {
			t.Fatalf("expected 2 content blocks (thinking'' + text), got %d", len(content))
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("expected thinking block, got %v", content[0]["type"])
		}
	})

	t.Run("thinking: nil + 空 signature", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: nil, ThinkingSignature: ""},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 1 {
			t.Fatalf("expected 1 content block (text only), got %d", len(content))
		}
		if content[0]["type"] != "text" {
			t.Errorf("expected text block, got %v", content[0]["type"])
		}
	})

	// Thinking block + ToolCalls 组合

	t.Run("thinking + tool_calls 组合", func(t *testing.T) {
		msgs := []Message{
			{
				Role:             "assistant",
				Content:          "let me search",
				ReasoningContent: "thinking...",
				ThinkingSignature: "sig_tools",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_1", "search", `{"query":"test"}`),
				),
			},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 3 {
			t.Fatalf("expected 3 blocks (thinking + text + tool_use), got %d", len(content))
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("block 0 should be thinking, got %v", content[0]["type"])
		}
		if content[1]["type"] != "text" {
			t.Errorf("block 1 should be text, got %v", content[1]["type"])
		}
		if content[2]["type"] != "tool_use" {
			t.Errorf("block 2 should be tool_use, got %v", content[2]["type"])
		}
	})

	t.Run("redacted thinking + tool_calls", func(t *testing.T) {
		msgs := []Message{
			{
				Role:             "assistant",
				Content:          "",
				ReasoningContent: nil,
				ThinkingSignature: "sig_redacted",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_r", "run", `{"cmd":"ls"}`),
				),
			},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		// thinking block (redacted) + tool_use, no text block (content is empty)
		if len(content) != 2 {
			t.Fatalf("expected 2 blocks (thinking + tool_use), got %d", len(content))
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("block 0 should be thinking, got %v", content[0]["type"])
		}
		if content[0]["signature"] != "sig_redacted" {
			t.Errorf("signature mismatch: %v", content[0]["signature"])
		}
		if content[1]["type"] != "tool_use" {
			t.Errorf("block 1 should be tool_use, got %v", content[1]["type"])
		}
	})

	// 多个 tool_calls

	t.Run("多个 tool_calls", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id1", "tool_a", `{}`),
					makeToolCall("id2", "tool_b", `{"key":"val"}`),
				),
			},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 2 {
			t.Fatalf("expected 2 tool_use blocks, got %d", len(content))
		}
		if content[0]["id"] != "id1" {
			t.Errorf("first tool_use id mismatch")
		}
		if content[1]["id"] != "id2" {
			t.Errorf("second tool_use id mismatch")
		}
	})

	t.Run("tool_calls 为 nil 时 content 数组格式正常", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "plain", ToolCalls: nil},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 1 {
			t.Fatalf("expected 1 text block, got %d", len(content))
		}
	})

	t.Run("空 assistant 无 content 无 tool_calls fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: nil, ToolCalls: nil},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"]
		// fallback: 空数组
		if arr, ok := content.([]map[string]interface{}); !ok || len(arr) != 0 {
			t.Errorf("expected empty array fallback, got %v", content)
		}
	})
}

// ============================================================================
// 2. OpenAI 格式转换 — TestConvertToOpenAIFormat
// ============================================================================

func TestConvertToOpenAIFormat(t *testing.T) {
	t.Run("基本 user 消息", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hello"},
		}
		result := convertToOpenAIFormat(msgs)
		want := []map[string]interface{}{
			{"role": "user", "content": "hello"},
		}
		assertJSONEqual(t, result, want, "basic user")
	})

	t.Run("基本 assistant 消息", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "hi"},
		}
		result := convertToOpenAIFormat(msgs)
		want := []map[string]interface{}{
			{"role": "assistant", "content": "hi"},
		}
		assertJSONEqual(t, result, want, "basic assistant")
	})

	t.Run("system 消息保留", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "You are helpful."},
		}
		result := convertToOpenAIFormat(msgs)
		if result[0]["role"] != "system" {
			t.Errorf("expected system role")
		}
	})

	t.Run("tool 消息转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "result", ToolCallID: "call_123"},
		}
		result := convertToOpenAIFormat(msgs)
		want := []map[string]interface{}{
			{"role": "tool", "content": "result", "tool_call_id": "call_123"},
		}
		assertJSONEqual(t, result, want, "tool message")
	})

	t.Run("tool 无 ToolCallID fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "result", ToolCallID: ""},
		}
		result := convertToOpenAIFormat(msgs)
		if result[0]["tool_call_id"] != "unknown_tool_call" {
			t.Errorf("expected 'unknown_tool_call', got %v", result[0]["tool_call_id"])
		}
	})

	t.Run("tool nil content fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: nil, ToolCallID: "call_x"},
		}
		result := convertToOpenAIFormat(msgs)
		if result[0]["content"] != "" {
			t.Errorf("expected empty string, got %v", result[0]["content"])
		}
	})

	t.Run("tool non-string content JSON", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: map[string]interface{}{"a": 1}, ToolCallID: "call_j"},
		}
		result := convertToOpenAIFormat(msgs)
		contentStr, ok := result[0]["content"].(string)
		if !ok || !strings.Contains(contentStr, `"a"`) {
			t.Errorf("expected JSON string content, got %v", result[0]["content"])
		}
	})

	t.Run("reasoning_content 传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "deep reasoning"},
		}
		result := convertToOpenAIFormat(msgs)
		if result[0]["reasoning_content"] != "deep reasoning" {
			t.Errorf("reasoning_content not passed: %v", result[0])
		}
	})

	t.Run("reasoning_content 空字符串不传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: ""},
		}
		result := convertToOpenAIFormat(msgs)
		if _, ok := result[0]["reasoning_content"]; ok {
			t.Error("empty reasoning_content should not be set")
		}
	})

	t.Run("reasoning_content nil 不传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: nil},
		}
		result := convertToOpenAIFormat(msgs)
		if _, ok := result[0]["reasoning_content"]; ok {
			t.Error("nil reasoning_content should not be set")
		}
	})

	t.Run("thinking_signature 传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ThinkingSignature: "sig_pass"},
		}
		result := convertToOpenAIFormat(msgs)
		if result[0]["thinking_signature"] != "sig_pass" {
			t.Errorf("thinking_signature not passed: %v", result[0])
		}
	})

	t.Run("thinking_signature 空不传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ThinkingSignature: ""},
		}
		result := convertToOpenAIFormat(msgs)
		if _, ok := result[0]["thinking_signature"]; ok {
			t.Error("empty thinking_signature should not be set")
		}
	})

	t.Run("tool_calls arguments 字符化 (string args)", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id1", "search", `{"q":"test"}`),
				),
			},
		}
		result := convertToOpenAIFormat(msgs)
		tcs := result[0]["tool_calls"].([]interface{})
		tc := tcs[0].(map[string]interface{})
		fn := tc["function"].(map[string]interface{})
		if fn["arguments"] != `{"q":"test"}` {
			t.Errorf("string args should stay string: %v", fn["arguments"])
		}
	})

	t.Run("tool_calls arguments 对象格式标准化", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id2", "run", map[string]interface{}{"cmd": "ls"}),
				),
			},
		}
		result := convertToOpenAIFormat(msgs)
		tcs := result[0]["tool_calls"].([]interface{})
		tc := tcs[0].(map[string]interface{})
		fn := tc["function"].(map[string]interface{})
		argsStr, ok := fn["arguments"].(string)
		if !ok {
			t.Fatalf("expected args to be string, got %T", fn["arguments"])
		}
		if !strings.Contains(argsStr, `"cmd"`) {
			t.Errorf("expected JSON string for args, got %s", argsStr)
		}
	})

	t.Run("空 content + tool_calls 省略 content 字段", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				Content: "",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id_e", "empty", `{}`),
				),
			},
		}
		result := convertToOpenAIFormat(msgs)
		if _, ok := result[0]["content"]; ok {
			t.Error("empty content with tool_calls should omit content field")
		}
	})

	t.Run("nil content + tool_calls 无 content 字段", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				Content: nil,
				ToolCalls: makeToolCallSlice(
					makeToolCall("id_n", "nil", `{}`),
				),
			},
		}
		result := convertToOpenAIFormat(msgs)
		if _, ok := result[0]["content"]; ok {
			t.Error("nil content with tool_calls should not have content field")
		}
	})

	t.Run("tool_calls 空切片", func(t *testing.T) {
		msgs := []Message{
			{
				Role:     "assistant",
				Content:  "test",
				ToolCalls: []interface{}{},
			},
		}
		result := convertToOpenAIFormat(msgs)
		// 空 tool_calls 不应出现在输出中
		if _, ok := result[0]["tool_calls"]; ok {
			t.Error("empty tool_calls should not be set")
		}
	})

	t.Run("多个 tool_calls arguments 混合类型", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id_a", "fn_a", `"string_arg"`),
					makeToolCall("id_b", "fn_b", map[string]interface{}{"b": 2}),
				),
			},
		}
		result := convertToOpenAIFormat(msgs)
		tcs := result[0]["tool_calls"].([]interface{})
		if len(tcs) != 2 {
			t.Fatalf("expected 2 tool_calls, got %d", len(tcs))
		}
		// 验证两个都是 string
		for i, tc := range tcs {
			tcMap := tc.(map[string]interface{})
			fn := tcMap["function"].(map[string]interface{})
			if _, ok := fn["arguments"].(string); !ok {
				t.Errorf("tool_call %d arguments should be string, got %T", i, fn["arguments"])
			}
		}
	})
}

// ============================================================================
// 3. Ollama 格式转换 — TestConvertToOllamaFormat
// ============================================================================

func TestConvertToOllamaFormat(t *testing.T) {
	t.Run("system 跳过", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "You are a bot."},
			{Role: "user", Content: "hi"},
		}
		result := convertToOllamaFormat(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message (system skipped), got %d", len(result))
		}
		if result[0]["role"] != "user" {
			t.Errorf("expected user, got %v", result[0]["role"])
		}
	})

	t.Run("thinking block 传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "thinking...", ThinkingSignature: "sig_abc"},
		}
		result := convertToOllamaFormat(msgs)
		if result[0]["reasoning_content"] != "thinking..." {
			t.Errorf("reasoning_content mismatch: %v", result[0]["reasoning_content"])
		}
		if result[0]["thinking_signature"] != "sig_abc" {
			t.Errorf("thinking_signature mismatch: %v", result[0]["thinking_signature"])
		}
	})

	t.Run("thinking 空 reasoning 不传递", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ReasoningContent: "", ThinkingSignature: ""},
		}
		result := convertToOllamaFormat(msgs)
		if _, ok := result[0]["reasoning_content"]; ok {
			t.Error("empty reasoning_content should not be set")
		}
	})

	t.Run("assistant + tool_calls", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				Content: "calling tool",
				ToolCalls: makeToolCallSlice(
					makeToolCall("id_o", "tool_ollama", `{}`),
				),
			},
		}
		result := convertToOllamaFormat(msgs)
		if result[0]["tool_calls"] == nil {
			t.Error("tool_calls should be preserved")
		}
		if result[0]["content"] != "calling tool" {
			t.Errorf("content mismatch: %v", result[0]["content"])
		}
	})

	t.Run("tool 消息序列化", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "tool result", ToolCallID: "id_t"},
		}
		result := convertToOllamaFormat(msgs)
		// Ollama tool 消息仅保留 role + content (字符串)
		if result[0]["content"] != "tool result" {
			t.Errorf("tool content mismatch: %v", result[0]["content"])
		}
	})

	t.Run("tool nil content fallback", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: nil, ToolCallID: "id_nil"},
		}
		result := convertToOllamaFormat(msgs)
		if result[0]["content"] != "" {
			t.Errorf("expected empty string, got %v", result[0]["content"])
		}
	})

	t.Run("tool non-string content JSON", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: map[string]interface{}{"x": 1}, ToolCallID: "id_j"},
		}
		result := convertToOllamaFormat(msgs)
		if _, ok := result[0]["content"].(string); !ok {
			t.Errorf("expected string content, got %T", result[0]["content"])
		}
	})
}

// ============================================================================
// 4. normalizeToolCall
// ============================================================================

func TestNormalizeToolCall(t *testing.T) {
	t.Run("非 map 类型原样返回", func(t *testing.T) {
		result := normalizeToolCall("not a map")
		if result != "not a map" {
			t.Errorf("non-map should be returned as-is")
		}
	})

	t.Run("arguments 已为字符串保持不变", func(t *testing.T) {
		tc := map[string]interface{}{
			"id": "id1",
			"function": map[string]interface{}{
				"name":      "test_fn",
				"arguments": `{"key":"val"}`,
			},
		}
		result := normalizeToolCall(tc)
		resultMap := result.(map[string]interface{})
		fn := resultMap["function"].(map[string]interface{})
		if fn["arguments"] != `{"key":"val"}` {
			t.Errorf("string args should be preserved")
		}
	})

	t.Run("arguments map 转字符串", func(t *testing.T) {
		tc := map[string]interface{}{
			"id": "id2",
			"function": map[string]interface{}{
				"name":      "test_fn2",
				"arguments": map[string]interface{}{"cmd": "ls", "dir": "/tmp"},
			},
		}
		result := normalizeToolCall(tc)
		resultMap := result.(map[string]interface{})
		fn := resultMap["function"].(map[string]interface{})
		argsStr, ok := fn["arguments"].(string)
		if !ok {
			t.Fatalf("expected string, got %T", fn["arguments"])
		}
		if !strings.Contains(argsStr, `"cmd"`) {
			t.Errorf("expected JSON string, got %s", argsStr)
		}
	})

	t.Run("arguments 其他类型转字符串", func(t *testing.T) {
		tc := map[string]interface{}{
			"id": "id3",
			"function": map[string]interface{}{
				"name":      "test_fn3",
				"arguments": 42,
			},
		}
		result := normalizeToolCall(tc)
		resultMap := result.(map[string]interface{})
		fn := resultMap["function"].(map[string]interface{})
		argsStr, ok := fn["arguments"].(string)
		if !ok {
			t.Fatalf("expected string, got %T", fn["arguments"])
		}
		if argsStr != "42" {
			t.Errorf("expected '42', got %s", argsStr)
		}
	})

	t.Run("无 function 字段", func(t *testing.T) {
		tc := map[string]interface{}{
			"id": "id4",
		}
		result := normalizeToolCall(tc)
		resultMap := result.(map[string]interface{})
		if resultMap["id"] != "id4" {
			t.Errorf("id should be preserved")
		}
	})

	t.Run("无 arguments 字段", func(t *testing.T) {
		tc := map[string]interface{}{
			"id": "id5",
			"function": map[string]interface{}{
				"name": "no_args_fn",
			},
		}
		result := normalizeToolCall(tc)
		resultMap := result.(map[string]interface{})
		fn := resultMap["function"].(map[string]interface{})
		if fn["name"] != "no_args_fn" {
			t.Errorf("name should be preserved")
		}
	})

	t.Run("不修改原始 TC", func(t *testing.T) {
		orig := map[string]interface{}{
			"id": "id6",
			"function": map[string]interface{}{
				"name":      "orig_fn",
				"arguments": map[string]interface{}{"x": 1},
			},
		}
		result := normalizeToolCall(orig)
		resultMap := result.(map[string]interface{})
		fnResult := resultMap["function"].(map[string]interface{})
		// 修改结果不应影响原始
		fnResult["name"] = "modified"
		if orig["function"].(map[string]interface{})["name"] != "orig_fn" {
			t.Error("original TC was mutated!")
		}
	})
}

// ============================================================================
// 5. validateAndCleanMessages
// ============================================================================

func TestValidateAndCleanMessages(t *testing.T) {
	t.Run("空消息列表", func(t *testing.T) {
		result := validateAndCleanMessages([]Message{})
		if len(result) != 0 {
			t.Errorf("expected empty, got %d", len(result))
		}
	})

	t.Run("空 role 跳过", func(t *testing.T) {
		msgs := []Message{
			{Role: "", Content: "orphan"},
			{Role: "user", Content: "hi"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result))
		}
		if result[0].Role != "user" {
			t.Errorf("expected user, got %s", result[0].Role)
		}
	})

	t.Run("nil content user 变成空字符串", func(t *testing.T) {
		// 单独一条 nil content user 会被 final pass 移除
		// 需要跟在有效消息后以避免被移除
		msgs := []Message{
			{Role: "user", Content: "valid"},
			{Role: "user", Content: nil},
		}
		result := validateAndCleanMessages(msgs)
		// 合并后 Content 是 "valid\n"（nil变成""后merge入 prior）
		// 两条 user 会合并为一条
		if len(result) != 1 {
			t.Fatalf("expected 1 merged message, got %d", len(result))
		}
		if result[0].Content != "valid" {
			t.Errorf("expected 'valid', got %v", result[0].Content)
		}
	})

	t.Run("nil content assistant 变成空字符串", func(t *testing.T) {
		// 单独一条 nil content assistant 会被 final pass 移除
		// 需要跟在有效消息后以避免被移除
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: nil, ReasoningContent: "think"},
		}
		result := validateAndCleanMessages(msgs)
		// assistant 有 ReasoningContent，不会被移除
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
		if result[1].Content != "" {
			t.Errorf("expected empty string (nil→\"\"), got %v", result[1].Content)
		}
	})

	t.Run("空 content + tool_calls assistant → content nil", func(t *testing.T) {
		// 需要跟在 user 后面，避免触发 synthetic user 插入
		// tool_calls 需要对应的 tool result 避免被移除为孤儿
		msgs := []Message{
			{Role: "user", Content: "run this"},
			{
				Role: "assistant",
				Content: "",
				ToolCalls: makeToolCallSlice(makeToolCall("tc_keep", "fn", `{}`)),
			},
			{Role: "tool", Content: "done", ToolCallID: "tc_keep"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result))
		}
		// assistant 的 content 应为 nil（空字符串 + tool_calls → nil）
		if result[1].Content != nil {
			t.Errorf("expected nil content, got %v", result[1].Content)
		}
	})

	t.Run("tool 无 tool_call_id 自动生成", func(t *testing.T) {
		// tool 消息缺少 tool_call_id，但 assistant 有匹配的 tool_calls
		// 由于 hasToolCallID 对空 toolCallID 返回 false，tool 被当作孤立移除
		// 然后 assistant 的 tool_calls 也没有对应结果 → 也被清除
		msgs := []Message{
			{Role: "user", Content: "run"},
			{Role: "assistant", Content: "calling", ToolCalls: makeToolCallSlice(makeToolCall("gen_id", "fn", `{}`))},
			{Role: "tool", Content: "result", ToolCallID: ""},
		}
		result := validateAndCleanMessages(msgs)
		// 孤立 tool 被移除，assistant 的 tool_calls 也被清除
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (user + cleaned assistant), got %d", len(result))
		}
		if result[1].ToolCalls != nil {
			t.Errorf("orphaned tool_calls should be removed")
		}
	})

	t.Run("tool nil content 变成空字符串", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "run"},
			{Role: "assistant", Content: "", ToolCalls: makeToolCallSlice(makeToolCall("tc_a", "fn", `{}`))},
			{Role: "tool", Content: nil, ToolCallID: "tc_a"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result))
		}
		if result[2].Content != "" {
			t.Errorf("expected empty string, got %v", result[2].Content)
		}
	})

	t.Run("tool non-string content 转 JSON", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "run"},
			{Role: "assistant", Content: "", ToolCalls: makeToolCallSlice(makeToolCall("tc_b", "fn", `{}`))},
			{Role: "tool", Content: map[string]interface{}{"result": true}, ToolCallID: "tc_b"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(result))
		}
		contentStr, ok := result[2].Content.(string)
		if !ok {
			t.Fatalf("expected string content, got %T", result[2].Content)
		}
		if !strings.Contains(contentStr, `"result"`) {
			t.Errorf("expected JSON string, got %s", contentStr)
		}
	})

	t.Run("连续 user 合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hello"},
			{Role: "user", Content: "world"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 merged message, got %d", len(result))
		}
		if result[0].Content != "hello\nworld" {
			t.Errorf("expected 'hello\\nworld', got %v", result[0].Content)
		}
	})

	t.Run("连续 assistant 无 tool_calls 无 thinking 合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", Content: "part1"},
			{Role: "assistant", Content: "part2"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
		if result[1].Content != "part1\npart2" {
			t.Errorf("expected merged content, got %v", result[1].Content)
		}
	})

	t.Run("连续 assistant 有 thinking block 不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", Content: "part1", ReasoningContent: "think1"},
			{Role: "assistant", Content: "part2"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages (no merge due to thinking), got %d", len(result))
		}
	})

	t.Run("连续 assistant 有 thinking_signature 不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "start"},
			{Role: "assistant", Content: "part1", ThinkingSignature: "sig"},
			{Role: "assistant", Content: "part2"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages (no merge due to sig), got %d", len(result))
		}
	})

	t.Run("连续 tool 合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "run"},
			{Role: "assistant", Content: "", ToolCalls: makeToolCallSlice(makeToolCall("id", "fn", `{}`))},
			{Role: "tool", Content: "r1", ToolCallID: "id"},
			{Role: "tool", Content: "r2", ToolCallID: "id"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 messages (user + assistant + merged tool), got %d", len(result))
		}
		if result[2].Content != "r1\nr2" {
			t.Errorf("expected merged tool content, got %v", result[2].Content)
		}
	})

	t.Run("移除空 assistant 无 tool_calls 无 reasoning 无 signature", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ToolCalls: nil, ReasoningContent: nil, ThinkingSignature: ""},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message (empty assistant removed), got %d", len(result))
		}
	})

	t.Run("保留空 content 但有 thinking signature 的 assistant", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "", ReasoningContent: nil, ThinkingSignature: "sig_empty"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (assistant kept due to signature), got %d", len(result))
		}
	})

	t.Run("序列以 assistant 开头插入 synthetic user", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "orphan answer"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (user inserted), got %d", len(result))
		}
		if result[0].Role != "user" || result[0].Content != "continue" {
			t.Errorf("expected synthetic user, got %v", result[0])
		}
	})

	t.Run("序列以 tool 开头插入 synthetic user", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "", ToolCalls: makeToolCallSlice(makeToolCall("tc_s", "fn", `{}`))},
			{Role: "tool", Content: "result", ToolCallID: "tc_s"},
		}
		result := validateAndCleanMessages(msgs)
		// tool result 不是 orphan，但以 tool 开头会触发插入
		if result[0].Role != "user" {
			t.Errorf("expected first message to be user or system, got %s", result[0].Role)
		}
	})

	t.Run("4步完整清理管线", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
			{Role: "user", Content: "q2"},
			{Role: "user", Content: "q3"}, // 连续 user 合并
		}
		result := validateAndCleanMessages(msgs)
		// 应当有 5 条：system, user(q1), assistant(a1), user(q2\nq3)
		if len(result) != 4 {
			t.Fatalf("expected 4 messages after merge, got %d: %v", len(result), result)
		}
		if result[3].Content != "q2\nq3" {
			t.Errorf("expected merged user content, got %v", result[3].Content)
		}
	})
}

// ============================================================================
// 6. 孤立消息处理
// ============================================================================

func TestRemoveOrphanedToolMessages(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		result := removeOrphanedToolMessages([]Message{})
		if len(result) != 0 {
			t.Errorf("expected empty")
		}
	})

	t.Run("正常配对", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc1", "fn", `{}`)),
			},
			{Role: "tool", Content: "result", ToolCallID: "tc1"},
		}
		result := removeOrphanedToolMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
	})

	t.Run("孤立 tool 移除", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", Content: "orphan", ToolCallID: "no_match"},
		}
		result := removeOrphanedToolMessages(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message (orphan removed), got %d", len(result))
		}
		if result[0].Role != "user" {
			t.Errorf("expected user, got %s", result[0].Role)
		}
	})

	t.Run("ID 不匹配视为孤儿", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc_a", "fn", `{}`)),
			},
			{Role: "tool", Content: "result", ToolCallID: "tc_b"},
		}
		result := removeOrphanedToolMessages(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("user 阻断搜索", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc", "fn", `{}`)),
			},
			{Role: "user", Content: "interruption"},
			{Role: "tool", Content: "result", ToolCallID: "tc"},
		}
		result := removeOrphanedToolMessages(msgs)
		// tool 消息前有 user，搜索中断 → 视为孤儿
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (tool removed as orphan), got %d", len(result))
		}
	})

	t.Run("system 阻断搜索", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc", "fn", `{}`)),
			},
			{Role: "system", Content: "interruption"},
			{Role: "tool", Content: "result", ToolCallID: "tc"},
		}
		result := removeOrphanedToolMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (tool removed as orphan), got %d", len(result))
		}
	})
}

func TestRemoveOrphanedToolCalls(t *testing.T) {
	t.Run("空切片", func(t *testing.T) {
		result := removeOrphanedToolCalls([]Message{})
		if len(result) != 0 {
			t.Errorf("expected empty")
		}
	})

	t.Run("正常配对", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc1", "fn", `{}`)),
			},
			{Role: "tool", Content: "result", ToolCallID: "tc1"},
		}
		result := removeOrphanedToolCalls(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
	})

	t.Run("全部孤立 tool_calls 移除", func(t *testing.T) {
		msgs := []Message{
			{
				Role:    "assistant",
				Content: "no result",
				ToolCalls: makeToolCallSlice(makeToolCall("orphan_id", "fn", `{}`)),
			},
		}
		result := removeOrphanedToolCalls(msgs)
		if result[0].ToolCalls != nil {
			t.Errorf("expected nil tool_calls, got %v", result[0].ToolCalls)
		}
	})

	t.Run("全部孤立且 nil content → 空字符串", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				Content: nil,
				ToolCalls: makeToolCallSlice(makeToolCall("orphan_id", "fn", `{}`)),
			},
		}
		result := removeOrphanedToolCalls(msgs)
		if result[0].Content != "" {
			t.Errorf("expected empty string content, got %v", result[0].Content)
		}
	})

	t.Run("部分有结果过滤", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("keep", "fn1", `{}`),
					makeToolCall("drop", "fn2", `{}`),
				),
			},
			{Role: "tool", Content: "result1", ToolCallID: "keep"},
		}
		result := removeOrphanedToolCalls(msgs)
		tcs := result[0].ToolCalls.([]interface{})
		if len(tcs) != 1 {
			t.Fatalf("expected 1 remaining tool_call, got %d", len(tcs))
		}
		tcMap := tcs[0].(map[string]interface{})
		if tcMap["id"] != "keep" {
			t.Errorf("expected 'keep' tool_call, got %v", tcMap["id"])
		}
	})

	t.Run("无 tool 消息全部孤立", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("no_result", "fn", `{}`)),
			},
		}
		result := removeOrphanedToolCalls(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
		if result[1].ToolCalls != nil {
			t.Errorf("expected nil tool_calls, got %v", result[1].ToolCalls)
		}
	})
}

func TestHasToolCallID(t *testing.T) {
	t.Run("空 ID 返回 false", func(t *testing.T) {
		tcs := makeToolCallSlice(makeToolCall("id1", "fn", `{}`))
		if hasToolCallID(tcs, "") {
			t.Error("empty toolCallID should return false")
		}
	})

	t.Run("[]interface{} 匹配成功", func(t *testing.T) {
		tcs := makeToolCallSlice(
			makeToolCall("a", "fna", `{}`),
			makeToolCall("b", "fnb", `{}`),
		)
		if !hasToolCallID(tcs, "b") {
			t.Error("should match 'b'")
		}
	})

	t.Run("[]interface{} 无匹配", func(t *testing.T) {
		tcs := makeToolCallSlice(makeToolCall("a", "fn", `{}`))
		if hasToolCallID(tcs, "z") {
			t.Error("should not match 'z'")
		}
	})

	t.Run("[]map[string]interface{} 匹配成功", func(t *testing.T) {
		tcs := []map[string]interface{}{
			makeToolCall("x", "fnx", `{}`),
		}
		if !hasToolCallID(tcs, "x") {
			t.Error("should match 'x'")
		}
	})

	t.Run("[]map[string]interface{} 无匹配", func(t *testing.T) {
		tcs := []map[string]interface{}{
			makeToolCall("x", "fnx", `{}`),
		}
		if hasToolCallID(tcs, "y") {
			t.Error("should not match 'y'")
		}
	})

	t.Run("nil toolCalls", func(t *testing.T) {
		if hasToolCallID(nil, "any") {
			t.Error("nil toolCalls should return false")
		}
	})
}

func TestFilterToolCallsWithResults(t *testing.T) {
	t.Run("全部匹配", func(t *testing.T) {
		results := map[string]bool{"a": true, "b": true}
		tcs := makeToolCallSlice(
			makeToolCall("a", "fn_a", `{}`),
			makeToolCall("b", "fn_b", `{}`),
		)
		hasAny := false
		remaining := filterToolCallsWithResults(tcs, results, &hasAny)
		if len(remaining) != 2 {
			t.Fatalf("expected 2 remaining, got %d", len(remaining))
		}
		if !hasAny {
			t.Error("hasAnyResult should be true")
		}
	})

	t.Run("部分匹配", func(t *testing.T) {
		results := map[string]bool{"a": true}
		tcs := makeToolCallSlice(
			makeToolCall("a", "fn_a", `{}`),
			makeToolCall("b", "fn_b", `{}`),
		)
		hasAny := false
		remaining := filterToolCallsWithResults(tcs, results, &hasAny)
		if len(remaining) != 1 {
			t.Fatalf("expected 1 remaining, got %d", len(remaining))
		}
		if remaining[0].(map[string]interface{})["id"] != "a" {
			t.Errorf("expected 'a', got %v", remaining[0])
		}
		if !hasAny {
			t.Error("hasAnyResult should be true")
		}
	})

	t.Run("无匹配", func(t *testing.T) {
		results := map[string]bool{"x": true}
		tcs := makeToolCallSlice(makeToolCall("a", "fn_a", `{}`))
		hasAny := false
		remaining := filterToolCallsWithResults(tcs, results, &hasAny)
		if len(remaining) != 0 {
			t.Fatalf("expected 0 remaining, got %d", len(remaining))
		}
		if hasAny {
			t.Error("hasAnyResult should be false")
		}
	})

	t.Run("[]map[string]interface{} 类型", func(t *testing.T) {
		results := map[string]bool{"keep": true}
		tcs := []map[string]interface{}{
			makeToolCall("keep", "fn", `{}`),
		}
		hasAny := false
		remaining := filterToolCallsWithResults(tcs, results, &hasAny)
		if len(remaining) != 1 {
			t.Fatalf("expected 1 remaining, got %d", len(remaining))
		}
	})
}

// ============================================================================
// 7. 合并与起始查找
// ============================================================================

func TestMergeConsecutiveSameRole(t *testing.T) {
	t.Run("空 / 单条", func(t *testing.T) {
		result := mergeConsecutiveSameRole([]Message{})
		if len(result) != 0 {
			t.Errorf("expected empty")
		}
		single := mergeConsecutiveSameRole([]Message{{Role: "user", Content: "hi"}})
		if len(single) != 1 {
			t.Errorf("expected 1")
		}
	})

	t.Run("连续 user 合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "a"},
			{Role: "user", Content: "b"},
			{Role: "user", Content: "c"},
		}
		result := mergeConsecutiveSameRole(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 merged, got %d", len(result))
		}
		if result[0].Content != "a\nb\nc" {
			t.Errorf("expected 'a\\nb\\nc', got %v", result[0].Content)
		}
	})

	t.Run("assistant 有 tool_calls 不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{
				Role: "assistant",
				Content: "a1",
				ToolCalls: makeToolCallSlice(makeToolCall("tc", "fn", `{}`)),
			},
			{Role: "assistant", Content: "a2"},
		}
		result := mergeConsecutiveSameRole(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 (no merge), got %d", len(result))
		}
	})

	t.Run("assistant 有 thinking blocks 不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a1", ReasoningContent: "think"},
			{Role: "assistant", Content: "a2"},
		}
		result := mergeConsecutiveSameRole(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 (no merge), got %d", len(result))
		}
	})

	t.Run("tool 消息不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "r1", ToolCallID: "id"},
			{Role: "tool", Content: "r2", ToolCallID: "id"},
		}
		result := mergeConsecutiveSameRole(msgs)
		// mergeConsecutiveSameRole 的代码确实跳过了 "tool" 角色
		if len(result) != 2 {
			t.Fatalf("expected 2 (tool not merged), got %d", len(result))
		}
	})

	t.Run("连续 assistant 无防护合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a1"},
			{Role: "assistant", Content: "a2"},
		}
		result := mergeConsecutiveSameRole(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 (merged), got %d", len(result))
		}
		if result[1].Content != "a1\na2" {
			t.Errorf("expected merged content, got %v", result[1].Content)
		}
	})

	t.Run("非字串内容不合并", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "hi"}}},
			{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "there"}}},
		}
		result := mergeConsecutiveSameRole(msgs)
		// 非 string 内容不合并
		if len(result) != 2 {
			t.Fatalf("expected 2 (no merge for non-string), got %d", len(result))
		}
	})
}

func TestFindLegalStart(t *testing.T) {
	t.Run("空", func(t *testing.T) {
		result := findLegalStart([]Message{})
		if len(result) != 0 {
			t.Errorf("expected empty")
		}
	})

	t.Run("正常序列", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hey"},
		}
		result := findLegalStart(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2, got %d", len(result))
		}
	})

	t.Run("孤立 tool 跳过", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "tool", Content: "orphan", ToolCallID: "no_declare"},
			{Role: "user", Content: "real"},
			{Role: "assistant", Content: "real"},
		}
		result := findLegalStart(msgs)
		if len(result) == 0 {
			t.Fatal("result should not be empty")
		}
		if result[0].Role == "tool" {
			t.Error("first message should not be tool")
		}
	})

	t.Run("连续孤儿跳过到有效消息", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "o1", ToolCallID: "no1"},
			{Role: "tool", Content: "o2", ToolCallID: "no2"},
			{Role: "user", Content: "real"},
			{Role: "assistant", Content: "real"},
		}
		result := findLegalStart(msgs)
		if result[0].Role != "user" || result[0].Content != "real" {
			t.Errorf("expected real user first, got %v", result[0])
		}
	})

	t.Run("有 system 前缀保持", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "tool", Content: "orphan", ToolCallID: "no_declare"},
			{Role: "user", Content: "real"},
		}
		result := findLegalStart(msgs)
		if result[0].Role != "system" {
			t.Errorf("expected system first, got %s", result[0].Role)
		}
		if result[1].Role != "user" {
			t.Errorf("expected user second (orphan skipped), got %s", result[1].Role)
		}
	})

	t.Run("孤立后截断时有 thinking block 回退", func(t *testing.T) {
		// 场景：assistant(with thinking sig) → assistant → tool(orphan) → user(real)
		// 孤立 tool 导致截断到 start=3 (tool 之后), 但 start 未 >= len
		// 截断会丢失 index 1 的 thinking block, 所以回退 start 到 index 1
		msgs := []Message{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1", ThinkingSignature: "sig_back"},
			{Role: "assistant", Content: "a2"},
			{Role: "tool", Content: "orphan", ToolCallID: "no_declare"},
			{Role: "user", Content: "real"},
		}
		result := findLegalStart(msgs)
		// 应该回退到 index 1 (含 thinking block 的 assistant)，因为孤儿 tool 后还有 user
		// start 初始为 4 (孤儿 tool 之后), systemEnd=0, start > systemEnd
		// 从 start-1 往回找 thinking block: index 1 has sig_back → start = 1
		if len(result) >= 2 && result[1].ThinkingSignature == "sig_back" {
			// OK — thinking block 被保留回来
		} else if result[0].ThinkingSignature == "sig_back" {
			// OK — thinking block 成为第一条
		} else {
			t.Errorf("expected thinking block preserved, got %d messages, first role=%s", len(result), result[0].Role)
		}
	})

	t.Run("全部 tool 消息降级", func(t *testing.T) {
		msgs := []Message{
			{Role: "tool", Content: "o1", ToolCallID: "n1"},
			{Role: "tool", Content: "o2", ToolCallID: "n2"},
		}
		result := findLegalStart(msgs)
		// 全部都是孤儿 → 最后一条
		if len(result) != 1 {
			t.Fatalf("expected 1 fallback, got %d", len(result))
		}
	})

	t.Run("正常 assistant→tool 配对保留", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc_good", "fn", `{}`)),
			},
			{Role: "tool", Content: "result", ToolCallID: "tc_good"},
		}
		result := findLegalStart(msgs)
		if len(result) != 3 {
			t.Fatalf("expected 3 (all kept), got %d", len(result))
		}
	})

	t.Run("推理内容回退 (DeepSeek reasoning_content 无 signature)", func(t *testing.T) {
		// DeepSeek thinking mode：仅 ReasoningContent，无 ThinkingSignature
		// 孤儿 tool 截断时必须回退保留 reasoning_content，否则 API 返回 400
		msgs := []Message{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1", ReasoningContent: "deepseek think only"},
			{Role: "assistant", Content: "a2"},
			{Role: "tool", Content: "orphan", ToolCallID: "no_declare"},
			{Role: "user", Content: "real"},
		}
		result := findLegalStart(msgs)
		// 应该回退到含 reasoning_content 的 assistant
		found := false
		for _, m := range result {
			if rc, ok := m.ReasoningContent.(string); ok && rc == "deepseek think only" {
				found = true
				break
			}
		}
		if !found {
			t.Error("DeepSeek reasoning_content lost during findLegalStart rollback — would cause API 400")
		}
	})
}

// ============================================================================
// 8. 压缩测试
// ============================================================================

func TestCompressMessages(t *testing.T) {
	// Helper: 构建 4 轮对话
	makeConv := func(rounds int) []Message {
		var msgs []Message
		for i := 0; i < rounds; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q " + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a " + itoa(i)})
		}
		return msgs
	}

	t.Run("Level 0: tool 结果截断", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				Content: "calling",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_t0", "bash", `{"command":"echo hello world and more text"}`),
				),
			},
			{Role: "tool", Content: strings.Repeat("long_output_", 300), ToolCallID: "tc_t0"},
		}
		result := compressMessages(msgs, 0)
		toolResult := result[1].Content.(string)
		// 应该被截断为 [bash: echo...] [成功] + 后200字符
		if !strings.Contains(toolResult, "[bash:") {
			t.Errorf("expected tool prefix, got: %s", toolResult[:100])
		}
		if !strings.Contains(toolResult, "成功") {
			t.Errorf("expected success status, got: %s", toolResult[:100])
		}
	})

	t.Run("Level 0: thinking block 不变", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "think", ThinkingSignature: "sig"},
		}
		result := compressMessages(msgs, 0)
		if len(result) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(result))
		}
		if result[1].ReasoningContent != "think" {
			t.Errorf("reasoning should be preserved")
		}
		if result[1].ThinkingSignature != "sig" {
			t.Errorf("signature should be preserved")
		}
	})

	t.Run("Level 0: Error 前缀工具结果", func(t *testing.T) {
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_err", "run", `{"command":"bad"}`),
				),
			},
			{Role: "tool", Content: "Error: something went wrong", ToolCallID: "tc_err"},
		}
		result := compressMessages(msgs, 0)
		toolResult := result[1].Content.(string)
		if !strings.Contains(toolResult, "失败") {
			t.Errorf("expected failure status, got: %s", toolResult)
		}
	})

	t.Run("Level 1: tool 消息移除", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{
				Role: "assistant",
				Content: "calling",
				ToolCalls: makeToolCallSlice(makeToolCall("tc_1", "fn", `{}`)),
			},
			{Role: "tool", Content: "result", ToolCallID: "tc_1"},
			{Role: "assistant", Content: "answer"},
		}
		result := compressMessages(msgs, 1)
		// tool 消息被移除，tool_calls 被清除，然后连续 assistant 被 mergeConsecutiveSameRole 合并
		if len(result) != 2 {
			t.Fatalf("expected 2 messages (user + merged assistants), got %d: %v", len(result), result)
		}
		// assistant 的 tool_calls 应被清空
		if result[1].ToolCalls != nil {
			t.Errorf("tool_calls should be nil")
		}
		// 两条 assistant 合并
		if result[1].Content != "calling\nanswer" {
			t.Errorf("expected merged content, got %v", result[1].Content)
		}
	})

	t.Run("Level 1: thinking block 保留", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "think_answer", ReasoningContent: "deep", ThinkingSignature: "sig_1"},
		}
		result := compressMessages(msgs, 1)
		if result[1].ReasoningContent != "deep" {
			t.Errorf("reasoning should be preserved at level 1")
		}
		if result[1].ThinkingSignature != "sig_1" {
			t.Errorf("signature should be preserved at level 1")
		}
	})

	t.Run("Level 2: 消息 <= 20 不变", func(t *testing.T) {
		msgs := makeConv(5) // 10 messages
		result := compressMessages(msgs, 2)
		if len(result) != 10 {
			t.Fatalf("expected 10 messages, got %d", len(result))
		}
	})

	t.Run("Level 2: > 20 截断保留 system", func(t *testing.T) {
		var msgs []Message
		msgs = append(msgs, Message{Role: "system", Content: "sys"})
		msgs = append(msgs, makeConv(15)...) // 30 条 non-system
		result := compressMessages(msgs, 2)
		if result[0].Role != "system" {
			t.Errorf("expected system first")
		}
		// 总条数应为 system + 20 = 21
		if len(result) > 21 {
			t.Errorf("expected <= 21 messages, got %d", len(result))
		}
	})

	t.Run("Level 2: 截断后保护 thinking block", func(t *testing.T) {
		var msgs []Message
		for i := 0; i < 15; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q" + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a" + itoa(i)})
		}
		// 在第 5 条 assistant (index 9) 插入 thinking 签名
		msgs[9].ThinkingSignature = "sig_protect"
		msgs[9].ReasoningContent = "early thinking"
		result := compressMessages(msgs, 2)
		// 检查保留部分是否含有 thinking block
		found := false
		for _, m := range result {
			if m.ThinkingSignature == "sig_protect" {
				found = true
				break
			}
		}
		if !found {
			t.Error("thinking block should be preserved after level 2 compression")
		}
	})

	t.Run("Level 2: 仅有 reasoning_content 的 DeepSeek thinking mode 保护", func(t *testing.T) {
		// DeepSeek thinking mode 返回 reasoning_content 但没有 thinking_signature
		// 这种情况必须在截断时保留，否则 API 返回 400:
		// "The reasoning_content in the thinking mode must be passed back to the API"
		var msgs []Message
		for i := 0; i < 15; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q" + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a" + itoa(i)})
		}
		// 仅有 ReasoningContent，无 ThinkingSignature（DeepSeek 场景）
		msgs[9].ReasoningContent = "deepseek reasoning without signature"
		result := compressMessages(msgs, 2)
		found := false
		for _, m := range result {
			if rc, ok := m.ReasoningContent.(string); ok && rc == "deepseek reasoning without signature" {
				found = true
				break
			}
		}
		if !found {
			t.Error("DeepSeek reasoning_content (without signature) must be preserved after level 2 compression to avoid API 400 error")
		}
	})

	t.Run("Level 2: 截断后的清理", func(t *testing.T) {
		// 截断后产生孤立 tool → 应被清理
		var msgs []Message
		for i := 0; i < 12; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q" + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a" + itoa(i)})
		}
		// 最后加一个 tool_calls + tool result 对 (在尾部保留区域)
		msgs = append(msgs, Message{
			Role: "assistant",
			Content: "calling",
			ToolCalls: makeToolCallSlice(makeToolCall("tc_end", "fn", `{}`)),
		})
		msgs = append(msgs, Message{Role: "tool", Content: "result", ToolCallID: "tc_end"})
		result := compressMessages(msgs, 2)
		// 不应有孤立 tool
		for _, m := range result {
			if m.Role == "tool" {
				// 如果在保留部分末尾有配对，应保留
				found := false
				for _, prev := range result {
					if prev.Role == "assistant" && prev.ToolCalls != nil {
						if hasToolCallID(prev.ToolCalls, m.ToolCallID) {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("orphan tool should be removed")
				}
			}
		}
	})

	t.Run("Level 异常值校正", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hey"},
		}
		rNeg := compressMessages(msgs, -1)
		rHigh := compressMessages(msgs, 99)
		// -1 → 0, 99 → 2
		if len(rNeg) != 2 || len(rHigh) != 2 {
			t.Errorf("level clamping failed")
		}
	})
}

// ============================================================================
// 9. 集成测试
// ============================================================================

func TestThinkingBlockPreservation(t *testing.T) {
	t.Run("通过压缩 level 0", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "think", ThinkingSignature: "sig0"},
		}
		result := compressMessages(msgs, 0)
		if result[1].ThinkingSignature != "sig0" {
			t.Error("sig lost at level 0")
		}
	})

	t.Run("通过压缩 level 1", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "think", ThinkingSignature: "sig1"},
		}
		result := compressMessages(msgs, 1)
		if result[1].ThinkingSignature != "sig1" {
			t.Error("sig lost at level 1")
		}
	})

	t.Run("通过压缩 level 2", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "think", ThinkingSignature: "sig2"},
		}
		result := compressMessages(msgs, 2)
		if result[1].ThinkingSignature != "sig2" {
			t.Error("sig lost at level 2")
		}
	})

	t.Run("仅有 reasoning_content 通过压缩 level 2 (DeepSeek)", func(t *testing.T) {
		// DeepSeek thinking mode: 只有 reasoning_content 没有 thinking_signature
		// 必须保留 reasoning_content，否则下次请求 API 会返回 400
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "deepseek think without sig"},
		}
		result := compressMessages(msgs, 2)
		rc, ok := result[1].ReasoningContent.(string)
		if !ok || rc != "deepseek think without sig" {
			t.Errorf("DeepSeek reasoning_content lost at level 2, got=%v", result[1].ReasoningContent)
		}
	})

	t.Run("仅有 reasoning_content 通过 OpenAI 转换 (DeepSeek)", func(t *testing.T) {
		// 确保 DeepSeek reasoning_content 正确传递到 OpenAI 格式
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "deepseek_reasoning_only"},
		}
		result := convertToOpenAIFormat(msgs)
		if result[1]["reasoning_content"] != "deepseek_reasoning_only" {
			t.Errorf("DeepSeek reasoning_content lost in OpenAI format, got=%v", result[1]["reasoning_content"])
		}
	})

	t.Run("仅有 reasoning_content 通过验证管线 (DeepSeek)", func(t *testing.T) {
		// 确保 validateAndCleanMessages 不会删除仅有 reasoning_content 的 assistant 消息
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "", ReasoningContent: "deepseek think only, no text content"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) < 2 {
			t.Fatal("assistant with reasoning_content only should not be deleted by validation")
		}
		rc, ok := result[1].ReasoningContent.(string)
		if !ok || rc != "deepseek think only, no text content" {
			t.Errorf("DeepSeek reasoning_content lost during validation, got=%v", result[1].ReasoningContent)
		}
	})

	t.Run("通过验证管线", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: "think", ThinkingSignature: "sig_val"},
		}
		result := validateAndCleanMessages(msgs)
		if result[1].ThinkingSignature != "sig_val" {
			t.Error("sig lost during validation")
		}
	})

	t.Run("redacted thinking 通过 Anthropic 转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: nil, ThinkingSignature: "redacted_sig"},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[1]["content"].([]map[string]interface{})
		if len(content) < 2 {
			t.Fatal("redacted thinking should produce thinking block")
		}
		if content[0]["type"] != "thinking" {
			t.Errorf("first block should be thinking")
		}
		if content[0]["signature"] != "redacted_sig" {
			t.Errorf("signature should be preserved")
		}
	})

	t.Run("redacted thinking 通过 OpenAI 转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "a", ReasoningContent: nil, ThinkingSignature: "redacted_openai"},
		}
		result := convertToOpenAIFormat(msgs)
		if result[1]["thinking_signature"] != "redacted_openai" {
			t.Error("redacted signature lost in OpenAI format")
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("空切片全管线", func(t *testing.T) {
		// 所有函数都应优雅处理空切片
		result1 := convertToAnthropicFormat([]Message{})
		if len(result1) != 0 {
			t.Errorf("anthropic: expected empty")
		}
		result2 := convertToOpenAIFormat([]Message{})
		if len(result2) != 0 {
			t.Errorf("openai: expected empty")
		}
		result3 := convertToOllamaFormat([]Message{})
		if len(result3) != 0 {
			t.Errorf("ollama: expected empty")
		}
		result4 := validateAndCleanMessages([]Message{})
		if len(result4) != 0 {
			t.Errorf("validate: expected empty")
		}
		result5 := compressMessages([]Message{}, 0)
		if len(result5) != 0 {
			t.Errorf("compress: expected empty")
		}
	})

	t.Run("system-only 通过 Anthropic 转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "only system"},
		}
		result := convertToAnthropicFormat(msgs)
		// system 被跳过 → 空
		if len(result) != 0 {
			t.Errorf("expected empty for system-only")
		}
	})

	t.Run("non-string content user", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: []interface{}{
				map[string]interface{}{"type": "text", "text": "multimodal"},
			}},
		}
		result := convertToAnthropicFormat(msgs)
		if result[0]["content"] == nil {
			t.Error("non-string content should be preserved")
		}
	})

	t.Run("assistant-only 开头 Anthropic", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "no user"},
		}
		result := convertToAnthropicFormat(msgs)
		// 应该正常转换（验证管线会处理序列问题）
		if len(result) != 1 {
			t.Fatalf("expected 1 message, got %d", len(result))
		}
	})

	t.Run("多条 system 消息 & tool 混合", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "sys1"},
			{Role: "system", Content: "sys2"},
			{Role: "user", Content: "hi"},
		}
		// Anthropic: 所有 system 跳过
		result := convertToAnthropicFormat(msgs)
		if len(result) != 1 {
			t.Fatalf("expected 1 (user only), got %d", len(result))
		}
		// OpenAI: 保留
		result2 := convertToOpenAIFormat(msgs)
		if len(result2) != 3 {
			t.Fatalf("expected 3 (all preserved), got %d", len(result2))
		}
	})

	t.Run("nil toolCalls 但 content 存在", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "answer", ToolCalls: nil},
		}
		result := convertToAnthropicFormat(msgs)
		content := result[0]["content"].([]map[string]interface{})
		if len(content) != 1 || content[0]["type"] != "text" {
			t.Errorf("expected single text block")
		}
	})

	t.Run("long tool output compression level 0", func(t *testing.T) {
		longStr := strings.Repeat("data_", 400) // 2000 chars
		msgs := []Message{
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_long", "fetch", `{"url":"http://example.com"}`),
				),
			},
			{Role: "tool", Content: longStr, ToolCallID: "tc_long"},
		}
		result := compressMessages(msgs, 0)
		toolResult := result[1].Content.(string)
		// 后200个字符应保留
		runes := []rune(longStr)
		expectedTail := string(runes[len(runes)-200:])
		if !strings.Contains(toolResult, expectedTail[:10]) {
			t.Errorf("tail not preserved properly")
		}
	})
}

// ============================================================================
// 10. 全管线测试
// ============================================================================

func TestFullPipelineCombinations(t *testing.T) {
	t.Run("验证 → Level 2 → Anthropic 含 thinking", func(t *testing.T) {
		var msgs []Message
		msgs = append(msgs, Message{Role: "system", Content: "sys"})
		for i := 0; i < 15; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q" + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a" + itoa(i)})
		}
		// 倒数第 3 条 assistant 加 thinking
		msgs[len(msgs)-3].ReasoningContent = "final thoughts"
		msgs[len(msgs)-3].ThinkingSignature = "final_sig"

		validated := validateAndCleanMessages(msgs)
		compressed := compressMessages(validated, 2)
		result := convertToAnthropicFormat(compressed)

		// 验证输出非空
		if len(result) == 0 {
			t.Fatal("pipeline produced empty result")
		}
		// 检查是否有 thinking block
		foundThinking := false
		for _, msg := range result {
			if content, ok := msg["content"].([]map[string]interface{}); ok {
				for _, block := range content {
					if block["type"] == "thinking" {
						foundThinking = true
						break
					}
				}
			}
		}
		if !foundThinking {
			t.Error("thinking block lost in full pipeline")
		}
	})

	t.Run("验证 → 压缩 → OpenAI 含 tool_calls", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "run cmd"},
			{
				Role: "assistant",
				Content: "running",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_pipe", "bash", `{"command":"ls"}`),
				),
			},
			{Role: "tool", Content: "file1.txt\nfile2.txt", ToolCallID: "tc_pipe"},
			{Role: "assistant", Content: "result: files found"},
		}
		validated := validateAndCleanMessages(msgs)
		compressed := compressMessages(validated, 0)
		result := convertToOpenAIFormat(compressed)

		if len(result) == 0 {
			t.Fatal("pipeline produced empty result")
		}
		// 应有 4 条消息
		if len(result) != 4 {
			t.Fatalf("expected 4 messages, got %d", len(result))
		}
	})

	t.Run("孤立 tool 完整流程", func(t *testing.T) {
		msgs := []Message{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "q"},
			{Role: "tool", Content: "orphan1", ToolCallID: "no_match"},
			{Role: "tool", Content: "orphan2", ToolCallID: "also_no_match"},
			{
				Role: "assistant",
				ToolCalls: makeToolCallSlice(makeToolCall("tc_real", "fn", `{}`)),
			},
			{Role: "tool", Content: "real_result", ToolCallID: "tc_real"},
		}
		result := validateAndCleanMessages(msgs)
		// 孤立 tool 应被移除
		for _, m := range result {
			if m.Content == "orphan1" || m.Content == "orphan2" {
				t.Errorf("orphan tool not removed: %v", m)
			}
		}
		// real_result 应保留
		found := false
		for _, m := range result {
			if m.Content == "real_result" {
				found = true
				break
			}
		}
		if !found {
			t.Error("valid tool result was removed")
		}
	})

	t.Run("连续同角色合并后转换", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "part1"},
			{Role: "user", Content: "part2"},
			{Role: "assistant", Content: "reply1"},
			{Role: "assistant", Content: "reply2"},
		}
		result := validateAndCleanMessages(msgs)
		if len(result) != 2 {
			t.Fatalf("expected 2 merged messages, got %d", len(result))
		}
		// 转换为 Anthropic 后验证
		anthropic := convertToAnthropicFormat(result)
		if len(anthropic) != 2 {
			t.Fatalf("expected 2 anthropic messages, got %d", len(anthropic))
		}
	})

	t.Run("管道不修改原始输入", func(t *testing.T) {
		orig := []Message{
			{Role: "user", Content: "q"},
			{
				Role: "assistant",
				Content: "a",
				ToolCalls: makeToolCallSlice(
					makeToolCall("tc_immut", "fn", `{"key":"val"}`),
				),
				ReasoningContent: "think",
				ThinkingSignature: "sig",
			},
		}
		origCopy := make([]Message, len(orig))
		copy(origCopy, orig)

		validateAndCleanMessages(orig)
		compressMessages(orig, 0)
		convertToAnthropicFormat(orig)
		convertToOpenAIFormat(orig)
		convertToOllamaFormat(orig)

		// 原始消息的 ToolCalls 切片 header 可能被 convertTo* 借用 type switch 读取，但内容不应被深层修改
		// 浅层比较 role, content, thinking_signature
		for i := range orig {
			if orig[i].Role != origCopy[i].Role {
				t.Errorf("role changed at %d", i)
			}
			if orig[i].Content != origCopy[i].Content {
				t.Errorf("content changed at %d", i)
			}
			if orig[i].ThinkingSignature != origCopy[i].ThinkingSignature {
				t.Errorf("sig changed at %d", i)
			}
		}
	})

	t.Run("DeepSeek reasoning_content 完整管線保護 (解決 400 錯誤)", func(t *testing.T) {
		// 模擬 DeepSeek thinking mode 場景：reasoning_content 在歷史深處，
		// 經歷長對話導致 level 2 壓縮觸發 — 必須保留 reasoning_content 否則 API 返回 400
		var msgs []Message
		msgs = append(msgs, Message{Role: "system", Content: "sys"})
		for i := 0; i < 15; i++ {
			msgs = append(msgs, Message{Role: "user", Content: "q" + itoa(i)})
			msgs = append(msgs, Message{Role: "assistant", Content: "a" + itoa(i)})
		}
		// 早期消息包含 DeepSeek thinking block（assistant a4，index 10）：
		// 仅有 reasoning_content，无 signature
		msgs[10].ReasoningContent = "deepseek_thinking_only"

		validated := validateAndCleanMessages(msgs)
		compressed := compressMessages(validated, 2)
		result := convertToOpenAIFormat(compressed)

		foundReasoning := false
		for _, msg := range result {
			if rc, ok := msg["reasoning_content"]; ok {
				if rcStr, ok := rc.(string); ok && rcStr == "deepseek_thinking_only" {
					foundReasoning = true
					break
				}
			}
		}
		if !foundReasoning {
			t.Error("DeepSeek reasoning_content lost in full pipeline (validate→compress level 2→OpenAI) — would cause API 400: 'reasoning_content must be passed back'")
		}
	})
}

// ============================================================================
// itoa helper (no need for strconv import)
// ============================================================================
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
