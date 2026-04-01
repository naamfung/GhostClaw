package main

import (
	"fmt"
	"strconv"
	"strings"
)

// 全局TodoManager实例
var TODO = NewTodoManager()

// TodoManager 管理待办事项
type TodoManager struct {
	items []TodoItem
}

// TodoItem 待办事项项
type TodoItem struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"` // pending, in_progress, completed
}

// NewTodoManager 创建新的TodoManager
func NewTodoManager() *TodoManager {
	return &TodoManager{
		items: []TodoItem{},
	}
}

// Update 更新待办事项列表
func (tm *TodoManager) Update(items []TodoItem) (string, error) {
	if len(items) > 20 {
		return "", fmt.Errorf("max 20 todos allowed")
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

		if status != "pending" && status != "in_progress" && status != "completed" {
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

	tm.items = validated
	return tm.Render(), nil
}

// Render 渲染待办事项列表
func (tm *TodoManager) Render() string {
	if len(tm.items) == 0 {
		return "No todos."
	}

	lines := []string{}
	done := 0

	for _, item := range tm.items {
		var marker string
		switch item.Status {
		case "pending":
			marker = "[ ]"
		case "in_progress":
			marker = "[>"
		case "completed":
			marker = "[x]"
			done++
		default:
			marker = "[?]"
		}
		lines = append(lines, fmt.Sprintf("%s #%s: %s", marker, item.ID, item.Text))
	}

	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", done, len(tm.items)))
	return strings.Join(lines, "\n")
}

