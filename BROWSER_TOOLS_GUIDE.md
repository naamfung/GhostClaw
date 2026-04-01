# GarClaw 浏览器工具完整指南

> 版本: v2.7.15 | 更新日期: 2026-03-28

## 概述

GarClaw 提供了一套功能完整的浏览器自动化工具，基于 [Rod](https://github.com/go-rod/rod) 浏览器自动化库实现。本文档详细介绍所有 27 个浏览器工具的使用方法、参数说明和最佳实践。

---

## 一、工具总览

### 1.1 工具分类

| 类别 | 工具数量 | 说明 |
|------|----------|------|
| **基础工具** | 3 | 搜索、访问、下载 |
| **交互操作** | 11 | 点击、输入、滚动、拖拽等 |
| **等待操作** | 2 | 元素等待、智能等待 |
| **导航操作** | 1 | 前进/后退/刷新 |
| **内容提取** | 4 | 链接、图片、元素、快照 |
| **截图/PDF** | 2 | 页面截图、元素截图 |
| **高级功能** | 4 | JS执行、Cookie、文件上传 |

### 1.2 完整工具列表

| 工具名称 | 功能描述 | 参数 |
|----------|----------|------|
| `browser_search` | 百度搜索 | `keyword` |
| `browser_visit` | 访问页面提取文本 | `url` |
| `browser_download` | 下载网页HTML | `url` |
| `browser_click` | 点击元素 | `url`, `selector` |
| `browser_double_click` | 双击元素 | `url`, `selector` |
| `browser_right_click` | 右键点击 | `url`, `selector` |
| `browser_hover` | 鼠标悬停 | `url`, `selector` |
| `browser_drag` | 拖拽元素 | `url`, `source_selector`, `target_selector` |
| `browser_type` | 输入文本 | `url`, `selector`, `text`, `submit` |
| `browser_key_press` | 模拟按键 | `url`, `keys` |
| `browser_fill_form` | 填写表单 | `url`, `form_data`, `submit_selector` |
| `browser_select_option` | 选择下拉选项 | `url`, `selector`, `values` |
| `browser_scroll` | 滚动页面 | `url`, `direction`, `amount` |
| `browser_upload_file` | 上传文件 | `url`, `selector`, `file_paths` |
| `browser_wait_element` | 等待元素出现 | `url`, `selector`, `timeout` |
| `browser_wait_smart` | 智能等待 | `url`, `selector`, `visible`, `interactable`, `stable`, `timeout` |
| `browser_navigate` | 导航操作 | `url`, `action` (back/forward/refresh) |
| `browser_extract_links` | 提取链接 | `url` |
| `browser_extract_images` | 提取图片 | `url` |
| `browser_extract_elements` | 提取元素 | `url`, `selector`, `include_html` |
| `browser_snapshot` | DOM快照 | `url`, `max_depth` |
| `browser_screenshot` | 页面截图 | `url`, `full_page` |
| `browser_element_screenshot` | 元素截图 | `url`, `selector` |
| `browser_execute_js` | 执行JS | `url`, `script` |
| `browser_get_cookies` | 获取Cookies | `url` |
| `browser_cookie_save` | 保存Cookies到TOON文件 | `url`, `file_path` |
| `browser_cookie_load` | 从TOON文件加载Cookies | `url`, `file_path` |

---

## 二、基础工具

### 2.1 browser_search - 搜索引擎搜索

使用百度搜索引擎进行关键词搜索，返回搜索结果列表。

**参数：**
- `keyword` (必需): 搜索关键词

**返回示例 (TOON格式)：**
```
Title: GarClaw - GitHub
Link: https://github.com/xxx/garclaw
---
Title: GarClaw 使用教程
Link: https://example.com/tutorial
```

**使用示例：**
```json
{
  "name": "browser_search",
  "arguments": {
    "keyword": "Go语言教程"
  }
}
```

### 2.2 browser_visit - 访问页面

访问指定URL并提取页面的文本内容，适合阅读文章、产品描述等场景。

**参数：**
- `url` (必需): 要访问的URL

**返回示例 (TOON格式)：**
```
URL: "https://example.com/article"
Title: 文章标题
Text: 这是文章的主要内容...
Length: 1234
```

**使用示例：**
```json
{
  "name": "browser_visit",
  "arguments": {
    "url": "https://example.com/article"
  }
}
```

### 2.3 browser_download - 下载网页

下载网页的完整HTML并保存到本地文件。

**参数：**
- `url` (必需): 要下载的URL

**返回示例：**
```
Browser download completed, saved to: download_20260328_123456.html
```

---

## 三、交互操作工具

### 3.1 browser_click - 点击元素

导航到页面并点击指定的元素。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器

**CSS选择器示例：**
```
button.submit      - class选择器
#login-btn         - ID选择器
a[href*='detail']  - 属性选择器
div.content > p    - 子元素选择器
```

**返回示例 (TOON格式)：**
```
Success: true
Message: 成功点击元素: button.submit
URL: "https://example.com/result"
```

### 3.2 browser_double_click - 双击元素

双击指定元素，常用于选择文本或触发双击事件。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器

### 3.3 browser_right_click - 右键点击

右键点击元素，触发上下文菜单。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器

### 3.4 browser_hover - 鼠标悬停

将鼠标悬停在指定元素上，常用于触发下拉菜单或工具提示。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器

**使用场景：**
- 触发悬停下拉菜单
- 显示工具提示 (tooltip)
- 触发悬停动画效果

### 3.5 browser_drag - 拖拽元素

将一个元素拖拽到另一个元素位置，支持拖放操作。

**参数：**
- `url` (必需): 页面URL
- `source_selector` (必需): 要拖拽的元素选择器
- `target_selector` (必需): 目标元素选择器

**返回示例 (TOON格式)：**
```
Success: true
Message: 成功将元素 '.draggable' 拖拽到 '.drop-zone'
```

### 3.6 browser_type - 输入文本

在输入框中输入文本，可选是否按回车提交。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): 输入框的CSS选择器
- `text` (必需): 要输入的文本
- `submit` (可选): 是否按回车提交，默认false

**使用示例：**
```json
{
  "name": "browser_type",
  "arguments": {
    "url": "https://example.com/search",
    "selector": "input[name='q']",
    "text": "Go语言",
    "submit": true
  }
}
```

### 3.7 browser_key_press - 模拟按键

模拟键盘按键操作，支持组合键。

**参数：**
- `url` (必需): 页面URL
- `keys` (必需): 按键数组

**支持的按键：**
```
enter, tab, escape (esc), backspace, delete
arrowup (up), arrowdown (down), arrowleft (left), arrowright (right)
control (ctrl), alt, shift, meta (cmd, command)
单个字符: a, b, c, 1, 2, 3...
```

**使用示例：**
```json
{
  "name": "browser_key_press",
  "arguments": {
    "url": "https://example.com",
    "keys": ["control", "a"]
  }
}
```

### 3.8 browser_fill_form - 填写表单

自动填写并提交表单，根据字段名或ID自动匹配输入框。

**参数：**
- `url` (必需): 页面URL
- `form_data` (必需): 表单数据对象
- `submit_selector` (可选): 提交按钮选择器，为空则按回车

**使用示例：**
```json
{
  "name": "browser_fill_form",
  "arguments": {
    "url": "https://example.com/login",
    "form_data": {
      "username": "admin",
      "password": "123456",
      "remember": "true"
    },
    "submit_selector": "button[type='submit']"
  }
}
```

### 3.9 browser_select_option - 选择下拉选项

操作下拉框 (select) 元素，选择一个或多个选项。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): 下拉框选择器
- `values` (必需): 要选择的值数组

**使用示例：**
```json
{
  "name": "browser_select_option",
  "arguments": {
    "url": "https://example.com/form",
    "selector": "select[name='country']",
    "values": ["china", "japan"]
  }
}
```

### 3.10 browser_scroll - 滚动页面

滚动页面到指定方向和距离。

**参数：**
- `url` (必需): 页面URL
- `direction` (必需): 滚动方向，值为 `up` 或 `down`
- `amount` (可选): 滚动像素数，默认500

**使用示例：**
```json
{
  "name": "browser_scroll",
  "arguments": {
    "url": "https://example.com/long-page",
    "direction": "down",
    "amount": 1000
  }
}
```

### 3.11 browser_upload_file - 上传文件

上传文件到文件输入框 (input[type=file])。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): 文件输入框选择器
- `file_paths` (必需): 文件路径数组

**使用示例：**
```json
{
  "name": "browser_upload_file",
  "arguments": {
    "url": "https://example.com/upload",
    "selector": "input[type='file']",
    "file_paths": ["/home/user/document.pdf", "/home/user/image.png"]
  }
}
```

---

## 四、等待操作工具

### 4.1 browser_wait_element - 等待元素

等待指定元素出现在页面上。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器
- `timeout` (可选): 超时秒数，默认10

### 4.2 browser_wait_smart - 智能等待

结合多种等待策略，更可靠地等待元素就绪。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器
- `visible` (可选): 等待元素可见，默认true
- `interactable` (可选): 等待元素可交互，默认false
- `stable` (可选): 等待元素稳定（无动画），默认false
- `timeout` (可选): 超时秒数，默认10

**使用示例：**
```json
{
  "name": "browser_wait_smart",
  "arguments": {
    "url": "https://example.com/dynamic",
    "selector": ".result-item",
    "visible": true,
    "interactable": true,
    "stable": true,
    "timeout": 15
  }
}
```

---

## 五、导航操作工具

### 5.1 browser_navigate - 导航操作

执行浏览器的前进、后退、刷新操作。

**参数：**
- `url` (必需): 页面URL
- `action` (必需): 操作类型，值为 `back`、`forward` 或 `refresh`

**使用示例：**
```json
{
  "name": "browser_navigate",
  "arguments": {
    "url": "https://example.com/page",
    "action": "back"
  }
}
```

---

## 六、内容提取工具

### 6.1 browser_extract_links - 提取链接

提取页面中的所有链接。

**参数：**
- `url` (必需): 页面URL

**返回示例 (TOON格式)：**
```
URL: "https://example.com"
Count: 5
Links[5]{text,href}:
  首页,https://example.com
  关于我们,https://example.com/about
  产品中心,https://example.com/products
  联系方式,https://example.com/contact
  博客,https://example.com/blog
```

### 6.2 browser_extract_images - 提取图片

提取页面中的所有图片信息。

**参数：**
- `url` (必需): 页面URL

**返回示例 (TOON格式)：**
```
URL: "https://example.com"
Count: 3
Images[3]{src,alt}:
  "https://example.com/img/logo.png",Logo
  "https://example.com/img/banner.jpg",Banner
  "https://example.com/img/product.png",产品图
```

### 6.3 browser_extract_elements - 提取元素

提取指定选择器的所有元素内容。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器
- `include_html` (可选): 是否包含HTML，默认false

**使用示例：**
```json
{
  "name": "browser_extract_elements",
  "arguments": {
    "url": "https://example.com/news",
    "selector": ".article-item",
    "include_html": false
  }
}
```

### 6.4 browser_snapshot - DOM快照

获取页面的简化DOM结构，用于视觉分析和元素定位。

**参数：**
- `url` (必需): 页面URL
- `max_depth` (可选): 最大嵌套深度，默认5

**返回示例 (TOON格式)：**
```
URL: "https://example.com"
Title: Example Site
Snapshot:
  Tag: body
  Text: ""
  Children[2]:
    - Tag: header
      Text: ""
      Children[1]:
        - Tag: nav
          ...
    - Tag: main
      ...
```

---

## 七、截图工具

### 7.1 browser_screenshot - 页面截图

截取页面图片，支持全页截图。

**参数：**
- `url` (必需): 页面URL
- `full_page` (可选): 是否截取整个页面（包括滚动区域），默认false

**返回示例 (TOON格式)：**
```
URL: "https://example.com"
Success: true
Base64: "iVBORw0KGgoAAAANSUhEUgAAA..."
FullPage: true
Width: 1920
Height: 5000
```

### 7.2 browser_element_screenshot - 元素截图

截取指定元素的图片。

**参数：**
- `url` (必需): 页面URL
- `selector` (必需): CSS选择器

---

## 八、高级功能工具

### 8.1 browser_execute_js - 执行JavaScript

在页面中执行自定义JavaScript代码。

**参数：**
- `url` (必需): 页面URL
- `script` (必需): JavaScript代码（函数表达式）

**使用示例：**
```json
{
  "name": "browser_execute_js",
  "arguments": {
    "url": "https://example.com",
    "script": "() => { return {url: location.href, title: document.title}; }"
  }
}
```

### 8.2 browser_get_cookies - 获取Cookies

获取页面的所有Cookies。

**参数：**
- `url` (必需): 页面URL

**返回示例 (TOON格式)：**
```
URL: "https://example.com"
Count: 2
Cookies[2]{name,value,domain,path}:
  session_id,abc123,.example.com,/
  user_token,xyz789,.example.com,/
```

### 8.3 browser_cookie_save - 保存Cookies

将页面的Cookies保存到TOON格式文件，用于持久化登录态。

**参数：**
- `url` (必需): 页面URL
- `file_path` (可选): 保存文件路径，不指定则自动生成 `cookies_{domain}.toon`

**使用示例：**
```json
{
  "name": "browser_cookie_save",
  "arguments": {
    "url": "https://example.com/dashboard",
    "file_path": "my_cookies.toon"
  }
}
```

**返回示例 (TOON格式)：**
```
Success: true
Message: 成功保存 5 个 Cookies
Count: 5
File: my_cookies.toon
```

### 8.4 browser_cookie_load - 加载Cookies

从TOON文件加载Cookies并应用到页面，恢复登录态。

**参数：**
- `url` (必需): 要应用Cookie的目标页面URL
- `file_path` (必需): Cookie文件路径

**使用示例：**
```json
{
  "name": "browser_cookie_load",
  "arguments": {
    "url": "https://example.com/dashboard",
    "file_path": "my_cookies.toon"
  }
}
```

**返回示例 (TOON格式)：**
```
Success: true
Message: 成功加载 5 个 Cookies 并应用到页面
Count: 5
File: my_cookies.toon
URL: "https://example.com/dashboard"
```

---

## 九、Cookie 持久化详解

### 9.1 TOON 格式说明

Cookie 文件使用 TOON (Token-Oriented Object Notation) 格式存储，比 JSON 更节省 Token。

**TOON 格式示例：**
```
Name: session_id
Value: abc123def456
Domain: .example.com
Path: /
Expires: 1735689600
HTTPOnly: true
Secure: true
SameSite: Lax
---
Name: user_token
Value: xyz789
Domain: .example.com
Path: /
Expires: 1735689600
HTTPOnly: true
Secure: true
SameSite: Strict
```

### 9.2 Cookie 持久化工作流程

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   登录页面      │────▶│  browser_cookie │────▶│  保存 TOON 文件 │
│  (首次登录)     │     │     _save       │     │  cookies.toon   │
└─────────────────┘     └─────────────────┘     └─────────────────┘

┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   访问页面      │────▶│  browser_cookie │────▶│   自动登录      │
│  (后续访问)     │     │     _load       │     │   恢复会话      │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

### 9.3 最佳实践

1. **登录后立即保存Cookie**
```json
// 1. 填写登录表单
browser_fill_form(url="https://example.com/login", form_data={...})

// 2. 保存Cookie
browser_cookie_save(url="https://example.com")
```

2. **访问需要登录的页面时加载Cookie**
```json
// 1. 加载Cookie
browser_cookie_load(url="https://example.com/dashboard", file_path="cookies_example_com.toon")

// 2. 访问受保护页面
browser_visit(url="https://example.com/dashboard")
```

3. **定期更新Cookie**
```json
// Cookie即将过期时重新保存
browser_cookie_save(url="https://example.com", file_path="cookies_example_com.toon")
```

---

## 十、浏览器会话管理

### 10.1 会话管理器架构

GarClaw 内置了浏览器会话管理器，支持：

- **会话复用**：避免每次操作重新启动浏览器
- **多标签页管理**：在同一个浏览器实例中管理多个页面
- **Cookie 持久化**：保存和恢复登录状态

**架构图：**
```
┌────────────────────────────────────────────────────┐
│              BrowserSessionManager                 │
├────────────────────────────────────────────────────┤
│  sessions: map[string]*BrowserSession              │
│                                                    │
│  ┌──────────────┐  ┌──────────────┐               │
│  │  Session 1   │  │  Session 2   │  ...          │
│  │  ┌────────┐  │  │  ┌────────┐  │               │
│  │  │ Page A │  │  │  │ Page X │  │               │
│  │  │ Page B │  │  │  │ Page Y │  │               │
│  │  └────────┘  │  │  └────────┘  │               │
│  └──────────────┘  └──────────────┘               │
└────────────────────────────────────────────────────┘
```

### 10.2 内部API

会话管理器提供以下内部API（供高级用户使用）：

```go
// 获取会话管理器
mgr := GetBrowserSessionManager()

// 创建会话
session, _ := mgr.CreateSession("my-session")

// 创建页面
page, _ := session.CreatePage("page-1", "https://example.com")

// 切换页面
session.SetActivePage("page-2")

// 关闭页面
session.ClosePage("page-1")

// 关闭会话
mgr.CloseSession("my-session")
```

---

## 十一、错误处理

### 11.1 BrowserError 结构

所有浏览器错误都使用统一的 `BrowserError` 结构：

```go
type BrowserError struct {
    Op      string  // 操作名称
    Err     error   // 原始错误
    Timeout int     // 超时时间(秒)，仅超时错误设置
}
```

### 11.2 常见错误类型

| 错误类型 | 说明 | 解决方案 |
|----------|------|----------|
| 导航失败 | 无法访问URL | 检查URL是否正确、网络是否可用 |
| 未找到元素 | CSS选择器无匹配 | 检查选择器、等待页面加载完成 |
| 超时错误 | 操作超时 | 增加timeout参数、优化网络 |
| Cookie文件不存在 | 加载Cookie失败 | 先使用browser_cookie_save保存 |

### 11.3 错误示例

**超时错误：**
```
浏览器操作失败 [BrowserVisit]: context deadline exceeded (超时设置: 30秒)
```

**元素未找到：**
```
未找到元素 '.non-existent-class': cannot find element
```

---

## 十二、CSS选择器快速参考

### 12.1 基本选择器

| 选择器 | 示例 | 说明 |
|--------|------|------|
| 元素 | `div` | 选择所有div元素 |
| 类 | `.btn-primary` | 选择class包含btn-primary的元素 |
| ID | `#login-form` | 选择id为login-form的元素 |
| 属性 | `[type='submit']` | 选择type属性为submit的元素 |

### 12.2 组合选择器

| 选择器 | 示例 | 说明 |
|--------|------|------|
| 后代 | `div p` | div内的所有p元素 |
| 子元素 | `ul > li` | ul的直接子元素li |
| 相邻兄弟 | `h1 + p` | h1后面紧邻的p元素 |
| 通用兄弟 | `h1 ~ p` | h1后面的所有p元素 |

### 12.3 属性选择器

| 选择器 | 示例 | 说明 |
|--------|------|------|
| 包含 | `[href]` | 有href属性的元素 |
| 等于 | `[type='text']` | type等于text |
| 包含词 | `[class~='btn']` | class包含btn这个词 |
| 前缀 | `[class^='btn']` | class以btn开头 |
| 后缀 | `[href$='.pdf']` | href以.pdf结尾 |
| 包含字符串 | `[href*='example']` | href包含example |

### 12.4 伪类选择器

| 选择器 | 示例 | 说明 |
|--------|------|------|
| 第一个子元素 | `li:first-child` | 第一个li |
| 最后一个子元素 | `li:last-child` | 最后一个li |
| 第N个子元素 | `tr:nth-child(2)` | 第2个tr |
| 奇偶 | `tr:nth-child(odd)` | 奇数行tr |

---

## 十三、最佳实践

### 13.1 性能优化

1. **使用智能等待代替固定等待**
```json
// ❌ 不推荐：固定等待
browser_wait_element(url="...", selector=".item", timeout=30)

// ✅ 推荐：智能等待
browser_wait_smart(url="...", selector=".item", visible=true, interactable=true)
```

2. **批量操作时复用会话**
```
虽然当前工具每次都会创建新浏览器实例，但可以连续调用多个工具来减少总开销。
```

3. **合理设置超时**
```json
// 复杂页面使用更长超时
browser_visit(url="...", timeout=60)
```

### 13.2 可靠性建议

1. **先等待再操作**
```json
// 1. 等待元素出现
browser_wait_smart(url="...", selector=".submit-btn", visible=true, interactable=true)

// 2. 执行点击
browser_click(url="...", selector=".submit-btn")
```

2. **处理动态内容**
```json
// 使用更通用的选择器
"div[data-testid='submit-button']"  // 优于 "button.btn.btn-primary.submit"
```

3. **错误重试**
```
对于可能失败的操作，实现重试逻辑
```

### 13.3 安全建议

1. **Cookie文件安全**
```
- 不要在公共位置保存Cookie文件
- 定期清理过期的Cookie文件
- 敏感网站使用后清除Cookie
```

2. **URL验证**
```
- 所有URL都经过安全验证
- 只允许 http/https 协议
- 阻止访问内网地址（可选配置）
```

---

## 十四、实现状态

### 14.1 已实现功能

| 功能 | 状态 | 说明 |
|------|------|------|
| 浏览器会话管理器 | ✅ | `browser_session.go` |
| Cookie 持久化 (TOON) | ✅ | `browser_cookie_save`, `browser_cookie_load` |
| 智能等待策略 | ✅ | `browser_wait_smart` |
| 多标签页管理 | ✅ | 会话管理器支持 |
| 文件上传 | ✅ | `browser_upload_file` |
| iframe 操作 | ✅ | `BrowserIframeEnter` |
| 对话框处理 | ✅ | `BrowserHandleDialog` |
| 高级交互操作 | ✅ | 悬停、双击、右键、拖拽 |
| PDF导出 | ⚠️ | 后端已实现，未暴露为工具 |

### 14.2 待实现功能

| 功能 | 优先级 | 说明 |
|------|--------|------|
| 设备模拟 | P2 | 模拟手机/平板访问 |
| 网络请求拦截 | P2 | 拦截/修改HTTP请求 |
| 隐身模式 | P2 | 创建隔离的浏览上下文 |

---

## 十五、更新日志

### v2.7.15 (2026-03-28)

**新增功能：**
- 新增 `browser_cookie_save` 和 `browser_cookie_load` 工具
- Cookie 文件使用 TOON 格式存储
- 新增 `browser_wait_smart` 智能等待工具
- 新增 `browser_hover`、`browser_double_click`、`browser_right_click` 高级交互工具
- 新增 `browser_drag` 拖拽工具
- 新增 `browser_navigate` 导航工具
- 新增 `browser_upload_file` 文件上传工具
- 新增 `browser_key_press` 按键模拟工具
- 新增 `browser_select_option` 下拉框选择工具
- 新增 `browser_element_screenshot` 元素截图工具
- 新增 `browser_snapshot` DOM快照工具
- 新增 `BrowserIframeEnter` iframe 操作
- 新增 `BrowserHandleDialog` 对话框处理

**改进：**
- 所有工具返回改为 TOON 格式
- 统一错误处理机制
- 添加浏览器会话管理器

**文件结构：**
```
browser_session.go        - 浏览器会话管理器
browser_tools.go          - 基础交互工具
browser_tools_advanced.go - 高级浏览器工具
services.go               - 基础服务（搜索、访问、下载）
getTools.go               - 工具定义
AgentLoop.go              - 工具调用处理
```

---

## 十六、参考资料

- [Rod 官方文档](https://github.com/go-rod/rod)
- [TOON 格式规范](./garclaw/skills/toon_format.md)
- [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/)
