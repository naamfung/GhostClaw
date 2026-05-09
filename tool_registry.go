package main

// ============================================================
// 工具注册表 - SINGLE SOURCE OF TRUTH for all tool definitions
// ============================================================
// 所有工具在此文件中定义一次，然后通过 ToOpenAI() / ToAnthropic()
// 转换为对应格式，消除 tools_openai.go 和 tools_anthropic.go 之间的
// 参数漂移问题。
//
// 不包含在此注册表中的特殊/动态工具：
//   - menu（tool_menu.go 中定义）
//   - Plan Mode 动态工具（next_phase, PlanWrite, PlanRead - plan_mode.go）
//   - 合并浏览器工具（GetConsolidatedBrowserTools - tool_tier.go）
//   - MCP 动态工具（运行时加载）
// ============================================================

// ToolDef 工具定义结构体
type ToolDef struct {
        Name        string                 // 工具名称
        Description string                 // 工具描述
        Category    string                 // 菜单分类：core, file, web, memory, schedule, plan, skill, plugin, profile, spawn, misc
        Tier        string                 // 层级："core", "extended", "expert"
        Parameters  map[string]interface{} // JSON Schema 输入参数（两种格式共用）
}

// toolRegistry 有序工具列表（保持注册顺序）
var toolRegistry []*ToolDef

// toolRegistryMap 工具名→定义快速查找表
var toolRegistryMap map[string]*ToolDef

// reg 注册一个工具到全局注册表
func reg(name, desc, category string, tier string, params map[string]interface{}) {
        td := &ToolDef{
                Name:        name,
                Description: desc,
                Category:    category,
                Tier:        tier,
                Parameters:  params,
        }
        toolRegistry = append(toolRegistry, td)
        toolRegistryMap[name] = td
}

func init() {
        toolRegistryMap = make(map[string]*ToolDef)

        // ========== 原有工具 ==========
        reg("SmartShell", `智能执行 shell 命令，自动判断同步或异步执行模式。

✅ 快速命令（ls, cat, grep 等）：同步执行，立即返回结果
✅ 慢速命令（apt, make, npm install 等）：异步执行，后台运行
✅ 远程 SSH 启动守护进程：
   - Linux: setsid /path/to/program < /dev/null > /tmp/program.log 2>&1 &
   - GhostBSD/FreeBSD: daemon -p /var/run/program.pid /path/to/program
   然后通过 ps aux | grep program 或 curl 检查状态。

系统自动判断命令类型：
• 包管理器、编译、下载、传输 → 异步执行
• 其他命令 → 同步执行

可选参数：
• mode: "async" 强制异步执行，"sync" 强制同步执行，默认自动判断
• timeout_secs: 异步任务最大执行时间（秒），超时自动终止并唤醒（默认无限制）
• wake_after_minutes: 异步唤醒时间（默认5分钟）

🚫 DO NOT POLL: 异步任务启动后不要轮询，系统会自动通知结果。如果当前有活跃的 todo 项目，请使用 todos 工具将相关 todo 标记为 waiting，然后继续处理其他工作或等待系统唤醒，切勿以同步模式重新执行同一命令。`,
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要执行的 shell 命令",
                                },
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"sync", "async", "auto"},
					"description": "执行模式：\"sync\" 强制同步，\"async\" 强制异步，\"auto\" 自动判断（默认）",
				},
                                "TimeoutSecs": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步任务最大执行时间（秒），超时自动终止并唤醒（可选，默认无限制）",
                                },
                                "WakeAfterMinutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步唤醒时间（分钟，默认5分钟）",
                                },
                                "force": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "设为 true 可绕过阻塞命令检测，直接执行（默认 false）",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "异步任务描述（可选，仅在 mode=\"async\" 时有效）",
                                },
                        },
                        "required":             []string{"command"},
                        "additionalProperties": false,
                })

        reg("ReadFileLine",
                "Read a specific line from a file. Use this when you need to read a particular line from a file without reading the entire file.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to read.",
                                },
                                "LineNum": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The line number to read (starting from 1).",
                                },
                                "verbose": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to return verbose information (line number, encoding, file size, etc.). Default: false (only returns content).",
                                },
                        },
                        "required":             []string{"filename", "LineNum"},
                        "additionalProperties": false,
                })

        reg("WriteFileLine",
                "Overwrite, insert, or append a single line in a file.\n\n- LineNum > 0: Overwrite line LineNum with content.\n- LineNum = 0: Create an empty file (content is ignored).\n- LineNum = -1: Append content to the end of the file.\n- LineNum < -1: Insert content as a new line BEFORE position |LineNum|, shifting existing lines down. Example: LineNum=-5 inserts before line 5.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to write to.",
                                },
                                "LineNum": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Determines the operation mode. > 0: overwrite line LineNum. 0: create empty file. -1: append to end. < -1: insert before |LineNum| (e.g., -5 inserts before line 5).",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The content to write. In overwrite mode: replaces the target line. In insert mode: becomes the new line. In append mode: added to the end.",
                                },
                        },
                        "required":             []string{"filename", "LineNum", "content"},
                        "additionalProperties": false,
                })

        reg("ReadFileLines",
                "Read all lines from a file and return them as a list of strings.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to read.",
                                },
                                "verbose": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to return verbose information (line numbers, encoding, file size, etc.). Default: false (only returns lines content).",
                                },
                        },
                        "required":             []string{"filename"},
                        "additionalProperties": false,
                })

        reg("WriteFileLines",
                "Write all lines to a file.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to write to.",
                                },
                                "lines": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "The list of lines to write to the file. Each element in the array corresponds to one line in the file. Do NOT pass a single string; it must be an array of strings. Example: [\"line1\", \"line2\", \"line3\"]",
                                },
                                "append": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to append to the end of the file. Default is false (overwrite the entire file).",
                                },
                        },
                        "required":             []string{"filename", "lines"},
                        "additionalProperties": false,
                })

        reg("AppendToFile",
                "Append content to the end of a file.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to append to.",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The content to append to the file.",
                                },
                                "LineBreak": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to add a line break after the content. Default is true.",
                                },
                        },
                        "required":             []string{"filename", "content"},
                        "additionalProperties": false,
                })

        reg("WriteFileRange",
                "Overwrite a range of lines, or insert multiple lines, in a file.\n\nOverwrite mode (StartLine >= 1):\n  Replace lines StartLine through EndLine with content. EndLine defaults to StartLine if not specified. Each line in content replaces one line in the range.\n\nInsert mode (StartLine < 0):\n  Insert all lines of content BEFORE position |StartLine|, shifting existing lines down. EndLine is ignored in this mode. Example: StartLine=-10 inserts before line 10.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to write to.",
                                },
                                "StartLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Determines the operation mode. >= 1: overwrite range starting at this line. < 0: insert all lines BEFORE position |StartLine|. Cannot be 0.",
                                },
                                "EndLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Overwrite mode only: the last line of the overwrite range (inclusive). Ignored in insert mode. Defaults to StartLine.",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The content to write. Overwrite mode: each line replaces one line in the range. Insert mode: each line becomes a new line inserted at the target position.",
                                },
                        },
                        "required":             []string{"filename", "StartLine", "content"},
                        "additionalProperties": false,
                })

        reg("ReadFileRange",
                "Read a specific range of lines from a file. Use this when you need to read a portion of a file without reading the entire file.",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to read.",
                                },
                                "StartLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The starting line number (1-based, inclusive).",
                                },
                                "EndLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The ending line number (1-based, inclusive). If not specified, only the start_line is read.",
                                },
                                "verbose": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to return verbose information (line numbers, encoding, file size, etc.). Default: false (only returns lines content).",
                                },
                        },
                        "required":             []string{"filename", "StartLine"},
                        "additionalProperties": false,
                })

        reg("FileInfo",
                "Get detailed file information (similar to the Unix 'file' command). Returns file size, type, MIME type, encoding, permissions, and system file description. Use this to inspect a file before reading it, especially for unknown or binary files.",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to inspect.",
                                },
                        },
                        "required":             []string{"filename"},
                        "additionalProperties": false,
                })

        // ========== 基础浏览器工具 ==========
        reg("BrowserSearch", `Search for a keyword using Baidu search engine. Returns a list of search results with titles and links.

⚠️ PRIORITY NOTICE: If OpenCLI is available on the system, prefer using OpenCLI commands via the 'shell' tool instead of this built-in browser tool.
OpenCLI provides better session persistence and more reliable web automation.

Example OpenCLI alternative:
- shell: "opencli search <keyword>" (if adapter available)
- shell: "opencli open https://www.baidu.com/s?wd=<keyword>"`,
                "web", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "keyword": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The keyword to search for.",
                                },
                        },
                        "required":             []string{"keyword"},
                        "additionalProperties": false,
                })

        reg("BrowserVisit", `Visit a URL and extract the text content from the web page. Useful for reading article content, product descriptions, etc.

⚠️ PRIORITY NOTICE: If OpenCLI is available on the system, prefer using OpenCLI commands via the 'shell' tool instead of this built-in browser tool.
OpenCLI provides better session persistence, cookie reuse, and more reliable web automation.

Example OpenCLI alternative:
- shell: "opencli open <url>"
- shell: "opencli <adapter> <command>" (e.g., "opencli hackernews top --limit 5")`,
                "web", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to visit.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        // ========== OpenCLI 工具 ==========
        reg("Opencli", `OpenCLI — 深度包装的网站交互工具。支持 70+ 网站适配器、网页抓取、适配器开发管理。

# 一、数据获取

## WebRead — 获取网页 Markdown
将任意网页转为干净 Markdown，支持图片下载和格式输出。
参数：url (必填), download_images (可选，默认 true), wait (可选，默认 3 秒), output (可选，输出目录)

## Adapter — 调用网站适配器
直接调用 70+ 预置网站 API 适配器。
参数：site (必填), command (必填), args (可选，额外参数如 --limit 10)

# 二、适配器开发

## Explore — 探索网站发现 API
分析网站结构，推荐数据抓取策略。
参数：url (必填), goal (可选), AutoFuzz (可选)

## Synthesize — 从探索结果合成 CLI
将 explore 的结果合成为适配器定义。
参数：site (必填), top (可选，默认 3)

## Generate — 一键生成适配器
explore → synthesize → verify → register 全自动。
参数：url (必填), SiteName (可选), goal (可选)

## Validate — 验证适配器定义
检查适配器 YAML 定义合法性。
参数：target (可选，site 或 site/command)

## Verify — 验证并冒烟测试
validate + 可选冒烟测试。
参数：target (可选), smoke (可选)

## Record — 录制浏览器 API 调用
录制实时浏览器会话的 API 调用，生成适配器候选 YAML。
参数：url (必填), site (可选), out (可选，输出目录), poll (可选，默认 2000ms), timeout (可选，默认 60000ms)

## Cascade — 策略级联
自动尝试从简到繁的策略，找到最简单可行的方案。
参数：url (必填), site (可选)

# 三、管理

## List — 列出所有适配器
参数：format (可选, table/json/yaml/md/csv)

## Adapter_status — 适配器覆写状态
查看哪些网站有本地覆写 vs 使用官方版本。

## Adapter_eject — 导出适配器到本地
将官方适配器复制到 ~/.opencli/clis/ 供本地编辑。
参数：site (必填)

## Adapter_reset — 重置适配器
移除本地覆写，恢复官方版本。
参数：site (可选，不指定则配合 all=true 重置全部), all (可选)

## Register — 注册外部 CLI
参数：name (必填), binary (可选), install_cmd (可选), desc (可选)

## Install — 安装外部 CLI
参数：name (必填)

## PluginList — 列出已安装插件
参数：format (可选, table/json)

## PluginInstall — 安装插件
从 Git 仓库安装。
参数：source (必填，如 github:user/repo)

## PluginUninstall — 卸载插件
参数：name (必填)

## PluginUpdate — 更新插件
参数：name (可选，不指定则更新全部)

## PluginCreate — 创建插件脚手架
参数：name (必填)

## Doctor — 诊断连接状态

## DaemonStop — 停止守护进程

# 常用适配器速查
| 网站 | site | 常用命令 |
|------|------|---------|
| B站 | bilibili | search, hot, comments, ranking, user-videos, subtitle, download, me |
| Google | google | search, news, trends, suggest |
| 知乎 | zhihu | search, hot, answers, questions |
| GitHub | gh | repo, issue, pr, search, gist |
| 百度贴吧 | tieba | search, hot, posts |
| 小红书 | xiaohongshu | search, notes, user |
| 微博 | weibo | search, hot, timeline |
| 抖音 | douyin | search, user, video |
| Twitter | twitter | search, tweet, user, timeline |
| YouTube | youtube | search, video, channel |
| Reddit | reddit | search, hot, subreddit |
| Wikipedia | wikipedia | search, article |
| HackerNews | hackernews | top, new, best, ask, show |
| arXiv | arxiv | search, paper |
| 豆瓣 | douban | search, movie, book |
| 京东 | jd | search, product |
| 淘宝 | taobao | search, product |
| 36氪 | 36kr | news, flash |
| V2EX | v2ex | hot, latest, nodes |
| Steam | steam | search, game, deals |
| 微信读书 | weread | search, book, review |
| 什么值得买 | smzdm | search, deals, hot |
| 闲鱼 | xianyu | search, product |
| 虎扑 | hupu | hot, posts, search |
| LinkedIn | linkedin | search, profile, jobs |
| ProductHunt | producthunt | today, popular, search |
| 飞书/Lark | lark-cli | search, docs, calendar, tasks |
| 企业微信 | wecom-cli | search, contacts, todos, meetings, messages |
| Obsidian | obsidian | search, notes, tags, tasks |
| Docker | docker | ps, images, pull, run |
| Medium | medium | search, articles |
| Notion | notion | search, pages |
| Spotify | spotify | search, playlist, track |
| Instagram | instagram | search, user, posts |
| Facebook | facebook | search, posts |
| 豆瓣 | douban | search, movie, book |`,
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "action": map[string]interface{}{
                                        "type": "string",
                                        "description": "操作：WebRead, Adapter, List, Explore, Synthesize, Generate, Validate, Verify, Record, Cascade, AdapterStatus, AdapterEject, AdapterReset, Register, Install, PluginList, PluginInstall, PluginUninstall, PluginUpdate, PluginCreate, Doctor, DaemonStop",
                                },
                                "url": map[string]interface{}{
                                        "type": "string",
                                        "description": "URL（WebRead, Explore, Generate, Record, Cascade 需要）",
                                },
                                "site": map[string]interface{}{
                                        "type": "string",
                                        "description": "网站适配器名称（Adapter, Synthesize, AdapterEject, AdapterReset 需要）",
                                },
                                "command": map[string]interface{}{
                                        "type": "string",
                                        "description": "适配器子命令（Adapter 需要），如 search, hot",
                                },
                                "args": map[string]interface{}{
                                        "type": "string",
                                        "description": "子命令额外参数（Adapter 可选），如 --limit 10",
                                },
                                "goal": map[string]interface{}{
                                        "type": "string",
                                        "description": "探索/生成目标（Explore, Generate 可选）",
                                },
                                "AutoFuzz": map[string]interface{}{
                                        "type": "boolean",
                                        "description": "交互式模糊测试（Explore 可选）",
                                },
                                "SiteName": map[string]interface{}{
                                        "type": "string",
                                        "description": "网站名称（Generate 可选）",
                                },
                                "target": map[string]interface{}{
                                        "type": "string",
                                        "description": "验证目标 (Validate, Verify 可选)，格式 site 或 site/command",
                                },
                                "smoke": map[string]interface{}{
                                        "type": "boolean",
                                        "description": "运行冒烟测试 (Verify 可选)",
                                },
                                "top": map[string]interface{}{
                                        "type": "integer",
                                        "description": "合成数量 (Synthesize 可选，默认 3)",
                                },
                                "format": map[string]interface{}{
                                        "type": "string",
                                        "description": "输出格式 (List, PluginList 可选)：table, json, yaml, md, csv",
                                },
                                "name": map[string]interface{}{
                                        "type": "string",
                                        "description": "名称 (register, Install, PluginUninstall, PluginUpdate, plugin_create 需要)",
                                },
                                "binary": map[string]interface{}{
                                        "type": "string",
                                        "description": "二进制名称 (register 可选)",
                                },
                                "InstallCmd": map[string]interface{}{
                                        "type": "string",
                                        "description": "自动安装命令 (register 可选)",
                                },
                                "desc": map[string]interface{}{
                                        "type": "string",
                                        "description": "描述 (register 可选)",
                                },
                                "source": map[string]interface{}{
                                        "type": "string",
                                        "description": "Git 仓库源 (plugin_install 需要)，如 github:user/repo",
                                },
                                "all": map[string]interface{}{
                                        "type": "boolean",
                                        "description": "应用于全部 (adapter_reset, plugin_update 可选)",
                                },
                                "DownloadImages": map[string]interface{}{
                                        "type": "boolean",
                                        "description": "下载图片到本地 (WebRead 可选，默认 true)",
                                },
                                "wait": map[string]interface{}{
                                        "type": "integer",
                                        "description": "页面加载后等待秒数 (WebRead 可选，默认 3)",
                                },
                                "output": map[string]interface{}{
                                        "type": "string",
                                        "description": "输出目录 (WebRead, record 可选)",
                                },
                                "poll": map[string]interface{}{
                                        "type": "integer",
                                        "description": "轮询间隔毫秒 (Record 可选，默认 2000)",
                                },
                                "timeout": map[string]interface{}{
                                        "type": "integer",
                                        "description": "自动停止毫秒数 (Record 可选，默认 60000)",
                                },
                        },
                        "required":             []string{"action"},
                        "additionalProperties": false,
                })

        reg("BrowserDownload",
                "Download a web page HTML and save it to a local file. Returns the saved file path.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to download.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        // ========== 浏览器增强工具 ==========
        reg("BrowserClick",
                "Click an element on a web page. Navigate to the URL and click the specified element using CSS selector. Useful for buttons, links, and other interactive elements.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to click. Examples: 'button.submit', '#login-btn', 'a[href*=\"detail\"]', '.btn-primary'",
                                },
                                "timeout": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Optional timeout in seconds. Default: 30. Increase for slow pages.",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserType",
                "Type text into an input field on a web page. Can optionally submit the form by pressing Enter.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the input field. Examples: 'input[name=\"username\"]', '#search-box', '.search-input'",
                                },
                                "text": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The text to type into the input field.",
                                },
                                "submit": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to press Enter after typing (to submit form). Default: false",
                                },
                        },
                        "required":             []string{"url", "selector", "text"},
                        "additionalProperties": false,
                })

        reg("BrowserScroll",
                "Scroll the web page up or down by a specified amount.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "direction": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"up", "down"},
                                        "description": "Scroll direction: 'up' or 'down'.",
                                },
                                "amount": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Number of pixels to scroll. Default: 500",
                                },
                        },
                        "required":             []string{"url", "direction"},
                        "additionalProperties": false,
                })

        reg("BrowserWaitElement",
                "Wait for a specific element to appear on the page. Useful for dynamic content that loads after page load.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to wait for.",
                                },
                                "timeout": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Maximum wait time in seconds. Default: 10",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserExtractLinks",
                "Extract all links from a web page. Returns link text and URL for each link found.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserExtractImages",
                "Extract all images from a web page. Returns image source URL and alt text for each image.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserExtractElements",
                "Extract content from specific elements matching a CSS selector. Returns text, HTML, and attributes of matched elements.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for elements to extract. Examples: '.article', 'div.content p', 'h2.title'",
                                },
                                "IncludeHtml": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to include HTML content. Default: false",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserScreenshot",
                "Take a screenshot of a web page. Returns base64-encoded image. Can capture full page or viewport only.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "FullPage": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Capture the entire page (including scrollable area) or just the viewport. Default: false (viewport only)",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserExecuteJs",
                "Execute custom JavaScript code on a web page. Returns the result of the script execution.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "script": map[string]interface{}{
                                        "type":        "string",
                                        "description": "JavaScript code to execute. Must be a function expression. Examples: '() => document.title' or '() => { return {url: location.href, title: document.title}; }'",
                                },
                        },
                        "required":             []string{"url", "script"},
                        "additionalProperties": false,
                })

        reg("BrowserFillForm",
                "Fill out and submit a web form. Automatically finds input fields by name or ID attribute.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "FormData": map[string]interface{}{
                                        "type":        "object",
                                        "description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
                                },
                                "SubmitSelector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for submit button. If empty, presses Enter to submit.",
                                },
                        },
                        "required":             []string{"url", "FormData"},
                        "additionalProperties": false,
                })

        // ========== 浏览器高级工具 ==========
        reg("BrowserHover",
                "Hover mouse over an element. Useful for triggering hover menus, tooltips, or hover effects.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to hover over.",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserDoubleClick",
                "Double-click an element on a web page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to double-click.",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserRightClick",
                "Right-click an element to open context menu.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to right-click.",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserDrag",
                "Drag an element and drop it onto another element. Useful for drag-and-drop interfaces.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "SourceSelector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to drag.",
                                },
                                "TargetSelector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the drop target.",
                                },
                        },
                        "required":             []string{"url", "SourceSelector", "TargetSelector"},
                        "additionalProperties": false,
                })

        reg("BrowserWaitSmart",
                "Smart wait for element with options: visible, interactable, stable. More reliable than basic wait.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element.",
                                },
                                "visible": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Wait for element to be visible. Default: true",
                                },
                                "interactable": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Wait for element to be clickable/not covered. Default: false",
                                },
                                "stable": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Wait for element to stop moving/animating. Default: false",
                                },
                                "timeout": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Maximum wait time in seconds. Default: 10",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("BrowserNavigate",
                "Navigate browser: go back, forward, or refresh page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to first.",
                                },
                                "action": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"back", "forward", "refresh"},
                                        "description": "Navigation action: 'back', 'forward', or 'refresh'.",
                                },
                        },
                        "required":             []string{"url", "action"},
                        "additionalProperties": false,
                })

        reg("BrowserGetCookies",
                "Get all cookies from a web page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to get cookies from.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserCookieSave",
                "Save cookies from a web page to a TOON file for persistence. Useful for saving login state.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to get cookies from.",
                                },
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Path to save the cookies file. If empty, uses default name like 'cookies_domain.json'.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserCookieLoad",
                "Load cookies from a TOON file and apply them to a web page. Useful for restoring login state.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to apply cookies to.",
                                },
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Path to the cookies file to load.",
                                },
                        },
                        "required":             []string{"url", "FilePath"},
                        "additionalProperties": false,
                })

        reg("BrowserSnapshot",
                "Get a simplified DOM snapshot of the page for visual analysis. Returns element tree with positions.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to analyze.",
                                },
                                "MaxDepth": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Maximum depth of element tree. Default: 5",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserUploadFile",
                "Upload files to a file input element.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the file input element.",
                                },
                                "FilePaths": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "List of file paths to upload.",
                                },
                        },
                        "required":             []string{"url", "selector", "FilePaths"},
                        "additionalProperties": false,
                })

        reg("BrowserSelectOption",
                "Select options in a dropdown/select element.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the select element.",
                                },
                                "values": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "Option values or text to select.",
                                },
                        },
                        "required":             []string{"url", "selector", "values"},
                        "additionalProperties": false,
                })

        reg("BrowserKeyPress",
                "Simulate keyboard key presses. Useful for shortcuts like Ctrl+C, Ctrl+Enter, etc.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "keys": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "Keys to press in sequence. Examples: ['Control', 'c'], ['Enter'], ['ArrowDown', 'Enter']",
                                },
                        },
                        "required":             []string{"url", "keys"},
                        "additionalProperties": false,
                })

        reg("BrowserElementScreenshot",
                "Take a screenshot of a specific element on the page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to screenshot.",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        // ========== PDF 导出工具 ==========
        reg("BrowserPdf",
                "Export a web page as PDF. Returns base64 encoded PDF data.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to and export as PDF.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("BrowserPdfFromFile",
                "Export a local HTML file as PDF. Useful for converting generated HTML to PDF. Returns base64 encoded PDF data.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Absolute path to the local HTML file to convert to PDF.",
                                },
                        },
                        "required":             []string{"FilePath"},
                        "additionalProperties": false,
                })

        // ========== Headers 与 UA 设置 ==========
        reg("BrowserSetHeaders",
                "Set custom HTTP headers and navigate to a page. Headers should be in 'Key: Value' format.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "headers": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "Array of headers in 'Key: Value' format, e.g. ['Authorization: Bearer token', 'X-Custom: value']",
                                },
                        },
                        "required":             []string{"url", "headers"},
                        "additionalProperties": false,
                })

        reg("BrowserSetUserAgent",
                "Set a custom User-Agent and navigate to a page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "UserAgent": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The User-Agent string to use.",
                                },
                        },
                        "required":             []string{"url", "UserAgent"},
                        "additionalProperties": false,
                })

        // ========== 设备模拟 ==========
        reg("BrowserEmulateDevice",
                "Emulate a mobile device (iPhone, iPad, Android, etc.) when accessing a page. Useful for testing responsive design.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "device": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Device preset: iphone, iphone_landscape, ipad, android_phone, android_tablet, desktop, desktop_mac",
                                },
                        },
                        "required":             []string{"url", "device"},
                        "additionalProperties": false,
                })

        // ========== 插件管理工具 ==========
        reg("PluginList",
                "列出所有已加载的插件及其提供的函数。",
                "plugin", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("PluginCreate",
                "Create a new empty plugin skeleton. This creates a folder with the plugin name and a Lua entry file containing a basic template. Use this to start developing a new plugin.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Unique plugin name (will be used as folder name).",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Optional description of the plugin's purpose (will be included as comment).",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("PluginLoad",
                "Load a new plugin from Lua code. The plugin will be saved in its own folder under plugins directory.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Unique plugin name (will be used as folder name).",
                                },
                                "code": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Lua script code (must contain at least one function).",
                                },
                        },
                        "required": []string{"name", "code"},
                })

        reg("PluginUnload",
                "Unload a plugin by name (removes from memory only, files remain).",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name.",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("PluginReload",
                "Reload a specific plugin from disk (useful after code update). This only reloads one plugin at a time, not all plugins.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Specific plugin name to reload.",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("PluginCall",
                "调用已加载插件中的函数。先用 PluginList 查看可用函数。args 中的 items 需要指定类型信息。",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "plugin": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name.",
                                },
                                "function": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Lua function name to call.",
                                },
                                "args": map[string]interface{}{
                                        "type":        "array",
                                        "description": "Arguments to pass to the function (optional).",
                                        "items":       map[string]interface{}{"type": "string"},
                                },
                        },
                        "required": []string{"plugin", "function"},
                })

        reg("PluginCompile",
                "Compile Lua code to bytecode (syntax check). If successful, no error; if compilation fails, returns error details. Use this to verify plugin code before loading.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name (used to locate source file or cache).",
                                },
                                "code": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Lua code to compile (optional, if not provided, compiles existing plugin source).",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("PluginDelete",
                "Permanently delete a plugin (removes its folder and all files). Use this to completely remove a plugin.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name to delete.",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("PluginApis",
                "List plugin system internal API documentation for model reference.",
                "plugin", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("PluginDetail",
                "Get detailed information about a specific plugin, including its functions, source code, and metadata.",
                "plugin", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name to get details for.",
                                },
                                "IncludeSource": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to include the full source code. Default: false.",
                                },
                        },
                        "required": []string{"name"},
                })

        // ========== Cron 管理工具 ==========
        reg("CronAdd",
                "添加定时任务。到指定时间自动执行一条自然语言指令。参数错误时会返回正确格式，请按格式重新调用。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务名称（必填），如「每日AI论文速递」",
                                },
                                "schedule": map[string]interface{}{
                                        "type":        "string",
                                        "description": "cron 表达式，6字段格式：秒 分 时 日 月 周。常用：\"0 0 9 * * *\" 每天9点，\"0 30 17 * * *\" 每天17:30，\"0 0 9 * * 1-5\" 工作日9点",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "到时要执行的自然语言指令（必填），如「去arXiv查看最新AI论文并汇总关键信息」",
                                },
                                "UserMessage": map[string]interface{}{
                                        "type":        "string",
                                        "description": "[兼容] 等同于 content，推荐使用 content",
                                },
                                "channel": map[string]interface{}{
                                        "type":        "object",
                                        "description": "输出目标配置，可选，默认输出到日志。格式: {\"type\": \"log\"} 或 {\"type\": \"email\", \"recipients\": [\"a@b.com\"]}",
                                },
                        },
                        "required": []string{"name", "schedule", "content"},
                })

        reg("CronRemove",
                "删除一个定时任务。先用 CronList 确认任务名称。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要删除的任务名称（必填）",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("CronList",
                "列出所有已配置的定时任务（名称、排程、状态）。无参数。",
                "schedule", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("CronStatus",
                "查询指定定时任务的详细状态（下次执行时间、最近执行结果等）。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要查询的任务名称（必填）",
                                },
                        },
                        "required": []string{"name"},
                })

        // ── TodoWrite (V1 批量替換) — 主要任務管理工具 ──
        // 智能合併模式：新 items 會同現有列表合併（內容相似則更新，新項追加，未提及舊項保留）。
        // 全部 Completed 時可傳 [] 清空。
        reg("TodoWrite",
                "批量更新任務列表。傳入待更新嘅 todos 陣列，會智能合併到現有列表。未提及嘅舊任務會自動保留。全部完成後傳 [] 清空。\n\n正確格式示例：\n{\"todos\": [{\"content\": \"檢查日誌文件\", \"status\": \"InProgress\", \"activeForm\": \"檢查緊日誌文件\"}, {\"content\": \"清理臨時文件\", \"status\": \"Pending\", \"activeForm\": \"清理緊臨時文件\"}]}\n\n注意：todos 必須係 array，每個元素必須係包含 content/status/activeForm 三個字段的 object。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "todos": map[string]interface{}{
                                        "type": "array",
                                        "items": map[string]interface{}{
                                                "type": "object",
                                                "properties": map[string]interface{}{
                                                        "content": map[string]interface{}{
                                                                "type":        "string",
                                                                "description": "任務內容（祈使句，例如「Remove socat」）",
                                                        },
                                                        "status": map[string]interface{}{
                                                                "type":        "string",
                                                                "enum":        []string{"Pending", "InProgress", "Completed", "Cancelled"},
                                                                "description": "任務狀態",
                                                        },
                                                        "activeForm": map[string]interface{}{
                                                                "type":        "string",
                                                                "description": "進行中嘅動詞形式（例如「Removing socat…」），用於 UI spinner",
                                                        },
                                                },
                                                "required": []string{"content", "status"},
                                        },
                                        "description": "完整任務列表（取代現有全部）",
                                },
                        },
                        "required": []string{"todos"},
                })

        // ── V2 獨立工具（保留作回退，見 globalTodoV2Mode toggle）──
        reg("TodoCreate",
                "创建新任务。传入 content（任务内容）同可选 status（默认 Pending）。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务内容描述",
                                },
                                "status": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"Pending", "InProgress", "Completed", "Waiting", "Cancelled"},
                                        "description": "任务状态，默认为 Pending",
                                },
                        },
                        "required": []string{"content"},
                })

        reg("TodoUpdate",
                "更新或刪除任務。傳入 id（任務 ID）同需要修改嘅欄位（content、status）。status 設為空字串可刪除該任務。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任務唯一標識",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "新的任務內容（可選）",
                                },
                                "status": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"Pending", "InProgress", "Completed", "Waiting", "Cancelled"},
                                        "description": "新狀態。設為空字串可刪除該任務",
                                },
                        },
                        "required": []string{"id"},
                })

        reg("TodoList",
                "列出當前所有任務及其狀態。無需參數。",
                "schedule", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("TodoDelete",
                "刪除指定 ID 的任務。用於清理已完成或不再需要嘅單個任務，無需重寫整個列表。",
                "schedule", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要刪除的任務 ID（如 #1、#2）",
                                },
                        },
                        "required": []string{"id"},
                })

        // ========== 记忆管理工具 ==========
        reg("MemorySave",
                "保存一条记忆到长期存储，跨会话持久化。支持分类（fact/preference/project/skill/context）和标签，便于后续检索。",
                "memory", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "key": map[string]interface{}{
                                        "type":        "string",
                                        "description": "记忆键名，如 'UserName', 'PreferredLanguage'",
                                },
                                "value": map[string]interface{}{
                                        "type":        "string",
                                        "description": "记忆内容",
                                },
                                "category": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"preference", "fact", "project", "skill", "context"},
                                        "description": "分类：preference(偏好)/fact(事实)/project(项目)/skill(技能)/context(上下文)，默认fact",
                                },
                                "scope": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"user", "global"},
                                        "description": "范围：user(用户级)/global(全局)，默认user",
                                },
                                "tags": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "标签数组，便于检索",
                                },
                        },
                        "required": []string{"key", "value"},
                })

        reg("MemoryRecall",
                "检索已保存的记忆。支持按关键词模糊搜索（query）或按键名精确查找，可限定分类。无参数时返回所有记忆。",
                "memory", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "query": map[string]interface{}{
                                        "type":        "string",
                                        "description": "检索关键词或键名",
                                },
                                "category": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"preference", "fact", "project", "skill", "context"},
                                        "description": "限定分类（可选）",
                                },
                                "limit": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "返回数量限制，默认10",
                                },
                        },
                        "required": []string{},
                })

        reg("MemoryForget",
                "删除指定键名的记忆（不可恢复）。建议先用 MemoryRecall 确认要删除的记忆内容。",
                "memory", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "key": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要删除的记忆键名",
                                },
                        },
                        "required": []string{"key"},
                })

        reg("MemoryList",
                "列出所有已保存的记忆，支持按分类（preference/fact/project/skill/context）和范围（user/global）过滤。",
                "memory", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "category": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"preference", "fact", "project", "skill", "context"},
                                        "description": "限定分类（可选）",
                                },
                                "scope": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"user", "global"},
                                        "description": "限定范围（可选）",
                                },
                        },
                        "required": []string{},
                })

        reg("ConsolidateMemory",
                "将当前对话中的关键信息整合到长期记忆系统中。当对话内容较长或包含重要信息时，使用此工具进行记忆整合。",
                "memory", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "HistoryEntry": map[string]interface{}{
                                        "type":        "string",
                                        "description": "一段总结关键事件/决策/主题的段落。以 [YYYY-MM-DD HH:MM] 开头。",
                                },
                                "MemoryUpdate": map[string]interface{}{
                                        "type":        "string",
                                        "description": "完整的更新后长期记忆（markdown 格式）。",
                                },
                        },
                        "required": []string{"HistoryEntry", "MemoryUpdate"},
                })

        // ========== Profile 工具 ==========
        reg("ProfileCheck",
                "检查哪些引导（bootstrap）所需的关键信息尚未收集。返回缺失的 key 列表和建议的收集方式。",
                "profile", "extended",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        // ========== 技能管理工具 ==========
        reg("SkillList",
                "列出所有可用的技能，支持分页、过滤、搜索和排序。技能采用层次化目录结构，存储在skills/分类/技能名/SKILL.md格式。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "page": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "页码，从1开始，默认1",
                                },
                                "PageSize": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "每页数量，默认20，最大100",
                                },
                                "tags": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "标签过滤",
                                },
                                "search": map[string]interface{}{
                                        "type":        "string",
                                        "description": "全文搜索关键词",
                                },
                                "SortBy": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排序字段：name, usage, quality, last_used",
                                },
                                "SortOrder": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排序方向：asc, desc",
                                },
                                "context": map[string]interface{}{
                                        "type":        "string",
                                        "description": "当前上下文，用于智能推荐排序",
                                },
                                "SuggestOnly": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "只返回推荐技能",
                                },
                        },
                        "required": []string{},
                })

        reg("SkillCreate",
                "创建一个新的技能，采用层次化目录结构，自动生成SKILL.md文件和相关子目录。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的唯一标识符（用于目录名称）",
                                },
                                "DisplayName": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的显示名称",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的描述",
                                },
                                "SystemPrompt": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的系统提示",
                                },
                                "TriggerWords": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "触发关键词列表",
                                },
                                "tags": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "标签列表",
                                },
                                "platforms": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "支持的平台列表（windows, linux, macos）",
                                },
                        },
                        "required": []string{"name", "SystemPrompt"},
                })

        reg("SkillDelete",
                "删除指定的技能，包括其目录结构和所有关联文件。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("SkillGet",
                "获取指定技能的详细信息，包括YAML frontmatter和关联文件。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("SkillReload",
                "重新加载所有技能，包括新的层次化结构和关联文件。",
                "skill", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        reg("SkillLoad",
                "激活指定技能，使其在后续对话中生效。需要先通过 SkillList 查看可用技能名称。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("SkillUpdate",
                "更新技能的部分内容，支持YAML frontmatter和关联文件。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                                "DisplayName": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的显示名称",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的描述",
                                },
                                "SystemPrompt": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的系统提示",
                                },
                                "TriggerWords": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "触发关键词列表",
                                },
                                "tags": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "标签列表",
                                },
                                "platforms": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "支持的平台列表（windows, linux, macos）",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("SkillSuggest",
                "根据当前对话上下文智能推荐相关技能，返回技能名称、描述和匹配理由。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "context": map[string]interface{}{
                                        "type":        "string",
                                        "description": "当前对话上下文",
                                },
                                "TopK": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "返回推荐数量，默认5",
                                },
                        },
                        "required": []string{"context"},
                })

        reg("SkillStats",
                "获取技能系统的统计信息，包括层次化结构和关联文件统计。",
                "skill", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        reg("SkillEvaluate",
                "评估指定技能的质量，返回结构化评分（含准确性、完整性、实用性等维度）。需要先通过 SkillList 查看可用技能名称。",
                "skill", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                        },
                        "required": []string{"name"},
                })

        reg("ActorIdentitySet",
                "设置演员的 IDENTITY.md 文件。将内容写入 profiles/actors/<actor_name>/IDENTITY.md。",
                "profile", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "ActorName": map[string]interface{}{
                                        "type":        "string",
                                        "description": "演员名称，如 \"hero_lin\"",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "IDENTITY.md 的 Markdown 内容",
                                },
                        },
                        "required": []string{"ActorName", "content"},
                })

        reg("ActorIdentityClear",
                "删除演员的 IDENTITY.md 文件（profiles/actors/<actor_name>/IDENTITY.md）。",
                "profile", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "ActorName": map[string]interface{}{
                                        "type":        "string",
                                        "description": "演员名称",
                                },
                        },
                        "required": []string{"ActorName"},
                })

        reg("ProfileReload",
                "强制重新从磁盘加载所有 profile 文件（USER.md, SOUL.md, AGENT.md, TOOLS.md, actors/*/IDENTITY.md）。",
                "profile", "extended",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        // ========== 文本搜索工具 ==========
        reg("TextSearch",
                "全系统文本搜索。在文件中搜索关键词，返回匹配的文件路径、行号与匹配内容。支持正则表达式。未指定 root_dir 时自动从当前工作目录开始级联向上搜索（CWD → 父目录 → ... → /）。搜索中文时请务必使用正则交替匹配简繁变体，如：'中文|華文'、'华语|華語'、'汉语|漢語'、'汉字|漢字'、'软件|軟體|軟件'、'网络|網路'等，以提升命中率。",
                "core", "small",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "keyword": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索关键词或正则表达式模式。搜索中文时请使用正则交替匹配简繁变体，如 '中文|華文'、'软件|軟體' 等",
                                },
                                "RootDir": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索根目录。默认自动使用当前工作目录并在无结果时级联向上搜索，仅当需限定搜索范围时才显式指定（可选）",
                                },
                                "FilePattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "文件名模式（glob），如 '*.go', '*.txt', '*.md'（可选）",
                                },
                                "IgnoreCase": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否忽略大小写，默认 false",
                                },
                                "UseRegex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否使用正则表达式，默认 false",
                                },
                                "MaxDepth": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大搜索深度，默认 20",
                                },
                                "MaxResults": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大结果数，默认 1000",
                                },
                        },
                        "required": []string{"keyword"},
                })

        // ========== 文本替换工具（类 sed）==========
        reg("TextReplace",
                "强大的文本替换工具，类似 sed 命令。支持字符串替换、正则表达式、行范围限制、多文件操作等。可用于文本处理、内容重构、批量修改等场景。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "text": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文本（与 FilePath 二选一）",
                                },
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文件路径（与 text 二选一）",
                                },
                                "pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索模式（字符串或正则表达式）。搜索中文时建议使用正则交替匹配简繁变体，如 '中文|華文'",
                                },
                                "replacement": map[string]interface{}{
                                        "type":        "string",
                                        "description": "替换文本（为空则删除匹配内容）",
                                },
                                "OutputToFile": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输出到指定文件（可选，默认返回文本）",
                                },
                                "UseRegex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "使用正则表达式模式，默认 false",
                                },
                                "IgnoreCase": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "忽略大小写，默认 false",
                                },
                                "global": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "全局替换（替换所有匹配），默认 true",
                                },
                                "StartLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "起始行号（1-based，0表示从头），默认 0",
                                },
                                "EndLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "结束行号（0表示到末尾），默认 0",
                                },
                                "LinePattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "只处理匹配此模式的行（可选）",
                                },
                                "ExcludePattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排除匹配此模式的行（可选）",
                                },
                                "operation": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"replace", "delete", "print", "count"},
                                        "description": "操作类型：replace(替换) / delete(删除行) / print(打印匹配行) / count(计数)，默认 replace",
                                },
                                "InPlace": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "原地修改文件（仅对文件有效），默认 false",
                                },
                                "backup": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "修改前备份文件（.bak），默认 false",
                                },
                                "DryRun": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "模拟运行，不实际修改，默认 false",
                                },
                                "ShowLineNumbers": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "显示行号，默认 false",
                                },
                                "MaxReplacements": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "每行最大替换次数（0无限制），默认 0",
                                },
                        },
                        "required": []string{},
                })

        // ========== 文本搜索工具（行内搜索）==========
        reg("TextGrep",
                "在指定文件中搜索匹配的行（类似 grep）。与 TextSearch 不同：TextGrep 需要指定文件路径，TextSearch 搜索整个目录。搜索中文时建议同时匹配简繁变体。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要搜索的文件路径",
                                },
                                "pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索模式（字符串或正则表达式）",
                                },
                                "UseRegex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "使用正则表达式，默认 false",
                                },
                                "IgnoreCase": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "忽略大小写，默认 false",
                                },
                                "ShowLineNumbers": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "显示行号，默认 true",
                                },
                                "ContextLines": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "显示匹配行的上下文行数，默认 0",
                                },
                                "MaxResults": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大结果数，默认 100",
                                },
                        },
                        "required": []string{"FilePath", "pattern"},
                })

        // ========== 文本转换工具 ==========
        reg("TextTransform",
                "文本转换工具，支持大小写转换、行排序、去重、反转、添加行号等操作。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "text": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文本（与 FilePath 二选一）",
                                },
                                "FilePath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文件路径（与 text 二选一）",
                                },
                                "transform": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"uppercase", "lowercase", "trim", "sort", "unique", "reverse", "NumberLines", "RemoveEmpty"},
                                        "description": "转换类型：uppercase/lowercase(大小写) / trim(去空白) / sort(排序) / unique(去重) / reverse(反转) / NumberLines(加行号) / RemoveEmpty(移除空行)",
                                },
                                "StartLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "起始行号（可选）",
                                },
                                "EndLine": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "结束行号（可选）",
                                },
                        },
                        "required": []string{"transform"},
                })

        // ========== 后台任务管理工具 ==========

        reg("TaskCheck", "检查后台任务的状态与结果。返回任务的当前状态、已运行时间、输出内容等信息。\n\n🚫 DO NOT POLL: 不要轮询！不要频繁调用此工具检查任务状态。系统会在唤醒时间主动通知你。只有在特殊情况下才需要调用此工具。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                        },
                        "required": []string{"TaskId"},
                })

        reg("TaskTerminate",
                "终止后台运行的任务。默认使用 SIGTERM 优雅终止，设置 force=true 使用 SIGKILL 强制终止。先用 TaskList 获取任务ID。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                                "force": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否强制终止（SIGKILL），默认 false（优雅终止 SIGTERM）",
                                },
                        },
                        "required": []string{"TaskId"},
                })

        reg("TaskList",
                "列出所有后台任务，显示任务ID、命令、状态和运行时长。无参数。",
                "core", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("TaskWait", "延长后台任务的唤醒时间。\n\n🚫 DO NOT POLL: 调用此工具后，不需要轮询！系统会在新的唤醒时间主动通知你。请继续其他工作或停止，等待系统通知。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                                "WaitMinutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "继续等待的时间（分钟），最小1分钟，最大1440分钟",
                                },
                        },
                        "required": []string{"TaskId", "WaitMinutes"},
                })

        reg("TaskRemove",
                "从任务列表中移除已完成或已终止的任务。运行中的任务需要先终止才能移除。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                        },
                        "required": []string{"TaskId"},
                })

        // ========== 子代理工具 ==========
        reg("Spawn", "创建一个后台子代理执行独立任务。子代理有自己的上下文，可以独立完成复杂任务，完成后会通知你。\n\n✅ 适用场景：\n- 需要独立执行的复杂任务\n- 不需要用户交互的后台任务\n- 可以并行执行的任务\n\n❌ 限制：\n- 子代理不能创建新的子代理\n- 子代理不能发送消息给用户\n- 最多执行 15 次工具调用迭代",
                "Spawn", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务描述，清晰说明子代理需要完成的工作",
                                },
                                "MaxIterations": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大迭代次数（1-50），默认15",
                                },
                        },
                        "required": []string{"task"},
                })

        reg("SpawnCheck",
                "检查子代理任务的执行状态和结果。返回 exit_code、stdout、stderr 和运行时长。",
                "Spawn", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "子代理任务ID",
                                },
                        },
                        "required": []string{"TaskId"},
                })

        reg("SpawnList",
                "列出所有子代理任务，显示任务ID、状态、命令和运行时间。",
                "Spawn", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("SpawnCancel",
                "取消正在运行的子代理任务。任务终止后可用 SpawnCheck 查看已产出的部分结果。",
                "Spawn", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "TaskId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "子代理任务ID",
                                },
                        },
                        "required": []string{"TaskId"},
                })

        // ========== SSH 持久化连接工具 ==========
        reg("SSHConnect",
                "建立一个到远程服务器的持久化 SSH 连接。连接会保存在会话管理器中，供后续的 SSHExec 命令使用。支持密码或私钥认证。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "username": map[string]interface{}{
                                        "type":        "string",
                                        "description": "SSH 用户名",
                                },
                                "host": map[string]interface{}{
                                        "type":        "string",
                                        "description": "远程服务器地址 (IP 或域名)",
                                },
                                "password": map[string]interface{}{
                                        "type":        "string",
                                        "description": "密码（与 PrivateKeyPath 二选一）",
                                },
                                "PrivateKeyPath": map[string]interface{}{
                                        "type":        "string",
                                        "description": "私钥文件路径（与 password 二选一）",
                                },
                                "port": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "SSH 端口，默认 22",
                                },
                        },
                        "required": []string{"username", "host"},
                })

        reg("SSHExec",
                "在一个已建立的持久化 SSH 连接上执行命令。支持同步和异步模式，可以维护会话上下文（如当前目录、环境变量）。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "SessionId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "由 SSHConnect 返回的会话 ID",
                                },
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要执行的命令",
                                },
                                "async": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否异步执行（适用于长时间命令），默认 false",
                                },
                                "TimeoutSecs": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "同步命令超时时间（秒），默认 60",
                                },
                                "WakeAfterMinutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步执行时的唤醒时间（分钟），默认 5",
                                },
                        },
                        "required": []string{"SessionId", "command"},
                })

        reg("SSHList",
                "列出所有活跃的持久化 SSH 连接，显示别名、主机、用户和连接状态。",
                "core", "core",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("SSHClose",
                "关闭指定的持久化 SSH 连接并释放资源。先用 SSHList 查看连接别名。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "SessionId": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要关闭的会话 ID",
                                },
                        },
                        "required": []string{"SessionId"},
                })

        // ========== Lisp/Scheme 计算工具 ==========
        reg("SchemeEval", `执行 Clojure/Lisp (S-表达式) 并返回计算结果。

                ✅ 适用场景：
                • 精确数学计算（整数/浮点运算、三角函数、对数、幂运算等）
                • 列表处理（map, filter, reduce, cons, first, rest 等）
                • 复杂逻辑运算（递归、高阶函数、闭包）
                • 需要精确数值结果的场景（避免 LLM 浮点幻觉）

                支持的语法（Clojure 方言）：
                • 整数算术: (+ 1 2 3), (* 4 5), (- 10 3), (/ 20 4)
                • 浮点算术: (f+ 1.5 2.3), (f* 3.14 2), (f/ 10 3)
                • 数学函数: (sqrt 16), (pow 2 10), (abs -5), (sin PI), (cos 0), (log 100), (exp 1)
                • 比较: (< 1 2), (> 3 1), (>= 5 5), (<= 1 1), (= 4 4)
                • 列表操作: (map (fn [x] (* x x)) '(1 2 3)), (filter odd? '(1 2 3 4 5)), (reduce + 0 '(1 2 3))
                • 条件: (if (> x 0) "positive" "non-positive"), (cond (< x 0) "neg" (= x 0) "zero" :else "pos")
                • 定义: (defn square [x] (* x x)), (def x 42)
                • let 绑定: (let [a 10 b 20] (+ a b))
                • 常量: PI, E

                ⚠️ 每次调用创建独立的沙箱环境，不会保留上一次的变量定义。`,
                "misc", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "expression": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Clojure/Lisp S-表达式。示例: (+ 1 2 3), (defn fib [n] (if (< n 2) n (+ (fib (- n 1)) (fib (- n 2))))) (fib 10)",
                                },
                        },
                        "required":             []string{"expression"},
                        "additionalProperties": false,
                })

        // ========== Tasks 模式工具（v2：統一取代 EnterPlanMode/ExitPlanMode） ==========
        reg("Tasks",
                "結構化任務分解工具。PlanPhase 控制階段：explore(探索)→design(設計+定義Tasks)→execute(退出執行)。Tasks 定義任務列表（無依賴），每個 task 用 TodoWrite / TodoCreate / TodoUpdate / TodoDelete / TodoList 管理子任務。\n\nTasks 正確格式示例：\n{\"Tasks\": [{\"id\": \"1\", \"title\": \"SSH連接\", \"status\": \"InProgress\"}]}\n\n注意：Tasks 必須係 array of objects，每個 object 含 id（字串）、title（字串）、status（Pending/InProgress/Completed/Waiting）。",
                "plan", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "PlanPhase": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"explore", "design", "execute"},
                                        "description": "計劃階段",
                                },
                                "PlanContent": map[string]interface{}{
                                        "type":        "string",
                                        "description": "計劃內容（design 階段使用，含 Context/Approach/Verification）",
                                },
                                "Tasks": map[string]interface{}{
                                        "type":        "array",
                                        "description": "任務列表",
                                        "items": map[string]interface{}{
                                                "type":       "object",
                                                "properties": map[string]interface{}{
                                                        "id":     map[string]interface{}{"type": "string", "description": "任務唯一標識"},
                                                        "title":  map[string]interface{}{"type": "string", "description": "任務標題"},
                                                        "status": map[string]interface{}{"type": "string", "enum": []string{"Pending", "InProgress", "Completed", "Waiting"}, "description": "任務狀態"},
                                                },
                                                "required": []string{"id", "title", "status"},
                                        },
                                },
                        },
                        "required":             []string{"PlanPhase"},
                        "additionalProperties": false,
                })

        reg("EnterPlanMode",
                "進入規劃模式（等同 Tasks({\"PlanPhase\": \"design\"})）。無需先經 explore，直接開始設計方案同定義任務。",
                "plan", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "PlanContent": map[string]interface{}{
                                        "type":        "string",
                                        "description": "計劃內容（可選，可後續補充）",
                                },
                        },
                        "additionalProperties": false,
                })

        reg("ExitPlanMode",
                "強制退出 Plan/Tasks Mode。正常流程應使用 Tasks({\"PlanPhase\": \"execute\"}) 退出。",
                "plan", "extended",
                map[string]interface{}{
                        "type":                 "object",
                        "properties":           map[string]interface{}{},
                        "required":             []string{},
                        "additionalProperties": false,
                })

        // 初始化 menu 分類表（必須在所有 reg() 之後調用）
        initMenuCategories()
}

// readOnlyToolNames 只讀工具集合。模型可以安全調用呢啲工具而無需擔心副作用。
// 用於系統提示詞引導模型並行調用只讀工具，同埋輔助判斷權限。
var readOnlyToolNames = map[string]bool{
        "ReadFileLine": true, "ReadFileLines": true, "ReadFileRange": true, "FileInfo": true,
        "TextSearch": true, "TextGrep": true,
        "BrowserSearch": true, "BrowserVisit": true, "BrowserDownload": true,
        "BrowserExtractLinks": true, "BrowserExtractImages": true, "BrowserExtractElements": true,
        "BrowserGetCookies": true, "BrowserScreenshot": true, "BrowserSnapshot": true,
        "BrowserElementScreenshot": true, "BrowserPdf": true, "BrowserPdfFromFile": true,
        "MemoryRecall": true, "MemoryList": true,
        "PluginList": true, "PluginApis": true, "PluginDetail": true,
        "SkillList": true, "SkillGet": true, "SkillStats": true,
        "SkillSuggest": true, "SkillEvaluate": true, "SkillCleanupSuggest": true,
        "CredentialList": true, "SSHList": true,
        "CronList": true, "CronStatus": true,
        "TaskList": true, "TaskCheck": true,
        "ProfileCheck": true, "ProfileList": true,
        "TodoList": true,
        "SpawnList": true, "SpawnCheck": true,
}

// IsToolReadOnly 檢查工具是否為只讀（無副作用，可安全並行調用）
func IsToolReadOnly(name string) bool {
        return readOnlyToolNames[name]
}

// ============================================================
// 格式转换函数
// ============================================================

// ToOpenAI 转换为 OpenAI/Ollama 格式
func (td *ToolDef) ToOpenAI() map[string]interface{} {
        return map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                        "name":        td.Name,
                        "description": td.Description,
                        "parameters":  td.Parameters,
                },
        }
}

// ToAnthropic 转换为 Anthropic 格式
func (td *ToolDef) ToAnthropic() map[string]interface{} {
        return map[string]interface{}{
                "name":         td.Name,
                "description":  td.Description,
                "input_schema": td.Parameters,
        }
}

// ============================================================
// 集合查询函数
// ============================================================

// GetRegistryTools 返回所有已注册的工具定义（有序列表）
func GetRegistryTools() []*ToolDef {
        return toolRegistry
}

// GetRegistryTool 根据名称查找工具定义，未找到返回 nil
func GetRegistryTool(name string) *ToolDef {
        return toolRegistryMap[name]
}

// AllKnownToolNamesFromRegistry 返回注册表中所有工具名称
func AllKnownToolNamesFromRegistry() []string {
        names := make([]string, 0, len(toolRegistry))
        for _, td := range toolRegistry {
                names = append(names, td.Name)
        }
        return names
}

// ============================================================
// 分类查询函数（从注册表自动派生）
// ============================================================

// GetCategoryRegistry 从注册表自动生成工具分类列表
func GetCategoryRegistry() []ToolCategory {
        catMap := make(map[string][]string)
        catOrder := make(map[string]int)
        orderIdx := 0

        // 预定义的分类顺序和顯示名稱
        predefinedOrder := []string{"core", "file", "web", "memory", "schedule", "plan", "skill", "plugin", "profile", "Spawn", "misc"}
        categoryDisplayNames := map[string]string{
                "core":    "命令执行",
                "file":    "文件操作",
                "web":     "浏览器操作",
                "memory":  "记忆管理",
                "schedule": "任务调度",
                "plan":    "规划模式",
                "skill":   "技能管理",
                "plugin":  "插件管理",
                "profile": "配置管理",
                "Spawn":   "子代理",
                "misc":    "其他工具",
        }

        for _, td := range toolRegistry {
                cat := td.Category
                if _, exists := catMap[cat]; !exists {
                        catMap[cat] = nil
                        if _, ordered := catOrder[cat]; !ordered {
                                catOrder[cat] = orderIdx
                                orderIdx++
                        }
                }
                catMap[cat] = append(catMap[cat], td.Name)
        }

        // 按预定义顺序排列，未预定义的分类追加到末尾
        var categories []ToolCategory
        for _, catName := range predefinedOrder {
                tools, exists := catMap[catName]
                if !exists || len(tools) == 0 {
                        continue
                }
                displayName := categoryDisplayNames[catName]
                if displayName == "" {
                        displayName = catName
                }
                categories = append(categories, ToolCategory{
                        Name:        catName,
                        DisplayName: displayName,
                        Tools:       tools,
                })
        }

        // 追加未预定义的分类
        for catName, tools := range catMap {
                if catOrder[catName] >= len(predefinedOrder) {
                        displayName := categoryDisplayNames[catName]
                        if displayName == "" {
                                displayName = catName
                        }
                        categories = append(categories, ToolCategory{
                                Name:        catName,
                                DisplayName: displayName,
                                Tools:       tools,
                        })
                }
        }

        return categories
}

// ============================================================
// 层级查询函数（从注册表自动派生）
// ============================================================

// GetCoreToolNamesFromRegistry 返回所有 core 层级工具名称
// GetSmallToolNamesFromRegistry 返回精简层工具名称（仅 tier="small"）
func GetSmallToolNamesFromRegistry() []string {
        names := make([]string, 0)
        for _, td := range toolRegistry {
                if td.Tier == "small" {
                        names = append(names, td.Name)
                }
        }
        return names
}

// GetCoreToolNamesFromRegistry 返回核心层工具名称（tier="small" + tier="core"）
func GetCoreToolNamesFromRegistry() []string {
        names := make([]string, 0)
        for _, td := range toolRegistry {
                if td.Tier == "small" || td.Tier == "core" {
                        names = append(names, td.Name)
                }
        }
        return names
}

// GetExtendedToolNamesFromRegistry 返回扩展层增量工具名称（不含 core）
func GetExtendedToolNamesFromRegistry() []string {
        names := make([]string, 0)
        for _, td := range toolRegistry {
                if td.Tier == "extended" {
                        names = append(names, td.Name)
                }
        }
        return names
}
