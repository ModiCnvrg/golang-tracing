package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/AccessibleAI/observability/pkg/metering"
	"github.com/AccessibleAI/observability/pkg/trace"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	_ "go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"opetelemetry-and-go/logging"
	"os"
	"time"
)

const (
	cxApplicationName = "observability-poc"
	cxSubsystemName   = "instrumentation-traces"
)

func getEnv(key, fallback string) string {
	log := logging.NewLogrus(context.Background())
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
		log.Infof("returning falback value %v for key %v", fallback, key)
	}
	log.Infof("returning value %v for key %v", value, key)
	return value
}

func main() {
	ctx := context.Background()
	tracesCollectorEndpoint := getEnv("traces-collector-addr", "nil")

	metering.InitGrpcMetricsProvider(ctx, tracesCollectorEndpoint, "a"+cxApplicationName, "a"+cxSubsystemName)
	go serviceA(ctx, 8081)

	//go func() {
	//	for {
	//		ctx, span := otel.Tracer("myTracer").Start(ctx, "")
	//		time.Sleep(2 * time.Second)
	//		log := logging.NewLogrus(ctx)
	//		log.Info("just printing")
	//		span.End()
	//	}
	//}()
	serviceB(ctx, 8082)
}

// curl -vkL http://127.0.0.1:8081/serviceA
func serviceA(ctx context.Context, port int) {
	r := gin.New()
	var tp oteltrace.TracerProvider
	var err error
	err = setupTracing(ctx, "Service A Trace Provider")
	if err != nil {
		panic(err)
	}
	r.Use(otelgin.Middleware("service A", otelgin.WithTracerProvider(tp)), RequestDurationMiddleware())
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
	var tp oteltrace.TracerProvider
	var err error
	err = setupTracing(ctx, "Service A Trace Provider")
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
			oteltrace.WithAttributes(attribute.String("component", "addition")),
			oteltrace.WithAttributes(attribute.String("someKey", "someValue")),
			oteltrace.WithAttributes(attribute.Int("age", 89)),
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

func RequestDurationMiddleware() gin.HandlerFunc {
	// define a meter
	meter := otel.Meter("goapp")
	// Create two synchronous instruments: counter and histogram
	reqCounter, err := meter.Int64Counter(
		"http.request.counter",
		metric.WithDescription("HTTP Request counter"),
	)
	if err != nil {
		panic(err)
	}
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		start := time.Now()

		// Call the next handler in the chain.
		c.Next()

		// Calculate request duration.
		duration := time.Since(start)

		// Obtain the current span from the context.
		span := oteltrace.SpanFromContext(ctx)

		// Add the duration as an attribute to the span.
		span.SetAttributes(attribute.Int64("request.duration.ms", duration.Milliseconds()))

		// You can also send the duration as a separate metric.
		reqCounter.Add(ctx, 1)

	}
}

func setupTracing(ctx context.Context, serviceName string) error {
	tracesCollectorEndpoint := getEnv("traces-collector-addr", "")
	trace.InitGrpcTraceProvider(ctx, tracesCollectorEndpoint, "a"+cxApplicationName, "a"+cxSubsystemName)
	return nil
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
