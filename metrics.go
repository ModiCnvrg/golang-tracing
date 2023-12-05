package main

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/semconv/v1.12.0"
	"log"
)

// MetricsOption represents the configuration option for initializing metrics.
type MetricsOption func(*MetricsConfig)

// MetricsConfig represents the configuration for initializing metrics.
type MetricsConfig struct {
	ExporterType string
	ExporterOpts []otlpmetricgrpc.Option
	Labels       []attribute.KeyValue
}

func WithExporterOpts(exporterOpts ...otlpmetricgrpc.Option) MetricsOption {
	return func(cfg *MetricsConfig) {
		cfg.ExporterOpts = append(cfg.ExporterOpts, exporterOpts...)
	}
}

func WithLabels(labels ...attribute.KeyValue) MetricsOption {
	return func(cfg *MetricsConfig) {
		cfg.Labels = labels
	}
}

func initGrpcMetrics(ctx context.Context, config MetricsConfig) {
	// Set up metrics exporter
	metricsExporter, err := otlpmetricgrpc.New(ctx, config.ExporterOpts...)
	if err != nil {
		log.Fatalf("failed to create metrics exporter: %v", err)
	}

	stdoutexporter, err := stdoutmetric.New()

	// Create batch metrics processor
	mp := metric.NewMeterProvider(
		metric.WithResource(resource.NewWithAttributes(semconv.SchemaURL, config.Labels...)),
		metric.WithReader(metric.NewPeriodicReader(metricsExporter)),
		metric.WithReader(metric.NewPeriodicReader(stdoutexporter)),
	)

	// Set the global meter provider
	otel.SetMeterProvider(mp)
}
