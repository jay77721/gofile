package httptransport

import (
	"net/http"
	"strings"

	"gofile/internal/infrastructure/cache/redis"
	"gofile/internal/observability/metrics"
	"gofile/internal/transport/http/handler"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the HTTP transport and wires handlers to the public API.
// Dependency construction remains in internal/app; this package owns the
// protocol routes, middleware, and HTTP-specific behavior.
func NewRouter(
	cacheClient *cache.Client,
	fileHandler *handler.FileHandler,
	userHandler *handler.UserHandler,
	shareHandler *handler.ShareHandler,
	authMiddleware *handler.AuthMiddleware,
	aiHandler *handler.AIHandler,
	aiCfgHandler *handler.AIConfigHandler,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(metrics.RequestIDMiddleware())
	r.Use(metrics.MetricsMiddleware())
	r.Use(gin.Recovery())
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		}
		c.Next()
	})
	r.Static("/static", "./web/dist")
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/static/index.html")
	})
	r.GET("/healthz", handler.HealthCheckHandler)
	r.GET("/metrics", gin.WrapH(metrics.Handler()))
	r.GET("/share/:token", handler.RateLimitMiddleware(10, 20, cacheClient), shareHandler.ShareDownloadHandler)

	rateLimit := handler.RateLimitMiddleware(5, 10, cacheClient)
	r.POST("/user/signup", rateLimit, userHandler.SignupHandler)
	r.POST("/user/signin", rateLimit, userHandler.SignInHandler)
	r.POST("/user/logout", userHandler.LogoutHandler)
	r.GET("/user/info", authMiddleware.Middleware(), userHandler.UserInfoHandler)

	file := r.Group("/file", authMiddleware.Middleware())
	{
		file.POST("/upload", fileHandler.UploadHandler)
		file.GET("/meta", fileHandler.GetFileHandler)
		file.GET("/query", fileHandler.FileQueryHandler)
		file.GET("/download", fileHandler.DownloadHandler)
		file.GET("/preview", fileHandler.PreviewHandler)
		file.POST("/update", fileHandler.FileMetaUpdateHandler)
		file.POST("/delete", fileHandler.FileDeleteHandler)
		file.GET("/trash", fileHandler.TrashHandler)
		file.POST("/restore", fileHandler.RestoreHandler)
		file.POST("/purge", fileHandler.PurgeHandler)
		file.POST("/folder/create", fileHandler.CreateFolderHandler)
		file.POST("/folder/rename", fileHandler.RenameFolderHandler)
		file.POST("/folder/move", fileHandler.MoveFolderHandler)
		file.POST("/share", shareHandler.CreateShareHandler)
		file.GET("/share/list", shareHandler.ShareListHandler)
		file.POST("/share/revoke", shareHandler.RevokeShareHandler)
		file.POST("/upload/chunk", fileHandler.UploadChunkHandler)
		file.GET("/upload/status", fileHandler.UploadStatusHandler)
		file.POST("/upload/merge", fileHandler.MergeChunkHandler)
		file.POST("/presigned/upload", fileHandler.PresignUploadHandler)
		file.POST("/presigned/upload/confirm", fileHandler.ConfirmUploadHandler)
		file.GET("/presigned/download", fileHandler.PresignDownloadHandler)
		file.POST("/upload/multipart/init", fileHandler.InitMultipartHandler)
		file.POST("/upload/multipart/complete", fileHandler.CompleteMultipartHandler)
		file.POST("/upload/multipart/abort", fileHandler.AbortMultipartHandler)
		if aiHandler != nil {
			file.GET("/ai/search", aiHandler.SearchHandler)
			file.GET("/ai/similar", aiHandler.SimilarHandler)
			file.GET("/ai/duplicates", aiHandler.DuplicatesHandler)
		}
	}

	if aiCfgHandler != nil {
		aiCfg := r.Group("/ai/config", authMiddleware.Middleware())
		{
			aiCfg.GET("", aiCfgHandler.GetConfigHandler)
			aiCfg.POST("", aiCfgHandler.SaveConfigHandler)
			aiCfg.DELETE("", aiCfgHandler.DeleteConfigHandler)
			aiCfg.POST("/test", aiCfgHandler.TestConfigHandler)
		}
	}
	return r
}
