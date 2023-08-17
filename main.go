package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc/credentials"
	"net/http"
	"opetelemetry-and-go/logging"
	kafkaProducer "opetelemetry-and-go/producer"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/metric/instrument"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
	"opetelemetry-and-go/kafkaConsumer"
)

const (
	serviceName = "AdderSvc"
)

var tp trace.TracerProvider

func main() {
	ctx := context.Background()
	{
		tp, err := setupTracing(ctx, serviceName)
		if err != nil {
			panic(err)
		}
		defer tp.Shutdown(ctx)

		mp, err := setupMetrics(ctx, serviceName)
		if err != nil {
			panic(err)
		}
		defer mp.Shutdown(ctx)
	}

	go serviceA(ctx, 8081)
	serviceB(ctx, 8082)
}

// curl -vkL http://127.0.0.1:8081/serviceA
func serviceA(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/serviceA", serviceA_HttpHandler)
	handler := otelhttp.NewHandler(mux, "server A.http")
	serverPort := fmt.Sprintf(":%d", port)
	server := &http.Server{Addr: serverPort, Handler: handler}

	fmt.Println("serviceA listening on", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func serviceA_HttpHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("myTracer").Start(r.Context(), "serviceA_HttpHandler")
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "service A",
		"age":       89,
	})
	log.Info("serviceA_HttpHandler_called")
	defer span.End()

	cli := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	kafkaProducer.InitProducer(ctx, tp)
	go kafkaConsumer.InitConsumer(tp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8082/serviceB", nil)
	if err != nil {
		panic(err)
	}
	resp, err := cli.Do(req)
	if err != nil {
		panic(err)
	}

	w.Header().Add("SVC-RESPONSE", resp.Header.Get("SVC-RESPONSE"))
}

func serviceB(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/serviceB", serviceB_HttpHandler)
	handler := otelhttp.NewHandler(mux, "server B.http")
	serverPort := fmt.Sprintf(":%d", port)
	server := &http.Server{Addr: serverPort, Handler: handler}

	fmt.Println("serviceB listening on", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func serviceB_HttpHandler(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("myTracer").Start(r.Context(), "serviceB_HttpHandler")
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "service B",
		"age":       89,
	})
	log.Info("serviceB_HttpHandler_called")
	defer span.End()

	answer := add(ctx, 42, 1813)
	w.Header().Add("SVC-RESPONSE", fmt.Sprint(answer))
	fmt.Fprintf(w, "hello from serviceB: Answer is: %d", answer)
	log.Info("hello from serviceB: Answer is: %d", answer)
}

func add(ctx context.Context, x, y int64) int64 {
	ctx, span := otel.Tracer("myTracer").Start(
		ctx,
		"add",
		// add labels/tags/resources(if any) that are specific to this scope.
		trace.WithAttributes(attribute.String("component", "addition")),
		trace.WithAttributes(attribute.String("someKey", "someValue")),
		trace.WithAttributes(attribute.Int("age", 89)),
	)
	defer span.End()

	counter, _ := global.MeterProvider().
		Meter(
			"instrumentation/package/name",
			metric.WithInstrumentationVersion("0.0.1"),
		).
		Int64Counter(
			"add_counter",
			instrument.WithDescription("how many times add function has been called."),
		)
	counter.Add(
		ctx,
		1,
		// labels/tags
		attribute.String("component", "addition"),
		attribute.Int("age", 89),
	)

	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "addition",
		"age":       89,
	})
	log.Info("add_called")

	return x + y
}

func setupTracing(ctx context.Context, serviceName string) (*sdkTrace.TracerProvider, error) {
	c, err := getTls()
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithTLSCredentials(
			// mutual tls.
			credentials.NewTLS(c),
		),
	)
	if err != nil {
		return nil, err
	}

	// labels/tags/resources that are common to all traces.
	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("some-attribute", "some-value"),
	)

	provider := sdkTrace.NewTracerProvider(
		sdkTrace.WithBatcher(exporter),
		sdkTrace.WithResource(resource),
		// set the sampling rate based on the parent span to 60%
		sdkTrace.WithSampler(sdkTrace.ParentBased(sdkTrace.TraceIDRatioBased(1))),
	)

	otel.SetTracerProvider(provider)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, // W3C Trace Context format; https://www.w3.org/TR/trace-context/
		),
	)

	return provider, nil
}

// getTls returns a configuration that enables the use of mutual TLS.
func getTls() (*tls.Config, error) {
	clientAuth, err := tls.LoadX509KeyPair("./confs/client.crt", "./confs/client.key")
	if err != nil {
		return nil, err
	}

	caCert, err := os.ReadFile("./confs/rootCA.crt")
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	c := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{clientAuth},
	}

	return c, nil
}

func setupMetrics(ctx context.Context, serviceName string) (*sdkmetric.MeterProvider, error) {
	c, err := getTls()
	if err != nil {
		return nil, err
	}

	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint("localhost:4317"),
		otlpmetricgrpc.WithTLSCredentials(
			// mutual tls.
			credentials.NewTLS(c),
		),
	)
	if err != nil {
		return nil, err
	}

	// labels/tags/resources that are common to all metrics.
	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("some-attribute", "some-value"),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource),
		sdkmetric.WithReader(
			// collects and exports metric data every 30 seconds.
			sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(30*time.Second)),
		),
	)

	global.SetMeterProvider(mp)

	return mp, nil
}
