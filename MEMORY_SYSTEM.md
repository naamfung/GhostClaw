# GarClaw 记忆系统详解

本文档详细说明 GarClaw 的双轨记忆系统架构、工作原理及使用方法。

---

## 一、系统概述

GarClaw 采用双轨记忆系统设计，两套系统相互协作，共同提供完整的记忆能力：

```
┌─────────────────────────────────────────────────────────────────────┐
│                        GarClaw 记忆系统                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    memory/ 目录                               │   │
│  │  ┌───────────────────┐    ┌───────────────────┐              │   │
│  │  │   memory.toon     │    │    MEMORY.md      │              │   │
│  │  │  结构化键值存储    │    │   长期记忆        │              │   │
│  │  │  • 精确查询        │    │   • 用户偏好      │              │   │
│  │  │  • 主动存储        │    │   • 事实信息      │              │   │
│  │  │  • Key-Value      │    │   • 项目信息      │              │   │
│  │  │                   │    │   • 技能能力      │              │   │
│  │  │  (工具调用操作)    │    │   (自动整合)      │              │   │
│  │  └───────────────────┘    └─────────┬─────────┘              │   │
│  │                                      │                        │   │
│  │                                      ▼                        │   │
│  │                            ┌───────────────────┐              │   │
│  │                            │    HISTORY.md     │              │   │
│  │                            │   会话历史摘要    │              │   │
│  │                            │   • 时间戳        │              │   │
│  │                            │   • 消息数        │              │   │
│  │                            │   • 会话摘要      │              │   │
│  │                            └───────────────────┘              │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    sessions/ 目录                             │   │
│  │  • 完整会话快照 (*.session.toon)                              │   │
│  │  • 包含完整对话历史、角色、演员信息                            │   │
│  │  • 支持会话恢复/加载                                          │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 二、双轨记忆系统

### 2.1 系统一：MemoryManager（结构化记忆）

| 特性 | 说明 |
|------|------|
| **存储位置** | `memory/memory.toon` |
| **数据格式** | TOON 结构化格式 |
| **核心功能** | Key-Value 键值对存储 |
| **使用方式** | 通过工具调用主动操作 |

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

#### 数据结构

```go
type Memory struct {
    ID          string         // 唯一标识
    Key         string         // 键名
    Value       string         // 值
    Category    MemoryCategory // 分类
    Scope       MemoryScope    // 范围 (user/global)
    Tags        []string       // 标签
    CreatedAt   time.Time      // 创建时间
    UpdatedAt   time.Time      // 更新时间
    AccessCount int            // 访问次数
}
```

### 2.2 系统二：TwoLayerMemorySystem（两层记忆）

| 特性 | 说明 |
|------|------|
| **存储位置** | `memory/` 目录 |
| **文件组成** | `MEMORY.md` + `HISTORY.md` |
| **数据格式** | Markdown 文本格式 |
| **核心功能** | 长期记忆 + 会话历史摘要 |
| **使用方式** | 系统自动整合 |

#### MEMORY.md 结构

```markdown
# 长期记忆 (MEMORY.md)

此文件存储用户的长期记忆，包括偏好、事实、项目信息等。
由 GarClaw AI Agent 自动维护。

## Preferences

- 编程语言: TypeScript
- UI框架: Next.js + shadcn/ui

## Facts

- 姓名: 张三
- 公司: ABC科技

## Projects

- 当前项目: GarClaw AI Agent 框架开发

## Skills

- 熟练: TypeScript, Go, Lua
- 学习中: Rust
```

#### HISTORY.md 结构

```markdown
# 会话历史记录 (HISTORY.md)

此文件记录 AI 与用户的对话会话历史摘要。
由 GarClaw AI Agent 自动维护。

## 会话记录

[2026-03-27 15:30] web_session_001 | 25 messages | 讨论了记忆系统的设计与实现 | web
[2026-03-27 14:00] telegram_12345 | 12 messages | 查询了北京天气和美元汇率 | telegram
```

---

## 三、系统协作流程

### 3.1 完整工作流程

```
用户消息进入
         │
         ▼
┌─────────────────────────────────────────────────────────┐
│          ChannelSessionManager（渠道会话管理器）          │
│  • 管理所有渠道（Telegram、Discord、Slack、飞书、CMD、Web） │
│  • 维护内存中的对话历史                                    │
│  • 定期自动保存到 sessions/*.session.toon                 │
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
         ┌───────────────────┴───────────────────┐
         ▼                                       ▼
┌─────────────────────┐                ┌─────────────────────┐
│     MEMORY.md       │                │     HISTORY.md      │
│  • 用户偏好更新      │                │  • 会话摘要记录     │
│  • 事实信息更新      │                │  • 时间戳          │
│  • 项目信息更新      │                │  • 标签            │
└─────────────────────┘                └─────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────┐
│        GetFullContext() 系统提示注入                      │
│  • 长期记忆上下文（最近 30 天）                            │
│  • 最近 5 条会话历史摘要                                   │
└─────────────────────────────────────────────────────────┘
```

### 3.2 记忆整合机制

MemoryConsolidator 负责在对话过程中自动整合记忆：

#### 触发条件

```go
type MemoryConsolidatorConfig struct {
    ContextWindowTokens     int     // 上下文窗口大小 (默认 128k)
    MaxCompletionTokens     int     // 最大补全 tokens (默认 4096)
    SafetyBuffer            int     // 安全缓冲 (默认 1024)
    ConsolidationRatio      float64 // 整合触发比例 (默认 0.7 = 70%)
    MaxConsolidationRound   int     // 最大整合轮数 (默认 5)
    MinMessagesToConsolidate int    // 最小整合消息数 (默认 10)
}
```

#### 整合流程

1. **监控**：持续监控当前对话的 token 使用量
2. **判断**：当达到预算的 70% 时，触发整合检查
3. **提取**：调用 LLM 分析对话内容，提取重要信息
4. **更新**：将提取的信息写入 MEMORY.md 和 HISTORY.md
5. **标记**：更新整合偏移量，避免重复处理

#### 整合输出格式

LLM 被要求输出以下格式：

```
HISTORY: [2026-03-27 15:30] 用户询问了记忆系统设计，讨论了双轨记忆架构...

MEMORY: 
## Preferences
- 编程语言: TypeScript
...
```

---

## 四、渠道会话管理

### 4.1 ChannelSessionManager

负责所有渠道的会话统一管理：

```go
type ChannelSession struct {
    ID           string       // 会话 ID
    ChannelType  string       // 渠道类型: web, telegram, discord, slack, feishu, cmd
    ChannelID    string       // 渠道内的标识（如 chatID, channelID）
    History      []Message    // 对话历史
    CreatedAt    time.Time    // 创建时间
    UpdatedAt    time.Time    // 更新时间
    MessageCount int          // 消息计数
}
```

### 4.2 支持的渠道

| 渠道 | 标识 | 说明 |
|------|------|------|
| 命令行 | `cmd` | 本地 CLI 交互 |
| Web | `web` | WebSocket 网页交互 |
| Telegram | `telegram` | Telegram Bot |
| Discord | `discord` | Discord Bot |
| Slack | `slack` | Slack App |
| 飞书 | `feishu` | 飞书/Lark Bot |

### 4.3 会话生命周期

```
用户首次消息
         │
         ▼
┌─────────────────────────────┐
│  GetOrCreateSession()       │
│  生成新会话 ID              │
│  会话有效期: 24 小时        │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  对话进行中                  │
│  • 每条消息更新 UpdatedAt    │
│  • 定期自动保存              │
│  • 5 分钟间隔                │
└────────────┬────────────────┘
             │
    ┌────────┴────────┐
    ▼                 ▼
 会话过期           用户 /new
    │                 │
    └────────┬────────┘
             │
             ▼
┌─────────────────────────────┐
│  保存会话                    │
│  • sessions/*.session.toon  │
│  • 记录到 HISTORY.md        │
└─────────────────────────────┘
```

### 4.4 会话命令

| 命令 | 说明 | 全渠道支持 |
|------|------|-----------|
| `/new` | 开始新对话 | ✅ |
| `/reset` | 重置会话 | ✅ |
| `/save` | 保存当前会话 | ✅ |
| `/stop` | 取消当前操作 | ✅ |

---

## 五、会话持久化

### 5.1 SessionPersistManager

负责会话的持久化存储：

```go
type SavedSession struct {
    ID          string    // 会话标识
    Description string    // 会话描述
    CreatedAt   time.Time // 创建时间
    UpdatedAt   time.Time // 更新时间
    History     []Message // 完整对话历史
    Role        string    // 当时的角色
    Actor       string    // 当时的演员
}
```

### 5.2 存储位置

```
garclaw/
├── memory/
│   ├── memory.toon      # 结构化记忆
│   ├── MEMORY.md        # 长期记忆
│   └── HISTORY.md       # 会话历史摘要
└── sessions/
    ├── 20260327_153022_telegram_12345.session.toon
    ├── 20260327_140000_web_session.session.toon
    └── 20260326_100000_cmd_local.session.toon
```

### 5.3 会话文件格式

```toon
session:
  id: "20260327_153022_telegram_12345"
  description: "查询北京天气和汇率"
  created_at: 2026-03-27T15:30:22Z
  updated_at: 2026-03-27T15:45:00Z
  history:
    - role: "user"
      content: "北京今天天气怎么样"
    - role: "assistant"
      content: "北京今天晴朗，气温 15°C..."
    - role: "user"
      content: "美元兑人民币汇率是多少"
    - role: "assistant"
      content: "当前美元兑人民币汇率为 7.25..."
  role: "coder"
  actor: "default"
```

---

## 六、使用指南

### 6.1 主动记忆存储

用户可以通过对话让 AI 主动存储记忆：

```
用户: 请记住，我正在使用 TypeScript 开发一个 Next.js 项目

AI 会调用: memory_save(
    key="当前项目",
    value="TypeScript + Next.js 项目",
    category="project"
)
```

### 6.2 记忆检索

```
用户: 我之前告诉过你我用什么技术栈吗？

AI 会调用: memory_recall(
    query="技术栈",
    category="project"
)

AI 回复: 是的，您之前告诉我您正在使用 TypeScript 和 Next.js 开发项目。
```

### 6.3 自动记忆整合

系统会在对话过程中自动整合重要信息：

1. 对话达到一定长度（10+ 条消息）
2. Token 使用接近上下文窗口的 70%
3. 系统自动调用 LLM 分析对话
4. 提取重要信息存入 MEMORY.md
5. 生成摘要存入 HISTORY.md

### 6.4 记忆上下文注入

每次新对话开始时，系统会自动注入记忆上下文：

```
## 关于用户的记忆

## 用户偏好
- 编程语言: TypeScript
- UI框架: Next.js + shadcn/ui

## 基本事实
- 姓名: 张三

## 项目信息
- 当前项目: GarClaw AI Agent 框架开发

## 最近的对话记录

- [2026-03-27 15:30] 讨论了记忆系统的设计与实现
- [2026-03-27 14:00] 查询了北京天气和美元汇率
```

---

## 七、配置选项

### 7.1 记忆整合配置

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

### 7.2 自动保存配置

```go
AutoSaveEnabled:   true,          // 启用自动保存
AutoSaveInterval:  5 * time.Minute, // 5 分钟间隔
```

---

## 八、最佳实践

### 8.1 记忆分类建议

| 场景 | 推荐分类 | 示例 |
|------|----------|------|
| 用户偏好设置 | `preference` | "喜欢深色主题"、"偏好函数式编程" |
| 个人信息 | `fact` | "姓名是张三"、"住在上海" |
| 工作项目 | `project` | "正在开发 GarClaw"、"使用 Go 语言" |
| 技能能力 | `skill` | "会 TypeScript"、"学习 Rust 中" |
| 当前任务 | `context` | "正在重构记忆系统" |

### 8.2 记忆键命名建议

- 使用简洁明了的键名
- 避免过于具体的键名
- 保持一致的命名风格

```
✅ 好的键名:
- "编程语言偏好"
- "当前项目"
- "工作单位"

❌ 不推荐的键名:
- "2026年3月27日编程语言偏好"
- "项目12345的详细信息"
```

### 8.3 定期清理

建议定期检查和清理过时的记忆：

```
用户: 查看所有记忆
AI: 调用 memory_list()

用户: 删除"旧项目"相关的记忆
AI: 调用 memory_forget(key="旧项目")
```

---

## 九、故障排查

### 9.1 记忆未被保存

**可能原因**：
- 记忆工具调用失败
- 文件权限问题

**解决方法**：
- 检查 `memory/` 目录权限
- 查看日志中的错误信息

### 9.2 记忆整合未触发

**可能原因**：
- 对话消息数不足 10 条
- Token 使用量未达到阈值

**解决方法**：
- 继续对话，等待自动触发
- 或手动保存会话

### 9.3 会话丢失

**可能原因**：
- 程序异常退出
- 自动保存间隔过长

**解决方法**：
- 检查 `sessions/` 目录是否有自动保存的会话
- 使用 `/save` 手动保存

---

## 十、技术实现

### 10.1 关键文件

| 文件 | 说明 |
|------|------|
| `memory.go` | MemoryManager 结构化记忆 |
| `two_layer_memory.go` | TwoLayerMemorySystem 两层记忆 |
| `memory_consolidator.go` | 记忆整合器 |
| `session.go` | SessionManager 会话管理 |
| `session_persist.go` | SessionPersistManager 持久化 |
| `channel_session_manager.go` | 渠道会话管理 |

### 10.2 全局变量

```go
var globalMemoryManager *MemoryManager           // 结构化记忆管理器
var globalTwoLayerMemory *TwoLayerMemorySystem   // 两层记忆系统
var globalMemoryConsolidator *MemoryConsolidator // 记忆整合器
var globalSessionPersist *SessionPersistManager  // 会话持久化管理器
var globalChannelSessionManager *ChannelSessionManager // 渠道会话管理器
```

---

*文档版本：2.0*
*最后更新：2026年3月*
