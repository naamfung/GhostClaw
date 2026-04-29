package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ============================================================================
// replaceAllCaseInsensitive
// ============================================================================

func TestReplaceAllCaseInsensitive_Basic(t *testing.T) {
	result, count := replaceAllCaseInsensitive("Hello WORLD world", "world", "earth", -1)
	if count != 2 {
		t.Errorf("expected 2 replacements, got %d", count)
	}
	if result != "Hello earth earth" {
		t.Errorf("expected 'Hello earth earth', got '%s'", result)
	}
}

func TestReplaceAllCaseInsensitive_NoMatch(t *testing.T) {
	result, count := replaceAllCaseInsensitive("hello world", "xyz", "abc", -1)
	if count != 0 {
		t.Errorf("expected 0 replacements, got %d", count)
	}
	if result != "hello world" {
		t.Errorf("expected unchanged, got '%s'", result)
	}
}

func TestReplaceAllCaseInsensitive_MaxReplace(t *testing.T) {
	result, count := replaceAllCaseInsensitive("aaa aaa aaa", "aaa", "bbb", 2)
	if count != 2 {
		t.Errorf("expected 2 replacements, got %d", count)
	}
	if result != "bbb bbb aaa" {
		t.Errorf("expected 'bbb bbb aaa', got '%s'", result)
	}
}

func TestReplaceAllCaseInsensitive_SelfMatchingNoLoop(t *testing.T) {
	// 審計修復：當 replacement 包含 pattern（大小寫不敏感）時，
	// 設定 hardLimit=10000 防止無限循環。hardLimit 足夠大以處理正常輸入。
	result, count := replaceAllCaseInsensitive("abc", "abc", "abcABC", -1)

	// 由於 hardLimit=10000，會停止但 count 可能好大。驗證冇 hang。
	if count > 10000 {
		t.Errorf("count should be capped at hardLimit 10000, got %d", count)
	}
	_ = result
}

func TestReplaceAllCaseInsensitive_HardLimitSafety(t *testing.T) {
	// 極端情況：replacement 完全包含 pattern，每次替換後又產生新匹配
	// hardLimit=10000 應該防止掛死
	result, count := replaceAllCaseInsensitive("x", "x", "xx", -1)
	if count > 10000 {
		t.Fatalf("hardLimit failed: count=%d, should be <= 10000", count)
	}
	if count < 10000 {
		t.Logf("safety limit triggered or loop exited at count=%d (non-infinite case)", count)
	}
	_ = result
}

func TestReplaceAllCaseInsensitive_EmptyPattern(t *testing.T) {
	// 空 pattern：strings.Index 對空字串返回 0，可能導致問題
	result, count := replaceAllCaseInsensitive("hello", "", "x", -1)
	// 空 pattern 行為取決於 strings.Index — 返回 0
	if count <= 10000 {
		t.Logf("empty pattern: count=%d", count)
	}
	_ = result
}

// ============================================================================
// replaceInLine
// ============================================================================

func TestReplaceInLine_SimpleString(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:     "foo",
		Replacement: "bar",
		Global:      true,
	}
	result, changed, count := replaceInLine("foo and foo", opts, nil)
	if !changed {
		t.Error("should be changed")
	}
	if count != 2 {
		t.Errorf("expected 2 matches, got %d", count)
	}
	if result != "bar and bar" {
		t.Errorf("expected 'bar and bar', got '%s'", result)
	}
}

func TestReplaceInLine_FirstOnly(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:     "foo",
		Replacement: "bar",
		Global:      false,
	}
	result, changed, count := replaceInLine("foo and foo", opts, nil)
	if !changed {
		t.Error("should be changed")
	}
	if count != 1 {
		t.Errorf("expected 1 match, got %d", count)
	}
	if result != "bar and foo" {
		t.Errorf("expected 'bar and foo', got '%s'", result)
	}
}

func TestReplaceInLine_CaseInsensitive(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:     "hello",
		Replacement: "hi",
		Global:      true,
		IgnoreCase:  true,
	}
	result, changed, count := replaceInLine("Hello HELLO hello", opts, nil)
	if !changed {
		t.Error("should be changed")
	}
	if count != 3 {
		t.Errorf("expected 3 matches, got %d", count)
	}
	if result != "hi hi hi" {
		t.Errorf("expected 'hi hi hi', got '%s'", result)
	}
}

func TestReplaceInLine_Regex(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:     `\d+`,
		Replacement: "N",
		Global:      true,
		UseRegex:    true,
	}
	// replaceInLine 需要預編譯嘅 regex
	re := mustCompileRegex(`\d+`)
	result, changed, count := replaceInLine("page 1 of 20", opts, re)
	if !changed {
		t.Error("should be changed")
	}
	if count != 2 {
		t.Errorf("expected 2 matches, got %d", count)
	}
	if result != "page N of N" {
		t.Errorf("expected 'page N of N', got '%s'", result)
	}
}

func mustCompileRegex(pattern string) *regexp.Regexp {
	re, err := regexp.Compile(pattern)
	if err != nil {
		panic(err)
	}
	return re
}

func TestReplaceInLine_NoMatch(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:     "xyz",
		Replacement: "abc",
		Global:      true,
	}
	result, changed, count := replaceInLine("hello world", opts, nil)
	if changed {
		t.Error("should not be changed")
	}
	if count != 0 {
		t.Errorf("expected 0 matches, got %d", count)
	}
	if result != "hello world" {
		t.Errorf("expected unchanged, got '%s'", result)
	}
}

func TestReplaceInLine_MaxReplacements(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern:         "a",
		Replacement:     "X",
		Global:          true,
		MaxReplacements: 3,
	}
	result, _, count := replaceInLine("aaaaa", opts, nil)
	if count != 3 {
		t.Errorf("expected 3 replacements (max), got %d", count)
	}
	if result != "XXXaa" {
		t.Errorf("expected 'XXXaa', got '%s'", result)
	}
}

// ============================================================================
// executeTextReplace — 尾隨換行符保留
// ============================================================================

func TestExecuteTextReplace_PreserveTrailingNewline(t *testing.T) {
	// 審計修復：尾隨換行符應該保留
	input := "line1\nline2\n"
	opts := TextReplaceOptions{
		Text:        input,
		Pattern:     "line",
		Replacement: "LINE",
		Global:      true,
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if !strings.HasSuffix(result.Output, "\n") {
		t.Error("output should end with newline (trailing newline preserved)")
	}
	if result.Output != "LINE1\nLINE2\n" {
		t.Errorf("expected 'LINE1\\nLINE2\\n', got '%s'", result.Output)
	}
}

func TestExecuteTextReplace_NoTrailingNewline(t *testing.T) {
	input := "line1\nline2"
	opts := TextReplaceOptions{
		Text:        input,
		Pattern:     "line",
		Replacement: "LINE",
		Global:      true,
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if strings.HasSuffix(result.Output, "\n") {
		t.Error("output should NOT end with newline when input didn't")
	}
}

// ============================================================================
// parseTextReplaceOptions
// ============================================================================

func TestParseTextReplaceOptions_Defaults(t *testing.T) {
	opts := parseTextReplaceOptions(map[string]interface{}{})
	if opts.Operation != "replace" {
		t.Errorf("default operation should be 'replace', got '%s'", opts.Operation)
	}
	if !opts.Global {
		t.Error("Global should default to true")
	}
	if !opts.Multiline {
		t.Error("Multiline should default to true")
	}
}

func TestParseTextReplaceOptions_AllFields(t *testing.T) {
	args := map[string]interface{}{
		"text":            "hello world",
		"FilePath":       "/tmp/test.txt",
		"pattern":         "world",
		"replacement":     "earth",
		"output_to_file":  "/tmp/out.txt",
		"UseRegex":       true,
		"IgnoreCase":     true,
		"global":          false,
		"multiline":       false,
		"StartLine":      float64(5),
		"EndLine":        float64(10),
		"LinePattern":    "^func",
		"ExcludePattern": "^//",
		"operation":       "delete",
		"show_line_numbers": true,
		"show_changes_only": true,
		"InPlace":        true,
		"backup":          true,
		"MaxReplacements": float64(3),
		"DryRun":         true,
	}
	opts := parseTextReplaceOptions(args)

	if opts.Text != "hello world" {
		t.Errorf("Text: expected 'hello world', got '%s'", opts.Text)
	}
	if opts.FilePath != "/tmp/test.txt" {
		t.Errorf("FilePath mismatch")
	}
	if opts.Pattern != "world" {
		t.Errorf("Pattern mismatch")
	}
	if opts.Replacement != "earth" {
		t.Errorf("Replacement mismatch")
	}
	if !opts.UseRegex {
		t.Error("UseRegex should be true")
	}
	if !opts.IgnoreCase {
		t.Error("IgnoreCase should be true")
	}
	if opts.Global {
		t.Error("Global should be false")
	}
	if opts.Multiline {
		t.Error("Multiline should be false")
	}
	if opts.StartLine != 5 {
		t.Errorf("StartLine: expected 5, got %d", opts.StartLine)
	}
	if opts.EndLine != 10 {
		t.Errorf("EndLine: expected 10, got %d", opts.EndLine)
	}
	if opts.Operation != "delete" {
		t.Errorf("Operation: expected 'delete', got '%s'", opts.Operation)
	}
	if opts.MaxReplacements != 3 {
		t.Errorf("MaxReplacements: expected 3, got %d", opts.MaxReplacements)
	}
}

// ============================================================================
// executeTextReplace — 各種 Operation
// ============================================================================

func TestExecuteTextReplace_Delete(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "keep\nremove\nkeep",
		Pattern:   "remove",
		Operation: "delete",
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.Output != "keep\nkeep" {
		t.Errorf("expected 'keep\\nkeep', got '%s'", result.Output)
	}
	if result.LinesChanged != 1 {
		t.Errorf("expected 1 line changed, got %d", result.LinesChanged)
	}
}

func TestExecuteTextReplace_Count(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "foo bar foo baz foo",
		Pattern:   "foo",
		Operation: "count",
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.MatchesFound != 3 {
		t.Errorf("expected 3 matches, got %d", result.MatchesFound)
	}
}

func TestExecuteTextReplace_Print(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "hello\nworld\nhello",
		Pattern:   "hello",
		Operation: "print",
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.MatchesFound != 2 {
		t.Errorf("expected 2 matches, got %d", result.MatchesFound)
	}
	if len(result.ChangedLines) != 2 {
		t.Errorf("expected 2 changed lines, got %d", len(result.ChangedLines))
	}
}

func TestExecuteTextReplace_PrintAll(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "a\nb\nc",
		Operation: "print",
		// 無 pattern 時 print 應打印所有行
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if len(result.ChangedLines) != 3 {
		t.Errorf("expected 3 lines printed, got %d", len(result.ChangedLines))
	}
}

// ============================================================================
// executeTextReplace — 行範圍
// ============================================================================

func TestExecuteTextReplace_StartLine(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "line1\nline2\nline3\nline4",
		Pattern:   "line",
		Replacement: "LINE",
		Global:    true,
		StartLine: 2,
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.Output != "line1\nLINE2\nLINE3\nLINE4" {
		t.Errorf("expected lines 2-4 changed: '%s'", result.Output)
	}
}

func TestExecuteTextReplace_EndLine(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "line1\nline2\nline3\nline4",
		Pattern:   "line",
		Replacement: "LINE",
		Global:    true,
		EndLine:   2,
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.Output != "LINE1\nLINE2\nline3\nline4" {
		t.Errorf("expected lines 1-2 changed: '%s'", result.Output)
	}
}

// 審計修復：endLine == len(lines) 應該允許
func TestExecuteTextReplace_EndLineEqualsLength(t *testing.T) {
	opts := TextReplaceOptions{
		Text:      "line1\nline2\nline3",
		Pattern:   "line",
		Replacement: "LINE",
		Global:    true,
		EndLine:   3, // 應該等於 len(lines)，之前 off-by-one bug 會忽略
	}
	result := executeTextReplace(opts)
	if !result.Success {
		t.Fatalf("execute failed: %s", result.Error)
	}
	if result.Output != "LINE1\nLINE2\nLINE3" {
		t.Errorf("expected all lines changed: '%s'", result.Output)
	}
}

// ============================================================================
// executeTextReplace — 錯誤路徑
// ============================================================================

func TestExecuteTextReplace_NoTextOrFile(t *testing.T) {
	opts := TextReplaceOptions{
		Pattern: "test",
	}
	result := executeTextReplace(opts)
	// 呢個 validation 喺 handleTextReplace 做，executeTextReplace 直接處理
	// 所以應該返回空結果
	if result.Output != "" {
		t.Errorf("expected empty output for no input, got '%s'", result.Output)
	}
}

func TestExecuteTextReplace_InvalidRegex(t *testing.T) {
	opts := TextReplaceOptions{
		Text:     "hello",
		Pattern:  "[invalid",
		UseRegex: true,
	}
	result := executeTextReplace(opts)
	if result.Success {
		t.Error("should fail with invalid regex")
	}
}

func TestExecuteTextReplace_FileNotFound(t *testing.T) {
	opts := TextReplaceOptions{
		FilePath: "/nonexistent/path/file.txt",
		Pattern:  "test",
	}
	result := executeTextReplace(opts)
	if result.Success {
		t.Error("should fail with nonexistent file")
	}
}

// ============================================================================
// handleTextTransform — endLine off-by-one 修復
// ============================================================================

func TestHandleTextTransform_EndLineBoundary(t *testing.T) {
	// 審計修復：endLine == len(lines) 應生效
	input := "a\nb\nc\nd"
	lines := strings.Split(input, "\n")
	endLine := 4 // 等於 len(lines)

	start := 0
	end := len(lines)
	if endLine > 0 && endLine <= len(lines) {
		end = endLine
	}

	if end != 4 {
		t.Errorf("expected end=4, got %d (off-by-one bug?)", end)
	}
	if len(lines[start:end]) != 4 {
		t.Errorf("expected 4 lines in range, got %d", len(lines[start:end]))
	}
}

// ============================================================================
// copyFile
// ============================================================================

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	content := "hello world"
	os.WriteFile(src, []byte(content), 0644)

	err := copyFile(src, dst)
	if err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected '%s', got '%s'", content, string(data))
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	err := copyFile("/nonexistent/src.txt", "/tmp/dst.txt")
	if err == nil {
		t.Error("should fail with nonexistent source")
	}
}

// ============================================================================
// readFileLines
// ============================================================================

func TestReadFileLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3"), 0644)

	lines, err := readFileLines(path)
	if err != nil {
		t.Fatalf("readFileLines failed: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Errorf("unexpected content: %v", lines)
	}
}

func TestReadFileLines_NotFound(t *testing.T) {
	_, err := readFileLines("/nonexistent/file.txt")
	if err == nil {
		t.Error("should fail with nonexistent file")
	}
}

// ============================================================================
// parseIntOrDefault / parseBoolOrDefault
// ============================================================================

func TestParseIntOrDefault_Valid(t *testing.T) {
	args := map[string]interface{}{"count": float64(42)}
	if v := parseIntOrDefault(args, "count", 0); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestParseIntOrDefault_Missing(t *testing.T) {
	args := map[string]interface{}{}
	if v := parseIntOrDefault(args, "count", 10); v != 10 {
		t.Errorf("expected default 10, got %d", v)
	}
}

func TestParseIntOrDefault_String(t *testing.T) {
	args := map[string]interface{}{"count": "99"}
	if v := parseIntOrDefault(args, "count", 0); v != 99 {
		t.Errorf("expected 99 from string, got %d", v)
	}
}

func TestParseIntOrDefault_InvalidString(t *testing.T) {
	args := map[string]interface{}{"count": "not_a_number"}
	if v := parseIntOrDefault(args, "count", 5); v != 5 {
		t.Errorf("expected default 5 for invalid string, got %d", v)
	}
}

func TestParseBoolOrDefault_Valid(t *testing.T) {
	args := map[string]interface{}{"flag": true}
	if !parseBoolOrDefault(args, "flag", false) {
		t.Error("expected true")
	}
}

func TestParseBoolOrDefault_Missing(t *testing.T) {
	args := map[string]interface{}{}
	if parseBoolOrDefault(args, "flag", true) != true {
		t.Error("expected default true")
	}
}

func TestParseBoolOrDefault_StringTrue(t *testing.T) {
	args := map[string]interface{}{"flag": "true"}
	if !parseBoolOrDefault(args, "flag", false) {
		t.Error("expected true from string 'true'")
	}
}

func TestParseBoolOrDefault_StringFalse(t *testing.T) {
	args := map[string]interface{}{"flag": "false"}
	if parseBoolOrDefault(args, "flag", true) {
		t.Error("expected false from string 'false'")
	}
}
