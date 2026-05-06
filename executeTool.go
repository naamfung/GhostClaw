package main

import (
        "bufio"
        "context"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "regexp"
        "strconv"
        "strings"
        "time"

        "github.com/toon-format/toon-go"
        yaml "gopkg.in/yaml.v3"
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
        if globalTasksMode.IsActive() {
                phaseName, msg, err := advanceTasksPhase()
                if err != nil {
                        return "錯誤：" + err.Error(), TaskStatusFailed
                }
                _ = phaseName
                return msg, TaskStatusSuccess
        }
        return "錯誤：Tasks Mode 未激活。使用 Tasks(PlanPhase=\"explore\") 進入。", TaskStatusFailed
}

func execPrevPhase(ec *ToolExecContext) (string, TaskStatus) {
        if globalTasksMode.IsActive() {
                msg, ok := handleTasksPrevPhase()
                if !ok {
                        return "錯誤：" + msg, TaskStatusFailed
                }
                return msg, TaskStatusSuccess
        }
        return "錯誤：Tasks Mode 未激活。", TaskStatusFailed
}

func execTasks(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTasks(ec.ArgsMap)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSmartShellTool(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSmartShell(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
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
        lineNumFloat, ok2 := ec.ArgsMap["LineNum"].(float64)
        if !ok1 || !ok2 || filename == "" || lineNumFloat < 1 {
                return "Error: Invalid arguments for ReadFileLine", TaskStatusFailed
        }

        // 二進制文件檢測
        if isBinaryFile(filename) {
                return getFileTypeDescription(filename), TaskStatusSuccess
        }

        lineNum := int(lineNumFloat)
        c, err := ReadFileLine(filename, lineNum)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 标记文件已读取特定行（先读后写安全检查 - 記錄精確行號以便 WriteFileLine 檢查寫入權限）
        globalReadWriteTracker.MarkFileLineRead(filename, lineNum)

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
                        "Content":   c,
                        "Line":      lineNum,
                        "Filename":  filename,
                        "Encoding":  "utf-8", // 假设 UTF-8 编码
                }
                if info != nil {
                        result["FileSize"] = info.Size()
                        result["Modified"] = info.ModTime().Format(time.RFC3339)
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
        lineNumFloat, ok2 := ec.ArgsMap["LineNum"].(float64)
        text, ok3 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || !ok3 || filename == "" {
                return "Error: Invalid arguments for WriteFileLine", TaskStatusFailed
        }

        lineNum := int(lineNumFloat)
        var content string
        var failed bool
        if lineNum == 0 {
                // 创建空文件
                file, err := os.Create(filename)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        file.Close()
                        content = "Successfully created empty file: " + filename
                }
        } else if lineNum == -1 {
                // 追加到文件末尾
                err := AppendFileLine(filename, text)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = "Successfully appended to end of file"
                }
        } else if lineNum < -1 {
                // 插入到指定行之前
                insertBefore := -lineNum
                err := InsertFileLine(filename, insertBefore, text)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = "Successfully inserted before line " + strconv.Itoa(insertBefore)
                }
        } else {
                // 写入指定行（覆写）
                err := WriteFileLine(filename, lineNum, text)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = "Successfully wrote to line " + strconv.Itoa(lineNum)
                }
        }
        fmt.Println(content)
        if failed {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// getEffectiveContextLength 獲取當前有效模型的上下文長度（token 數量）。
// 優先級：當前 session 嘅 active actor model > main model > globalConfig.Models fallback > 安全默認值。
func getEffectiveContextLength() int {
        // 1) 優先：當前 session 嘅 active actor model（同 CallModel 保持一致）
        if globalStage != nil {
                currentActor := globalStage.GetCurrentActor()
                if modelConfig := getActorModelConfig(currentActor); modelConfig != nil {
                        if modelConfig.Model != "" {
                                return GetModelContextLengthSafe(modelConfig.Model)
                        }
                }
        }
        // 2) 退而求其次：globalConfigManager 嘅 main model
        if globalConfigManager != nil {
                if mainModel := globalConfigManager.GetMainModel(); mainModel != nil && mainModel.Model != "" {
                        return GetModelContextLengthSafe(mainModel.Model)
                }
        }
        // 3) Fallback：iterate globalConfig.Models（保留兼容性）
        if globalConfig.Models != nil {
                for _, m := range globalConfig.Models {
                        if m.Model != "" {
                                return GetModelContextLengthSafe(m.Model)
                        }
                }
        }
        // 4) 安全默認值
        return GetModelContextLengthSafe("")
}

// maxReadFileLinesFraction 定義 ReadFileLines 允許讀取的最大文件大小比例。
// 若文件估算 token 數超過上下文長度的此比例，則拒絕整文件讀取並建議使用範圍讀取工具。
const maxReadFileLinesFraction = 0.5

func execReadFileLines(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok := ec.ArgsMap["filename"].(string)
        if !ok || filename == "" {
                return "Error: Invalid arguments for ReadFileLines", TaskStatusFailed
        }

        // 二進制文件檢測
        if isBinaryFile(filename) {
                return getFileTypeDescription(filename), TaskStatusSuccess
        }

        // 防禦性檢查：若文件大小超過上下文長度的 50%，拒絕整文件讀取，
        // 建議模型使用 ReadFileRange 進行範圍讀取
        if info, statErr := os.Stat(filename); statErr == nil {
                fileSize := info.Size()
                contextLen := getEffectiveContextLength()
                // 粗略估算：1 token ≈ 4 bytes，50% 上下文 ≈ contextLen * 2 bytes
                maxSafeBytes := int64(float64(contextLen) * maxReadFileLinesFraction * 4)
                if maxSafeBytes > 0 && fileSize > maxSafeBytes {
                        // 由於 ReadFileLines 已被調用這一次，標記文件為已完整讀取，
                        // 以便後續寫入操作不受阻
                        globalReadWriteTracker.MarkFileFullyRead(filename)
                        return fmt.Sprintf(
                                "Warning: 文件過大，拒絕整文件讀取。\n"+
                                        "文件大小: %d bytes (約 %d tokens)\n"+
                                        "模型上下文上限: %d tokens\n"+
                                        "超出安全閾值: %.0f%% (limit: %.0f%%)\n\n"+
                                        "請改用 ReadFileRange 指定行範圍進行部分讀取，或使用其他範圍工具讀寫文件。\n"+
                                        "文件已標記為完整讀取狀態，後續寫入操作不受影響。",
                                fileSize, fileSize/4,
                                contextLen,
                                float64(fileSize)*100/float64(contextLen*4), maxReadFileLinesFraction*100,
                        ), TaskStatusSuccess
                }
        }

        lines, err := ReadFileLines(filename)
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
        var failed bool
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
                                "Line":    i + 1,
                                "Content": line,
                        }
                }

                result := map[string]interface{}{
                        "Lines":      linedContent,
                        "TotalLines": len(lines),
                        "Filename":   filename,
                        "Encoding":   "utf-8", // 假设 UTF-8 编码
                }
                if info != nil {
                        result["FileSize"] = info.Size()
                        result["Modified"] = info.ModTime().Format(time.RFC3339)
                }

                resultTOON, err := toon.Marshal(result)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = string(resultTOON)
                }
        } else {
                // 默认只返回内容列表
                resultTOON, err := toon.Marshal(lines)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = string(resultTOON)
                }
        }
        fmt.Println(TruncateString(content, 200))
        if failed {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execReadFileRange(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        startLineFloat, ok2 := ec.ArgsMap["StartLine"].(float64)
        if !ok1 || !ok2 || filename == "" || startLineFloat < 1 {
                return "Error: Invalid arguments for ReadFileRange", TaskStatusFailed
        }

        // 二進制文件檢測
        if isBinaryFile(filename) {
                return getFileTypeDescription(filename), TaskStatusSuccess
        }

        startLine := int(startLineFloat)
        endLine := startLine
        if endLineFloat, ok := ec.ArgsMap["EndLine"].(float64); ok && endLineFloat >= float64(startLine) {
                endLine = int(endLineFloat)
        }

        lines, err := ReadFileRange(filename, startLine, endLine)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }

        // 标记文件已讀取精確範圍（先讀後寫安全檢查 - 記錄範圍以便 WriteFileRange 檢查交集寫入權限）
        globalReadWriteTracker.MarkFileRangeRead(filename, startLine, endLine)

        // 检查是否需要详细信息
        verbose := false
        if v, ok := ec.ArgsMap["verbose"].(bool); ok {
                verbose = v
        }

        var content string
        var failed bool
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
                                "Line":    startLine + i,
                                "Content": line,
                        }
                }

                result := map[string]interface{}{
                        "Lines":      linedContent,
                        "TotalLines": len(lines),
                        "StartLine":  startLine,
                        "EndLine":    endLine,
                        "Filename":   filename,
                        "Encoding":   "utf-8",
                }
                if info != nil {
                        result["FileSize"] = info.Size()
                        result["Modified"] = info.ModTime().Format(time.RFC3339)
                }

                resultTOON, err := toon.Marshal(result)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = string(resultTOON)
                }
        } else {
                // 默认只返回内容列表
                resultTOON, err := toon.Marshal(lines)
                if err != nil {
                        content = "Error: " + err.Error()
                        failed = true
                } else {
                        content = string(resultTOON)
                }
        }
        fmt.Println(TruncateString(content, 200))
        if failed {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// execFileInfo 獲取文件的詳細信息（類似 Unix file 命令）
func execFileInfo(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok := ec.ArgsMap["filename"].(string)
        if !ok || filename == "" {
                return "Error: Invalid arguments for file_info", TaskStatusFailed
        }

        info, err := os.Stat(filename)
        if err != nil {
                return fmt.Sprintf("Error: 無法讀取檔案: %v", err), TaskStatusFailed
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("📄 **檔案資訊**: %s\n\n", filepath.Base(filename)))

        // 基本檔案屬性
        sb.WriteString(fmt.Sprintf("- **完整路徑**: %s\n", filename))
        sb.WriteString(fmt.Sprintf("- **檔案大小**: %s\n", formatFileSize(info.Size())))
        sb.WriteString(fmt.Sprintf("- **修改時間**: %s\n", info.ModTime().Format(time.RFC3339)))
        sb.WriteString(fmt.Sprintf("- **權限**: %s\n", info.Mode().String()))
        sb.WriteString(fmt.Sprintf("- **副檔名**: %s\n", filepath.Ext(filename)))

        // 檢測是否為二進制文件
        isBin := isBinaryFile(filename)
        if isBin {
                sb.WriteString(fmt.Sprintf("- **類型**: 二進制文件\n"))
        } else {
                sb.WriteString(fmt.Sprintf("- **類型**: 純文字文件\n"))
        }

        // MIME 類型（優先 magic bytes，次選副檔名比對）
        mimeType := detectMIMEType(filename)
        if mimeType != "" {
                sb.WriteString(fmt.Sprintf("- **MIME 類型**: %s\n", mimeType))
        }

        // 使用系統 file 命令獲取詳細描述
        fileDesc := runFileCommand(filename)
        if fileDesc != "" {
                sb.WriteString(fmt.Sprintf("- **系統 file 描述**: %s\n", fileDesc))
        }

        // 對於文字檔案，顯示編碼資訊
        if !isBin {
                enc := detectTextEncoding(filename)
                if enc != "" {
                        sb.WriteString(fmt.Sprintf("- **編碼**: %s\n", enc))
                }
                // 顯示行數和字符數
                lineCount, charCount := countLinesAndChars(filename)
                if lineCount >= 0 {
                        sb.WriteString(fmt.Sprintf("- **行數**: %d\n", lineCount))
                        sb.WriteString(fmt.Sprintf("- **字符數**: %d\n", charCount))
                }
        }

        return sb.String(), TaskStatusSuccess
}

// detectTextEncoding 檢測文字檔案的編碼
func detectTextEncoding(path string) string {
        data, err := os.ReadFile(path)
        if err != nil {
                return ""
        }
        if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
                return "UTF-8 with BOM"
        }
        if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
                return "UTF-16 BE"
        }
        if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
                return "UTF-16 LE"
        }
        // 快速檢查是否為合法 UTF-8
        if isValidUTF8(data) {
                return "UTF-8"
        }
        return "未知編碼（可能為 ANSI/Latin1 或二進制）"
}

// isValidUTF8 檢查數據是否為合法 UTF-8
func isValidUTF8(data []byte) bool {
        i := 0
        for i < len(data) {
                if data[i] < 0x80 {
                        i++
                        continue
                }
                // 計算後續字節數
                var count int
                if (data[i] & 0xE0) == 0xC0 {
                        count = 1
                } else if (data[i] & 0xF0) == 0xE0 {
                        count = 2
                } else if (data[i] & 0xF8) == 0xF0 {
                        count = 3
                } else {
                        return false
                }
                i++
                for j := 0; j < count; j++ {
                        if i >= len(data) || (data[i] & 0xC0) != 0x80 {
                                return false
                        }
                        i++
                }
        }
        return true
}

// countLinesAndChars 計算文字檔案的行數和字符數
func countLinesAndChars(path string) (lines int, chars int) {
        data, err := os.ReadFile(path)
        if err != nil {
                return -1, -1
        }
        content := string(data)
        return strings.Count(content, "\n"), len([]rune(content))
}

func execWriteFileLines(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        linesInterface, ok2 := ec.ArgsMap["lines"].([]interface{})
        if !ok1 || !ok2 || filename == "" {
                return "Error: Invalid arguments for WriteFileLines", TaskStatusFailed
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
                err = WriteFileLines(filename, lines)
        }

        var content string
        if err != nil {
                content = "Error: " + err.Error()
                fmt.Println(content)
                return content, TaskStatusFailed
        }
        if appendMode {
                content = "Successfully appended " + strconv.Itoa(len(lines)) + " lines to " + filename
        } else {
                content = "Successfully wrote " + strconv.Itoa(len(lines)) + " lines to " + filename
        }
        fmt.Println(content)
        return content, TaskStatusSuccess
}

func execAppendToFile(ec *ToolExecContext) (string, TaskStatus) {
        filename, ok1 := ec.ArgsMap["filename"].(string)
        contentStr, ok2 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || filename == "" {
                return "Error: Invalid arguments for AppendToFile", TaskStatusFailed
        }

        lineBreak := true
        if lineBreakVal, ok := ec.ArgsMap["LineBreak"].(bool); ok {
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
        startLineFloat, ok2 := ec.ArgsMap["StartLine"].(float64)
        contentStr, ok3 := ec.ArgsMap["content"].(string)
        if !ok1 || !ok2 || !ok3 || filename == "" || startLineFloat == 0 {
                return "Error: Invalid arguments for WriteFileRange (StartLine cannot be 0)", TaskStatusFailed
        }

        startLine := int(startLineFloat)
        endLine := startLine
        if endLineFloat, ok := ec.ArgsMap["EndLine"].(float64); ok {
                endLine = int(endLineFloat)
        }

        // For insert mode (StartLine < 0), endLine is ignored
        isInsert := startLine < 0

        err := WriteFileRange(filename, startLine, endLine, contentStr)
        if err != nil {
                content := "Error: " + err.Error()
                fmt.Println(content)
                return content, TaskStatusFailed
        }
        var content string
        if isInsert {
                content = "Successfully inserted " + strconv.Itoa(len(strings.Split(contentStr, "\n"))) + " lines before line " + strconv.Itoa(-startLine)
        } else if startLine == endLine {
                content = "Successfully wrote to line " + strconv.Itoa(startLine)
        } else {
                content = "Successfully wrote to lines " + strconv.Itoa(startLine) + "-" + strconv.Itoa(endLine)
        }
        fmt.Println(content)
        return content, TaskStatusSuccess
}

// ========== 浏览器工具 handlers ==========

func execBrowserSearch(ec *ToolExecContext) (string, TaskStatus) {
        keyword, ok := ec.ArgsMap["keyword"].(string)
        if !ok || keyword == "" {
                return "Error: Empty keyword in BrowserSearch tool call", TaskStatusFailed
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
                return "Error: Empty url in BrowserVisit tool call", TaskStatusFailed
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
                return "Error: Empty url in BrowserDownload tool call", TaskStatusFailed
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
                return "Error: Empty url in BrowserClick tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in BrowserClick tool call", TaskStatusFailed
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
                return "Error: Empty url in BrowserType tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in BrowserType tool call", TaskStatusFailed
        }

        text, ok := ec.ArgsMap["text"].(string)
        if !ok {
                return "Error: Empty text in BrowserType tool call", TaskStatusFailed
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
                return "Error: Empty url in BrowserScroll tool call", TaskStatusFailed
        }

        direction, ok := ec.ArgsMap["direction"].(string)
        if !ok || direction == "" {
                return "Error: Empty direction in BrowserScroll tool call", TaskStatusFailed
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
                return "Error: Empty url in browser_ExtractLinks tool call", TaskStatusFailed
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
                return "Error: Empty url in browser_ExtractImages tool call", TaskStatusFailed
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
                return "Error: Empty url in browser_ExtractElements tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_ExtractElements tool call", TaskStatusFailed
        }

        includeHTML, _ := ec.ArgsMap["IncludeHtml"].(bool)
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
                return "Error: Empty url in BrowserScreenshot tool call", TaskStatusFailed
        }

        fullPage, _ := ec.ArgsMap["FullPage"].(bool)
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
                return "Error: Empty url in browser_ExecuteJs tool call", TaskStatusFailed
        }

        script, ok := ec.ArgsMap["script"].(string)
        if !ok || script == "" {
                return "Error: Empty script in browser_ExecuteJs tool call", TaskStatusFailed
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

        formDataRaw, ok := ec.ArgsMap["FormData"].(map[string]interface{})
        if !ok {
                return "Error: Invalid form_data in browser_fill_form tool call", TaskStatusFailed
        }

        formData := make(FormData)
        for k, v := range formDataRaw {
                if strVal, ok := v.(string); ok {
                        formData[k] = strVal
                }
        }

        submitSelector, _ := ec.ArgsMap["SubmitSelector"].(string)
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
                return "Error: Empty url in browser_DoubleClick tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_DoubleClick tool call", TaskStatusFailed
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
                return "Error: Empty url in browser_RightClick tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_RightClick tool call", TaskStatusFailed
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

        sourceSelector, ok := ec.ArgsMap["SourceSelector"].(string)
        if !ok || sourceSelector == "" {
                return "Error: Empty source_selector in browser_drag tool call", TaskStatusFailed
        }

        targetSelector, ok := ec.ArgsMap["TargetSelector"].(string)
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
                return "Error: Empty url in BrowserNavigate tool call", TaskStatusFailed
        }

        action, ok := ec.ArgsMap["action"].(string)
        if !ok || action == "" {
                return "Error: Empty action in BrowserNavigate tool call", TaskStatusFailed
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
                fmt.Println("Browser navigate failed:", action)
                return "Error: " + err.Error(), TaskStatusFailed
        }
        if result != nil {
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

        filePath, _ := ec.ArgsMap["FilePath"].(string)

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

        filePath, ok := ec.ArgsMap["FilePath"].(string)
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
        if d, ok := ec.ArgsMap["MaxDepth"].(float64); ok {
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

        filePathsRaw, ok := ec.ArgsMap["FilePaths"].([]interface{})
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
                return "Error: Empty url in browser_ElementScreenshot tool call", TaskStatusFailed
        }

        selector, ok := ec.ArgsMap["selector"].(string)
        if !ok || selector == "" {
                return "Error: Empty selector in browser_ElementScreenshot tool call", TaskStatusFailed
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
        filePath, ok := ec.ArgsMap["FilePath"].(string)
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

        userAgent, ok := ec.ArgsMap["UserAgent"].(string)
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
                return "Error: Missing 'action' parameter in browser_interact call. Valid actions: Click, DoubleClick, Hover, RightClick, Type, Scroll, Drag", TaskStatusFailed
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
        case "DoubleClick":
                result, err = BrowserDoubleClick(sessionID, url, selector)
        case "hover":
                result, err = BrowserHover(sessionID, url, selector)
        case "RightClick":
                result, err = BrowserRightClick(sessionID, url, selector)
        case "type":
                result, err = BrowserType(sessionID, url, selector, text, submit, timeout)
        case "scroll":
                result, err = BrowserScroll(sessionID, url, direction, amount, timeout)
        case "drag":
                // drag 需要兩個 selector：source 和 target
                targetSelector, _ := ec.ArgsMap["TargetSelector"].(string)
                if targetSelector == "" {
                        targetSelector = selector
                }
                result, err = BrowserDrag(sessionID, url, selector, targetSelector)
        default:
                return "Error: Invalid action '" + action + "' in browser_interact. Valid: click, DoubleClick, hover, RightClick, type, scroll, drag", TaskStatusFailed
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
                return "Error: Missing 'mode' parameter in browser_extract call. Valid modes: Screenshot, ExecuteJs, ExtractLinks, ExtractImages, ExtractElements, Snapshot, Pdf, ElementScreenshot", TaskStatusFailed
        }

        url, _ := ec.ArgsMap["url"].(string)
        selector, _ := ec.ArgsMap["selector"].(string)
        script, _ := ec.ArgsMap["script"].(string)
        fullPage, _ := ec.ArgsMap["FullPage"].(bool)
        includeHTML, _ := ec.ArgsMap["IncludeHtml"].(bool)
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
        case "ExecuteJs":
                result, err = BrowserExecuteJS(sessionID, url, script, timeout)
        case "ExtractLinks":
                result, err = BrowserExtractLinks(sessionID, url, timeout)
        case "ExtractImages":
                result, err = BrowserExtractImages(sessionID, url, timeout)
        case "ExtractElements":
                result, err = BrowserExtractElements(sessionID, url, selector, includeHTML, timeout)
        case "snapshot":
                maxDepth := 5
                if d, ok := ec.ArgsMap["MaxDepth"].(float64); ok {
                        maxDepth = int(d)
                }
                result, err = BrowserSnapshot(sessionID, url, maxDepth)
        case "pdf":
                result, err = BrowserPDF(sessionID, url, timeout)
        case "ElementScreenshot":
                result, err = BrowserElementScreenshot(sessionID, url, selector)
        default:
                return "Error: Invalid mode '" + mode + "' in browser_extract. Valid: screenshot, ExecuteJs, ExtractLinks, ExtractImages, ExtractElements, snapshot, pdf, ElementScreenshot", TaskStatusFailed
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
        submitSelector, _ := ec.ArgsMap["SubmitSelector"].(string)
        timeout := 0
        if t, ok := ec.ArgsMap["timeout"].(float64); ok {
                timeout = int(t)
        }

        // 優先處理 file_path（文件上傳）
        if filePath, ok := ec.ArgsMap["FilePath"].(string); ok && filePath != "" {
                fileSelector, _ := ec.ArgsMap["FileSelector"].(string)
                if fileSelector == "" {
                        fileSelector, _ = ec.ArgsMap["selector"].(string)
                }
                if fileSelector == "" {
                        return "Error: Missing 'FileSelector' or 'selector' for file upload in browser_form_fill", TaskStatusFailed
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
        if selectValue, ok := ec.ArgsMap["SelectValue"].(string); ok && selectValue != "" {
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
        formDataRaw, ok := ec.ArgsMap["FormData"].(map[string]interface{})
        if !ok || len(formDataRaw) == 0 {
                return "Error: Missing 'FormData' parameter in browser_form_fill call", TaskStatusFailed
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

// execTodoWrite 批量替換任務列表（V1 模式）。
// 模型每次傳入完整列表，完全取代現有。全部 Completed 時傳 [] 清空。
func execTodoWrite(ec *ToolExecContext) (string, TaskStatus) {
        rawTodos, ok := ec.ArgsMap["todos"].([]interface{})
        if !ok {
                return "Error: todos 必須係 array。正確格式：{\"todos\": [{\"content\":\"...\",\"status\":\"Pending\",\"activeForm\":\"...\"}]}", TaskStatusFailed
        }

        var items []TodoItem
        for i, itemInterface := range rawTodos {
                itemMap, ok := itemInterface.(map[string]interface{})
                if !ok {
                        return fmt.Sprintf("Error: todos[%d] 必須係 object", i), TaskStatusFailed
                }
                content, _ := itemMap["content"].(string)
                status, _ := itemMap["status"].(string)
                if content == "" || status == "" {
                        return fmt.Sprintf("Error: todos[%d] 缺少 content 或 status（必填）", i), TaskStatusFailed
                }
                status = normalizeTodoStatus(status)
                if status != "Pending" && status != "InProgress" && status != "Completed" && status != "Waiting" {
                        return fmt.Sprintf("Error: todos[%d] status 無效：%s（可選：Pending/InProgress/Completed）", i, status), TaskStatusFailed
                }
                items = append(items, TodoItem{
                        ID:     strconv.Itoa(i + 1),
                        Text:   strings.TrimSpace(content),
                        Status: status,
                })
        }

        output, err := TODO.Update(items)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }
        if !TODO.HasUnfinishedItems() && globalTaskTracker != nil {
                globalTaskTracker.MarkCompleted()
                globalTaskTracker.ResetStuckState()
        }
        return output, TaskStatusSuccess
}

func execTodoCreate(ec *ToolExecContext) (string, TaskStatus) {
        content, _ := ec.ArgsMap["content"].(string)
        if content == "" {
                return "Error: content 係必填參數（任務內容描述）", TaskStatusFailed
        }
        status, _ := ec.ArgsMap["status"].(string)
        if status == "" {
                status = "Pending"
        }
        output, err := TODO.Create(content, status)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }
        if !TODO.HasUnfinishedItems() && globalTaskTracker != nil {
                globalTaskTracker.MarkCompleted()
                globalTaskTracker.ResetStuckState()
        }
        return output, TaskStatusSuccess
}

func execTodoUpdate(ec *ToolExecContext) (string, TaskStatus) {
        id, _ := ec.ArgsMap["id"].(string)
        if id == "" {
                return "Error: id 係必填參數（任務唯一標識）", TaskStatusFailed
        }
        content, _ := ec.ArgsMap["content"].(string)
        status, _ := ec.ArgsMap["status"].(string)
        output, err := TODO.UpdateSingle(id, content, status)
        if err != nil {
                return "Error: " + err.Error(), TaskStatusFailed
        }
        if !TODO.HasUnfinishedItems() && globalTaskTracker != nil {
                globalTaskTracker.MarkCompleted()
                globalTaskTracker.ResetStuckState()
        }
        return output, TaskStatusSuccess
}

func execTodoList(ec *ToolExecContext) (string, TaskStatus) {
        return TODO.Render(), TaskStatusSuccess
}

// --- Wrappers for existing handler functions ---

func execCronAdd(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleCronAdd(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execCronRemove(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleCronRemove(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execCronList(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleCronList(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execCronStatus(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleCronStatus(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execMemorySave(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleMemorySave(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execMemoryRecall(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleMemoryRecall(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execMemoryForget(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleMemoryForget(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execMemoryList(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleMemoryList(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execProfileCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleProfileCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execActorIdentitySet(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleActorIdentitySet(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execActorIdentityClear(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleActorIdentityClear(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execProfileReload(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleProfileReload(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
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
        if pageSize, ok := ec.ArgsMap["PageSize"].(float64); ok {
                req.PageSize = int(pageSize)
        }
        if search, ok := ec.ArgsMap["search"].(string); ok {
                req.Search = search
        }
        if sortBy, ok := ec.ArgsMap["SortBy"].(string); ok {
                req.SortBy = sortBy
        }
        if sortOrder, ok := ec.ArgsMap["SortOrder"].(string); ok {
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
        if suggestOnly, ok := ec.ArgsMap["SuggestOnly"].(bool); ok {
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
        systemPrompt, ok2 := ec.ArgsMap["SystemPrompt"].(string)
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
        if triggerWords, ok := ec.ArgsMap["TriggerWords"].([]interface{}); ok {
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

        if displayName, ok := ec.ArgsMap["DisplayName"].(string); ok && displayName != "" {
                updates["DisplayName"] = displayName
        }
        if description, ok := ec.ArgsMap["description"].(string); ok && description != "" {
                updates["description"] = description
        }
        if systemPrompt, ok := ec.ArgsMap["SystemPrompt"].(string); ok && systemPrompt != "" {
                updates["SystemPrompt"] = systemPrompt
        }
        if triggerWords, ok := ec.ArgsMap["TriggerWords"].([]interface{}); ok && len(triggerWords) > 0 {
                var triggers []string
                for _, tw := range triggerWords {
                        if twStr, ok := tw.(string); ok && twStr != "" {
                                triggers = append(triggers, twStr)
                        }
                }
                updates["TriggerWords"] = triggers
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
        if k, ok := ec.ArgsMap["TopK"].(float64); ok {
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
                return "Error: Empty keyword in TextSearch tool call", TaskStatusFailed
        }

        opts := TextSearchOptions{}
        if rootDir, ok := ec.ArgsMap["RootDir"].(string); ok && rootDir != "" {
                opts.RootDir = rootDir
        }
        if filePattern, ok := ec.ArgsMap["FilePattern"].(string); ok {
                opts.FilePattern = filePattern
        }
        if ignoreCase, ok := ec.ArgsMap["IgnoreCase"].(bool); ok {
                opts.IgnoreCase = ignoreCase
        }
        if useRegex, ok := ec.ArgsMap["UseRegex"].(bool); ok {
                opts.UseRegex = useRegex
        }
        if maxDepth, ok := ec.ArgsMap["MaxDepth"].(float64); ok {
                opts.MaxDepth = int(maxDepth)
        }
        if maxResults, ok := ec.ArgsMap["MaxResults"].(float64); ok {
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
        content, ok := handleTextReplace(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTextGrep(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTextSearch(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTextTransform(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTextTransform(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// --- Plugin tool handlers ---

func execPluginCreate(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginCreate(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginList(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginList(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginLoad(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginLoad(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginUnload(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginUnload(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginReload(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginReload(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginCall(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginCall(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginCompile(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginCompile(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginDelete(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginDelete(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginAPIs(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginAPIs(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execPluginDetail(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handlePluginDetail(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// --- Shell delayed tool handlers ---

func execShellDelayed(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleDelayedExec(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTaskCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTaskCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTaskTerminate(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTaskTerminate(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTaskList(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTaskList(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTaskWait(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTaskWait(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execTaskRemove(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleTaskRemove(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
        	return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// --- Spawn tool handlers ---

func execSpawn(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSpawn(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSpawnCheck(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSpawnCheck(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSpawnList(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSpawnList(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSpawnCancel(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSpawnCancel(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

func execSpawnBatch(ec *ToolExecContext) (string, TaskStatus) {
        content, ok := handleSpawnBatch(ec.Ctx, ec.ArgsMap, ec.Ch)
        if !ok {
                return content, TaskStatusFailed
        }
        return content, TaskStatusSuccess
}

// --- Other tool handlers ---

func execConsolidateMemory(ec *ToolExecContext) (string, TaskStatus) {
        content, err := HandleConsolidateMemory(ec.ArgsMap)
        if err != nil {
                return fmt.Sprintf("Error: %v", err), TaskStatusFailed
        }
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

// --- WebRead post-processing helpers ---

// webReadMetadata holds the YAML metadata output by opencli web read.
type webReadMetadata struct {
        Title       string `yaml:"title"`
        Author      string `yaml:"author"`
        PublishTime string `yaml:"publish_time"`
        Status      int    `yaml:"status"`
        Size        int64  `yaml:"size"`
}

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var whitespaceSeq = regexp.MustCompile(`\s+`)
var multiUnderscore = regexp.MustCompile(`_+`)

// sanitizeWebFilename sanitizes a title into a safe directory/file name,
// mirroring the JS sanitizeFilename logic in opencli.
func sanitizeWebFilename(name string, maxLength int) string {
        if name == "" {
                return "untitled"
        }
        // Replace invalid filename chars
        safe := invalidFilenameChars.ReplaceAllString(name, "_")
        // Replace whitespace sequences with underscore
        safe = whitespaceSeq.ReplaceAllString(safe, "_")
        // Collapse consecutive underscores
        safe = multiUnderscore.ReplaceAllString(safe, "_")
        // Trim leading/trailing underscores
        safe = strings.Trim(safe, "_")
        if safe == "" {
                return "untitled"
        }
        if len(safe) > maxLength {
                safe = safe[:maxLength]
        }
        return safe
}

// readWebArticleFile parses opencli YAML metadata from stdout, reconstructs
// the saved markdown file path, and reads its content. Returns the content
// string, file path, and whether the operation succeeded.
func readWebArticleFile(stdout, outputDir string) (content, filePath string, ok bool) {
        var meta webReadMetadata
        if err := yaml.Unmarshal([]byte(stdout), &meta); err != nil {
                return "", "", false
        }
        if meta.Title == "" {
                return "", "", false
        }
        safeTitle := sanitizeWebFilename(meta.Title, 80)
        articlePath := filepath.Join(outputDir, safeTitle, safeTitle+".md")
        data, err := os.ReadFile(articlePath)
        if err != nil {
                return "", articlePath, false
        }
        return string(data), articlePath, true
}

func execOpenCLITool(ec *ToolExecContext) (string, TaskStatus) {
        action, ok := ec.ArgsMap["action"].(string)
        if !ok || action == "" {
                return "Error: 必须指定 action 参数", TaskStatusFailed
        }

        var opencliCmd string
        var webReadOutputDir string // tracked for post-processing

        switch action {
        // === 数据获取 ===
        case "WebRead":
                url, _ := ec.ArgsMap["url"].(string)
                if url == "" {
                        return "Error: WebRead 需要 url 参数", TaskStatusFailed
                }
                opencliCmd = "opencli web read --url " + url
                if v, _ := ec.ArgsMap["DownloadImages"].(bool); !v {
                        opencliCmd += " --download-images false"
                }
                if v, ok := ec.ArgsMap["wait"].(float64); ok && v > 0 {
                        opencliCmd += fmt.Sprintf(" --wait %.0f", v)
                }
                if v, _ := ec.ArgsMap["output"].(string); v != "" {
                        opencliCmd += " --output " + v
                        webReadOutputDir = v
                } else {
                        webReadOutputDir = "./web-articles"
                }

        case "Adapter":
                site, _ := ec.ArgsMap["site"].(string)
                cmd, _ := ec.ArgsMap["command"].(string)
                if site == "" || cmd == "" {
                        return "Error: Adapter 需要 site 和 command 参数", TaskStatusFailed
                }
                args, _ := ec.ArgsMap["args"].(string)
                opencliCmd = "opencli " + site + " " + cmd
                if args != "" {
                        opencliCmd += " " + args
                }

        case "List":
                format, _ := ec.ArgsMap["format"].(string)
                if format != "" {
                        opencliCmd = "opencli list --format " + format
                } else {
                        opencliCmd = "opencli list"
                }

        // === 适配器开发 ===
        case "Explore":
                url, _ := ec.ArgsMap["url"].(string)
                if url == "" {
                        return "Error: Explore 需要 url 参数", TaskStatusFailed
                }
                opencliCmd = "opencli explore " + url
                if v, _ := ec.ArgsMap["goal"].(string); v != "" {
                        opencliCmd += " --goal " + v
                }
                if v, _ := ec.ArgsMap["AutoFuzz"].(bool); v {
                        opencliCmd += " --auto"
                }

        case "Synthesize":
                site, _ := ec.ArgsMap["site"].(string)
                if site == "" {
                        return "Error: Synthesize 需要 site 参数", TaskStatusFailed
                }
                opencliCmd = "opencli synthesize " + site
                if v, ok := ec.ArgsMap["top"].(float64); ok && v > 0 {
                        opencliCmd += fmt.Sprintf(" --top %.0f", v)
                }

        case "Generate":
                url, _ := ec.ArgsMap["url"].(string)
                if url == "" {
                        return "Error: Generate 需要 url 参数", TaskStatusFailed
                }
                opencliCmd = "opencli generate " + url
                if v, _ := ec.ArgsMap["SiteName"].(string); v != "" {
                        opencliCmd += " --site " + v
                }
                if v, _ := ec.ArgsMap["goal"].(string); v != "" {
                        opencliCmd += " --goal " + v
                }

        case "Validate":
                target, _ := ec.ArgsMap["target"].(string)
                if target != "" {
                        opencliCmd = "opencli validate " + target
                } else {
                        opencliCmd = "opencli validate"
                }

        case "Verify":
                target, _ := ec.ArgsMap["target"].(string)
                if target != "" {
                        opencliCmd = "opencli verify " + target
                } else {
                        opencliCmd = "opencli verify"
                }
                if v, _ := ec.ArgsMap["smoke"].(bool); v {
                        opencliCmd += " --smoke"
                }

        case "Record":
                url, _ := ec.ArgsMap["url"].(string)
                if url == "" {
                        return "Error: Record 需要 url 参数", TaskStatusFailed
                }
                opencliCmd = "opencli record " + url
                if v, _ := ec.ArgsMap["site"].(string); v != "" {
                        opencliCmd += " --site " + v
                }
                if v, _ := ec.ArgsMap["output"].(string); v != "" {
                        opencliCmd += " --out " + v
                }
                if v, ok := ec.ArgsMap["poll"].(float64); ok && v > 0 {
                        opencliCmd += fmt.Sprintf(" --poll %.0f", v)
                }
                if v, ok := ec.ArgsMap["timeout"].(float64); ok && v > 0 {
                        opencliCmd += fmt.Sprintf(" --timeout %.0f", v)
                }

        case "Cascade":
                url, _ := ec.ArgsMap["url"].(string)
                if url == "" {
                        return "Error: Cascade 需要 url 参数", TaskStatusFailed
                }
                opencliCmd = "opencli cascade " + url
                if v, _ := ec.ArgsMap["site"].(string); v != "" {
                        opencliCmd += " --site " + v
                }

        // === 管理 ===
        case "AdapterStatus":
                opencliCmd = "opencli adapter status"

        case "AdapterEject":
                site, _ := ec.ArgsMap["site"].(string)
                if site == "" {
                        return "Error: AdapterEject 需要 site 参数", TaskStatusFailed
                }
                opencliCmd = "opencli adapter eject " + site

        case "AdapterReset":
                if v, _ := ec.ArgsMap["all"].(bool); v {
                        opencliCmd = "opencli adapter reset --all"
                } else if site, _ := ec.ArgsMap["site"].(string); site != "" {
                        opencliCmd = "opencli adapter reset " + site
                } else {
                        return "Error: AdapterReset 需要 site 或 all=true 参数", TaskStatusFailed
                }

        case "Register":
                name, _ := ec.ArgsMap["name"].(string)
                if name == "" {
                        return "Error: Register 需要 name 参数", TaskStatusFailed
                }
                opencliCmd = "opencli register " + name
                if v, _ := ec.ArgsMap["binary"].(string); v != "" {
                        opencliCmd += " --binary " + v
                }
                if v, _ := ec.ArgsMap["InstallCmd"].(string); v != "" {
                        opencliCmd += " --install " + v
                }
                if v, _ := ec.ArgsMap["desc"].(string); v != "" {
                        opencliCmd += " --desc " + v
                }

        case "Install":
                name, _ := ec.ArgsMap["name"].(string)
                if name == "" {
                        return "Error: Install 需要 name 参数", TaskStatusFailed
                }
                opencliCmd = "opencli install " + name

        case "PluginList":
                format, _ := ec.ArgsMap["format"].(string)
                if format != "" {
                        opencliCmd = "opencli plugin list --format " + format
                } else {
                        opencliCmd = "opencli plugin list"
                }

        case "PluginInstall":
                source, _ := ec.ArgsMap["source"].(string)
                if source == "" {
                        return "Error: PluginInstall 需要 source 参数", TaskStatusFailed
                }
                opencliCmd = "opencli plugin install " + source

        case "PluginUninstall":
                name, _ := ec.ArgsMap["name"].(string)
                if name == "" {
                        return "Error: PluginUninstall 需要 name 参数", TaskStatusFailed
                }
                opencliCmd = "opencli plugin uninstall " + name

        case "PluginUpdate":
                if v, _ := ec.ArgsMap["all"].(bool); v {
                        opencliCmd = "opencli plugin update --all"
                } else if name, _ := ec.ArgsMap["name"].(string); name != "" {
                        opencliCmd = "opencli plugin update " + name
                } else {
                        return "Error: PluginUpdate 需要 name 参数或 all=true", TaskStatusFailed
                }

        case "PluginCreate":
                name, _ := ec.ArgsMap["name"].(string)
                if name == "" {
                        return "Error: PluginCreate 需要 name 参数", TaskStatusFailed
                }
                opencliCmd = "opencli plugin create " + name

        case "Doctor":
                opencliCmd = "opencli doctor"

        case "DaemonStop":
                opencliCmd = "opencli daemon stop"

        default:
                return fmt.Sprintf("Error: 未知的 action '%s'。可用：WebRead, Adapter, List, Explore, Synthesize, Generate, Validate, Verify, Record, Cascade, AdapterStatus, AdapterEject, AdapterReset, Register, Install, PluginList, PluginInstall, PluginUninstall, PluginUpdate, PluginCreate, Doctor, DaemonStop", action), TaskStatusFailed
        }

        // 执行 opencli 命令
        result := runShellWithTimeout(ec.Ctx, opencliCmd, false, false)

        if result.ConfirmRequired {
                var confirmResult strings.Builder
                confirmResult.WriteString("⚠️ **确认请求**\n\n")
                confirmResult.WriteString(result.ConfirmMessage)
                confirmResult.WriteString("\n\n---\n")
                confirmResult.WriteString("要强制执行此命令，请使用 SmartShell 运行原始 opencli 命令。\n")

                content := confirmResult.String()
                fmt.Println(content)
                return content, TaskStatusSuccess
        } else if result.Err != nil {
                content := fmt.Sprintf("Error: %v", result.Err)
                if result.Stderr != "" {
                        content += "\n" + result.Stderr
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
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
                        if strings.Contains(strings.ToLower(result.Stderr), "error: unknown command") {
                                helpResult := runShellWithTimeout(ec.Ctx, "opencli help", false, false)
                                if helpResult.Err == nil {
                                        content += "\n\n=== OpenCLI 帮助信息 ===\n" + helpResult.Stdout
                                }
                        }
                        fmt.Println(content)
                        return content, TaskStatusFailed
                }
                // WebRead post-processing: read full markdown content from saved file
                if action == "WebRead" && webReadOutputDir != "" {
                        articleContent, _, ok := readWebArticleFile(content, webReadOutputDir)
                        if ok {
                                content += "\n\n---\n\n" + articleContent
                        }
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
                "Menu":        execMenuTool,
                "Tasks":       execTasks,
                "NextPhase":   execNextPhase,
                "PrevPhase":   execPrevPhase,
                "PlanWrite":   execTasks, // 合入 Tasks
                "PlanRead":    execTasks, // 合入 Tasks

                // Shell tools
                "SmartShell": execSmartShellTool,
                "Opencli":     execOpenCLITool,

                // SSH tools
                "SSHConnect": execSSHConnect,
                "SSHExec":    execSSHExec,
                "SSHList":    execSSHList,
                "SSHClose":   execSSHClose,

                // File tools
                "ReadFileLine":  execReadFileLine,
                "WriteFileLine": execWriteFileLine,
                "ReadFileLines":  execReadFileLines,
                "WriteFileLines": execWriteFileLines,
                "AppendToFile":  execAppendToFile,
                "WriteFileRange": execWriteFileRange,
                "ReadFileRange":  execReadFileRange,
                "FileInfo":       execFileInfo,

                // Browser basic tools
                "BrowserSearch":    execBrowserSearch,
                "BrowserVisit":     execBrowserVisit,
                "BrowserDownload":  execBrowserDownload,

                // Browser enhanced tools
                "BrowserClick":            execBrowserClick,
                "BrowserType":             execBrowserType,
                "BrowserScroll":           execBrowserScroll,
                "BrowserWaitElement":     execBrowserWaitElement,
                "BrowserExtractLinks":    execBrowserExtractLinks,
                "BrowserExtractImages":   execBrowserExtractImages,
                "BrowserExtractElements": execBrowserExtractElements,
                "BrowserScreenshot":       execBrowserScreenshot,
                "BrowserExecuteJs":       execBrowserExecuteJS,
                "BrowserFillForm":        execBrowserFillForm,

                // Browser advanced tools
                "BrowserHover":              execBrowserHover,
                "BrowserDoubleClick":       execBrowserDoubleClick,
                "BrowserRightClick":        execBrowserRightClick,
                "BrowserDrag":               execBrowserDrag,
                "BrowserWaitSmart":         execBrowserWaitSmart,
                "BrowserNavigate":           execBrowserNavigate,
                "BrowserGetCookies":        execBrowserGetCookies,
                "BrowserCookieSave":        execBrowserCookieSave,
                "BrowserCookieLoad":        execBrowserCookieLoad,
                "BrowserSnapshot":           execBrowserSnapshot,
                "BrowserUploadFile":        execBrowserUploadFile,
                "BrowserSelectOption":      execBrowserSelectOption,
                "BrowserKeyPress":          execBrowserKeyPress,
                "BrowserElementScreenshot": execBrowserElementScreenshot,
                "BrowserPdf":                execBrowserPDF,
                "BrowserPdfFromFile":      execBrowserPDFFromFile,
                "BrowserSetHeaders":        execBrowserSetHeaders,
                "BrowserSetUserAgent":     execBrowserSetUserAgent,
                "BrowserEmulateDevice":     execBrowserEmulateDevice,

                // 合併瀏覽器工具（聚合分發，對應 GetConsolidatedBrowserTools）
                "BrowserInteract":   execBrowserInteract,
                "BrowserExtract":    execBrowserExtract,
                "BrowserFormFill":  execBrowserFormFill,

                // Todo tools
                "TodoWrite":  execTodoWrite,
                "TodoCreate": execTodoCreate,
                "TodoUpdate": execTodoUpdate,
                "TodoList":   execTodoList,

                // Cron tools
                "CronAdd":    execCronAdd,
                "CronRemove": execCronRemove,
                "CronList":   execCronList,
                "CronStatus": execCronStatus,

                // Memory tools
                "MemorySave":   execMemorySave,
                "MemoryRecall": execMemoryRecall,
                "MemoryForget": execMemoryForget,
                "MemoryList":   execMemoryList,

                // Profile tools
                "ProfileCheck":        execProfileCheck,
                "ActorIdentitySet":   execActorIdentitySet,
                "ActorIdentityClear": execActorIdentityClear,
                "ProfileReload":       execProfileReload,

                // Skill tools
                "SkillList":     execSkillList,
                "SkillCreate":   execSkillCreate,
                "SkillDelete":   execSkillDelete,
                "SkillGet":      execSkillGet,
                "SkillReload":   execSkillReload,
                "SkillUpdate":   execSkillUpdate,
                "SkillSuggest":  execSkillSuggest,
                "SkillStats":    execSkillStats,
                "SkillEvaluate": execSkillEvaluate,
                "SkillLoad":     execSkillLoad,

                // Text tools
                "TextSearch":    execTextSearch,
                "TextReplace":   execTextReplace,
                "TextGrep":      execTextGrep,
                "TextTransform": execTextTransform,

                // Plugin tools
                "PluginCreate":  execPluginCreate,
                "PluginList":    execPluginList,
                "PluginLoad":    execPluginLoad,
                "PluginUnload":  execPluginUnload,
                "PluginReload":  execPluginReload,
                "PluginCall":    execPluginCall,
                "PluginCompile": execPluginCompile,
                "PluginDelete":  execPluginDelete,
                "PluginApis":    execPluginAPIs,
                "PluginDetail":  execPluginDetail,

                // Task management tools
                "TaskCheck":    execTaskCheck,
                "TaskTerminate": execTaskTerminate,
                "TaskList":     execTaskList,
                "TaskWait":     execTaskWait,
                "TaskRemove":   execTaskRemove,

                // Spawn tools
                "Spawn":         execSpawn,
                "SpawnCheck":   execSpawnCheck,
                "SpawnList":    execSpawnList,
                "SpawnCancel":  execSpawnCancel,
                "SpawnBatch":   execSpawnBatch,

                // Other tools
                "ConsolidateMemory": execConsolidateMemory,
                "SchemeEval":        execSchemeEval,

                // ── P3: RL 導出工具處理函數 ────────────────────────────
                "ExportSftData":  execExportSFTData,
                "ExportRlData":   execExportRLData,
                "TrajectoryStats": execTrajectoryStats,

                // ── P4: 憑證池 & Profile 管理工具 ────────────────────────
                "CredentialAdd":  execCredentialAdd,
                "CredentialList": execCredentialList,
                "ProfileCreate":  execProfileCreate,
                "ProfileSwitch":  execProfileSwitch,
                "ProfileList":    execProfileList,

                // ── 技能演化工具 ──────────────────────────────────────
                "SkillCleanupSuggest": execSkillCleanupSuggest,
                "SkillAutotag":         execSkillAutoTag,
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
                content = GetUnknownToolErrorMessage(toolName)
                status = TaskStatusFailed
        }

        if status == TaskStatusSuccess && (strings.HasPrefix(content, "Error:") || strings.HasPrefix(content, "error:")) {
                status = TaskStatusFailed
        }

        content = sanitizeContent(content)
        content = GetGlobalToolResultBudget().CheckAndPersistResult(toolName, content)
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
        if v, ok := ec.ArgsMap["OutputPath"].(string); ok && v != "" {
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
        if v, ok := ec.ArgsMap["OutputPath"].(string); ok && v != "" {
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
        if v, ok := ec.ArgsMap["OutputPath"].(string); ok && v != "" {
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
        if modelID, ok := ec.ArgsMap["ModelId"].(string); ok && modelID != "" {
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
                return "No profiles created. Use 'ProfileCreate' to create one.", TaskStatusSuccess
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
