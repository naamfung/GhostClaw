package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
)

// ============================================================================
// prefix_shape.go — Prefix 缓存诊断系统
// ============================================================================
// 参考 DeepSeek-Reasonix 的 cache_shape.go 设计。
// 在每次 LLM 请求前捕获 system + tools 的哈希快照，与上次对比，
// 找出哪个层发生了变化（system / tools / log_rewrite），
// 解释 provider prompt cache miss 的根因。
//
// 集成点：prepareRequestData() 内组装完 system + tools 后调用 CaptureAndCompare
// 日志输出：[PrefixShape] step=N system=ab12cd34 tools=ef56ab78 changed=[tools]
// ============================================================================

// PrefixShape 前缀哈希快照（一个 step 一个）
type PrefixShape struct {
	SystemHash        string // system prompt 的短哈希（sha256 前 8 字节）
	ToolsHash         string // tools schema 的短哈希
	PrefixHash        string // system + tools 合并哈希（前缀指纹）
	LogRewriteVersion int    // 历史重写版本（compaction/normalize 等会递增）
	ToolSchemaTokens  int    // 工具 schema 估算 token 数
	MessageCount      int    // 本步消息数
}

// shortHash 对任意值取 sha256 前 8 字节的 hex 表示
func shortHash(v interface{}) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:4])
}

// estimateShapeTokens 粗略估算字符串 token 数（4 字符/token）
func estimateShapeTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}

// CaptureShape 捕获当前前缀状态快照
// systemPrompt: 最终 system prompt
// tools: 工具定义列表（任意格式：map[string]interface{} 或 anthropicToolBlock 等）
// rewriteVersion: 历史重写版本号
func CaptureShape(systemPrompt string, tools interface{}, rewriteVersion, msgCount int) PrefixShape {
	// 工具列表归一化排序：避免相同工具集因顺序不同导致 hash 不同
	normalizedTools := normalizeToolsForShape(tools)
	toolsJSON, _ := json.Marshal(normalizedTools)
	return PrefixShape{
		SystemHash:        shortHash(systemPrompt),
		ToolsHash:         shortHash(string(toolsJSON)),
		PrefixHash: shortHash(map[string]interface{}{
			"system": systemPrompt,
			"tools":  string(toolsJSON),
		}),
		LogRewriteVersion: rewriteVersion,
		ToolSchemaTokens:  estimateShapeTokens(string(toolsJSON)),
		MessageCount:      msgCount,
	}
}

// normalizeToolsForShape 将工具列表归一化为稳定顺序
// 支持 []map[string]interface{}、[]anthropicToolBlock、[]interface{} 等
func normalizeToolsForShape(tools interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}

	switch v := tools.(type) {
	case []map[string]interface{}:
		for _, t := range v {
			out = append(out, normalizeOneTool(t))
		}
	case []interface{}:
		for _, t := range v {
			if m, ok := t.(map[string]interface{}); ok {
				out = append(out, normalizeOneTool(m))
			} else {
				// 兜底：直接序列化原值
				out = append(out, map[string]interface{}{"_raw": t})
			}
		}
	case []anthropicToolBlock:
		for _, t := range v {
			out = append(out, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"schema":      t.InputSchema,
			})
		}
	default:
		// 其他类型：直接短哈希（无法排序，但能检测变化）
		if tools != nil {
			out = append(out, map[string]interface{}{"_raw": tools})
		}
	}

	// 按 name 排序保证顺序稳定
	sort.Slice(out, func(i, j int) bool {
		ni, _ := out[i]["name"].(string)
		nj, _ := out[j]["name"].(string)
		return ni < nj
	})
	return out
}

// normalizeOneTool 提取工具的关键字段用于哈希（忽略 cache_control 等运行时字段）
func normalizeOneTool(t map[string]interface{}) map[string]interface{} {
	name, _ := t["name"].(string)
	if name == "" {
		if fn, ok := t["function"].(map[string]interface{}); ok {
			name, _ = fn["name"].(string)
		}
	}
	desc, _ := t["description"].(string)
	if desc == "" {
		if fn, ok := t["function"].(map[string]interface{}); ok {
			desc, _ = fn["description"].(string)
		}
	}
	// schema 可能是 input_schema 或 parameters
	schema := t["input_schema"]
	if schema == nil {
		schema = t["parameters"]
	}
	if schema == nil {
		if fn, ok := t["function"].(map[string]interface{}); ok {
			schema = fn["parameters"]
		}
	}
	return map[string]interface{}{
		"name":        name,
		"description": desc,
		"schema":      schema,
	}
}

// CompareShape 返回前后两个 shape 的差异原因
func CompareShape(prev, cur PrefixShape) []string {
	reasons := []string{}
	if prev.SystemHash != "" && prev.SystemHash != cur.SystemHash {
		reasons = append(reasons, "system")
	}
	if prev.ToolsHash != "" && prev.ToolsHash != cur.ToolsHash {
		reasons = append(reasons, "tools")
	}
	if prev.LogRewriteVersion != cur.LogRewriteVersion {
		reasons = append(reasons, "log_rewrite")
	}
	return reasons
}

// ============================================================================
// 全局前缀 shape 跟踪器（per-session 粒度）
// ============================================================================

// prefixShapeTracker 跟踪最近一次的前缀 shape，用于下一步对比
type prefixShapeTracker struct {
	mu        sync.RWMutex
	lastShape PrefixShape
	step      int
}

var globalPrefixShapeTracker = &prefixShapeTracker{}

// IncrementRewriteVersion 历史被重写时（compaction/normalize）调用，递增版本号
// 这样下一步 CompareShape 能检测到 "log_rewrite" 变化
func (t *prefixShapeTracker) IncrementRewriteVersion() {
	t.mu.Lock()
	t.lastShape.LogRewriteVersion++
	t.mu.Unlock()
}

// CaptureAndCompare 捕获当前 shape 并与上次对比，输出诊断日志
// 返回当前 shape 供调用方使用
func (t *prefixShapeTracker) CaptureAndCompare(systemPrompt string, tools interface{}, msgCount int) PrefixShape {
	cur := CaptureShape(systemPrompt, tools, t.lastShape.LogRewriteVersion, msgCount)

	t.mu.RLock()
	prev := t.lastShape
	step := t.step
	t.mu.RUnlock()

	reasons := CompareShape(prev, cur)
	if len(reasons) > 0 {
		// 前缀发生变化：缓存必然 miss
		log.Printf("[PrefixShape] step=%d system=%s tools=%s tokens=%d msgs=%d changed=[%s] (prev: system=%s tools=%s)",
			step, cur.SystemHash, cur.ToolsHash, cur.ToolSchemaTokens, cur.MessageCount,
			joinStrings(reasons, ","), prev.SystemHash, prev.ToolsHash)
	} else if prev.PrefixHash != "" {
		// 前缀稳定：缓存应命中
		log.Printf("[PrefixShape] step=%d system=%s tools=%s tokens=%d msgs=%d stable (prefix cached)",
			step, cur.SystemHash, cur.ToolsHash, cur.ToolSchemaTokens, cur.MessageCount)
	}

	t.mu.Lock()
	t.lastShape = cur
	t.step++
	t.mu.Unlock()

	return cur
}

// Reset 重置跟踪器（新 session 开始时调用）
func (t *prefixShapeTracker) Reset() {
	t.mu.Lock()
	t.lastShape = PrefixShape{}
	t.step = 0
	t.mu.Unlock()
}

// Save 返回当前 tracker 状态快照（供 planner 等子调用隔离前缀跟踪）
func (t *prefixShapeTracker) Save() (PrefixShape, int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastShape, t.step
}

// Restore 恢复 tracker 状态快照
func (t *prefixShapeTracker) Restore(s PrefixShape, step int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastShape = s
	t.step = step
}

// joinStrings 简易字符串 join（避免引入 strings 包）
func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for i := 1; i < len(ss); i++ {
		out += sep + ss[i]
	}
	return out
}

// ResetPrefixShapeTracker 对外暴露的重置接口（供 session 切换时调用）
func ResetPrefixShapeTracker() {
	globalPrefixShapeTracker.Reset()
}

// IncrementLogRewriteVersion 对外暴露的版本递增（供 compaction/normalize 调用）
func IncrementLogRewriteVersion() {
	globalPrefixShapeTracker.IncrementRewriteVersion()
}

// SavePrefixShapeTracker 保存全局 tracker 快照
// 用于 Coordinator planner 调用前后隔离前缀跟踪，避免污染 executor 的诊断数据
func SavePrefixShapeTracker() (PrefixShape, int) {
	return globalPrefixShapeTracker.Save()
}

// RestorePrefixShapeTracker 恢复全局 tracker 快照
func RestorePrefixShapeTracker(s PrefixShape, step int) {
	globalPrefixShapeTracker.Restore(s, step)
}
