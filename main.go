package main

import (
	"context"
	"filestore-server/config"
	"filestore-server/db/mysql"
	"filestore-server/handler"
	"filestore-server/rd"
	"filestore-server/storage"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
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

	// 确保临时目录存在
	os.MkdirAll(cfg.ChunkDir, 0755)

	// 启动定时清理过期 chunk 目录
	handler.StartChunkCleanup(cfg.ChunkDir)

	// 初始化存储层（优先 MinIO，fallback 本地）
	var store storage.Storage
	if cfg.MinioEndpoint != "" {
		minioStore, err := storage.NewMinIO(
			cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey,
			cfg.MinioBucket, cfg.MinioUseSSL,
		)
		if err != nil {
			slog.Error("MinIO init failed, falling back to local", "error", err)
			store = storage.NewLocal(cfg.UploadDir)
		} else {
			store = minioStore
			slog.Info("using MinIO storage", "endpoint", cfg.MinioEndpoint, "bucket", cfg.MinioBucket)
		}
	} else {
		store = storage.NewLocal(cfg.UploadDir)
		slog.Info("using local storage", "dir", cfg.UploadDir)
	}

	// 注入存储层和配置到 handler
	handler.InitStore(store, cfg)

	// 初始化 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 配置可信代理：默认只信任回环地址，防止 X-Forwarded-For 伪造
	// 生产环境需根据实际反向代理 IP 配置
	_ = r.SetTrustedProxies(nil)

	// 静态文件
	r.Static("/static", "./static")

	// 健康检查（不需要鉴权）
	r.GET("/healthz", handler.HealthCheckHandler)

	// 用户接口
	rateLimit := handler.RateLimitMiddleware(5, 10) // 5 req/s, burst 10
	r.POST("/user/signup", rateLimit, handler.SignupHandler)
	r.POST("/user/signin", rateLimit, handler.SignInHandler)
	r.GET("/user/info", handler.AuthMiddleware(), handler.UserInfoHandler)

	// 文件接口（全部需要鉴权）
	file := r.Group("/file", handler.AuthMiddleware())
	{
		file.POST("/upload", handler.UploadHandler)
		file.GET("/meta", handler.GetFileHandler)
		file.GET("/query", handler.FileQueryHandler)
		file.GET("/download", handler.DownloadHandler)
		file.POST("/update", handler.FileMetaUpdateHandler)
		file.POST("/delete", handler.FileDeleteHandler)
		file.POST("/upload/chunk", handler.UploadChunkHandler)
		file.GET("/upload/status", handler.UploadStatusHandler)
		file.POST("/upload/merge", handler.MergeChunkHandler)
	}

	// 创建 HTTP Server（支持优雅关闭）
	srv := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      r,
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

	slog.Info("server started", "addr", cfg.ServerAddr)

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
