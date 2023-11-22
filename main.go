package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	_ "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
	"log"
	"opetelemetry-and-go/logging"
	"os"
	"time"
)

const (
	cxApplicationName = "observability-poc"
	cxSubsystemName   = "instrumentation-traces"
)

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func main() {
	ctx := context.Background()
	go serviceA(ctx, 8081)
	//go func() {
	//	for {
	//		ctx, _ := otel.Tracer("myTracer").Start(ctx, "")
	//		time.Sleep(2 * time.Second)
	//		log := logging.NewLogrus(ctx)
	//		log.Info("just printing")
	//	}
	//}
	serviceB(ctx, 8082)
}

// curl -vkL http://127.0.0.1:8081/serviceA
func serviceA(ctx context.Context, port int) {
	r := gin.New()
	var tp trace.TracerProvider
	var err error
	tp, err = setupTracing(ctx, "Service A Trace Provider")
	if err != nil {
		panic(err)
	}
	r.Use(otelgin.Middleware("service A", otelgin.WithTracerProvider(tp)))
	r.GET("/serviceA", serviceA_HttpHandler)
	fmt.Println("serviceA listening on", r.BasePath())
	host := "0.0.0.0"
	hostAddress := fmt.Sprintf("%s:%d", host, port)
	err = r.Run(hostAddress)
	if err != nil {
		panic(err)
	}
}

func serviceA_HttpHandler(c *gin.Context) {
	//r := c.Request
	ctx, span := otel.Tracer("myTracer").Start(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.Request.RequestURI))
	span.SetAttributes(
		attribute.String("span.kind", "server"),
	)
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "service A",
	})
	log.Info("serviceA_HttpHandler_called")
	defer span.End()

	resp, err := otelhttp.Get(c.Request.Context(), "http://localhost:8082/serviceB")

	if err != nil {
		panic(err)
	}

	c.Writer.Header().Add("SVC-RESPONSE", resp.Header.Get("SVC-RESPONSE"))
}

func serviceB(ctx context.Context, port int) {
	r := gin.New()
	var tp trace.TracerProvider
	var err error
	tp, err = setupTracing(ctx, "Service A Trace Provider")
	if err != nil {
		panic(err)
	}

	r.Use(otelgin.Middleware("service B", otelgin.WithTracerProvider(tp)))
	r.GET("/serviceB", serviceB_HttpHandler)
	fmt.Println("serviceB listening on", r.BasePath())
	host := "0.0.0.0"
	hostAddress := fmt.Sprintf("%s:%d", host, port)
	err = r.Run(hostAddress)
	if err != nil {
		panic(err)
	}
}

func serviceB_HttpHandler(c *gin.Context) {
	ctx, span := otel.Tracer("myTracer").Start(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.Request.RequestURI))
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{"component": "service B"})
	log.Info("serviceB_HttpHandler_called")
	for k, vals := range c.Request.Header {
		log.Infof("%s", k)
		for _, v := range vals {
			log.Infof("\t%s", v)
		}
	}

	defer span.End()
	add := func(ctx context.Context, x, y int64) int64 {
		ctx, span := otel.Tracer("myTracer").Start(
			ctx,
			"add",
			// add labels/tags/resources(if any) that are specific to this scope.
			trace.WithAttributes(attribute.String("component", "addition")),
			trace.WithAttributes(attribute.String("someKey", "someValue")),
			trace.WithAttributes(attribute.Int("age", 89)),
		)
		defer span.End()

		log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
			"component": "addition",
			"age":       89,
		})
		log.Info("add_called")

		return x + y
	}

	answer := add(ctx, 42, 1813)
	c.Writer.Header().Add("SVC-RESPONSE", fmt.Sprint(answer))
	log.Info("hello from serviceB: Answer is: %d", answer)
}

func setupTracing(ctx context.Context, serviceName string) (*sdktrace.TracerProvider, error) {
	// 1. define trace connection options
	var headers = map[string]string{
		"Authorization": "Bearer " + os.Getenv("CX_TOKEN"),
	}
	tracesCollectorEndpoint := getEnv("traces-collector-addr", "ingress.coralogix.us:443")

	traceConnOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithTimeout(1 * time.Second),
		otlptracegrpc.WithEndpoint(tracesCollectorEndpoint),
		otlptracegrpc.WithHeaders(headers),
		otlptracegrpc.WithTLSCredentials(credentials.NewTLS(&tls.Config{})),
	}

	// 2. set up a trace exporter
	exporter, err := otlptracegrpc.New(ctx, traceConnOpts...)
	if err != nil {
		log.Fatalf("failed to create trace exporter: %v", err)
	}

	// 3 define span resource attributes,
	// these resource attributes will be added to all Spans
	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("go-manual-instro-traces-example"),

		// cx.application.name and cx.subsystem.name are required for the
		// spans being sent to the coralogix platform
		attribute.String("cx.application.name", cxApplicationName),
		attribute.String("cx.subsystem.name", cxSubsystemName),
	)

	// 4. create batch span processor
	//      Note: SpanProcessor is a processing pipeline for spans in the trace signal.
	//      SpanProcessors registered with a TracerProvider and are called at the start and end of a
	//      Span's lifecycle, and are called in the order they are registered.
	//      https://pkg.go.dev/go.opentelemetry.io/otel/sdk/trace#SpanProcessor
	sp := sdktrace.NewSimpleSpanProcessor(exporter)

	// 5. add span processor and resource attributes to the trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource),
		sdktrace.WithSpanProcessor(sp),
	)

	// 6. set the global trace provider
	otel.SetTracerProvider(tp)
	return tp, nil
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
