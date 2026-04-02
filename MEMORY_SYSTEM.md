# GhostClaw 记忆系统详解

本文档详细说明 GhostClaw 的 GORM/SQLite 记忆系统架构、工作原理及使用方法。GhostClaw 采用基于 GORM ORM 和 SQLite 数据库的统一记忆方案，替代了旧版 GhostClaw 基于文件系统的双轨记忆设计（`memory.toon` + `MEMORY.md` + `HISTORY.md`）。

---

## 一、系统概述

GhostClaw 的记忆系统所有数据统一存储在 `ghostclaw.db`（SQLite）文件中，通过 GORM 提供类型安全的数据库操作接口。系统包含三张核心表：`Memories`（结构化记忆）、`Sessions`（会话记录）、`Experiences`（经验学习）。

```
┌─────────────────────────────────────────────────────────────────────┐
│                     GhostClaw 记忆系统                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                GORM / SQLite (ghostclaw.db)                  │   │
│  │                                                              │   │
│  │  ┌─────────────────┐  ┌─────────────────┐                   │   │
│  │  │   Memories 表    │  │  Sessions 表    │                   │   │
│  │  │  结构化记忆存储   │  │  会话摘要记录    │                   │   │
│  │  │  • Key-Value     │  │  • Summary      │                   │   │
│  │  │  • Category/Scope│  │  • Channel      │                   │   │
│  │  │  • Score/Access  │  │  • StartTime    │                   │   │
│  │  └─────────────────┘  └─────────────────┘                   │   │
│  │                                                              │   │
│  │  ┌─────────────────┐                                        │   │
│  │  │ Experiences 表   │                                        │   │
│  │  │  经验学习记录     │                                        │   │
│  │  │  • TaskDesc      │                                        │   │
│  │  │  • Result/Score  │                                        │   │
│  │  │  • UsedCount     │                                        │   │
│  │  └─────────────────┘                                        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │              UnifiedMemory (unified_memory.go)                │   │
│  │  • memory_save / memory_recall / memory_forget / memory_list  │   │
│  │  • SearchEntries（全文搜索）                                   │   │
│  │  • 记忆整合（MemoryConsolidator）                               │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 二、数据库表结构

### 2.1 Memories 表（结构化记忆）

Memories 表是核心的记忆存储，采用 Key-Value 模型，支持分类、评分和访问计数。

```go
type Memories struct {
    ID        string    `gorm:"primaryKey;type:text"`
    Category  string    `gorm:"index;default:fact;type:text"`
    Scope     string    `gorm:"index;default:user;type:text"`
    Key       string    `gorm:"index;type:text"`
    Value     string    `gorm:"type:text"`
    Tags      string    `gorm:"type:text"` // JSON array
    CreatedAt time.Time
    UpdatedAt time.Time
    AccessCnt int       `gorm:"default:0"`
    Score     float64   `gorm:"default:0"`
}
```

#### 字段说明

| 字段 | 类型 | 索引 | 说明 |
|------|------|------|------|
| `ID` | string | PK | UUID 唯一标识 |
| `Category` | string | Yes | 分类：`preference`/`fact`/`project`/`skill`/`context` |
| `Scope` | string | Yes | 范围：`user`（用户级）/ `global`（全局） |
| `Key` | string | Yes | 记忆键名，用于精确查询 |
| `Value` | string | No | 记忆内容 |
| `Tags` | string | No | JSON 数组格式的标签 |
| `Score` | float64 | No | 评分（0.0~1.0），用于排序和权重计算 |
| `AccessCnt` | int | No | 访问计数，高频记忆更容易被检索 |

#### 记忆分类

| 分类 | 标识 | 说明 | 示例 |
|------|------|------|------|
| 偏好 | `preference` | 用户偏好设置 | 编程语言偏好、UI 风格 |
| 事实 | `fact` | 客观事实信息 | 用户姓名、工作单位 |
| 项目 | `project` | 项目相关信息 | 当前项目名称、技术栈 |
| 技能 | `skill` | 技能/能力信息 | 已掌握的技能 |
| 上下文 | `context` | 临时上下文信息 | 当前任务背景 |

#### 记忆工具

| 工具 | 说明 | 示例 |
|------|------|------|
| `memory_save` | 保存记忆 | `memory_save(key="语言偏好", value="TypeScript", category="preference")` |
| `memory_recall` | 检索记忆 | `memory_recall(query="偏好", category="preference")` |
| `memory_forget` | 删除记忆 | `memory_forget(key="语言偏好")` |
| `memory_list` | 列出记忆 | `memory_list(category="fact")` |

### 2.2 Sessions 表（会话记录）

Sessions 表记录会话的摘要信息，用于历史追踪和上下文恢复。

```go
type Sessions struct {
    ID           string    `gorm:"primaryKey;type:text"`
    SessionKey   string    `gorm:"index;type:text"`
    StartTime    time.Time
    EndTime      time.Time
    MessageCount int
    Summary      string    `gorm:"type:text"`
    Tags         string    `gorm:"type:text"` // JSON array
    Channel      string    `gorm:"type:text"`
}
```

### 2.3 Experiences 表（经验学习）

Experiences 表记录任务执行过程中的经验，供模型未来参考和学习。

```go
type Experiences struct {
    ID        string    `gorm:"primaryKey;type:text"`
    SessionID string    `gorm:"index;type:text"`
    TaskDesc  string    `gorm:"type:text"`
    Actions   string    `gorm:"type:text"` // JSON array of ExperienceAction
    Result    bool
    Summary   string    `gorm:"type:text"`
    Score     float64   `gorm:"default:0.5"`
    UsedCount int       `gorm:"default:0"`
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

---

## 三、系统工作流程

### 3.1 完整记忆流程

```
用户消息进入
         │
         ▼
┌─────────────────────────────────────────────────────────┐
│            GlobalSession（全局单会话）                      │
│  • 所有渠道消息统一写入 History                            │
│  • 通过 AddToHistory() 添加消息                           │
│  • 异步调用 autoSaveHistory() 持久化                       │
└────────────────────────────┬────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────┐
│          MemoryConsolidator（记忆整合器）                 │
│  • 监控对话 token 预算                                    │
│  • 当达到 70% 阈值时触发整合                               │
│  • 调用 LLM 生成摘要                                      │
└────────────────────────────┬────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────┐
│          GORM / SQLite 数据库                             │
│  • Memories 表：提取重要信息存入                          │
│  • Sessions 表：生成摘要记录                              │
│  • Experiences 表：记录有效经验                          │
└─────────────────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────┐
│        GetFullContext() 系统提示注入                      │
│  • 从 Memories 表检索相关记忆                            │
│  • 注入用户偏好、事实、项目信息                            │
└─────────────────────────────────────────────────────────┘
```

### 3.2 记忆整合机制

MemoryConsolidator 负责在对话过程中自动整合记忆：

#### 触发条件

```go
config := MemoryConsolidatorConfig{
    ContextWindowTokens:     128000,  // 128k 上下文
    ConsolidationRatio:      0.7,     // 70% 时触发
    MinMessagesToConsolidate: 10,     // 最少 10 条消息
}
```

#### 整合流程

1. **监控**：持续监控当前对话的 token 使用量
2. **判断**：当达到预算的 70% 时，触发整合检查
3. **提取**：调用 LLM 分析对话内容，提取重要信息
4. **写入**：将提取的信息写入 GORM 数据库（Memories 表）
5. **记录**：生成摘要写入 Sessions 表

---

## 四、与旧版 GhostClaw 的对比

### 4.1 架构差异

| 特性 | GhostClaw（旧版） | GhostClaw（新版） |
|------|----------------|-------------------|
| 存储方式 | 文件系统 | GORM/SQLite 数据库 |
| 结构化记忆 | `memory/memory.toon` | `Memories` 表 |
| 长期记忆 | `memory/MEMORY.md` | `Memories` 表（统一） |
| 历史摘要 | `memory/HISTORY.md` | `Sessions` 表 |
| 经验学习 | 无 | `Experiences` 表（新增） |
| 记忆排序 | 无 | `Score` 字段评分排序 |
| 访问计数 | 无 | `AccessCnt` 字段 |
| 查询方式 | 文件读写 | SQL 查询（GORM） |
| 数据完整性 | 无保障 | SQLite 事务保障 |

### 4.2 迁移说明

从 GhostClaw 迁移到 GhostClaw 时，旧的 `memory/` 目录文件不会被自动导入。如需保留旧记忆，需手动将 `memory.toon` 中的关键信息通过 `memory_save` 工具重新保存到新的数据库中。

---

## 五、使用指南

### 5.1 主动记忆存储

用户可以通过对话让 AI 主动存储记忆：

```
用户: 请记住，我正在使用 TypeScript 开发一个 Next.js 项目

AI 会调用: memory_save(
    key="当前项目",
    value="TypeScript + Next.js 项目",
    category="project"
)
```

### 5.2 记忆检索

```
用户: 我之前告诉过你我用什么技术栈吗？

AI 会调用: memory_recall(
    query="技术栈",
    category="project"
)

AI 回复: 是的，您之前告诉我您正在使用 TypeScript 和 Next.js 开发项目。
```

### 5.3 自动记忆整合

系统会在对话过程中自动整合重要信息：

1. 对话达到一定长度（10+ 条消息）
2. Token 使用接近上下文窗口的 70%
3. 系统自动调用 LLM 分析对话
4. 提取重要信息存入 GORM 数据库

### 5.4 记忆上下文注入

每次新对话开始时，系统会自动从数据库检索相关记忆并注入到系统提示中：

```
## 关于用户的记忆

## 用户偏好
- 编程语言: TypeScript
- UI框架: Next.js + shadcn/ui

## 基本事实
- 姓名: 张三

## 项目信息
- 当前项目: GhostClaw AI Agent 框架开发
```

---

## 六、配置选项

### 6.1 记忆整合配置

可在代码中调整 MemoryConsolidator 配置：

```go
config := MemoryConsolidatorConfig{
    ContextWindowTokens:     128000,  // 128k 上下文
    MaxCompletionTokens:     4096,    // 最大补全
    SafetyBuffer:            1024,    // 安全缓冲
    ConsolidationRatio:      0.7,     // 70% 时触发
    MaxConsolidationRound:   5,       // 最大 5 轮
    MinMessagesToConsolidate: 10,     // 最少 10 条消息
}
```

---

## 七、最佳实践

### 7.1 记忆分类建议

| 场景 | 推荐分类 | 示例 |
|------|----------|------|
| 用户偏好设置 | `preference` | "喜欢深色主题"、"偏好函数式编程" |
| 个人信息 | `fact` | "姓名是张三"、"住在上海" |
| 工作项目 | `project` | "正在开发 GhostClaw"、"使用 Go 语言" |
| 技能能力 | `skill` | "会 TypeScript"、"学习 Rust 中" |
| 当前任务 | `context` | "正在重构记忆系统" |

### 7.2 定期清理

建议定期检查和清理过时的记忆：

```
用户: 查看所有记忆
AI: 调用 memory_list()

用户: 删除"旧项目"相关的记忆
AI: 调用 memory_forget(key="旧项目")
```

---

## 八、技术实现

### 8.1 关键文件

| 文件 | 说明 |
|------|------|
| `db.go` | GORM 数据库初始化与表模型定义 |
| `unified_memory.go` | 统一记忆系统（CRUD 操作） |
| `memory_consolidator.go` | 记忆整合器（自动整合） |
| `memory_tools.go` | 记忆工具（memory_save/recall/forget/list） |
| `session_persist.go` | 会话持久化管理器 |
| `session.go` | GlobalSession 全局单会话 |

### 8.2 全局变量

```go
var globalDB *gorm.DB                          // GORM 数据库实例
var globalUnifiedMemory *UnifiedMemory        // 统一记忆系统
var globalMemoryConsolidator *MemoryConsolidator // 记忆整合器
var globalSessionPersist *SessionPersistManager  // 会话持久化
```

---

*文档版本：3.0*
*最后更新：2026年4月*
