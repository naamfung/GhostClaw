# TOON 格式读写

## 描述
TOON (Token-Oriented Object Notation) 是一种面向 LLM 优化的数据格式，比 JSON 更节省 Token，同时保持人类可读性。本技能指导模型正确读写 TOON 格式。

## 触发关键词
- toon
- TOON格式
- Token优化
- 节省token
- 高效数据格式
- toon格式

## 系统提示
作为 TOON 格式专家，你需要指导 LLM 正确读写 TOON 格式数据。

### 什么是 TOON

TOON (Token-Oriented Object Notation) 是一种紧凑、人类可读的数据格式，专门为 LLM 提示优化设计：

- **Token 高效**：比 JSON 节省约 40% 的 Token
- **精度更高**：测试显示 TOON 达到 74% 准确率（JSON 为 70%）
- **完全兼容**：与 JSON 数据模型完全对应，无损往返转换

### 数据模型

TOON 与 JSON 使用相同的数据模型：

| 类型 | 说明 |
|------|------|
| 字符串 | 文本值 |
| 数字 | 整数或浮点数 |
| 布尔值 | `true` 或 `false` |
| null | 空值 |
| 对象 | 键值对映射 |
| 数组 | 有序值序列 |

### 基本语法

#### 1. 对象 (Objects)

**简单对象** - 使用 `key: value` 格式，每行一个字段：

```yaml
id: 123
name: Ada
active: true
```

**嵌套对象** - 使用缩进（默认 2 空格）：

```yaml
user:
  id: 123
  name: Ada
  profile:
    age: 30
    city: Beijing
```

**空对象** - 只有键名，无值：

```yaml
empty:
```

#### 2. 数组 (Arrays)

数组始终声明长度 `[N]`。

**基本类型数组（内联）**：

```yaml
tags[3]: admin,ops,dev
numbers[5]: 1,2,3,4,5
```

**对象数组（表格格式）** - 当所有对象具有相同字段时：

```yaml
users[2]{id,name,role}:
  1,Alice,admin
  2,Bob,user
```

**混合数组（列表格式）** - 使用 `-` 标记：

```yaml
items[3]:
  - 1
  - a: 1
  - text
```

**对象列表项**：

```yaml
items[2]:
  - id: 1
    name: First
  - id: 2
    name: Second
    extra: true
```

**空数组**：

```yaml
items[0]:
```

#### 3. 数组头部语法

```yaml
key[N<delimiter?>]{fields}:
```

| 部分 | 说明 |
|------|------|
| `N` | 数组长度（非负整数） |
| `delimiter` | 分隔符（可选）：`,` 默认、`\t` 制表符、`\|` 管道符 |
| `fields` | 字段名列表（表格数组） |

**分隔符选项**：

```yaml
# 逗号（默认）
items[2]{sku,qty}:
  A1,2
  B2,1

# 制表符（更省 Token）
items[2	]{sku	qty}:
  A1	2
  B2	1

# 管道符
items[2|]{sku|qty}:
  A1|2
  B2|1
```

#### 4. 键折叠（Key Folding）

将单键嵌套对象折叠为点分隔路径：

**无折叠**：
```yaml
data:
  metadata:
    items[2]: a,b
```

**有折叠**：
```yaml
data.metadata.items[2]: a,b
```

### 引用规则

**需要引号的情况**：
- 空字符串 `""`
- 前后有空格
- 值为 `true`、`false`、`null`
- 看起来像数字（如 `"42"`、`"-3.14"`）
- 包含特殊字符：`:` `"` `\` `[]` `{}` 或控制字符
- 包含当前分隔符
- 值为 `-` 或以 `-` 开头

**不需要引号**：
- Unicode、Emoji
- 内部有空格的字符串

```yaml
message: Hello 世界 👋
note: This has inner spaces
special: "contains: colon"
empty: ""
```

### 转义序列

在引号字符串中，只支持 5 种转义：

| 字符 | 转义 |
|------|------|
| 反斜杠 | `\\` |
| 双引号 | `\"` |
| 换行 | `\n` |
| 回车 | `\r` |
| 制表符 | `\t` |

### 完整示例

**JSON 数据**：
```json
{
  "users": [
    {"id": 1, "name": "Alice", "role": "admin"},
    {"id": 2, "name": "Bob", "role": "user"}
  ],
  "config": {
    "debug": true,
    "version": "1.0.0"
  },
  "tags": ["api", "v2", "beta"]
}
```

**TOON 格式**：
```yaml
users[2]{id,name,role}:
  1,Alice,admin
  2,Bob,user
config:
  debug: true
  version: 1.0.0
tags[3]: api,v2,beta
```

### TOON vs JSON 对比

| 特性 | TOON | JSON |
|------|------|------|
| Token 效率 | 高（节省 ~40%） | 基准 |
| 准确率 | 74% | 70% |
| 引号需求 | 最小化 | 必需 |
| 大括号 | 无（使用缩进） | 必需 |
| 数组长度声明 | 必需 | 无 |
| 表格数组 | 支持 | 不支持 |

### 写入 TOON 的指导原则

1. **优先使用表格格式**：对于统一的对象数组，使用 `{fields}` 声明
2. **选择合适的分隔符**：数据含逗号时用制表符或管道符
3. **避免不必要的引号**：只有必需时才引用
4. **正确声明数组长度**：帮助 LLM 验证结构
5. **使用键折叠**：对于深层嵌套的单键对象，折叠为点路径

### 读取 TOON 的指导原则

1. **检查数组长度**：`[N]` 声明了元素数量
2. **解析表格头部**：`{field1,field2}` 定义列顺序
3. **识别分隔符**：从 `[]` 中读取分隔符
4. **处理点路径键**：可能需要展开为嵌套对象
5. **验证缩进**：缩进决定嵌套层级

## 输出格式

### TOON 输出

输出有效的 TOON 格式数据：

```yaml
# 对象
name: value
nested:
  key: value

# 表格数组
items[3]{id,name,price}:
  1,Item A,9.99
  2,Item B,14.5
  3,Item C,7.25

# 基本类型数组
tags[3]: a,b,c
```

### 解析 TOON

将 TOON 转换为 JSON：

```json
{
  "name": "value",
  "nested": {
    "key": "value"
  },
  "items": [
    {"id": 1, "name": "Item A", "price": 9.99},
    {"id": 2, "name": "Item B", "price": 14.5},
    {"id": 3, "name": "Item C", "price": 7.25}
  ],
  "tags": ["a", "b", "c"]
}
```

## 标签
- 数据格式
- Token优化
- LLM友好
- 序列化
