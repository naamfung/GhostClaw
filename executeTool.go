package main

import (
        "bufio"
        "context"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "strconv"
        "strings"
        "time"

        "github.com/toon-format/toon-go"
)

// --- Types ---

// ToolExecContext 工具执行上下文，传递给每个工具处理函数
type ToolExecContext struct {
        Ctx     context.Context
        ToolID  string
        ArgsMap map[string]interface{}
        Ch      Channel
        Role    *Role
}

// ToolHandler 工具处理函数类型
type ToolHandler func(ec *ToolExecContext) (string, TaskStatus)

// --- Handler functions ---

func execMenuTool(ec *ToolExecContext) (string, TaskStatus) {
        content := executeMenuTool(ec.ArgsMap)
        return content, TaskStatusSuccess
}

func execNextPhase(ec *ToolExecContext) (string, TaskStatus) {
        if globalPlanMode.IsActive() {
                phaseName, msg, err := AdvancePhase()
                if err != nil {
                        return "錯誤：" + err.Error(), TaskStatusFailed
                }
                _ = phaseName
                return msg, TaskStatusSuccess
        }
        return "錯誤：Plan Mode 未激活。", TaskStatusFailed
}

func execSmartShellTool(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSmartShell(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellTool(ec *ToolExecContext) (string, TaskStatus) {
        command, ok := ec.ArgsMap["command"].(string)
        if !ok || command == "" {
                return "Error: Invalid or empty command", TaskStatusFailed
        }

        force := false
        if forceVal, ok := ec.ArgsMap["force"].(bool); ok {
                force = forceVal
        }
        // SECURITY: The LLM must NOT be able to self-approve blocking commands.
        // The is_blocking_confirmed flag can only be set by a human via the confirmation UI.
        // Disabling this to prevent privilege escalation by the LLM.
        // if confirmedVal, ok := ec.ArgsMap["is_blocking_confirmed"].(bool); ok {
        //         isBlockingConfirmed = confirmedVal
        // }

        result := runShellWithTimeout(ec.Ctx, command, force, false)

        if result.ConfirmRequired {
                var confirmResult strings.Builder
                confirmResult.WriteString("⚠️ **确认请求**\n\n")
                confirmResult.WriteString(result.ConfirmMessage)
                confirmResult.WriteString("\n\n---\n")
                confirmResult.WriteString("要强制执行此命令，请使用: `shell(command=\"...\", force=true)`\n")
                confirmResult.WriteString("或使用建议的替代命令。")

                content := confirmResult.String()
                fmt.Println(content)
                return content, TaskStatusSuccess
        } else if result.Err != nil {
                // 注意：取消检查由 executeTool 统一处理
                content := fmt.Sprintf("Error: %v", result.Err)
                if result.Stderr != "" {
                        content += "\n" + result.Stderr
                        // 检查是否是未知命令错误
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
                                // 执行 opencli help 命令获取帮助信息
                                helpResult := runShellWithTimeout(ec.Ctx, "opencli help", false, false)
                                if helpResult.Err == nil {
                                        content += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
                                }
                        }
                }
                fmt.Println(content)
                return content, TaskStatusFailed
        } else {
                content := result.Stdout
                if result.ExitCode != 0 && result.Stderr != "" {
                        content += "\n" + result.Stderr
                        // 检查是否是未知命令错误
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
                                // 执行 opencli help 命令获取帮助信息
                                helpResult := runShellWithTimeout(ec.Ctx, "opencli help", false, false)
                                if helpResult.Err == nil {
                                        content += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
                                }
                        }
                        fmt.Println(content)
                        return content, TaskStatusFailed
                }
                fmt.Println(content)
                return content, TaskStatusSuccess
        }
}

func execSSHConnect(ec *ToolExecContext) (string, TaskStatus) {
        content, err := handleSSHConnect(ec.ArgsMap)
        if err != nil {
                return err.Error(), TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSSHExec(ec *ToolExecContext) (string, TaskStatus) {
        return handleSSHExec(ec.Ctx, ec.ArgsMap, ec.Ch)
}

func execSSHList(ec *ToolExecContext) (string, TaskStatus) {
        content, err := handleSSHList()
        if err != nil {
                return err.Error(), TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSSHClose(ec *ToolExecContext) (string, TaskStatus) {
        content, err := handleSSHClose(ec.ArgsMap)
        if err != nil {
                return err.Error(), TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execReadFileLine(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        lineNumFloat, ok2 := ec.ArgsMap["line_num"].(float64)
        if !ok1 || !ok2 || filename == "" || lineNumFloat < 1 {
                return "Error: Invalid arguments for read_file_line", TaskStatusFailed
        }

        lineNum := int(lineNumFloat)
        c, err := ReadFileLine(filename, lineNum)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 标记文件已部分读取（先读后写安全检查 - 部分读取不满足写入前置要求，仅作追踪）
        globalReadWriteTracker.MarkFilePartialRead(filename)

        // 检查是否需要详细信息
        verbose := false
        if v, ok := ec.ArgsMap["verbose"].(bool); ok {
                verbose = v
        }

        var content string
        if verbose {
                // 获取文件信息（防護 nil pointer：os.Stat 可能返回 nil info）
                info, infoErr := os.Stat(filename)
                if info == nil || infoErr != nil {
                        info = nil
                }
                result := map[string]interface{}{
                        "content":   c,
                        "line":      lineNum,
                        "filename":  filename,
                        "encoding":  "utf-8", // 假设 UTF-8 编码
                }
                if info != nil {
                        result["file_size"] = info.Size()
                        result["modified"] = info.ModTime().Format(time.RFC3339)
                }
                resultTOON, _ := toon.Marshal(result)
                content = string(resultTOON)
        } else {
                // 默认只返回内容
                content = c
        }
        fmt.Println(TruncateString(content, 200))
        return content, TaskStatusSuccess
}

func execWriteFileLine(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        lineNumFloat, ok2 := ec.ArgsMap["line_num"].(float64)
        text, ok3 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || !ok3 || filename == "" {
                return "Error: Invalid arguments for write_file_line", TaskStatusFailed
        }

        lineNum := int(lineNumFloat)
        var content string
        if lineNum == 0 {
                // 创建空文件
                file, err := os.Create(filename)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        file.Close()
                        content = "Successfully created empty file: " + filename
                }
        } else if lineNum < 0 {
                // 追加到文件末尾
                err := AppendFileLine(filename, text)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = "Successfully appended to end of file"
                }
        } else {
                // 写入指定行
                err := WriteFileLine(filename, lineNum, text)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = "Successfully wrote to line " + strconv.Itoa(lineNum)
                }
        }
        fmt.Println(content)
        return content, TaskStatusSuccess
}

func execReadAllLines(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok := ec.ArgsMap["filename"].(string)
        if !ok || filename == "" {
                return "Error: Invalid arguments for read_all_lines", TaskStatusFailed
        }

        lines, err := ReadAllLines(filename)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 标记文件已完整读取（先读后写安全检查 - 完整读取满足所有写入工具）
        globalReadWriteTracker.MarkFileFullyRead(filename)

        // 检查是否需要详细信息
        verbose := false
        if v, ok := ec.ArgsMap["verbose"].(bool); ok {
                verbose = v
        }

        var content string
        if verbose {
                // 获取文件信息
                info, infoErr := os.Stat(filename)
                if info == nil || infoErr != nil {
                        info = nil
                }

                // 构建带有行号的结果
                linedContent := make([]map[string]interface{}, len(lines))
                for i, line := range lines {
                        linedContent[i] = map[string]interface{}{
                                "line":    i + 1,
                                "content": line,
                        }
                }

                result := map[string]interface{}{
                        "lines":       linedContent,
                        "total_lines": len(lines),
                        "filename":    filename,
                        "encoding":    "utf-8", // 假设 UTF-8 编码
                }
                if info != nil {
                        result["file_size"] = info.Size()
                        result["modified"] = info.ModTime().Format(time.RFC3339)
                }

                resultTOON, err := toon.Marshal(result)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = string(resultTOON)
                }
        } else {
                // 默认只返回内容列表
                resultTOON, err := toon.Marshal(lines)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = string(resultTOON)
                }
        }
        fmt.Println(TruncateString(content, 200))
        return content, TaskStatusSuccess
}

func execReadFileRange(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        startLineFloat, ok2 := ec.ArgsMap["start_line"].(float64)
        if !ok1 || !ok2 || filename == "" || startLineFloat < 1 {
                return "Error: Invalid arguments for read_file_range", TaskStatusFailed
        }

        startLine := int(startLineFloat)
        endLine := startLine
        if endLineFloat, ok := ec.ArgsMap["end_line"].(float64); ok && endLineFloat >= float64(startLine) {
                endLine = int(endLineFloat)
        }

        lines, err := ReadFileRange(filename, startLine, endLine)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 标记文件已部分读取（先读后写安全检查 - 部分读取不满足写入前置要求，仅作追踪）
        globalReadWriteTracker.MarkFilePartialRead(filename)

        // 检查是否需要详细信息
        verbose := false
        if v, ok := ec.ArgsMap["verbose"].(bool); ok {
                verbose = v
        }

        var content string
        if verbose {
                // 获取文件信息
                info, infoErr := os.Stat(filename)
                if info == nil || infoErr != nil {
                        info = nil
                }

                // 构建带有行号的结果
                linedContent := make([]map[string]interface{}, len(lines))
                for i, line := range lines {
                        linedContent[i] = map[string]interface{}{
                                "line":    startLine + i,
                                "content": line,
                        }
                }

                result := map[string]interface{}{
                        "lines":       linedContent,
                        "total_lines": len(lines),
                        "start_line":  startLine,
                        "end_line":    endLine,
                        "filename":    filename,
                        "encoding":    "utf-8",
                }
                if info != nil {
                        result["file_size"] = info.Size()
                        result["modified"] = info.ModTime().Format(time.RFC3339)
                }

                resultTOON, err := toon.Marshal(result)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = string(resultTOON)
                }
        } else {
                // 默认只返回内容列表
                resultTOON, err := toon.Marshal(lines)
                if err != nil {
                        content = "Error: " + err.Error()
                } else {
                        content = string(resultTOON)
                }
        }
        fmt.Println(TruncateString(content, 200))
        return content, TaskStatusSuccess
}

func execWriteAllLines(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        linesInterface, ok2 := ec.ArgsMap["lines"].([]interface{})
        if !ok1 || !ok2 || filename == "" {
                return "Error: Invalid arguments for write_all_lines", TaskStatusFailed
        }

        lines := make([]string, len(linesInterface))
        for i, line := range linesInterface {
                if lineStr, ok := line.(string); ok {
                        lines[i] = lineStr
                } else {
                        content := fmt.Sprintf("Error: line %d is not a string", i)
                        return content, TaskStatusFailed
                }
        }

        appendMode := false
        if appendVal, ok := ec.ArgsMap["append"].(bool); ok {
                appendMode = appendVal
        }

        var err error
        if appendMode {
                err = AppendAllLines(filename, lines)
        } else {
                err = WriteAllLines(filename, lines)
        }

        var content string
        if err != nil {
                content = "Error: " + err.Error()
        } else {
                if appendMode {
                        content = "Successfully appended " + strconv.Itoa(len(lines)) + " lines to " + filename
                } else {
                        content = "Successfully wrote " + strconv.Itoa(len(lines)) + " lines to " + filename
                }
        }
        fmt.Println(content)
        return content, TaskStatusSuccess
}

func execAppendToFile(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        contentStr, ok2 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || filename == "" {
                return "Error: Invalid arguments for append_to_file", TaskStatusFailed
        }

        lineBreak := true
        if lineBreakVal, ok := ec.ArgsMap["line_break"].(bool); ok {
                lineBreak = lineBreakVal
        }

        file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }
        defer file.Close()

        writer := bufio.NewWriter(file)
        defer writer.Flush() // 確保任何提前返回時緩存數據也被刷新
        _, err = writer.WriteString(contentStr)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        if lineBreak {
                _, err = writer.WriteString("\n")
                if err != nil {
                        return "Error: " + err.Error(), TaskStatusFailed
                }
        }

        if err := writer.Flush(); err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        content := "Successfully appended content to " + filename
        fmt.Println(content)
        return content, TaskStatusSuccess
}

func execWriteFileRange(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        startLineFloat, ok2 := ec.ArgsMap["start_line"].(float64)
        contentStr, ok3 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || !ok3 || filename == "" || startLineFloat < 1 {
                return "Error: Invalid arguments for write_file_range", TaskStatusFailed
        }

        startLine := int(startLineFloat)
        endLine := startLine
        if endLineFloat, ok := ec.ArgsMap["end_line"].(float64); ok && endLineFloat >= float64(startLine) {
                endLine = int(endLineFloat)
        }

        err := WriteFileRange(filename, startLine, endLine, contentStr)
        var content string
        if err != nil {
                content = "Error: " + err.Error()
        } else {
                if startLine == endLine {
                        content = "Successfully wrote to line " + strconv.Itoa(startLine)
                } else {
                        content = "Successfully wrote to lines " + strconv.Itoa(startLine) + "-" + strconv.Itoa(endLine)
                }
        }
        fmt.Println(content)
        return content, TaskStatusSuccess
}

// ========== 浏览器工具 handlers ==========

func execBrowserSearch(ec *ToolExecContext) (string, TaskStatus) {
        keyword, ok := ec.ArgsMap["keyword"].(string)
        if !ok || keyword == "" {
                return "Error: Empty keyword in browser_search tool call", TaskStatusFailed
        }

        resultsList, err := Search(ec.Ch.GetSessionID(), keyword)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        } else if resultsList != nil {
                resultsTOON, err := toon.Marshal(resultsList)
                if err != nil {
                        log.Printf("Failed to marshal search results: %v", err)
                        return "Error: Failed to marshal search results", TaskStatusFailed
                }
                fmt.Println("Browser search completed")
                return string(resultsTOON), TaskStatusSuccess
        }
        fmt.Println("Browser search completed")
        return "No search results found", TaskStatusSuccess
}

func execBrowserVisit(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_visit tool call", TaskStatusFailed
        }

        result, err := Visit(ec.Ch.GetSessionID(), url)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, err := toon.Marshal(result)
        if err != nil {
                return "Error: Failed to marshal visit result", TaskStatusFailed
        }
        fmt.Println("Browser visit completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserDownload(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_download tool call", TaskStatusFailed
        }

        fileName, err := Download(ec.Ch.GetSessionID(), url)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        content := "Browser download completed, saved to: " + fileName
        fmt.Println(content)
        return content, TaskStatusSuccess
}

// ========== 浏览器增强工具 ==========

func execBrowserClick(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_click tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_click tool call", TaskStatusFailed
        }

        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserClick(ec.Ch.GetSessionID(), url, selector, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser click completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserType(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_type tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_type tool call", TaskStatusFailed
        }

        text, ok := ec.ArgsMap["text"].(string)
        if !ok {
                return "Error: Empty text in browser_type tool call", TaskStatusFailed
        }

        submit, _ := ec.ArgsMap["submit"].(bool)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserType(ec.Ch.GetSessionID(), url, selector, text, submit, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser type completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserScroll(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_scroll tool call", TaskStatusFailed
        }

        direction, ok := ec.ArgsMap["direction"].(string)
        if !ok || direction == "" {
                return "Error: Empty direction in browser_scroll tool call", TaskStatusFailed
        }

        amount := 500
        if a, ok := ec.ArgsMap["amount"].(float64); ok {
                amount = int(a)
        }
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserScroll(ec.Ch.GetSessionID(), url, direction, amount, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser scroll completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserWaitElement(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_wait_element tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_wait_element tool call", TaskStatusFailed
        }

        timeout := 10
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserWaitElement(ec.Ch.GetSessionID(), url, selector, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser wait element completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserExtractLinks(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_extract_links tool call", TaskStatusFailed
        }

        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserExtractLinks(ec.Ch.GetSessionID(), url, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser extract links completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserExtractImages(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_extract_images tool call", TaskStatusFailed
        }

        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserExtractImages(ec.Ch.GetSessionID(), url, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser extract images completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserExtractElements(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_extract_elements tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_extract_elements tool call", TaskStatusFailed
        }

        includeHTML, _ := ec.ArgsMap["include_html"].(bool)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserExtractElements(ec.Ch.GetSessionID(), url, selector, includeHTML, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser extract elements completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserScreenshot(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_screenshot tool call", TaskStatusFailed
        }

        fullPage, _ := ec.ArgsMap["full_page"].(bool)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserScreenshot(ec.Ch.GetSessionID(), url, fullPage, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser screenshot completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserExecuteJS(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_execute_js tool call", TaskStatusFailed
        }

        script, ok := ec.ArgsMap["script"].(string)
        if !ok || script == "" {
                return "Error: Empty script in browser_execute_js tool call", TaskStatusFailed
        }

        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserExecuteJS(ec.Ch.GetSessionID(), url, script, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser execute JS completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserFillForm(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_fill_form tool call", TaskStatusFailed
        }

        formDataRaw, ok := ec.ArgsMap["form_data"].(map[string]interface{})
        if !ok {
                return "Error: Invalid form_data in browser_fill_form tool call", TaskStatusFailed
        }

        formData := make(FormData)
        for k, v := range formDataRaw {
                if strVal, ok := v.(string); ok {
                        formData[k] = strVal
                }
        }

        submitSelector, _ := ec.ArgsMap["submit_selector"].(string)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserFillForm(ec.Ch.GetSessionID(), url, formData, submitSelector, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser fill form completed")
        return string(resultTOON), TaskStatusSuccess
}

// ========== 浏览器高级工具 ==========

func execBrowserHover(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_hover tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_hover tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserHover(ec.Ch.GetSessionID(), url, selector)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser hover completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserDoubleClick(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_double_click tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_double_click tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserDoubleClick(ec.Ch.GetSessionID(), url, selector)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser double click completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserRightClick(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_right_click tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_right_click tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserRightClick(ec.Ch.GetSessionID(), url, selector)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser right click completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserDrag(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_drag tool call", TaskStatusFailed
        }

        sourceSelector, ok := ec.ArgsMap["source_selector"].(string)
        if !ok || sourceSelector == "" {
                return "Error: Empty source_selector in browser_drag tool call", TaskStatusFailed
        }

        targetSelector, ok := ec.ArgsMap["target_selector"].(string)
        if !ok || targetSelector == "" {
                return "Error: Empty target_selector in browser_drag tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserDrag(ec.Ch.GetSessionID(), url, sourceSelector, targetSelector)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser drag completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserWaitSmart(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_wait_smart tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_wait_smart tool call", TaskStatusFailed
        }

        opts := BrowserWaitForOptions{
                Visible: true,
        }
        if v, ok := ec.ArgsMap["visible"].(bool); ok {
                opts.Visible = v
        }
        if v, ok := ec.ArgsMap["interactable"].(bool); ok {
                opts.Interactable = v
        }
        if v, ok := ec.ArgsMap["stable"].(bool); ok {
                opts.Stable = v
        }
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                opts.Timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserWaitForSmart(ec.Ch.GetSessionID(), url, selector, opts)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser smart wait completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserNavigate(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_navigate tool call", TaskStatusFailed
        }

        action, ok := ec.ArgsMap["action"].(string)
        if !ok || action == "" {
                return "Error: Empty action in browser_navigate tool call", TaskStatusFailed
        }

        var result *BrowserNavigateResult
        var err error
        var content string

        switch action {
        case "back":
                result, err = BrowserNavigateBack(ec.Ch.GetSessionID(), url)
        case "forward":
                result, err = BrowserNavigateForward(ec.Ch.GetSessionID(), url)
        case "refresh":
                result, err = BrowserRefresh(ec.Ch.GetSessionID(), url)
        default:
                return "Error: Invalid action: " + action, TaskStatusFailed
        }

        if err != nil {
                content = "Error: " + err.Error()
        } else if result != nil {
                resultTOON, _ := toon.Marshal(result)
                content = string(resultTOON)
        }
        fmt.Println("Browser navigate completed:", action)
        return content, TaskStatusSuccess
}

func execBrowserGetCookies(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_get_cookies tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserGetCookies(ec.Ch.GetSessionID(), url)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser get cookies completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserCookieSave(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_cookie_save tool call", TaskStatusFailed
        }

        filePath, _ := ec.ArgsMap["file_path"].(string)

        // 修正：传递 sessionID
        result, err := BrowserCookieSave(ec.Ch.GetSessionID(), url, filePath)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser cookie save completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserCookieLoad(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_cookie_load tool call", TaskStatusFailed
        }

        filePath, ok := ec.ArgsMap["file_path"].(string)
        if !ok || filePath == "" {
                return "Error: Empty file_path in browser_cookie_load tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserCookieLoad(ec.Ch.GetSessionID(), url, filePath)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser cookie load completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserSnapshot(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_snapshot tool call", TaskStatusFailed
        }

        maxDepth := 5
        if d, ok := ec.ArgsMap["max_depth"].(float64); ok {
                maxDepth = int(d)
        }

        // 修正：传递 sessionID
        result, err := BrowserSnapshot(ec.Ch.GetSessionID(), url, maxDepth)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser snapshot completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserUploadFile(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_upload_file tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_upload_file tool call", TaskStatusFailed
        }

        filePathsRaw, ok := ec.ArgsMap["file_paths"].([]interface{})
        if !ok {
                return "Error: Invalid file_paths in browser_upload_file tool call", TaskStatusFailed
        }

        var filePaths []string
        for _, p := range filePathsRaw {
                if s, ok := p.(string); ok {
                        filePaths = append(filePaths, s)
                }
        }

        // 修正：传递 sessionID
        result, err := BrowserUploadFile(ec.Ch.GetSessionID(), url, selector, filePaths)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser upload file completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserSelectOption(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_select_option tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_select_option tool call", TaskStatusFailed
        }

        valuesRaw, ok := ec.ArgsMap["values"].([]interface{})
        if !ok {
                return "Error: Invalid values in browser_select_option tool call", TaskStatusFailed
        }

        var values []string
        for _, v := range valuesRaw {
                if s, ok := v.(string); ok {
                        values = append(values, s)
                }
        }

        // 修正：传递 sessionID
        result, err := BrowserSelectOption(ec.Ch.GetSessionID(), url, selector, values)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser select option completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserKeyPress(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_key_press tool call", TaskStatusFailed
        }

        keysRaw, ok := ec.ArgsMap["keys"].([]interface{})
        if !ok {
                return "Error: Invalid keys in browser_key_press tool call", TaskStatusFailed
        }

        var keys []string
        for _, k := range keysRaw {
                if s, ok := k.(string); ok {
                        keys = append(keys, s)
                }
        }

        // 修正：传递 sessionID
        result, err := BrowserKeyPress(ec.Ch.GetSessionID(), url, keys)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser key press completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserElementScreenshot(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_element_screenshot tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_element_screenshot tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserElementScreenshot(ec.Ch.GetSessionID(), url, selector)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser element screenshot completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserPDF(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_pdf tool call", TaskStatusFailed
        }

        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 修正：传递 sessionID
        result, err := BrowserPDF(ec.Ch.GetSessionID(), url, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser PDF export completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserPDFFromFile(ec *ToolExecContext) (string, TaskStatus) {
        filePath, ok := ec.ArgsMap["file_path"].(string)
        if !ok || filePath == "" {
                return "Error: Empty file_path in browser_pdf_from_file tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserPDFFromFile(ec.Ch.GetSessionID(), filePath)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser PDF from file completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserSetHeaders(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_set_headers tool call", TaskStatusFailed
        }

        headersInterface, ok := ec.ArgsMap["headers"].([]interface{})
        if !ok {
                return "Error: Invalid headers in browser_set_headers tool call", TaskStatusFailed
        }

        var headers []string
        for _, h := range headersInterface {
                if hStr, ok := h.(string); ok {
                        headers = append(headers, hStr)
                }
        }

        // 修正：传递 sessionID
        result, err := BrowserSetHeaders(ec.Ch.GetSessionID(), url, headers)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser set headers completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserSetUserAgent(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_set_user_agent tool call", TaskStatusFailed
        }

        userAgent, ok := ec.ArgsMap["user_agent"].(string)
        if !ok || userAgent == "" {
                return "Error: Empty user_agent in browser_set_user_agent tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserSetUserAgent(ec.Ch.GetSessionID(), url, userAgent)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser set user agent completed")
        return string(resultTOON), TaskStatusSuccess
}

func execBrowserEmulateDevice(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Empty url in browser_emulate_device tool call", TaskStatusFailed
        }

        device, ok := ec.ArgsMap["device"].(string)
        if !ok || device == "" {
                return "Error: Empty device in browser_emulate_device tool call", TaskStatusFailed
        }

        // 修正：传递 sessionID
        result, err := BrowserEmulateDevice(ec.Ch.GetSessionID(), url, device)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser emulate device completed")
        return string(resultTOON), TaskStatusSuccess
}

// ========== 合併瀏覽器工具 handlers（聚合工具分發）==========
// 這三個 handler 對應 GetConsolidatedBrowserTools 中定義的聚合工具，
// 當 LLM 使用小模型工具集（合併工具）時，由這裡分發到具體的底層實現。

// execBrowserInteract 處理 browser_interact 合併工具
// 將 action 參數分發到對應的獨立瀏覽器操作函數
func execBrowserInteract(ec *ToolExecContext) (string, TaskStatus) {
        action, ok := ec.ArgsMap["action"].(string)
        if !ok || action == "" {
                return "Error: Missing 'action' parameter in browser_interact call. Valid actions: click, double_click, hover, right_click, type, scroll, drag", TaskStatusFailed
        }

        url, _ := ec.ArgsMap["url"].(string)
        selector, _ := ec.ArgsMap["selector"].(string)
        text, _ := ec.ArgsMap["text"].(string)
        submit, _ := ec.ArgsMap["submit"].(bool)
        direction, _ := ec.ArgsMap["direction"].(string)
        amount := 0
        if a, ok := ec.ArgsMap["amount"].(float64); ok {
                amount = int(a)
        }
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        sessionID := ec.Ch.GetSessionID()
        var result interface{}
        var err error

        switch action {
        case "click":
                result, err = BrowserClick(sessionID, url, selector, timeout)
        case "double_click":
                result, err = BrowserDoubleClick(sessionID, url, selector)
        case "hover":
                result, err = BrowserHover(sessionID, url, selector)
        case "right_click":
                result, err = BrowserRightClick(sessionID, url, selector)
        case "type":
                result, err = BrowserType(sessionID, url, selector, text, submit, timeout)
        case "scroll":
                result, err = BrowserScroll(sessionID, url, direction, amount, timeout)
        case "drag":
                // drag 需要兩個 selector：source 和 target
                targetSelector, _ := ec.ArgsMap["target_selector"].(string)
                if targetSelector == "" {
                        targetSelector = selector
                }
                result, err = BrowserDrag(sessionID, url, selector, targetSelector)
        default:
                return "Error: Invalid action '" + action + "' in browser_interact. Valid: click, double_click, hover, right_click, type, scroll, drag", TaskStatusFailed
        }

        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Printf("Browser interact [%s] completed\n", action)
        return string(resultTOON), TaskStatusSuccess
}

// execBrowserExtract 處理 browser_extract 合併工具
// 將 mode 參數分發到對應的內容提取函數
func execBrowserExtract(ec *ToolExecContext) (string, TaskStatus) {
        mode, ok := ec.ArgsMap["mode"].(string)
        if !ok || mode == "" {
                return "Error: Missing 'mode' parameter in browser_extract call. Valid modes: screenshot, execute_js, extract_links, extract_images, extract_elements, snapshot, pdf, element_screenshot", TaskStatusFailed
        }

        url, _ := ec.ArgsMap["url"].(string)
        selector, _ := ec.ArgsMap["selector"].(string)
        script, _ := ec.ArgsMap["script"].(string)
        fullPage, _ := ec.ArgsMap["full_page"].(bool)
        includeHTML, _ := ec.ArgsMap["include_html"].(bool)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        sessionID := ec.Ch.GetSessionID()
        var result interface{}
        var err error

        switch mode {
        case "screenshot":
                result, err = BrowserScreenshot(sessionID, url, fullPage, timeout)
        case "execute_js":
                result, err = BrowserExecuteJS(sessionID, url, script, timeout)
        case "extract_links":
                result, err = BrowserExtractLinks(sessionID, url, timeout)
        case "extract_images":
                result, err = BrowserExtractImages(sessionID, url, timeout)
        case "extract_elements":
                result, err = BrowserExtractElements(sessionID, url, selector, includeHTML, timeout)
        case "snapshot":
                maxDepth := 5
                if d, ok := ec.ArgsMap["max_depth"].(float64); ok {
                        maxDepth = int(d)
                }
                result, err = BrowserSnapshot(sessionID, url, maxDepth)
        case "pdf":
                result, err = BrowserPDF(sessionID, url, timeout)
        case "element_screenshot":
                result, err = BrowserElementScreenshot(sessionID, url, selector)
        default:
                return "Error: Invalid mode '" + mode + "' in browser_extract. Valid: screenshot, execute_js, extract_links, extract_images, extract_elements, snapshot, pdf, element_screenshot", TaskStatusFailed
        }

        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Printf("Browser extract [%s] completed\n", mode)
        return string(resultTOON), TaskStatusSuccess
}

// execBrowserFormFill 處理 browser_form_fill 合併工具
// 支持多字段填寫、文件上傳、下拉選擇、按鍵模擬
func execBrowserFormFill(ec *ToolExecContext) (string, TaskStatus) {
        url, ok := ec.ArgsMap["url"].(string)
        if !ok || url == "" {
                return "Error: Missing 'url' parameter in browser_form_fill call", TaskStatusFailed
        }

        sessionID := ec.Ch.GetSessionID()
        submitSelector, _ := ec.ArgsMap["submit_selector"].(string)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 優先處理 file_path（文件上傳）
        if filePath, ok := ec.ArgsMap["file_path"].(string); ok && filePath != "" {
                fileSelector, _ := ec.ArgsMap["file_selector"].(string)
                if fileSelector == "" {
                        fileSelector, _ = ec.ArgsMap["selector"].(string)
                }
                if fileSelector == "" {
                        return "Error: Missing 'file_selector' or 'selector' for file upload in browser_form_fill", TaskStatusFailed
                }
                result, err := BrowserUploadFile(sessionID, url, fileSelector, []string{filePath})
                if err != nil {
                        return "Error: " + err.Error(), TaskStatusFailed
                }
                resultTOON, _ := toon.Marshal(result)
                fmt.Println("Browser form fill [upload] completed")
                return string(resultTOON), TaskStatusSuccess
        }

        // 處理 select_value（下拉選擇）
        if selectValue, ok := ec.ArgsMap["select_value"].(string); ok && selectValue != "" {
                selector, _ := ec.ArgsMap["selector"].(string)
                if selector == "" {
                        return "Error: Missing 'selector' for select option in browser_form_fill", TaskStatusFailed
                }
                result, err := BrowserSelectOption(sessionID, url, selector, []string{selectValue})
                if err != nil {
                        return "Error: " + err.Error(), TaskStatusFailed
                }
                resultTOON, _ := toon.Marshal(result)
                fmt.Println("Browser form fill [select] completed")
                return string(resultTOON), TaskStatusSuccess
        }

        // 處理 keys（按鍵模擬）
        if keysRaw, ok := ec.ArgsMap["keys"].([]interface{}); ok && len(keysRaw) > 0 {
                var keys []string
                for _, k := range keysRaw {
                        if s, ok := k.(string); ok {
                                keys = append(keys, s)
                        }
                }
                result, err := BrowserKeyPress(sessionID, url, keys)
                if err != nil {
                        return "Error: " + err.Error(), TaskStatusFailed
                }
                resultTOON, _ := toon.Marshal(result)
                fmt.Println("Browser form fill [key_press] completed")
                return string(resultTOON), TaskStatusSuccess
        }

        // 處理 form_data（標準表單填寫）
        formDataRaw, ok := ec.ArgsMap["form_data"].(map[string]interface{})
        if !ok || len(formDataRaw) == 0 {
                return "Error: Missing 'form_data' parameter in browser_form_fill call", TaskStatusFailed
        }

        formData := make(FormData)
        for k, v := range formDataRaw {
                if s, ok := v.(string); ok {
                        formData[k] = s
                } else {
                        // 非 string 值轉爲 JSON string
                        jsonBytes, _ := json.Marshal(v)
                        formData[k] = string(jsonBytes)
                }
        }

        result, err := BrowserFillForm(sessionID, url, formData, submitSelector, timeout)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        resultTOON, _ := toon.Marshal(result)
        fmt.Println("Browser form fill [form_data] completed")
        return string(resultTOON), TaskStatusSuccess
}

func execTodos(ec *ToolExecContext) (string, TaskStatus) {
        itemsInterface, ok := ec.ArgsMap["todos"].([]interface{})
        if !ok {
                return "Error: Invalid todos in todos tool call", TaskStatusFailed
        }

        var items []TodoItem
        valid := true
        var content string
        for _, itemInterface := range itemsInterface {
                itemMap, ok := itemInterface.(map[string]interface{})
                if !ok {
                        content = "Error: Invalid item format"
                        valid = false
                        break
                }
                item := TodoItem{}
                if id, ok := itemMap["id"].(string); ok {
                        item.ID = id
                }
                if text, ok := itemMap["content"].(string); ok {
                        item.Text = text
                } else {
                        content = "Error: Item missing content"
                        valid = false
                        break
                }
                if status, ok := itemMap["status"].(string); ok {
                        item.Status = status
                } else {
                        content = "Error: Item missing status"
                        valid = false
                        break
                }
                items = append(items, item)
        }
        if !valid {
                return content, TaskStatusSuccess
        }

        // 支持 list_id 參數（Plan Mode 每階段使用不同列表）
        listID, _ := ec.ArgsMap["list_id"].(string)
        // Plan Mode 自動檢測：如果在 Plan Mode 中且未指定 list_id，自動使用當前 Phase 的列表
        if listID == "" && globalPlanMode != nil && globalPlanMode.IsActive() {
                phase := globalPlanMode.CurrentPhase()
                switch phase {
                case PlanPhaseExplore:
                        listID = "phase1"
                case PlanPhaseDesign:
                        listID = "phase2"
                case PlanPhaseReview:
                        listID = "phase3"
                case PlanPhasePlan:
                        listID = "phase4"
                }
        }

        var err error
        var output string
        if listID != "" {
                output, err = TODO.Update(items, listID)
        } else {
                output, err = TODO.Update(items)
        }
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }
        return output, TaskStatusSuccess
}

// --- Wrappers for existing handler functions ---

func execCronAdd(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleCronAdd(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execCronRemove(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleCronRemove(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execCronList(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleCronList(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execCronStatus(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleCronStatus(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execMemorySave(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleMemorySave(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execMemoryRecall(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleMemoryRecall(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execMemoryForget(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleMemoryForget(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execMemoryList(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleMemoryList(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execProfileCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleProfileCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execActorIdentitySet(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleActorIdentitySet(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execActorIdentityClear(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleActorIdentityClear(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execProfileReload(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleProfileReload(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

// --- Skill tool handlers ---

func execSkillList(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        req := SkillListRequest{}
        if page, ok := ec.ArgsMap["page"].(float64); ok {
                req.Page = int(page)
        }
        if pageSize, ok := ec.ArgsMap["page_size"].(float64); ok {
                req.PageSize = int(pageSize)
        }
        if search, ok := ec.ArgsMap["search"].(string); ok {
                req.Search = search
        }
        if sortBy, ok := ec.ArgsMap["sort_by"].(string); ok {
                req.SortBy = sortBy
        }
        if sortOrder, ok := ec.ArgsMap["sort_order"].(string); ok {
                req.SortOrder = sortOrder
        }
        if context, ok := ec.ArgsMap["context"].(string); ok {
                req.Context = context
        }
        if tags, ok := ec.ArgsMap["tags"].([]interface{}); ok {
                for _, tag := range tags {
                        if tagStr, ok := tag.(string); ok {
                                req.Tags = append(req.Tags, tagStr)
                        }
                }
        }
        if suggestOnly, ok := ec.ArgsMap["suggest_only"].(bool); ok {
                req.SuggestOnly = suggestOnly
        }

        resp, err := globalSkillManagerV2.ListSkills(req)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 如果有上下文，添加推荐
        if req.Context != "" {
                suggestions, _ := globalSkillManagerV2.EvolutionOptimizer().SuggestSkills(req.Context, 5)
                for _, s := range suggestions {
                        resp.Suggestions = append(resp.Suggestions, s.SkillName)
                }
        }

        skillsTOON, err := toon.Marshal(resp)
        if err != nil {
                return "Error: failed to marshal skills", TaskStatusFailed
        }

        fmt.Println("Skill list completed")
        return string(skillsTOON), TaskStatusSuccess
}

func execSkillCreate(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok1 := ec.ArgsMap["name"].(string)
        systemPrompt, ok2 := ec.ArgsMap["system_prompt"].(string)
        if !ok1 || !ok2 || name == "" || systemPrompt == "" {
                return "Error: missing required parameters (name and system_prompt)", TaskStatusFailed
        }

        skill := &Skill{
                Name:         name,
                DisplayName:  name,
                SystemPrompt: systemPrompt,
        }

        if description, ok := ec.ArgsMap["description"].(string); ok {
                skill.Description = description
        }
        if triggerWords, ok := ec.ArgsMap["trigger_words"].([]interface{}); ok {
                for _, tw := range triggerWords {
                        if twStr, ok := tw.(string); ok && twStr != "" {
                                skill.TriggerWords = append(skill.TriggerWords, twStr)
                        }
                }
        }
        if tags, ok := ec.ArgsMap["tags"].([]interface{}); ok {
                for _, tag := range tags {
                        if tagStr, ok := tag.(string); ok && tagStr != "" {
                                skill.Tags = append(skill.Tags, tagStr)
                        }
                }
        }

        if err := globalSkillManagerV2.CreateSkill(skill); err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        return "Skill created successfully: " + name, TaskStatusSuccess
}

func execSkillDelete(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }

        if err := globalSkillManagerV2.DeleteSkill(name); err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        fmt.Println("Skill delete completed")
        return "Skill deleted successfully: " + name, TaskStatusSuccess
}

func execSkillGet(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }

        skill, err := globalSkillManagerV2.GetSkill(name)
        if err != nil {
                return "Error: skill not found", TaskStatusFailed
        }

        skillTOON, err := toon.Marshal(skill)
        if err != nil {
                return "Error: failed to marshal skill", TaskStatusFailed
        }

        fmt.Println("Skill get completed")
        return string(skillTOON), TaskStatusSuccess
}

func execSkillReload(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        if err := globalSkillManagerV2.Reload(); err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        fmt.Println("Skill reload completed")
        return "Skills reloaded successfully", TaskStatusSuccess
}

func execSkillUpdate(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }

        updates := make(map[string]interface{})

        if displayName, ok := ec.ArgsMap["display_name"].(string); ok && displayName != "" {
                updates["display_name"] = displayName
        }
        if description, ok := ec.ArgsMap["description"].(string); ok && description != "" {
                updates["description"] = description
        }
        if systemPrompt, ok := ec.ArgsMap["system_prompt"].(string); ok && systemPrompt != "" {
                updates["system_prompt"] = systemPrompt
        }
        if triggerWords, ok := ec.ArgsMap["trigger_words"].([]interface{}); ok && len(triggerWords) > 0 {
                var triggers []string
                for _, tw := range triggerWords {
                        if twStr, ok := tw.(string); ok && twStr != "" {
                                triggers = append(triggers, twStr)
                        }
                }
                updates["trigger_words"] = triggers
        }
        if tags, ok := ec.ArgsMap["tags"].([]interface{}); ok && len(tags) > 0 {
                var tagList []string
                for _, tag := range tags {
                        if tagStr, ok := tag.(string); ok && tagStr != "" {
                                tagList = append(tagList, tagStr)
                        }
                }
                updates["tags"] = tagList
        }

        if len(updates) == 0 {
                return "No changes provided for skill: " + name, TaskStatusSuccess
        }

        if err := globalSkillManagerV2.UpdateSkill(name, updates); err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        fmt.Println("Skill update completed")
        return "Skill updated successfully: " + name, TaskStatusSuccess
}

func execSkillSuggest(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        context, ok := ec.ArgsMap["context"].(string)
        if !ok || context == "" {
                return "Error: missing required parameter 'context'", TaskStatusFailed
        }

        topK := 5
        if k, ok := ec.ArgsMap["top_k"].(float64); ok {
                topK = int(k)
        }

        suggestions, err := globalSkillManagerV2.EvolutionOptimizer().SuggestSkills(context, topK)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        suggestionsTOON, err := toon.Marshal(suggestions)
        if err != nil {
                return "Error: failed to marshal suggestions", TaskStatusFailed
        }

        fmt.Println("Skill suggest completed")
        return string(suggestionsTOON), TaskStatusSuccess
}

func execSkillStats(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        stats, err := globalSkillManagerV2.EvolutionOptimizer().GetSkillStats()
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        statsTOON, err := toon.Marshal(stats)
        if err != nil {
                return "Error: failed to marshal stats", TaskStatusFailed
        }

        fmt.Println("Skill stats completed")
        return string(statsTOON), TaskStatusSuccess
}

func execSkillEvaluate(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }

        report, err := globalSkillManagerV2.EvolutionOptimizer().EvaluateSkillQuality(name)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        reportTOON, err := toon.Marshal(report)
        if err != nil {
                return "Error: failed to marshal report", TaskStatusFailed
        }

        fmt.Println("Skill evaluate completed")
        return string(reportTOON), TaskStatusSuccess
}

func execSkillLoad(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }

        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }

        // 检查技能是否存在
        skill, err := globalSkillManagerV2.GetSkill(name)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // ── 記錄技能使用事件（skill_evolution 追蹤）──────────────
        if globalSkillManagerV2 != nil {
                evo := globalSkillManagerV2.EvolutionOptimizer()
                if evo != nil {
                        _ = evo.RecordUsageEvent(SkillUsageEvent{
                                SkillName:    skill.Name,
                                SessionID:    "",
                                Timestamp:    time.Now().Unix(),
                                ContextMatch: 0.8,
                                                UserFeedback: 0,   // 等待用戶反饋
                                SuccessRate:  1.0,
                                TokensSaved:  0,
                        })
                }
        }

        // 技能加载成功
        fmt.Println("Skill load completed")
        return fmt.Sprintf("Skill '%s' loaded successfully", skill.Name), TaskStatusSuccess
}

// --- Text tool handlers ---

func execTextSearch(ec *ToolExecContext) (string, TaskStatus) {
        keyword, ok := ec.ArgsMap["keyword"].(string)
        if !ok || keyword == "" {
                return "Error: Empty keyword in text_search tool call", TaskStatusFailed
        }

        opts := TextSearchOptions{}
        if rootDir, ok := ec.ArgsMap["root_dir"].(string); ok && rootDir != "" {
                opts.RootDir = rootDir
        }
        if filePattern, ok := ec.ArgsMap["file_pattern"].(string); ok {
                opts.FilePattern = filePattern
        }
        if ignoreCase, ok := ec.ArgsMap["ignore_case"].(bool); ok {
                opts.IgnoreCase = ignoreCase
        }
        if useRegex, ok := ec.ArgsMap["use_regex"].(bool); ok {
                opts.UseRegex = useRegex
        }
        if maxDepth, ok := ec.ArgsMap["max_depth"].(float64); ok {
                opts.MaxDepth = int(maxDepth)
        }
        if maxResults, ok := ec.ArgsMap["max_results"].(float64); ok {
                opts.MaxResults = int(maxResults)
        }

        results, err := TextSearch(keyword, opts)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        } else if len(results) == 0 {
                return "No matches found", TaskStatusSuccess
        }

        resultsTOON, err := toon.Marshal(results)
        if err != nil {
                return "Error: Failed to marshal search results", TaskStatusFailed
        }

        fmt.Printf("Text search completed: %d results\n", len(results))
        return string(resultsTOON), TaskStatusSuccess
}

func execTextReplace(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTextReplace(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execTextGrep(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTextSearch(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execTextTransform(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTextTransform(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

// --- Plugin tool handlers ---

func execPluginCreate(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginCreate(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginList(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginList(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginLoad(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginLoad(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginUnload(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginUnload(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginReload(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginReload(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginCall(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginCall(ec.Ctx, ec.ArgsMap, ec.Ch)
        // 检查是否是插件不存在的错误
        if strings.Contains(content, "Error:") || strings.Contains(content, "error:") {
                // 确保错误消息格式正确，以Error:开头
                if !strings.HasPrefix(content, "Error:") {
                        content = "Error: " + content
                }
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginCompile(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginCompile(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginDelete(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginDelete(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginAPIs(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginAPIs(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execPluginDetail(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handlePluginDetail(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

// --- Shell delayed tool handlers ---

func execShellDelayed(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleDelayedExec(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellDelayedCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTaskCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellDelayedTerminate(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTaskTerminate(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellDelayedList(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTaskList(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellDelayedWait(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTaskWait(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execShellDelayedRemove(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleTaskRemove(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

// --- Spawn tool handlers ---

func execSpawn(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSpawn(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execSpawnCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSpawnCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execSpawnList(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSpawnList(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execSpawnCancel(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSpawnCancel(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

func execSpawnBatch(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := handleSpawnBatch(ec.Ctx, ec.ArgsMap, ec.Ch)
        return content, TaskStatusSuccess
}

// --- Other tool handlers ---

func execConsolidateMemory(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := HandleConsolidateMemory(ec.ArgsMap)
        return content, TaskStatusSuccess
}

func execSchemeEval(ec *ToolExecContext) (string, TaskStatus) {
        expression, ok := ec.ArgsMap["expression"].(string)
        if !ok || expression == "" {
                return "Error: Invalid or empty expression", TaskStatusFailed
        }

        result, err := schemeEval(ec.Ctx, expression)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), TaskStatusFailed
        }
        return result, TaskStatusSuccess
}

func execOpenCLITool(ec *ToolExecContext) (string, TaskStatus) {
        command, ok := ec.ArgsMap["command"].(string)
        if !ok || command == "" {
                return "Error: Invalid or empty command", TaskStatusFailed
        }

        // 构建完整的 opencli 命令
        fullCommand := "opencli " + command
        result := runShellWithTimeout(ec.Ctx, fullCommand, false, false)

        if result.ConfirmRequired {
                var confirmResult strings.Builder
                confirmResult.WriteString("⚠️ **确认请求**\n\n")
                confirmResult.WriteString(result.ConfirmMessage)
                confirmResult.WriteString("\n\n---\n")
                confirmResult.WriteString("要强制执行此命令，请使用: `opencli(command=\"...\")`\n")

                content := confirmResult.String()
                fmt.Println(content)
                return content, TaskStatusSuccess
        } else if result.Err != nil {
                // 注意：取消检查由 executeTool 统一处理
                content := fmt.Sprintf("Error: %v", result.Err)
                if result.Stderr != "" {
                        content += "\n" + result.Stderr
                        // 检查是否是未知命令错误
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
                                // 执行 opencli help 命令获取帮助信息
                                helpResult := runShellWithTimeout(ec.Ctx, "opencli help", false, false)
                                if helpResult.Err == nil {
                                        content += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
                                }
                        }
                }
                fmt.Println(content)
                return content, TaskStatusFailed
        } else {
                content := result.Stdout
                if result.ExitCode != 0 && result.Stderr != "" {
                        content += "\n" + result.Stderr
                        // 检查是否是未知命令错误
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
                                // 执行 opencli help 命令获取帮助信息
                                helpResult := runShellWithTimeout(ec.Ctx, "opencli help", false, false)
                                if helpResult.Err == nil {
                                        content += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
                                }
                        }
                        fmt.Println(content)
                        return content, TaskStatusFailed
                }
                fmt.Println(content)
                return content, TaskStatusSuccess
        }
}

// --- Registry ---

var toolHandlerRegistry map[string]ToolHandler

func init() {
        toolHandlerRegistry = map[string]ToolHandler{
                // Menu & planning
                "menu":        execMenuTool,
                "next_phase":  execNextPhase,

                // Shell tools
                "smart_shell": execSmartShellTool,
                "shell":       execShellTool,
                "opencli":     execOpenCLITool,

                // SSH tools
                "ssh_connect": execSSHConnect,
                "ssh_exec":    execSSHExec,
                "ssh_list":    execSSHList,
                "ssh_close":   execSSHClose,

                // File tools
                "read_file_line":  execReadFileLine,
                "write_file_line": execWriteFileLine,
                "read_all_lines":  execReadAllLines,
                "write_all_lines": execWriteAllLines,
                "append_to_file":  execAppendToFile,
                "write_file_range": execWriteFileRange,
                "read_file_range":  execReadFileRange,

                // Browser basic tools
                "browser_search":    execBrowserSearch,
                "browser_visit":     execBrowserVisit,
                "browser_download":  execBrowserDownload,

                // Browser enhanced tools
                "browser_click":            execBrowserClick,
                "browser_type":             execBrowserType,
                "browser_scroll":           execBrowserScroll,
                "browser_wait_element":     execBrowserWaitElement,
                "browser_extract_links":    execBrowserExtractLinks,
                "browser_extract_images":   execBrowserExtractImages,
                "browser_extract_elements": execBrowserExtractElements,
                "browser_screenshot":       execBrowserScreenshot,
                "browser_execute_js":       execBrowserExecuteJS,
                "browser_fill_form":        execBrowserFillForm,

                // Browser advanced tools
                "browser_hover":              execBrowserHover,
                "browser_double_click":       execBrowserDoubleClick,
                "browser_right_click":        execBrowserRightClick,
                "browser_drag":               execBrowserDrag,
                "browser_wait_smart":         execBrowserWaitSmart,
                "browser_navigate":           execBrowserNavigate,
                "browser_get_cookies":        execBrowserGetCookies,
                "browser_cookie_save":        execBrowserCookieSave,
                "browser_cookie_load":        execBrowserCookieLoad,
                "browser_snapshot":           execBrowserSnapshot,
                "browser_upload_file":        execBrowserUploadFile,
                "browser_select_option":      execBrowserSelectOption,
                "browser_key_press":          execBrowserKeyPress,
                "browser_element_screenshot": execBrowserElementScreenshot,
                "browser_pdf":                execBrowserPDF,
                "browser_pdf_from_file":      execBrowserPDFFromFile,
                "browser_set_headers":        execBrowserSetHeaders,
                "browser_set_user_agent":     execBrowserSetUserAgent,
                "browser_emulate_device":     execBrowserEmulateDevice,

                // 合併瀏覽器工具（聚合分發，對應 GetConsolidatedBrowserTools）
                "browser_interact":   execBrowserInteract,
                "browser_extract":    execBrowserExtract,
                "browser_form_fill":  execBrowserFormFill,

                // Todo tools
                "todos": execTodos,

                // Cron tools
                "cron_add":    execCronAdd,
                "cron_remove": execCronRemove,
                "cron_list":   execCronList,
                "cron_status": execCronStatus,

                // Memory tools
                "memory_save":   execMemorySave,
                "memory_recall": execMemoryRecall,
                "memory_forget": execMemoryForget,
                "memory_list":   execMemoryList,

                // Profile tools
                "profile_check":        execProfileCheck,
                "actor_identity_set":   execActorIdentitySet,
                "actor_identity_clear": execActorIdentityClear,
                "profile_reload":       execProfileReload,

                // Skill tools
                "skill_list":     execSkillList,
                "skill_create":   execSkillCreate,
                "skill_delete":   execSkillDelete,
                "skill_get":      execSkillGet,
                "skill_reload":   execSkillReload,
                "skill_update":   execSkillUpdate,
                "skill_suggest":  execSkillSuggest,
                "skill_stats":    execSkillStats,
                "skill_evaluate": execSkillEvaluate,
                "skill_load":     execSkillLoad,

                // Text tools
                "text_search":    execTextSearch,
                "text_replace":   execTextReplace,
                "text_grep":      execTextGrep,
                "text_transform": execTextTransform,

                // Plugin tools
                "plugin_create":  execPluginCreate,
                "plugin_list":    execPluginList,
                "plugin_load":    execPluginLoad,
                "plugin_unload":  execPluginUnload,
                "plugin_reload":  execPluginReload,
                "plugin_call":    execPluginCall,
                "plugin_compile": execPluginCompile,
                "plugin_delete":  execPluginDelete,
                "plugin_apis":    execPluginAPIs,
                "plugin_detail":  execPluginDetail,

                // Shell delayed tools
                "shell_delayed":          execShellDelayed,
                "shell_delayed_check":    execShellDelayedCheck,
                "shell_delayed_terminate": execShellDelayedTerminate,
                "shell_delayed_list":     execShellDelayedList,
                "shell_delayed_wait":     execShellDelayedWait,
                "shell_delayed_remove":   execShellDelayedRemove,

                // Spawn tools
                "spawn":         execSpawn,
                "spawn_check":   execSpawnCheck,
                "spawn_list":    execSpawnList,
                "spawn_cancel":  execSpawnCancel,
                "spawn_batch":   execSpawnBatch,

                // Other tools
                "consolidate_memory": execConsolidateMemory,
                "scheme_eval":        execSchemeEval,

                // ── P3: RL 導出工具處理函數 ────────────────────────────
                "export_sft_data":  execExportSFTData,
                "export_rl_data":   execExportRLData,
                "trajectory_stats": execTrajectoryStats,

                // ── P4: 憑證池 & Profile 管理工具 ────────────────────────
                "credential_add":  execCredentialAdd,
                "credential_list": execCredentialList,
                "profile_create":  execProfileCreate,
                "profile_switch":  execProfileSwitch,
                "profile_list":    execProfileList,

                // ── 技能演化工具 ──────────────────────────────────────
                "skill_cleanup_suggest": execSkillCleanupSuggest,
                "skill_autotag":         execSkillAutoTag,
        }
}

// --- Main dispatch ---

// executeTool 执行单个工具调用，返回增强消息
func executeTool(ctx context.Context, toolID, toolName string, argsMap map[string]interface{}, ch Channel, role *Role) EnrichedMessage {
        var content string
        status := TaskStatusSuccess

        if ctx.Err() == context.Canceled {
                cancelMsg := "User cancelled before execution"
                emitToolCallTags(ch, toolName, argsMap, cancelMsg, TaskStatusCancelled)
                return CancelToolResult(toolID, CancelByUser, cancelMsg, toolName)
        }

        if role != nil && !role.IsToolAllowed(toolName) {
                errMsg := fmt.Sprintf("❌ 权限拒绝：当前角色「%s」无权使用工具「%s」。\n\n可用工具：%v",
                        role.DisplayName, toolName, getAllowedToolsList(role))
                argsJSON, _ := json.Marshal(map[string]interface{}{"error": "permission denied"})
                sendToolCallStart(ch, toolName, string(argsJSON))
                ch.WriteChunk(StreamChunk{Content: errMsg + "\n"})
                sendToolCallStatus(ch, TaskStatusFailed)
                sendToolCallEnd(ch)
                return NewToolResultMessage(toolID, errMsg, TaskStatusFailed, toolName)
        }

        argsJSON, _ := json.Marshal(argsMap)
        sendToolCallStart(ch, toolName, string(argsJSON))
        defer sendToolCallEnd(ch)
        // 注意：defer 按注册逆序执行，所以 status 标签会在 END 标记之前写入
        // 使用闭包捕获 status 变量引用，确保返回时读取的是最终值
        defer func() {
                sendToolCallStatus(ch, status)
        }()

        handler, ok := toolHandlerRegistry[toolName]
        if ok {
                ec := &ToolExecContext{
                        Ctx:     ctx,
                        ToolID:  toolID,
                        ArgsMap: argsMap,
                        Ch:      ch,
                        Role:    role,
                }
                content, status = handler(ec)
        } else if strings.HasPrefix(toolName, "mcp_") && globalMCPClientManager != nil {
                result, err := globalMCPClientManager.CallTool(ctx, toolName, argsMap)
                if err != nil {
                        content = fmt.Sprintf("Error: %v", err)
                        status = TaskStatusFailed
                } else {
                        content = result
                }
        } else {
                content = "Error: Unknown tool name: " + toolName
                status = TaskStatusFailed
        }

        if status == TaskStatusSuccess && (strings.HasPrefix(content, "Error:") || strings.HasPrefix(content, "error:")) {
                status = TaskStatusFailed
        }

        content = sanitizeContent(content)
        if content != "" {
                ch.WriteChunk(StreamChunk{Content: content + "\n"})
        }

        return NewToolResultMessage(toolID, content, status, toolName)
}

// ============================================================
// P3: RL 導出工具處理函數
// ============================================================

func execExportSFTData(ec *ToolExecContext) (string, TaskStatus) {
        outputPath := "data/trajectories/sft_export.jsonl"
        if v, ok := ec.ArgsMap["output_path"].(string); ok && v != "" {
                outputPath = v
        }
        limit := 0
        if v, ok := ec.ArgsMap["limit"].(float64); ok {
                limit = int(v)
        }

        tm := GetTrajectoryManager()
        if tm == nil {
                return "Error: Trajectory manager not initialized", TaskStatusFailed
        }
        if err := ExportSFTToJSONL(tm, outputPath, limit); err != nil {
                return fmt.Sprintf("Error exporting SFT data: %v", err), TaskStatusFailed
        }
        return fmt.Sprintf("SFT data exported to %s (limit: %d)", outputPath, limit), TaskStatusSuccess
}

func execExportRLData(ec *ToolExecContext) (string, TaskStatus) {
        outputPath := "data/trajectories/rl_export.jsonl"
        if v, ok := ec.ArgsMap["output_path"].(string); ok && v != "" {
                outputPath = v
        }
        limit := 0
        if v, ok := ec.ArgsMap["limit"].(float64); ok {
                limit = int(v)
        }

        tm := GetTrajectoryManager()
        if tm == nil {
                return "Error: Trajectory manager not initialized", TaskStatusFailed
        }

        if err := ExportRLToJSONL(tm, outputPath, limit); err != nil {
                return fmt.Sprintf("Error exporting RL data: %v", err), TaskStatusFailed
        }
        return fmt.Sprintf("RL data exported to %s (limit: %d)", outputPath, limit), TaskStatusSuccess
}

func execTrajectoryStats(ec *ToolExecContext) (string, TaskStatus) {
        tm := GetTrajectoryManager()
        if tm == nil {
                return "Error: Trajectory manager not initialized", TaskStatusFailed
        }
        outputPath := "data/trajectories/stats.json"
        if v, ok := ec.ArgsMap["output_path"].(string); ok && v != "" {
                outputPath = v
        }

        if err := ExportTrajectoryStatsToFile(tm, outputPath); err != nil {
                return fmt.Sprintf("Error exporting trajectory stats: %v", err), TaskStatusFailed
        }
        return fmt.Sprintf("Trajectory stats exported to %s", outputPath), TaskStatusSuccess
}

// ============================================================
// P4: 憑證池 & Profile 管理工具處理函數
// ============================================================

func execCredentialAdd(ec *ToolExecContext) (string, TaskStatus) {
        key, ok := ec.ArgsMap["key"].(string)
        if !ok || key == "" {
                return "Error: 'key' parameter is required", TaskStatusFailed
        }
        priority := 10
        if v, ok := ec.ArgsMap["priority"].(float64); ok {
                priority = int(v)
        }

        if globalCredentialPool == nil {
                return "Error: Credential pool not initialized", TaskStatusFailed
        }
        cred := globalCredentialPool.AddCredential(key, priority)
        return fmt.Sprintf("Credential added: ID=%s, Priority=%d, Masked=%s",
                cred.ID, cred.Priority, MaskAPIKey(cred.Key)), TaskStatusSuccess
}

func execCredentialList(ec *ToolExecContext) (string, TaskStatus) {
        if globalCredentialPool == nil {
                return "Error: Credential pool not initialized", TaskStatusFailed
        }
        creds := globalCredentialPool.GetAllCredentials()
        if len(creds) == 0 {
                return "No credentials registered.", TaskStatusSuccess
        }
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("=== Credential Pool (%d credentials) ===\n", len(creds)))
        for _, c := range creds {
                status := "healthy"
                if !time.Now().Before(c.CooldownUntil) && c.CooldownUntil.After(time.Time{}) {
                        status = "cooldown"
                }
                sb.WriteString(fmt.Sprintf("  ID: %s | Key: %s | Priority: %d | Status: %s | Usage: %d\n",
                        c.ID, MaskAPIKey(c.Key), c.Priority, status, c.UsageCount))
        }
        sb.WriteString(fmt.Sprintf("Healthy count: %d\n", globalCredentialPool.GetHealthyCredentialCount()))
        return sb.String(), TaskStatusSuccess
}

func execProfileCreate(ec *ToolExecContext) (string, TaskStatus) {
        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: 'name' parameter is required", TaskStatusFailed
        }
        if globalProfileManager == nil {
                return "Error: Profile manager not initialized", TaskStatusFailed
        }
        description := ""
        if v, ok := ec.ArgsMap["description"].(string); ok {
                description = v
        }
        profile := &ManagedProfile{
                ID:          "",
                Name:        name,
                Description: description,
        }
        if modelID, ok := ec.ArgsMap["model_id"].(string); ok && modelID != "" {
                profile.Model.ModelID = modelID
        }
        if err := globalProfileManager.CreateProfile(profile); err != nil {
                return fmt.Sprintf("Error creating profile: %v", err), TaskStatusFailed
        }
        return fmt.Sprintf("Profile created: ID=%s, Name=%s", profile.ID, profile.Name), TaskStatusSuccess
}

func execProfileSwitch(ec *ToolExecContext) (string, TaskStatus) {
        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: 'name' parameter is required", TaskStatusFailed
        }
        if globalProfileManager == nil {
                return "Error: Profile manager not initialized", TaskStatusFailed
        }
        profile, found := globalProfileManager.GetProfile(name)
        if !found || profile == nil {
                return fmt.Sprintf("Error: Profile '%s' not found", name), TaskStatusFailed
        }
        if err := globalProfileManager.SetActiveProfile(profile.ID); err != nil {
                return fmt.Sprintf("Error switching profile: %v", err), TaskStatusFailed
        }
        return fmt.Sprintf("Switched to profile: %s (%s)", profile.Name, profile.ID), TaskStatusSuccess
}

func execProfileList(ec *ToolExecContext) (string, TaskStatus) {
        if globalProfileManager == nil {
                return "Error: Profile manager not initialized", TaskStatusFailed
        }
        profiles := globalProfileManager.ListProfiles()
        activeID := ""
        if ap, err := globalProfileManager.GetActiveProfile(); err == nil && ap != nil {
                activeID = ap.ID
        }
        if len(profiles) == 0 {
                return "No profiles created. Use 'profile_create' to create one.", TaskStatusSuccess
        }
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("=== Profiles (%d) ===\n", len(profiles)))
        for _, p := range profiles {
                marker := " "
                if p.ID == activeID {
                        marker = "*"
                }
                sb.WriteString(fmt.Sprintf("  %s [%s] %s - %s (model: %s)\n",
                        marker, p.ID, p.Name, p.Description, p.Model.ModelID))
        }
        sb.WriteString("  (* = active)\n")
        return sb.String(), TaskStatusSuccess
}

// ============================================================
// 技能演化工具處理函數
// ============================================================

// execSkillCleanupSuggest 生成技能清理建議（基於使用統計）
func execSkillCleanupSuggest(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }
        evo := globalSkillManagerV2.EvolutionOptimizer()
        if evo == nil {
                return "Error: skill evolution optimizer not available", TaskStatusFailed
        }
        suggestions, err := evo.GenerateCleanupSuggestions()
        if err != nil {
                return fmt.Sprintf("Error generating cleanup suggestions: %v", err), TaskStatusFailed
        }
        if len(suggestions) == 0 {
                return "No cleanup suggestions — all skills appear healthy.", TaskStatusSuccess
        }
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("=== Skill Cleanup Suggestions (%d) ===\n", len(suggestions)))
        for i, s := range suggestions {
                sb.WriteString(fmt.Sprintf("%d. [%s] action=%s reason=%s\n", i+1, s.SkillName, s.Action, s.Reason))
        }
        return sb.String(), TaskStatusSuccess
}

// execSkillAutoTag 自動為指定技能生成標籤
func execSkillAutoTag(ec *ToolExecContext) (string, TaskStatus) {
        if globalSkillManagerV2 == nil {
                return "Error: skill manager v2 not initialized", TaskStatusFailed
        }
        name, ok := ec.ArgsMap["name"].(string)
        if !ok || name == "" {
                return "Error: missing required parameter 'name'", TaskStatusFailed
        }
        evo := globalSkillManagerV2.EvolutionOptimizer()
        if evo == nil {
                return "Error: skill evolution optimizer not available", TaskStatusFailed
        }
        tags, err := evo.AutoTagSkill(name)
        if err != nil {
                return fmt.Sprintf("Error auto-tagging skill '%s': %v", name, err), TaskStatusFailed
        }
        if len(tags) == 0 {
                return fmt.Sprintf("No tags generated for skill '%s'.", name), TaskStatusSuccess
        }
        return fmt.Sprintf("Auto-generated tags for '%s': %v", name, tags), TaskStatusSuccess
}
