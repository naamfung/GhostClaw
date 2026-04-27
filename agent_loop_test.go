package main

import (
	"encoding/json"
	"testing"
)

// ============================================================================
// getCurrentTaskDescriptionFromMessages
// ============================================================================

func TestGetCurrentTaskDescriptionFromMessages(t *testing.T) {
	t.Run("提取最后一条 user 消息", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: "task 1"},
			{Role: "assistant", Content: "reply"},
			{Role: "user", Content: "task 2"},
		}
		got := getCurrentTaskDescriptionFromMessages(msgs)
		if got != "task 2" {
			t.Errorf("expected 'task 2', got %q", got)
		}
	})

	t.Run("空消息列表", func(t *testing.T) {
		got := getCurrentTaskDescriptionFromMessages([]Message{})
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("没有 user 消息", func(t *testing.T) {
		msgs := []Message{
			{Role: "assistant", Content: "reply"},
		}
		got := getCurrentTaskDescriptionFromMessages(msgs)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("跳过空内容的 user", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: ""},
			{Role: "user", Content: "real task"},
		}
		got := getCurrentTaskDescriptionFromMessages(msgs)
		if got != "real task" {
			t.Errorf("expected 'real task', got %q", got)
		}
	})

	t.Run("跳过非字符串内容", func(t *testing.T) {
		msgs := []Message{
			{Role: "user", Content: []interface{}{map[string]interface{}{"type": "text", "text": "multimodal"}}},
			{Role: "user", Content: "plain task"},
		}
		got := getCurrentTaskDescriptionFromMessages(msgs)
		if got != "plain task" {
			t.Errorf("expected 'plain task', got %q", got)
		}
	})
}

// ============================================================================
// getAllowedToolsList
// ============================================================================

func TestGetAllowedToolsList(t *testing.T) {
	t.Run("nil role", func(t *testing.T) {
		got := getAllowedToolsList(nil)
		if got != "所有工具" {
			t.Errorf("expected '所有工具', got %q", got)
		}
	})

	t.Run("ToolPermissionAll", func(t *testing.T) {
		role := &Role{
			ToolPermission: ToolPermission{Mode: ToolPermissionAll},
		}
		got := getAllowedToolsList(role)
		if got != "所有工具" {
			t.Errorf("expected '所有工具', got %q", got)
		}
	})

	t.Run("Allowlist", func(t *testing.T) {
		role := &Role{
			ToolPermission: ToolPermission{
				Mode:         ToolPermissionAllowlist,
				AllowedTools: []string{"shell", "read_file_line", "grep"},
			},
		}
		got := getAllowedToolsList(role)
		if got != "shell, read_file_line, grep" {
			t.Errorf("expected 'shell, read_file_line, grep', got %q", got)
		}
	})

	t.Run("Allowlist empty", func(t *testing.T) {
		role := &Role{
			ToolPermission: ToolPermission{
				Mode:         ToolPermissionAllowlist,
				AllowedTools: []string{},
			},
		}
		got := getAllowedToolsList(role)
		if got != "无" {
			t.Errorf("expected '无', got %q", got)
		}
	})

	t.Run("Denylist", func(t *testing.T) {
		role := &Role{
			ToolPermission: ToolPermission{
				Mode:        ToolPermissionDenylist,
				DeniedTools: []string{"shell", "browser_visit"},
			},
		}
		got := getAllowedToolsList(role)
		if got != "除 shell, browser_visit 以外的工具" {
			t.Errorf("got %q", got)
		}
	})
}

// ============================================================================
// parseSingleOpenAIToolCall
// ============================================================================

func TestParseSingleOpenAIToolCall(t *testing.T) {
	t.Run("正常 function 类型", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":   "call_123",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "shell",
				"arguments": `{"command":"ls"}`,
			},
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.ID != "call_123" {
			t.Errorf("ID mismatch: %q", got.ID)
		}
		if got.Name != "shell" {
			t.Errorf("Name mismatch: %q", got.Name)
		}
		if got.ArgsJSON != `{"command":"ls"}` {
			t.Errorf("ArgsJSON mismatch: %q", got.ArgsJSON)
		}
	})

	t.Run("缺少 id → nil", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name": "shell",
			},
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("id 为空字符串 → nil", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":   "",
			"type": "function",
			"function": map[string]interface{}{
				"name": "shell",
			},
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("id 非字符串 → Sprint 转换", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":   42,
			"type": "function",
			"function": map[string]interface{}{
				"name":      "shell",
				"arguments": `{}`,
			},
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.ID != "42" {
			t.Errorf("expected '42', got %q", got.ID)
		}
	})

	t.Run("非 function 类型返回空名", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":   "call_456",
			"type": "unknown",
			"function": map[string]interface{}{
				"name": "shell",
			},
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.ID != "call_456" {
			t.Errorf("ID should be preserved")
		}
		if got.Name != "" {
			t.Errorf("Name should be empty for non-function type, got %q", got.Name)
		}
	})

	t.Run("缺少 function 返回空名", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":   "call_789",
			"type": "function",
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.Name != "" {
			t.Errorf("Name should be empty, got %q", got.Name)
		}
	})

	t.Run("function 不是 map → 空名", func(t *testing.T) {
		toolUse := map[string]interface{}{
			"id":       "call_abc",
			"type":     "function",
			"function": "not_a_map",
		}
		got := parseSingleOpenAIToolCall(toolUse)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if got.Name != "" {
			t.Errorf("Name should be empty, got %q", got.Name)
		}
	})
}

// ============================================================================
// parseToolCallsFromOpenAI
// ============================================================================

func TestParseToolCallsFromOpenAI(t *testing.T) {
	t.Run("[]interface{} 格式", func(t *testing.T) {
		rawCalls := []interface{}{
			map[string]interface{}{
				"id":   "call_1",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "shell",
					"arguments": `{"command":"ls"}`,
				},
			},
		}
		calls := parseToolCallsFromOpenAI(rawCalls)
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Name != "shell" {
			t.Errorf("expected 'shell', got %q", calls[0].Name)
		}
	})

	t.Run("[]map[string]interface{} 格式", func(t *testing.T) {
		rawCalls := []map[string]interface{}{
			{
				"id":   "call_2",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "grep",
					"arguments": `{"pattern":"test"}`,
				},
			},
		}
		calls := parseToolCallsFromOpenAI(rawCalls)
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].Name != "grep" {
			t.Errorf("expected 'grep', got %q", calls[0].Name)
		}
	})

	t.Run("混合无效条目", func(t *testing.T) {
		rawCalls := []interface{}{
			"not a map",              // 跳过
			map[string]interface{}{}, // 无 id → nil → 跳过
			map[string]interface{}{
				"id":   "call_valid",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "spawn",
					"arguments": `{}`,
				},
			},
		}
		calls := parseToolCallsFromOpenAI(rawCalls)
		if len(calls) != 1 {
			t.Fatalf("expected 1 valid call, got %d", len(calls))
		}
		if calls[0].Name != "spawn" {
			t.Errorf("expected 'spawn', got %q", calls[0].Name)
		}
	})

	t.Run("空输入", func(t *testing.T) {
		calls := parseToolCallsFromOpenAI(nil)
		if len(calls) != 0 {
			t.Errorf("expected empty, got %d", len(calls))
		}
	})

	t.Run("多个 tool calls", func(t *testing.T) {
		rawCalls := []interface{}{
			map[string]interface{}{
				"id":   "tc_a",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "fn_a",
					"arguments": `{}`,
				},
			},
			map[string]interface{}{
				"id":   "tc_b",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "fn_b",
					"arguments": `{"x":1}`,
				},
			},
		}
		calls := parseToolCallsFromOpenAI(rawCalls)
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
	})
}

// ============================================================================
// parseToolCallsFromAnthropic
// ============================================================================

func TestParseToolCallsFromAnthropic(t *testing.T) {
	t.Run("正常 tool_use 块", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "toolu_001",
				"name":  "shell",
				"input": map[string]interface{}{"command": "ls"},
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].ID != "toolu_001" {
			t.Errorf("ID mismatch: %q", calls[0].ID)
		}
		if calls[0].Name != "shell" {
			t.Errorf("Name mismatch: %q", calls[0].Name)
		}

		var args map[string]interface{}
		json.Unmarshal([]byte(calls[0].ArgsJSON), &args)
		if args["command"] != "ls" {
			t.Errorf("command mismatch: %v", args["command"])
		}
	})

	t.Run("跳过非 tool_use 块", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "hello",
			},
			map[string]interface{}{
				"type": "thinking",
				"thinking": "I think...",
			},
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "toolu_002",
				"name":  "grep",
				"input": map[string]interface{}{"pattern": "test"},
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 1 {
			t.Fatalf("expected 1 call (only tool_use), got %d", len(calls))
		}
		if calls[0].Name != "grep" {
			t.Errorf("expected 'grep', got %q", calls[0].Name)
		}
	})

	t.Run("缺少 name → 跳过", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "toolu_003",
				"input": map[string]interface{}{"x": 1},
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 0 {
			t.Errorf("expected empty, got %d", len(calls))
		}
	})

	t.Run("缺少 input → 跳过", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type": "tool_use",
				"id":   "toolu_004",
				"name": "shell",
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 0 {
			t.Errorf("expected empty, got %d", len(calls))
		}
	})

	t.Run("id 非字符串 → Sprint 转换", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type":  "tool_use",
				"id":    12345,
				"name":  "shell",
				"input": map[string]interface{}{},
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(calls))
		}
		if calls[0].ID != "12345" {
			t.Errorf("expected '12345', got %q", calls[0].ID)
		}
	})

	t.Run("非数组输入", func(t *testing.T) {
		calls := parseToolCallsFromAnthropic("not an array")
		if len(calls) != 0 {
			t.Errorf("expected empty, got %d", len(calls))
		}
	})

	t.Run("nil 输入", func(t *testing.T) {
		calls := parseToolCallsFromAnthropic(nil)
		if len(calls) != 0 {
			t.Errorf("expected empty, got %d", len(calls))
		}
	})

	t.Run("多个 tool_use", func(t *testing.T) {
		content := []interface{}{
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "tu_a",
				"name":  "shell",
				"input": map[string]interface{}{"command": "a"},
			},
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "tu_b",
				"name":  "grep",
				"input": map[string]interface{}{"pattern": "b"},
			},
		}
		calls := parseToolCallsFromAnthropic(content)
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
	})
}

// ============================================================================
// detectXMLToolInvocation
// ============================================================================

func TestDetectXMLToolInvocation(t *testing.T) {
	t.Run("invoke + 已知工具名 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<invoke name="shell">`) {
			t.Error("should detect invoke with known tool")
		}
	})

	t.Run("invoke + 单引号 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<invoke name='smart_shell'>`) {
			t.Error("should detect invoke with single quotes")
		}
	})

	t.Run("invoke + 未知工具名 → false", func(t *testing.T) {
		if detectXMLToolInvocation(`<invoke name="my_custom_tool_not_in_list">`) {
			t.Error("should not detect invoke with unknown tool name")
		}
	})

	t.Run("tool_call + 已知工具名 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<tool_call name="spawn">`) {
			t.Error("should detect tool_call with known tool")
		}
	})

	t.Run("tool_call + 单引号 + 浏览器工具 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<tool_call name='browser_click'>`) {
			t.Error("should detect tool_call with known browser tool")
		}
	})

	t.Run("function_call 通用模式 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<function_call>stuff here</function_call>`) {
			t.Error("should detect function_call tag")
		}
	})

	t.Run("parameter + 常见参数名 → true", func(t *testing.T) {
		if !detectXMLToolInvocation(`<parameter name="command">ls</parameter>`) {
			t.Error("should detect parameter with 'command'")
		}
	})

	t.Run("parameter 无常见参数名 → false", func(t *testing.T) {
		if detectXMLToolInvocation(`<parameter name="unknown_field">val</parameter>`) {
			t.Error("should not detect parameter without known field name")
		}
	})

	t.Run("parameter 无闭合标签 → false", func(t *testing.T) {
		if detectXMLToolInvocation(`<parameter name="command">stuff`) {
			t.Error("should not detect unclosed parameter")
		}
	})

	t.Run("普通聊天文本 → false", func(t *testing.T) {
		if detectXMLToolInvocation("你好，请帮我运行一个 shell 命令") {
			t.Error("should not detect plain Chinese text")
		}
	})

	t.Run("讨论 XML 但不含工具名 → false", func(t *testing.T) {
		if detectXMLToolInvocation("You can use <invoke> tags to call tools") {
			t.Error("should not detect XML discussion without tool names")
		}
	})

	t.Run("大小写不敏感", func(t *testing.T) {
		if !detectXMLToolInvocation(`<INVOKE NAME="SHELL">`) {
			t.Error("should be case-insensitive")
		}
	})

	t.Run("500 字符截断边界", func(t *testing.T) {
		// 创建 > 500 字符的前缀，后面接 invoke
		prefix := make([]rune, 500)
		for i := range prefix {
			prefix[i] = 'x'
		}
		long := string(prefix) + `<invoke name="shell">`
		// invoke 部分在第 500 字符之后，应被截断检测不到
		if detectXMLToolInvocation(long) {
			t.Error("should NOT detect invoke beyond 500-rune boundary")
		}
	})

	t.Run("500 字符内的 invoke", func(t *testing.T) {
		prefix := make([]rune, 10)
		for i := range prefix {
			prefix[i] = 'x'
		}
		within := string(prefix) + `<invoke name="grep">`
		if !detectXMLToolInvocation(within) {
			t.Error("should detect invoke within 500-rune boundary")
		}
	})
}
