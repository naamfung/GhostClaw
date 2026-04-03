# 版本历史

## v0.0.7 (2026-04-03)

### 反馈收集策略优化

**核心改进**：
- 改为任务完成时询问反馈，而非固定轮次
- 遵循「只问当前任务，过后不再问」的原则

**具体调整**：

1. **智能任务完成检测**：
   - 检测助手回复中的任务完成信号（"完成"、"搞定"、"解决"等）
   - 检测结论性表达（"总结"、"综上所述"、"总之"等）
   - 只有在真正完成任务时才询问反馈

2. **去重机制**：
   - 检查是否已经为当前任务询问过反馈
   - 避免重复打扰用户

3. **简化策略**：
   - 移除固定轮次和时间间隔的限制
   - 改为基于任务完成的触发机制
   - 保持最少 3 轮对话的要求

**预期效果**：
- 更精准的反馈时机
- 避免过度打扰用户
- 收集到更相关、更及时的反馈
- 提高反馈收集的质量和用户体验

## v0.0.6 (2026-04-03)

### 智能反馈收集系统

**核心设计**：
- 采用模型主动追问的方式收集用户反馈，用户无感知且自然
- 参考零数据启动时的主动询问策略，在对话中自然融入反馈收集

**实现机制**：

1. **反馈收集器 (`feedback_collector.go`)**：
   - 支持三种反馈类型：显式反馈（直接评分）、隐式反馈（从对话推断）、推断反馈（行为分析）
   - 智能评分解析：识别 "5分"、"很好"、"完美" 等多种评分表达
   - 反馈分类：自动分类为 accuracy、helpfulness、clarity、speed 等类别
   - 改进建议提取：从用户回复中提取 "不过"、"但是"、"建议" 等引导的改进意见

2. **隐式反馈信号检测**：
   - 追问信号（?）：可能表示回答不够清晰（+0.3）
   - 纠正信号（"不对"、"错误"）：表示回答有误（-0.5）
   - 感谢信号（"谢谢"、"有用"）：表示回答有帮助（+0.5）
   - 满意信号（"完美"、"正是"）：高度满意（+0.8）
   - 不满信号（"不行"、"没用"）：表示不满（-0.6）

3. **智能询问策略**：
   - 每隔 10 轮对话自动询问一次反馈
   - 最少 5 轮对话后才开始询问
   - 避免频繁打扰（间隔至少 5 分钟）
   - 反馈提示自然融入对话："作为你的助手，我希望持续改进..."

4. **数据持久化**：
   - 反馈记录保存为 JSONL 格式（`feedback.jsonl`）
   - 包含完整上下文：用户消息、助手回复、评分、改进建议
   - 支持统计分析：平均评分、反馈类型分布、每日趋势

**集成方式**：
- 在 `AgentLoop` 结尾处异步执行反馈收集
- 不影响主对话流程，后台静默处理
- 在 `main.go` 中初始化反馈收集器

**预期效果**：
- 持续收集用户满意度数据
- 识别助手表现的问题和改进点
- 为后续策略优化提供数据基础
- 实现自我进化的第一步：数据收集闭环

## v0.0.5 (2026-04-03)

### 对话注意力机制改进

**Phase 1 - 消息截断优化**：
- 改进 `AgentLoop.go` 中的截断逻辑，确保最新用户消息始终被保留
- 优化用户消息边界检测算法，优先保护最新用户消息
- 增强时间分隔标记，添加"用户最新请求"摘要，明确提示模型优先响应最新消息

**Phase 2 - 上下文压缩器**：
- 新增 `context_compressor.go`，实现"头尾保护 + 中间摘要"的压缩策略
  - 保护头部：system 消息 + 前 3 条消息
  - 保护尾部：确保最新用户消息始终在尾部
  - 中间部分：生成结构化摘要（用户目标、代理响应、工具调用）
- 集成到 `AgentLoop`，在消息截断之前使用压缩器

**Phase 3 - 记忆 Prefetch 机制**：
- 改进记忆注入逻辑，基于最新用户消息来搜索和预取相关记忆
- 使用最新用户消息作为查询关键词，从记忆系统中检索相关内容
- 确保注入的记忆与当前用户查询高度相关

**技术亮点**：
- 参考 hermes-agent 的先进设计，采用智能压缩策略
- 使用结构化摘要保留关键信息的同时减少 Token 消耗
- 确保模型始终关注用户的最新请求
- 向后兼容，保持与现有代码的兼容性

## v0.0.4 (2026-04-03)

### 代码质量优化

**修复**：
- 修复 `api_handlers.go` 中 17 个未使用的 `r *http.Request` 参数，统一替换为匿名变量 `_`
- 涉及函数：getConfig、listRoles、getRole、deleteRole、listSkills、getSkill、deleteSkill、listActors、getActor、deleteActor、listModelsAPI、getModelAPI、deleteModelAPI、setMainModelAPI、getHook、setHookEnabled、reloadHooks

**版本信息更新**：
- 更新 `version.go` 版本号从 v0.2.5 统一为 v0.0.4，保持版本序列一致性

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
