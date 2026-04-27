package main

import (
        "bufio"
        "errors"
        "fmt"
        "io/fs"
        "os"
        "path/filepath"
        "regexp"
        "runtime"
        "strings"
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

// WriteFileLine 写入文件的指定行（替换原内容），若行号超出则自动扩展
func WriteFileLine(filename string, lineNum int, content string) error {
        if lineNum < 1 {
                return errors.New("line number must be >= 1")
        }

        // 读取文件所有行，如果文件不存在则视为空
        lines, err := ReadAllLines(filename)
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

        return WriteAllLines(filename, lines)
}

// ReadAllLines 读取文件所有行，返回字符串切片
// 若文件不存在，返回空切片与 nil 错误
func ReadAllLines(filename string) ([]string, error) {
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

// WriteAllLines 将字符串切片写入文件（覆盖原有内容）
// Uses atomic write: writes to a .tmp file first, then renames to prevent corruption.
func WriteAllLines(filename string, lines []string) error {
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

// WriteFileRange writes content to a specific range of lines in a file
func WriteFileRange(filename string, startLine, endLine int, content string) error {
        // 读取文件内容
        lines, err := ReadAllLines(filename)
        if err != nil {
                return err
        }

        // 检查行号范围
        if startLine < 1 || startLine > len(lines) {
                return fmt.Errorf("start_line out of range (1-%d)", len(lines))
        }
        if endLine < startLine {
                endLine = startLine
        }
        if endLine > len(lines) {
                endLine = len(lines)
        }

        // 分割内容为行
        newLines := strings.Split(content, "\n")
        // 移除最后一个空行（如果有）
        if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
                newLines = newLines[:len(newLines)-1]
        }

        // 替换指定范围的行
        result := make([]string, 0, len(lines)- (endLine-startLine+1) + len(newLines))
        result = append(result, lines[:startLine-1]...)
        result = append(result, newLines...)
        result = append(result, lines[endLine:]...)

        // 写回文件
        return WriteAllLines(filename, result)
}

// TextSearchResult 表示单个文本搜索结果
type TextSearchResult struct {
        FilePath  string `toon:"file_path" json:"file_path"`
        LineNum   int    `toon:"line_num" json:"line_num"`
        LineText  string `toon:"line_text" json:"line_text"`
        MatchText string `toon:"match_text" json:"match_text"`
}

// TextSearchOptions 文本搜索选项
type TextSearchOptions struct {
        RootDir         string   // 搜索根目录，默认为系统根目录或用户主目录
        FilePattern     string   // 文件名模式（glob），如 "*.go", "*.txt"，默认匹配所有文件
        IgnoreCase      bool     // 是否忽略大小写
        UseRegex        bool     // 是否使用正则表达式
        MaxDepth        int      // 最大搜索深度，0 表示无限制
        MaxResults      int      // 最大结果数，0 表示无限制
        ExcludeDirs     []string // 排除的目录名
        ExcludeFiles    []string // 排除的文件模式
        FollowSymlinks  bool     // 是否跟随符号链接
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
func isBinaryFile(path string) bool {
        file, err := os.Open(path)
        if err != nil {
                return true
        }
        defer file.Close()

        // 读取前 512 字节进行检测
        buf := make([]byte, 512)
        n, err := file.Read(buf)
        if err != nil {
                return true
        }

        // 检查是否包含空字节（二进制文件的典型特征）
        for i := 0; i < n; i++ {
                if buf[i] == 0 {
                        return true
                }
        }

        return false
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
func TextSearch(keyword string, opts TextSearchOptions) ([]TextSearchResult, error) {
        if keyword == "" {
                return nil, errors.New("keyword cannot be empty")
        }

        // 设置默认值
        if opts.RootDir == "" {
                opts.RootDir = getDefaultRootDir()
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

        // 准备匹配模式
        var pattern *regexp.Regexp
        var err error
        
        searchKeyword := keyword
        if opts.UseRegex {
                if opts.IgnoreCase {
                        searchKeyword = "(?i)" + keyword
                }
                pattern, err = regexp.Compile(searchKeyword)
                if err != nil {
                        return nil, fmt.Errorf("invalid regex pattern: %v", err)
                }
        } else if opts.IgnoreCase {
                searchKeyword = "(?i)" + regexp.QuoteMeta(keyword)
                pattern, err = regexp.Compile(searchKeyword)
                if err != nil {
                        return nil, fmt.Errorf("invalid regex pattern: %v", err)
                }
        } else {
                pattern, err = regexp.Compile(regexp.QuoteMeta(keyword))
                if err != nil {
                        return nil, fmt.Errorf("invalid regex pattern: %v", err)
                }
        }

        var results []TextSearchResult

        // 遍历文件系统
        err = filepath.WalkDir(opts.RootDir, func(path string, d fs.DirEntry, err error) error {
                if err != nil {
                        // 跳过无法访问的目录/文件
                        return nil
                }

                // 计算当前深度
                relPath, _ := filepath.Rel(opts.RootDir, path)
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

