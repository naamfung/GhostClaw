package main

import (
        "context"
        "encoding/json"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/toon-format/toon-go"
)

// ============================================================================
// 工具安全网 - 灵感来自 cc-mini 的先读后写检查、迭代上限等设计
// ============================================================================

var (
        // MaxAgentLoopIterations Agent Loop 最大迭代次数（每轮 = 一次 LLM 调用 + 工具执行）
        // 0 = 不限制。可通过配置文件 Tools.MaxAgentIterations 覆盖
        MaxAgentLoopIterations = 0

        // IterationWarningThreshold 迭代警告阈值
        // 接近上限时注入提醒消息（默认为上限的 80%）
        IterationWarningThreshold = 0
)

// ============================================================================
// 先读后写检查（Read-Before-Write Enforcement）
// ============================================================================

// readLevel 表示文件的讀取級別
type readLevel int

const (
        readLevelNone    readLevel = iota // 未讀取
        readLevelPartial                  // 部分讀取（ReadFileLine, ReadFileRange, TextGrep）
        readLevelFull                     // 完整讀取（ReadFileLines）
)

// LineRange 表示文件中已讀取的行範圍（1-based，兩端包含）
type LineRange struct {
        StartLine int
        EndLine   int
}

// overlaps 檢查兩個 LineRange 是否有交集
func (r LineRange) overlaps(other LineRange) bool {
        return r.StartLine <= other.EndLine && other.StartLine <= r.EndLine
}

// containsLine 檢查 LineRange 是否包含指定行號
func (r LineRange) containsLine(lineNum int) bool {
        return lineNum >= r.StartLine && lineNum <= r.EndLine
}

// mergeRanges 將新的 LineRange 合併到現有 ranges 列表中。
// 會自動合併所有 overlapping 或 adjacent 的範圍。
func mergeRanges(ranges []LineRange, newRange LineRange) []LineRange {
        // 將 newRange 加入，然後按 StartLine 排序
        all := append(ranges, newRange)
        if len(all) <= 1 {
                return all
        }

        // 按 StartLine 排序
        for i := 0; i < len(all)-1; i++ {
                for j := i + 1; j < len(all); j++ {
                        if all[i].StartLine > all[j].StartLine {
                                all[i], all[j] = all[j], all[i]
                        }
                }
        }

        // 合併 overlapping 或 adjacent 的範圍
        merged := []LineRange{all[0]}
        for i := 1; i < len(all); i++ {
                last := &merged[len(merged)-1]
                // 檢查是否 overlapping 或 adjacent（例如 [1,5] 同 [6,10] 合併為 [1,10]）
                if all[i].StartLine <= last.EndLine+1 {
                        if all[i].EndLine > last.EndLine {
                                last.EndLine = all[i].EndLine
                        }
                } else {
                        merged = append(merged, all[i])
                }
        }
        return merged
}

// isLineInRanges 檢查指定行號是否存在於 ranges 列表中
func isLineInRanges(ranges []LineRange, lineNum int) bool {
        for _, r := range ranges {
                if r.containsLine(lineNum) {
                        return true
                }
        }
        return false
}

// isRangeOverlapping 檢查目標範圍是否與 ranges 列表有交集
func isRangeOverlapping(ranges []LineRange, target LineRange) bool {
        for _, r := range ranges {
                if r.overlaps(target) {
                        return true
                }
        }
        return false
}

// readExpiryTime ReadFileLines 完整讀取記錄的有效期。
// SSH 持久會話等耗時操作可能令模型在讀取後超過 10 分鐘先寫入，
// 因此設為 60 分鐘以容納長操作場景。evict 使用相同閾值。
const readExpiryTime = 60 * time.Minute

// escalatePrefix 通用錯誤升級 sentinel prefix。
// 當 SafeExecuteTool 或其他錯誤處理返回以此前綴開頭的 EnrichedMessage 時，
// AgentLoop 主循環會提取消息內容並以用戶身份注入對話歷史。
// 格式：__ESCALATE__:<category>:<message>
// 目前支援的 category: write_without_read, repeated_tool_failure
const escalatePrefix = "__ESCALATE__:"

// readWriteTracker 追踪已讀取的文件及其讀取級別，強制先讀後寫
// 核心設計：
//   - 完整讀取（ReadFileLines）滿足所有寫入工具的先讀要求
//   - 部分讀取配合精確行範圍（readRanges）可允許對應範圍的單行/範圍寫入
//   - 防止模型只讀一行就用幻覺重寫整個文件
type readWriteTracker struct {
        mu               sync.RWMutex
        fullReadFiles    map[string]time.Time   // 完整讀取：文件路徑 -> 讀取時間
        partialReadFiles map[string]time.Time   // 部分讀取（無具體範圍）：文件路徑 -> 讀取時間（僅 TextGrep）
        readRanges       map[string][]LineRange // 精確行範圍追蹤：文件路徑 -> 已讀取的 LineRange 列表
        maxEntries       int                    // 最大緩存條目數
}

var globalReadWriteTracker = &readWriteTracker{
        fullReadFiles:    make(map[string]time.Time),
        partialReadFiles: make(map[string]time.Time),
        readRanges:       make(map[string][]LineRange),
        maxEntries:       200,
}

// MarkFileFullyRead 標記文件已被完整讀取（僅由 ReadFileLines 調用）
// 完整讀取是滿足任何寫入操作的最高級別要求
func (t *readWriteTracker) MarkFileFullyRead(filePath string) {
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        t.fullReadFiles[filePath] = time.Now()
        // 升級後同時從部分讀取和範圍記錄中移除（避免冗餘）
        delete(t.partialReadFiles, filePath)
        delete(t.readRanges, filePath)
        t.evictIfNeeded()

        // 模型正確讀取文件後，重置寫入違規計數
        globalErrorEscalator.ResetCategory(EscalateWriteWithoutRead)
}

// MarkFilePartialRead 標記文件已被部分讀取但無具體行範圍（僅由 TextGrep 調用）
// 此級別不滿足任何寫入操作的先讀要求，僅作內部追蹤用途。
// 如需允許寫入，請使用 MarkFileLineRead 或 MarkFileRangeRead 記錄具體行範圍。
func (t *readWriteTracker) MarkFilePartialRead(filePath string) {
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        // 如果已經是完整讀取，不降級
        if _, ok := t.fullReadFiles[filePath]; ok {
                return
        }
        t.partialReadFiles[filePath] = time.Now()
        t.evictIfNeeded()
}

// MarkFileLineRead 標記文件中的特定行已被讀取（由 ReadFileLine 調用）
// 記錄精確行號以便後續 WriteFileLine 檢查同一行的寫入權限
func (t *readWriteTracker) MarkFileLineRead(filePath string, lineNum int) {
        if lineNum < 1 {
                return // 無效行號，忽略
        }
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        // 如果已經是完整讀取，不降級
        if _, ok := t.fullReadFiles[filePath]; ok {
                return
        }
        // 合併到現有範圍
        newRange := LineRange{StartLine: lineNum, EndLine: lineNum}
        t.readRanges[filePath] = mergeRanges(t.readRanges[filePath], newRange)
        t.partialReadFiles[filePath] = time.Now()
        t.evictIfNeeded()
}

// MarkFileRangeRead 標記文件中的行範圍已被讀取（由 ReadFileRange 調用）
// 記錄精確範圍以便後續 WriteFileRange 檢查交集寫入權限
func (t *readWriteTracker) MarkFileRangeRead(filePath string, startLine, endLine int) {
        if startLine < 1 {
                return // 無效起始行，忽略
        }
        if endLine < startLine {
                endLine = startLine
        }
        t.mu.Lock()
        defer t.mu.Unlock()

        filePath = normalizeFilePath(filePath)
        // 如果已經是完整讀取，不降級
        if _, ok := t.fullReadFiles[filePath]; ok {
                return
        }
        newRange := LineRange{StartLine: startLine, EndLine: endLine}
        t.readRanges[filePath] = mergeRanges(t.readRanges[filePath], newRange)
        t.partialReadFiles[filePath] = time.Now()
        t.evictIfNeeded()
}

// evictIfNeeded 防止緩存無限增長，清理最舊的條目
func (t *readWriteTracker) evictIfNeeded() {
        total := len(t.fullReadFiles) + len(t.partialReadFiles)
        if total <= t.maxEntries {
                return
        }
        count := 0
        for key, ts := range t.partialReadFiles {
                if count >= 50 {
                        break
                }
                if time.Since(ts) > readExpiryTime {
                        delete(t.partialReadFiles, key)
                        delete(t.readRanges, key) // 同步清理範圍記錄
                        count++
                }
        }
        for key, ts := range t.fullReadFiles {
                if count >= 50 {
                        break
                }
                if time.Since(ts) > readExpiryTime {
                        delete(t.fullReadFiles, key)
                        count++
                }
        }
}

// GetFileReadLevel 獲取文件的讀取級別
func (t *readWriteTracker) GetFileReadLevel(filePath string) readLevel {
        t.mu.RLock()
        defer t.mu.RUnlock()

        filePath = normalizeFilePath(filePath)
        if ts, ok := t.fullReadFiles[filePath]; ok && time.Since(ts) < readExpiryTime {
                return readLevelFull
        }
        if ts, ok := t.partialReadFiles[filePath]; ok && time.Since(ts) < readExpiryTime {
                return readLevelPartial
        }
        return readLevelNone
}

// HasFileBeenRead 檢查文件是否已被讀取（兼容舊接口，任何級別均返回 true）
func (t *readWriteTracker) HasFileBeenRead(filePath string) bool {
        return t.GetFileReadLevel(filePath) != readLevelNone
}

// GetFileReadRanges 獲取文件已讀取的行範圍列表。
// 返回 nil 表示文件未被部分讀取或已是完整讀取狀態。
func (t *readWriteTracker) GetFileReadRanges(filePath string) []LineRange {
        t.mu.RLock()
        defer t.mu.RUnlock()

        filePath = normalizeFilePath(filePath)
        // 完整讀取無需返回範圍
        if _, ok := t.fullReadFiles[filePath]; ok {
                return nil
        }
        ranges, ok := t.readRanges[filePath]
        if !ok || len(ranges) == 0 {
                return nil
        }
        // 返回副本防止外部修改
        result := make([]LineRange, len(ranges))
        copy(result, ranges)
        return result
}

// normalizeFilePath 規範化文件路徑
func normalizeFilePath(path string) string {
        // 使用 filepath.Abs + filepath.Clean 進行規範化，防止路徑遍歷繞過安全檢查
        abs, err := filepath.Abs(path)
        if err != nil {
                return path
        }
        return filepath.Clean(abs)
}

// CheckWritePermission 檢查是否允許寫入文件。
// 返回 nil 表示允許，返回 error 表示需要先讀取。
// 新建文件（目標路徑不存在）無需先讀，直接允許寫入。
//
// 安全策略（按工具類型細分）：
//   - WriteFileLine: 單行覆寫需該行已被 ReadFileLine/ReadFileRange 讀取；append(-1)/insert(< -1) 需 fullRead
//   - WriteFileRange: 範圍覆寫需與已讀範圍有交集；insert(StartLine<0) 需插入點在已讀範圍內
//   - WriteFileLines/AppendToFile/TextReplace/TextTransform: 全局操作，統一要求完整讀取
//   - 新建文件（LineNum=0）無需先讀檢查，由調用方處理
func CheckWritePermission(filePath string, toolName string, argsMap map[string]interface{}) error {
        // 歸一化路徑，確保 os.Stat 和 GetFileReadLevel 使用相同的路徑表示
        filePath = normalizeFilePath(filePath)
        // 如果文件不存在，視為新建文件，無需先讀
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
                return nil
        }

        readLvl := globalReadWriteTracker.GetFileReadLevel(filePath)

        // 完整讀取：允許所有操作
        if readLvl == readLevelFull {
                return nil
        }

        // 未讀取：直接拒絕
        if readLvl == readLevelNone {
                return fmt.Errorf("安全檢查失敗：你必須先使用 ReadFileLines 完整讀取 %s 才能進行寫入/編輯操作。這是為了確保你理解現有文件內容後再修改。", filePath)
        }

        // 部分讀取（readLevelPartial）：按工具類型細分檢查
        readRanges := globalReadWriteTracker.GetFileReadRanges(filePath)

        switch toolName {
        case "WriteFileLine":
                return checkWriteFileLinePermission(filePath, argsMap, readRanges)
        case "WriteFileRange":
                return checkWriteFileRangePermission(filePath, argsMap, readRanges)
        case "WriteFileLines", "AppendToFile", "TextReplace", "TextTransform":
                // 全局寫入操作：即使有部分範圍讀取，仍需完整讀取
                return fmt.Errorf(
                        "安全檢查失敗：%s 是全局寫入操作，必須先使用 ReadFileLines 完整讀取 %s 才能使用。\n"+
                                "ReadFileLine / ReadFileRange 僅讀取部分內容，不足以支撐全局寫入。",
                        toolName, filePath)
        default:
                // 未知寫入工具：保守處理，要求完整讀取
                return fmt.Errorf(
                        "安全檢查失敗：你必須先使用 ReadFileLines 完整讀取 %s 才能進行寫入操作。",
                        filePath)
        }
}

// checkWriteFileLinePermission 檢查 WriteFileLine 的寫入權限。
// 處理邏輯：
//   - LineNum >= 1 (覆寫): 檢查該行是否在已讀範圍內
//   - LineNum = 0 (新建文件): 由 CheckWritePermission 上層處理（文件不存在時直接放行）
//   - LineNum = -1 (追加): 需完整讀取（無法確知文件末尾行號）
//   - LineNum < -1 (插入): 檢查插入點 |LineNum| 是否在已讀範圍內
func checkWriteFileLinePermission(filePath string, argsMap map[string]interface{}, readRanges []LineRange) error {
        lineNumFloat, ok := argsMap["LineNum"].(float64)
        if !ok {
                return fmt.Errorf("安全檢查失敗：WriteFileLine 缺少 LineNum 參數，無法進行精確權限檢查。請先使用 ReadFileLines 完整讀取 %s。", filePath)
        }

        lineNum := int(lineNumFloat)

        // LineNum = 0: 新建文件（理論上不應到此處，因為文件存在時不會是 LineNum=0）
        // 但為安全起見，這裡放行
        if lineNum == 0 {
                return nil
        }

        // LineNum = -1: 追加到末尾 → 需完整讀取
        if lineNum == -1 {
                return fmt.Errorf(
                        "安全檢查失敗：WriteFileLine 追加模式 (LineNum=-1) 需先使用 ReadFileLines 完整讀取 %s。\n"+
                                "追加操作影響文件結尾，ReadFileLine/ReadFileRange 的部分範圍不足以確認文件結構。",
                        filePath)
        }

        // LineNum < -1: 插入到 |LineNum| 之前 → 檢查插入點是否在已讀範圍內
        if lineNum < -1 {
                targetLine := -lineNum
                if readRanges == nil || !isLineInRanges(readRanges, targetLine) {
                        rangesDesc := describeRanges(readRanges)
                        return fmt.Errorf(
                                "安全檢查失敗：你嘗試在第 %d 行之前插入內容，但你尚未讀取該區域。\n"+
                                        "已讀取範圍：%s\n"+
                                        "請先用 ReadFileLine 或 ReadFileRange 讀取第 %d 行附近的內容。",
                                targetLine, rangesDesc, targetLine)
                }
                return nil
        }

        // LineNum >= 1: 覆寫指定行 → 檢查該行是否在已讀範圍內
        if readRanges == nil || !isLineInRanges(readRanges, lineNum) {
                rangesDesc := describeRanges(readRanges)
                return fmt.Errorf(
                        "安全檢查失敗：你嘗試寫入第 %d 行，但你尚未讀取該行。\n"+
                                "已讀取範圍：%s\n"+
                                "請先用 ReadFileLine(LineNum=%d) 讀取該行，或使用 ReadFileRange 讀取包含該行的範圍。",
                        lineNum, rangesDesc, lineNum)
        }
        return nil
}

// checkWriteFileRangePermission 檢查 WriteFileRange 的寫入權限。
// 處理邏輯：
//   - StartLine >= 1 (覆寫): 檢查 [StartLine, EndLine] 是否與已讀範圍有交集
//   - StartLine < 0 (插入): 檢查插入點 |StartLine| 是否在已讀範圍內
func checkWriteFileRangePermission(filePath string, argsMap map[string]interface{}, readRanges []LineRange) error {
        startLineFloat, ok := argsMap["StartLine"].(float64)
        if !ok {
                return fmt.Errorf("安全檢查失敗：WriteFileRange 缺少 StartLine 參數，無法進行精確權限檢查。請先使用 ReadFileLines 完整讀取 %s。", filePath)
        }

        startLine := int(startLineFloat)

        // StartLine = 0 不合法（handler 會拒絕），但此處防禦性處理
        if startLine == 0 {
                return nil
        }

        // StartLine < 0: 插入模式 → 檢查插入點 |StartLine| 是否在已讀範圍內
        if startLine < 0 {
                targetLine := -startLine
                if readRanges == nil || !isLineInRanges(readRanges, targetLine) {
                        rangesDesc := describeRanges(readRanges)
                        return fmt.Errorf(
                                "安全檢查失敗：你嘗試在第 %d 行之前插入內容，但你尚未讀取該區域。\n"+
                                        "已讀取範圍：%s\n"+
                                        "請先用 ReadFileLine 或 ReadFileRange 讀取第 %d 行附近的內容。",
                                targetLine, rangesDesc, targetLine)
                }
                return nil
        }

        // StartLine >= 1: 覆寫模式 → 檢查是否與已讀範圍有交集
        endLine := startLine
        if endLineFloat, ok := argsMap["EndLine"].(float64); ok && endLineFloat >= float64(startLine) {
                endLine = int(endLineFloat)
        }

        if readRanges == nil || !isRangeOverlapping(readRanges, LineRange{StartLine: startLine, EndLine: endLine}) {
                rangesDesc := describeRanges(readRanges)
                return fmt.Errorf(
                        "安全檢查失敗：你嘗試寫入第 %d-%d 行，但該範圍與你已讀取的範圍無交集。\n"+
                                "已讀取範圍：%s\n"+
                                "請先用 ReadFileRange 讀取包含目標範圍的內容。",
                        startLine, endLine, rangesDesc)
        }
        return nil
}

// describeRanges 將已讀範圍列表格式化為人類可讀的字串
func describeRanges(ranges []LineRange) string {
        if len(ranges) == 0 {
                return "（無）"
        }
        parts := make([]string, len(ranges))
        for i, r := range ranges {
                if r.StartLine == r.EndLine {
                        parts[i] = fmt.Sprintf("第 %d 行", r.StartLine)
                } else {
                        parts[i] = fmt.Sprintf("第 %d-%d 行", r.StartLine, r.EndLine)
                }
        }
        return strings.Join(parts, "、")
}

// ============================================================================
// 寫入前未讀取違規追蹤（Write-Without-Read Violation Tracking）
// ============================================================================

// EscalationCategory 錯誤升級類別
type EscalationCategory string

const (
	// EscalateWriteWithoutRead 寫入前未讀取違規
	EscalateWriteWithoutRead EscalationCategory = "write_without_read"
	// EscalateRepeatedFailure 重複工具調用失敗（同一工具+參數連續失敗）
	EscalateRepeatedFailure EscalationCategory = "repeated_tool_failure"
)

// escalationTracker 單個錯誤類別的追蹤器
type escalationTracker struct {
	category  EscalationCategory
	errorKey  string   // 錯誤鍵（如文件路徑、工具名+參數哈希）
	count     int
	messages  []string // 保存每次錯誤的完整信息
}

// RepeatedErrorEscalator 通用重複錯誤升級器。
// 為不同類別和鍵的錯誤獨立追蹤連續失敗次數，
// 達到閾值後觸發升級：以用戶身份將錯誤摘要注入消息歷史。
type RepeatedErrorEscalator struct {
	mu       sync.Mutex
	trackers map[string]*escalationTracker // key: "category:errorKey"
}

var globalErrorEscalator = &RepeatedErrorEscalator{
	trackers: make(map[string]*escalationTracker),
}

const defaultEscalationThresholdValue = 3

// getEscalationThreshold returns the configurable escalation threshold
// (1-5, default 3). Uses globalEscalationThreshold if set, otherwise falls
// back to the hardcoded default.
func getEscalationThreshold() int {
	if globalEscalationThreshold > 0 {
		return globalEscalationThreshold
	}
	return defaultEscalationThresholdValue
}

// RecordEscalation 記錄一次錯誤並判斷是否需要升級。
// category: 錯誤類別
// errorKey: 錯誤鍵（同類別+同鍵的錯誤累計計數）
// errMsg:   錯誤消息
// 返回 shouldStop=true 表示已達到閾值，需強制升級
func (e *RepeatedErrorEscalator) RecordEscalation(
	category EscalationCategory, errorKey, errMsg string,
) (shouldStop bool, userMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	trackKey := string(category) + ":" + errorKey
	t, ok := e.trackers[trackKey]
	if !ok {
		t = &escalationTracker{
			category: category,
			errorKey: errorKey,
		}
		e.trackers[trackKey] = t
	}

	t.count++
	t.messages = append(t.messages, errMsg)

	if t.count >= getEscalationThreshold() {
		userMsg = e.buildEscalationMessage(t)
		shouldStop = true
		// 重置此追蹤器，為下一輪做準備
		delete(e.trackers, trackKey)
	}

	return
}

// buildEscalationMessage 根據類別構建升級消息
func (e *RepeatedErrorEscalator) buildEscalationMessage(t *escalationTracker) string {
	var sb strings.Builder

	switch t.category {
	case EscalateWriteWithoutRead:
		sb.WriteString("[SYSTEM_ESCALATE] 以下是你連續多次無視安全檢查錯誤的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("你必須使用 ReadFileLines 完整讀取目標文件後才能進行寫入操作。請立即讀取相關文件。")

	case EscalateRepeatedFailure:
		sb.WriteString("[SYSTEM_ESCALATE] 以下是你連續多次重複相同失敗操作的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("請停止重複此操作。分析錯誤原因後嘗試不同的方法，或向用戶說明遇到的問題並請求指導。")

	default:
		sb.WriteString("[SYSTEM_ESCALATE] 以下是你連續多次錯誤的記錄：\n\n")
		for i, msg := range t.messages {
			sb.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, msg))
		}
		sb.WriteString("請停止重複操作，分析原因並採取不同的策略。")
	}

	return sb.String()
}

// ResetCategory 重置指定類別的所有追蹤器
func (e *RepeatedErrorEscalator) ResetCategory(category EscalationCategory) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, t := range e.trackers {
		if t.category == category {
			delete(e.trackers, key)
		}
	}
}

// ResetKey 重置指定類別+鍵的追蹤器
func (e *RepeatedErrorEscalator) ResetKey(category EscalationCategory, errorKey string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	trackKey := string(category) + ":" + errorKey
	delete(e.trackers, trackKey)
}
// ============================================================================
// 未知工具引导（Unknown Tool Guidance）
// ============================================================================

// allRegisteredToolNames returns a snapshot of all tool names in toolRegistryMap.
// Used for fuzzy matching suggestions when the model calls an unknown tool.
func allRegisteredToolNames() []string {
        names := make([]string, 0, len(toolRegistryMap))
        for name := range toolRegistryMap {
                names = append(names, name)
        }
        return names
}

// FindSimilarTool 找到与输入最相似的工具名称
// 使用简单的编辑距离算法
func FindSimilarTool(input string) string {
        input = strings.ToLower(strings.TrimSpace(input))

        bestMatch := ""
        bestDistance := len(input) + 5 // 初始阈值

        for _, name := range allRegisteredToolNames() {
                distance := levenshteinDistance(input, name)
                // 只考虑距离足够小的匹配
                if distance < bestDistance && distance <= max(len(input), len(name))/2+1 {
                        bestDistance = distance
                        bestMatch = name
                }
        }

        return bestMatch
}

// levenshteinDistance 计算两个字符串的 Levenshtein 编辑距离
func levenshteinDistance(s1, s2 string) int {
        if len(s1) == 0 {
                return len(s2)
        }
        if len(s2) == 0 {
                return len(s1)
        }

        // 优化：如果长度差异太大，直接返回
        diff := absInt(len(s1) - len(s2))
        if diff > max(len(s1), len(s2))/2 {
                return diff
        }

        // 创建距离矩阵
        prev := make([]int, len(s2)+1)
        curr := make([]int, len(s2)+1)

        for j := range prev {
                prev[j] = j
        }

        for i := 1; i <= len(s1); i++ {
                curr[0] = i
                for j := 1; j <= len(s2); j++ {
                        cost := 1
                        if s1[i-1] == s2[j-1] {
                                cost = 0
                        }
                        curr[j] = minInt(
                                curr[j-1]+1,      // 插入
                                prev[j]+1,       // 删除
                                prev[j-1]+cost,  // 替换
                        )
                }
                prev, curr = curr, prev
        }

        return prev[len(s2)]
}

func absInt(x int) int {
        if x < 0 {
                return -x
        }
        return x
}

func minInt(a, b, c int) int {
        if a < b {
                if a < c {
                        return a
                }
                return c
        }
        if b < c {
                return b
        }
        return c
}

// snakeToPascalCase 將 snake_case 工具名轉換為 PascalCase
// 例如：SSHConnect, BrowserClick
func snakeToPascalCase(s string) string {
        parts := strings.Split(s, "_")
        for i, part := range parts {
                if len(part) > 0 {
                        parts[i] = strings.Title(part)
                }
        }
        return strings.Join(parts, "")
}

// normalizeArgsMapKeys 將含底線嘅 snake_case key 轉為 PascalCase 版本並合併入 argsMap
// 處理 LLM 用 snake_case 傳參但 handler 用 PascalCase 讀取嘅兼容問題
// 只處理含 "_" 嘅 key，保留原始 key 並新增 PascalCase 版本，雙向兼容
func normalizeArgsMapKeys(argsMap map[string]interface{}) {
        if argsMap == nil {
                return
        }
        for key, value := range argsMap {
                if strings.Contains(key, "_") {
                        pascalKey := snakeToPascalCase(key)
                        if pascalKey != key {
                                if _, exists := argsMap[pascalKey]; !exists {
                                        argsMap[pascalKey] = value
                                }
                        }
                }
        }
}

// GetUnknownToolErrorMessage 生成未知工具的错误消息
// 自動檢測 snake_case 命名並提供明確的 PascalCase 修正指引
func GetUnknownToolErrorMessage(toolName string) string {
        // 檢測 snake_case：如果工具名含底線，先試自動轉 PascalCase
        if strings.Contains(toolName, "_") {
                pascalName := snakeToPascalCase(toolName)
                for _, name := range allRegisteredToolNames() {
                        if name == pascalName {
                                return fmt.Sprintf("工具名不能使用底線格式 (snake_case)！請使用駝峰式 (PascalCase)：'%s'", pascalName)
                        }
                }
        }

        suggestion := FindSimilarTool(toolName)
        if suggestion != "" {
                return fmt.Sprintf("工具 '%s' 不存在。你是否想使用 '%s'？\n可用的工具列表请参考系统提示中的工具部分。", toolName, suggestion)
        }
        return fmt.Sprintf("工具 '%s' 不存在。请检查工具名称是否正确。\n可用的工具列表请参考系统提示中的工具部分。", toolName)
}

// ============================================================================
// 迭代上限与智能中断
// ============================================================================

// LoopWarningInjector 迭代警告注入器
type LoopWarningInjector struct {
        warningInjected bool
        lastWarnTime   time.Time
}

var globalLoopWarningInjector = &LoopWarningInjector{}

// ShouldInjectWarning 是否应该注入迭代警告
func (l *LoopWarningInjector) ShouldInjectWarning(iteration int) bool {
        // 未设置上限则不警告
        if MaxAgentLoopIterations <= 0 || iteration < IterationWarningThreshold {
                return false
        }
        // 每次警告间隔至少 3 次迭代
        if l.warningInjected && int64(iteration)-lastWarnIteration < 3 {
                return false
        }
        return true
}

var lastWarnIteration int64 = 0

// GetIterationWarningMessage 获取迭代警告消息
func GetIterationWarningMessage(iteration int) string {
        lastWarnIteration = int64(iteration)
        remaining := MaxAgentLoopIterations - iteration
        if remaining <= 5 {
                return fmt.Sprintf(`[系统警告] Agent Loop 已迭代 %d 轮（上限 %d 轮，剩余 %d 轮）。
请尽快总结当前进展并完成最后的步骤。如果无法完成，请向用户报告当前进度和未完成的事项。`, iteration, MaxAgentLoopIterations, remaining)
        }
        return fmt.Sprintf(`[系统提醒] Agent Loop 已迭代 %d 轮（上限 %d 轮）。
建议你合理安排剩余步骤，避免不必要的重复操作。`, iteration, MaxAgentLoopIterations)
}

// ShouldForceStop 是否应该强制停止 Agent Loop
func ShouldForceStop(iteration int) bool {
        return MaxAgentLoopIterations > 0 && iteration >= MaxAgentLoopIterations
}

// ============================================================================
// 只读工具并行执行标记
// ============================================================================

// ReadOnlyTools 只读工具列表，这些工具可以并行执行
var ReadOnlyTools = map[string]bool{
        "ReadFileLine": true,
        "ReadFileLines":  true,
        "TextSearch":     true,
        "TextGrep":       true,
        "MemoryRecall":   true,
        "MemoryList":     true,
        "PlanRead":       true,
        "PluginList":     true,
        "SkillList":      true,
        "SkillGet":       true,
        "CronList":       true,
        "CronStatus":     true,
        "SpawnList":      true,
        "SSHList":        true,
        "ProfileCheck":   true,
}

// IsReadOnlyTool 检查工具是否为只读工具
func IsReadOnlyTool(toolName string) bool {
        return ReadOnlyTools[toolName]
}

// ============================================================================
// 工具执行包装器 - 集成安全检查
// ============================================================================

// emitToolCallTags 向前端发送完整的工具调用 agentic tags（用于早期返回路径）
// 确保所有工具执行路径（包括安全检查拒绝、Plan Mode 拦截等）都能在网页端显示为工具块
func emitToolCallTags(ch Channel, toolName string, argsMap map[string]interface{}, content string, status TaskStatus) {
        // 檢查任務是否已被取消：若已取消則不應再向前端推送工具結果
        // 避免用戶在 /stop 之後仍然看到 SSH 錯誤等後續輸出
        session := GetGlobalSession()
        if session.IsTaskCancelled() {
                return
        }
        argsJSON, _ := json.Marshal(argsMap)
        sendToolCallStart(ch, toolName, string(argsJSON))
        if content != "" {
                ch.WriteChunk(StreamChunk{Content: content + "\n"})
        }
        sendToolCallStatus(ch, status)
        sendToolCallEnd(ch)
}

// SafeExecuteTool 安全工具执行包装器
// 在原有 executeTool 基础上添加安全检查：
// 1. Plan Mode 权限检查
// 2. 先读后写检查
// 3. 未知工具引导
func SafeExecuteTool(ctx context.Context, toolID, toolName string, argsMap map[string]interface{}, ch Channel, role *Role) EnrichedMessage {
        // 參數名歸一化：將 snake_case key 轉為 PascalCase 兼容版本
        normalizeArgsMapKeys(argsMap)

        // Tasks Mode 权限检查（分階段工具控制）
        if globalTasksMode.IsActive() && !IsToolAllowedInTasksMode(toolName) {
                currentPhase := globalTasksMode.Phase()
                var content string
                switch toolName {
                case "EnterPlanMode":
                        content = fmt.Sprintf("你已經在 Tasks Mode 中（%s）。使用 Tasks(PlanPhase=\"design\") 進入設計階段，或 Tasks(PlanPhase=\"execute\") 退出。", currentPhase)
                case "SmartShell":
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用 shell。此階段僅允許只讀工具（TextSearch、ReadFileLines 等）。", currentPhase)
                case "WriteFileLine", "WriteFileLines", "AppendToFile", "WriteFileRange", "TextReplace":
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用寫入工具 '%s'。先完成探索和設計，最終計劃確認後再執行。", currentPhase, toolName)
                default:
                        content = fmt.Sprintf("Tasks Mode %s 中不允許使用工具 '%s'。當前階段可用工具有限。", currentPhase, toolName)
                }
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusFailed},
                }
        }

        // Tasks Mode 专用工具处理
        switch toolName {
        case "EnterPlanMode":
                planContent, _ := argsMap["PlanContent"].(string)
                content, ok := handleTasks(map[string]interface{}{
                        "PlanPhase":   "design",
                        "PlanContent": planContent,
                })
                if !ok {
                        content = "Error: " + content
                }
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                }
        case "ExitPlanMode":
                if globalTasksMode.IsActive() {
                        content, _ := handleTasks(map[string]interface{}{"PlanPhase": "execute"})
                        emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                        return EnrichedMessage{
                                Content: content,
                                Meta:    MessageMeta{Status: TaskStatusSuccess},
                        }
                }
                content := "Tasks Mode 當前未激活。"
                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)
                return EnrichedMessage{
                        Content: content,
                        Meta:    MessageMeta{Status: TaskStatusFailed},
                }
        }

		// 未知工具检查 - 先查 toolRegistryMap，再查 toolHandlerRegistry（含 Menu 等特殊工具），最后查 MCP
		_, isKnown := toolRegistryMap[toolName]
		if !isKnown {
			// 检查是否在 toolHandlerRegistry 中（例如 Menu, Tasks 等特殊工具）
			if _, handlerExists := toolHandlerRegistry[toolName]; handlerExists {
				isKnown = true
			}
		}
		if !isKnown {
			// 检查是否是 MCP 动态工具
			isMCP := false
			if globalMCPClientManager != nil {
				mcpTools := globalMCPClientManager.GetAllTools()
				for _, t := range mcpTools {
					if t["name"] == toolName {
						isMCP = true
						break
					}
				}
			}
			if !isMCP {
				log.Printf("[ToolSafety] 未知工具调用: %s", toolName)
				content := GetUnknownToolErrorMessage(toolName)

				// 追蹤重複未知工具調用，達到閾值後觸發 escalation
				shouldStop, userMsg := globalErrorEscalator.RecordEscalation(
					EscalateRepeatedFailure, toolName, content,
				)

				emitToolCallTags(ch, toolName, argsMap, content, TaskStatusFailed)

				finalContent := content
				if shouldStop {
					finalContent = escalatePrefix + userMsg
				}

				return EnrichedMessage{
					Content: finalContent,
					Meta:    MessageMeta{Status: TaskStatusFailed},
				}
			}
		}

        // 先读后写检查 - 对写入类工具
        if isWriteTool(toolName) {
                filePath := extractFilePathFromArgs(argsMap)
                if filePath != "" {
                        // 自動讀取：如果模型尚未讀取文件，自動按其寫入範圍讀取並返回內容
                        if content, didAutoRead := autoReadForWrite(filePath, toolName, argsMap); didAutoRead {
                                log.Printf("[ToolSafety] 先讀後寫自動讀取: tool=%s file=%s", toolName, filePath)
                                emitToolCallTags(ch, toolName, argsMap, content, TaskStatusSuccess)
                                return EnrichedMessage{
                                        Content: content,
                                        Meta:    MessageMeta{Status: TaskStatusSuccess},
                                }
                        }

                        if err := CheckWritePermission(filePath, toolName, argsMap); err != nil {
                                log.Printf("[ToolSafety] 先读后写检查失败: tool=%s file=%s", toolName, filePath)

                                errStr := err.Error()
                                shouldStop, userMsg := globalErrorEscalator.RecordEscalation(
                                        EscalateWriteWithoutRead, filePath, errStr,
                                )
                                if shouldStop {
                                        // 連續 3 次違規：前端只顯示一般錯誤，內部返回 force-stop 標記
                                        // 主循環檢測標記後會以用戶身份注入消息（僅模型可見）
                                        emitToolCallTags(ch, toolName, argsMap, errStr, TaskStatusFailed)
                                        return EnrichedMessage{
                                                Content: escalatePrefix + userMsg,
                                                Meta:    MessageMeta{Status: TaskStatusFailed},
                                        }
                                }

                                emitToolCallTags(ch, toolName, argsMap, errStr, TaskStatusFailed)
                                return EnrichedMessage{
                                        Content: errStr,
                                        Meta:    MessageMeta{Status: TaskStatusFailed},
                                }
                        }
                }
        }

        // 调用原始工具执行（executeTool 内部会自行发送 agentic tags）
        return executeTool(ctx, toolID, toolName, argsMap, ch, role)
}

// ============================================================================
// 先讀後寫自動讀取 — 當模型未先讀取文件就調用寫入工具時，
// 系統自動按其寫入範圍讀取文件並返回內容，詢問模型是否繼續寫入。
// ============================================================================

// autoReadForWrite 在模型未先讀取文件時，自動按其寫入範圍讀取文件並返回內容。
// 返回 (formattedContent, didAutoRead)。didAutoRead=false 表示已滿足讀取要求。
func autoReadForWrite(filePath string, toolName string, argsMap map[string]interface{}) (string, bool) {
        absPath := normalizeFilePath(filePath)

        // 新建文件：無需自動讀取（CheckWritePermission 對新文件直接放行）
        if _, err := os.Stat(absPath); os.IsNotExist(err) {
                return "", false
        }

        readLvl := globalReadWriteTracker.GetFileReadLevel(absPath)

        // 完整讀取：已滿足所有寫入工具的要求
        if readLvl == readLevelFull {
                return "", false
        }

        // 部分讀取：檢查是否已覆蓋寫入目標
        if readLvl == readLevelPartial && writeTargetCovered(absPath, toolName, argsMap) {
                return "", false
        }

        // 需要自動讀取
        lines, err := ReadFileLines(absPath)
        if err != nil {
                log.Printf("[ToolSafety] autoReadForWrite ReadFileLines error for %s: %v", absPath, err)
                return "", false
        }

        content := formatAutoReadResult(absPath, lines, toolName, argsMap)

        // 標記為完整讀取，確保下次調用寫入工具時通過權限檢查
        globalReadWriteTracker.MarkFileFullyRead(absPath)
        globalErrorEscalator.ResetCategory(EscalateWriteWithoutRead)

        log.Printf("[ToolSafety] 先讀後寫自動讀取完成: tool=%s file=%s lines=%d", toolName, absPath, len(lines))

        return content, true
}

// writeTargetCovered 檢查部分讀取範圍是否已覆蓋當前寫入工具的目標位置。
// 對於全局寫入工具（WriteFileLines, TextReplace 等），即使有部分讀取也需要完整讀取。
func writeTargetCovered(absPath string, toolName string, argsMap map[string]interface{}) bool {
        readRanges := globalReadWriteTracker.GetFileReadRanges(absPath)
        if readRanges == nil {
                return false
        }

        switch toolName {
        case "WriteFileLine":
                if lineNumFloat, ok := argsMap["LineNum"].(float64); ok {
                        lineNum := int(lineNumFloat)
                        switch {
                        case lineNum >= 1:
                                return isLineInRanges(readRanges, lineNum)
                        case lineNum < -1:
                                return isLineInRanges(readRanges, -lineNum)
                        case lineNum == -1:
                                return false // append 需要讀取文件尾部
                        }
                }
                return false

        case "WriteFileRange":
                if startFloat, ok := argsMap["StartLine"].(float64); ok {
                        start := int(startFloat)
                        if start >= 1 {
                                end := start
                                if endFloat, ok := argsMap["EndLine"].(float64); ok && endFloat >= float64(start) {
                                        end = int(endFloat)
                                }
                                return isRangeOverlapping(readRanges, LineRange{StartLine: start, EndLine: end})
                        } else if start < 0 {
                                return isLineInRanges(readRanges, -start)
                        }
                }
                return false

        default:
                // 全局寫入工具（WriteFileLines, AppendToFile, TextReplace, TextTransform）
                // 即使有部分讀取也需要完整讀取
                return false
        }
}

// formatAutoReadResult 將自動讀取的文件內容格式化為 TOON 格式返回給模型。
// 使用與 ReadFileLines verbose 模式一致的結構，確保模型以相同方式解析文件內容。
func formatAutoReadResult(filePath string, lines []string, toolName string, argsMap map[string]interface{}) string {
        totalLines := len(lines)
        dispStart, dispEnd := computeAutoReadWindow(toolName, argsMap, totalLines)

        // 構建與 ReadFileLines verbose 模式一致的行內容結構
        shownLines := make([]map[string]interface{}, 0, dispEnd-dispStart+1)
        for i := dispStart - 1; i < dispEnd && i < totalLines; i++ {
                shownLines = append(shownLines, map[string]interface{}{
                        "Line":    i + 1,
                        "Content": lines[i],
                })
        }

        result := map[string]interface{}{
                "AutoRead":    true,
                "Message":     "你尚未讀取此文件就調用了寫入工具。已自動為你讀取寫入範圍附近的內容。請確認是否繼續寫入？如果內容與你預期一致，請重新調用寫入工具以繼續操作。",
                "Tool":        toolName,
                "Filename":    filePath,
                "TotalLines":  totalLines,
                "ShownStart":  dispStart,
                "ShownEnd":    dispEnd,
                "Truncated":   totalLines > dispEnd,
                "Lines":       shownLines,
        }

        resultTOON, err := toon.Marshal(result)
        if err != nil {
                return fmt.Sprintf("Error: %v", err)
        }
        return string(resultTOON)
}

// computeAutoReadWindow 根據寫入工具類型和參數決定自動讀取的顯示窗口。
func computeAutoReadWindow(toolName string, argsMap map[string]interface{}, totalLines int) (int, int) {
        const defaultWindow = 15 // 單行寫入的上下文行數
        const rangePadding = 5   // 範圍寫入的附加上下文行數
        const fullLimit = 2000   // 全文顯示的行數上限
        const tailLines = 20     // 追加模式的尾部行數

        switch toolName {
        case "WriteFileLine":
                if lineNumFloat, ok := argsMap["LineNum"].(float64); ok {
                        lineNum := int(lineNumFloat)
                        switch {
                        case lineNum >= 1:
                                return max(1, lineNum-defaultWindow), min(totalLines, lineNum+defaultWindow)
                        case lineNum == -1:
                                return max(1, totalLines-tailLines+1), totalLines
                        case lineNum < -1:
                                insPoint := -lineNum
                                return max(1, insPoint-defaultWindow), min(totalLines, insPoint+defaultWindow)
                        }
                }
                return 1, min(totalLines, fullLimit)

        case "WriteFileRange":
                if startFloat, ok := argsMap["StartLine"].(float64); ok {
                        start := int(startFloat)
                        if start >= 1 {
                                end := start
                                if endFloat, ok := argsMap["EndLine"].(float64); ok && endFloat >= float64(start) {
                                        end = int(endFloat)
                                }
                                return max(1, start-rangePadding), min(totalLines, end+rangePadding)
                        } else {
                                insPoint := -start
                                return max(1, insPoint-10), min(totalLines, insPoint+10)
                        }
                }
                return 1, min(totalLines, fullLimit)

        case "AppendToFile":
                return max(1, totalLines-tailLines+1), totalLines

        default:
                // WriteFileLines, TextReplace, TextTransform: 顯示全文（有上限）
                return 1, min(totalLines, fullLimit)
        }
}

// isWriteTool 检查工具是否为写入类工具
func isWriteTool(toolName string) bool {
        writeTools := map[string]bool{
                "WriteFileLine": true,
                "WriteFileLines": true,
                "AppendToFile":  true,
                "WriteFileRange": true,
                "TextReplace":    true,
                "TextTransform":  true,
                "MemorySave":     true,
                "MemoryForget":   true,
        }
        return writeTools[toolName]
}

// extractFilePathFromArgs 从工具参数中提取文件路径
func extractFilePathFromArgs(args map[string]interface{}) string {
        // 尝试常见的文件路径参数名
        for _, key := range []string{"FilePath", "filePath", "path", "filename", "file"} {
                if val, ok := args[key]; ok {
                        if str, ok := val.(string); ok && str != "" {
                                return str
                        }
                }
        }
        return ""
}

// init 初始化：工具安全网启动日志
// 讀寫追蹤已集成到以下工具中：
//   - executeTool.go: execReadFileLines -> MarkFileFullyRead
//   - executeTool.go: execReadFileLine -> MarkFileLineRead（精確行號追蹤）
//   - executeTool.go: execReadFileRange -> MarkFileRangeRead（精確範圍追蹤）
//   - TextReplace_tools.go: handleTextSearch (TextGrep) -> MarkFilePartialRead（無具體範圍）
func init() {
        log.Printf("[ToolSafety] 工具安全网已初始化: MaxIterations=%d, ReadOnlyTools=%d",
                MaxAgentLoopIterations, len(ReadOnlyTools))
}
