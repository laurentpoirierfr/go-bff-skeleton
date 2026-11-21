// Package logger crée le journal structuré (slog) des binaires.
package logger

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// New renvoie un journal JSON (production) ou texte (développement), qui
// ajoute à chaque message le nom du service et sa version.
func New(w io.Writer, format string, level slog.Level, service, version string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(w, opts)
	if format == "text" {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(traceHandler{h}).With("service", service, "version", version)
}

// traceHandler ajoute à chaque message écrit dans une trace (méthodes
// …Context de slog) ses identifiants trace_id et span_id : on passe ainsi
// d'une ligne de journal à sa trace dans Jaeger.
type traceHandler struct{ slog.Handler }

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}
