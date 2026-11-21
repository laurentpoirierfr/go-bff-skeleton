// Package runner fait tourner les composants d'un binaire (serveur HTTP,
// relais de l'outbox, consommateur…) et les arrête ensemble : à l'annulation
// du contexte (signal), ou dès que l'un d'eux échoue.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
)

// Component tourne jusqu'à l'annulation de ctx, puis s'arrête proprement et
// renvoie nil ; il renvoie une erreur s'il ne peut plus fonctionner.
type Component struct {
	Name string
	Run  func(ctx context.Context) error
}

// Run démarre les composants et attend qu'ils soient tous arrêtés. La première
// erreur annule les autres, et c'est elle qui est renvoyée.
func Run(ctx context.Context, log *slog.Logger, components ...Component) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, c := range components {
		g.Go(func() error {
			log.Info("starting", "component", c.Name)
			if err := c.Run(ctx); err != nil {
				log.Error("stopped on error", "component", c.Name, "err", err)
				return fmt.Errorf("%s: %w", c.Name, err)
			}
			log.Info("stopped", "component", c.Name)
			return nil
		})
	}
	return g.Wait()
}

// HTTPServer fait d'un serveur HTTP un composant. À l'arrêt, beforeShutdown
// est appelée d'abord (pour que /health/ready réponde 503), puis le serveur
// laisse timeout aux requêtes en cours pour se terminer.
func HTTPServer(srv *http.Server, timeout time.Duration, beforeShutdown func()) Component {
	return Component{Name: "http " + srv.Addr, Run: func(ctx context.Context) error {
		ln, err := net.Listen("tcp", srv.Addr) // une adresse prise échoue tout de suite
		if err != nil {
			return err
		}
		errs := make(chan error, 1)
		go func() { errs <- srv.Serve(ln) }()
		select {
		case err := <-errs:
			return err
		case <-ctx.Done():
		}
		if beforeShutdown != nil {
			beforeShutdown()
		}
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-errs; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}}
}
