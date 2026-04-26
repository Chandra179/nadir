package otel

import (
	"context"
	"fmt"
	"net/http"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Provider holds the OTEL MeterProvider and exposes a Prometheus /metrics handler.
type Provider struct {
	mp      *sdkmetric.MeterProvider
	handler http.Handler
}

// NewPrometheusProvider creates a MeterProvider backed by a fresh Prometheus registry.
// Call Close() on shutdown to flush last data points.
func NewPrometheusProvider() (*Provider, error) {
	reg := promclient.NewRegistry()
	exp, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp))
	return &Provider{
		mp:      mp,
		handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	}, nil
}

// Meter returns a named meter for instrument registration.
func (p *Provider) Meter(name string) metric.Meter {
	return p.mp.Meter(name)
}

// HTTPHandler returns the Prometheus /metrics HTTP handler.
func (p *Provider) HTTPHandler() http.Handler {
	return p.handler
}

// Close shuts down the MeterProvider (flushes pending data).
func (p *Provider) Close() error {
	return p.mp.Shutdown(context.Background())
}
