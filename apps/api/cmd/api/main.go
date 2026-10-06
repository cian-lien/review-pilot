package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("CONFIG_INVALID", "code", err.Error())
		os.Exit(1)
	}
	public := httpapi.NewRouter()
	public.GET("/api/bootstrap", func(c *gin.Context) {
		c.JSON(200, gin.H{"phase": "foundation", "aiReviewEnabled": false, "missingConfiguration": cfg.Missing})
	})
	// Replaced by authenticated business route registration in migration task 3.
	for _, path := range []string{"/auth/github", "/auth/github/callback", "/installations/callback"} {
		public.GET(path, func(c *gin.Context) { httpapi.Error(c, 503, "NOT_CONFIGURED", "业务认证迁移尚未启用") })
	}
	internal := gin.New()
	internal.NoRoute(func(c *gin.Context) { c.Status(404) })
	servers := []*http.Server{
		{Addr: cfg.APIBindAddr, Handler: public, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10},
		{Addr: cfg.InternalBindAddr, Handler: internal, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10},
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	failure := make(chan error, 2)
	for _, server := range servers {
		go func(s *http.Server) { failure <- s.ListenAndServe() }(server)
	}
	select {
	case <-ctx.Done():
	case err := <-failure:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP_LISTENER_FAILED", "code", "LISTEN_FAILED")
			cancel()
		}
	}
	shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	for _, server := range servers {
		_ = server.Shutdown(shutdown)
	}
}
