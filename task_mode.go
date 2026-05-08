package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Tasks Mode — 結構化任務分解（v2：Tasks 工具取代 EnterPlanMode/ExitPlanMode）
// ============================================================================
//
// 層級結構：
//   Tasks
//   ├─ Plan（explore → design → execute）
//   ├─ Task 1 → Todos [...]
//   ├─ Task 2 → Todos [...]
//   └─ Task 3 → Todos [...]
//
// Plan 階段保留現有 Explore + Design 兩階段（唔縮水）
// Task 之間無依賴，LLM 自行決定執行順序
// 每個 Task 用現有 Todos 工具管理子任務
// ============================================================================

// TaskItem 單個任務（Tasks 容器下嘅 task）
type TaskItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // Pending / InProgress / Completed / Waiting
}

// TasksMode Tasks 模式管理器
type TasksMode struct {
	PlanPhase      string `json:"PlanPhase"`   // inactive / explore / design / execute
	PlanContent    string `json:"PlanContent"` // 計劃文件內容

	PlanFilePath string
	StartTime    time.Time
	PhaseStart   time.Time
	TaskDesc     string

	DowngradeCount int
	TimedOut       bool
	stopTimeout    chan struct{}

	tasks []TaskItem // 當前 tasks 列表
	mu    sync.RWMutex
}

// Plan Phase 常量（同舊 PlanMode 兼容）
const (
	TasksPhaseInactive = "inactive"
	TasksPhaseExplore  = "explore"
	TasksPhaseDesign   = "design"
	TasksPhaseExecute  = "execute"
)

const (
	tasksPhaseTimeout = 5 * time.Minute
	tasksTotalTimeout = 20 * time.Minute
	tasksMaxDowngrades = 2
)

var globalTasksMode = &TasksMode{
	PlanPhase: TasksPhaseInactive,
}

// ============================================================================
// 狀態查詢
// ============================================================================

func (tm *TasksMode) IsActive() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.PlanPhase != TasksPhaseInactive
}

func (tm *TasksMode) Phase() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.PlanPhase
}

func (tm *TasksMode) GetTasks() []TaskItem {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]TaskItem, len(tm.tasks))
	copy(result, tm.tasks)
	return result
}

// ============================================================================
// Tasks 工具處理器
// ============================================================================

// handleTasks 處理 Tasks 工具調用
func handleTasks(args map[string]interface{}) (string, bool) {
	planPhase, _ := args["PlanPhase"].(string)
	if planPhase == "" {
		planPhase, _ = args["PlanPhase"].(string) // 兼容小寫
	}
	planContent, _ := args["PlanContent"].(string)
	if planContent == "" {
		planContent, _ = args["PlanContent"].(string) // 兼容小寫
	}

	// 解析 tasks（帶格式驗證，防止 silent failure）
	var tasks []TaskItem
	if rawTasks, ok := args["tasks"]; ok {
		arr, ok := rawTasks.([]interface{})
		if !ok {
			return fmt.Sprintf("Error: tasks 必須係 array。正確格式：{\"tasks\": [{\"id\": \"1\", \"title\": \"...\", \"status\": \"Pending\"}]}"), false
		}
		for i, item := range arr {
			m, ok := item.(map[string]interface{})
			if !ok {
				return fmt.Sprintf("Error: tasks[%d] 必須係 object（包含 id/title/status）", i), false
			}
			id := fmt.Sprintf("%v", m["id"])
			title := fmt.Sprintf("%v", m["title"])
			status := normalizeTodoStatus(fmt.Sprintf("%v", m["status"]))
			if id == "" || id == "<nil>" || title == "" || title == "<nil>" {
				return fmt.Sprintf("Error: tasks[%d] 缺少 id 或 title（必填）。\n\n💡 正確格式：{\"tasks\": [{\"id\": \"1\", \"title\": \"任務標題\", \"status\": \"Pending\"}]}", i), false
			}
			if status != "Pending" && status != "InProgress" && status != "Completed" && status != "Waiting" {
				return fmt.Sprintf("Error: tasks[%d] status 無效：%s（可選：Pending/InProgress/Completed/Waiting）", i, status), false
			}
			tasks = append(tasks, TaskItem{
				ID:     id,
				Title:  title,
				Status: status,
			})
		}
	}

	// ── 守衛：有未完成 Todos 時禁止進入 Tasks Mode ──
	// 模型必須先完成或清空現有任務，先可以切換到結構化規劃模式。
	// 已在 Tasks Mode 中 / 執行退出（execute）唔受此限。
	if (planPhase == "explore" || planPhase == "design") &&
		!globalTasksMode.IsActive() &&
		!TODO.IsEmpty() && TODO.HasUnfinishedItems() {
		unfinished := TODO.GetUnfinishedSummary()
		return fmt.Sprintf(
			"無法進入 Tasks Mode：當前仍有未完成嘅任務。\n\n%s\n\n請先完成或清空現有任務（使用 TodoWrite），然後再進入 Tasks Mode。",
			unfinished,
		), false
	}

	switch planPhase {
	case "explore":
		return enterTasksExplore()
	case "design":
		return enterTasksDesign(planContent, tasks)
	case "execute":
		return enterTasksExecute()
	default:
		// 未指定 phase → 純更新 tasks 列表
		return updateTasksList(tasks)
	}
}

// enterTasksExplore 進入探索階段
func enterTasksExplore() (string, bool) {
	globalTasksMode.mu.Lock()
	defer globalTasksMode.mu.Unlock()

	// 如果已在 active，提取 taskDesc
	taskDesc := globalTasksMode.TaskDesc
	if globalTasksMode.PlanPhase == TasksPhaseInactive {
		// 從 session 攞最近嘅 user 消息作為 task description
		session := GetGlobalSession()
		history := session.GetHistory()
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "user" {
				if content, ok := history[i].Content.(string); ok && content != "" {
					taskDesc = content
					break
				}
			}
		}
	}

	globalTasksMode.PlanPhase = TasksPhaseExplore
	globalTasksMode.StartTime = time.Now()
	globalTasksMode.PhaseStart = time.Now()
	globalTasksMode.TaskDesc = taskDesc
	globalTasksMode.DowngradeCount = 0

	dataDir := globalDataDir
	globalTasksMode.PlanFilePath = filepath.Join(dataDir, "plan.md")

	// 初始化 Tasks todos（list_id="tasks_plan"）
	todos := TODO
	if todos != nil {
		_, _ = todos.Update([]TodoItem{
			{ID: "1", Text: "Explore: 探索代碼結構", Status: "InProgress"},
			{ID: "2", Text: "Design: 設計實現方案", Status: "Pending"},
			{ID: "3", Text: "Execute: 執行任務", Status: "Pending"},
		}, "tasks_plan")
	}

	// 啟動 timeout goroutine
	globalTasksMode.stopTimeout = make(chan struct{})
	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if timedOut, _, _ := checkTasksTimeout(); timedOut {
					content := forceExitTasks("超時強制退出")
					log.Printf("[TasksMode] Timeout: force exited, plan=%d bytes", len(content))
					return
				}
			}
		}
	}(globalTasksMode.stopTimeout)

	log.Printf("[TasksMode] Enter Explore phase, task: %.80s", taskDesc)
	return "已進入 Tasks 模式 — 探索階段。\n\n使用只讀工具（TextSearch、ReadFileLines 等）探索代碼結構。完成後調用 Tasks(PlanPhase=\"design\", ...) 進入設計階段。", true
}

// autoInitTasksModeLocked 從 inactive 自動初始化 TasksMode（需已持鎖）。
// 用於從 inactive 直接跳入 design 時嘅內部初始化。
func autoInitTasksModeLocked() {
	if globalTasksMode.StartTime.IsZero() {
		globalTasksMode.StartTime = time.Now()
	}
	if globalTasksMode.PhaseStart.IsZero() {
		globalTasksMode.PhaseStart = time.Now()
	}
	if globalTasksMode.TaskDesc == "" {
		session := GetGlobalSession()
		history := session.GetHistory()
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == "user" {
				contentStr := fmt.Sprintf("%v", history[i].Content)
				if !strings.HasPrefix(contentStr, "[SYSTEM_") {
					if len(contentStr) > 80 {
						contentStr = contentStr[:80] + "..."
					}
					globalTasksMode.TaskDesc = contentStr
					break
				}
			}
		}
	}
	if globalTasksMode.PlanFilePath == "" {
		globalTasksMode.PlanFilePath = filepath.Join(globalDataDir, "plan.md")
	}
	globalTasksMode.PlanPhase = TasksPhaseExplore

	// 啟動 timeout
	if globalTasksMode.stopTimeout != nil {
		close(globalTasksMode.stopTimeout)
	}
	timeoutCh := make(chan struct{})
	globalTasksMode.stopTimeout = timeoutCh
	go func() {
		select {
		case <-time.After(tasksPhaseTimeout):
			log.Printf("[TasksMode] Phase timeout reached, auto advancing")
			globalTasksMode.mu.Lock()
			if globalTasksMode.PlanPhase != TasksPhaseInactive {
				globalTasksMode.PlanPhase = TasksPhaseDesign
				log.Printf("[TasksMode] Auto advanced to design after phase timeout")
			}
			globalTasksMode.mu.Unlock()
		case <-timeoutCh:
			return
		}
	}()
}

// enterTasksDesign 進入設計階段
// 允許直接從 inactive 進入 design（自動初始化 explore 階段），唔再強制先 explore。
func enterTasksDesign(planContent string, tasks []TaskItem) (string, bool) {
	globalTasksMode.mu.Lock()
	defer globalTasksMode.mu.Unlock()

	// 如果從 inactive 直接跳入 design：自動初始化（跳過 explore）
	if globalTasksMode.PlanPhase == TasksPhaseInactive {
		autoInitTasksModeLocked()
	}

	globalTasksMode.PlanPhase = TasksPhaseDesign
	globalTasksMode.PhaseStart = time.Now()
	globalTasksMode.PlanContent = planContent
	globalTasksMode.tasks = tasks

	// 寫入計劃文件
	if globalTasksMode.PlanFilePath != "" && planContent != "" {
		dir := filepath.Dir(globalTasksMode.PlanFilePath)
		os.MkdirAll(dir, 0755)
		os.WriteFile(globalTasksMode.PlanFilePath, []byte(planContent), 0644)
	}

	// 更新 tasks_plan todos
	todos := TODO
	if todos != nil {
		todos.Update([]TodoItem{
			{ID: "1", Text: "Explore: 探索代碼結構", Status: "Completed"},
			{ID: "2", Text: "Design: 設計實現方案", Status: "InProgress"},
			{ID: "3", Text: "Execute: 執行任務", Status: "Pending"},
		}, "tasks_plan")
	}

	log.Printf("[TasksMode] Enter Design phase, plan=%d bytes, tasks=%d", len(planContent), len(tasks))

	var sb strings.Builder
	sb.WriteString("已進入 Tasks 模式 — 設計階段。\n\n")
	if len(tasks) > 0 {
		sb.WriteString(fmt.Sprintf("已定義 %d 個任務：\n", len(tasks)))
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", t.Status, t.Title))
		}
		sb.WriteString("\n完成設計後調用 Tasks(PlanPhase=\"execute\") 退出計劃階段開始執行。\n")
		sb.WriteString("每個 Task 可用 Todos(list_id=\"task_<id>\") 管理子任務。")
	} else {
		sb.WriteString("請定義具體 tasks 列表（每個 task 包含 id、title、status）。\n")
		sb.WriteString("如需要更多探索，可用 Tasks(PlanPhase=\"explore\") 回溯。")
	}

	return sb.String(), true
}

// enterTasksExecute 進入執行階段（退出計劃）
func enterTasksExecute() (string, bool) {
	globalTasksMode.mu.Lock()

	if globalTasksMode.PlanPhase == TasksPhaseInactive {
		globalTasksMode.mu.Unlock()
		return "錯誤：Tasks 模式未激活。", false
	}

	// 退出計劃
	content := globalTasksMode.PlanContent
	if globalTasksMode.PlanFilePath != "" {
		data, err := os.ReadFile(globalTasksMode.PlanFilePath)
		if err == nil {
			content = string(data)
		}
	}
	tasks := make([]TaskItem, len(globalTasksMode.tasks))
	copy(tasks, globalTasksMode.tasks)

	elapsed := time.Since(globalTasksMode.StartTime)

	// 清理
	if globalTasksMode.stopTimeout != nil {
		close(globalTasksMode.stopTimeout)
		globalTasksMode.stopTimeout = nil
	}
	globalTasksMode.PlanPhase = TasksPhaseInactive
	globalTasksMode.PlanFilePath = ""
	globalTasksMode.PlanContent = ""
	globalTasksMode.TaskDesc = ""
	globalTasksMode.tasks = nil

	globalTasksMode.mu.Unlock()

	if globalTaskTracker != nil {
		globalTaskTracker.ResetStuckState()
	}

	// 清理 todos
	todos := TODO
	if todos != nil {
		_ = todos.Clear("tasks_plan")
	}

	log.Printf("[TasksMode] Enter Execute phase, elapsed=%v, tasks=%d", elapsed, len(tasks))

	// 注入計劃到會話歷史
	var sb strings.Builder
	sb.WriteString("Tasks 模式已退出")
	if elapsed > 0 {
		sb.WriteString(fmt.Sprintf("（耗時 %v）", elapsed.Round(time.Second)))
	}
	sb.WriteString("。\n\n")

	if len(tasks) > 0 {
		sb.WriteString("## 任務列表\n\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("- [%s] **%s** (id=%s)\n", t.Status, t.Title, t.ID))
		}
		sb.WriteString("\n")
	}
	if content != "" {
		sb.WriteString(fmt.Sprintf("## 計劃內容\n\n%s\n\n", content))
	}
	sb.WriteString("所有工具已恢復可用。按任務列表逐一執行，每個 task 用 Todos(list_id=\"task_<id>\") 管理子任務。")

	// 注入會話歷史
	session := GetGlobalSession()
	session.AddToHistory("system", sb.String())

	return sb.String(), true
}

// updateTasksList 更新 tasks 列表（唔改 plan phase）
func updateTasksList(tasks []TaskItem) (string, bool) {
	globalTasksMode.mu.Lock()
	globalTasksMode.tasks = tasks
	globalTasksMode.mu.Unlock()

	if len(tasks) == 0 {
		return "Tasks 列表已清空。", true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tasks 列表已更新（%d 個任務）：\n", len(tasks)))
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("  - [%s] %s\n", t.Status, t.Title))
	}
	return sb.String(), true
}

// ============================================================================
// 回溯（Phase 2 → Phase 1）
// ============================================================================

func handleTasksPrevPhase() (string, bool) {
	globalTasksMode.mu.Lock()
	defer globalTasksMode.mu.Unlock()

	if globalTasksMode.PlanPhase != TasksPhaseDesign {
		return "錯誤：回溯僅在設計階段可用。", false
	}
	if globalTasksMode.DowngradeCount >= tasksMaxDowngrades {
		return fmt.Sprintf("已達最大回溯次數（%d 次）。", tasksMaxDowngrades), false
	}

	globalTasksMode.PlanPhase = TasksPhaseExplore
	globalTasksMode.PhaseStart = time.Now()
	globalTasksMode.DowngradeCount++
	remaining := tasksMaxDowngrades - globalTasksMode.DowngradeCount

	log.Printf("[TasksMode] Downgrade: Design → Explore (remaining=%d)", remaining)

	todos := TODO
	if todos != nil {
		todos.Update([]TodoItem{
			{ID: "1", Text: "Explore: 探索代碼結構", Status: "InProgress"},
			{ID: "2", Text: "Design: 設計實現方案", Status: "Pending"},
			{ID: "3", Text: "Execute: 執行任務", Status: "Pending"},
		}, "tasks_plan")
	}

	return fmt.Sprintf("已回溯到探索階段。剩餘回溯次數：%d/%d。", remaining, tasksMaxDowngrades), true
}

// ============================================================================
// 超時檢查
// ============================================================================

func checkTasksTimeout() (bool, time.Duration, time.Duration) {
	globalTasksMode.mu.RLock()
	defer globalTasksMode.mu.RUnlock()

	if globalTasksMode.PlanPhase == TasksPhaseInactive {
		return false, 0, 0
	}

	phaseElapsed := time.Since(globalTasksMode.PhaseStart)
	totalElapsed := time.Since(globalTasksMode.StartTime)

	if phaseElapsed > tasksPhaseTimeout || totalElapsed > tasksTotalTimeout {
		return true, phaseElapsed, totalElapsed
	}
	return false, phaseElapsed, totalElapsed
}

func forceExitTasks(reason string) string {
	globalTasksMode.mu.Lock()
	globalTasksMode.TimedOut = true
	content := globalTasksMode.PlanContent
	if globalTasksMode.PlanFilePath != "" {
		data, _ := os.ReadFile(globalTasksMode.PlanFilePath)
		if len(data) > 0 {
			content = string(data)
		}
	}
	if globalTasksMode.stopTimeout != nil {
		close(globalTasksMode.stopTimeout)
		globalTasksMode.stopTimeout = nil
	}
	globalTasksMode.PlanPhase = TasksPhaseInactive
	globalTasksMode.PlanFilePath = ""
	globalTasksMode.PlanContent = ""
	globalTasksMode.TaskDesc = ""
	globalTasksMode.tasks = nil
	globalTasksMode.mu.Unlock()

	todos := TODO
	if todos != nil {
		_ = todos.Clear("tasks_plan")
	}

	log.Printf("[TasksMode] Force exit: %s", reason)
	return content
}

// ============================================================================
// 工具權限控制（同舊 PlanMode 兼容）
// ============================================================================

// IsToolAllowedInTasksMode 檢查工具是否在當前 phase 允許
func IsToolAllowedInTasksMode(toolName string) bool {
	globalTasksMode.mu.RLock()
	defer globalTasksMode.mu.RUnlock()

	if globalTasksMode.PlanPhase == TasksPhaseInactive {
		return true
	}

	// NextPhase / PrevPhase 處理
	if toolName == "PrevPhase" && globalTasksMode.PlanPhase == TasksPhaseDesign {
		return true
	}

	// 獲取當前 phase 允許的工具
	allowed := getTasksPhaseTools()
	for _, t := range allowed {
		if t == toolName {
			return true
		}
	}
	return false
}

func getTasksPhaseTools() []string {
	tools := make([]string, 0, len(PhaseReadTools)+6)
	tools = append(tools, PhaseReadTools...)
	tools = append(tools, "Tasks", "TodoWrite", "TodoCreate", "TodoUpdate", "TodoList") // Tasks 同 Todo 工具始終可用
	tools = append(tools, "Spawn", "SpawnCheck", "SpawnList")

	if globalTasksMode.PlanPhase == TasksPhaseDesign {
		tools = append(tools, "PrevPhase")
	}
	return tools
}

// ============================================================================
// 系統提示生成
// ============================================================================

// GetTasksModeSystemPrompt 返回當前 phase 嘅系統提示
func GetTasksModeSystemPrompt() string {
	globalTasksMode.mu.RLock()
	phase := globalTasksMode.PlanPhase
	taskDesc := globalTasksMode.TaskDesc
	phaseStart := globalTasksMode.PhaseStart
	downgradeCount := globalTasksMode.DowngradeCount
	tasks := make([]TaskItem, len(globalTasksMode.tasks))
	copy(tasks, globalTasksMode.tasks)
	globalTasksMode.mu.RUnlock()

	if phase == TasksPhaseInactive {
		return ""
	}

	elapsed := time.Since(phaseStart).Round(time.Second)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Tasks Mode — %s]\n", phaseLabel(phase)))
	sb.WriteString(fmt.Sprintf("已持續 %v\n\n", elapsed))

	if taskDesc != "" {
		sb.WriteString(fmt.Sprintf("任務：%s\n\n", taskDesc))
	}

	// 進度
	sb.WriteString("進度：\n")
	for _, p := range []string{TasksPhaseExplore, TasksPhaseDesign, TasksPhaseExecute} {
		marker := "[ ]"
		if p == phase {
			marker = "[>]"
		} else if phaseOrder(p) < phaseOrder(phase) {
			marker = "[x]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s\n", marker, phaseLabel(p)))
	}
	sb.WriteString("\n")

	if phase == TasksPhaseDesign {
		sb.WriteString(fmt.Sprintf("回溯：%d/%d\n\n", downgradeCount, tasksMaxDowngrades))
	}

	// Tasks 列表
	if len(tasks) > 0 {
		sb.WriteString("Tasks：\n")
		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("  [%s] %s (id=%s)\n", t.Status, t.Title, t.ID))
		}
		sb.WriteString("\n")
	}

	switch phase {
	case TasksPhaseExplore:
		sb.WriteString(explorePhasePrompt())
	case TasksPhaseDesign:
		sb.WriteString(designPhasePrompt())
	case TasksPhaseExecute:
		sb.WriteString(executePhasePrompt())
	}

	sb.WriteString(fmt.Sprintf("\n完成當前階段後，調用 Tasks(PlanPhase=\"%s\") 推進。", nextPhaseName(phase)))

	return sb.String()
}

func phaseLabel(phase string) string {
	switch phase {
	case TasksPhaseExplore:
		return "探索"
	case TasksPhaseDesign:
		return "設計"
	case TasksPhaseExecute:
		return "執行"
	}
	return phase
}

func phaseOrder(phase string) int {
	switch phase {
	case TasksPhaseExplore:
		return 1
	case TasksPhaseDesign:
		return 2
	case TasksPhaseExecute:
		return 3
	}
	return 0
}

func nextPhaseName(current string) string {
	switch current {
	case TasksPhaseExplore:
		return "design"
	case TasksPhaseDesign:
		return "execute"
	}
	return ""
}

// ============================================================================
// 工具定義（OpenAI 格式）
// ============================================================================

func tasksToolDef() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "Tasks",
			"description": "結構化任務分解工具。用 plan_phase 控制計劃階段（explore→design→execute），用 tasks 定義任務列表。每個 task 可用 Todos(list_id=\"task_<id>\") 管理子任務。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"PlanPhase": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"explore", "design", "execute"},
						"description": "計劃階段：explore=探索代碼結構(只讀工具), design=設計方案+定義tasks, execute=退出計劃開始執行",
					},
					"PlanContent": map[string]interface{}{
						"type":        "string",
						"description": "計劃內容（design 階段使用）。應包含 Context、Approach、Verification 三個部分。",
					},
					"tasks": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":        "string",
									"description": "任務唯一標識",
								},
								"title": map[string]interface{}{
									"type":        "string",
									"description": "任務標題",
								},
								"status": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"Pending", "InProgress", "Completed", "Waiting"},
									"description": "任務狀態",
								},
							},
							"required": []string{"id", "title", "status"},
						},
						"description": "任務列表（無依賴關係，LLM 自行決定執行順序）",
					},
				},
				"required":             []string{"PlanPhase"},
				"additionalProperties": false,
			},
		},
	}
}

// ============================================================================
// PhaseReadTools — 所有 phase 可用嘅只讀基礎工具
// ============================================================================

var PhaseReadTools = []string{
	"ReadFileLine",
	"ReadFileRange",
	"ReadFileLines",
	"TextSearch",
	"TextGrep",
	"MemoryRecall",
	"MemoryList",
}

// ============================================================================
// Phase 專用提示
// ============================================================================

func explorePhasePrompt() string {
	return `## 探索階段

目標：充分理解任務涉及的文件結構和依賴關係。

操作指引：
1. 使用 TextSearch / TextGrep 搜索關鍵詞，定位相關文件
2. 使用 ReadFileLine / ReadFileRange / ReadFileLines 閱讀相關文件
3. 對於複雜任務，使用 Spawn 創建最多 3 個並行子代理探索不同方面
4. 使用 Todos(list_id="explore") 管理探索子任務

探索要點：
- 項目整體結構是什麼？
- 需要修改哪些文件？每個文件的職責是什麼？
- 有哪些依賴和約束？
- 是否有類似的現有實現可以參考？

完成探索後，調用 Tasks(PlanPhase="design", ...) 進入設計階段。`
}

func designPhasePrompt() string {
	return `## 設計階段

目標：基於探索結果，設計實現方案並定義 Tasks。

操作指引：
1. 綜合探索發現，設計實現方案
2. 使用 Tasks(PlanPhase="design", PlanContent="...", Tasks=[...]) 寫入計劃 + 定義任務列表
3. 重新審查關鍵文件，驗證方案可行性
4. 確認無誤後，更新 Tasks 為最終版本

計劃格式（PlanContent 必須包含）：
  ## Context（上下文）
  你已了解的信息、涉及哪些文件和模塊

  ## Approach（方案）
  按步驟列出要執行的操作，具體到文件路徑和位置

  ## Verification（驗證方式）
  如何驗證修改正確性

Tasks 格式：
  [{"id":"1", "title":"任務標題", "status":"Pending"}, ...]

如有遺漏信息，可使用 PrevPhase 回溯到探索階段。

完成設計後，調用 Tasks(PlanPhase="execute") 退出並開始執行。`
}

func executePhasePrompt() string {
	return `## 執行階段

系統正在處理退出...
調用 Tasks(PlanPhase="execute") 完成退出。退出後：
- 所有工具訪問權限恢復
- Tasks 列表將注入會話歷史作為執行指引
- 每個 Task 用 Todos(list_id="task_<id>") 管理子任務`
}

// ============================================================================
// AdvancePhase — TasksMode 版本（取代舊 PlanMode AdvancePhase）
// ============================================================================

func advanceTasksPhase() (string, string, error) {
	globalTasksMode.mu.Lock()

	currentPhase := globalTasksMode.PlanPhase

	if currentPhase >= TasksPhaseExecute {
		globalTasksMode.mu.Unlock()
		return "", "", fmt.Errorf("已是最終階段，無法繼續推進")
	}

	nextPhase := nextPhaseName(currentPhase)
	if nextPhase == "" {
		globalTasksMode.mu.Unlock()
		return "", "", fmt.Errorf("無法從當前階段推進")
	}

	oldLabel := phaseLabel(currentPhase)
	globalTasksMode.PlanPhase = nextPhase
	globalTasksMode.PhaseStart = time.Now()

	// 更新 tasks_plan todos
	newPhaseOrder := phaseOrder(nextPhase)
	todos := TODO
	if todos != nil {
		items := []TodoItem{
			{ID: "1", Text: "Explore: 探索代碼結構", Status: "Pending"},
			{ID: "2", Text: "Design: 設計實現方案", Status: "Pending"},
			{ID: "3", Text: "Execute: 執行任務", Status: "Pending"},
		}
		for i := range items {
			itemOrder := i + 1
			if itemOrder < newPhaseOrder {
				items[i].Status = "Completed"
			} else if itemOrder == newPhaseOrder {
				items[i].Status = "InProgress"
			}
		}
		_, _ = todos.Update(items, "tasks_plan")
	}

	log.Printf("[TasksMode] Phase advance: %s → %s", oldLabel, phaseLabel(nextPhase))

	// Execute 階段自動退出
	if nextPhase == TasksPhaseExecute {
		content := globalTasksMode.PlanContent
		if globalTasksMode.PlanFilePath != "" {
			data, _ := os.ReadFile(globalTasksMode.PlanFilePath)
			if len(data) > 0 {
				content = string(data)
			}
		}
		tasks := make([]TaskItem, len(globalTasksMode.tasks))
		copy(tasks, globalTasksMode.tasks)

		if globalTasksMode.stopTimeout != nil {
			close(globalTasksMode.stopTimeout)
			globalTasksMode.stopTimeout = nil
		}
		globalTasksMode.PlanPhase = TasksPhaseInactive
		globalTasksMode.PlanFilePath = ""
		globalTasksMode.PlanContent = ""
		globalTasksMode.TaskDesc = ""
		globalTasksMode.tasks = nil
		globalTasksMode.mu.Unlock()

		if todos != nil {
			_ = todos.Clear("tasks_plan")
		}

		var sb strings.Builder
		sb.WriteString("Tasks 模式已退出")
		if totalElapsed := time.Since(globalTasksMode.StartTime); totalElapsed > 0 {
			sb.WriteString(fmt.Sprintf("（耗時 %v）", totalElapsed.Round(time.Second)))
		}
		sb.WriteString("。\n\n")
		if len(tasks) > 0 {
			sb.WriteString("## 任務列表\n\n")
			for _, t := range tasks {
				sb.WriteString(fmt.Sprintf("- [%s] **%s** (id=%s)\n", t.Status, t.Title, t.ID))
			}
			sb.WriteString("\n")
		}
		if content != "" {
			sb.WriteString(fmt.Sprintf("## 計劃內容\n\n%s\n\n", content))
		}
		sb.WriteString("所有工具已恢復可用。按任務列表逐一執行。")

		session := GetGlobalSession()
		session.AddToHistory("system", sb.String())
		return "已退出", sb.String(), nil
	}

	msg := fmt.Sprintf("已進入 %s\n\n可用工具已更新。%s",
		phaseLabel(nextPhase), nextPhaseDesc(nextPhase))

	globalTasksMode.mu.Unlock()
	return phaseLabel(nextPhase), msg, nil
}

func nextPhaseDesc(phase string) string {
	switch phase {
	case TasksPhaseDesign:
		return "使用 Tasks(PlanPhase=\"design\", PlanContent=\"...\", Tasks=[...]) 定義計劃和任務列表。"
	case TasksPhaseExecute:
		return ""
	}
	return ""
}

// ============================================================================
// 獲取 phase 工具
// ============================================================================

func prevPhaseToolDef() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "PrevPhase",
			"description": "回溯到探索階段（僅設計階段可用）。當設計過程中發現需要更多探索時使用。最多回溯 2 次。",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{},
				"required":             []string{},
				"additionalProperties": false,
			},
		},
	}
}

// GetTasksModeToolDefs 返回當前 phase 嘅額外工具定義
func GetTasksModeToolDefs() []map[string]interface{} {
	globalTasksMode.mu.RLock()
	phase := globalTasksMode.PlanPhase
	globalTasksMode.mu.RUnlock()

	if phase == TasksPhaseInactive {
		return nil
	}

	// Tasks 工具始終注入
	tools := []map[string]interface{}{tasksToolDef()}

	if phase == TasksPhaseDesign {
		// 設計階段加入 PrevPhase
		tools = append(tools, prevPhaseToolDef())
	}
	return tools
}

// ResetTasksMode 完整重置（用於 /new 等）
func ResetTasksMode() {
	globalTasksMode.mu.Lock()
	defer globalTasksMode.mu.Unlock()

	if globalTasksMode.stopTimeout != nil {
		close(globalTasksMode.stopTimeout)
		globalTasksMode.stopTimeout = nil
	}
	globalTasksMode.PlanPhase = TasksPhaseInactive
	globalTasksMode.PlanFilePath = ""
	globalTasksMode.PlanContent = ""
	globalTasksMode.TaskDesc = ""
	globalTasksMode.tasks = nil
	globalTasksMode.DowngradeCount = 0
	globalTasksMode.TimedOut = false
}

// resetGlobalTasksMode 測試用 helper
func resetGlobalTasksMode() {
	ResetTasksMode()
}

// GetTasksStatusJSON 返回 Tasks Mode JSON 狀態（用於 API）
func GetTasksStatusJSON() string {
	globalTasksMode.mu.RLock()
	defer globalTasksMode.mu.RUnlock()

	data := map[string]interface{}{
		"PlanPhase": globalTasksMode.PlanPhase,
		"task_desc":  globalTasksMode.TaskDesc,
		"tasks":      globalTasksMode.tasks,
		"timed_out":  globalTasksMode.TimedOut,
	}
	if globalTasksMode.PlanPhase != TasksPhaseInactive {
		data["elapsed"] = time.Since(globalTasksMode.StartTime).Round(time.Second).String()
	}

	b, _ := json.Marshal(data)
	return string(b)
}

// IsTasksModeTimedOut 檢查 Tasks Mode 是否因超時退出
func IsTasksModeTimedOut() bool {
	globalTasksMode.mu.RLock()
	defer globalTasksMode.mu.RUnlock()
	return globalTasksMode.TimedOut
}

// tryRestoreTasksModeFromPlan 從 plan.md 自動恢復 Tasks Mode 狀態（session resume 時調用）。
// 仿 Claude Code plan 恢復機制：讀取現有 plan 文件 → 恢復 TasksMode。
func tryRestoreTasksModeFromPlan() {
	planPath := filepath.Join(globalDataDir, "plan.md")
	data, err := os.ReadFile(planPath)
	if err != nil || len(data) == 0 {
		return
	}

	globalTasksMode.mu.Lock()
	defer globalTasksMode.mu.Unlock()

	// 只有在 inactive 時先恢復（避免覆蓋活躍 session）
	if globalTasksMode.PlanPhase != TasksPhaseInactive {
		return
	}

	globalTasksMode.PlanFilePath = planPath
	globalTasksMode.PlanContent = string(data)
	globalTasksMode.PlanPhase = TasksPhaseDesign
	globalTasksMode.StartTime = time.Now()
	globalTasksMode.PhaseStart = time.Now()

	log.Printf("[TasksMode] Restored from plan.md (%d bytes), entering design phase", len(data))
}

