# GarClaw

GarClaw 是一个基于 LLM（大语言模型）的多前端智能助手，使用 Go 语言开发，支持命令行、Web 网页、邮件及多种聊天应用交互。

## 核心特性

- **多前端支持**：命令行（`readline`）、Web 网页（WebSocket）、邮件（IMAP/SMTP）
- **多渠道支持**：Telegram、Discord、Slack、飞书（按需编译）
- **多模型兼容**：支持 OpenAI、Anthropic、Ollama 等标准接口
- **流式输出**：实时显示模型响应，提供丝滑交互体验
- **跨平台**：自动适配 Windows 与 Unix 命令差异
- **双轨记忆**：结构化记忆 + 自动整合记忆，跨会话保持上下文

## 📱 多渠道聊天应用支持

GarClaw 支持多种聊天应用平台，采用按需加载机制：

### 支持的平台

| 平台 | SDK | 特性 |
|------|-----|------|
| **Telegram** | telebot.v3 | 流式响应、群组策略、权限控制 |
| **Discord** | Gateway WS | 心跳机制、群组策略、Typing 指示 |
| **Slack** | Socket Mode | 线程回复、表情反应、Markdown |
| **飞书/Lark** | HTTP API | 消息卡片、表情反应、Token 自动刷新 |

### 构建选项

```bash
# 默认构建（仅核心渠道：CLI、HTTP、WebSocket、Email）
./build.sh

# 启用单个渠道
ENABLE_TELEGRAM=1 ./build.sh
ENABLE_DISCORD=1 ./build.sh
ENABLE_SLACK=1 ./build.sh
ENABLE_FEISHU=1 ./build.sh

# 启用所有渠道
ENABLE_ALL_CHANNELS=1 ./build.sh
```

### 配置示例

```toml
# Telegram 配置
telegram_config:
  enabled: true
  token: "YOUR_BOT_TOKEN"
  allow_from: ["*"]           # 或 ["123456789", "username"]
  group_policy: "mention"     # "open" 或 "mention"
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
  react_emoji: "eyes"
  done_emoji: "white_check_mark"

# 飞书配置
feishu_config:
  enabled: true
  app_id: "cli_xxx"
  app_secret: "xxx"
  group_policy: "mention"
  reply_to_message: true
```

### 群组策略说明

- `open`：响应群组中所有消息
- `mention`：仅响应 @提及 机器人的消息

> 📖 **详细配置指南**：请参阅 [CHANNELS_GUIDE.md](./CHANNELS_GUIDE.md) 获取各平台的完整配置步骤。

## 🧠 双轨记忆系统

GarClaw 采用创新的双轨记忆系统，两套系统协作提供完整的记忆能力：

### 系统一：结构化记忆（memory.toon）

| 特性 | 说明 |
|------|------|
| **存储格式** | Key-Value 键值对 |
| **使用方式** | 主动工具调用 |
| **适用场景** | 精确存储用户偏好、事实信息 |

**记忆工具**：

| 工具 | 说明 |
|------|------|
| `memory_save` | 保存记忆 |
| `memory_recall` | 检索记忆 |
| `memory_forget` | 删除记忆 |
| `memory_list` | 列出记忆 |

**记忆分类**：`preference`（偏好）、`fact`（事实）、`project`（项目）、`skill`（技能）、`context`（上下文）

### 系统二：两层记忆系统（MEMORY.md + HISTORY.md）

| 特性 | 说明 |
|------|------|
| **存储格式** | Markdown 文本 |
| **使用方式** | 系统自动整合 |
| **适用场景** | 长期记忆、会话历史摘要 |

**自动整合机制**：
- 当对话达到 token 预算的 70% 时自动触发
- 调用 LLM 分析对话内容
- 提取重要信息更新长期记忆
- 生成会话摘要记录历史

### 记忆系统协作

```
┌────────────────────────────────────────────────────────┐
│                    memory/ 目录                         │
├────────────────────────────────────────────────────────┤
│  memory.toon    ← 结构化键值对（主动存储）              │
│  MEMORY.md      ← 长期记忆（自动整合）                  │
│  HISTORY.md     ← 会话历史摘要（自动整合）              │
└────────────────────────────────────────────────────────┘
```

> 📖 **详细说明**：请参阅 [MEMORY_SYSTEM.md](./MEMORY_SYSTEM.md) 了解记忆系统的完整架构。

## 💾 会话持久化

GarClaw 支持完整的会话状态持久化，**所有渠道均已接入会话管理**。

### 会话命令

| 命令 | 说明 | 全渠道支持 |
|------|------|-----------|
| `/save [描述]` | 保存当前会话 | ✅ |
| `/load [会话ID]` | 加载指定会话 | ✅ |
| `/session` | 显示当前会话信息 | ✅ |
| `/session list` | 列出所有保存的会话 | ✅ |
| `/new` | 创建新会话 | ✅ |
| `/reset` | 重置会话 | ✅ |

### 自动保存

- 系统每 5 分钟自动保存所有活跃会话
- 程序退出时自动保存所有会话
- 会话文件存储在 `sessions/` 目录，格式为 `.session.toon`

### 渠道会话管理

所有渠道共享统一的会话管理系统：

| 渠道 | 会话标识 | 持久化 |
|------|----------|--------|
| CMD | `cmd_<timestamp>` | ✅ |
| Web | `web_<session_id>` | ✅ |
| Telegram | `telegram_<sender_id>` | ✅ |
| Discord | `discord_<sender_id>` | ✅ |
| Slack | `slack_<sender_id>` | ✅ |
| 飞书 | `feishu_<sender_id>` | ✅ |

## 🎭 角色系统

GarClaw 拥有可切换的"灵魂"系统，支持不同角色设定，可用于编程、写作、教学等多种场景。

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

### 技能文件格式

技能文件使用 Markdown 格式，需要包含以下 section：

```markdown
# 技能名称

## 描述
技能的简要描述...

## 触发关键词
- 触发词1
- 触发词2

## 系统提示
激活时注入的系统提示内容...

## 标签
- 标签1
- 标签2
```

**重要**：Section 标题必须使用以下格式才能被正确解析：
- `## 描述` 或 `## Description` → Description 字段
- `## 系统提示` 或 `## System Prompt` → SystemPrompt 字段
- `## 触发关键词` 或 `## Trigger Words` → TriggerWords 字段
- `## 标签` 或 `## Tags` → Tags 字段

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

### 插件调用示例

```
plugin_call(plugin="weather", function="get_weather", args=["广州"])
```

### 预置插件

| 插件 | 说明 |
|------|------|
| `weather` | 天气查询（Open-Meteo API） |
| `exchange` | 汇率查询（ExchangeRate-API） |

### 插件开发

GarClaw 提供完整的 Lua API 供插件调用：

```lua
-- 日志输出
garclaw.log("info", "消息")

-- HTTP 请求
local resp = garclaw.http_get(url)
local resp = garclaw.http_post(url, body, content_type)

-- JSON 处理
local json_str = garclaw.json_encode(table)
local data = garclaw.json_decode(json_string)

-- 文件操作
local content, err = garclaw.read_file(path)
local ok, err = garclaw.write_file(path, content)
local exists = garclaw.exists(path)

-- 时间函数
local ts = garclaw.time()
local str = garclaw.time_format(timestamp, layout)

-- 字符串处理
local parts = garclaw.split(str, separator)
local trimmed = garclaw.trim(str)

-- 其他
local hash = garclaw.hash("sha256", data)
local uuid = garclaw.uuid()
```

> 📖 **插件开发指南**：请使用 `garclaw-plugin-developer` 技能获取完整的插件开发教程。

## 🐚 Shell 工具系统

GarClaw 提供两种 Shell 执行工具，根据任务性质自动选择：

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

GarClaw 内置了完整的浏览器自动化能力，基于 Rod 库实现，共 **32 个** 浏览器工具。

### 工具分类

| 类别 | 工具数量 | 说明 |
|------|----------|------|
| **基础工具** | 3 | 搜索、访问、下载 |
| **交互操作** | 11 | 点击、输入、滚动、拖拽等 |
| **等待操作** | 2 | 元素等待、智能等待 |
| **导航操作** | 1 | 前进/后退/刷新 |
| **内容提取** | 4 | 链接、图片、元素、快照 |
| **截图/PDF** | 3 | 页面截图、元素截图、PDF导出 |
| **高级功能** | 8 | JS执行、Cookie、文件上传、Headers、UA、设备模拟 |

### 核心功能

**Cookie 持久化**：使用 TOON 格式保存登录态，支持自动恢复会话

```json
// 保存 Cookie
browser_cookie_save(url="https://example.com", file_path="cookies.toon")

// 加载 Cookie
browser_cookie_load(url="https://example.com", file_path="cookies.toon")
```

**智能等待**：多条件组合等待，提高操作稳定性

```json
browser_wait_smart(
  url="https://example.com",
  selector=".loading",
  visible=true,
  interactable=true,
  stable=true
)
```

**高级交互**：悬停、双击、右键、拖拽

```json
// 悬停触发下拉菜单
browser_hover(url="...", selector=".dropdown-trigger")

// 拖拽排序
browser_drag(url="...", source_selector=".item-1", target_selector=".item-5")
```

**PDF 导出**：将网页或本地 HTML 转为 PDF

```json
// 将网页导出为 PDF
browser_pdf(url="https://example.com/report")

// 将本地 HTML 文件导出为 PDF（模型可先生成 HTML，再转 PDF）
browser_pdf_from_file(file_path="/tmp/generated_report.html")
```

**设备模拟**：测试移动端响应式设计

```json
// 模拟 iPhone 访问
browser_emulate_device(url="https://example.com", device="iphone")

// 模拟 iPad 横屏
browser_emulate_device(url="https://example.com", device="ipad")
```

**自定义 Headers/UA**：

```json
// 设置自定义请求头
browser_set_headers(url="https://api.example.com", headers=["Authorization: Bearer token", "X-API-Key: key"])

// 设置自定义 User-Agent
browser_set_user_agent(url="https://example.com", user_agent="Mozilla/5.0 (custom)")
```

### 快速示例

```json
// 1. 自动登录
browser_fill_form(url="https://example.com/login", form_data={"username": "user", "password": "pass"})

// 2. 保存登录态
browser_cookie_save(url="https://example.com")

// 3. 提取内容
browser_extract_elements(url="https://example.com/dashboard", selector=".notification-item")
```

> 📖 **详细指南**：请参阅 [BROWSER_TOOLS_GUIDE.md](./BROWSER_TOOLS_GUIDE.md) 获取完整的浏览器工具使用文档。

## 快速开始

### 本地构建

```bash
git clone https://github.com/naamfung/garclaw.git --depth=1
cd garclaw
./build.sh
```

### Docker 跨平台构建

支持在任意平台编译出多平台版本：

| 目标 | 输出文件 | 说明 |
|------|----------|------|
| `linux-amd64` | `garclaw-linux-amd64` | Linux x86_64 (glibc) |
| `linux-arm64` | `garclaw-linux-arm64` | Linux ARM64 (树莓派等) |
| `alpine-amd64` | `garclaw-alpine-amd64` | Alpine Linux (静态链接) |
| `alpine-arm64` | `garclaw-alpine-arm64` | Alpine ARM64 (静态链接) |
| `loong64` | `garclaw-linux-loong64` | LoongArch 龙芯 |
| `darwin-amd64` | `garclaw-darwin-amd64` | macOS Intel |
| `darwin-arm64` | `garclaw-darwin-arm64` | macOS Apple Silicon (M1/M2/M3) |
| `windows-amd64` | `garclaw-windows-amd64.exe` | Windows |
| `freebsd-amd64` | `garclaw-freebsd-amd64` | FreeBSD |
| `ghostbsd-amd64` | `garclaw-ghostbsd-amd64` | GhostBSD |

```bash
# Linux (glibc)
./docker-build.sh linux-amd64
./docker-build.sh linux-arm64

# Alpine Linux (musl, 静态链接, 适合 Docker 容器)
./docker-build.sh alpine-amd64
./docker-build.sh alpine-arm64

# LoongArch (龙芯处理器)
./docker-build.sh loong64

# macOS (Intel / Apple Silicon)
./docker-build.sh darwin-amd64
./docker-build.sh darwin-arm64

# Windows / FreeBSD / GhostBSD
./docker-build.sh windows-amd64
./docker-build.sh freebsd-amd64
./docker-build.sh ghostbsd-amd64

# 构建所有平台 (国内用户加 --cn 加速)
./docker-build.sh all --cn
```

构建产物位于 `dist/` 目录。

### Docker Compose 方式

```bash
# 构建指定平台
docker compose build linux-amd64
docker compose build darwin-amd64
docker compose build darwin-arm64
docker compose build ghostbsd-amd64

# 运行容器
docker compose up runtime
```

首次运行自动生成 `config.toon` 配置文件。

## 项目结构

```
garclaw/
├── main.go               # 程序入口
├── AgentLoop.go          # 核心对话循环
├── role.go               # 角色模板管理
├── role_presets.go       # 预置角色定义
├── actor.go              # 演员实例管理
├── stage.go              # 场景管理（自动切换）
├── skill.go              # 技能管理
├── session.go            # 会话管理
├── session_persist.go    # 会话持久化
├── channel_session_manager.go  # 渠道会话管理
├── memory.go             # 结构化记忆管理
├── two_layer_memory.go   # 两层记忆系统
├── memory_consolidator.go # 记忆整合器
├── plugin.go             # 插件管理器
├── cron.go               # 定时任务管理
├── browser_session.go    # 浏览器会话管理器
├── browser_tools.go      # 基础浏览器工具
├── browser_tools_advanced.go # 高级浏览器工具
├── skills/               # 技能定义目录
│   ├── code_review.md
│   ├── translation.md
│   └── custom/           # 自定义技能
├── plugins/              # 插件目录
│   ├── weather/
│   │   └── weather.lua
│   └── exchange/
│       └── exchange.lua
├── memory/               # 记忆存储目录
│   ├── memory.toon       # 结构化记忆
│   ├── MEMORY.md         # 长期记忆
│   └── HISTORY.md        # 会话历史摘要
└── sessions/             # 会话存储目录
    └── *.session.toon
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

**重要提示：GarClaw 的安全策略依赖外部机制保障**

本程序的安全设计原则：

1. **内部安全机制有限**：程序仅包含基础的「危险命令拦截」功能，用于过滤 `rm -rf /`、`mkfs`、`dd if=` 等极端危险命令。此机制**不可作为唯一的安全防线**。

2. **外部安全机制必需**：在生产环境中运行 GarClaw 时，**必须**配合以下安全措施：
   - **容器隔离**：使用 Docker、Podman 等容器技术
   - **监狱机制**：使用 chroot、jail、namespace 隔离
   - **权限限制**：以非 root 用户运行，使用 sudo 限制
   - **网络隔离**：限制网络访问，使用防火墙规则
   - **资源限制**：使用 cgroups 限制 CPU/内存使用

3. **安全责任**：
   - 不要在裸机环境直接运行 GarClaw 处理不可信输入
   - 不要授予 GarClaw 超过必要的系统权限
   - 定期审计 GarClaw 的操作日志
   - 敏感数据应通过配置文件管理，避免硬编码

4. **默认配置**：程序默认关闭危险命令拦截（`block_dangerous_commands = false`），用户可通过配置开启此保护。开启后，将拦截 `rm -rf /`、`mkfs`、`dd if=` 等极端危险命令。

**安全是一个多层次的问题，GarClaw 专注于功能实现，安全防护应通过外部机制完成。**
