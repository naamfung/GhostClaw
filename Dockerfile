# GhostClaw 跨平台构建镜像
# 支持目标：Linux (amd64/arm64), Alpine (amd64/arm64), Loong64, Windows (amd64), FreeBSD/GhostBSD (amd64)

# 阶段1: 前端构建
FROM node:22-alpine AS frontend-builder

# 构建参数：是否使用国内镜像
ARG USE_CN_MIRROR=false

# 国内镜像加速
RUN if [ "$USE_CN_MIRROR" = "true" ]; then \
        sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories; \
    fi

WORKDIR /app/webui

# 安装 pnpm
RUN corepack enable && corepack prepare pnpm@latest --activate

# 复制前端依赖文件
COPY webui/package.json webui/pnpm-lock.yaml* webui/.npmrc* ./

# 配置 pnpm 镜像（国内加速）
RUN if [ "$USE_CN_MIRROR" = "true" ]; then \
        pnpm config set registry https://registry.npmmirror.com; \
    fi

# 安装依赖
RUN pnpm install --frozen-lockfile || pnpm install

# 复制前端源码
COPY webui/ ./

# 构建前端
RUN pnpm run build

# 阶段2: 后端编译 (多平台)
FROM golang:alpine AS backend-builder

# 构建参数：是否使用国内镜像
ARG USE_CN_MIRROR=false

# 国内镜像加速
RUN if [ "$USE_CN_MIRROR" = "true" ]; then \
        sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories; \
    fi

# 只安装必要的依赖
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 配置 Go 代理
ARG USE_CN_MIRROR=false
ENV GOPROXY=${USE_CN_MIRROR:+https://goproxy.cn,direct}
ENV GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}

# 复制 Go 模块文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY *.go ./

# 复制前端构建产物
COPY --from=frontend-builder /app/embed ./embed

# 构建参数
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG BINARY_NAME=ghostclaw
ARG STATIC_LINK=false

# 设置交叉编译环境
ENV CGO_ENABLED=0
ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}

# 编译后端
RUN if [ "$STATIC_LINK" = "true" ]; then \
        go build -ldflags="-s -w -extldflags '-static'" -o ${BINARY_NAME} . ; \
    else \
        go build -ldflags="-s -w" -o ${BINARY_NAME} . ; \
    fi

# 阶段3: 运行时镜像
FROM alpine:3.19 AS runtime

# 构建参数：是否使用国内镜像
ARG USE_CN_MIRROR=false

# 国内镜像加速
RUN if [ "$USE_CN_MIRROR" = "true" ]; then \
        sed -i 's/dl-cdn.alpinelinux.org/mirrors.tuna.tsinghua.edu.cn/g' /etc/apk/repositories; \
    fi

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 复制编译产物
COPY --from=backend-builder /app/ghostclaw /app/ghostclaw

# 复制必要资源
COPY roles/ ./roles/
COPY skills/ ./skills/
COPY plugins/ ./plugins/
COPY public/ ./public/

# 创建必要目录
RUN mkdir -p uploads embed

# 暴露端口
EXPOSE 10086

# 启动命令
CMD ["./ghostclaw"]
