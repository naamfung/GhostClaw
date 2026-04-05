---
name: file_upload
description: 将文件上传到临时文件分享服务，获取可分享的下载链接。支持多个上传服务，自动尝试可用服务。
tags:
  - 上传
  - 分享
  - 文件传输
  - 临时存储
platforms:
  - windows
  - linux
  - macos
---

# 文件上传服务

## 描述
将文件上传到临时文件分享服务，获取可分享的下载链接。支持多个上传服务，自动尝试可用服务。

## 触发关键词
- 上传
- upload
- 分享文件
- 临时链接
- 文件分享
- 上传到临时

## 系统提示
作为文件上传专家，你可以帮助用户将文件上传到临时文件分享服务。

### 支持的上传服务

按推荐顺序尝试以下服务：

#### 1. Temp.sh（推荐）
- 网址：https://temp.sh
- 特点：简单快速，文件保留 3 天
- 最大文件：约 500MB
- 用法：
```bash
curl -F "file=@文件路径" https://temp.sh/upload
```
- 返回：直接返回下载链接
- 示例：
```bash
curl -F "file=@/path/to/example.tar.gz" https://temp.sh/upload
## 返回: https://temp.sh/XXXXX/example.tar.gz
```

#### 2. Filebin
- 网址：https://filebin.net
- 特点：稳定可靠，文件保留 7 天
- 最大文件：无明确限制
- 用法：
```bash
curl -X POST -H "filename: 文件名.ext" --data-binary @文件路径 https://filebin.net/
```
- 返回：JSON，包含 bin.id 与 file.filename
- 下载链接格式：`https://filebin.net/{bin_id}/{filename}`
- 示例：
```bash
curl -X POST -H "filename: example.tar.gz" --data-binary @/path/to/example.tar.gz https://filebin.net/
## 返回 JSON，下载链接: https://filebin.net/{bin_id}/example.tar.gz
```

#### 3. Catbox.moe
- 网址：https://catbox.moe
- 特点：稳定，支持多种文件类型
- 最大文件：约 200MB（匿名）
- 用法：
```bash
curl -X POST -F "reqtype=fileupload" -F "fileToUpload=@文件路径" https://catbox.moe/user/api.php
```
- 返回：直接返回下载链接
- 示例：
```bash
curl -X POST -F "reqtype=fileupload" -F "fileToUpload=@/path/to/example.tar.gz" https://catbox.moe/user/api.php
## 返回: https://files.catbox.moe/XXXXX.tar.gz
```

#### 4. Uguu.se
- 网址：https://uguu.se
- 特点：简单快速，文件保留 24 小时
- 最大文件：约 128MB
- 用法：
```bash
curl -X POST -F "files[]=@文件路径" https://uguu.se/upload
```
- 返回：JSON，包含 files[].url 字段
- 示例：
```bash
curl -X POST -F "files[]=@/path/to/example.tar.gz" https://uguu.se/upload
## 返回 JSON，下载链接: https://d.uguu.se/XXXXX.tar.gz
```

### 上传流程

1. **确认文件**：确认用户要上传的文件路径
2. **选择服务**：按推荐顺序尝试服务
3. **执行上传**：使用 curl 命令上传
4. **解析结果**：从响应中提取下载链接
5. **返回链接**：向用户提供下载链接

### 错误处理

- 如果某个服务不可用，自动尝试下一个
- 如果所有服务都失败，告知用户当前网络环境可能受限
- 记录失败的服务，避免短时间内重复尝试

### 注意事项

1. 上传的文件通常是公开可访问的，不要上传敏感信息
2. 文件有有效期，过期后自动删除
3. 大文件上传可能需要较长时间
4. 某些服务可能在特定地区不可用

## 输出格式

### 上传成功

✅ **文件上传成功**

- **文件名**：[文件名]
- **文件大小**：[大小]
- **下载链接**：[URL]
- **有效期**：[保留时间]

### 上传失败

❌ **上传失败**

- **错误原因**：[原因]
- **建议**：[替代方案]
