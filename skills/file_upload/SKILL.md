# 文件临时上传服务

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

### 执行步骤

1. **检查文件是否存在**：使用`smart_shell`工具执行`ls -la hello_test.txt`命令检查文件是否存在
2. **执行上传命令**：使用`smart_shell`工具执行curl命令上传文件到临时文件分享服务
3. **解析上传结果**：从命令输出中提取下载链接
4. **返回结果**：按照输出格式向用户提供下载链接或错误信息

### 工具调用示例

**检查文件是否存在：**
```json
{
  "toolcall": {
    "thought": "检查hello_test.txt文件是否存在",
    "name": "smart_shell",
    "params": {
      "command": "ls -la hello_test.txt"
    }
  }
}
```

**上传文件到temp.sh：**
```json
{
  "toolcall": {
    "thought": "使用curl命令上传hello_test.txt文件到临时文件分享服务",
    "name": "smart_shell",
    "params": {
      "command": "curl -F \"file=@hello_test.txt\" https://temp.sh/upload"
    }
  }
}
```

**上传文件到filebin.net：**
```json
{
  "toolcall": {
    "thought": "使用curl命令上传hello_test.txt文件到filebin.net",
    "name": "smart_shell",
    "params": {
      "command": "curl -F \"file=@hello_test.txt\" https://filebin.net"
    }
  }
}
```

### 重要提示

- **不需要激活技能**：直接使用`smart_shell`工具执行命令即可
- **不需要创建技能**：file_upload技能已经存在
- **直接执行命令**：按照上述步骤直接执行curl命令
- **错误处理**：如果上传失败，尝试其他上传服务

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

## 标签

- 上传
- 分享
- 文件传输
- 临时存储

