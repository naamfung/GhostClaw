#!/bin/sh
# GhostClaw 构建脚本
# 用法: ./build.sh [--check-deps]
#
# 兼容性：POSIX shell，适用于 Linux、FreeBSD、OpenBSD、NetBSD、macOS、GhostBSD 等
# 包管理器优先级：pnpm > npm > bun

set -e

# 颜色定义（兼容所有终端）
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

printf "=== GhostClaw 构建脚本 ===\n\n"

# 切换到脚本所在目录
cd "$(dirname "$0")"
rm -rf ghostclaw ghostclaw.exe ghostclaw_*
rm -rf webui/node_modules webui/.svelte-kit

cp webui/tsconfig.json webui/tsconfig.env.json
rm -rf webui/tsconfig.json

# 检测操作系统
# 返回格式: 系统名称:是否为Windows(0/1)
detect_os() {
    # 首先检测 Windows 环境（包括 MSYS2、MinGW、Cygwin）
    if [ "$OS" = "Windows_NT" ] || [ "$(uname -s)" = "MINGW64_NT" ] || [ "$(uname -s)" = "MSYS_NT" ] || [ "$(uname -s)" = "CYGWIN_NT" ]; then
        echo "windows:1"
        return
    fi
    
    if [ -f "/etc/os-release" ]; then
        . /etc/os-release
        case "$ID" in
            ghostbsd) echo "ghostbsd:0" ;;
            freebsd)  echo "freebsd:0" ;;
            *)        echo "$ID:0" ;;
        esac
    elif [ "$(uname)" = "Darwin" ]; then
        echo "macos:0"
    elif [ "$(uname)" = "Linux" ]; then
        echo "linux:0"
    else
        echo "$(uname)" | tr '[:upper:]' '[:lower:]':0
    fi
}

# 解析 detect_os 的返回值
OS_INFO=$(detect_os)
OS=$(echo "$OS_INFO" | cut -d: -f1)
IS_WINDOWS=$(echo "$OS_INFO" | cut -d: -f2)
printf "检测到系统: %s\n" "$OS"

# 检测可用的包管理器（按优先级：pnpm > npm > bun）
detect_package_manager() {
    if command -v pnpm > /dev/null 2>&1; then
        echo "pnpm"
    elif command -v npm > /dev/null 2>&1; then
        echo "npm"
    elif command -v bun > /dev/null 2>&1; then
        echo "bun"
    else
        echo ""
    fi
}

# 检查并提示安装依赖
check_dependencies() {
    missing=""
    has_node=0
    has_npm=0
    has_pnpm=0
    
    printf "检查依赖...\n\n"
    
    # 检查 Go
    if command -v go > /dev/null 2>&1; then
        go_version=$(go version 2>/dev/null | head -1 || echo 'installed')
        printf "  \033[0;32m✓\033[0m Go: %s\n" "$go_version"
    else
        printf "  \033[0;31m✗\033[0m Go: 未安装\n"
        missing="$missing go"
    fi
    
    # 检查 Node.js
    if command -v node > /dev/null 2>&1; then
        has_node=1
        node_version=$(node --version 2>/dev/null || echo 'installed')
        printf "  \033[0;32m✓\033[0m Node.js: %s\n" "$node_version"
    else
        printf "  \033[0;31m✗\033[0m Node.js: 未安装\n"
        missing="$missing node"
    fi
    
    # 检查 npm
    if command -v npm > /dev/null 2>&1; then
        has_npm=1
        npm_version=$(npm --version 2>/dev/null || echo 'installed')
        printf "  \033[0;32m✓\033[0m npm: %s\n" "$npm_version"
    else
        printf "  \033[0;31m✗\033[0m npm: 未安装\n"
        if [ "$OS" = "ghostbsd" ] || [ "$OS" = "freebsd" ]; then
            printf "    提示: 在 %s 上，需要单独安装 npm: pkg install npm-node24\n" "$OS"
        fi
        missing="$missing npm"
    fi
    
    # 检查 pnpm
    if command -v pnpm > /dev/null 2>&1; then
        has_pnpm=1
        pnpm_version=$(pnpm --version 2>/dev/null || echo 'installed')
        printf "  \033[0;32m✓\033[0m pnpm: %s\n" "$pnpm_version"
    else
        printf "  - pnpm: 未安装（可选，推荐）\n"
    fi
    
    # 检查 bun（仅显示状态，不作为必需）
    if command -v bun > /dev/null 2>&1; then
        bun_version=$(bun --version 2>/dev/null | head -1 || echo 'installed')
        printf "  - bun: %s（可能存在兼容性问题）\n" "$bun_version"
    fi
    
    printf "\n"
    
    # 如果缺少必需依赖，显示安装提示
    if [ -n "$missing" ]; then
        printf "\033[0;31m错误: 缺少必需依赖\033[0m\n\n"
        printf "请安装缺少的依赖：\n\n"
        
        case "$OS" in
            ghostbsd|freebsd)
                printf "  # 安装 Go\n"
                printf "  sudo pkg install go\n\n"
                printf "  # 安装 Node.js 和 npm（注意：node 包不含 npm，需单独安装）\n"
                printf "  sudo pkg install node npm-node24\n\n"
                printf "  # 可选：安装 pnpm（推荐）\n"
                printf "  sudo npm install -g pnpm\n"
                ;;
            macos)
                printf "  # 使用 Homebrew\n"
                printf "  brew install go node\n"
                printf "  npm install -g pnpm  # 可选，推荐\n"
                ;;
            *)
                printf "  # Debian/Ubuntu\n"
                printf "  sudo apt install golang-go nodejs npm\n\n"
                printf "  # Fedora/RHEL\n"
                printf "  sudo dnf install golang nodejs npm\n\n"
                printf "  # Arch Linux\n"
                printf "  sudo pacman -S go nodejs npm\n\n"
                printf "  # 安装 pnpm（推荐）\n"
                printf "  npm install -g pnpm\n"
                ;;
        esac
        
        printf "\n"
        exit 1
    fi
    
    # 如果有 npm 但没有 pnpm，提示安装
    if [ "$has_npm" = "1" ] && [ "$has_pnpm" = "0" ]; then
        printf "\033[0;33m提示: 建议安装 pnpm 以获得更快的构建速度\033[0m\n"
        printf "  npm install -g pnpm\n\n"
    fi
}

# 如果传入 --check-deps 参数，只检查依赖
if [ "$1" = "--check-deps" ]; then
    check_dependencies
    exit 0
fi

# 检查依赖
check_dependencies

# 检测包管理器
PKG_MANAGER=$(detect_package_manager)
if [ -z "$PKG_MANAGER" ]; then
    printf "\033[0;31m错误: 未找到 pnpm、npm 或 bun\033[0m\n"
    printf "请安装 Node.js: https://nodejs.org/\n"
    printf "或运行: ./build.sh --check-deps 查看详细安装指南\n"
    exit 1
fi

printf "使用包管理器: \033[0;32m%s\033[0m\n\n" "$PKG_MANAGER"

# 检查 package.json 中的依赖是否正确
check_package_json() {
    pkg_file="webui/package.json"
    
    # 检查是否使用 sass-embedded
    if grep -q "sass-embedded" "$pkg_file" 2>/dev/null; then
        printf "\033[0;33m警告: package.json 中仍使用 sass-embedded\033[0m\n"
        printf "建议将 sass-embedded 替换为 sass 以提高兼容性\n\n"
    fi
}

check_package_json

# 1. 构建前端
printf "\033[0;34m[1/2] 构建前端...\033[0m\n"
cd webui

# 检查 node_modules 是否存在，不存在则安装依赖
if [ ! -d "node_modules" ]; then
    printf "安装依赖...\n"
    case "$PKG_MANAGER" in
        pnpm)
            pnpm install || {
                printf "\033[0;31mpnpm 安装失败，尝试使用 npm...\033[0m\n"
                if command -v npm > /dev/null 2>&1; then
                    npm install
                else
                    printf "\033[0;31mnpm 不可用，请检查安装\033[0m\n"
                    exit 1
                fi
            }
            ;;
        npm)
            npm install || {
                printf "\033[0;31mnpm 安装失败\033[0m\n"
                exit 1
            }
            ;;
        bun)
            printf "\033[0;33m警告: bun 在某些系统上可能存在兼容性问题\033[0m\n"
            bun install || {
                printf "\033[0;31mbun 安装失败，尝试使用 npm...\033[0m\n"
                if command -v npm > /dev/null 2>&1; then
                    npm install
                else
                    printf "\033[0;31mnpm 不可用，请检查安装\033[0m\n"
                    exit 1
                fi
            }
            ;;
    esac
fi

# 构建
printf "构建中...\n"
case "$PKG_MANAGER" in
    pnpm)
        pnpm run build || {
            printf "\033[0;31mpnpm 构建失败，尝试使用 npm...\033[0m\n"
            if command -v npm > /dev/null 2>&1; then
                npm run build
            else
                exit 1
            fi
        }
        ;;
    npm)
        npm run build || exit 1
        ;;
    bun)
        bun run build || {
            printf "\033[0;31mbun 构建失败，尝试使用 npm...\033[0m\n"
            if command -v npm > /dev/null 2>&1; then
                npm run build
            else
                exit 1
            fi
        }
        ;;
esac

cd ..
cp webui/tsconfig.env.json webui/tsconfig.json
rm -rf webui/tsconfig.env.json

# 2. 编译后端
printf "\n\033[0;34m[2/2] 编译后端...\033[0m\n"

# 获取版本号（从 version.go 提取，兼容 BSD/Linux sed）
VERSION=$(sed -n 's/.*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' version.go 2>/dev/null)
if [ -z "$VERSION" ]; then
    VERSION="dev"
fi
printf "版本号: %s\n" "$VERSION"

# 获取 git commit
GIT_COMMIT=""
if [ -d ".git" ]; then
    GIT_COMMIT=$(git rev-parse --short=7 HEAD 2>/dev/null || echo "")
fi
if [ -z "$GIT_COMMIT" ]; then
    GIT_COMMIT="unknown"
fi
printf "Git Commit: %s\n" "$GIT_COMMIT"

# 获取构建时间
BUILD_TIME=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
printf "构建时间: %s\n" "$BUILD_TIME"

# 构建标签列表
BUILD_TAGS=""
TAG_DESCRIPTIONS=""

# 检查是否启用扩展渠道
if [ "${ENABLE_TELEGRAM:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}telegram"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS telegram"
fi

if [ "${ENABLE_DISCORD:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}discord"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS discord"
fi

if [ "${ENABLE_SLACK:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}slack"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS slack"
fi

if [ "${ENABLE_FEISHU:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}feishu"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS feishu"
fi

if [ "${ENABLE_IRC:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}irc"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS irc"
fi

if [ "${ENABLE_WEBHOOK:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}webhook"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS webhook"
fi

if [ "${ENABLE_XMPP:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}xmpp"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS xmpp"
fi

if [ "${ENABLE_MATRIX:-}" = "1" ] || [ "${ENABLE_ALL_CHANNELS:-}" = "1" ]; then
    if [ -n "$BUILD_TAGS" ]; then BUILD_TAGS="$BUILD_TAGS,"; fi
    BUILD_TAGS="${BUILD_TAGS}matrix"
    TAG_DESCRIPTIONS="$TAG_DESCRIPTIONS matrix"
fi

# 显示启用的渠道
if [ -n "$TAG_DESCRIPTIONS" ]; then
    printf "启用扩展渠道:%s\n" "$TAG_DESCRIPTIONS"
fi
# 判断系统是否为 Windows，如果是则为程序名加 .exe 后缀
OUTPUT_NAME="ghostclaw"
if [ "$IS_WINDOWS" = "1" ]; then
    OUTPUT_NAME="ghostclaw.exe"
fi

# 构建
LDFLAGS="-X 'main.Version=$VERSION' -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'"
if [ -n "$BUILD_TAGS" ]; then
    go build -tags "$BUILD_TAGS" -ldflags "$LDFLAGS" -o "$OUTPUT_NAME" . || {
        printf "\033[0;31mGo 编译失败\033[0m\n"
        printf "请确保已安装 Go 编译器\n"
        exit 1
    }
else
    go build -ldflags "$LDFLAGS" -o "$OUTPUT_NAME" . || {
        printf "\033[0;31mGo 编译失败\033[0m\n"
        printf "请确保已安装 Go 编译器\n"
        exit 1
    }
fi

printf "\n\033[0;32m=== 构建完成 ===\033[0m\n"
printf "可执行文件: ./%s\n\n" "$OUTPUT_NAME"
printf "运行程序: ./%s\n" "$OUTPUT_NAME"
printf "访问地址: http://localhost:10086\n"
if [ -n "$TAG_DESCRIPTIONS" ]; then
    printf "\n已启用扩展渠道:%s\n" "$TAG_DESCRIPTIONS"
fi
printf "\n扩展渠道构建选项:\n"
printf "  ENABLE_TELEGRAM=1 ./build.sh  - 启用 Telegram 渠道\n"
printf "  ENABLE_DISCORD=1 ./build.sh   - 启用 Discord 渠道\n"
printf "  ENABLE_SLACK=1 ./build.sh     - 启用 Slack 渠道\n"
printf "  ENABLE_FEISHU=1 ./build.sh    - 启用飞书渠道\n"
printf "  ENABLE_IRC=1 ./build.sh       - 启用 IRC 渠道\n"
printf "  ENABLE_WEBHOOK=1 ./build.sh   - 启用 Webhook 渠道\n"
printf "  ENABLE_XMPP=1 ./build.sh      - 启用 XMPP 渠道\n"
printf "  ENABLE_MATRIX=1 ./build.sh    - 启用 Matrix 渠道\n"
printf "  ENABLE_ALL_CHANNELS=1 ./build.sh - 启用所有扩展渠道\n\n"
