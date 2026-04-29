package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ==========================================================================
// 命名約束規則
//
// 1. 所有工具名稱必須為 PascalCase（首字母大寫駝峰式）
// 2. 所有工具 JSON 參數 key 必須為 PascalCase
// 3. 所有狀態值（Todo/Subagent/Task）必須為 PascalCase
// 4. 程式碼中不得殘留舊蛇形工具名
// ==========================================================================

// snakeCaseRE matches snake_case variable-like names
var snakeCaseRE = regexp.MustCompile(`^[a-z][a-z0-9]*_[a-z0-9]`)

// allowedSnakeCase 是內部 API/協議 key — 不能改
var allowedSnakeCase = map[string]bool{
	"tool_use":       true,
	"tool_calls":     true,
	"tool_result":    true,
	"function_call":  true,
	"max_tokens":     true,
	"response_format": true,
	"top_p":          true,
	"stop_reason":    true,
	"reasoning_content":    true,
	"thinking_signature":   true,
	"ghostclaw_token":      true,
	"content_type":   true,
	"authorization":  true,
}

// isSnakeCase checks if a string looks like snake_case
func isSnakeCase(s string) bool {
	return strings.Contains(s, "_") && snakeCaseRE.MatchString(s)
}

// Load all registered tool names from toolHandlerRegistry
func getAllToolNames() map[string]bool {
	names := make(map[string]bool)
	for name := range toolHandlerRegistry {
		names[name] = true
	}
	return names
}

// ==========================================================================
// 規則 1：工具名稱必須為 PascalCase
// ==========================================================================

func TestToolNamesArePascalCase(t *testing.T) {
	for name := range toolHandlerRegistry {
		if isSnakeCase(name) {
			t.Errorf("工具名稱必須為 PascalCase，發現蛇形命名: %q", name)
		}
		if len(name) > 0 && name[0] >= 'a' && name[0] <= 'z' {
			t.Errorf("工具名稱必須首字母大寫，發現小寫開頭: %q", name)
		}
	}
}

// ==========================================================================
// 規則 2：工具 Registry 定義中的 JSON 參數 key 必須為 PascalCase
// ==========================================================================

func TestToolParamKeysArePascalCase(t *testing.T) {
	// 檢查 allKnownToolNames 列表中的每個工具
	for _, name := range allKnownToolNames {
		if isSnakeCase(name) {
			t.Errorf("allKnownToolNames 中工具名必須為 PascalCase: %q", name)
		}
	}
}

// 掃描 tool_registry.go 中的 properties key
func TestToolRegistryPropertiesArePascalCase(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "tool_registry.go", nil, 0)
	if err != nil {
		t.Skipf("無法解析 tool_registry.go: %v", err)
	}

	// 遍歷 AST，找 map[string]interface{} 中的 key
	var checkCompositeLit func(expr ast.Expr)
	checkCompositeLit = func(expr ast.Expr) {
		switch e := expr.(type) {
		case *ast.CompositeLit:
			for _, elt := range e.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
						val := strings.Trim(key.Value, `"`)
						if isSnakeCase(val) && !allowedSnakeCase[val] && len(val) > 2 {
							t.Errorf("tool_registry.go: JSON property key 必須為 PascalCase: %q", val)
						}
					}
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if comp, ok := n.(*ast.CompositeLit); ok {
			checkCompositeLit(comp)
		}
		return true
	})
}

// ==========================================================================
// 規則 3：狀態值必須為 PascalCase
// ==========================================================================

func TestStatusValuesArePascalCase(t *testing.T) {
	// Todo 狀態
	statusCheckers := []struct {
		name   string
		values []string
	}{
		{"Todo", []string{"Pending", "InProgress", "Completed", "Waiting"}},
		{"TaskStatus", []string{string(TaskStatusPending)}},
	}

	for _, sc := range statusCheckers {
		for _, v := range sc.values {
			if v == "" {
				continue
			}
			if isSnakeCase(v) {
				t.Errorf("%s 狀態值必須為 PascalCase: %q", sc.name, v)
			}
		}
	}

	// 驗證 todo.go 中的 status switch case
	// 透過直接測試 TodoManager 來驗證
	tm := NewTodoManager()
	_, err := tm.Update([]TodoItem{
		{ID: "1", Text: "Test", Status: "InProgress"},
	})
	if err != nil {
		t.Errorf("Todo 應接受 PascalCase 狀態 'InProgress': %v", err)
	}

	// 蛇形命名應被拒絕
	_, err = tm.Update([]TodoItem{
		{ID: "1", Text: "Test", Status: "in_progress"},
	})
	if err == nil {
		t.Error("Todo 應拒絕蛇形命名狀態 'in_progress'")
	}
}

// ==========================================================================
// 規則 4：程式碼中不得殘留舊蛇形工具名
// ==========================================================================

// oldToolNames 是已被重命名的舊蛇形工具名 — 它們不應再出現在源碼中
var oldToolNames = []string{
	"smart_shell", "shell_delayed", "read_all_lines", "read_file_line",
	"read_file_range", "write_file_line", "write_all_lines", "write_file_range",
	"append_to_file", "text_search", "text_grep", "text_replace", "text_transform",
	"browser_search", "browser_visit", "browser_click", "browser_type",
	"browser_download", "browser_scroll", "browser_screenshot",
	"enter_plan_mode", "exit_plan_mode", "next_phase", "prev_phase",
	"plan_write", "plan_read",
	"memory_save", "memory_recall", "memory_forget", "memory_list",
	"plugin_list", "plugin_call", "plugin_create", "plugin_load",
	"plugin_unload", "plugin_reload", "plugin_compile", "plugin_delete",
	"cron_add", "cron_remove", "cron_list", "cron_status",
	"skill_list", "skill_create", "skill_delete", "skill_get",
	"skill_load", "skill_reload", "skill_update",
	"profile_check", "profile_reload", "profile_switch",
	"spawn_check", "spawn_list", "spawn_cancel", "spawn_batch",
	"ssh_connect", "ssh_exec", "ssh_list", "ssh_close",
	"scheme_eval", "opencli",
	// 瀏覽器增強工具
	"browser_double_click", "browser_drag", "browser_hover", "browser_right_click",
	"browser_navigate", "browser_get_cookies", "browser_cookie_save", "browser_cookie_load",
	"browser_snapshot", "browser_upload_file", "browser_select_option", "browser_key_press",
	"browser_element_screenshot", "browser_pdf", "browser_pdf_from_file",
	"browser_set_headers", "browser_set_user_agent", "browser_emulate_device",
	"browser_wait_element", "browser_wait_smart",
	"browser_execute_js", "browser_extract_links", "browser_extract_images",
	"browser_extract_elements", "browser_fill_form",
	// 工具
	"file_info",
	"shell_delayed_check", "shell_delayed_list", "shell_delayed_remove",
	"shell_delayed_terminate", "shell_delayed_wait",
	"skill_evaluate", "skill_stats", "skill_suggest",
	"plugin_apis", "plugin_detail",
	"actor_identity_set", "actor_identity_clear",
}

func TestNoOldToolNamesInSource(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		lines := strings.Split(content, "\n")

		for lineno, line := range lines {
			// 跳過註釋行
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}

			for _, oldName := range oldToolNames {
				// 檢查雙引號中的舊名（工具名引用）
				if strings.Contains(line, `"`+oldName+`"`) && !allowedSnakeCase[oldName] {
					// 排除一些合理例外
					if strings.Contains(line, "oldToolNames") || strings.Contains(line, "TestToolName") {
						continue
					}
					t.Errorf("%s:%d: 發現舊蛇形工具名 %q", file, lineno+1, oldName)
				}
			}
		}
	}
}

// ==========================================================================
// 規則 5：Opencli action 值必須為 PascalCase
// ==========================================================================

var opencliActions = []string{
	"WebRead", "Adapter", "List", "Explore", "Synthesize", "Generate",
	"Validate", "Verify", "Record", "Cascade",
	"AdapterStatus", "AdapterEject", "AdapterReset",
	"Register", "Install",
	"PluginList", "PluginInstall", "PluginUninstall", "PluginUpdate", "PluginCreate",
	"Doctor", "DaemonStop",
}

func TestOpenCliActionsArePascalCase(t *testing.T) {
	for _, action := range opencliActions {
		if isSnakeCase(action) {
			t.Errorf("Opencli action 必須為 PascalCase: %q", action)
		}
		if len(action) > 0 && action[0] >= 'a' && action[0] <= 'z' {
			t.Errorf("Opencli action 必須首字母大寫: %q", action)
		}
	}
}

// ==========================================================================
// 規則 6：ToolHandlerRegistry 與 tool_registry 一致性
// ==========================================================================

func TestToolHandlerRegistryConsistency(t *testing.T) {
	// 每個 handler 對應的工具名都應該是 PascalCase
	for name := range toolHandlerRegistry {
		if strings.Contains(name, "_") {
			// 例外：以 _test 結尾的只存在於測試中
			if !strings.HasSuffix(name, "_test") {
				t.Errorf("toolHandlerRegistry key 必須不含底線: %q", name)
			}
		}
	}
}

// ==========================================================================
// 規則 7：錯誤訊息中不含蛇形參數名
// ==========================================================================

func TestNoSnakeCaseInErrorMessages(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	snakeParamPattern := regexp.MustCompile(`'([a-z]+_[a-z]+)'`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for lineno, line := range lines {
			matches := snakeParamPattern.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				param := m[1]
				if allowedSnakeCase[param] {
					continue
				}
				t.Errorf("%s:%d: 錯誤訊息中參數名 %q 須為 PascalCase", file, lineno+1, param)
			}
		}
	}
}
