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
	"go.opentelemetry.io/otel"

	"service-b/internal/api"
	"service-b/internal/config"
	"service-b/internal/infra/viacep"
	"service-b/internal/infra/weatherapi"
	"service-b/internal/observability/telemetry"
	"service-b/internal/usecase"
)

const shutdownTimeout = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, os.Getenv); err != nil {
		logger.Error("service-b failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	shutdownTracing, err := telemetry.Init(ctx, "service-b")
	if err != nil {
		return err
	}
	// Flush pending spans on the way out, including when startup fails below.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			logger.Warn("flush traces", "error", err)
		}
	}()

	// Composition root: the only place that knows the concrete adapters.
	tracer := otel.Tracer("service-b")
	// ViaCEP calls get an automatic client span under lookup-cep. WeatherAPI
	// calls deliberately do not: its key travels in the query string and the
	// otelhttp client span would record the full URL (url.full) with it.
	viaCEPHTTP := &http.Client{
		Timeout:   cfg.HTTPClientTimeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	weatherHTTP := &http.Client{Timeout: cfg.HTTPClientTimeout}
	location := viacep.NewClient(cfg.ViaCEPBaseURL, viaCEPHTTP, tracer)
	weather := weatherapi.NewClient(cfg.WeatherAPIBaseURL, cfg.WeatherAPIKey, weatherHTTP, tracer)
	handler := api.NewHandler(usecase.NewGetWeatherByCEP(location, weather), logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(handler, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      2*cfg.HTTPClientTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("service-b listening", "addr", srv.Addr, "config", cfg.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(drainCtx)
}
