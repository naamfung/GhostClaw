package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanControlChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "正常文本",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "包含换行符",
			input:    "Line1\nLine2\nLine3",
			expected: "Line1\nLine2\nLine3",
		},
		{
			name:     "包含制表符",
			input:    "Column1\tColumn2\tColumn3",
			expected: "Column1\tColumn2\tColumn3",
		},
		{
			name:     "包含NULL字符",
			input:    "Hello\x00World",
			expected: "HelloWorld",
		},
		{
			name:     "包含BEL字符",
			input:    "Hello\x07World",
			expected: "HelloWorld",
		},
		{
			name:     "包含BS字符",
			input:    "Hello\x08World",
			expected: "HelloWorld",
		},
		{
			name:     "包含多种控制字符",
			input:    "\x00Hello\x07\x08World\x00\nTest\x07",
			expected: "HelloWorld\nTest",
		},
		{
			name:     "包含回车换行",
			input:    "Line1\r\nLine2",
			expected: "Line1\r\nLine2",
		},
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
		{
			name:     "只有控制字符",
			input:    "\x00\x01\x02\x03\x04\x05\x06\x07",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanControlChars(tt.input)
			if result != tt.expected {
				t.Errorf("cleanControlChars() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// ============================================================================
// isBinaryFile
// ============================================================================

func TestIsBinaryFile_TextFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("Hello World\nThis is a test.\n"), 0644)

	if isBinaryFile(path) {
		t.Error("text file should not be detected as binary")
	}
}

func TestIsBinaryFile_NullBytes(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "binary.bin")
	data := []byte("text\x00\x00\x00more")
	os.WriteFile(path, data, 0644)

	if !isBinaryFile(path) {
		t.Error("file with null bytes should be detected as binary")
	}
}

func TestIsBinaryFile_HighNonPrintableRatio(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "binary.dat")
	// 創建一個包含大量非可打印字符但沒有 null byte 的檔案
	data := make([]byte, 1000)
	nonPrintable := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x10, 0x11, 0x12, 0x13}
	for i := 0; i < 500; i++ {
		data[i] = nonPrintable[i%len(nonPrintable)]
	}
	for i := 500; i < 1000; i++ {
		data[i] = 'A'
	}
	os.WriteFile(path, data, 0644)

	if !isBinaryFile(path) {
		t.Error("file with >30% non-printable chars should be binary")
	}
}

func TestIsBinaryFile_NonExistent(t *testing.T) {
	if !isBinaryFile("/tmp/nonexistent_file_xyz_test.bin") {
		t.Error("non-existent file should be treated as binary (safety)")
	}
}

func TestIsBinaryFile_UTF8WithSpecialChars(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "unicode.txt")
	os.WriteFile(path, []byte("你好世界\nテスト\n한국어\n🎉🎊\n"), 0644)

	if isBinaryFile(path) {
		t.Error("valid UTF-8 text with unicode should not be binary")
	}
}

func TestIsBinaryFile_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	os.WriteFile(path, []byte{}, 0644)

	if isBinaryFile(path) {
		t.Error("empty file should not be detected as binary")
	}
}

// ============================================================================
// detectMIMEFromBytes
// ============================================================================

func TestDetectMIMEFromBytes(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"GIF", []byte{'G', 'I', 'F', '8', '9', 'a'}, "image/gif"},
		{"BMP", []byte{'B', 'M', 0x00, 0x00}, "image/bmp"},
		{"GZIP", []byte{0x1F, 0x8B, 0x08}, "application/gzip"},
		{"ZIP", []byte{'P', 'K', 0x03, 0x04}, "application/zip"},
		{"ELF", []byte{0x7F, 'E', 'L', 'F'}, "application/x-elf"},
		{"PDF", []byte{'%', 'P', 'D', 'F', '-'}, "application/pdf"},
		{"UTF-8 BOM", []byte{0xEF, 0xBB, 0xBF, 'H', 'e', 'l', 'l', 'o'}, "text/plain; charset=UTF-8-BOM"},
		{"UTF-16 BE", []byte{0xFE, 0xFF}, "text/plain; charset=UTF-16BE"},
		{"UTF-16 LE", []byte{0xFF, 0xFE}, "text/plain; charset=UTF-16LE"},
		{"plain text", []byte("Hello World"), ""},
		{"empty", []byte{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectMIMEFromBytes(tt.data)
			if got != tt.want {
				t.Errorf("detectMIMEFromBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// formatFileSize
// ============================================================================

func TestFormatFileSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatFileSize(tt.size)
			if got != tt.want {
				t.Errorf("formatFileSize(%d) = %q, want %q", tt.size, got, tt.want)
			}
		})
	}
}

// ============================================================================
// isValidUTF8
// ============================================================================

func TestIsValidUTF8(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"ASCII only", []byte("hello"), true},
		{"2-byte UTF-8", []byte("café"), true},
		{"3-byte UTF-8 (CJK)", []byte("你好世界"), true},
		{"4-byte UTF-8 (emoji)", []byte("🎉"), true},
		{"mixed", []byte("Hello 世界 🎉"), true},
		{"invalid sequence", []byte{0xFF, 0xFE, 0x00, 0x00}, false},
		{"truncated 2-byte", []byte{0xC2}, false},
		{"truncated 3-byte", []byte{0xE2, 0x82}, false},
		{"binary data", []byte{0x80, 0x81, 0x82}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUTF8(tt.data)
			if got != tt.valid {
				t.Errorf("isValidUTF8(%v) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

// ============================================================================
// countLinesAndChars
// ============================================================================

func TestCountLinesAndChars(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

	lines, chars := countLinesAndChars(path)
	if lines != 3 {
		t.Errorf("lines = %d, want 3", lines)
	}
	if chars != 18 {
		t.Errorf("chars = %d, want 18", chars)
	}
}

func TestCountLinesAndChars_Empty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	os.WriteFile(path, []byte{}, 0644)

	lines, chars := countLinesAndChars(path)
	if lines != 0 {
		t.Errorf("lines = %d, want 0", lines)
	}
	if chars != 0 {
		t.Errorf("chars = %d, want 0", chars)
	}
}

// ============================================================================
// detectTextEncoding
// ============================================================================

func TestDetectTextEncoding_UTF8(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "utf8.txt")
	os.WriteFile(path, []byte("Hello World 你好"), 0644)

	enc := detectTextEncoding(path)
	if enc != "UTF-8" {
		t.Errorf("encoding = %q, want UTF-8", enc)
	}
}

func TestDetectTextEncoding_BOM(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bom.txt")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("Hello")...)
	os.WriteFile(path, data, 0644)

	enc := detectTextEncoding(path)
	if enc != "UTF-8 with BOM" {
		t.Errorf("encoding = %q, want UTF-8 with BOM", enc)
	}
}

func TestDetectTextEncoding_UTF16BE(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "utf16be.txt")
	os.WriteFile(path, []byte{0xFE, 0xFF, 0x00, 0x48}, 0644)

	enc := detectTextEncoding(path)
	if enc != "UTF-16 BE" {
		t.Errorf("encoding = %q, want UTF-16 BE", enc)
	}
}

// ============================================================================
// InsertFileLine tests
// ============================================================================

func TestInsertFileLine_MiddleOfFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"line1", "line2", "line3", "line4"})

	err := InsertFileLine(path, 3, "INSERTED")
	if err != nil {
		t.Fatalf("InsertFileLine() error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("line1 = %q, want \"line1\"", lines[0])
	}
	if lines[1] != "line2" {
		t.Errorf("line2 = %q, want \"line2\"", lines[1])
	}
	if lines[2] != "INSERTED" {
		t.Errorf("line3 = %q, want \"INSERTED\"", lines[2])
	}
	if lines[3] != "line3" {
		t.Errorf("line4 = %q, want \"line3\"", lines[3])
	}
	if lines[4] != "line4" {
		t.Errorf("line5 = %q, want \"line4\"", lines[4])
	}
}

func TestInsertFileLine_Prepend(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"first", "second"})

	err := InsertFileLine(path, 1, "NEW_FIRST")
	if err != nil {
		t.Fatalf("InsertFileLine() error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "NEW_FIRST" {
		t.Errorf("got %q, want \"NEW_FIRST\"", lines[0])
	}
	if lines[1] != "first" {
		t.Errorf("got %q, want \"first\"", lines[1])
	}
}

func TestInsertFileLine_NewFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	err := InsertFileLine(path, 1, "sole line")
	if err != nil {
		t.Fatalf("InsertFileLine() error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 1 || lines[0] != "sole line" {
		t.Errorf("got %v, want [\"sole line\"]", lines)
	}
}

// ============================================================================
// WriteFileLine insert mode tests
// ============================================================================

func TestWriteFileLine_InsertMode_BeforeLine2(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"A", "B", "C"})

	// LineNum = -2 → insert before line 2
	err := WriteFileLine(path, -2, "INSERTED")
	if err != nil {
		t.Fatalf("WriteFileLine(-2) error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "A" || lines[1] != "INSERTED" || lines[2] != "B" || lines[3] != "C" {
		t.Errorf("got %v, want [A, INSERTED, B, C]", lines)
	}
}

func TestWriteFileLine_OverwriteMode_LineN(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"A", "B", "C"})

	err := WriteFileLine(path, 2, "REPLACED")
	if err != nil {
		t.Fatalf("WriteFileLine(2) overwrite error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 3 || lines[1] != "REPLACED" {
		t.Errorf("got %v, want [A, REPLACED, C]", lines)
	}
}

// ============================================================================
// WriteFileRange insert mode tests
// ============================================================================

func TestWriteFileRange_InsertMode_MultiLine(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"A", "B", "C", "D"})

	// StartLine = -3 → insert 2 lines before line 3
	err := WriteFileRange(path, -3, 0, "X\nY")
	if err != nil {
		t.Fatalf("WriteFileRange(-3) insert error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "A" || lines[1] != "B" || lines[2] != "X" || lines[3] != "Y" || lines[4] != "C" || lines[5] != "D" {
		t.Errorf("got %v, want [A, B, X, Y, C, D]", lines)
	}
}

func TestWriteFileRange_OverwriteMode_Range(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"A", "B", "C", "D", "E"})

	err := WriteFileRange(path, 2, 4, "X\nY\nZ")
	if err != nil {
		t.Fatalf("WriteFileRange(2,4) overwrite error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "A" || lines[1] != "X" || lines[2] != "Y" || lines[3] != "Z" || lines[4] != "E" {
		t.Errorf("got %v, want [A, X, Y, Z, E]", lines)
	}
}

func TestWriteFileRange_InsertMode_SingleLine(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	WriteFileLines(path, []string{"header", "body", "footer"})

	// StartLine = -2 → insert 1 line before line 2
	err := WriteFileRange(path, -2, 0, "new_section")
	if err != nil {
		t.Fatalf("WriteFileRange(-2) insert error: %v", err)
	}

	lines, _ := ReadFileLines(path)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	if lines[0] != "header" || lines[1] != "new_section" || lines[2] != "body" || lines[3] != "footer" {
		t.Errorf("got %v, want [header, new_section, body, footer]", lines)
	}
}

