### 第一阶段：发现核心矛盾（Alpine vs Playwright）
最初在 **Alpine Linux** 上运行 Playwright，遇到了两个典型报错：
1. **`BEWARE: your OS is not officially supported`** - 提示 Alpine 不是官方支持系统
2. **`could not run driver: fork/exec ... node: no such file or directory`** - Playwright Go 内嵌的 Node.js 二进制无法在 Alpine 上运行

**根本原因**：Alpine 使用 **musl libc**，而 Playwright 官方构建的浏览器与 Node.js 驱动都依赖 **glibc**，两者不兼容。

### 第二阶段：制定策略（绕过 vs 替换）
我们讨论后确定了解决思路：**不依赖 Playwright 下载的 glibc 组件，全部替换为 Alpine 原生组件**

| 需要替换的组件 | Playwright 默认方式 | 的 Alpine 替代方案 |
|--------------|-------------------|---------------------|
| **Node.js 驱动** | 内嵌的 glibc 版 Node.js | 使用系统安装的 musl 版 Node.js |
| **Chromium 浏览器** | 下载 glibc 版 Chromium | 使用 apk 安装的 musl 版 Chromium |

### 第三阶段：关键操作步骤
执行了几个关键操作，每一步都在解决一个具体问题：

**1. 阻止自动下载 glibc 组件**
```bash
export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
```
👉 作用：告诉 Playwright 不要下载它自己的浏览器，为使用系统浏览器铺路

**2. 解决驱动问题（最关键的一步）**
```bash
export PLAYWRIGHT_NODEJS_PATH=$(which node)
```
👉 作用：令 Playwright Go 使用 Alpine 系统里安装的 musl 版 Node.js 来运行驱动脚本，而不是用内嵌的那个 glibc 版 Node.js

**3. 配置系统 Chromium 路径**
```bash
export PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium-browser
```
👉 作用：明确告诉 Playwright 的浏览器在哪里

**4. 解决路径查找问题（最后的临门一脚）**
```bash
ln -s /usr/bin/chromium-browser /opt/google/chrome/chrome
```
👉 作用：虽然环境变量经已指定了路径，但 Playwright 某些内部逻辑可能还在默认路径查找，符号链接最终使 Playwright 找到浏览器

### 第四阶段：为何现在成功了？

让我们看看的环境变量组合：
```
PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1        # 1. 不下载 glibc 浏览器
PLAYWRIGHT_NODEJS_PATH=/usr/bin/node      # 2. 使用 musl Node.js 运行驱动
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium-browser  # 3. 指定 musl Chromium
```
再加上符号链接作为**双重保险**，最终形成了完整的解决方案：
- **驱动层** ✅ 系统 Node.js（musl）运行 Playwright 驱动脚本
- **浏览器层** ✅ 系统 Chromium（musl）被 Playwright 调用
- **查找层** ✅ 无论 Playwright 用什么方式找浏览器都能找到
