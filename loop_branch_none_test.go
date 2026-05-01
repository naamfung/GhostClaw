package main

import (
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
			name:    "invoke with EnterPlanMode",
			content: `<invoke name="EnterPlanMode"/>`,
			want:    true,
		},
		{
			name:    "invoke with todos",
			content: `<invoke name="todos"><parameter name="action">create</parameter></invoke>`,
			want:    true,
		},
		{
			name:    "invoke with grep",
			content: `<invoke name="grep"><parameter name="pattern">test</parameter></invoke>`,
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
	knownTools := []string{
		"SmartShell", "Shell", "ShellDelayed", "ReadAllLines", "ReadFileLine", "ReadFileRange",
		"write_file", "WriteFileLine", "WriteAllLines", "search_files",
		"EnterPlanMode", "Spawn", "SpawnCheck", "SpawnList", "SpawnBatch",
		"Menu", "todo", "Todos", "grep", "list_directory", "web_search",
		"BrowserNavigate", "BrowserClick", "BrowserType", "BrowserSnapshot",
		"mcp_call", "replace", "batch_replace", "file_exists",
	}

	for _, tool := range knownTools {
		t.Run(tool, func(t *testing.T) {
			content := `<invoke name="` + tool + `"><parameter name="command">test</parameter></invoke>`
			if !detectXMLToolInvocation(content) {
				t.Errorf("Should detect XML invoke for known tool %q", tool)
			}
		})
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

func TestRunBranchNone_XMLRePrompt(t *testing.T) {
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
		t.Error("XML detection should trigger re-prompt (ShouldContinue)")
	}
	if xmlCount != 1 {
		t.Errorf("xmlRePromptCount should be 1, got %d", xmlCount)
	}
	if len(result.Messages) <= len(messages) {
		t.Error("Should have appended re-prompt user message")
	}
}

func TestRunBranchNone_XMLRePromptLimit(t *testing.T) {
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
		t.Error("XML re-prompt should not trigger when limit exceeded")
	}
	// Should fall through to normal exit
	if !result.ShouldBreak {
		t.Error("Should break when XML limit exceeded and no other guards trigger")
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
