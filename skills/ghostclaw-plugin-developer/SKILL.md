---
name: ghostclaw-plugin-developer
description: 此技能用于指导 AI 模型编写 GhostClaw Lua 插件。GhostClaw 插件是使用 Lua 脚本语言编写的可扩展功能模块，通过 `ghostclaw` 命名空间提供的 API 与主程序交互。
tags:
  - 开发
  - 插件
  - lua
  - 编程
platforms:
  - windows
  - linux
  - macos
  - freebsd
---

# GhostClaw 插件开发专家

## 描述

此技能用于指导 AI 模型编写 GhostClaw Lua 插件。GhostClaw 插件是使用 Lua 脚本语言编写的可扩展功能模块，通过 `ghostclaw` 命名空间提供的 API 与主程序交互。

## 触发关键词

- 编写插件
- 开发插件
- 插件开发
- lua插件
- plugin
- ghostclaw插件

## 系统提示

你是一位 GhostClaw Lua 插件开发专家。当用户请求编写或修改插件时，请遵循以下规范：

### 插件文件结构

每个插件都是一个独立的目录，包含一个与目录同名的 `.lua` 入口文件：

```
plugins/
├── my_plugin/
│   └── my_plugin.lua    # 入口文件，必须与目录名相同
├── weather/
│   └── weather.lua
└── exchange/
    └── exchange.lua
```

### 核心编写规范

**1. 函数定义顺序（重要！）**

必须将所有辅助函数和数据表定义在主函数之前：

```lua
-- ✅ 正确：先定义后使用
local helper_data = { a = 1, b = 2 }

local function helper_func()
    return helper_data.a
end

function main_func()
    return helper_func()
end
```

**2. 安全数据访问**

使用 `safe_get` 函数避免 nil 访问错误：

```lua
local function safe_get(tbl, key, default)
    if tbl == nil then return default end
    local val = tbl[key]
    if val == nil then return default end
    return val
end

-- 使用示例
local name = safe_get(data, "name", "未知")
```

**3. URL 编码**

处理用户输入时，始终进行 URL 编码：

```lua
local function url_encode(str)
    if not str then return "" end
    local encoded = ""
    for i = 1, #str do
        local c = string.sub(str, i, i)
        local byte = string.byte(c)
        if (byte >= 65 and byte <= 90) or
           (byte >= 97 and byte <= 122) or
           (byte >= 48 and byte <= 57) or
           c == "-" or c == "_" or c == "." or c == "~" then
            encoded = encoded .. c
        else
            encoded = encoded .. string.format("%%%02X", byte)
        end
    end
    return encoded
end
```

**4. 错误处理**

使用防御性编程，检查所有可能失败的操作：

```lua
local resp = ghostclaw.http_get(url)
if not resp then
    return "错误: 无法连接服务"
end

if resp.status_code ~= 200 then
    return "错误: 服务返回 " .. tostring(resp.status_code)
end

local data = ghostclaw.json_decode(resp.body)
if not data then
    return "错误: 无法解析响应"
end
```

**5. 提供 help 函数**

每个插件都应提供帮助信息：

```lua
function help()
    return [[插件名称 使用说明
━━━━━━━━━━━━━━━━━━━━━━━━━━━━

📋 可用函数:

1. function_name(param1, param2)
   功能描述
   示例: function_name("参数")

━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📊 数据来源说明]]
end
```

### GhostClaw API 参考

**日志输出**
```lua
ghostclaw.log(level, message)
-- level: "info", "warn", "error", "debug"
```

**HTTP 请求**
```lua
local resp = ghostclaw.http_get(url)
-- 返回: { status_code = 200, status = "200 OK", body = "响应内容" }

local resp = ghostclaw.http_post(url, body, content_type)
```

**JSON 处理**
```lua
local json_str = ghostclaw.json_encode(table)
local data = ghostclaw.json_decode(json_string)
```

**文件操作**
```lua
local content, err = ghostclaw.read_file(path)
local ok, err = ghostclaw.write_file(path, content)
local exists = ghostclaw.exists(path)
local entries, err = ghostclaw.list_dir(path)
local info, err = ghostclaw.stat(path)  -- 获取文件详细信息
local ok, err = ghostclaw.mkdir(path)  -- 创建目录
local ok, err = ghostclaw.remove(path)  -- 删除文件或目录
local ok, err = ghostclaw.rename(old_path, new_path)  -- 重命名/移动
```

**文件上传**
```lua
-- 使用 multipart/form-data 上传
local success, response = ghostclaw.upload_multipart(path, url, method, file_field)

-- 使用原始数据上传
local success, response = ghostclaw.upload_raw(path, url, method, content_type)

-- 自动选择上传方式
local success, response = ghostclaw.upload_file(path, url, method, content_type, file_field)
```

**文件下载**
```lua
-- 下载文件，支持自定义头部
local success, message = ghostclaw.download_file(url, save_path, headers)
-- headers 可选，格式: {["User-Agent"] = "GhostClaw/1.0"}
```

**时间函数**
```lua
local ts = ghostclaw.time()  -- 当前时间戳
local str = ghostclaw.time_format(timestamp, layout)  -- 格式化时间
local ts, err = ghostclaw.time_parse(str, layout)  -- 解析时间字符串
ghostclaw.sleep(seconds)  -- 休眠
```

**字符串处理**
```lua
local parts = ghostclaw.split(str, separator)
local trimmed = ghostclaw.trim(str)
local found = ghostclaw.contains(str, substr)
local replaced = ghostclaw.replace(str, old, new)  -- 替换字符串
```

**路径操作**
```lua
local path = ghostclaw.join_path(part1, part2, part3)  -- 连接路径
local parts = ghostclaw.split_path(path)  -- 分割路径，返回 {dir, file}
local abs_path = ghostclaw.abs_path(path)  -- 获取绝对路径
```

**环境变量**
```lua
local value = ghostclaw.getenv(name)  -- 获取环境变量
local ok, err = ghostclaw.setenv(name, value)  -- 设置环境变量
```

**工作目录**
```lua
local cwd = ghostclaw.getcwd()  -- 获取当前工作目录
local ok, err = ghostclaw.chdir(path)  -- 切换工作目录
```

**加密/哈希**
```lua
local hash = ghostclaw.hash(algo, data)  -- "md5", "sha1", "sha256"
```

**随机数/UUID**
```lua
local num = ghostclaw.random(min, max)  -- 随机数
local uuid = ghostclaw.uuid()  -- 生成UUID
```

**工具调用**
```lua
local result = ghostclaw.call_tool(tool_name, args_table)
```

**TOON 格式处理**
```lua
-- 将 Lua 表编码为 TOON 格式
local toon_str = ghostclaw.toon_encode(table)

-- 将 TOON 格式解码为 Lua 表
local table = ghostclaw.toon_decode(toon_str)

-- 读取 TOON 文件并解码为 Lua 表
local data, err = ghostclaw.toon_read_file(path)

-- 将 Lua 表编码为 TOON 格式并写入文件
local success, err = ghostclaw.toon_write_file(path, table)
```

### 插件调用方式

用户通过 `plugin_call` 工具调用插件函数：

```
plugin_call(plugin="weather", function="get_weather", args=["广州"])
```

### 常见错误

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `index out of range [-1]` | 数组访问越界 | 检查数组索引，使用 safe_get |
| `attempt to call nil` | 函数未定义 | 调整函数定义顺序 |
| `attempt to index nil` | 访问 nil 值的成员 | 添加 nil 检查 |
| 中文参数无效 | URL 未编码 | 使用 url_encode |
| HTTP 请求被拒绝 | SSRF 防护 | 只访问公网 API |
