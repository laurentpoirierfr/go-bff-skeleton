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
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laurentpoirierfr/go-bff-skeleton/internal/compose"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/config"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/health"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/httpmw"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/logger"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/runner"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/telemetry"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/version"
	"github.com/laurentpoirierfr/go-bff-skeleton/internal/proxy"
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

	routes, err := proxyRoutes(cfg)
	if err != nil {
		return err
	}
	transport := proxy.Transport()
	proxy.Register(mux, routes, proxy.Options{MaxBodyBytes: cfg.Proxy.MaxBodyBytes, Transport: transport, Log: log})
	for _, r := range routes {
		log.Info("route", "name", r.Name, "prefix", r.Prefix, "target", r.Target.Redacted(), "timeout", r.Timeout)
	}
	// Les API de composition appellent les backends des routes users et
	// orders ; sans elles, il n'y a rien à composer.
	if users, orders, ok := composeBackends(routes); ok {
		compose.New(users, orders, transport, log).Register(mux)
		log.Info("composition", "endpoints", "/api/dashboard, /api/profiles/{id}")
	} else {
		log.Warn("composition disabled: routes users and orders are required")
	}

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

// proxyRoutes traduit la table de routage de la configuration.
func proxyRoutes(cfg config.Config) ([]proxy.Route, error) {
	var routes []proxy.Route
	for _, name := range config.RouteNames(cfg.Routes) {
		r := cfg.Routes[name]
		target, err := url.Parse(r.Target)
		if err != nil {
			return nil, fmt.Errorf("routes.%s.target: %w", name, err)
		}
		routes = append(routes, proxy.Route{Name: name, Prefix: r.Prefix, Target: target, Timeout: r.Timeout})
	}
	return routes, nil
}

// composeBackends trouve les backends des routes users et orders.
func composeBackends(routes []proxy.Route) (users, orders compose.Backend, ok bool) {
	found := map[string]compose.Backend{}
	for _, r := range routes {
		found[r.Name] = compose.Backend{Name: r.Name, URL: r.Target, Timeout: r.Timeout}
	}
	users, okU := found["users"]
	orders, okO := found["orders"]
	return users, orders, okU && okO
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
