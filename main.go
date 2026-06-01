package main

import (
	"context"
	"filestore-server/config"
	"filestore-server/db/mysql"
	"filestore-server/handler"
	"filestore-server/rd"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 初始化结构化日志
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 加载配置
	cfg := config.Load()

	// 初始化 Redis
	if err := rd.InitRedis(cfg.RedisAddr, cfg.RedisPass, cfg.RedisDB); err != nil {
		slog.Error("Redis init failed", "error", err)
		os.Exit(1)
	}

	// 初始化 MySQL
	if err := mysql.Init(cfg.MySQLDSN); err != nil {
		slog.Error("MySQL init failed", "error", err)
		os.Exit(1)
	}

	// 确保上传目录存在
	os.MkdirAll(cfg.UploadDir, 0755)
	os.MkdirAll(cfg.ChunkDir, 0755)

	slog.Info("server starting", "addr", cfg.ServerAddr)

	// 静态文件（不需要鉴权）
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// 健康检查（不需要鉴权）
	http.HandleFunc("/healthz", handler.HealthCheckHandler)

	// 用户接口（注册登录不需要鉴权，但有速率限制）
	rateLimit := handler.RateLimitMiddleware(5, 10) // 5 req/s, burst 10
	http.HandleFunc("/user/signup", rateLimit(handler.SignupHandler))
	http.HandleFunc("/user/signin", rateLimit(handler.SignInHandler))
	http.HandleFunc("/user/info", handler.HTTPInterceptor(handler.UserInfoHandler))

	// 文件接口（全部需要鉴权）
	http.HandleFunc("/file/upload", handler.HTTPInterceptor(handler.UploadHandler))
	http.HandleFunc("/file/upload/suc", handler.HTTPInterceptor(handler.UploadSucHandler))
	http.HandleFunc("/file/meta", handler.HTTPInterceptor(handler.GetFileHandler))
	http.HandleFunc("/file/query", handler.HTTPInterceptor(handler.FileQueryHandler))
	http.HandleFunc("/file/download", handler.HTTPInterceptor(handler.DownloadHandler))
	http.HandleFunc("/file/update", handler.HTTPInterceptor(handler.FileMetaUpdateHandler))
	http.HandleFunc("/file/delete", handler.HTTPInterceptor(handler.FileDeleteHandler))
	http.HandleFunc("/file/upload/chunk", handler.HTTPInterceptor(handler.UploadChunkHandler))
	http.HandleFunc("/file/upload/status", handler.HTTPInterceptor(handler.UploadStatusHandler))
	http.HandleFunc("/file/upload/merge", handler.HTTPInterceptor(handler.MergeChunkHandler))

	// 创建 HTTP Server（支持优雅关闭）
	srv := &http.Server{
		Addr:         cfg.ServerAddr,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 在 goroutine 中启动服务
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server start failed", "error", err)
			os.Exit(1)
		}
	}()

	// 监听系统信号，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server exited")
}
