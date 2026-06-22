package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// cleanControlChars 清理字符串中的非法控制字符
// 保留正常的换行符(\n)、回车符(\r)、制表符(\t)等常用控制字符
// 移除其他可能导致读取问题的控制字符（如 NULL、BEL、BS 等）
func cleanControlChars(s string) string {
	var result strings.Builder
	for _, r := range s {
		// 保留有效的 Unicode 字符与常用控制字符
		if r == '\n' || r == '\r' || r == '\t' || r == '\f' || r == '\v' {
			// 保留常用空白控制字符
			result.WriteRune(r)
		} else if unicode.IsControl(r) {
			// 跳过其他控制字符（如 NULL \x00, BEL \x07, BS \x08 等）
			continue
		} else {
			// 保留所有非控制字符
			result.WriteRune(r)
		}
	}
	return result.String()
}

// ReadFileLine 读取文件的指定行（行号从1开始）
func ReadFileLine(filename string, lineNum int) (string, error) {
	if lineNum < 1 {
		return "", errors.New("line number must be >= 1")
	}

	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	const initialBufSize = 1024 * 1024   // 1MB
	const maxBufSize = 100 * 1024 * 1024 // 100MB
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)

	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine == lineNum {
			// 清理非法控制字符后返回
			return cleanControlChars(scanner.Text()), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", errors.New("line number out of range")
}

// ReadFileRange 读取文件的指定行范围（行号从1开始）
// startLine 和 endLine 都包含在内。若 endLine 为 0 或小于 startLine，则只读取 startLine 一行。
func ReadFileRange(filename string, startLine, endLine int) ([]string, error) {
	if startLine < 1 {
		return nil, errors.New("start_line must be >= 1")
	}
	if endLine == 0 || endLine < startLine {
		endLine = startLine
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	const initialBufSize = 1024 * 1024   // 1MB
	const maxBufSize = 100 * 1024 * 1024 // 100MB
	scanner.Buffer(make([]byte, initialBufSize), maxBufSize)

	var lines []string
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine < startLine {
			continue
		}
		if currentLine > endLine {
			break
		}
		lines = append(lines, cleanControlChars(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(lines) == 0 {
		return nil, errors.New("line range out of bounds")
	}

	return lines, nil
}

// WriteFileLine writes or inserts a single line at a specific position:
//
//	LineNum > 0  → overwrite line LineNum (existing behaviour)
//	LineNum = 0  → truncate / create empty file (caller handles)
//	LineNum < -1 → insert BEFORE line |LineNum|, shifting content down
//
// (LineNum == -1 is handled by the caller as "append to end" via AppendFileLine)
func WriteFileLine(filename string, lineNum int, content string) error {
	if lineNum == 0 {
		return errors.New("line number must not be 0 (use caller helper for empty file)")
	}

	// Insert mode: negative lineNum < -1
	if lineNum < -1 {
		return InsertFileLine(filename, -lineNum, content)
	}

	// Overwrite mode: lineNum >= 1
	if lineNum < 1 {
		return errors.New("line number must be >= 1 for overwrite mode")
	}

	// 读取文件所有行，如果文件不存在则视为空
	lines, err := ReadFileLines(filename)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		lines = []string{}
	}

	// 扩展行切片至足够长度
	if lineNum > len(lines) {
		needed := lineNum - len(lines)
		lines = append(lines, make([]string, needed)...)
	}
	lines[lineNum-1] = content

	return WriteFileLines(filename, lines)
}

// InsertFileLine inserts a single line of content BEFORE the specified line
// position, shifting all existing content at that position and below down by 1.
// insertBefore is 1-based (1 = insert before line 1, i.e. prepend).
func InsertFileLine(filename string, insertBefore int, content string) error {
	if insertBefore < 1 {
		return errors.New("insert position must be >= 1")
	}

	lines, err := ReadFileLines(filename)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		lines = []string{}
	}

	// Insert before position <insertBefore>. If insertBefore is beyond the
	// file and the file has content, extend with empty lines first.
	if insertBefore > len(lines) && len(lines) > 0 {
		needed := insertBefore - len(lines)
		lines = append(lines, make([]string, needed)...)
	}
	// For empty files (or insert beyond end), clamp idx to the end
	if insertBefore > len(lines) {
		insertBefore = len(lines) + 1
	}

	idx := insertBefore - 1
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:idx]...)
	result = append(result, content)
	result = append(result, lines[idx:]...)

	return WriteFileLines(filename, result)
}

// ReadFileLines 读取文件所有行，返回字符串切片
// 若文件不存在，返回空切片与 nil 错误
func ReadFileLines(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 10*1024*1024)
	const maxBufSize = 100 * 1024 * 1024
	scanner.Buffer(buf, maxBufSize)

	for scanner.Scan() {
		// 清理非法控制字符后添加到行列表
		lines = append(lines, cleanControlChars(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// WriteFileLines 将字符串切片写入文件（覆盖原有内容）
// Uses atomic write: writes to a .tmp file first, then renames to prevent corruption.
func WriteFileLines(filename string, lines []string) error {
	tmpFile := filename + ".tmp"
	file, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			file.Close()
			os.Remove(tmpFile)
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		os.Remove(tmpFile)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpFile)
		return err
	}
	return os.Rename(tmpFile, filename)
}

// AppendAllLines appends multiple lines to the end of a file
func AppendAllLines(filename string, lines []string) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}

// AppendFileLine appends a single line to the end of a file
func AppendFileLine(filename string, content string) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	_, err = writer.WriteString(content + "\n")
	if err != nil {
		return err
	}
	return writer.Flush()
}

// WriteFileRange writes to or inserts into a specific range of lines:
//
//	Overwrite mode   — StartLine >= 1: replaces lines StartLine..EndLine with content
//	Insert mode      — StartLine < 0 : inserts content BEFORE line |StartLine|,
//	                    shifting existing lines down. EndLine is ignored in insert mode.
func WriteFileRange(filename string, startLine, endLine int, content string) error {
	// 分割内容为行
	newLines := strings.Split(content, "\n")
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	// Insert mode: startLine < 0 → insert before |startLine|
	if startLine < 0 {
		insertBefore := -startLine
		if insertBefore < 1 {
			return fmt.Errorf("invalid insert position: start_line=%d", startLine)
		}
		lines, err := ReadFileLines(filename)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if os.IsNotExist(err) {
			lines = []string{}
		}
		if insertBefore > len(lines) {
			needed := insertBefore - len(lines)
			lines = append(lines, make([]string, needed)...)
		}
		idx := insertBefore - 1
		result := make([]string, 0, len(lines)+len(newLines))
		result = append(result, lines[:idx]...)
		result = append(result, newLines...)
		result = append(result, lines[idx:]...)
		return WriteFileLines(filename, result)
	}

	// Overwrite mode: startLine >= 1
	lines, err := ReadFileLines(filename)
	if err != nil {
		return err
	}
	if startLine < 1 || startLine > len(lines) {
		return fmt.Errorf("start_line out of range (1-%d)", len(lines))
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	result := make([]string, 0, len(lines)-(endLine-startLine+1)+len(newLines))
	result = append(result, lines[:startLine-1]...)
	result = append(result, newLines...)
	result = append(result, lines[endLine:]...)

	return WriteFileLines(filename, result)
}

// TextSearchResult 表示单个文本搜索结果
type TextSearchResult struct {
	FilePath  string `toon:"FilePath" json:"FilePath"`
	LineNum   int    `toon:"LineNum" json:"LineNum"`
	LineText  string `toon:"line_text" json:"line_text"`
	MatchText string `toon:"match_text" json:"match_text"`
}

// TextSearchOptions 文本搜索选项
type TextSearchOptions struct {
	RootDir        string   // 搜索根目录，默认为系统根目录或用户主目录
	FilePattern    string   // 文件名模式（glob），如 "*.go", "*.txt"，默认匹配所有文件
	IgnoreCase     bool     // 是否忽略大小写
	UseRegex       bool     // 是否使用正则表达式
	MaxDepth       int      // 最大搜索深度，0 表示无限制
	MaxResults     int      // 最大结果数，0 表示无限制
	ExcludeDirs    []string // 排除的目录名
	ExcludeFiles   []string // 排除的文件模式
	FollowSymlinks bool     // 是否跟随符号链接
}

// getDefaultRootDir 获取默认搜索根目录
func getDefaultRootDir() string {
	switch runtime.GOOS {
	case "windows":
		// Windows: 使用系统驱动器根目录
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return "C:\\"
	default:
		// Unix/Linux/macOS: 使用根目录，但优先用户主目录
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return "/"
	}
}

// getDefaultExcludeDirs 获取默认排除目录
func getDefaultExcludeDirs() []string {
	commonExcludes := []string{
		".git", ".svn", ".hg",
		"node_modules", "vendor",
		"__pycache__", ".pytest_cache",
		"build", "dist", "target", "out",
		".idea", ".vscode", ".vs",
		"bin", "obj",
	}

	switch runtime.GOOS {
	case "windows":
		return append(commonExcludes, "Windows", "Program Files", "Program Files (x86)", "$Recycle.Bin")
	case "darwin":
		return append(commonExcludes, "Library", "Applications", "System")
	default:
		return append(commonExcludes, "proc", "sys", "dev", "run", "tmp", "var")
	}
}

// isBinaryFile 检查文件是否为二进制文件
// 採用雙重檢測策略：先檢查 null byte（最可靠），再檢查非可打印字符比例
func isBinaryFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return true
	}
	defer file.Close()

	// 读取前 8192 字节进行检测
	buf := make([]byte, 8192)
	n, err := file.Read(buf)
	if n == 0 {
		return false // 空檔案不是二進制
	}
	if err != nil {
		return true
	}
	buf = buf[:n]

	// 策略 1：檢查是否包含空字節（二進制文件的典型特徵）
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}

	// 策略 2：檢查非可打印字符比例
	// 超過 30% 非可打印字符（排除常見空白字符）視為二進制
	nonPrintable := 0
	for _, b := range buf {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' && b != '\v' {
			nonPrintable++
		} else if b == 0x7F {
			nonPrintable++
		}
	}
	if n > 0 && float64(nonPrintable)/float64(n) > 0.3 {
		return true
	}

	return false
}

// getFileTypeDescription 獲取文件類型描述
// 優先使用系統的 file 命令，失敗時回退到基本檔案資訊
func getFileTypeDescription(path string) string {
	// 先獲取基本檔案資訊，確保檔案存在且可讀
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("無法讀取檔案: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("⚠️ 此文件為二進制文件，無法以純文字形式顯示內容。\n\n")
	sb.WriteString(fmt.Sprintf("**檔案名稱**: %s\n", filepath.Base(path)))
	sb.WriteString(fmt.Sprintf("**檔案大小**: %s\n", formatFileSize(info.Size())))
	sb.WriteString(fmt.Sprintf("**修改時間**: %s\n", info.ModTime().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**副檔名**: %s\n", filepath.Ext(path)))

	// 嘗試使用系統的 file 命令獲取 MIME 類型
	mimeType := detectMIMEType(path)
	if mimeType != "" {
		sb.WriteString(fmt.Sprintf("**MIME 類型**: %s\n", mimeType))
	}

	// 嘗試使用系統的 file 命令獲取詳細描述
	fileDesc := runFileCommand(path)
	if fileDesc != "" {
		sb.WriteString(fmt.Sprintf("**檔案類型**: %s\n", fileDesc))
	}

	sb.WriteString("\n💡 **建議**: 如需查看或操作此二進制文件的內容，可以使用以下工具：\n")
	sb.WriteString("- 使用 `SmartShell` 執行 `xxd <file>`、`hexdump -C <file>` 或 `od -A x -t x1z <file>` 查看十六進制內容\n")
	sb.WriteString("- 使用 `SmartShell` 執行 `file <file>` 獲取詳細文件類型信息\n")

	return sb.String()
}

// detectMIMEType 檢測文件的 MIME 類型
func detectMIMEType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	// 讀取前 512 字節用於 magic bytes 檢測
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	buf = buf[:n]

	return detectMIMEFromBytes(buf)
}

// detectMIMEFromBytes 從字節內容檢測 MIME 類型
func detectMIMEFromBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// 常見 magic bytes 檢測
	switch {
	// 圖片
	case len(data) >= 4 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 6 && data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return "image/gif"
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "image/bmp"
	case len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F':
		return "image/webp" // 部分匹配
	case len(data) >= 4 && data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 && data[3] == 0x00:
		return "image/x-icon"

	// 壓縮檔案
	case len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B:
		return "application/gzip"
	case len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04:
		return "application/zip"
	case len(data) >= 6 && data[0] == 'R' && data[1] == 'a' && data[2] == 'r' && data[3] == '!':
		return "application/x-rar"
	case len(data) >= 5 && data[0] == 0x42 && data[1] == 0x5A && data[2] == 0x68:
		return "application/x-bzip2"
	case len(data) >= 6 && data[0] == 0xFD && data[1] == '7' && data[2] == 'z' && data[3] == 'X' && data[4] == 'Z':
		return "application/x-7z-compressed"

	// 二進制可執行檔
	case len(data) >= 4 && data[0] == 0x7F && data[1] == 'E' && data[2] == 'L' && data[3] == 'F':
		return "application/x-elf"
	case len(data) >= 2 && data[0] == 'M' && data[1] == 'Z':
		return "application/x-msdos-program" // PE/EXE
	case len(data) >= 4 && data[0] == 0xCA && data[1] == 0xFE && data[2] == 0xBA && data[3] == 0xBE:
		return "application/java-vm" // Mach-O

	// 文件格式
	case len(data) >= 5 && data[0] == '%' && data[1] == 'P' && data[2] == 'D' && data[3] == 'F' && data[4] == '-':
		return "application/pdf"
	case len(data) >= 8 && data[0] == 0xD0 && data[1] == 0xCF && data[2] == 0x11 && data[3] == 0xE0:
		return "application/msword" // DOC/XLS/PPT (OLE2)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return "text/plain; charset=UTF-16BE"
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return "text/plain; charset=UTF-16LE"
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return "text/plain; charset=UTF-8-BOM"

	// 音頻/視頻
	case len(data) >= 4 && data[0] == 'O' && data[1] == 'g' && data[2] == 'g' && data[3] == 'S':
		return "video/ogg"
	case len(data) >= 4 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F':
		if len(data) >= 12 && data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
			return "image/webp"
		}
		return "application/x-riff"

	// 憑證/密鑰
	case len(data) >= 27 && string(data[:27]) == "-----BEGIN CERTIFICATE-----":
		return "application/x-pem-certificate"
	case len(data) >= 10 && string(data[:10]) == "-----BEGIN":
		return "application/x-pem-file"
	}

	return ""
}

// runFileCommand 執行系統的 file 命令獲取檔案類型
func runFileCommand(path string) string {
	filePath := "/usr/bin/file"
	if runtime.GOOS == "freebsd" {
		filePath = "/usr/local/bin/file" // FreeBSD 上 file 可能在 /usr/local/bin
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// file 命令不可用
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, filePath, "-b", path)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// formatFileSize 格式化文件大小為人類可讀格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// shouldExclude 检查路径是否应该被排除
func shouldExclude(path string, excludeDirs, excludeFiles []string) bool {
	// 获取路径的各个部分
	parts := strings.Split(filepath.ToSlash(path), "/")

	// 检查目录排除
	for _, excludeDir := range excludeDirs {
		for _, part := range parts {
			if part == excludeDir {
				return true
			}
		}
	}

	// 检查文件名模式排除
	filename := filepath.Base(path)
	for _, pattern := range excludeFiles {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}

	return false
}

// TextSearch 执行文本搜索
// 当未指定 RootDir 时，采用级联搜索策略：
// 先从当前工作目录开始搜索，若无结果则逐级向上（父目录）直到根目录 /
// 这避免了模型在模糊指令下从 / 全局搜索导致的性能问题
func TextSearch(keyword string, opts TextSearchOptions) ([]TextSearchResult, error) {
	if keyword == "" {
		return nil, errors.New("keyword cannot be empty")
	}

	// 记录是否由调用者显式指定了 root_dir
	explicitRoot := opts.RootDir != ""

	// 设置默认值
	if opts.RootDir == "" {
		if globalOriginalWorkingDir != "" {
			opts.RootDir = globalOriginalWorkingDir
		} else {
			opts.RootDir = getDefaultRootDir()
		}
	}
	if len(opts.ExcludeDirs) == 0 {
		opts.ExcludeDirs = getDefaultExcludeDirs()
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = 1000 // 默认限制 1000 条结果
	}
	if opts.MaxDepth == 0 {
		opts.MaxDepth = 20 // 默认最大深度 20
	}

	// 准备搜索模式（只构建一次，级联搜索时复用）
	pattern, err := buildSearchPattern(keyword, opts)
	if err != nil {
		return nil, err
	}

	// 在起始目录执行搜索
	results, err := searchInDir(opts.RootDir, pattern, opts)
	if err != nil {
		return results, err
	}

	// 级联回退：如果未显式指定 root_dir 且当前目录无结果，
	// 逐级向上搜索（父目录 → 祖父目录 → ... → /）
	if !explicitRoot && len(results) == 0 {
		currentDir := opts.RootDir
		for {
			parent := filepath.Dir(currentDir)
			if parent == currentDir {
				break // 已到达文件系统根目录，无法继续向上
			}
			currentDir = parent

			cascadeResults, cascadeErr := searchInDir(currentDir, pattern, opts)
			if cascadeErr != nil {
				// 跳过无法访问的目录（如权限不足），继续向上
				continue
			}
			results = cascadeResults
			if len(results) > 0 {
				break // 找到结果，停止级联
			}
		}
	}

	return results, err
}

// buildSearchPattern 根据选项构建搜索正则表达式
func buildSearchPattern(keyword string, opts TextSearchOptions) (*regexp.Regexp, error) {
	searchKeyword := keyword
	if opts.UseRegex {
		if opts.IgnoreCase {
			searchKeyword = "(?i)" + keyword
		}
		return regexp.Compile(searchKeyword)
	} else if opts.IgnoreCase {
		searchKeyword = "(?i)" + regexp.QuoteMeta(keyword)
		return regexp.Compile(searchKeyword)
	}
	return regexp.Compile(regexp.QuoteMeta(keyword))
}

// searchInDir 在指定目录中执行文本搜索的 WalkDir 核心逻辑
func searchInDir(rootDir string, pattern *regexp.Regexp, opts TextSearchOptions) ([]TextSearchResult, error) {
	var results []TextSearchResult

	// 遍历文件系统
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 跳过无法访问的目录/文件
			return nil
		}

		// 计算当前深度
		relPath, _ := filepath.Rel(rootDir, path)
		depth := strings.Count(relPath, string(filepath.Separator))
		if depth > opts.MaxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// 检查排除规则
		if shouldExclude(path, opts.ExcludeDirs, opts.ExcludeFiles) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// 只处理普通文件
		if d.IsDir() {
			return nil
		}

		// 处理符号链接
		if d.Type()&fs.ModeSymlink != 0 && !opts.FollowSymlinks {
			return nil
		}

		// 检查文件名模式
		if opts.FilePattern != "" {
			matched, _ := filepath.Match(opts.FilePattern, d.Name())
			if !matched {
				return nil
			}
		}

		// 跳过二进制文件
		if isBinaryFile(path) {
			return nil
		}

		// 打开文件搜索
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 10*1024*1024)
		const maxBufSize = 100 * 1024 * 1024
		scanner.Buffer(buf, maxBufSize)

		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			// 搜索匹配
			matches := pattern.FindAllStringIndex(line, -1)
			for _, match := range matches {
				if len(results) >= opts.MaxResults {
					return errors.New("max results reached")
				}

				start, end := match[0], match[1]
				matchText := line[start:end]

				results = append(results, TextSearchResult{
					FilePath:  path,
					LineNum:   lineNum,
					LineText:  line,
					MatchText: matchText,
				})
			}
		}

		return nil
	})

	// 忽略"最大结果"错误
	if err != nil && err.Error() == "max results reached" {
		err = nil
	}

	return results, err
}
