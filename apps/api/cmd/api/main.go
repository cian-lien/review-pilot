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
	"reviewpilot.local/api/internal/auth"
	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/httpapi"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/repositories"
	"reviewpilot.local/api/internal/webhooks"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("CONFIG_INVALID", "code", err.Error())
		os.Exit(1)
	}
	public := httpapi.NewRouter()
	public.GET("/api/bootstrap", func(c *gin.Context) {
		missing := cfg.Missing
		if missing == nil {
			missing = []string{}
		}
		response := contracts.BootstrapResponse{Phase: contracts.BootstrapResponsePhaseFoundation, AiReviewEnabled: false, MissingConfiguration: missing}
		if cfg.GitHubAppSlug != "" {
			address := "https://github.com/apps/" + cfg.GitHubAppSlug + "/installations/new"
			response.InstallUrl = &address
		}
		c.JSON(200, response)
	})
	var store *persistence.Store
	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		store, err = persistence.Open(ctx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			slog.Error("DATABASE_UNAVAILABLE")
		} else {
			defer store.Close()
		}
	}
	service := auth.New(store, cfg, github.New(cfg))
	auth.Register(public, service)
	repositories.Register(public, repositories.New(store, service))
	var webhookService *webhooks.Service
	if store != nil {
		webhookService = webhooks.New(store.DB(), cfg.PipelineVersion)
	} else {
		webhookService = webhooks.New(nil, cfg.PipelineVersion)
	}
	webhooks.Register(public, webhookService, cfg)
	internal := httpapi.NewRouter()
	var taskService *jobs.Service
	if store != nil {
		taskService = jobs.New(store.DB())
	} else {
		taskService = jobs.New(nil)
	}
	jobs.Register(internal, taskService, cfg)
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
