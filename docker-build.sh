#!/bin/sh
# GarClaw Docker 跨平台构建脚本
# 用法: ./docker-build.sh [目标] [选项]
#
# 目标:
#   linux-amd64    - Linux x86_64 (glibc)
#   linux-arm64    - Linux ARM64 (glibc)
#   alpine-amd64   - Alpine Linux x86_64 (musl, 静态链接)
#   alpine-arm64   - Alpine Linux ARM64 (musl, 静态链接)
#   loong64        - LoongArch 64位 (龙芯)
#   darwin-amd64   - macOS Intel (x86_64)
#   darwin-arm64   - macOS Apple Silicon (M1/M2/M3)
#   windows-amd64  - Windows x86_64
#   freebsd-amd64  - FreeBSD x86_64
#   ghostbsd-amd64 - GhostBSD x86_64
#   all            - 构建所有平台
#   runtime        - 构建 Docker 运行时镜像
#   clean          - 清理构建产物
#
# 选项:
#   --cn           - 使用国内镜像加速 (Alpine/Go)
#   --no-cache     - 不使用 Docker 缓存

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# 版本号
VERSION="v2.5.17"
DIST_DIR="./dist"

# 默认参数
USE_CN_MIRROR="false"
NO_CACHE="false"
TARGET=""

printf "${CYAN}=== GarClaw Docker 跨平台构建 ===${NC}\n\n"

# 解析参数
parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --cn)
                USE_CN_MIRROR="true"
                shift
                ;;
            --no-cache)
                NO_CACHE="true"
                shift
                ;;
            help|--help|-h)
                show_help
                exit 0
                ;;
            *)
                if [ -z "$TARGET" ]; then
                    TARGET="$1"
                fi
                shift
                ;;
        esac
    done
}

# 显示帮助
show_help() {
    printf "用法: ./docker-build.sh [目标] [选项]\n\n"
    printf "目标:\n"
    printf "  ${GREEN}linux-amd64${NC}    - Linux x86_64 (glibc)\n"
    printf "  ${GREEN}linux-arm64${NC}    - Linux ARM64 (glibc, 树莓派等)\n"
    printf "  ${GREEN}alpine-amd64${NC}   - Alpine Linux x86_64 (musl, 静态链接)\n"
    printf "  ${GREEN}alpine-arm64${NC}   - Alpine Linux ARM64 (musl, 静态链接)\n"
    printf "  ${GREEN}loong64${NC}        - LoongArch 64位 (龙芯处理器)\n"
    printf "  ${GREEN}darwin-amd64${NC}   - macOS Intel (x86_64)\n"
    printf "  ${GREEN}darwin-arm64${NC}   - macOS Apple Silicon (M1/M2/M3)\n"
    printf "  ${GREEN}windows-amd64${NC}  - Windows x86_64\n"
    printf "  ${GREEN}freebsd-amd64${NC}  - FreeBSD x86_64\n"
    printf "  ${GREEN}ghostbsd-amd64${NC} - GhostBSD x86_64\n"
    printf "  ${GREEN}all${NC}            - 构建所有平台\n"
    printf "  ${GREEN}runtime${NC}        - 构建 Docker 运行时镜像\n"
    printf "  ${GREEN}clean${NC}          - 清理构建产物\n"
    printf "\n选项:\n"
    printf "  ${YELLOW}--cn${NC}           - 使用国内镜像加速 (Alpine/Go)\n"
    printf "  ${YELLOW}--no-cache${NC}     - 不使用 Docker 缓存\n"
    printf "\n示例:\n"
    printf "  ./docker-build.sh linux-amd64\n"
    printf "  ./docker-build.sh linux-amd64 --cn\n"
    printf "  ./docker-build.sh all --cn --no-cache\n"
    printf "  docker compose build linux-amd64\n"
    printf "\n"
}

# 检查 Docker
check_docker() {
    if ! command -v docker > /dev/null 2>&1; then
        printf "${RED}错误: 未安装 Docker${NC}\n"
        printf "请先安装 Docker: https://docs.docker.com/get-docker/\n"
        exit 1
    fi

    if ! docker info > /dev/null 2>&1; then
        printf "${RED}错误: Docker 未运行或权限不足${NC}\n"
        printf "请启动 Docker 服务或使用 sudo 运行\n"
        exit 1
    fi

    printf "${GREEN}✓${NC} Docker 已就绪\n"
    if [ "$USE_CN_MIRROR" = "true" ]; then
        printf "${GREEN}✓${NC} 已启用国内镜像加速\n"
    fi
    printf "\n"
}

# 创建输出目录
create_dist_dir() {
    mkdir -p "$DIST_DIR"
}

# 构建指定平台
build_platform() {
    TARGET="$1"
    printf "${BLUE}[构建] ${TARGET}${NC}\n"

    STATIC="false"
    case "$TARGET" in
        linux-amd64)
            BINARY="garclaw-linux-amd64"
            GOOS="linux"
            GOARCH="amd64"
            ;;
        linux-arm64)
            BINARY="garclaw-linux-arm64"
            GOOS="linux"
            GOARCH="arm64"
            ;;
        windows-amd64)
            BINARY="garclaw-windows-amd64.exe"
            GOOS="windows"
            GOARCH="amd64"
            ;;
        freebsd-amd64)
            BINARY="garclaw-freebsd-amd64"
            GOOS="freebsd"
            GOARCH="amd64"
            ;;
        ghostbsd-amd64)
            BINARY="garclaw-ghostbsd-amd64"
            GOOS="freebsd"
            GOARCH="amd64"
            ;;
        alpine-amd64)
            BINARY="garclaw-alpine-amd64"
            GOOS="linux"
            GOARCH="amd64"
            STATIC="true"
            ;;
        alpine-arm64)
            BINARY="garclaw-alpine-arm64"
            GOOS="linux"
            GOARCH="arm64"
            STATIC="true"
            ;;
        loong64)
            BINARY="garclaw-linux-loong64"
            GOOS="linux"
            GOARCH="loong64"
            ;;
        darwin-amd64)
            # macOS Intel (x86_64)
            BINARY="garclaw-darwin-amd64"
            GOOS="darwin"
            GOARCH="amd64"
            ;;
        darwin-arm64)
            # macOS Apple Silicon (M1/M2/M3)
            BINARY="garclaw-darwin-arm64"
            GOOS="darwin"
            GOARCH="arm64"
            ;;
        *)
            printf "${RED}错误: 未知目标 '%s'${NC}\n" "$TARGET"
            show_help
            exit 1
            ;;
    esac

    printf "  目标系统: %s\n" "$GOOS"
    printf "  目标架构: %s\n" "$GOARCH"
    if [ "${STATIC}" = "true" ]; then
        printf "  链接方式: 静态链接 (musl)\n"
    fi
    printf "  输出文件: %s\n\n" "$BINARY"

    # 构建 docker build 参数
    BUILD_ARGS="--build-arg TARGETOS=$GOOS"
    BUILD_ARGS="$BUILD_ARGS --build-arg TARGETARCH=$GOARCH"
    BUILD_ARGS="$BUILD_ARGS --build-arg BINARY_NAME=$BINARY"
    BUILD_ARGS="$BUILD_ARGS --build-arg STATIC_LINK=${STATIC:-false}"
    BUILD_ARGS="$BUILD_ARGS --build-arg USE_CN_MIRROR=$USE_CN_MIRROR"

    if [ "$NO_CACHE" = "true" ]; then
        BUILD_ARGS="$BUILD_ARGS --no-cache"
    fi

    # 使用 docker build 构建
    docker build \
        $BUILD_ARGS \
        --target backend-builder \
        -t "garclaw-build:${TARGET}" \
        .

    # 提取构建产物
    CONTAINER_ID=$(docker create "garclaw-build:${TARGET}")
    docker cp "${CONTAINER_ID}:/app/${BINARY}" "${DIST_DIR}/${BINARY}"
    docker rm "$CONTAINER_ID" > /dev/null

    printf "${GREEN}✓ 完成: ${DIST_DIR}/${BINARY}${NC}\n\n"
}

# 构建所有平台
build_all() {
    printf "${BLUE}构建所有平台...${NC}\n\n"

    for TARGET in linux-amd64 linux-arm64 alpine-amd64 alpine-arm64 loong64 darwin-amd64 darwin-arm64 windows-amd64 freebsd-amd64 ghostbsd-amd64; do
        build_platform "$TARGET"
    done

    printf "${GREEN}=== 全部构建完成 ===${NC}\n"
    ls -lh "$DIST_DIR"
}

# 构建运行时镜像
build_runtime() {
    printf "${BLUE}构建 Docker 运行时镜像...${NC}\n\n"

    BUILD_ARGS="--build-arg USE_CN_MIRROR=$USE_CN_MIRROR"
    if [ "$NO_CACHE" = "true" ]; then
        BUILD_ARGS="$BUILD_ARGS --no-cache"
    fi

    docker build \
        $BUILD_ARGS \
        --target runtime \
        -t "garclaw:${VERSION}" \
        -t "garclaw:latest" \
        .

    printf "${GREEN}✓ 运行时镜像构建完成${NC}\n"
    printf "  镜像标签: garclaw:${VERSION}, garclaw:latest\n\n"
    printf "运行容器:\n"
    printf "  docker run -d -p 10086:10086 --name garclaw garclaw:latest\n\n"
}

# 清理构建产物
clean() {
    printf "${YELLOW}清理构建产物...${NC}\n"

    rm -rf "$DIST_DIR"
    rm -rf webui/node_modules webui/.svelte-kit embed

    # 清理 Docker 镜像
    docker images --filter=reference='garclaw-build:*' -q | xargs -r docker rmi -f 2>/dev/null || true
    docker images --filter=reference='garclaw:*' -q | xargs -r docker rmi -f 2>/dev/null || true

    printf "${GREEN}✓ 清理完成${NC}\n"
}

# 主入口
parse_args "$@"

case "$TARGET" in
    linux-amd64|linux-arm64|alpine-amd64|alpine-arm64|loong64|darwin-amd64|darwin-arm64|windows-amd64|freebsd-amd64|ghostbsd-amd64)
        check_docker
        create_dist_dir
        build_platform "$TARGET"
        ;;
    all)
        check_docker
        create_dist_dir
        build_all
        ;;
    runtime)
        check_docker
        build_runtime
        ;;
    clean)
        clean
        ;;
    "")
        show_help
        ;;
    *)
        printf "${RED}错误: 未知命令 '%s'${NC}\n\n" "$TARGET"
        show_help
        exit 1
        ;;
esac
