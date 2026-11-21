// Command bff est le Backend For Frontend : il sert l'application React
// embarquée, relaie les appels /api/… vers les backends selon sa table de
// routage, et agrège pour l'interface ce qui demande plusieurs appels.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/config"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/health"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/httpmw"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/logger"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/runner"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/telemetry"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "bff:", err)
		os.Exit(1)
	}
}

// run assemble et lance le binaire ; les tests l'appellent avec leurs propres
// arguments.
func run(ctx context.Context, args []string, out io.Writer) error {
	opts, printConfig, err := configOptions(args)
	if err != nil {
		return err
	}
	if printConfig {
		text, err := config.Explain(opts)
		fmt.Fprint(out, text)
		return err
	}
	cfg, err := config.Load(opts)
	if err != nil {
		return err
	}
	log := logger.New(out, cfg.Log.Format, cfg.Log.SlogLevel(), cfg.Service.Name, version.Version)
	log.Info("configuration", "http", cfg.HTTP.Address, "routes", len(cfg.Routes))

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Options{
		Enabled: cfg.Telemetry.Enabled, Endpoint: cfg.Telemetry.Endpoint, SampleRatio: cfg.Telemetry.SampleRatio,
		Service: cfg.Service.Name, Version: version.Version,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Les dernières traces partent à l'arrêt, dans un délai borné.
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(flush); err != nil {
			log.Warn("telemetry shutdown", "err", err)
		}
	}()

	h := health.New(2 * time.Second)
	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           httpmw.Chain(mux, httpmw.RequestID, httpmw.Tracing, httpmw.AccessLog(log), httpmw.Recover(log)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second, // au-delà du plus long délai d'un backend
		IdleTimeout:       2 * time.Minute,
	}
	return runner.Run(ctx, log, runner.HTTPServer(srv, cfg.Shutdown.Timeout, h.ShuttingDown))
}

// configOptions lit les options de la ligne de commande : le fichier désigné
// par -config ou CONFIG_FILE (obligatoire alors), sinon configs/bff.yaml s'il
// existe ; -print-config affiche la configuration effective, sans démarrer.
func configOptions(args []string) (config.Options, bool, error) {
	fs := flag.NewFlagSet("bff", flag.ContinueOnError)
	file := fs.String("config", os.Getenv("CONFIG_FILE"), "YAML configuration file")
	printConfig := fs.Bool("print-config", false, "print the effective configuration and the source of each value, then exit")
	if err := fs.Parse(args); err != nil {
		return config.Options{}, false, err
	}
	opts := config.Options{File: *file, FileRequired: *file != ""}
	if opts.File == "" {
		opts.File = "configs/bff.yaml"
	}
	return opts, *printConfig, nil
}
