# OpenCLI 完整指南

## 描述
专业的 OpenCLI 安装、配置与使用指南。提供详细的安装步骤、工具使用优先级规范、丰富的命令示例以及常见问题解决方案。当用户要求获取网站数据或进行网页操作时，必须优先使用 OpenCLI 而非内置浏览器工具。

## 标签
- opencli
- 安装
- 命令行工具
- bun
- pnpm
- npm
- 浏览器自动化
- CLI中心
- 工具优先级
- 强制使用

## 触发关键词
- opencli
- opencli安装
- 安装opencli
- opencli setup
- opencli怎么安装
- opencli使用
- opencli教程
- opencli指南
- 获取网站数据
- 网页数据
- 爬取网站
- 网站内容
- 网页操作
- 浏览器控制
- 用opencli
- opencli获取

## 系统提示
你系一个严格遵守工具使用规范的 OpenCLI 专家助手。**当用户要求获取网站数据或进行网页操作时，你必须优先使用 OpenCLI 工具，绝对禁止直接使用内置的 browser_search 或 browser_visit 等浏览器工具。**

### 工具使用优先级（必须遵守）

**第一优先级：OpenCLI 工具**
- 当用户要求获取任何网站数据时，首先检查 OpenCLI 命令是否可用
- 使用内置 shell 类命令执行 `opencli` 相关操作
- 所有网页访问、数据获取任务优先通过 OpenCLI 完成

**第二优先级：内置浏览器工具**
- 仅当 OpenCLI 明确无法使用或用户指定时才使用内置 browser 工具
- 必须在使用内置工具前说明为何不使用 OpenCLI

### 强制规则

⚠️ **绝对禁止的行为**：
- 用户要求获取网站数据时，直接调用 `browser_visit` 或 `browser_search`
- 未检查 OpenCLI 可用性就使用内置浏览器工具
- 忽视用户明确指定的 OpenCLI 请求

✅ **必须执行的行为**：
- 首先尝试使用 `shell` 工具执行 opencli 命令
- 如果 OpenCLI 未安装，主动提出安装建议
- 记录 OpenCLI 执行结果，避免重复失败

### 错误处理流程

如果 OpenCLI 执行失败：
1. 分析错误原因（扩展未连接、未登录、命令错误等）
2. 尝试修复问题（提示用户安装扩展、登录网站等）
3. 仅当 OpenCLI 确实无法使用时，才回退到内置工具

## 安装指南

### 安装优先级（重要）
1. **首选：bun安装**（最快、最现代的包管理器）
2. **次选：pnpm安装**（高效、快速的包管理器）  
3. **最后：原生npm/npx安装**（最通用的方法）

### 核心功能
- 将任何网站、Electron应用或本地工具转换为CLI
- 支持浏览器会话重用（Chrome/Chromium登录状态）
- AI驱动的发现功能
- 通用CLI中心
- 支持 Electron 应用控制
- 73+个预建适配器（Bilibili、知乎、小红书、Twitter/X、Reddit、YouTube等）

### 安装步骤

#### 1. 安装Browser Bridge扩展
- 下载最新版本的opencli-extension.zip：
  ```bash
  curl -L -o opencli-extension.zip https://github.com/jackwener/opencli/releases/latest/download/opencli-extension.zip
  ```
- 或使用指定版本：
  ```bash
  curl -L -o opencli-extension.zip https://github.com/jackwener/opencli/releases/download/v1.6.1/opencli-extension.zip
  ```
- 或从 GhostClaw 技能目录获取自带版本：`skills/opencli/opencli-extension.zip`
- 解压并在 `chrome://extensions` 中加载（需开启开发者模式）

#### 2. 安装OpenCLI（按优先级）

**bun安装（推荐）**：
```bash
bun install -g @jackwener/opencli
```

**pnpm安装**：
```bash
pnpm add -g @jackwener/opencli
```

**npm安装**：
```bash
npm install -g @jackwener/opencli
```

#### 3. 验证安装
```bash
opencli doctor          # 检查扩展+守护进程连接
opencli daemon status   # 检查守护进程状态
opencli --version       # 查看版本
```

#### 4. 更新OpenCLI
```bash
# 根据安装方式选择对应的更新命令
bun install -g --trust @jackwener/opencli
pnpm add -g @jackwener/opencli
npm install -g @jackwener/opencli@latest
```

## 命令使用示例

### 基础命令

```bash
# 查看所有可用命令
opencli list

# 查看帮助
opencli --help
opencli <命令> --help

# 检查系统状态
opencli doctor
```

### 网站适配器命令

#### 技术资讯类
```bash
# Hacker News - 公共API，无需浏览器
opencli hackernews top --limit 10
opencli hackernews new --limit 5
opencli hackernews show --limit 5
opencli hackernews ask --limit 5
opencli hackernews job --limit 5

# GitHub Trending
opencli github trending --language go --period daily
opencli github trending --language python --period weekly

# Product Hunt
opencli producthunt top --limit 10
```

#### 中文社区类
```bash
# Bilibili - 需要浏览器扩展
opencli bilibili hot --limit 10
opencli bilibili rank --limit 10
opencli bilibili search "关键词" --limit 5

# 知乎 - 需要浏览器扩展
opencli zhihu hot --limit 10
opencli zhihu search "关键词" --limit 5

# 小红书 - 需要浏览器扩展
opencli xiaohongshu hot --limit 10
opencli xiaohongshu search "关键词" --limit 5

# 微博热搜
opencli weibo hot --limit 10

# 百度热搜
opencli baidu hot --limit 10
```

#### 国际社交媒体
```bash
# Twitter/X - 需要浏览器扩展且已登录
opencli twitter home --limit 10
opencli twitter search "关键词" --limit 10
opencli twitter user elonmusk --limit 10

# Reddit - 需要浏览器扩展
opencli reddit hot --subreddit programming --limit 10
opencli reddit top --subreddit technology --limit 5
opencli reddit new --subreddit golang --limit 5

# YouTube - 需要浏览器扩展
opencli youtube trending --limit 10
opencli youtube search "关键词" --limit 5
```

#### 开发工具类
```bash
# npm 包搜索
opencli npm search "lodash" --limit 5

# Docker Hub
opencli dockerhub search "nginx" --limit 5

# Stack Overflow
opencli stackoverflow search "golang goroutine" --limit 5
```

### 浏览器自动化命令

#### 基础页面操作
```bash
# 打开网页
opencli open https://example.com

# 获取页面文本内容
opencli text

# 获取页面HTML
opencli html

# 截图
opencli screenshot --full-page
opencli screenshot --selector "#main-content"

# 导出PDF
opencli pdf
```

#### 元素交互
```bash
# 点击元素
opencli click "#login-button"
opencli click ".submit-btn"
opencli click "//button[contains(text(),'提交')]"

# 输入文本
opencli type "#username" "your-email@example.com"
opencli type "#password" "your-password"
opencli type "#search-box" "搜索关键词" --submit

# 清除输入框并输入
opencli type "#search" "" --clear
opencli type "#search" "新内容"

# 下拉选择
opencli select "#country" "China"
opencli select "#multi-select" "option1,option2,option3"
```

#### 页面导航
```bash
# 滚动页面
opencli scroll down --amount 500
opencli scroll up --amount 300
opencli scroll to-bottom
opencli scroll to-top

# 前进/后退/刷新
opencli navigate back
opencli navigate forward
opencli navigate refresh
```

#### 高级操作
```bash
# 等待元素出现
opencli wait "#dynamic-content" --timeout 10

# 智能等待（等待元素可见且可交互）
opencli wait-smart "#submit-btn" --visible --interactable --timeout 10

# 悬停
opencli hover "#dropdown-menu"

# 右键点击
opencli right-click "#context-menu-target"

# 双击
opencli double-click "#editable-area"

# 拖拽
opencli drag "#source-element" "#target-element"

# 执行JavaScript
opencli execute "() => document.title"
opencli execute "() => { return { url: location.href, title: document.title }; }"

# 上传文件
opencli upload "#file-input" "/path/to/file.pdf"

# 按键操作
opencli keypress "Enter"
opencli keypress "Control+c"
opencli keypress "ArrowDown,ArrowDown,Enter"
```

#### Cookie管理
```bash
# 获取当前页面cookies
opencli cookies

# 保存cookies到文件
opencli cookie-save --file mycookies.json

# 加载cookies
opencli cookie-load --file mycookies.json
```

### 完整自动化流程示例

#### 自动登录流程
```bash
opencli open https://github.com/login
opencli type "#login_field" "your-username"
opencli type "#password" "your-password"
opencli click "input[name='commit']"
opencli wait "#dashboard" --timeout 10
```

#### 搜索并获取结果
```bash
opencli open https://www.google.com
opencli type "[name='q']" "OpenCLI GitHub" --submit
opencli wait "#search" --timeout 5
opencli text
```

#### 表单填写
```bash
opencli open https://example.com/form
opencli type "#name" "张三"
opencli type "#email" "zhangsan@example.com"
opencli select "#gender" "male"
opencli click "#agree-terms"
opencli click "#submit-btn"
```

### CLI中心功能

```bash
# 注册本地CLI工具
opencli register mycli

# 使用已注册的CLI
opencli gh pr list --limit 5           # GitHub CLI
opencli docker ps                      # Docker
opencli kubectl get pods               # Kubernetes
opencli aws s3 ls                      # AWS CLI
```

### 实用技巧

#### 链式操作
```bash
# 使用管道连接多个命令
opencli open https://example.com | opencli extract-links

# 结合其他工具使用
opencli hackernews top --limit 5 | jq '.[].title'
```

#### 输出格式化
```bash
# JSON输出
opencli hackernews top --limit 5 --json

# 仅获取特定字段
opencli hackernews top --limit 5 --fields title,url
```

#### 调试模式
```bash
# 启用详细输出
opencli --verbose open https://example.com

# 显示浏览器操作
opencli --debug click "#button"
```

## 输出格式规范

当需要使用 OpenCLI 时，输出格式如下：
1. 说明将使用 OpenCLI 执行任务
2. 执行具体的 opencli 命令
3. 返回执行结果或错误信息

### 示例对话

**场景1：获取热门资讯**
```
用户：获取 Hacker News 的热门文章
助手：我将使用 OpenCLI 为你获取 Hacker News 的热门文章。

执行命令：
opencli hackernews top --limit 10

结果：
[返回文章列表...]
```

**场景2：网页自动化**
```
用户：访问 example.com 并获取内容
助手：我将使用 OpenCLI 访问该网站。

执行命令：
opencli open https://example.com

结果：
[返回页面内容...]
```

**场景3：复杂操作**
```
用户：用 opencli 获取 b站热门视频
助手：我将使用 OpenCLI 获取 Bilibili 热门视频列表。

执行命令：
opencli bilibili hot --limit 5

结果：
[返回视频列表...]
```

**场景4：搜索操作**
```
用户：访问 github.com 并搜索 "opencli"
助手：我将使用 OpenCLI 访问 GitHub 并执行搜索。

执行命令：
opencli open https://github.com
opencli type "[name='q']" "opencli"
opencli click "[type='submit']"
opencli wait "#js-pjax-container" --timeout 5

结果：
[返回搜索结果页面内容...]
```

## 常见问题解决

### 扩展相关问题
- **"Extension not connected"**：确保Browser Bridge扩展已安装并启用，且Chrome正在运行
- **"Empty data or 'Unauthorized'"**：检查Chrome/Chromium登录状态，必须先登录目标网站
- **扩展无法加载**：确保在 `chrome://extensions` 中开启了开发者模式

### 安装相关问题
- **"Node API errors"**：确保Node.js >= 20或Bun >= 1.0
- **权限错误**：尝试使用 `sudo`（Linux/Mac）或以管理员身份运行（Windows）
- **命令找不到**：检查全局npm包路径是否已添加到系统PATH

### 命令执行问题
- **页面加载超时**：增加 `--timeout` 参数，如 `opencli wait "#element" --timeout 30`
- **元素找不到**：检查选择器是否正确，使用浏览器开发者工具验证
- **操作被拒绝**：某些网站有反爬虫机制，尝试添加延迟或使用 `--headful` 模式

### 其他问题
- **Cookie未保留**：确保使用 `opencli cookie-save` 和 `opencli cookie-load` 管理会话
- **页面跳转后丢失上下文**：使用 `opencli wait` 等待新页面加载完成

## 注意事项

⚠️ **重要提醒**：
1. Browser命令重用你的Chrome/Chromium登录会话。你必须先登录到目标网站。如果得到空数据或错误，请先检查登录状态。
2. 频繁操作可能导致IP被暂时封禁，请适当添加延迟。
3. 敏感操作（如登录、支付）请在安全环境下进行。
4. 遵守目标网站的使用条款和 robots.txt 规定。

## 相关资源

- GitHub仓库：https://github.com/jackwener/opencli
- 官方文档：https://github.com/jackwener/opencli#readme
- 适配器列表：`opencli list`
