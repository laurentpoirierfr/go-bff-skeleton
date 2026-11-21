package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Le contexte de trace traverse des métadonnées : c'est ce qui relie le
// worker à la requête HTTP d'origine.
func TestInjectExtract(t *testing.T) {
	shutdown, err := Setup(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	md := map[string]string{}
	Inject(ctx, md)
	if md["traceparent"] == "" {
		t.Fatalf("no traceparent: %v", md)
	}
	got := trace.SpanContextFromContext(Extract(context.Background(), md))
	if got.TraceID() != span.SpanContext().TraceID() || !got.IsRemote() {
		t.Errorf("extracted %v, want trace %v", got, span.SpanContext().TraceID())
	}
}

// Activée, la télémétrie installe un fournisseur de traces ; l'exportateur ne
// se connecte qu'à l'envoi, le collecteur peut donc être absent au démarrage.
func TestSetupEnabled(t *testing.T) {
	before := otel.GetTracerProvider()
	defer otel.SetTracerProvider(before)
	shutdown, err := Setup(context.Background(), Options{Enabled: true, Endpoint: "http://127.0.0.1:1", SampleRatio: 1, Service: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Errorf("provider %T", otel.GetTracerProvider())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_ = shutdown(ctx) // collecteur absent : l'erreur est attendue, le test vérifie l'absence de blocage
}
