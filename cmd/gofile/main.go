package main

import (
	"context"
	"gofile/internal/app"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// @title gofile API
// @version 1.0
// @description 轻量级网盘服务 API — 文件上传/下载、分片上传断点续传、用户认证、秒传去重、AI 语义检索
// @BasePath /
//
// @securityDefinitions.apikey ApiKeyAuth
// @in cookie
// @name token
// @description Cookie 鉴权（username + token），登录后由 Set-Cookie 自动设置
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Error("application exited with error", "error", err)
		os.Exit(1)
	}
}
