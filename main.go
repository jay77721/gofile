package main

import (
	"context"
	"gofile/ai"
	"gofile/cache"
	"gofile/config"
	"gofile/db/mysql"
	"gofile/handler"
	"gofile/metrics"
	"gofile/repository"
	"gofile/service"
	"gofile/storage"
	"gofile/task"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	_ "github.com/swaggo/swag"
)

// @title gofile API
// @version 1.0
// @description 轻量级网盘服务 API — 文件上传/下载、分片上传断点续传、用户认证、秒传去重、AI 语义检索
// @BasePath /

// @securityDefinitions.apikey ApiKeyAuth
// @in cookie
// @name token
// @description Cookie 鉴权（username + token），登录后由 Set-Cookie 自动设置

func main() {
	// 初始化结构化日志：ContextHandler 从 context 提取 request_id 自动附加到每条日志
	slog.SetDefault(slog.New(metrics.NewContextHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))))
	// 注册 Prometheus 指标（幂等，可重复调用）
	metrics.Register()

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
	multipartRepo := repository.NewMultipartRepository(db)
	userRepo := repository.NewUserRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	shareRepo := repository.NewShareRepository(db)

	var indexer ai.Indexer

	// 初始化 AI 功能（可选，AI_ENABLED=false 时跳过，不影响主链路）
	var aiProcessor *ai.Processor
	var aiHandler *handler.AIHandler
	var aiCfgHandler *handler.AIConfigHandler
	if cfg.AIEnabled {
		aiRepo := repository.NewAITaskRepository(db)
		provider := ai.NewProvider(cfg)
		indexer = ai.NewTypesenseIndexer(cfg.TypesenseURL, cfg.TypesenseAPIKey, cfg.AIEmbedDim)

		// 幂等创建 Typesense collection（失败则降级为 MySQL LIKE 搜索，不阻断启动）
		initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := indexer.EnsureCollection(initCtx); err != nil {
			slog.Warn("Typesense unavailable, AI search will fallback to LIKE", "error", err)
		}
		cancel()

		aiProcessor = ai.NewProcessor(provider, indexer, fileRepo, aiRepo, store, cfg)
		aiProcessor.Start()
		handler.StartAICompensation(aiProcessor)
		handler.StartAITaskCleanup(aiRepo, 0)

		aiSvc := service.NewAIService(indexer, provider, fileRepo)
		aiHandler = handler.NewAIHandler(aiSvc)

		// 用户级 AI Provider 配置:自定义 OpenAI 协议 baseURL/API key
		// 解析器注入 Processor(异步分析)与 AIService(语义搜索),按任务用户名生效
		aiCfgSvc := service.NewAIConfigService(repository.NewAIConfigRepository(db), cfg, provider)
		aiProcessor.WithResolver(aiCfgSvc.ResolveProvider)
		aiSvc.WithResolver(aiCfgSvc.ResolveProvider)
		aiCfgHandler = handler.NewAIConfigHandler(aiCfgSvc)

		// M3: Asynq 分布式任务队列（ASYNQ_ENABLED=true 且 Redis 可用时启动）
		// 替代进程内 chan，实现任务持久化 + 跨实例调度 + 内置重试 + 死信队列
		if cfg.AsynqEnabled && cfg.RedisAddr != "" {
			taskClient := task.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
			// 注入 taskEnqueuer，Enqueue 优先走 Asynq，Redis 故障自动回退 chan
			aiProcessor.WithTaskEnqueuer(taskClient)

			asynqSrv := task.NewServer(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.AIWorkers)
			mux := asynq.NewServeMux()
			mux.HandleFunc(task.TypeFileAIAnalyze, task.NewAITaskProcessor(aiProcessor).ProcessTask)

			go func() {
				slog.Info("asynq task server started", "queue", "ai", "workers", cfg.AIWorkers)
				if err := asynqSrv.Run(mux); err != nil {
					slog.Error("asynq server stopped", "error", err)
				}
			}()
		} else if cfg.AIEnabled {
			slog.Info("asynq disabled, using in-process chan queue (set ASYNQ_ENABLED=true to upgrade)")
		}

		slog.Info("AI features enabled", "provider", cfg.AIProvider, "embedDim", cfg.AIEmbedDim, "workers", cfg.AIWorkers, "asynq", cfg.AsynqEnabled)
	}

	handler.StartSoftDeleteGC(fileRepo, store, 0, indexer)

	fileSvc := service.NewFileService(fileRepo, store, cfg, cacheClient).
		WithMultipart(multipartRepo).
		WithAI(aiProcessor).
		WithIndexer(indexer)
	userSvc := service.NewUserService(userRepo, tokenRepo)
	authSvc := service.NewAuthService(tokenRepo)
	shareSvc := service.NewShareService(shareRepo, fileRepo)

	fileHandler := handler.NewFileHandler(fileSvc, cfg)
	userHandler := handler.NewUserHandler(userSvc, cfg)
	shareHandler := handler.NewShareHandler(shareSvc, fileSvc)
	handler.StartShareCleanup(shareRepo)
	authMiddleware := handler.NewAuthMiddleware(authSvc)

	// 初始化 Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// 中间件顺序是关键不变量：
	//   1. RequestIDMiddleware 先生成 request_id 注入 context，供后续日志/指标使用
	//   2. MetricsMiddleware 计时 + 写访问日志 + 记录指标（依赖 request_id 已注入）
	//   3. gin.Recovery 最内层，捕获 handler 与 MetricsMiddleware 的 panic
	r.Use(metrics.RequestIDMiddleware())
	r.Use(metrics.MetricsMiddleware())
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
	// 前端静态资源(Vite 构建产物,见 web/ 目录)
	r.Static("/static", "./web/dist")

	// 根路径重定向到首页
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/static/index.html")
	})

	// 健康检查（不需要鉴权）
	r.GET("/healthz", handler.HealthCheckHandler)

	// Prometheus 指标端点（不需要鉴权，供监控系统抓取）
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	// 免登录分享下载（公开路由,分享令牌即访问凭证;限流防提取码爆破）
	r.GET("/share/:token", handler.RateLimitMiddleware(10, 20, cacheClient), shareHandler.ShareDownloadHandler)

	// 用户接口
	rateLimit := handler.RateLimitMiddleware(5, 10, cacheClient)
	r.POST("/user/signup", rateLimit, userHandler.SignupHandler)
	r.POST("/user/signin", rateLimit, userHandler.SignInHandler)
	r.POST("/user/logout", userHandler.LogoutHandler)
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
		// 回收站
		file.GET("/trash", fileHandler.TrashHandler)
		file.POST("/restore", fileHandler.RestoreHandler)
		file.POST("/purge", fileHandler.PurgeHandler)
		// 文件夹树形管理 (VFS)
		file.POST("/folder/create", fileHandler.CreateFolderHandler)
		file.POST("/folder/rename", fileHandler.RenameFolderHandler)
		file.POST("/folder/move", fileHandler.MoveFolderHandler)
		// 文件分享
		file.POST("/share", shareHandler.CreateShareHandler)
		file.GET("/share/list", shareHandler.ShareListHandler)
		file.POST("/share/revoke", shareHandler.RevokeShareHandler)
		file.POST("/upload/chunk", fileHandler.UploadChunkHandler)
		file.GET("/upload/status", fileHandler.UploadStatusHandler)
		file.POST("/upload/merge", fileHandler.MergeChunkHandler)
		// S3 预签名直传直下与 S3 Multipart 分片直传
		file.POST("/presigned/upload", fileHandler.PresignUploadHandler)
		file.POST("/presigned/upload/confirm", fileHandler.ConfirmUploadHandler)
		file.GET("/presigned/download", fileHandler.PresignDownloadHandler)
		file.POST("/upload/multipart/init", fileHandler.InitMultipartHandler)
		file.POST("/upload/multipart/complete", fileHandler.CompleteMultipartHandler)
		file.POST("/upload/multipart/abort", fileHandler.AbortMultipartHandler)
		// AI 语义检索（全部需要鉴权）
		if aiHandler != nil {
			file.GET("/ai/search", aiHandler.SearchHandler)
			file.GET("/ai/similar", aiHandler.SimilarHandler)
			file.GET("/ai/duplicates", aiHandler.DuplicatesHandler)
		}
	}

	// AI Provider 配置（自定义 OpenAI 协议 baseURL/API key,需鉴权）
	if aiCfgHandler != nil {
		aiCfg := r.Group("/ai/config", authMiddleware.Middleware())
		{
			aiCfg.GET("", aiCfgHandler.GetConfigHandler)
			aiCfg.POST("", aiCfgHandler.SaveConfigHandler)
			aiCfg.DELETE("", aiCfgHandler.DeleteConfigHandler)
			aiCfg.POST("/test", aiCfgHandler.TestConfigHandler)
		}
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
