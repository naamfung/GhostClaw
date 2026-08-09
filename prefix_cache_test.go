package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// ============================================================================
// 字节级前缀缓存端到端测试护栏（inx cachehit_e2e_test 思想移植）
//
// 核心断言：DeepSeek（openai 分支）多轮工具循环中，每轮请求的 messages 数组
// 必须是上一轮请求 messages 数组的前缀（逐条消息字节完全相同，append-only）。
// 只要这个性质成立，provider 的自动前缀缓存（KV Cache）就会持续命中
// system + tools + 历史消息，每轮只需计算新增尾部——
// 这正是「不重算好多内容」的字节级保证。
//
// 注意：断言的是「消息级字节前缀」而非「整个请求体字节前缀」。因为请求体
// 含 tools/temperature 等结构字段，新增消息后 JSON 数组必然在结构上与上一轮
// 不同（`]` 变 `,{...}`）；但 token 层的 LCP（最长公共前缀）覆盖到历史消息
// 末尾，与 inx 的 commonPrefixMsgs 语义一致。
//
// 测试前保存并恢复全局状态，避免污染其他测试。
// ============================================================================

// parseMessages 从 openai 请求体中提取 messages 数组的原始字节（json.RawMessage 列表）
func parseOpenAIMessages(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var req struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v\nbody=%s", err, body)
	}
	return req.Messages
}

// commonPrefixMsgs 返回两个消息列表逐条字节相等的前缀长度（inx 同款语义）
func commonPrefixMsgs(a, b []json.RawMessage) int {
	n := 0
	for n < len(a) && n < len(b) && bytes.Equal(a[n], b[n]) {
		n++
	}
	return n
}

func TestOpenAIPrefixStability_ToolLoop(t *testing.T) {
	savedPC := globalPromptCacheConfig
	savedMgr := globalToolDistributionMgr
	savedTasks := globalTasksMode
	savedMCP := globalMCPClientManager
	defer func() {
		globalPromptCacheConfig = savedPC
		globalToolDistributionMgr = savedMgr
		globalTasksMode = savedTasks
		globalMCPClientManager = savedMCP
	}()

	globalPromptCacheConfig = PromptCacheConfig{Enabled: true, StableTools: true}
	globalToolDistributionMgr = nil
	globalTasksMode = nil
	globalMCPClientManager = nil

	// 模拟一轮工具循环：user → assistant(tool_call) → tool(result) → assistant(tool_call) → tool(result)
	messages := []Message{
		{Role: "system", Content: "You are GhostClaw, a helpful assistant."},
		{Role: "user", Content: "Please read file a.txt and then write b.txt"},
	}
	messages = append(messages, Message{
		Role: "assistant", Content: "",
		ToolCalls: []map[string]interface{}{
			{
				"id":   "call_0001",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "read_file",
					"arguments": `{"path":"a.txt"}`,
				},
			},
		},
	})
	messages = append(messages, Message{
		Role: "tool", Content: "file a.txt contents: hello world",
		ToolCallID: "call_0001",
	})
	messages = append(messages, Message{
		Role: "assistant", Content: "",
		ToolCalls: []map[string]interface{}{
			{
				"id":   "call_0002",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "write_file",
					"arguments": `{"path":"b.txt","content":"done"}`,
				},
			},
		},
	})
	messages = append(messages, Message{
		Role: "tool", Content: "b.txt written successfully",
		ToolCallID: "call_0002",
	})

	// 逐轮生成请求体（模拟每轮迭代：轮 1 只有 user，轮 2 含第一对，轮 3 含两对）
	finalReply := Message{Role: "assistant", Content: "All done: read a.txt and wrote b.txt."}
	stages := [][]Message{
		messages[:2], // 轮 1: system + user
		messages[:4], // 轮 2: + assistant(tool_call) + tool
		messages[:6], // 轮 3: + assistant(tool_call) + tool
		append(messages[:6], finalReply), // 轮 4: 追加纯 assistant 回复（结束）
	}

	var prevMsgs []json.RawMessage
	for i, stage := range stages {
		body, _, _, err := prepareRequestData(stage, "openai", "", "deepseek-chat",
			0.7, 4096, false, false, nil)
		if err != nil {
			t.Fatalf("stage %d: prepareRequestData error: %v", i+1, err)
		}

		// 断言 1：请求体可解析为合法 JSON
		var parsed map[string]interface{}
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("stage %d: request body is not valid JSON: %v\nbody=%s", i+1, err, body)
		}

		curMsgs := parseOpenAIMessages(t, body)
		if i > 0 {
			// 断言 2（核心）：本轮 messages 数组完整包含上一轮的每条消息（字节相同）
			common := commonPrefixMsgs(prevMsgs, curMsgs)
			if common != len(prevMsgs) {
				t.Errorf("stage %d: message prefix BROKEN — common=%d, prev total=%d — prompt cache would MISS at message %d\n"+
					"prev[%d] (stage %d): %s\ncur[%d]  (stage %d): %s",
					i+1, common, len(prevMsgs), common,
					common, i, prevMsgs[common], common, i+1, curMsgs[common])
			}
		}
		prevMsgs = curMsgs
	}
}

// TestAppendBeforeLatestUser_KeepPrefix 验证低频注入（Tasks 提醒）只错位一次：
// 注入后 system 前缀仍稳定；注入之后的消息继续 append-only，后续轮次前缀稳定。
func TestAppendBeforeLatestUser_KeepPrefix(t *testing.T) {
	savedPC := globalPromptCacheConfig
	savedMgr := globalToolDistributionMgr
	savedTasks := globalTasksMode
	savedMCP := globalMCPClientManager
	defer func() {
		globalPromptCacheConfig = savedPC
		globalToolDistributionMgr = savedMgr
		globalTasksMode = savedTasks
		globalMCPClientManager = savedMCP
	}()
	globalPromptCacheConfig = PromptCacheConfig{Enabled: true, StableTools: true}
	globalToolDistributionMgr = nil
	globalTasksMode = nil
	globalMCPClientManager = nil

	base := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "do task"},
	}
	base = append(base, Message{
		Role: "assistant", Content: "",
		ToolCalls: []map[string]interface{}{
			{"id": "c1", "type": "function", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"a"}`}},
		},
	})
	base = append(base, Message{Role: "tool", Content: "result", ToolCallID: "c1"})

	// 注入（模拟 iteration==4 的 Tasks 提醒）→ 注入后历史
	injected := appendBeforeLatestUser(base, Message{Role: "system", Content: "[系统提示] reminder"})

	// 断言 1：注入消息插在最新 user（index 1）之前，且 system（index 0）未被移动
	if injected[0].Role != "system" || injected[0].Content != "sys" {
		t.Fatalf("system prefix moved: injected[0]=%+v", injected[0])
	}
	if injected[1].Role != "system" || injected[1].Content != "[系统提示] reminder" {
		t.Fatalf("injected message should be at index 1, got %+v", injected[1])
	}

	// 注入后继续 append 新消息（下一轮迭代）
	next := append(injected, Message{Role: "assistant", Content: "next step"})

	bInjected, _, _, err := prepareRequestData(injected, "openai", "", "deepseek-chat", 0.7, 4096, false, false, nil)
	if err != nil {
		t.Fatalf("injected: %v", err)
	}
	bNext, _, _, err := prepareRequestData(next, "openai", "", "deepseek-chat", 0.7, 4096, false, false, nil)
	if err != nil {
		t.Fatalf("next: %v", err)
	}

	// 断言 2（核心）：注入后的完整消息数组是下一轮的前缀（append-only 恢复）
	msgsInjected := parseOpenAIMessages(t, bInjected)
	msgsNext := parseOpenAIMessages(t, bNext)
	common := commonPrefixMsgs(msgsInjected, msgsNext)
	if common != len(msgsInjected) {
		t.Errorf("after injection, messages NOT append-only: common=%d, injected total=%d — 注入后的下一轮前缀应完全稳定", common, len(msgsInjected))
	}
}

func TestOpenAIRequestPreservesToolContext(t *testing.T) {
	savedPC := globalPromptCacheConfig
	defer func() { globalPromptCacheConfig = savedPC }()
	globalPromptCacheConfig = PromptCacheConfig{Enabled: true, StableTools: true}

	messages := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "read a.txt"},
		{
			Role: "assistant", Content: "",
			ToolCalls: []map[string]interface{}{
				{
					"id":   "call_abc",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "read_file",
						"arguments": `{"path":"a.txt"}`,
					},
				},
			},
		},
		{Role: "tool", Content: "contents", ToolCallID: "call_abc"},
	}

	body, _, _, err := prepareRequestData(messages, "openai", "", "deepseek-chat",
		0.7, 4096, false, false, nil)
	if err != nil {
		t.Fatalf("prepareRequestData error: %v", err)
	}

	var req struct {
		Messages []openaiMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 断言：assistant 消息必须保留 tool_calls
	foundAssistantToolCalls := false
	foundToolCallID := false
	for _, m := range req.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			foundAssistantToolCalls = true
		}
		if m.Role == "tool" && m.ToolCallID == "call_abc" {
			foundToolCallID = true
		}
	}
	if !foundAssistantToolCalls {
		t.Errorf("assistant tool_calls missing from request — DeepSeek would lose tool history (json=%s)", body)
	}
	if !foundToolCallID {
		t.Errorf("tool_call_id missing from tool message — tool result cannot be linked (json=%s)", body)
	}
}

// TestAnthropicCacheControlPlacement 验证 Anthropic 分支的断点数量与位置约束。
func TestAnthropicCacheControlPlacement(t *testing.T) {
	savedPC := globalPromptCacheConfig
	defer func() { globalPromptCacheConfig = savedPC }()
	globalPromptCacheConfig = PromptCacheConfig{Enabled: true, StableTools: true}

	messages := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
	}
	body, _, _, err := prepareRequestData(messages, "anthropic", "", "claude-sonnet",
		0.7, 4096, false, false, nil)
	if err != nil {
		t.Fatalf("prepareRequestData error: %v", err)
	}

	// 统计 cache_control 出现次数（system 段 + last tool + last message block，≤3）
	// 以 JSON 中 "cache_control" 出现次数粗验
	count := bytes.Count(body, []byte(`"cache_control"`))
	if count > 4 {
		t.Errorf("cache_control count = %d, exceeds Anthropic limit (4)", count)
	}
	if count < 1 {
		t.Errorf("cache_control count = %d, expected at least system breakpoint (PromptCache enabled)", count)
	}
}
