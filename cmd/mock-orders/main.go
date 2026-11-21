// Command mock-orders est un service simulé, pour montrer le BFF : il répond
// avec des données fixes, et peut simuler un backend lent ou en panne.
//
//	go run ./cmd/mock-orders -address :8092 -delay 2s -fail 0.3
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/mock"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/httpmw"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/logger"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/runner"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/telemetry"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/version"
)

func main() {
	address := flag.String("address", envOr("HTTP_ADDRESS", ":8092"), "listen address")
	delay := flag.Duration("delay", 0, "delay added to every response")
	fail := flag.Float64("fail", 0, "share of requests answered with 503, from 0 to 1")
	otel := flag.String("telemetry-endpoint", os.Getenv("TELEMETRY_ENDPOINT"), "OTLP HTTP collector URL (empty: no traces)")
	flag.Parse()

	log := logger.New(os.Stderr, "text", slog.LevelInfo, "mock-orders", version.Version)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := telemetry.Setup(ctx, telemetry.Options{Enabled: *otel != "", Endpoint: *otel, SampleRatio: 1, Service: "mock-orders", Version: version.Version})
	if err != nil {
		log.Error("telemetry", "err", err)
		os.Exit(1)
	}
	defer func() { _ = shutdown(context.WithoutCancel(ctx)) }() // dernières traces, au mieux

	srv := &http.Server{
		Addr:              *address,
		Handler:           httpmw.Chain(mock.Orders(mock.Options{Delay: *delay, FailRate: *fail}), httpmw.Tracing, httpmw.AccessLog(log)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("mock started", "address", *address, "delay", *delay, "fail", *fail)
	if err := runner.Run(ctx, log, runner.HTTPServer(srv, 5*time.Second, nil)); err != nil {
		log.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
