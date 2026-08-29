package app

import (
	"context"
	"errors"
	"fmt"
	"gofile/internal/application/service"
	"gofile/internal/config"
	"gofile/internal/infrastructure/ai"
	"gofile/internal/infrastructure/cache/redis"
	"gofile/internal/infrastructure/persistence/mysql"
	"gofile/internal/infrastructure/persistence/repository"
	"gofile/internal/infrastructure/queue/asynq"
	"gofile/internal/infrastructure/storage"
	"gofile/internal/job"
	"gofile/internal/observability/metrics"
	"gofile/internal/port"
	httptransport "gofile/internal/transport/http"
	"gofile/internal/transport/http/handler"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

const shutdownTimeout = 10 * time.Second

// Application owns the process-level dependencies and their lifecycle.
//
// Start starts all long-lived components. Run waits for either the supplied
// context or a component failure, then performs a graceful shutdown. Shutdown
// is safe to call more than once.
type Application struct {
	cfg   *config.Config
	cache *cache.Client
	db    *mysql.Connection

	aiProcessor *ai.Processor
	asynqServer *asynq.Server
	taskClient  *task.Client

	server *http.Server
	jobs   *job.Manager
	errCh  chan error

	mu           sync.Mutex
	started      bool
	shutdown     bool
	aiStarted    bool
	asynqStarted bool
	shutdownOnce sync.Once
	shutdownErr  error
}

// New assembles the application without starting long-lived workers.
func New() (*Application, error) {
	configureLogging()
	metrics.Register()

	cfg := config.Load()
	cacheClient := connectCache(cfg)

	dbConnection, err := mysql.Open(cfg.MySQLDSN)
	if err != nil {
		if cacheClient != nil {
			_ = cacheClient.Close()
		}
		return nil, fmt.Errorf("initialize mysql: %w", err)
	}
	db := dbConnection.DB()

	if err := os.MkdirAll(cfg.ChunkDir, 0755); err != nil {
		slog.Warn("create chunk directory failed", "dir", cfg.ChunkDir, "error", err)
	}

	store := connectStorage(cfg)
	fileRepo := repository.NewFileRepository(db)
	multipartRepo := repository.NewMultipartRepository(db)
	userRepo := repository.NewUserRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	shareRepo := repository.NewShareRepository(db)

	var indexer port.Indexer
	var aiProcessor *ai.Processor
	var aiRepo port.AITaskRepository
	var aiHandler *handler.AIHandler
	var aiCfgHandler *handler.AIConfigHandler
	var asynqServer *asynq.Server
	var taskClient *task.Client

	if cfg.AIEnabled {
		aiRepo = repository.NewAITaskRepository(db)
		provider := ai.NewProvider(cfg)
		indexer = ai.NewTypesenseIndexer(cfg.TypesenseURL, cfg.TypesenseAPIKey, cfg.AIEmbedDim)

		initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := indexer.EnsureCollection(initCtx); err != nil {
			slog.Warn("Typesense unavailable, AI search will fallback to LIKE", "error", err)
		}
		cancel()

		aiProcessor = ai.NewProcessor(provider, indexer, fileRepo, aiRepo, store, cfg)
		aiSvc := service.NewAIService(indexer, provider, fileRepo)

		aiCfgSvc := service.NewAIConfigService(repository.NewAIConfigRepository(db), cfg, provider)
		aiProcessor.WithResolver(aiCfgSvc.ResolveProvider)
		aiSvc.WithResolver(aiCfgSvc.ResolveProvider)
		aiHandler = handler.NewAIHandler(aiSvc)
		aiCfgHandler = handler.NewAIConfigHandler(aiCfgSvc)

		if cfg.AsynqEnabled && cfg.RedisAddr != "" {
			taskClient = task.NewClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
			aiProcessor.WithTaskEnqueuer(taskClient)

			asynqServer = task.NewServer(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.AIWorkers)
		}
	}

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
	authMiddleware := handler.NewAuthMiddleware(authSvc)

	r := httptransport.NewRouter(cacheClient, fileHandler, userHandler, shareHandler, authMiddleware, aiHandler, aiCfgHandler)

	application := &Application{
		cfg:         cfg,
		cache:       cacheClient,
		db:          dbConnection,
		aiProcessor: aiProcessor,
		asynqServer: asynqServer,
		taskClient:  taskClient,
		server: &http.Server{
			Addr:         cfg.ServerAddr,
			Handler:      r,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		errCh: make(chan error, 1),
	}
	application.jobs = job.NewManager(
		cfg.ChunkDir,
		fileRepo,
		multipartRepo,
		shareRepo,
		aiRepo,
		aiProcessor,
		store,
		indexer,
	)
	return application, nil
}

// Run creates, starts, and owns an Application until ctx is canceled or a
// long-lived component fails.
func Run(ctx context.Context) error {
	a, err := New()
	if err != nil {
		return err
	}
	return a.Run(ctx)
}

// Start starts the AI processor, Asynq consumer, cleanup loops, and HTTP
// server. It does not block.
func (a *Application) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}
	if a.shutdown {
		a.mu.Unlock()
		return errors.New("application is already shut down")
	}
	a.started = true
	a.aiStarted = a.aiProcessor != nil
	a.asynqStarted = a.asynqServer != nil
	a.mu.Unlock()

	if a.aiProcessor != nil {
		a.aiProcessor.Start()
	}

	if a.asynqServer != nil {
		mux := asynq.NewServeMux()
		mux.HandleFunc(task.TypeFileAIAnalyze, task.NewAITaskProcessor(a.aiProcessor).ProcessTask)
		go func() {
			slog.Info("asynq task server started", "queue", "ai", "workers", a.cfg.AIWorkers)
			if err := a.asynqServer.Run(mux); err != nil {
				a.reportError(fmt.Errorf("asynq server stopped: %w", err))
			}
		}()
	} else if a.aiProcessor != nil {
		slog.Info("asynq disabled, using in-process chan queue (set ASYNQ_ENABLED=true to upgrade)")
	}

	if a.aiProcessor != nil {
		slog.Info("AI features enabled", "provider", a.cfg.AIProvider, "embedDim", a.cfg.AIEmbedDim, "workers", a.cfg.AIWorkers, "asynq", a.cfg.AsynqEnabled)
	}

	if a.jobs != nil {
		a.jobs.Start(parent)
	}

	go func() {
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.reportError(fmt.Errorf("http server stopped: %w", err))
		}
	}()
	slog.Info("server started", "addr", a.cfg.ServerAddr)
	return nil
}

// Run waits for shutdown or a component error. If Start has not been called,
// Run starts the application first.
func (a *Application) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.Start(ctx); err != nil {
		return err
	}

	var runErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown requested", "reason", ctx.Err())
	case runErr = <-a.errCh:
		slog.Error("application component failed", "error", runErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return errors.Join(runErr, a.Shutdown(shutdownCtx))
}

// Shutdown gracefully stops components in dependency order: HTTP ingress,
// Asynq consumers, AI workers, producers/caches, and finally the database.
func (a *Application) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.shutdownOnce.Do(func() {
		a.mu.Lock()
		a.shutdown = true
		aiStarted := a.aiStarted
		asynqStarted := a.asynqStarted
		a.mu.Unlock()

		var shutdownErr error
		if a.server != nil {
			if err := a.server.Shutdown(ctx); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown http server: %w", err))
			}
		}
		if a.jobs != nil {
			if err := a.jobs.Stop(ctx); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("shutdown background jobs: %w", err))
			}
		}

		if asynqStarted && a.asynqServer != nil {
			slog.Info("shutting down asynq server")
			a.asynqServer.Shutdown()
		}
		if aiStarted && a.aiProcessor != nil {
			slog.Info("stopping ai processor")
			a.aiProcessor.Stop()
		}
		if a.taskClient != nil {
			if err := a.taskClient.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close task client: %w", err))
			}
		}
		if a.cache != nil {
			if err := a.cache.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close cache: %w", err))
			}
		}
		if a.db != nil {
			if err := a.db.Close(); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close mysql: %w", err))
			}
		}
		a.shutdownErr = shutdownErr
	})
	return a.shutdownErr
}

func (a *Application) reportError(err error) {
	select {
	case a.errCh <- err:
	default:
		slog.Error("application error channel full", "error", err)
	}
}

func configureLogging() {
	slog.SetDefault(slog.New(metrics.NewContextHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))))
}

func connectCache(cfg *config.Config) *cache.Client {
	if cfg.RedisAddr == "" {
		return nil
	}
	cacheClient, err := cache.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		slog.Warn("Redis unavailable, running without cache", "error", err)
		return nil
	}
	slog.Info("Redis connected", "addr", cfg.RedisAddr)
	return cacheClient
}

func connectStorage(cfg *config.Config) port.Storage {
	if cfg.MinioEndpoint != "" {
		minioStore, err := storage.NewMinIO(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioBucket, cfg.MinioUseSSL)
		if err == nil {
			slog.Info("using MinIO storage", "endpoint", cfg.MinioEndpoint, "bucket", cfg.MinioBucket)
			return minioStore
		}
		slog.Error("MinIO init failed, falling back to local", "error", err)
	}
	store := storage.NewLocal(cfg.UploadDir)
	slog.Info("using local storage", "dir", cfg.UploadDir)
	return store
}
