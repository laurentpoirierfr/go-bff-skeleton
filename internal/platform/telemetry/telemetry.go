// Package telemetry configure OpenTelemetry : les traces sont envoyées en
// OTLP (HTTP) au collecteur, qui les relaie vers les backends (Jaeger). Le
// contexte de trace se propage au format W3C (en-tête traceparent), entre
// services HTTP comme dans les messages.
package telemetry

import (
	"context"
	"errors"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Options règle la télémétrie.
type Options struct {
	Enabled     bool
	Endpoint    string  // URL du collecteur OTLP HTTP : http://localhost:4318
	SampleRatio float64 // part des traces gardées, de 0 à 1
	Service     string
	Version     string
}

// Setup installe le fournisseur de traces global, et renvoie la fonction qui
// envoie les dernières traces à l'arrêt. Désactivée, la télémétrie ne crée
// aucune trace, mais le contexte reçu (traceparent) est quand même propagé.
func Setup(ctx context.Context, o Options) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !o.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(strings.TrimSuffix(o.Endpoint, "/")+"/v1/traces"))
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", o.Service),
		attribute.String("service.version", o.Version),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// ParentBased : une trace commencée ailleurs (requête entrante, message)
		// garde la décision de son origine ; seules les nouvelles sont tirées.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(o.SampleRatio))),
	)
	otel.SetTracerProvider(tp)
	return func(ctx context.Context) error {
		return errors.Join(tp.ForceFlush(ctx), tp.Shutdown(ctx))
	}, nil
}

// Inject écrit le contexte de trace de ctx dans des métadonnées (celles d'un
// message, par exemple).
func Inject(ctx context.Context, metadata map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(metadata))
}

// Extract lit le contexte de trace de métadonnées, et l'ajoute à ctx.
func Extract(ctx context.Context, metadata map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(metadata))
}
