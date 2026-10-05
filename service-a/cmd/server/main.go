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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"service-a/internal/api"
	"service-a/internal/config"
	"service-a/internal/infra/serviceb"
	"service-a/internal/observability/telemetry"
	"service-a/internal/usecase"
)

const shutdownTimeout = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("service-a failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	shutdownTracing, err := telemetry.Init(context.Background(), "service-a")
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			logger.Warn("flush traces", "error", err)
		}
	}()

	// The otelhttp transport injects the W3C traceparent into calls to service-b.
	httpClient := &http.Client{
		Timeout:   cfg.HTTPClientTimeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	gateway := serviceb.NewClient(cfg.ServiceBURL, httpClient)
	handler := api.NewHandler(usecase.NewRequestWeather(gateway), logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(handler, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.HTTPClientTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("service-a listening", "addr", srv.Addr, "config", cfg.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-serverErr:
		return err
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}
