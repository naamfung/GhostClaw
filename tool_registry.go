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
//   - Plan Mode 动态工具（next_phase, plan_write, plan_read - plan_mode.go）
//   - 合并浏览器工具（GetConsolidatedBrowserTools - tool_tier.go）
//   - MCP 动态工具（运行时加载）
//   - 记忆整合工具（GetConsolidationTools - memory_consolidator.go）
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
        reg("smart_shell", `智能执行 shell 命令，自动判断同步或异步执行模式。

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
• async: true 强制异步执行
• sync: true 强制同步执行
• timeout_secs: 异步任务最大执行时间（秒），超时自动终止并唤醒（默认无限制）
• wake_after_minutes: 异步唤醒时间（默认5分钟）

🚫 DO NOT POLL: 异步任务启动后不要轮询，系统会自动通知结果。`,
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要执行的 shell 命令",
                                },
                                "async": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "强制异步执行（可选）",
                                },
                                "sync": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "强制同步执行（可选）",
                                },
                                "timeout_secs": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步任务最大执行时间（秒），超时自动终止并唤醒（可选，默认无限制）",
                                },
                                "wake_after_minutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步唤醒时间（分钟，默认5分钟）",
                                },
                        },
                        "required":             []string{"command"},
                        "additionalProperties": false,
                })

        reg("shell", `Execute a shell command synchronously with a timeout (default 60s). This tool BLOCKS until the command completes or times out.

✅ USE THIS FOR: ls, cat, mkdir, rm, cp, mv, grep, find, echo, pwd, which, stat, date, simple git commands, and other quick operations under 60 seconds.

🚫 CRITICAL WARNING - USE shell_delayed INSTEAD:
❌ Package managers: apt, apt-get, yum, dnf, pacman, pkg (FreeBSD/GhostBSD)
❌ Compilation: make, cmake, npm install, pip install, cargo build, go build
❌ Downloads: wget, curl, git clone, rsync, scp, sftp
❌ Docker: docker build, docker-compose build
❌ System updates: apt update, yum update, pkg update, freebsd-update
❌ Archives: tar, unzip, 7z (for large files)
❌ Media: ffmpeg, handbrake
❌ Any command that MAY take more than 60 seconds

⚠️ REMOTE DAEMON STARTUP:
   - Linux: setsid /path/to/program < /dev/null > /tmp/program.log 2>&1 &
   - GhostBSD/FreeBSD: daemon -p /var/run/program.pid /path/to/program
   Otherwise the process will die when the SSH session ends.

Using 'shell' for long-running commands will cause TIMEOUT and FAIL the task!

⚠️ INTERACTIVE COMMANDS: ssh, scp, rsync, sudo, su, vim, top etc. may require interactive input and will trigger a confirmation request.`,
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The shell command to execute. For example, use 'ls' or 'ls -la' (Unix/Linux) to list files, 'mkdir test' to create a directory, 'echo hello' to print text.",
                                },
                                "force": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Set to true to bypass confirmation for potentially blocking commands (ssh, scp, sudo, etc.). Use with caution - the command will execute without user confirmation.",
                                },
                        },
                        "required":             []string{"command"},
                        "additionalProperties": false,
                })

        reg("read_file_line",
                "Read a specific line from a file. Use this when you need to read a particular line from a file without reading the entire file.",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to read.",
                                },
                                "line_num": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The line number to read (starting from 1).",
                                },
                                "verbose": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to return verbose information (line number, encoding, file size, etc.). Default: false (only returns content).",
                                },
                        },
                        "required":             []string{"filename", "line_num"},
                        "additionalProperties": false,
                })

        reg("write_file_line",
                "Write content to a specific line in a file. If the line number is beyond the current file length, the file will be extended with empty lines.",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to write to.",
                                },
                                "line_num": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The line number to write to (starting from 1).",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The content to write to the specified line.",
                                },
                        },
                        "required":             []string{"filename", "line_num", "content"},
                        "additionalProperties": false,
                })

        reg("read_all_lines",
                "Read all lines from a file and return them as a list of strings.",
                "core", "core",
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

        reg("write_all_lines",
                "Write all lines to a file.",
                "core", "core",
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

        reg("append_to_file",
                "Append content to the end of a file.",
                "file", "extended",
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
                                "line_break": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to add a line break after the content. Default is true.",
                                },
                        },
                        "required":             []string{"filename", "content"},
                        "additionalProperties": false,
                })

        reg("write_file_range",
                "Write content to a specific range of lines in a file.",
                "file", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "filename": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The path to the file to write to.",
                                },
                                "start_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The starting line number (1-based).",
                                },
                                "end_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "The ending line number (1-based). If not specified, only the start_line will be written.",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The content to write. Each line in the content will replace one line in the file range.",
                                },
                        },
                        "required":             []string{"filename", "start_line", "content"},
                        "additionalProperties": false,
                })

        // ========== 基础浏览器工具 ==========
        reg("browser_search", `Search for a keyword using Baidu search engine. Returns a list of search results with titles and links.

⚠️ PRIORITY NOTICE: If OpenCLI is available on the system, prefer using OpenCLI commands via the 'shell' tool instead of this built-in browser tool.
OpenCLI provides better session persistence and more reliable web automation.

Example OpenCLI alternative:
- shell: "opencli search <keyword>" (if adapter available)
- shell: "opencli open https://www.baidu.com/s?wd=<keyword>"`,
                "web", "expert",
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

        reg("browser_visit", `Visit a URL and extract the text content from the web page. Useful for reading article content, product descriptions, etc.

⚠️ PRIORITY NOTICE: If OpenCLI is available on the system, prefer using OpenCLI commands via the 'shell' tool instead of this built-in browser tool.
OpenCLI provides better session persistence, cookie reuse, and more reliable web automation.

Example OpenCLI alternative:
- shell: "opencli open <url>"
- shell: "opencli <adapter> <command>" (e.g., "opencli hackernews top --limit 5")`,
                "web", "expert",
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
        reg("opencli", `Execute OpenCLI commands. OpenCLI is available on this system!

✅ USE THIS FOR ALL WEB-RELATED TASKS:
• Web browsing and page reading
• Web searching (Google, Bing, etc.)
• Website automation (click, type, fill forms)
• Interacting with specific websites (YouTube, GitHub, etc.)
• Any task that would have used browser_* tools

✅ OPENCLI ADVANTAGES:
• Better session persistence
• Cookie reuse
• More reliable web automation
• Rich adapter ecosystem

⚠️ FOR DOWNLOADING FILES:
If you need to download a file (not just browse), use curl/wget via the 'shell' tool instead.

Example commands:
- opencli web read https://example.com
- opencli google search keyword
- opencli doctor
- opencli --help`,
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The OpenCLI command to execute (without 'opencli' prefix). Example: 'web read https://example.com', 'google search keyword', '--help'",
                                },
                        },
                        "required":             []string{"command"},
                        "additionalProperties": false,
                })

        reg("browser_download",
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
        reg("browser_click",
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

        reg("browser_type",
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

        reg("browser_scroll",
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

        reg("browser_wait_element",
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

        reg("browser_extract_links",
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

        reg("browser_extract_images",
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

        reg("browser_extract_elements",
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
                                "include_html": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to include HTML content. Default: false",
                                },
                        },
                        "required":             []string{"url", "selector"},
                        "additionalProperties": false,
                })

        reg("browser_screenshot",
                "Take a screenshot of a web page. Returns base64-encoded image. Can capture full page or viewport only.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "full_page": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Capture the entire page (including scrollable area) or just the viewport. Default: false (viewport only)",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("browser_execute_js",
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

        reg("browser_fill_form",
                "Fill out and submit a web form. Automatically finds input fields by name or ID attribute.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "form_data": map[string]interface{}{
                                        "type":        "object",
                                        "description": "Form field values as key-value pairs. Keys match input 'name' or 'id' attributes. Example: {\"username\": \"admin\", \"password\": \"123456\"}",
                                },
                                "submit_selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for submit button. If empty, presses Enter to submit.",
                                },
                        },
                        "required":             []string{"url", "form_data"},
                        "additionalProperties": false,
                })

        // ========== 浏览器高级工具 ==========
        reg("browser_hover",
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

        reg("browser_double_click",
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

        reg("browser_right_click",
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

        reg("browser_drag",
                "Drag an element and drop it onto another element. Useful for drag-and-drop interfaces.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "source_selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the element to drag.",
                                },
                                "target_selector": map[string]interface{}{
                                        "type":        "string",
                                        "description": "CSS selector for the drop target.",
                                },
                        },
                        "required":             []string{"url", "source_selector", "target_selector"},
                        "additionalProperties": false,
                })

        reg("browser_wait_smart",
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

        reg("browser_navigate",
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

        reg("browser_get_cookies",
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

        reg("browser_cookie_save",
                "Save cookies from a web page to a TOON file for persistence. Useful for saving login state.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to get cookies from.",
                                },
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Path to save the cookies file. If empty, uses default name like 'cookies_domain.json'.",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("browser_cookie_load",
                "Load cookies from a TOON file and apply them to a web page. Useful for restoring login state.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to apply cookies to.",
                                },
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Path to the cookies file to load.",
                                },
                        },
                        "required":             []string{"url", "file_path"},
                        "additionalProperties": false,
                })

        reg("browser_snapshot",
                "Get a simplified DOM snapshot of the page for visual analysis. Returns element tree with positions.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to analyze.",
                                },
                                "max_depth": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "Maximum depth of element tree. Default: 5",
                                },
                        },
                        "required":             []string{"url"},
                        "additionalProperties": false,
                })

        reg("browser_upload_file",
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
                                "file_paths": map[string]interface{}{
                                        "type":  "array",
                                        "items": map[string]interface{}{"type": "string"},
                                        "description": "List of file paths to upload.",
                                },
                        },
                        "required":             []string{"url", "selector", "file_paths"},
                        "additionalProperties": false,
                })

        reg("browser_select_option",
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

        reg("browser_key_press",
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

        reg("browser_element_screenshot",
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
        reg("browser_pdf",
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

        reg("browser_pdf_from_file",
                "Export a local HTML file as PDF. Useful for converting generated HTML to PDF. Returns base64 encoded PDF data.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Absolute path to the local HTML file to convert to PDF.",
                                },
                        },
                        "required":             []string{"file_path"},
                        "additionalProperties": false,
                })

        // ========== Headers 与 UA 设置 ==========
        reg("browser_set_headers",
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

        reg("browser_set_user_agent",
                "Set a custom User-Agent and navigate to a page.",
                "web", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "url": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The URL to navigate to.",
                                },
                                "user_agent": map[string]interface{}{
                                        "type":        "string",
                                        "description": "The User-Agent string to use.",
                                },
                        },
                        "required":             []string{"url", "user_agent"},
                        "additionalProperties": false,
                })

        // ========== 设备模拟 ==========
        reg("browser_emulate_device",
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
        reg("plugin_list",
                "列出所有已加载的插件及其提供的函数。",
                "plugin", "extended",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("plugin_create",
                "Create a new empty plugin skeleton. This creates a folder with the plugin name and a Lua entry file containing a basic template. Use this to start developing a new plugin.",
                "plugin", "expert",
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

        reg("plugin_load",
                "Load a new plugin from Lua code. The plugin will be saved in its own folder under plugins directory.",
                "plugin", "expert",
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

        reg("plugin_unload",
                "Unload a plugin by name (removes from memory only, files remain).",
                "plugin", "expert",
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

        reg("plugin_reload",
                "Reload a specific plugin from disk (useful after code update). This only reloads one plugin at a time, not all plugins.",
                "plugin", "expert",
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

        reg("plugin_call",
                "调用已加载插件中的函数。先用 plugin_list 查看可用函数。args 中的 items 需要指定类型信息。",
                "plugin", "extended",
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

        reg("plugin_compile",
                "Compile Lua code to bytecode (syntax check). If successful, no error; if compilation fails, returns error details. Use this to verify plugin code before loading.",
                "plugin", "expert",
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

        reg("plugin_delete",
                "Permanently delete a plugin (removes its folder and all files). Use this to completely remove a plugin.",
                "plugin", "expert",
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

        reg("plugin_apis",
                "List plugin system internal API documentation for model reference.",
                "plugin", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("plugin_detail",
                "Get detailed information about a specific plugin, including its functions, source code, and metadata.",
                "plugin", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "Plugin name to get details for.",
                                },
                                "include_source": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "Whether to include the full source code. Default: false.",
                                },
                        },
                        "required": []string{"name"},
                })

        // ========== Cron 管理工具 ==========
        reg("cron_add",
                "添加定时任务。到指定时间自动执行一条自然语言指令。参数错误时会返回正确格式，请按格式重新调用。",
                "schedule", "extended",
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
                                "user_message": map[string]interface{}{
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

        reg("cron_remove",
                "删除一个定时任务。先用 cron_list 确认任务名称。",
                "schedule", "extended",
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

        reg("cron_list",
                "列出所有已配置的定时任务（名称、排程、状态）。无参数。",
                "schedule", "extended",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("cron_status",
                "查询指定定时任务的详细状态（下次执行时间、最近执行结果等）。",
                "schedule", "expert",
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

        reg("todos",
                "管理待办事项列表（CRUD）。传入 todos 数组可批量创建/更新/删除任务。每个任务需包含 id、content、status。",
                "schedule", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "todos": map[string]interface{}{
                                        "type": "array",
                                        "items": map[string]interface{}{
                                                "type": "object",
                                                "properties": map[string]interface{}{
                                                        "id": map[string]interface{}{
                                                                "type":        "string",
                                                                "description": "任务唯一标识",
                                                        },
                                                        "content": map[string]interface{}{
                                                                "type":        "string",
                                                                "description": "任务内容",
                                                        },
                                                        "status": map[string]interface{}{
                                                                "type":        "string",
                                                                "enum":        []string{"pending", "in_progress", "completed", "waiting"},
                                                                "description": "任务状态：pending（待处理）、in_progress（进行中）、completed（已完成）、waiting（异步等待中）",
                                                        },
                                                        "priority": map[string]interface{}{
                                                                "type":        "string",
                                                                "enum":        []string{"high", "medium", "low"},
                                                                "description": "任务优先级：high（高）、medium（中）、low（低）",
                                                        },
                                                },
                                                "required": []string{"id", "content", "status"},
                                        },
                                        "description": "待办事项列表",
                                },
                                "summary": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务执行摘要（可选）",
                                },
                        },
                        "required": []string{"todos"},
                })

        // ========== 记忆管理工具 ==========
        reg("memory_save",
                "保存一条记忆到长期存储，跨会话持久化。支持分类（fact/preference/project/skill/context）和标签，便于后续检索。",
                "memory", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "key": map[string]interface{}{
                                        "type":        "string",
                                        "description": "记忆键名，如 'user_name', 'preferred_language'",
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

        reg("memory_recall",
                "检索已保存的记忆。支持按关键词模糊搜索（query）或按键名精确查找，可限定分类。无参数时返回所有记忆。",
                "core", "core",
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

        reg("memory_forget",
                "删除指定键名的记忆（不可恢复）。建议先用 memory_recall 确认要删除的记忆内容。",
                "memory", "expert",
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

        reg("memory_list",
                "列出所有已保存的记忆，支持按分类（preference/fact/project/skill/context）和范围（user/global）过滤。",
                "memory", "extended",
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

        // ========== Profile 工具 ==========
        reg("profile_check",
                "检查哪些引导（bootstrap）所需的关键信息尚未收集。返回缺失的 key 列表和建议的收集方式。",
                "profile", "extended",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        // ========== 技能管理工具 ==========
        reg("skill_list",
                "列出所有可用的技能，支持分页、过滤、搜索和排序。技能采用层次化目录结构，存储在skills/分类/技能名/SKILL.md格式。",
                "skill", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "page": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "页码，从1开始，默认1",
                                },
                                "page_size": map[string]interface{}{
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
                                "sort_by": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排序字段：name, usage, quality, last_used",
                                },
                                "sort_order": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排序方向：asc, desc",
                                },
                                "context": map[string]interface{}{
                                        "type":        "string",
                                        "description": "当前上下文，用于智能推荐排序",
                                },
                                "suggest_only": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "只返回推荐技能",
                                },
                        },
                        "required": []string{},
                })

        reg("skill_create",
                "创建一个新的技能，采用层次化目录结构，自动生成SKILL.md文件和相关子目录。",
                "skill", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的唯一标识符（用于目录名称）",
                                },
                                "display_name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的显示名称",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的描述",
                                },
                                "system_prompt": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的系统提示",
                                },
                                "trigger_words": map[string]interface{}{
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
                        "required": []string{"name", "system_prompt"},
                })

        reg("skill_delete",
                "删除指定的技能，包括其目录结构和所有关联文件。",
                "skill", "expert",
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

        reg("skill_get",
                "获取指定技能的详细信息，包括YAML frontmatter和关联文件。",
                "skill", "expert",
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

        reg("skill_reload",
                "重新加载所有技能，包括新的层次化结构和关联文件。",
                "skill", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        reg("skill_load",
                "激活指定技能，使其在后续对话中生效。需要先通过 skill_list 查看可用技能名称。",
                "skill", "extended",
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

        reg("skill_update",
                "更新技能的部分内容，支持YAML frontmatter和关联文件。",
                "skill", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的名称",
                                },
                                "display_name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的显示名称",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的描述",
                                },
                                "system_prompt": map[string]interface{}{
                                        "type":        "string",
                                        "description": "技能的系统提示",
                                },
                                "trigger_words": map[string]interface{}{
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

        reg("skill_suggest",
                "根据当前对话上下文智能推荐相关技能，返回技能名称、描述和匹配理由。",
                "skill", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "context": map[string]interface{}{
                                        "type":        "string",
                                        "description": "当前对话上下文",
                                },
                                "top_k": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "返回推荐数量，默认5",
                                },
                        },
                        "required": []string{"context"},
                })

        reg("skill_stats",
                "获取技能系统的统计信息，包括层次化结构和关联文件统计。",
                "skill", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        reg("skill_evaluate",
                "评估指定技能的质量，返回结构化评分（含准确性、完整性、实用性等维度）。需要先通过 skill_list 查看可用技能名称。",
                "skill", "expert",
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

        reg("actor_identity_set",
                "设置演员的 IDENTITY.md 文件。将内容写入 profiles/actors/<actor_name>/IDENTITY.md。",
                "profile", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "actor_name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "演员名称，如 \"hero_lin\"",
                                },
                                "content": map[string]interface{}{
                                        "type":        "string",
                                        "description": "IDENTITY.md 的 Markdown 内容",
                                },
                        },
                        "required": []string{"actor_name", "content"},
                })

        reg("actor_identity_clear",
                "删除演员的 IDENTITY.md 文件（profiles/actors/<actor_name>/IDENTITY.md）。",
                "profile", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "actor_name": map[string]interface{}{
                                        "type":        "string",
                                        "description": "演员名称",
                                },
                        },
                        "required": []string{"actor_name"},
                })

        reg("profile_reload",
                "强制重新从磁盘加载所有 profile 文件（USER.md, SOUL.md, AGENT.md, TOOLS.md, actors/*/IDENTITY.md）。",
                "profile", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                        "required":   []string{},
                })

        // ========== 文本搜索工具 ==========
        reg("text_search",
                "全系统文本搜索。在文件中搜索关键词，返回匹配的文件路径、行号与匹配内容。支持正则表达式。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "keyword": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索关键词或正则表达式模式",
                                },
                                "root_dir": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索根目录，默认为用户主目录（可选）",
                                },
                                "file_pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "文件名模式（glob），如 '*.go', '*.txt', '*.md'（可选）",
                                },
                                "ignore_case": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否忽略大小写，默认 false",
                                },
                                "use_regex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否使用正则表达式，默认 false",
                                },
                                "max_depth": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大搜索深度，默认 20",
                                },
                                "max_results": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大结果数，默认 1000",
                                },
                        },
                        "required": []string{"keyword"},
                })

        // ========== 文本替换工具（类 sed）==========
        reg("text_replace",
                "强大的文本替换工具，类似 sed 命令。支持字符串替换、正则表达式、行范围限制、多文件操作等。可用于文本处理、内容重构、批量修改等场景。",
                "file", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "text": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文本（与 file_path 二选一）",
                                },
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文件路径（与 text 二选一）",
                                },
                                "pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索模式（字符串或正则表达式）",
                                },
                                "replacement": map[string]interface{}{
                                        "type":        "string",
                                        "description": "替换文本（为空则删除匹配内容）",
                                },
                                "output_to_file": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输出到指定文件（可选，默认返回文本）",
                                },
                                "use_regex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "使用正则表达式模式，默认 false",
                                },
                                "ignore_case": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "忽略大小写，默认 false",
                                },
                                "global": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "全局替换（替换所有匹配），默认 true",
                                },
                                "start_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "起始行号（1-based，0表示从头），默认 0",
                                },
                                "end_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "结束行号（0表示到末尾），默认 0",
                                },
                                "line_pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "只处理匹配此模式的行（可选）",
                                },
                                "exclude_pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "排除匹配此模式的行（可选）",
                                },
                                "operation": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"replace", "delete", "print", "count"},
                                        "description": "操作类型：replace(替换) / delete(删除行) / print(打印匹配行) / count(计数)，默认 replace",
                                },
                                "in_place": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "原地修改文件（仅对文件有效），默认 false",
                                },
                                "backup": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "修改前备份文件（.bak），默认 false",
                                },
                                "dry_run": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "模拟运行，不实际修改，默认 false",
                                },
                                "show_line_numbers": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "显示行号，默认 false",
                                },
                                "max_replacements": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "每行最大替换次数（0无限制），默认 0",
                                },
                        },
                        "required": []string{},
                })

        // ========== 文本搜索工具（行内搜索）==========
        reg("text_grep",
                "在指定文件中搜索匹配的行（类似 grep）。与 text_search 不同：text_grep 需要指定文件路径，text_search 搜索整个目录。",
                "core", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要搜索的文件路径",
                                },
                                "pattern": map[string]interface{}{
                                        "type":        "string",
                                        "description": "搜索模式（字符串或正则表达式）",
                                },
                                "use_regex": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "使用正则表达式，默认 false",
                                },
                                "ignore_case": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "忽略大小写，默认 false",
                                },
                                "show_line_numbers": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "显示行号，默认 true",
                                },
                                "context_lines": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "显示匹配行的上下文行数，默认 0",
                                },
                                "max_results": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大结果数，默认 100",
                                },
                        },
                        "required": []string{"file_path", "pattern"},
                })

        // ========== 文本转换工具 ==========
        reg("text_transform",
                "文本转换工具，支持大小写转换、行排序、去重、反转、添加行号等操作。",
                "file", "extended",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "text": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文本（与 file_path 二选一）",
                                },
                                "file_path": map[string]interface{}{
                                        "type":        "string",
                                        "description": "输入文件路径（与 text 二选一）",
                                },
                                "transform": map[string]interface{}{
                                        "type":        "string",
                                        "enum":        []string{"uppercase", "lowercase", "trim", "sort", "unique", "reverse", "number_lines", "remove_empty"},
                                        "description": "转换类型：uppercase/lowercase(大小写) / trim(去空白) / sort(排序) / unique(去重) / reverse(反转) / number_lines(加行号) / remove_empty(移除空行)",
                                },
                                "start_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "起始行号（可选）",
                                },
                                "end_line": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "结束行号（可选）",
                                },
                        },
                        "required": []string{"transform"},
                })

        // ========== 后台任务管理工具 ==========
        reg("shell_delayed", "Execute a shell command in background with NO timeout. Use this for long-running commands that may take minutes or hours.\n\n✅ USE THIS FOR:\n• Package managers: apt/yum/dnf/pacman (Linux), pkg (FreeBSD/GhostBSD)\n• System updates: apt update, yum update, pkg update, freebsd-update, portsnap\n• Compilation: make, cmake, npm install, pip install, cargo build, go build\n• Network transfers: ssh, scp, rsync, sftp, wget, curl, git clone\n• Docker: docker build, docker-compose build\n• Archives: tar, unzip, 7z (large files)\n• Media encoding: ffmpeg, handbrake\n• Backups, long scripts, any command > 60 seconds\n\n❌ DO NOT USE THIS FOR: quick commands like ls, cat, mkdir - use 'shell' instead.\n\n⏱️ The command runs in background. You specify when to wake up (1-1440 minutes).\n\n🚫 DO NOT POLL: After starting the task, DO NOT call shell_delayed_check repeatedly. The system will automatically notify you when the task completes or wake time arrives.",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要执行的 shell 命令",
                                },
                                "wake_after_minutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "唤醒时间（分钟），最小1分钟，最大1440分钟(24小时)，默认5分钟",
                                },
                                "description": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务描述（可选，用于后续识别）",
                                },
                        },
                        "required": []string{"command"},
                })

        reg("shell_delayed_check", "检查后台任务的状态与结果。返回任务的当前状态、已运行时间、输出内容等信息。\n\n🚫 DO NOT POLL: 不要轮询！不要频繁调用此工具检查任务状态。系统会在唤醒时间主动通知你。只有在特殊情况下才需要调用此工具。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                        },
                        "required": []string{"task_id"},
                })

        reg("shell_delayed_terminate",
                "终止后台运行的任务。默认使用 SIGTERM 优雅终止，设置 force=true 使用 SIGKILL 强制终止。先用 shell_delayed_list 获取任务ID。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                                "force": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否强制终止（SIGKILL），默认 false（优雅终止 SIGTERM）",
                                },
                        },
                        "required": []string{"task_id"},
                })

        reg("shell_delayed_list",
                "列出所有后台任务，显示任务ID、命令、状态和运行时长。无参数。",
                "core", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("shell_delayed_wait", "延长后台任务的唤醒时间。\n\n🚫 DO NOT POLL: 调用此工具后，不需要轮询！系统会在新的唤醒时间主动通知你。请继续其他工作或停止，等待系统通知。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                                "wait_minutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "继续等待的时间（分钟），最小1分钟，最大1440分钟",
                                },
                        },
                        "required": []string{"task_id", "wait_minutes"},
                })

        reg("shell_delayed_remove",
                "从任务列表中移除已完成或已终止的任务。运行中的任务需要先终止才能移除。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务ID",
                                },
                        },
                        "required": []string{"task_id"},
                })

        // ========== 子代理工具 ==========
        reg("spawn", "创建一个后台子代理执行独立任务。子代理有自己的上下文，可以独立完成复杂任务，完成后会通知你。\n\n✅ 适用场景：\n- 需要独立执行的复杂任务\n- 不需要用户交互的后台任务\n- 可以并行执行的任务\n\n❌ 限制：\n- 子代理不能创建新的子代理\n- 子代理不能发送消息给用户\n- 最多执行 15 次工具调用迭代",
                "spawn", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任务描述，清晰说明子代理需要完成的工作",
                                },
                                "max_iterations": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "最大迭代次数（1-50），默认15",
                                },
                        },
                        "required": []string{"task"},
                })

        reg("spawn_check",
                "检查子代理任务的执行状态和结果。返回 exit_code、stdout、stderr 和运行时长。",
                "spawn", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "子代理任务ID",
                                },
                        },
                        "required": []string{"task_id"},
                })

        reg("spawn_list",
                "列出所有子代理任务，显示任务ID、状态、命令和运行时间。",
                "spawn", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("spawn_cancel",
                "取消正在运行的子代理任务。任务终止后可用 spawn_check 查看已产出的部分结果。",
                "spawn", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "子代理任务ID",
                                },
                        },
                        "required": []string{"task_id"},
                })

        // ========== SSH 持久化连接工具 ==========
        reg("ssh_connect",
                "建立一个到远程服务器的持久化 SSH 连接。连接会保存在会话管理器中，供后续的 ssh_exec 命令使用。支持密码或私钥认证。",
                "core", "expert",
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
                                        "description": "密码（与 private_key_path 二选一）",
                                },
                                "private_key_path": map[string]interface{}{
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

        reg("ssh_exec",
                "在一个已建立的持久化 SSH 连接上执行命令。支持同步和异步模式，可以维护会话上下文（如当前目录、环境变量）。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "session_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "由 ssh_connect 返回的会话 ID",
                                },
                                "command": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要执行的命令",
                                },
                                "async": map[string]interface{}{
                                        "type":        "boolean",
                                        "description": "是否异步执行（适用于长时间命令），默认 false",
                                },
                                "timeout_secs": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "同步命令超时时间（秒），默认 60",
                                },
                                "wake_after_minutes": map[string]interface{}{
                                        "type":        "integer",
                                        "description": "异步执行时的唤醒时间（分钟），默认 5",
                                },
                        },
                        "required": []string{"session_id", "command"},
                })

        reg("ssh_list",
                "列出所有活跃的持久化 SSH 连接，显示别名、主机、用户和连接状态。",
                "core", "expert",
                map[string]interface{}{
                        "type":       "object",
                        "properties": map[string]interface{}{},
                })

        reg("ssh_close",
                "关闭指定的持久化 SSH 连接并释放资源。先用 ssh_list 查看连接别名。",
                "core", "expert",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "session_id": map[string]interface{}{
                                        "type":        "string",
                                        "description": "要关闭的会话 ID",
                                },
                        },
                        "required": []string{"session_id"},
                })

        // ========== Lisp/Scheme 计算工具 ==========
        reg("scheme_eval", `执行 Clojure/Lisp (S-表达式) 并返回计算结果。

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
                "misc", "expert",
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

        // ========== Plan Mode 工具 ==========
        reg("enter_plan_mode",
                "進入 Plan Mode（結構化任務分解模式）。系統將啟動 5 階段工作流：探索→設計→審查→計劃→退出。每階段有獨立的工具集和 todos 列表。Phase 間需調用 next_phase 推進。",
                "plan", "core",
                map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                                "task": map[string]interface{}{
                                        "type":        "string",
                                        "description": "任務描述（可選），幫助系統理解要分解的任務",
                                },
                        },
                        "required":             []string{},
                        "additionalProperties": false,
                })

        reg("exit_plan_mode",
                "強制退出 Plan Mode（跳過剩餘階段）。正常流程應使用 next_phase 逐步推進。強制退出會丟失未完成的階段。",
                "plan", "expert",
                map[string]interface{}{
                        "type":                 "object",
                        "properties":           map[string]interface{}{},
                        "required":             []string{},
                        "additionalProperties": false,
                })

        // 初始化 menu 分類表（必須在所有 reg() 之後調用）
        initMenuCategories()
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
        predefinedOrder := []string{"core", "file", "web", "memory", "schedule", "plan", "skill", "plugin", "profile", "spawn", "misc"}
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
                "spawn":   "子代理",
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
func GetCoreToolNamesFromRegistry() []string {
        names := make([]string, 0)
        for _, td := range toolRegistry {
                if td.Tier == "core" {
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
