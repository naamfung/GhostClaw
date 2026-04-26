# GhostClaw

GhostClaw 是一个基于 LLM 的多渠道 AI Agent 框架，使用 Go 语言开发。它通过命令行、Web 网页、邮件及多种聊天应用与模型交互，所有渠道共享同一个对话上下文。无论是日常助手、代码开发、创意写作还是自动化运维，GhostClaw 都能胜任。

## 目录

- [安装与启动](#安装与启动)
- [首次配置](#首次配置)
- [启动模式](#启动模式)
- [WebUI 界面](#webui-界面)
- [核心功能一览](#核心功能一览)
- [角色系统](#角色系统)
- [技能系统](#技能系统)
- [多角色协作](#多角色协作)
- [子代理（Spawn）](#子代理spawn)
- [规划模式（Plan Mode）](#规划模式plan-mode)
- [记忆系统](#记忆系统)
- [Shell 与任务管理](#shell-与任务管理)
- [浏览器自动化](#浏览器自动化)
- [SSH 远程执行](#ssh-远程执行)
- [定时任务](#定时任务)
- [插件系统](#插件系统)
- [MCP 协议](#mcp-协议)
- [API Key 池与故障转移](#api-key-池与故障转移)
- [多模型配置](#多模型配置)
- [多渠道接入](#多渠道接入)
- [Profile 系统](#profile-系统)
- [安全机制](#安全机制)
- [Docker 构建](#docker-构建)
- [许可证](#许可证)

---

## 安装与启动

### 本地构建

需要 Go 及 Node.js 环境：

```bash
# 克隆仓库
git clone https://github.com/naamfung/GhostClaw.git --depth=1
cd GhostClaw

# 构建并运行
./build.sh
./ghostclaw
```

### Docker 构建

支持跨平台编译：

```bash
# 构建指定平台
./docker-build.sh linux-amd64
./docker-build.sh darwin-arm64
./docker-build.sh windows-amd64

# 构建所有平台（国内用户加 --cn 加速）
./docker-build.sh all --cn

# Docker Compose 运行
docker compose build linux-amd64
docker compose up runtime
```

支持的目标平台：`linux-amd64`、`linux-arm64`、`alpine-amd64`、`alpine-arm64`、`loong64`（龙芯）、`darwin-amd64`、`darwin-arm64`、`windows-amd64`、`freebsd-amd64`、`ghostbsd-amd64`。

---

## 首次配置

首次运行时，如果未检测到 API 配置，程序会自动进入**交互式配置向导**，引导你完成以下设置：

1. 选择 API 类型（OpenAI / Anthropic / Ollama / 自定义）
2. 设置 Base URL（如 `https://api.deepseek.com/v1`）
3. 输入 API Key
4. 选择模型 ID（如 `deepseek-chat`）

向导完成后自动生成 `config.toon` 配置文件。你也可以跳过向导，手动编辑配置：

```yaml
Models:
  "deepseek-chat":
    ModelBase:
      Name: deepseek-chat
      APIType: "openai"
      BaseURL: "https://api.deepseek.com/v1"
      APIKey: "your-api-key-here"
      Model: deepseek-chat
      Temperature: 0.7
      MaxTokens: 4096
      Stream: true
      IsDefault: true
```

> **提示**：API Key 支持环境变量引用，格式为 `${VAR_NAME}`，例如 `APIKey: "${DEEPSEEK_API_KEY}"`。

GhostClaw 内置了 20+ 家主流 LLM 提供商的自动识别（包括 OpenAI、Anthropic、DeepSeek、Google Gemini、通义千问、智谱 GLM、Moonshot、Groq、Ollama 本地部署等），输入模型名或 API Key 时会自动匹配对应的 API 类型及 Base URL，无需手动查阅文档。

---

## 启动模式

```bash
./ghostclaw              # Log 模式（后台运行，终端显示日志）
./ghostclaw --repl        # REPL 模式（直接与模型对话）
./ghostclaw -p "你好"     # 调试模式（输出后自动退出）
./ghostclaw --debug       # 启用调试日志
```

在 Log 模式下，按 `/` 键即可随时切换到 REPL 对话模式。在 REPL 模式中，输入 `/quit` 可切回 Log 模式，输入 `/exit` 彻底退出。

---

## WebUI 界面

程序启动后，浏览器打开 `http://localhost:10086` 即可使用 Web 界面。WebUI 提供完整的对话、模型切换、角色选择、技能管理、MCP 服务器配置等功能。

---

## 核心功能一览

| 功能 | 说明 |
|------|------|
| **多渠道统一会话** | CLI、Web、Telegram、Discord、Slack、飞书等 12 种渠道共享上下文 |
| **角色系统** | 预置 + 自定义角色，不同场景切换不同人格及工具权限 |
| **技能系统** | 轻量级能力模板，可跨角色共享 |
| **多角色协作** | 多个角色自动轮流对话，适用于群像创作 |
| **子代理（Spawn）** | 后台并行执行独立任务，结果自动回传 |
| **规划模式** | 5 阶段结构化任务分解，从探索到执行 |
| **记忆系统** | 自动整合长对话，重要信息持久化存储 |
| **Shell 工具** | 同步/异步/智能三种执行模式，后台任务自动唤醒 |
| **浏览器自动化** | 27 个内置工具，支持网页操作、截图、搜索 |
| **SSH 远程执行** | 连接远程服务器执行命令，支持同步/异步 |
| **定时任务** | 模型自主安排 cron 任务 |
| **插件系统** | Lua 动态插件，模型可自行创建管理 |
| **MCP 协议** | 同时作为 MCP 服务器及客户端 |
| **API Key 池** | 多 Key 轮转、故障转移、速率限制感知 |
| **安全机制** | 危险命令拦截、重复调用检测、SSRF 防护、审计日志 |

---

## 角色系统

GhostClaw 通过角色来定义 AI 的身份、性格、说话风格及工具权限。

### 预置角色

| 角色 | 标识 | 适用场景 |
|------|------|----------|
| 程序员 | `coder` | 编程助手，可执行系统命令（默认） |
| 小说家 | `novelist` | 文学创作 |
| 编剧 | `screenwriter` | 影视剧本 |
| 导演 | `director` | 视听语言 |
| 翻译官 | `translator` | 多语言翻译 |
| 教师 | `teacher` | 知识传授 |
| 反派 | `antagonist` | 对抗角色 |
| 叙事者 | `narrator` | 故事叙述 |
| 主角 | `protagonist` | 故事主角 |

此外还有 40+ 个自定义角色（在 `roles/custom/` 目录下），涵盖侦探、刺客、厨师、心理咨询师、投资顾问、游戏设计师等各种人设。

### 切换角色

```
/role list          # 列出所有角色
/role coder         # 切换到程序员角色
```

### 创建自定义角色

在 `roles/custom/` 目录下添加 `.md` 文件即可。角色文件支持设置身份、性格、说话风格、工具权限等。修改后无需重启，热加载生效。

---

## 技能系统

技能是轻量级的能力模板，可被不同角色共享使用。与角色不同，技能更侧重于特定任务的执行方法。

```
/skill              # 列出所有技能
/skill code_review  # 激活代码审查技能
```

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
| `file_upload` | 文件上传 |
| `toon_format` | Toon 格式处理 |
| `opencli` | OpenCLI 浏览器扩展 |
| `ghostclaw-plugin-developer` | 插件开发指南 |

技能文件位于 `skills/` 目录，每个技能包含 `SKILL.md` 描述文件，部分技能还带有脚本、模板及参考文档。

---

## 多角色协作

适用于小说创作、群像 RP 等需要多个角色互动的场景：

```
GhostClaw /> /stage auto on        # 开启自动演绎
GhostClaw /> /actor hero_lin       # 添加角色「林风」
GhostClaw /> /actor villain_mozun  # 添加角色「魔尊」
GhostClaw /> 开始写林风与魔尊的对决
```

系统会自动在不同角色间切换，每个角色使用独立的人格及风格进行对话。使用 `/next` 可手动触发下一个角色发言。

---

## 子代理（Spawn）

子代理允许模型在后台并行执行独立任务，适合信息收集、代码分析、多维度探索等场景。

**使用方式**：模型会自动调用 `spawn` 工具创建子代理。你也可以通过 WebUI 管理子代理任务。

| 工具 | 说明 |
|------|------|
| `spawn` | 创建后台子代理执行任务 |
| `spawn_batch` | 批量创建多个并行子代理 |
| `spawn_check` | 检查子代理执行状态及结果 |
| `spawn_list` | 列出所有子代理任务 |
| `spawn_cancel` | 取消子代理任务 |

子代理支持嵌套深度限制（默认最多 2 层），防止单个任务无限递归。子代理完成任务后，结果会自动回传到主会话。

**与规划模式搭配**：在 Plan Mode 的探索阶段，可以 spawn 多个子代理并行探索不同模块，大幅提升效率。

---

## 规划模式（Plan Mode）

规划模式将复杂任务分解为 5 个阶段，由程序状态机严格控制：

| 阶段 | 名称 | 允许的操作 |
|------|------|------------|
| Phase 1 | 初始理解（探索） | 只读搜索、文件读取、spawn 子代理 |
| Phase 2 | 方案设计 | 草拟计划文件 |
| Phase 3 | 审查验证 | 只读确认（不能修改计划） |
| Phase 4 | 编写最终计划 | 编写正式实施计划 |
| Phase 5 | 退出 | 自动完成，计划注入会话历史 |

在 `config.toon` 中启用：

```toml
Tools:
  PlanModeEnabled: true
```

每个阶段有独立的时间限制及工具权限，模型需要调用 `next_phase` 推进到下一阶段。退出后，计划内容作为执行指引注入会话，模型开始使用完整工具集执行。

**适用场景**：代码库重构、功能开发、复杂任务分析等需要先理解再行动的场景。

---

## 记忆系统

GhostClaw 自动管理对话记忆，无需手动干预。当对话达到一定长度时，系统会自动整合重要信息并持久化存储。

可调整的参数：

```toml
Memory:
  MinMessagesToConsolidate: 10    # 最小整合消息数
  ConsolidationRatio: 0.7         # token 占比达 70% 时触发整合
  ContextWindowTokens: 4096       # 上下文窗口大小（0=跟随模型）
```

模型可以通过以下工具主动操作记忆：

| 工具 | 说明 |
|------|------|
| `memory_save` | 保存记忆 |
| `memory_recall` | 检索记忆 |
| `memory_forget` | 删除记忆 |
| `memory_list` | 列出记忆 |

---

## Shell 与任务管理

GhostClaw 提供三种 Shell 执行模式，覆盖不同场景：

| 工具 | 说明 | 适用场景 |
|------|------|----------|
| `shell` | 同步执行（有超时） | 快速命令：ls、cat、grep、git status |
| `shell_delayed` | 后台执行（无超时） | 长时任务：编译、部署、下载 |
| `smart_shell` | 智能判断执行方式 | 无需手动选择，自动匹配 |

后台任务管理：

| 工具 | 说明 |
|------|------|
| `shell_delayed_check` | 检查后台任务状态 |
| `shell_delayed_terminate` | 终止后台任务 |
| `shell_delayed_list` | 列出所有后台任务 |

后台任务完成后会自动唤醒模型，通知其查看结果。可在 `config.toon` 中调整超时：

```toml
Timeout:
  Shell: 60      # Shell 命令超时（秒）
  HTTP: 120      # HTTP 请求超时
  Plugin: 30     # 插件调用超时
  Browser: 30    # 浏览器操作超时
```

---

## 浏览器自动化

内置 27 个浏览器工具，支持网页搜索、访问、点击、填表、截图、Cookie 管理等操作。

```toml
BrowserConfig:
  Headless: true              # 无头模式
  DisableGPU: true            # 禁用 GPU
  NoSandbox: false            # 沙盒模式
  DisableBrowserTools: false  # 启用浏览器工具
  UserMode: false             # 使用已有浏览器会话
```

启用后，模型可以自主浏览网页、提取信息、填写表单等。`UserMode: true` 时会连接到已有的浏览器实例，适用于需要登录态的场景。

---

## SSH 远程执行

支持连接远程服务器并在其上执行命令：

| 工具 | 说明 |
|------|------|
| `ssh_connect` | 建立 SSH 连接 |
| `ssh_exec` | 在远程服务器执行命令（支持同步/异步） |
| `ssh_list` | 列出活跃的 SSH 会话 |
| `ssh_close` | 关闭 SSH 会话 |

异步 SSH 命令完成后会自动通知模型，适用于远程部署、运维等场景。**与定时任务搭配**：可以安排定时 SSH 巡检或备份任务。

---

## 定时任务

模型可以自主安排定时任务，无需人工干预：

| 工具 | 说明 |
|------|------|
| `cron_add` | 添加定时任务 |
| `cron_remove` | 删除任务 |
| `cron_list` | 列出任务 |
| `cron_status` | 查看任务状态 |

```toml
CronConfig:
  MaxConcurrent: 5    # 最大并发任务数
```

**使用场景**：定时提醒、定期数据采集、自动化巡检等。**与浏览器及 SSH 搭配**：可以实现定时网页监控、远程服务器健康检查等。

---

## 插件系统

基于 Lua 的动态插件机制，模型可以自行创建及管理插件。

预置插件：

| 插件 | 说明 |
|------|------|
| `weather` | 天气查询（Open-Meteo API） |
| `exchange` | 汇率查询 |
| `temp_uploader` | 临时文件上传 |

插件文件位于 `plugins/` 目录，以 `.lua` 为扩展名。模型可以通过工具创建新插件、调用插件功能。

---

## MCP 协议

GhostClaw 同时支持作为 MCP 服务器及 MCP 客户端。

### 作为 MCP 服务器

让外部工具（如 Claude Desktop、IDE 等）调用 GhostClaw 的能力：

```toml
MCP:
  Enabled: true
  Transport: "stdio"    # stdio / sse / http
```

在 stdio 模式下，GhostClaw 作为 MCP 服务器运行，暴露其所有工具给外部客户端使用。

### 作为 MCP 客户端

连接外部 MCP 服务器，扩展 GhostClaw 的工具集。在程序目录下创建 `mcp_servers.toon`：

```yaml
Servers:
  "my-server":
    Type: "stdio"
    Command: "node"
    Args: ["server.js"]
    Env:
      API_KEY: "xxx"
    ToolTimeout: 30
```

也支持 SSE 及 HTTP 类型的 MCP 服务器。在 WebUI 中可以可视化管理 MCP 服务器连接。

---

## API Key 池与故障转移

为单个模型配置多个 API Key，实现自动轮转及故障转移：

- 优先级调度：高优先级 Key 优先使用
- Round-Robin：同优先级内轮转
- RPM 感知：接近速率限制时自动切换
- 指数退避：失败后自动冷却（30s → 1min → 2min → ... 最大 30min）
- HTTP 429 处理：收到限流响应后立即切换到备用 Key

可通过 WebUI 管理 API Key 池，支持添加、移除、设置优先级及速率限制。

---

## 多模型配置

在 `config.toon` 中配置多个模型，通过 WebUI 或命令切换：

```yaml
Models:
  "deepseek-chat":
    ModelBase:
      Name: deepseek-chat
      APIType: "openai"
      BaseURL: "https://api.deepseek.com/v1"
      APIKey: "${DEEPSEEK_API_KEY}"
      Model: deepseek-chat
      Temperature: 0.7
      MaxTokens: 4096
      Stream: true
      IsDefault: true

  "gpt-4":
    ModelBase:
      Name: gpt-4
      APIType: "openai"
      BaseURL: "https://api.openai.com/v1"
      APIKey: "${OPENAI_API_KEY}"
      Model: gpt-4
      Description: "用于复杂推理任务"
```

子代理也可以使用不同的模型或 API Key，在 spawn 时通过参数覆盖。

---

## 多渠道接入

GhostClaw 支持多达 12 种交互渠道，所有渠道共享同一个全局会话。

| 渠道 | 编译方式 | 说明 |
|------|----------|------|
| CLI | 内置 | 命令行交互 |
| WebSocket | 内置 | WebUI 后端 |
| HTTP | 内置 | REST API |
| Email | 内置 | IMAP 收信 + SMTP 发信 |
| Telegram | 可选 | 流式响应、群组策略 |
| Discord | 可选 | 心跳机制、Typing 指示 |
| Slack | 可选 | 线程回复、表情反应 |
| 飞书 | 可选 | 消息卡片、Token 自动刷新 |
| IRC | 可选 | 原生协议支持 |
| Webhook | 可选 | 通用接口 |
| XMPP | 可选 | Jabber 协议 |
| Matrix | 可选 | 去中心化协议 |

### 启用扩展渠道

```bash
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

### 渠道配置示例

```toml
# Telegram
TelegramConfig:
  Enabled: true
  Token: "YOUR_BOT_TOKEN"
  AllowFrom: ["*"]
  GroupPolicy: "mention"
  Streaming: true

# Discord
DiscordConfig:
  Enabled: true
  Token: "YOUR_BOT_TOKEN"
  AllowFrom: ["*"]
  GroupPolicy: "mention"

# Slack
SlackConfig:
  Enabled: true
  BotToken: "xoxb-xxx"
  AppToken: "xapp-xxx"
  ReplyInThread: true

# 飞书
FeishuConfig:
  Enabled: true
  AppID: "cli_xxx"
  AppSecret: "xxx"
  GroupPolicy: "mention"

# 邮件
EmailConfig:
  IMAPServer: "imap.example.com"
  IMAPPort: 993
  IMAPUseTLS: true
  IMAPUser: "bot@example.com"
  IMAPPassword: "xxx"
  SMTPServer: "smtp.example.com"
  SMTPPort: 587
  SMTPUseTLS: true
  SMTPUser: "bot@example.com"
  SMTPPassword: "xxx"
  PollInterval: 60

# XMPP
XMPPConfig:
  Enabled: true
  Server: "example.com"
  Username: "user@example.com"
  Password: "YOUR-PASSWORD"
  Rooms: ["room@conference.example.com"]
  Nick: "GhostClaw"

# Matrix
MatrixConfig:
  Enabled: true
  HomeserverURL: "https://matrix.org"
  UserID: "@user:matrix.org"
  AccessToken: "YOUR-ACCESS-TOKEN"
  Rooms: ["!roomid:matrix.org"]

# IRC
IRCConfig:
  Enabled: true
  Server: "irc.libera.chat"
  Port: 6697
  Nick: "GhostClaw"
  Channels: ["#ghostclaw"]
  UseTLS: true

# Webhook
WebhookConfig:
  Enabled: true
  Path: "/webhook"
  Secret: "your-webhook-secret"
```

群组策略：`open`（响应所有消息）或 `mention`（仅响应 @提及，推荐）。

---

## Profile 系统

通过 `profiles/` 目录下的文件定义全局行为规范，所有角色共享：

| 文件 | 说明 |
|------|------|
| `SOUL.md` | 核心价值观（诚实、隐私保护、法律边界等） |
| `AGENT.md` | 工作习惯、任务分析策略 |
| `USER.md` | 用户档案（首次对话时自动收集） |
| `TOOLS.md` | 工具别名、环境设置 |

修改 Profile 文件后即时生效。也可以配置加载模式：

```toml
Profile:
  ReloadMode: "per_session"    # "once" 或 "per_session"
```

---

## 安全机制

### 危险命令拦截

```toml
# 在模型配置中启用
BlockDangerousCommands: true
```

内置 Hook 系统会自动检测 `rm -rf /`、`dd if=` 等危险命令并发出警告。

### SSRF 防护

```toml
Security:
  EnableSSRFProtection: true
  AllowPrivateIPs: false
  AllowedHosts: ["example.com"]
  BlockedHosts: ["malicious.com"]
```

### 认证保护

为 WebUI 启用密码保护：

```toml
Auth:
  Enabled: true
  Password: "your-password"
  TokenExpiry: 86400
```

### 审计与防循环

内置三个安全 Hook 自动运行：

- **危险命令检测**：拦截高危 Shell 命令
- **重复调用检测**：当模型连续执行相同命令超过阈值时自动警告
- **审计日志**：记录所有工具调用

### 生产环境建议

- 使用 Docker 容器隔离
- 以非 root 用户运行
- 限制网络访问（防火墙规则）
- 配置 `AllowFrom` 限制可访问的用户
- 定期审计操作日志

---

## 常用命令速查

| 命令 | 说明 |
|------|------|
| `/exit` | 退出程序 |
| `/help` | 显示帮助 |
| `/role list` | 列出所有可用角色 |
| `/role <名称>` | 切换到指定角色 |
| `/skill` | 列出所有可用技能 |
| `/skill <名称>` | 激活指定技能 |
| `/save [描述]` | 保存当前会话 |
| `/load` | 列出已保存的会话 |
| `/load <会话ID>` | 加载指定会话 |
| `/session` | 显示当前会话信息 |
| `/new` | 创建新会话 |
| `/memory` | 查看记忆列表 |
| `/plan` | 查看 Plan Mode 状态 |
| `/stage auto on` | 开启自动角色切换 |
| `/next` | 手动触发下一个角色发言 |

---

## 许可证

Apache License Version 2.0
