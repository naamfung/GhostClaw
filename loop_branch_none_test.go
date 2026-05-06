package main

import (
	"encoding/json"
	"testing"
)

// ============================================================================
// detectXMLToolInvocation Tests
// ============================================================================

func TestDetectXML_InvokeWithKnownTool(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "invoke with SmartShell",
			content: `<invoke name="SmartShell"><parameter name="command">ls -la</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with Shell lowercase name",
			content: `<invoke name="shell"><parameter name="command">echo hi</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with known tool and single quotes",
			content: `<invoke name='SmartShell'><parameter name='command'>ls</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with Tasks",
			content: `<invoke name="Tasks"/>`,
			want:    true,
		},
		{
			name:    "invoke with todos",
			content: `<invoke name="TodoCreate"><parameter name="content">test task</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with TextGrep",
			content: `<invoke name="TextGrep"><parameter name="pattern">test</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with BrowserNavigate",
			content: `<invoke name="BrowserNavigate"><parameter name="url">https://example.com</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "tool_call with known tool",
			content: `<tool_call name="SmartShell">ls -la</tool_call>`,
			want:    true,
		},
		{
			name:    "function_call tag",
			content: `<function_call>{"name": "shell", "arguments": {"command": "ls"}}</function_call>`,
			want:    true,
		},
		{
			name:    "parameter with command",
			content: `<parameter name="command">ls -la</parameter>`,
			want:    true,
		},
		{
			name:    "parameter with filename",
			content: `<parameter name="filename">/etc/hosts</parameter>`,
			want:    true,
		},
		{
			name:    "parameter with query",
			content: `<parameter name="query">SELECT * FROM</parameter>`,
			want:    true,
		},
		{
			name:    "parameter with url",
			content: `<parameter name="url">https://example.com</parameter>`,
			want:    true,
		},
		{
			name:    "parameter with content",
			content: `<parameter name="content">hello world</parameter>`,
			want:    true,
		},
		{
			name:    "parameter with path",
			content: `<parameter name="path">/tmp/file.txt</parameter>`,
			want:    true,
		},
		{
			name:    "DSML invoke with known tool",
			content: `<DSML_invoke name="SmartShell"><DSML_parameter name="command">ls</DSML_parameter></DSML_invoke>`,
			want:    true,
		},
		{
			name:    "DSML tool_calls with known tool",
			content: `<DSML_tool_calls><DSML_invoke name="TaskCheck"><DSML_parameter name="TaskId">task_123</DSML_parameter></DSML_invoke></DSML_tool_calls>`,
			want:    true,
		},
		{
			name:    "DSML invoke without known tool (still detected)",
			content: `<DSML_invoke name="SomeUnknownTool"><DSML_parameter name="x">y</DSML_parameter></DSML_invoke>`,
			want:    true,
		},
		{
			name:    "DSML tool_calls without known tool (still detected)",
			content: `<DSML_tool_calls><DSML_invoke name="Foo"/></DSML_tool_calls>`,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectXMLToolInvocation(tt.content)
			if got != tt.want {
				t.Errorf("detectXMLToolInvocation(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestDetectXML_FalsePositives(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "normal text about HTML",
			content: "你可以使用 <div> 标签来包裹内容，然后用 CSS 设置样式",
		},
		{
			name:    "discussion of XML format",
			content: "我建议使用 XML 格式来存储配置数据，因为它的可读性更好",
		},
		{
			name:    "invoke with unknown tool name",
			content: `<invoke name="UnknownTool"><parameter name="arg">value</parameter></invoke>`,
		},
		{
			name:    "tool_call with unknown name",
			content: `<tool_call name="FakeTool">do something</tool_call>`,
		},
		{
			name:    "empty string",
			content: "",
		},
		{
			name:    "plain code example",
			content: "func main() { fmt.Println(\"hello\") }",
		},
		{
			name:    "Markdown code block",
			content: "```xml\n<invoke name=\"test\">\n</invoke>\n```",
		},
		{
			name:    "parameter without known attr",
			content: `<parameter name="custom_attr">some value</parameter>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if detectXMLToolInvocation(tt.content) {
				t.Errorf("detectXMLToolInvocation(%q) = true, want false (false positive)", tt.content)
			}
		})
	}
}

func TestDetectXML_First500RunesOnly(t *testing.T) {
	// Create a string with 600 runes, where XML is at position 550
	prefix := make([]rune, 550)
	for i := range prefix {
		prefix[i] = 'x'
	}
	suffix := `<invoke name="SmartShell"><parameter name="command">ls</parameter></invoke>`

	content := string(prefix) + suffix
	// XML is at position 550+rune, which is beyond the 500 rune check window
	if detectXMLToolInvocation(content) {
		t.Error("Should NOT detect XML beyond first 500 runes")
	}

	// Put XML at position 100 (well within 500 rune window)
	shortPrefix := make([]rune, 100)
	for i := range shortPrefix {
		shortPrefix[i] = 'x'
	}
	content2 := string(shortPrefix) + suffix
	if !detectXMLToolInvocation(content2) {
		t.Error("Should detect XML within first 500 runes")
	}
}

func TestDetectXML_CaseInsensitive(t *testing.T) {
	tests := []string{
		`<INVOKE NAME="SMARTSHELL"><PARAMETER NAME="COMMAND">ls</PARAMETER></INVOKE>`,
		`<Invoke Name="SmartShell"><Parameter Name="Command">ls</Parameter></Invoke>`,
		`<TOOL_CALL NAME="SMARTSHELL">ls</TOOL_CALL>`,
		`<Function_Call>{"name": "tool"}</Function_Call>`,
	}

	for _, content := range tests {
		if !detectXMLToolInvocation(content) {
			t.Errorf("Should detect XML case-insensitively: %q", content)
		}
	}
}

func TestDetectXML_AllKnownTools(t *testing.T) {
	// 使用全局 toolRegistryMap，保證與實際註冊工具完全同步
	for toolName := range toolRegistryMap {
		content := `<invoke name="` + toolName + `"><parameter name="command">test</parameter></invoke>`
		if !detectXMLToolInvocation(content) {
			t.Errorf("Should detect XML invoke for known tool %q", toolName)
		}
	}
}

// ============================================================================
// RunBranchNone — Guard Logic Tests
// ============================================================================

func TestRunBranchNone_EmptyRespContent(t *testing.T) {
	var xmlCount int
	var resume, subResume, todoReminder int
	var exited bool
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: ""},
	}
	dc := &dummyChannel{}

	result := RunBranchNone(messages, "", "", "",
		&xmlCount, &resume, &subResume, &todoReminder, &exited,
		dc, 1, 4096)

	if !result.ShouldBreak {
		t.Error("Empty response should trigger natural exit (ShouldBreak)")
	}
	if !exited {
		t.Error("loopExitedNaturally should be true for empty response")
	}
}

func TestRunBranchNone_XMLParseAndContinue(t *testing.T) {
	var xmlCount int
	var resume, subResume, todoReminder int
	var exited bool
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: `<invoke name="SmartShell"><parameter name="command">ls</parameter></invoke>`},
	}
	dc := &dummyChannel{}

	result := RunBranchNone(messages, messages[2].Content, "", "",
		&xmlCount, &resume, &subResume, &todoReminder, &exited,
		dc, 1, 4096)

	if !result.ShouldContinue {
		t.Error("XML detection should parse+execute and continue (ShouldContinue)")
	}
	if xmlCount != 1 {
		t.Errorf("xmlRePromptCount should be 1, got %d", xmlCount)
	}
	// 應該追加咗 tool result message，唔係 re-prompt message
	if len(result.Messages) <= len(messages) {
		t.Error("Should have appended tool result message(s)")
	}
}

func TestRunBranchNone_XMLLimitExceeded(t *testing.T) {
	var xmlCount int = maxXMLRePromptRounds + 1 // already exceeded
	var resume, subResume, todoReminder int
	var exited bool
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: `<invoke name="SmartShell"><parameter name="command">ls</parameter></invoke>`},
	}
	dc := &dummyChannel{}

	result := RunBranchNone(messages, messages[2].Content, "", "",
		&xmlCount, &resume, &subResume, &todoReminder, &exited,
		dc, 1, 4096)

	if result.ShouldContinue {
		t.Error("XML parse should not trigger when limit exceeded")
	}
	if !result.ShouldBreak {
		t.Error("Should break when XML limit exceeded and no other guards trigger")
	}
}

// ============================================================================
// parseInlineXMLToolCalls Tests
// ============================================================================

func TestParseInlineXMLToolCalls_Invoke(t *testing.T) {
	content := `<invoke name="SmartShell"><parameter name="command">ls -la</parameter><parameter name="mode">sync</parameter></invoke>`
	calls := parseInlineXMLToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "SmartShell" {
		t.Errorf("expected SmartShell, got %s", calls[0].Name)
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(calls[0].ArgsJSON), &args)
	if args["command"] != "ls -la" {
		t.Errorf("expected command 'ls -la', got '%v'", args["command"])
	}
}

func TestParseInlineXMLToolCalls_DSML(t *testing.T) {
	content := `<DSML_tool_calls>
<DSML_invoke name="TaskCheck">
<DSML_parameter name="TaskId">task_e91084df</DSML_parameter>
</DSML_invoke>
<DSML_invoke name="SmartShell">
<DSML_parameter name="command">ls</DSML_parameter>
<DSML_parameter name="mode">sync</DSML_parameter>
</DSML_invoke>
</DSML_tool_calls>`
	calls := parseInlineXMLToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls (from DSML_tool_calls wrapper), got %d", len(calls))
	}
	if calls[0].Name != "TaskCheck" {
		t.Errorf("expected TaskCheck somewhere, got %v", calls)
	}
	// Verify TaskId parameter gets parsed
	found := false
	for _, c := range calls {
		if c.Name == "TaskCheck" {
			var args map[string]interface{}
			json.Unmarshal([]byte(c.ArgsJSON), &args)
			if args["TaskId"] == "task_e91084df" {
				found = true
			}
		}
	}
	if !found {
		t.Error("TaskCheck with TaskId not found")
	}
}

func TestParseInlineXMLToolCalls_NoToolCalls(t *testing.T) {
	calls := parseInlineXMLToolCalls("普通聊天文字，無任何工具調用")
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseInlineXMLToolCalls_JSONArgs(t *testing.T) {
	content := `<invoke name="TestTool"><parameter name="config">{"key":"value","num":42}</parameter></invoke>`
	calls := parseInlineXMLToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(calls[0].ArgsJSON), &args)
	config, ok := args["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected config to be map, got %T", args["config"])
	}
	if config["num"] != float64(42) {
		t.Errorf("expected num 42, got %v", config["num"])
	}
}

func TestParseInlineXMLToolCalls_MixedQuotes(t *testing.T) {
	content := `<invoke name='ReadFileLines'><parameter name='filename'>/tmp/test</parameter></invoke>`
	calls := parseInlineXMLToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "ReadFileLines" {
		t.Errorf("expected ReadFileLines, got %s", calls[0].Name)
	}
}

func TestRunBranchNone_NormalExit(t *testing.T) {
	var xmlCount int
	var resume, subResume, todoReminder int
	var exited bool
	messages := []Message{
		{Role: "system", Content: "test"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "你好！我可以帮你什么？"},
	}
	dc := &dummyChannel{}

	result := RunBranchNone(messages, messages[2].Content, "", "",
		&xmlCount, &resume, &subResume, &todoReminder, &exited,
		dc, 1, 4096)

	if !result.ShouldBreak {
		t.Error("Normal text response should trigger natural exit (ShouldBreak)")
	}
	if !exited {
		t.Error("loopExitedNaturally should be true")
	}
}

// ============================================================================
// isToolUseStopReason Tests
// ============================================================================

func TestIsToolUseStopReason(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{"tool_use", true},
		{"function_call", true},
		{"tool_calls", true},
		{"stop", false},
		{"length", false},
		{"", false},
		{"TOOL_USE", false},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			if got := isToolUseStopReason(tt.reason); got != tt.want {
				t.Errorf("isToolUseStopReason(%q) = %v, want %v", tt.reason, got, tt.want)
			}
		})
	}
}
