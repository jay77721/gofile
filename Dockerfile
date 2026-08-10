# 前端构建阶段
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --registry=https://registry.npmmirror.com
COPY web/ .
RUN npm run build

# 后端构建阶段
FROM golang:1.25-alpine AS builder

ARG http_proxy
ARG https_proxy
ENV http_proxy=$http_proxy https_proxy=$https_proxy

WORKDIR /app

# 安装依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并构建
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

# 运行阶段
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# 从构建阶段复制二进制
COPY --from=builder /app/server .

# 复制前端构建产物
COPY --from=web /web/dist ./web/dist

# 创建数据目录
RUN mkdir -p uploads chunks

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["./server"]
