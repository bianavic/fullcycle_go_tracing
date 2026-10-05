package main

import (
	"bianavic/fullcycle_go_tracing/internal/api"
	"bianavic/fullcycle_go_tracing/internal/config"
	"bianavic/fullcycle_go_tracing/internal/infra/viacep"
	"bianavic/fullcycle_go_tracing/internal/infra/weatherapi"
	"bianavic/fullcycle_go_tracing/internal/usecase"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("service-b failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	defer func() {
		_, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
	}()

	// Composition root: the only place that knows the concrete adapters.
	httpClient := &http.Client{Timeout: cfg.HTTPClientTimeout}
	location := viacep.NewClient(cfg.ViaCEPBaseURL, httpClient)
	weather := weatherapi.NewClient(cfg.WeatherAPIBaseURL, cfg.WeatherAPIKey, httpClient)
	handler := api.NewHandler(usecase.NewGetWeatherByCEP(location, weather), logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(handler, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Must outlast two sequential upstream calls plus processing.
		WriteTimeout: 2*cfg.HTTPClientTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("service-b listening", "addr", srv.Addr, "config", cfg.String())
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
