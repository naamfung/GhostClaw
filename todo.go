package main

import (
        "fmt"
        "sort"
        "strconv"
        "strings"
        "sync"
)

// ============================================================================
// TodoManager - 多列表待辦事項管理器
// ============================================================================
// 支持多個獨立的 todo 列表，每個列表由 list_id 標識。
// Plan Mode 每個 Phase 使用不同的 list_id。
// ============================================================================

// 全局 TodoManager 實例
var TODO = NewTodoManager()

// TodoItem 待辦事項
type TodoItem struct {
        ID     string `json:"id"`
        Text   string `json:"text"`
        Status string `json:"status"` // pending, in_progress, completed, waiting
}

// TodoList 一個 todo 列表
type TodoList struct {
        ID    string
        Items []TodoItem
}

// TodoManager 管理多個待辦事項列表
type TodoManager struct {
        lists map[string]*TodoList // list_id -> TodoList
        mu    sync.RWMutex
}

// NewTodoManager 創建新的 TodoManager
func NewTodoManager() *TodoManager {
        return &TodoManager{
                lists: make(map[string]*TodoList),
        }
}

// Update 更新指定列表的待辦事項
// list_id 為空時使用 "default"
func (tm *TodoManager) Update(items []TodoItem, listID ...string) (string, error) {
        id := "default"
        if len(listID) > 0 && listID[0] != "" {
                id = listID[0]
        }

        tm.mu.Lock()
        defer tm.mu.Unlock()

        if len(items) > 20 {
                return "", fmt.Errorf("max 20 todos per list")
        }

        validated := []TodoItem{}
        inProgressCount := 0

        for i, item := range items {
                text := strings.TrimSpace(item.Text)
                status := strings.ToLower(item.Status)
                itemID := item.ID
                if itemID == "" {
                        itemID = strconv.Itoa(i + 1)
                }

                if text == "" {
                        return "", fmt.Errorf("item %s: text required", itemID)
                }

                if status != "pending" && status != "in_progress" && status != "completed" && status != "waiting" {
                        return "", fmt.Errorf("item %s: invalid status '%s'", itemID, status)
                }

                if status == "in_progress" {
                        inProgressCount++
                }

                validated = append(validated, TodoItem{
                        ID:     itemID,
                        Text:   text,
                        Status: status,
                })
        }

        if inProgressCount > 1 {
                return "", fmt.Errorf("only one task can be in_progress at a time")
        }

        tm.lists[id] = &TodoList{ID: id, Items: validated}
        return tm.renderListLocked(id), nil
}

// UpdateDefault 舊接口兼容：更新默認列表
func (tm *TodoManager) UpdateDefault(items []TodoItem) (string, error) {
        return tm.Update(items)
}

// GetItems 獲取指定列表的所有事項（副本）
func (tm *TodoManager) GetItems(listID ...string) []TodoItem {
        id := "default"
        if len(listID) > 0 && listID[0] != "" {
                id = listID[0]
        }

        tm.mu.RLock()
        defer tm.mu.RUnlock()

        list, ok := tm.lists[id]
        if !ok {
                return nil
        }

        result := make([]TodoItem, len(list.Items))
        copy(result, list.Items)
        return result
}

// Render 渲染指定列表的待辦事項
func (tm *TodoManager) Render(listID ...string) string {
        id := "default"
        if len(listID) > 0 && listID[0] != "" {
                id = listID[0]
        }

        tm.mu.RLock()
        defer tm.mu.RUnlock()
        return tm.renderListLocked(id)
}

// renderListLocked 渲染列表（需持有鎖）
func (tm *TodoManager) renderListLocked(id string) string {
        list, ok := tm.lists[id]
        if !ok || len(list.Items) == 0 {
                return ""
        }

        lines := []string{}
        done := 0

        for _, item := range list.Items {
                var marker string
                switch item.Status {
                case "pending":
                        marker = "[ ]"
                case "in_progress":
                        marker = "[>"
                case "waiting":
                        marker = "[~]"
                case "completed":
                        marker = "[x]"
                        done++
                default:
                        marker = "[?]"
                }
                lines = append(lines, fmt.Sprintf("%s #%s: %s", marker, item.ID, item.Text))
        }

        result := fmt.Sprintf(" todos[%s] (%d/%d completed)", id, done, len(list.Items))
        for _, line := range lines {
                result += "\n  " + line
        }
        return result
}

// Clear 清空指定列表
func (tm *TodoManager) Clear(listID ...string) error {
        id := "default"
        if len(listID) > 0 && listID[0] != "" {
                id = listID[0]
        }

        tm.mu.Lock()
        defer tm.mu.Unlock()

        delete(tm.lists, id)
        return nil
}

// planRelatedListIDs Plan Mode 使用的列表 ID，退出守衛應排除這些列表
var planRelatedListIDs = map[string]bool{
        "plan":   true,
        "phase1": true,
        "phase2": true,
        "phase3": true,
        "phase4": true,
}

// HasUnfinishedItems 檢查是否有未完成的非計劃項目（pending 或 in_progress）
// 用於 AgentLoop 退出守衛：如果有未完成項目，程序不允許模型停止
func (tm *TodoManager) HasUnfinishedItems() bool {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        for id, list := range tm.lists {
                if planRelatedListIDs[id] {
                        continue
                }
                for _, item := range list.Items {
                        if item.Status == "pending" || item.Status == "in_progress" {
                                return true
                        }
                }
        }
        return false
}

// AllUnfinishedAreWaiting 檢查所有未完成的非計劃項目是否都處於 waiting 狀態
// 如果是，說明所有剩餘任務已提交為異步操作（如 cron_add），允許退出
func (tm *TodoManager) AllUnfinishedAreWaiting() bool {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        for id, list := range tm.lists {
                if planRelatedListIDs[id] {
                        continue
                }
                for _, item := range list.Items {
                        if item.Status == "pending" || item.Status == "in_progress" {
                                return false
                        }
                }
        }
        return true
}

// GetUnfinishedSummary 獲取未完成的非計劃項目摘要（用於注入續行提示）
func (tm *TodoManager) GetUnfinishedSummary() string {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        var unfinished []string
        for id, list := range tm.lists {
                if planRelatedListIDs[id] {
                        continue
                }
                for _, item := range list.Items {
                        if item.Status == "pending" || item.Status == "in_progress" {
                                unfinished = append(unfinished, fmt.Sprintf("  - #%s: %s [%s]", item.ID, item.Text, item.Status))
                        }
                }
        }
        if len(unfinished) == 0 {
                return ""
        }
        return strings.Join(unfinished, "\n")
}

// ClearAll 清空所有列表
func (tm *TodoManager) ClearAll() {
        tm.mu.Lock()
        defer tm.mu.Unlock()
        tm.lists = make(map[string]*TodoList)
}

// ListIDs 返回所有列表 ID
func (tm *TodoManager) ListIDs() []string {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        ids := make([]string, 0, len(tm.lists))
        for id := range tm.lists {
                ids = append(ids, id)
        }
        sort.Strings(ids)
        return ids
}

// RenderAll 渲染所有非空列表
func (tm *TodoManager) RenderAll() string {
        tm.mu.RLock()
        defer tm.mu.RUnlock()

        if len(tm.lists) == 0 {
                return "No todos."
        }

        // 收集所有列表並排序
        ids := make([]string, 0, len(tm.lists))
        for id, list := range tm.lists {
                if len(list.Items) > 0 {
                        ids = append(ids, id)
                }
        }
        sort.Strings(ids)

        if len(ids) == 0 {
                return "No todos."
        }

        parts := make([]string, 0, len(ids))
        for _, id := range ids {
                parts = append(parts, tm.renderListLocked(id))
        }
        return strings.Join(parts, "\n")
}
