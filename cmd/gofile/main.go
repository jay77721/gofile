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
// @description Lightweight cloud storage API — file upload/download, chunked resumable upload, user auth, fast dedup, AI semantic search
// @BasePath /
//
// @securityDefinitions.apikey ApiKeyAuth
// @in cookie
// @name token
// @description Cookie auth (username + token), automatically set via Set-Cookie after login
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		slog.Error("application exited with error", "error", err)
		os.Exit(1)
	}
}
