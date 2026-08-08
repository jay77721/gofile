package main

import (
	"context"
	"gofile/cache"
	"gofile/config"
	"gofile/db/mysql"
	"gofile/handler"
	"gofile/repository"
	"gofile/service"
	"gofile/storage"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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

	// 初始化 Redis（可选，失败不影响启动）
	var cacheClient *cache.Client
	if cfg.RedisAddr != "" {
		cc, err := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			slog.Warn("Redis unavailable, running without cache", "error", err)
		} else {
			cacheClient = cc
			slog.Info("Redis connected", "addr", cfg.RedisAddr)
		}
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

	// 依赖注入：Repository → Service → Handler
	db := mysql.DBConn()

	fileRepo := repository.NewFileRepository(db)
	userRepo := repository.NewUserRepository(db)
	tokenRepo := repository.NewTokenRepository(db)

	// 启动软删除文件垃圾回收（需要 fileRepo + store）
	handler.StartSoftDeleteGC(fileRepo, store, 0)

	fileSvc := service.NewFileService(fileRepo, store, cfg, cacheClient)
	userSvc := service.NewUserService(userRepo, tokenRepo)
	authSvc := service.NewAuthService(tokenRepo)

	fileHandler := handler.NewFileHandler(fileSvc, cfg)
	userHandler := handler.NewUserHandler(userSvc, cfg)
	authMiddleware := handler.NewAuthMiddleware(authSvc)

	// 初始化 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 配置可信代理
	_ = r.SetTrustedProxies(nil)

	// 禁用静态文件缓存，确保前端更新即时生效
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	})
	// 静态文件
	r.Static("/static", "./static")

	// 根路径重定向到首页
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/static/index.html")
	})

	// 健康检查（不需要鉴权）
	r.GET("/healthz", handler.HealthCheckHandler)

	// 用户接口
	rateLimit := handler.RateLimitMiddleware(5, 10, cacheClient)
	r.POST("/user/signup", rateLimit, userHandler.SignupHandler)
	r.POST("/user/signin", rateLimit, userHandler.SignInHandler)
	r.GET("/user/info", authMiddleware.Middleware(), userHandler.UserInfoHandler)

	// 文件接口（全部需要鉴权）
	file := r.Group("/file", authMiddleware.Middleware())
	{
		file.POST("/upload", fileHandler.UploadHandler)
		file.GET("/meta", fileHandler.GetFileHandler)
		file.GET("/query", fileHandler.FileQueryHandler)
		file.GET("/download", fileHandler.DownloadHandler)
		file.GET("/preview", fileHandler.PreviewHandler)
		file.POST("/update", fileHandler.FileMetaUpdateHandler)
		file.POST("/delete", fileHandler.FileDeleteHandler)
		file.POST("/upload/chunk", fileHandler.UploadChunkHandler)
		file.GET("/upload/status", fileHandler.UploadStatusHandler)
		file.POST("/upload/merge", fileHandler.MergeChunkHandler)
		// 预签名 URL 直传直下
		file.POST("/presigned/upload", fileHandler.PresignUploadHandler)
		file.POST("/presigned/upload/confirm", fileHandler.ConfirmUploadHandler)
		file.GET("/presigned/download", fileHandler.PresignDownloadHandler)
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