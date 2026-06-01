# 构建阶段
FROM golang:1.25-alpine AS builder

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

# 复制静态文件
COPY static/ ./static/

# 创建数据目录
RUN mkdir -p uploads chunks

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["./server"]
