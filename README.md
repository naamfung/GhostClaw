# GhostClaw

GhostClaw 是 GhostClaw 的架构重构版本——一个基于 LLM（大语言模型）的多渠道智能 AI Agent 框架，使用 Go 语言开发。GhostClaw 在 GhostClaw 的基础上进行了核心架构升级，采用全局单会话模型（GlobalSession）和 GORM/SQLite 数据库持久化，支持命令行、Web 网页、邮件及多种聊天应用交互，所有渠道共享同一个对话上下文。

## 核心特性

- **全局单会话模型**：所有渠道（CLI、WebSocket、Telegram、Discord、Slack、IRC 等）共享同一个 `GlobalSession`，无论从哪个渠道发来消息，模型看到的都是同一份对话历史
- **GORM/SQLite 数据持久化**：记忆系统从文件存储迁移到 GORM/SQLite 数据库（`ghostclaw.db`），包含 `Memories`、`Sessions`、`Experiences` 三张表，支持高效查询与持久化
- **多渠道支持**：Telegram、Discord、Slack、飞书、IRC、Webhook、XMPP、Matrix（按需编译），加上 CLI、WebSocket、HTTP、Email（内置）
- **多模型兼容**：支持 OpenAI、Anthropic、Ollama 等标准接口
- **流式输出**：实时显示模型响应，提供丝滑交互体验
- **跨平台**：支持 Linux（amd64/arm64）、macOS（Intel/Apple Silicon）、Windows、FreeBSD、GhostBSD、LoongArch 龙芯
- **结构化记忆**：基于 GORM 数据库的统一记忆系统，支持分类存储、评分排序、经验学习

## 📱 多渠道支持

GhostClaw 支持多达 12 种交互渠道，采用按需编译机制。所有渠道共享全局会话（GlobalSession），实现无缝的多渠道对话体验。

### 支持的平台

| 平台 | SDK | 编译方式 | 特性 |
|------|-----|----------|------|
| **CLI** | readline | 内置 | 命令行交互，支持 `-p` 调试模式 |
| **WebSocket** | gorilla | 内置 | 实时流式输出，WebUI 后端 |
| **HTTP** | net/http | 内置 | REST API，配置管理 |
| **Email** | go-imap | 内置 | IMAP 收信，SMTP 发信 |
| **Telegram** | telebot.v3 | 可选 | 流式响应、群组策略、权限控制 |
| **Discord** | Gateway WS | 可选 | 心跳机制、群组策略、Typing 指示 |
| **Slack** | Socket Mode | 可选 | 线程回复、表情反应、Markdown |
| **飞书/Lark** | HTTP API | 可选 | 消息卡片、表情反应、Token 自动刷新 |
| **IRC** | go-ircevent | 可选 | IRC 协议原生支持 |
| **Webhook** | net/http | 可选 | 通用 Webhook 接入 |
| **XMPP** | melodic | 可选 | XMPP/Jabber 协议支持 |
| **Matrix** | mautrix | 可选 | Matrix 去中心化协议支持 |

### 构建选项

```bash
# 默认构建（CLI + HTTP + WebSocket + Email）
./build.sh

# 启用单个渠道
ENABLE_TELEGRAM=1 ./build.sh
ENABLE_DISCORD=1 ./build.sh
ENABLE_SLACK=1 ./build.sh
ENABLE_FEISHU=1 ./build.sh
ENABLE_IRC=1 ./build.sh
ENABLE_WEBHOOK=1 ./build.sh
ENABLE_XMPP=1 ./build.sh
ENABLE_MATRIX=1 ./build.sh

# 启用所有渠道
ENABLE_ALL_CHANNELS=1 ./build.sh
```

### 配置示例

```toml
# Telegram 配置
telegram_config:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  allow_from: ["*"]
  group_policy: "mention"
  streaming: true

# Discord 配置
discord_config:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  allow_from: ["*"]
  group_policy: "mention"

# Slack 配置
slack_config:
  enabled: true
  bot_token: "xoxb-xxx"
  app_token: "xapp-xxx"
  reply_in_thread: true

# 飞书配置
feishu_config:
  enabled: true
  app_id: "cli_xxx"
  app_secret: "xxx"
  group_policy: "mention"
```

### 群组策略说明

- `open`：响应群组中所有消息
- `mention`：仅响应 @提及 机器人的消息

> 📖 **详细配置指南**：请参阅 [CHANNELS_GUIDE.md](./CHANNELS_GUIDE.md) 获取各平台的完整配置步骤。

## 🔄 全局单会话模型

GhostClaw 采用全局单会话模型（GlobalSession），这是与旧版 GhostClaw 最核心的架构差异。

### 架构对比

| 特性 | GhostClaw（旧版） | GhostClaw（新版） |
|------|----------------|-------------------|
| 会话模型 | 多会话（WebSessionManager） | 全局单会话（GlobalSession） |
| 渠道隔离 | 每渠道独立会话 | 所有渠道共享同一会话 |
| 记忆存储 | 文件系统（memory.toon） | GORM/SQLite 数据库 |
| 会话管理器 | ChannelSessionManager | GlobalSession 单例 |
| 渠道路由 | ProcessChannelMessage | 各渠道内置 processUserInput |

### GlobalSession 工作原理

```
┌──────────────────────────────────────────────────────────────┐
│                     GlobalSession                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ History  │  │ OutputQ  │  │  Task    │  │ Persist  │    │
│  │ []Message│  │ StreamCh │  │ Control  │  │ AutoSave │    │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘    │
└──────────────────────┬───────────────────────────────────────┘
                       │ 共享
         ┌─────────────┼─────────────┐
         ▼             ▼             ▼
    ┌─────────┐  ┌──────────┐  ┌─────────┐
    │  Telegram│  │ Discord  │  │   CLI   │
    └─────────┘  └──────────┘  └─────────┘
```

GlobalSession 是一个线程安全的单例对象，所有渠道的消息都写入同一份对话历史。每个渠道的消息通过 `AddToHistory()` 统一管理，任务并发控制通过 `TryStartTask()` / `SetTaskRunning()` 实现，流式输出通过 `OutputQueue` 分发到 WebSocket 等消费端。会话历史通过 `SessionPersistManager` 自动持久化到 GORM 数据库。

### 核心代码结构

```go
// session.go
type GlobalSession struct {
    ID          string
    History     []Message        // 全局共享的对话历史
    TaskRunning bool             // 任务并发控制
    TaskCtx     context.Context  // 当前任务上下文
    OutputQueue chan StreamChunk // 流式输出队列
    // ...
}
```

## 🧠 GORM/SQLite 记忆系统

GhostClaw 的记忆系统基于 GORM ORM 和 SQLite 数据库，所有记忆数据存储在 `ghostclaw.db` 文件中，通过三张核心表实现结构化存储。

### 数据库表结构

| 表名 | 说明 | 关键字段 |
|------|------|----------|
| `Memories` | 结构化记忆 | ID, Category, Scope, Key, Value, Tags, Score |
| `Sessions` | 会话记录 | ID, SessionKey, StartTime, Summary, Channel |
| `Experiences` | 经验学习 | ID, SessionID, TaskDesc, Actions, Result, Score |

### Memories 表详解

记忆按分类（Category）和范围（Scope）组织，支持评分排序（Score）和访问计数（AccessCnt），使高频使用的记忆更容易被检索到。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Category` | string | 分类：`preference`（偏好）、`fact`（事实）、`project`（项目）、`skill`（技能）、`context`（上下文） |
| `Scope` | string | 范围：`user`（用户级）、`global`（全局） |
| `Key` | string | 记忆键名，用于精确查询 |
| `Value` | string | 记忆内容 |
| `Tags` | string | JSON 数组格式的标签 |
| `Score` | float64 | 评分，用于排序和权重计算 |

### 记忆工具

| 工具 | 说明 |
|------|------|
| `memory_save` | 保存记忆 |
| `memory_recall` | 检索记忆 |
| `memory_forget` | 删除记忆 |
| `memory_list` | 列出记忆 |

### 记忆整合器

MemoryConsolidator 在对话过程中自动整合记忆，当对话达到 token 预算的 70% 时自动触发，调用 LLM 分析对话内容，提取重要信息更新数据库中的记忆条目。

> 📖 **详细说明**：请参阅 [MEMORY_SYSTEM.md](./MEMORY_SYSTEM.md) 了解记忆系统的完整架构。

## 💾 会话持久化

GhostClaw 通过 `SessionPersistManager` 实现会话持久化，所有渠道共享统一的会话管理系统。

### 会话命令

| 命令 | 说明 |
|------|------|
| `/save [描述]` | 保存当前会话 |
| `/load [会话ID]` | 加载指定会话 |
| `/session` | 显示当前会话信息 |
| `/session list` | 列出所有保存的会话 |
| `/new` | 创建新会话 |
| `/reset` | 重置会话 |

### 自动保存

系统在每次消息更新后异步自动保存会话历史，确保数据不丢失。

## 🎭 角色系统

GhostClaw 拥有可切换的"灵魂"系统，支持不同角色设定，可用于编程、写作、教学等多种场景。

### 预置角色

| 角色 | 图标 | 说明 |
|------|------|------|
| **程序员** | 💻 | 专业编程助手（默认） |
| **小说家** | ✍️ | 文学创作，构建故事世界 |
| **编剧** | 🎬 | 影视剧本创作 |
| **导演** | 🎥 | 视听语言，镜头设计 |
| **翻译官** | 🌐 | 多语言专业翻译 |
| **教师** | 👨‍🏫 | 教育辅导，知识传授 |

### 角色切换命令

```
/role              显示当前角色
/role list         列出所有可用角色
/role show <name>  显示角色详情
/role <name>       切换到指定角色
```

### 自动演绎系统

在小说创作等场景中，可以开启自动演绎模式：

```
/stage auto on              开启自动演绎（导演模式）
/stage auto off             关闭自动演绎
/stage auto pause           暂停自动演绎
/stage auto resume          恢复自动演绎
/stage                      查看当前场景状态
/next                       手动触发下一角色
```

## 🎯 技能系统

技能是用 Markdown 格式定义的能力模板，比角色更轻量，可以被不同角色共享使用。

### 技能命令

| 命令 | 说明 |
|------|------|
| `/skill` | 列出所有可用技能 |
| `/skill <技能名>` | 激活指定技能 |
| `/skill show <技能名>` | 显示技能详情 |
| `/skill create <名称>` | 创建新技能模板 |
| `/skill delete <名称>` | 删除技能 |
| `/skill reload` | 重新加载所有技能 |
| `/skill search <关键词>` | 搜索技能 |

### 预置技能

| 技能 | 说明 |
|------|------|
| `code_review` | 专业代码审查 |
| `translation` | 多语言翻译 |
| `creative_writing` | 创意写作 |
| `document_summary` | 文档总结 |
| `explanation` | 概念解释 |
| `decision_analysis` | 决策分析 |
| `learning_coach` | 学习辅导 |
| `debugging` | 调试排错 |

## 🔌 插件系统

基于 Lua 的动态插件机制，模型可自行创建与管理插件。

### 插件工具

| 工具 | 说明 |
|------|------|
| `plugin_list` | 列出插件 |
| `plugin_create` | 创建插件 |
| `plugin_load` | 加载插件 |
| `plugin_call` | 调用插件函数 |
| `plugin_delete` | 删除插件 |

### 预置插件

| 插件 | 说明 |
|------|------|
| `weather` | 天气查询（Open-Meteo API） |
| `exchange` | 汇率查询（ExchangeRate-API） |

### 插件开发

GhostClaw 提供完整的 Lua API 供插件调用：

```lua
-- 日志输出
ghostclaw.log("info", "消息")

-- HTTP 请求
local resp = ghostclaw.http_get(url)
local resp = ghostclaw.http_post(url, body, content_type)

-- JSON 处理
local json_str = ghostclaw.json_encode(table)
local data = ghostclaw.json_decode(json_string)

-- 文件操作
local content, err = ghostclaw.read_file(path)
local ok, err = ghostclaw.write_file(path, content)

-- 时间函数
local ts = ghostclaw.time()

-- 字符串处理
local parts = ghostclaw.split(str, separator)

-- 其他
local hash = ghostclaw.hash("sha256", data)
local uuid = ghostclaw.uuid()
```

## 🐚 Shell 工具系统

GhostClaw 提供两种 Shell 执行工具，根据任务性质自动选择：

### shell - 同步执行（快速命令）

适用于快速操作，有超时保护（默认 60 秒）：

```
✅ 适用场景：ls, cat, mkdir, rm, cp, mv, grep, find, echo, pwd, date, git status
❌ 不适用：ssh, scp, rsync, apt install, make, docker build, 大文件下载
```

### shell_delayed - 后台执行（长时间任务）

适用于长时间运行的任务，无超时限制：

```
✅ 适用场景：ssh, scp, rsync, sftp, apt/yum install, make, docker build,
            系统更新, 大文件传输, 远程备份, 长时间脚本
❌ 不适用：快速命令（应使用 shell）
```

### 后台任务管理工具

| 工具 | 说明 |
|------|------|
| `shell_delayed` | 在后台执行长时间命令 |
| `shell_delayed_check` | 检查后台任务状态和结果 |
| `shell_delayed_terminate` | 终止后台任务 |
| `shell_delayed_list` | 列出所有后台任务 |
| `shell_delayed_wait` | 延长等待唤醒时间 |
| `shell_delayed_remove` | 移除已完成的任务记录 |

## ⏰ 定时任务

支持模型自主安排未来任务。

| 工具 | 说明 |
|------|------|
| `cron_add` | 添加任务 |
| `cron_remove` | 删除任务 |
| `cron_list` | 列出任务 |
| `cron_status` | 任务状态 |

## 内置工具

### 文件操作
- `read_file_line` / `write_file_line`：读写文件行
- `read_all_lines` / `write_all_lines`：读写整个文件
- `text_search`：文本搜索（支持正则表达式）
- `text_replace`：文本替换（类 sed 功能）

### 系统交互
- `shell`：执行系统命令（同步，有超时）
- `shell_delayed`：后台执行命令（异步，无超时）
- `todo`：管理待办事项

### 网络功能
- `search`：搜索引擎
- `visit`：访问网页
- `download`：下载文件

## 🌐 浏览器自动化工具

GhostClaw 内置了完整的浏览器自动化能力，基于 Rod 库实现，共 **32 个** 浏览器工具，涵盖基础操作、交互、等待、导航、内容提取、截图/PDF、高级功能等类别。

> 📖 **详细指南**：请参阅 [BROWSER_TOOLS_GUIDE.md](./BROWSER_TOOLS_GUIDE.md) 获取完整的浏览器工具使用文档。

## 快速开始

### 本地构建

```bash
git clone https://github.com/naamfung/GhostClaw.git --depth=1
cd GhostClaw
./build.sh
```

### Docker 跨平台构建

支持在任意平台编译出多平台版本：

| 目标 | 输出文件 | 说明 |
|------|----------|------|
| `linux-amd64` | `ghostclaw-linux-amd64` | Linux x86_64 (glibc) |
| `linux-arm64` | `ghostclaw-linux-arm64` | Linux ARM64 (树莓派等) |
| `alpine-amd64` | `ghostclaw-alpine-amd64` | Alpine Linux (静态链接) |
| `alpine-arm64` | `ghostclaw-alpine-arm64` | Alpine ARM64 (静态链接) |
| `loong64` | `ghostclaw-linux-loong64` | LoongArch 龙芯 |
| `darwin-amd64` | `ghostclaw-darwin-amd64` | macOS Intel |
| `darwin-arm64` | `ghostclaw-darwin-arm64` | macOS Apple Silicon |
| `windows-amd64` | `ghostclaw-windows-amd64.exe` | Windows |
| `freebsd-amd64` | `ghostclaw-freebsd-amd64` | FreeBSD |
| `ghostbsd-amd64` | `ghostclaw-ghostbsd-amd64` | GhostBSD |

```bash
./docker-build.sh linux-amd64
./docker-build.sh darwin-arm64
./docker-build.sh all --cn
```

### Docker Compose 方式

```bash
docker compose build linux-amd64
docker compose up runtime
```

首次运行自动生成 `config.toon` 配置文件。

## 项目结构

```
ghostclaw/
├── main.go               # 程序入口，初始化各组件
├── AgentLoop.go          # 核心对话循环
├── CallModel.go          # 模型 API 调用（含消息压缩）
├── const.go              # 常量定义，基础系统提示
├── session.go            # GlobalSession 全局单会话
├── session_channel.go    # SessionChannel 输出桥接
├── session_persist.go    # 会话持久化管理
├── command.go            # 统一斜杠命令处理
├── db.go                 # GORM/SQLite 数据库层
├── types.go              # Message/ToolUse/Response 类型
├── config.go             # TOON 格式配置加载
├── role.go               # 角色模板管理
├── role_presets.go       # 预置角色定义
├── actor.go              # 演员实例管理
├── stage.go              # 场景管理（自动切换）
├── skill.go              # 技能管理
├── plugin.go             # 插件管理器
├── cron.go               # 定时任务管理
├── cron_executor.go      # 任务执行器
├── task_tracker.go       # 任务进度追踪
├── task_manager.go       # 后台任务管理
├── unified_memory.go     # 统一记忆系统（GORM）
├── memory_consolidator.go # 记忆整合器
├── memory_tools.go       # 记忆工具
├── heartbeat.go          # 心跳服务
├── subagent.go           # 子代理管理
├── mcp_server.go         # MCP 服务器
├── mcp_client.go         # MCP 客户端
├── mcp_tools.go          # MCP 工具注册
├── channel.go            # Channel 接口定义
├── cmd_channel.go        # 命令行通道
├── ws_channel.go         # WebSocket 通道
├── http_server.go        # HTTP 服务器
├── telegram_channel.go   # Telegram 通道（可选编译）
├── discord_channel.go    # Discord 通道（可选编译）
├── slack_channel.go      # Slack 通道（可选编译）
├── feishu_channel.go     # 飞书通道（可选编译）
├── irc_channel.go        # IRC 通道（可选编译）
├── webhook_channel.go    # Webhook 通道（可选编译）
├── xmpp_channel.go       # XMPP 通道（可选编译）
├── matrix_channel.go     # Matrix 通道（可选编译）
├── email_channel.go      # Email 通道
├── browser_tools.go      # 浏览器工具
├── browser_session.go    # 浏览器会话管理
├── shell.go              # Shell 执行
├── file.go               # 文件操作
├── getTools.go           # 工具定义
├── services.go           # 外部服务（搜索、访问）
├── security.go           # 安全策略（SSRF 防护等）
├── auth.go               # 认证管理
├── version.go            # 版本信息与启动横幅
├── tools_alias.go        # 工具别名
├── profile_loader.go     # Profile 热加载
├── messagebus.go         # 消息总线
├── hooks.go              # Hook 管理
├── embed.go              # 嵌入资源
├── skills/               # 技能定义目录
├── plugins/              # 插件目录
│   ├── weather/weather.lua
│   └── exchange/exchange.lua
├── roles/                # 角色定义目录
│   ├── coder.md
│   ├── novelist.md
│   └── custom/           # 自定义角色
├── profiles/             # 系统提示 Profile
├── webui/                # SvelteKit 前端
└── ghostclaw.db          # SQLite 数据库（运行时生成）
```

## 文档

| 文档 | 说明 |
|------|------|
| [README.md](./README.md) | 项目概述（本文档） |
| [USER_GUIDE.md](./USER_GUIDE.md) | 用户使用指南 |
| [SYSTEM_DESIGN.md](./SYSTEM_DESIGN.md) | 系统架构设计 |
| [MEMORY_SYSTEM.md](./MEMORY_SYSTEM.md) | 记忆系统详解 |
| [CHANNELS_GUIDE.md](./CHANNELS_GUIDE.md) | 渠道配置指南 |
| [BROWSER_TOOLS_GUIDE.md](./BROWSER_TOOLS_GUIDE.md) | 浏览器工具完整指南 |
| [VERSION.md](./VERSION.md) | 版本更新日志 |

## 许可证

Apache License Version 2.0

---

## ⚠️ 安全警告

**重要提示：GhostClaw 的安全策略依赖外部机制保障**

本程序的安全设计原则：

1. **内部安全机制有限**：程序仅包含基础的「危险命令拦截」功能和 SSRF 防护机制，用于过滤 `rm -rf /`、`mkfs`、`dd if=` 等极端危险命令。此机制**不可作为唯一的安全防线**。

2. **外部安全机制必需**：在生产环境中运行 GhostClaw 时，**必须**配合以下安全措施：
   - **容器隔离**：使用 Docker、Podman 等容器技术
   - **监狱机制**：使用 chroot、jail、namespace 隔离
   - **权限限制**：以非 root 用户运行，使用 sudo 限制
   - **网络隔离**：限制网络访问，使用防火墙规则
   - **资源限制**：使用 cgroups 限制 CPU/内存使用

3. **安全责任**：
   - 不要在裸机环境直接运行 GhostClaw 处理不可信输入
   - 不要授予 GhostClaw 超过必要的系统权限
   - 定期审计 GhostClaw 的操作日志
   - 敏感数据应通过配置文件管理，避免硬编码

4. **默认配置**：程序默认关闭危险命令拦截（`block_dangerous_commands = false`），用户可通过配置开启此保护。开启后，将拦截 `rm -rf /`、`mkfs`、`dd if=` 等极端危险命令。

**安全是一个多层次的问题，GhostClaw 专注于功能实现，安全防护应通过外部机制完成。**
