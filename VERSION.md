# 版本历史

## v0.0.3 (2026-04-01)

### 文档全面更新：GhostClaw → GhostClaw

将所有文档从旧版 GhostClaw 名称与架构描述更新为 GhostClaw 的新架构。

**文档更新**：

1. **version.go**：启动横幅从 `GhostClaw` 更新为 `GhostClaw`
2. **README.md**：全面重写，反映 GhostClaw 的全局单会话模型、GORM/SQLite 记忆系统、新项目结构
3. **SYSTEM_DESIGN.md**：重写系统设计文档，更新架构图为 GlobalSession 架构，更新记忆系统为 GORM/SQLite 方案
4. **USER_GUIDE.md**：全面更新为 GhostClaw 品牌，反映单会话模型下的交互方式
5. **CHANNELS_GUIDE.md**：更新为 GhostClaw 品牌，新增 IRC/Webhook/XMPP/Matrix 渠道配置说明
6. **MEMORY_SYSTEM.md**：重写为基于 GORM/SQLite 的记忆系统架构，移除旧的文件系统相关描述

**名称变更**：
- 所有文档中的 `GhostClaw` 引用替换为 `GhostClaw`
- 所有文档中的 `ghostclaw` 二进制名引用替换为 `ghostclaw`
- 切换标记 `[GARCLAW:NEXT:...]` / `[GARCLAW:END]` 保持不变（代码兼容）

## v0.0.2 (2026-04-01)

### 架构升级：全局单会话模型

GhostClaw 是 GhostClaw 的架构重构版本，核心变更如下：

**架构重塑**：

1. **全局单会话模型**：移除了旧版的 `WebSessionManager` 和 `channel_session_manager`（多会话管理器），改为全局唯一的 `GlobalSession`，所有渠道（CLI、HTTP/WS、Telegram、Discord、Slack 等）共享同一个会话上下文。这意味着无论从哪个渠道发来消息，模型看到的是同一份对话历史。

2. **新增文件**：
   - `session.go` — `GlobalSession` 全局单例，包含历史管理、任务并发控制、输出队列、自动持久化
   - `session_channel.go` — `SessionChannel`，将 AgentLoop 的流式输出桥接到 GlobalSession 的输出队列
   - `command.go` — `ApplyCommandResult` 和 `HandleSlashCommandWithDefaults`，统一所有渠道的斜杠命令处理
   - `db.go` — GORM/SQLite 数据库层，`Memories`/`Sessions`/`Experiences` 三张表
   - `types.go` — 将 `Message`、`ToolUse`、`Response` 从 `main.go` 抽离为独立类型文件

3. **移除文件**：
   - `channel_session_manager.go` — 不再需要多会话路由
   - `session_tools.go` — 会话工具合并到其他模块
   - `web_session.go` — 被 `GlobalSession` 取代
   - `web_session_channel.go` — 被 `SessionChannel` 取代
   - `memory/` 目录 — 记忆系统从文件系统迁移到 GORM 数据库

4. **记忆系统升级**：
   - 从基于文件系统（`memory/memory.toon`、`MEMORY.md`、`HISTORY.md`）迁移到 GORM/SQLite 数据库存储
   - `unified_memory.go` 大幅重写，所有 CRUD 操作通过 `globalDB` 执行
   - 新增 `Score` 字段支持记忆条目排序
   - 数据库文件：`ghostclaw.db`

5. **渠道架构调整**：
   - Telegram、Discord、Slack、Webhook 渠道改为内置 `processUserInput` 方法，直接管理任务生命周期（`TryStartTask` → `AgentLoop` → `SetTaskRunning`），不再依赖旧版的外部 `ProcessChannelMessage` 路由
   - 所有渠道统一使用 `HandleSlashCommandWithDefaults` 处理斜杠命令

6. **消息压缩**：
   - `CallModel.go` 新增 `compressMessages` 函数，支持三级消息压缩策略：
     - Level 0：简化工具消息（提取原始命令+后200字符）
     - Level 1：移除所有工具消息
     - Level 2：保留最近20条消息
   - 替代旧版的 `MaxRequestSizeBytes` 请求体大小检查机制

7. **模块重命名**：
   - Go module: `ghostclaw` → `ghostclaw`
   - 二进制输出: `ghostclaw` → `ghostclaw`
   - 数据库文件: `ghostclaw.db` → `ghostclaw.db`

### 修复

- 修复 `telegram_channel.go:245`：`c.Sender().ID`（`int64`）未转换为 `string` 导致编译失败
- 修复 `irc_channel.go:6`：误引入未使用的 `"context"` 包导致编译失败
- 修复 `db.go`：`Memories` 表缺少 `Score` 字段，导致 `unified_memory.go` SQL 查询报错 `no such column: score`
- 修复 `main.go`：v0.0.1 版本中初始化代码被 `// ...` 省略，导致所有组件未初始化，程序无法正常工作

## v0.0.1 (2026-03-30)

### 架構重置 版本重置
