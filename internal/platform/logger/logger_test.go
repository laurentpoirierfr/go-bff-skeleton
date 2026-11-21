package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	log := New(&b, "json", slog.LevelInfo, "api", "1.0.0")
	log.Debug("ignored")
	log.Info("started", "port", 8080)
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("%v: %s", err, b.String())
	}
	if m["service"] != "api" || m["version"] != "1.0.0" || m["msg"] != "started" {
		t.Errorf("%v", m)
	}
}

func TestText(t *testing.T) {
	var b bytes.Buffer
	New(&b, "text", slog.LevelDebug, "worker", "dev").Debug("visible")
	if !strings.Contains(b.String(), "msg=visible service=worker version=dev") {
		t.Errorf("%s", b.String())
	}
}

// Un message écrit dans une trace porte ses identifiants ; hors trace, non.
func TestTraceIDs(t *testing.T) {
	var b bytes.Buffer
	log := New(&b, "text", slog.LevelInfo, "api", "dev").With("component", "test")
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	defer span.End()
	log.InfoContext(ctx, "in a trace")
	log.Info("outside")
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if !strings.Contains(lines[0], "trace_id="+span.SpanContext().TraceID().String()) || !strings.Contains(lines[0], "span_id=") {
		t.Errorf("%s", lines[0])
	}
	if strings.Contains(lines[1], "trace_id") {
		t.Errorf("%s", lines[1])
	}
}
