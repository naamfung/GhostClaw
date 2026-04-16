package main

import (
        "bufio"
        "context"
        "fmt"
        "os"
        "regexp"
        "sort"
        "strconv"
        "strings"
)

// TextReplaceOptions 文本替换选项
type TextReplaceOptions struct {
        // 基本参数
        Text         string `json:"text"`           // 输入文本（优先使用）
        FilePath     string `json:"file_path"`      // 文件路径（可选，与 Text 二选一）
        Pattern      string `json:"pattern"`        // 搜索模式（字符串或正则表达式）
        Replacement  string `json:"replacement"`    // 替换文本（为空则删除匹配）
        OutputToFile string `json:"output_to_file"` // 输出到文件（可选，默认返回字符串）

        // 模式选项
        UseRegex     bool `json:"use_regex"`      // 使用正则表达式（默认 false，简单字符串匹配）
        IgnoreCase   bool `json:"ignore_case"`    // 忽略大小写（默认 false）
        Global       bool `json:"global"`         // 全局替换（默认 true，替换所有匹配）
        Multiline    bool `json:"multiline"`      // 多行模式（默认 true）

        // 行范围限制
        StartLine    int `json:"start_line"`    // 起始行号（1-based，0 表示从头开始）
        EndLine      int `json:"end_line"`      // 结束行号（0 表示到末尾）
        LinePattern  string `json:"line_pattern"`  // 只处理匹配此模式的行
        ExcludePattern string `json:"exclude_pattern"` // 排除匹配此模式的行

        // 操作类型
        Operation string `json:"operation"` // 操作类型：replace（替换）, delete（删除行）, print（打印匹配行）, count（计数）

        // 输出选项
        ShowLineNumbers bool `json:"show_line_numbers"` // 显示行号
        ShowChangesOnly bool `json:"show_changes_only"` // 只显示修改的行
        InPlace         bool `json:"in_place"`          // 原地修改文件（仅对文件有效）
        Backup          bool `json:"backup"`            // 修改前备份文件（仅对 in_place 有效）

        // 高级选项
        MaxReplacements int `json:"max_replacements"` // 每行最大替换次数（0 表示无限制）
        DryRun          bool `json:"dry_run"`          // 模拟运行，不实际修改
}

// TextReplaceResult 文本替换结果
type TextReplaceResult struct {
        Success      bool     `json:"success"`
        Output       string   `json:"output"`        // 输出文本
        LinesChanged int      `json:"lines_changed"` // 修改的行数
        TotalLines   int      `json:"total_lines"`   // 总行数
        MatchesFound int      `json:"matches_found"` // 匹配次数
        ChangedLines []string `json:"changed_lines"` // 修改的行（当 ShowChangesOnly 时）
        Error        string   `json:"error,omitempty"`
}

// handleTextReplace 处理文本替换工具调用
func handleTextReplace(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        // 解析参数
        opts := parseTextReplaceOptions(argsMap)

        // 验证参数
        if opts.Text == "" && opts.FilePath == "" {
                return "Error: 必须提供 'text' 或 'file_path' 参数", false
        }
        if opts.Pattern == "" && opts.Operation != "print" && opts.Operation != "count" {
                return "Error: 必须提供 'pattern' 参数", false
        }
        if opts.FilePath != "" && opts.InPlace && opts.OutputToFile != "" {
                return "Error: 'in_place' 和 'output_to_file' 不能同时使用", false
        }

        // 执行操作
        result := executeTextReplace(opts)

        // 返回结果
        if !result.Success {
                return fmt.Sprintf("Error: %s", result.Error), false
        }

        // 构建响应
        var sb strings.Builder
        sb.WriteString("✅ 操作完成\n")
        sb.WriteString(fmt.Sprintf("- 修改行数: %d\n", result.LinesChanged))
        sb.WriteString(fmt.Sprintf("- 总行数: %d\n", result.TotalLines))
        sb.WriteString(fmt.Sprintf("- 匹配次数: %d\n", result.MatchesFound))

        if opts.ShowChangesOnly && len(result.ChangedLines) > 0 {
                sb.WriteString("\n修改的行:\n")
                for _, line := range result.ChangedLines {
                        sb.WriteString(fmt.Sprintf("  %s\n", line))
                }
        }

        if result.Output != "" && !opts.InPlace && opts.OutputToFile == "" {
                sb.WriteString("\n输出:\n")
                sb.WriteString("---\n")
                // 限制输出长度
                if len(result.Output) > 10000 {
                        sb.WriteString(result.Output[:10000])
                        sb.WriteString("\n... (输出过长，已截断)")
                } else {
                        sb.WriteString(result.Output)
                }
                sb.WriteString("\n---")
        }

        return sb.String(), false
}

// parseTextReplaceOptions 解析参数
func parseTextReplaceOptions(argsMap map[string]interface{}) TextReplaceOptions {
        opts := TextReplaceOptions{
                Operation:    "replace",
                Global:       true,
                Multiline:    true,
                MaxReplacements: 0,
        }

        if v, ok := argsMap["text"].(string); ok {
                opts.Text = v
        }
        if v, ok := argsMap["file_path"].(string); ok {
                opts.FilePath = v
        }
        if v, ok := argsMap["pattern"].(string); ok {
                opts.Pattern = v
        }
        if v, ok := argsMap["replacement"].(string); ok {
                opts.Replacement = v
        }
        if v, ok := argsMap["output_to_file"].(string); ok {
                opts.OutputToFile = v
        }
        if v, ok := argsMap["use_regex"].(bool); ok {
                opts.UseRegex = v
        }
        if v, ok := argsMap["ignore_case"].(bool); ok {
                opts.IgnoreCase = v
        }
        if v, ok := argsMap["global"].(bool); ok {
                opts.Global = v
        }
        if v, ok := argsMap["multiline"].(bool); ok {
                opts.Multiline = v
        }
        if v, ok := argsMap["start_line"].(float64); ok {
                opts.StartLine = int(v)
        }
        if v, ok := argsMap["end_line"].(float64); ok {
                opts.EndLine = int(v)
        }
        if v, ok := argsMap["line_pattern"].(string); ok {
                opts.LinePattern = v
        }
        if v, ok := argsMap["exclude_pattern"].(string); ok {
                opts.ExcludePattern = v
        }
        if v, ok := argsMap["operation"].(string); ok {
                opts.Operation = v
        }
        if v, ok := argsMap["show_line_numbers"].(bool); ok {
                opts.ShowLineNumbers = v
        }
        if v, ok := argsMap["show_changes_only"].(bool); ok {
                opts.ShowChangesOnly = v
        }
        if v, ok := argsMap["in_place"].(bool); ok {
                opts.InPlace = v
        }
        if v, ok := argsMap["backup"].(bool); ok {
                opts.Backup = v
        }
        if v, ok := argsMap["max_replacements"].(float64); ok {
                opts.MaxReplacements = int(v)
        }
        if v, ok := argsMap["dry_run"].(bool); ok {
                opts.DryRun = v
        }

        return opts
}

// executeTextReplace 执行文本替换
func executeTextReplace(opts TextReplaceOptions) TextReplaceResult {
        result := TextReplaceResult{
                Success:      true,
                ChangedLines: make([]string, 0),
        }

        // 获取输入文本
        var inputText string
        var filePath string
        if opts.FilePath != "" {
                data, err := os.ReadFile(opts.FilePath)
                if err != nil {
                        result.Success = false
                        result.Error = fmt.Sprintf("无法读取文件: %v", err)
                        return result
                }
                inputText = string(data)
                filePath = opts.FilePath
        } else {
                inputText = opts.Text
        }

        // 分割为行
        lines := strings.Split(inputText, "\n")
        result.TotalLines = len(lines)

        // 编译正则表达式（如果需要）
        var patternRegex *regexp.Regexp
        var linePatternRegex *regexp.Regexp
        var excludePatternRegex *regexp.Regexp
        var err error

        if opts.UseRegex && opts.Pattern != "" {
                flags := ""
                if opts.IgnoreCase {
                        flags = "(?i)"
                }
                if opts.Multiline {
                        flags += "(?m)"
                }
                patternRegex, err = regexp.Compile(flags + opts.Pattern)
                if err != nil {
                        result.Success = false
                        result.Error = fmt.Sprintf("无效的正则表达式: %v", err)
                        return result
                }
        }

        if opts.LinePattern != "" {
                flags := ""
                if opts.IgnoreCase {
                        flags = "(?i)"
                }
                linePatternRegex, err = regexp.Compile(flags + opts.LinePattern)
                if err != nil {
                        result.Success = false
                        result.Error = fmt.Sprintf("无效的行模式: %v", err)
                        return result
                }
        }

        if opts.ExcludePattern != "" {
                flags := ""
                if opts.IgnoreCase {
                        flags = "(?i)"
                }
                excludePatternRegex, err = regexp.Compile(flags + opts.ExcludePattern)
                if err != nil {
                        result.Success = false
                        result.Error = fmt.Sprintf("无效的排除模式: %v", err)
                        return result
                }
        }

        // 处理每一行
        outputLines := make([]string, 0, len(lines))
        for lineNum, line := range lines {
                lineNum1Based := lineNum + 1

                // 检查行范围
                if opts.StartLine > 0 && lineNum1Based < opts.StartLine {
                        outputLines = append(outputLines, line)
                        continue
                }
                if opts.EndLine > 0 && lineNum1Based > opts.EndLine {
                        outputLines = append(outputLines, line)
                        continue
                }

                // 检查行模式
                if linePatternRegex != nil && !linePatternRegex.MatchString(line) {
                        outputLines = append(outputLines, line)
                        continue
                }
                if excludePatternRegex != nil && excludePatternRegex.MatchString(line) {
                        outputLines = append(outputLines, line)
                        continue
                }

                // 执行操作
                var newLine string
                var changed bool
                var matchesInLine int

                switch opts.Operation {
                case "delete":
                        // 删除匹配的行
                        matches := false
                        if opts.UseRegex && patternRegex != nil {
                                matches = patternRegex.MatchString(line)
                        } else if opts.Pattern != "" {
                                if opts.IgnoreCase {
                                        matches = strings.Contains(strings.ToLower(line), strings.ToLower(opts.Pattern))
                                } else {
                                        matches = strings.Contains(line, opts.Pattern)
                                }
                        }
                        if matches {
                                result.LinesChanged++
                                changed = true
                                continue // 不添加到输出
                        }
                        newLine = line

                case "print":
                        // 打印匹配的行
                        matches := false
                        if opts.UseRegex && patternRegex != nil {
                                matches = patternRegex.MatchString(line)
                        } else if opts.Pattern != "" {
                                if opts.IgnoreCase {
                                        matches = strings.Contains(strings.ToLower(line), strings.ToLower(opts.Pattern))
                                } else {
                                        matches = strings.Contains(line, opts.Pattern)
                                }
                        } else {
                                matches = true // 无模式时打印所有行
                        }
                        if matches {
                                if opts.ShowLineNumbers {
                                        result.ChangedLines = append(result.ChangedLines, fmt.Sprintf("%d: %s", lineNum1Based, line))
                                } else {
                                        result.ChangedLines = append(result.ChangedLines, line)
                                }
                                result.MatchesFound++
                        }
                        outputLines = append(outputLines, line)
                        continue

                case "count":
                        // 计数
                        if opts.UseRegex && patternRegex != nil {
                                matchesInLine = len(patternRegex.FindAllString(line, -1))
                        } else if opts.Pattern != "" {
                                if opts.IgnoreCase {
                                        matchesInLine = strings.Count(strings.ToLower(line), strings.ToLower(opts.Pattern))
                                } else {
                                        matchesInLine = strings.Count(line, opts.Pattern)
                                }
                        }
                        result.MatchesFound += matchesInLine
                        outputLines = append(outputLines, line)
                        continue

                default: // "replace"
                        newLine, changed, matchesInLine = replaceInLine(line, opts, patternRegex)
                        result.MatchesFound += matchesInLine
                }

                if changed {
                        result.LinesChanged++
                        if opts.ShowLineNumbers {
                                result.ChangedLines = append(result.ChangedLines, fmt.Sprintf("%d: %s -> %s", lineNum1Based, line, newLine))
                        } else {
                                result.ChangedLines = append(result.ChangedLines, fmt.Sprintf("%s -> %s", line, newLine))
                        }
                }

                outputLines = append(outputLines, newLine)
        }

        // 构建输出
        result.Output = strings.Join(outputLines, "\n")

        // 写入文件（如果需要）
        if !opts.DryRun {
                if opts.InPlace && filePath != "" {
                        if opts.Backup {
                                backupPath := filePath + ".bak"
                                if err := copyFile(filePath, backupPath); err != nil {
                                        result.Success = false
                                        result.Error = fmt.Sprintf("无法备份文件: %v", err)
                                        return result
                                }
                        }
                        if err := os.WriteFile(filePath, []byte(result.Output), 0644); err != nil {
                                result.Success = false
                                result.Error = fmt.Sprintf("无法写入文件: %v", err)
                                return result
                        }
                } else if opts.OutputToFile != "" {
                        if err := os.WriteFile(opts.OutputToFile, []byte(result.Output), 0644); err != nil {
                                result.Success = false
                                result.Error = fmt.Sprintf("无法写入输出文件: %v", err)
                                return result
                        }
                }
        }

        return result
}

// replaceInLine 在单行中执行替换
func replaceInLine(line string, opts TextReplaceOptions, patternRegex *regexp.Regexp) (string, bool, int) {
        originalLine := line
        matchesFound := 0

        if opts.UseRegex && patternRegex != nil {
                // 正则表达式替换
                if opts.Global {
                        // 全局替换
                        count := -1 // 无限制
                        if opts.MaxReplacements > 0 {
                                count = opts.MaxReplacements
                        }
                        newLine := patternRegex.ReplaceAllString(line, opts.Replacement)
                        matchesFound = len(patternRegex.FindAllString(line, count))
                        return newLine, newLine != originalLine, matchesFound
                } else {
                        // 仅替换第一个匹配
                        loc := patternRegex.FindStringIndex(line)
                        if loc != nil {
                                matchesFound = 1
                                newLine := line[:loc[0]] + opts.Replacement + line[loc[1]:]
                                return newLine, true, 1
                        }
                }
        } else {
                // 简单字符串替换
                searchPattern := opts.Pattern
                searchLine := line
                if opts.IgnoreCase {
                        searchPattern = strings.ToLower(opts.Pattern)
                        searchLine = strings.ToLower(line)
                }

                if strings.Contains(searchLine, searchPattern) {
                        if opts.Global {
                                // 全局替换
                                maxReplace := -1
                                if opts.MaxReplacements > 0 {
                                        maxReplace = opts.MaxReplacements
                                }

                                if opts.IgnoreCase {
                                        // 大小写不敏感的全局替换
                                        newLine, count := replaceAllCaseInsensitive(line, opts.Pattern, opts.Replacement, maxReplace)
                                        return newLine, count > 0, count
                                } else {
                                        count := strings.Count(line, opts.Pattern)
                                        if maxReplace > 0 && count > maxReplace {
                                                count = maxReplace
                                        }
                                        newLine := strings.Replace(line, opts.Pattern, opts.Replacement, count)
                                        matchesFound = count
                                        return newLine, newLine != originalLine, matchesFound
                                }
                        } else {
                                // 仅替换第一个匹配
                                if opts.IgnoreCase {
                                        newLine, count := replaceAllCaseInsensitive(line, opts.Pattern, opts.Replacement, 1)
                                        return newLine, count > 0, count
                                } else {
                                        newLine := strings.Replace(line, opts.Pattern, opts.Replacement, 1)
                                        return newLine, newLine != originalLine, 1
                                }
                        }
                }
        }

        return line, false, 0
}

// replaceAllCaseInsensitive 大小写不敏感的全局替换
func replaceAllCaseInsensitive(line, pattern, replacement string, maxReplace int) (string, int) {
        result := line
        count := 0

        lowerLine := strings.ToLower(line)
        lowerPattern := strings.ToLower(pattern)

        idx := 0
        for {
                pos := strings.Index(lowerLine[idx:], lowerPattern)
                if pos == -1 {
                        break
                }

                if maxReplace > 0 && count >= maxReplace {
                        break
                }

                actualPos := idx + pos
                result = result[:actualPos] + replacement + result[actualPos+len(pattern):]
                lowerLine = strings.ToLower(result)
                idx = actualPos + len(replacement)
                count++
        }

        return result, count
}

// copyFile 复制文件
func copyFile(src, dst string) error {
        data, err := os.ReadFile(src)
        if err != nil {
                return err
        }
        return os.WriteFile(dst, data, 0644)
}

// handleTextSearch 处理文本搜索（在文件中搜索）
func handleTextSearch(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        filePath, _ := argsMap["file_path"].(string)
        pattern, _ := argsMap["pattern"].(string)
        useRegex, _ := argsMap["use_regex"].(bool)
        ignoreCase, _ := argsMap["ignore_case"].(bool)
        showLineNumbers, _ := argsMap["show_line_numbers"].(bool)
        contextLines := 0
        if v, ok := argsMap["context_lines"].(float64); ok {
                contextLines = int(v)
        }
        maxResults := 100
        if v, ok := argsMap["max_results"].(float64); ok {
                maxResults = int(v)
        }

        if filePath == "" {
                return "Error: 必须提供 'file_path' 参数", false
        }
        if pattern == "" {
                return "Error: 必须提供 'pattern' 参数", false
        }

        data, err := os.ReadFile(filePath)
        if err != nil {
                return fmt.Sprintf("Error: 无法读取文件: %v", err), false
        }

        // 标记文件已部分读取（先读后写安全检查 - 搜索仅读取匹配行，不满足全量写入要求）
        globalReadWriteTracker.MarkFilePartialRead(filePath)

        lines := strings.Split(string(data), "\n")

        // 编译正则表达式
        var patternRegex *regexp.Regexp
        if useRegex {
                flags := ""
                if ignoreCase {
                        flags = "(?i)"
                }
                patternRegex, err = regexp.Compile(flags + pattern)
                if err != nil {
                        return fmt.Sprintf("Error: 无效的正则表达式: %v", err), false
                }
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("📋 搜索结果: %s\n\n", filePath))
        sb.WriteString(fmt.Sprintf("模式: %s\n\n", pattern))

        matches := 0
        for lineNum, line := range lines {
                if matches >= maxResults {
                        sb.WriteString(fmt.Sprintf("\n... 已达到最大结果数 (%d)", maxResults))
                        break
                }

                matched := false
                if useRegex && patternRegex != nil {
                        matched = patternRegex.MatchString(line)
                } else {
                        if ignoreCase {
                                matched = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
                        } else {
                                matched = strings.Contains(line, pattern)
                        }
                }

                if matched {
                        matches++
                        if showLineNumbers {
                                sb.WriteString(fmt.Sprintf("%d: %s\n", lineNum+1, line))
                        } else {
                                sb.WriteString(fmt.Sprintf("%s\n", line))
                        }

                        // 显示上下文行
                        if contextLines > 0 {
                                // 前面的上下文
                                for i := lineNum - contextLines; i < lineNum; i++ {
                                        if i >= 0 {
                                                sb.WriteString(fmt.Sprintf("  %d: %s\n", i+1, lines[i]))
                                        }
                                }
                                // 后面的上下文
                                for i := lineNum + 1; i <= lineNum+contextLines && i < len(lines); i++ {
                                        sb.WriteString(fmt.Sprintf("  %d: %s\n", i+1, lines[i]))
                                }
                                sb.WriteString("\n")
                        }
                }
        }

        sb.WriteString(fmt.Sprintf("\n共找到 %d 个匹配", matches))
        return sb.String(), false
}

// handleTextTransform 处理文本转换（大小写转换、行操作等）
func handleTextTransform(ctx context.Context, argsMap map[string]interface{}, ch Channel) (string, bool) {
        text, _ := argsMap["text"].(string)
        filePath, _ := argsMap["file_path"].(string)
        transform, _ := argsMap["transform"].(string) // uppercase, lowercase, trim, sort, unique, reverse, number_lines
        startLine := 0
        if v, ok := argsMap["start_line"].(float64); ok {
                startLine = int(v)
        }
        endLine := 0
        if v, ok := argsMap["end_line"].(float64); ok {
                endLine = int(v)
        }

        if text == "" && filePath == "" {
                return "Error: 必须提供 'text' 或 'file_path' 参数", false
        }

        var input string
        if filePath != "" {
                data, err := os.ReadFile(filePath)
                if err != nil {
                        return fmt.Sprintf("Error: 无法读取文件: %v", err), false
                }
                input = string(data)
                // 標記為部分讀取，允許後續寫入操作通過安全檢查
                globalReadWriteTracker.MarkFilePartialRead(filePath)
        } else {
                input = text
        }

        lines := strings.Split(input, "\n")

        // 应用行范围
        if startLine > 0 || endLine > 0 {
                start := 0
                if startLine > 0 {
                        start = startLine - 1
                }
                end := len(lines)
                if endLine > 0 && endLine < len(lines) {
                        end = endLine
                }
                if start < len(lines) {
                        lines = lines[start:end]
                }
        }

        // 执行转换
        switch transform {
        case "uppercase", "upper", "to_upper":
                for i, line := range lines {
                        lines[i] = strings.ToUpper(line)
                }
        case "lowercase", "lower", "to_lower":
                for i, line := range lines {
                        lines[i] = strings.ToLower(line)
                }
        case "trim":
                for i, line := range lines {
                        lines[i] = strings.TrimSpace(line)
                }
        case "sort":
                // 排序行
                sorted := make([]string, len(lines))
                copy(sorted, lines)
                sort.Strings(sorted)
                lines = sorted
        case "unique", "uniq":
                // 移除重复行
                seen := make(map[string]bool)
                unique := make([]string, 0)
                for _, line := range lines {
                        if !seen[line] {
                                seen[line] = true
                                unique = append(unique, line)
                        }
                }
                lines = unique
        case "reverse":
                // 反转行顺序
                for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
                        lines[i], lines[j] = lines[j], lines[i]
                }
        case "number_lines", "nl":
                // 添加行号
                numbered := make([]string, len(lines))
                for i, line := range lines {
                        numbered[i] = fmt.Sprintf("%6d\t%s", i+1, line)
                }
                lines = numbered
        case "remove_empty", "remove_blank":
                // 移除空行
                nonEmpty := make([]string, 0)
                for _, line := range lines {
                        if strings.TrimSpace(line) != "" {
                                nonEmpty = append(nonEmpty, line)
                        }
                }
                lines = nonEmpty
        default:
                return fmt.Sprintf("Error: 未知的转换类型: %s (可用: uppercase, lowercase, trim, sort, unique, reverse, number_lines, remove_empty)", transform), false
        }

        result := strings.Join(lines, "\n")
        return fmt.Sprintf("✅ 转换完成\n\n结果:\n%s", result), false
}

// parseIntOrDefault 解析整数参数
func parseIntOrDefault(argsMap map[string]interface{}, key string, defaultVal int) int {
        if v, ok := argsMap[key].(float64); ok {
                return int(v)
        }
        if v, ok := argsMap[key].(string); ok {
                if i, err := strconv.Atoi(v); err == nil {
                        return i
                }
        }
        return defaultVal
}

// parseBoolOrDefault 解析布尔参数
func parseBoolOrDefault(argsMap map[string]interface{}, key string, defaultVal bool) bool {
        if v, ok := argsMap[key].(bool); ok {
                return v
        }
        if v, ok := argsMap[key].(string); ok {
                return strings.ToLower(v) == "true"
        }
        return defaultVal
}

// 辅助函数：读取文件行
func readFileLines(filePath string) ([]string, error) {
        file, err := os.Open(filePath)
        if err != nil {
                return nil, err
        }
        defer file.Close()

        var lines []string
        scanner := bufio.NewScanner(file)
        for scanner.Scan() {
                lines = append(lines, scanner.Text())
        }
        return lines, scanner.Err()
}
