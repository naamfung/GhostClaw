# 文件读写工具改进方案

## 问题分析

当前文件写入工具存在以下问题：

1. **`write_all_lines` 工具**：每次调用都会覆盖整个文件
2. **`write_file_line` 工具**：必须指定行号，不支持追加操作
3. **模型使用问题**：模型在增量式写入时，每次调用 `write_all_lines` 只写入一行，导致新内容覆盖旧内容

## 改进方案

### 方案 1：增强现有工具

#### 1.1 改进 `write_all_lines` 工具
- 添加 `append` 参数（布尔值，默认 false）
- 当 `append` 为 true 时，将内容追加到文件末尾
- 当 `append` 为 false 时（默认），覆盖整个文件

#### 1.2 改进 `write_file_line` 工具
- 当 `line_num` 为 0 或负数时，将内容追加到文件末尾
- 保持原有的行号写入功能

### 方案 2：新增专门工具

#### 2.1 新增 `append_file` 工具
- **参数**：
  - `filename`：文件路径（必需）
  - `content`：要追加的内容（必需）
  - `line_break`：是否添加换行符（可选，默认 true）
- **功能**：将内容追加到文件末尾，支持自动添加换行符

#### 2.2 新增 `write_file_range` 工具
- **参数**：
  - `filename`：文件路径（必需）
  - `start_line`：起始行号（必需）
  - `end_line`：结束行号（可选，不指定则只写入一行）
  - `content`：要写入的内容（必需）
- **功能**：写入指定行范围，支持替换多行

### 方案 3：综合方案

结合方案 1 和方案 2，提供全面的文件操作能力：

1. **增强 `write_all_lines`**：添加 `append` 参数
2. **改进 `write_file_line`**：支持行号 0 追加
3. **新增 `append_file`**：专门用于追加操作
4. **新增 `write_file_range`**：支持范围写入

## 实现建议

### 优先实现
1. **增强 `write_all_lines`**：添加 `append` 参数（最紧急，解决当前问题）
2. **新增 `append_file`**：提供直观的追加功能

### 次要实现
3. **改进 `write_file_line`**：支持行号 0 追加
4. **新增 `write_file_range`**：支持范围写入

## 工具调用示例

### 1. 增强后的 `write_all_lines`
```json
{
  "toolcall": {
    "thought": "追加一行到文件末尾",
    "name": "write_all_lines",
    "params": {
      "filename": "output.txt",
      "lines": ["新的一行内容"],
      "append": true
    }
  }
}
```

### 2. 新增的 `append_file`
```json
{
  "toolcall": {
    "thought": "追加内容到文件",
    "name": "append_file",
    "params": {
      "filename": "output.txt",
      "content": "这是要追加的内容",
      "line_break": true
    }
  }
}
```

### 3. 改进后的 `write_file_line`
```json
{
  "toolcall": {
    "thought": "追加到文件末尾",
    "name": "write_file_line",
    "params": {
      "filename": "output.txt",
      "line_num": 0,
      "content": "追加到末尾的内容"
    }
  }
}
```

## 预期效果

通过这些改进，模型可以：
- 逐行增量式写入文件，而不会覆盖之前的内容
- 灵活选择适合的写入方式（覆盖、追加、指定行）
- 更高效地处理文件操作任务
