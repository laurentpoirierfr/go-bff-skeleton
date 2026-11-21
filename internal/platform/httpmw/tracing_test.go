package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracing(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	before := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	defer otel.SetTracerProvider(before)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orders/{id}/cancellation", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /health/live", func(http.ResponseWriter, *http.Request) {})
	h := Tracing(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/orders/0193/cancellation", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health/live", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans, want 1 (probes are not traced)", len(spans))
	}
	if spans[0].Name() != "POST /v1/orders/{id}/cancellation" {
		t.Errorf("span name %q", spans[0].Name())
	}
}
