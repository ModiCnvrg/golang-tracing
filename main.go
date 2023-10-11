package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
	"log"
	"opetelemetry-and-go/logging"
	"opetelemetry-and-go/otelconfluent"
	"os"
)

var (
	brokers       = *flag.String("brokers", "localhost:9092", "The Kafka bootstrap servers to connect to, as a comma separated list")
	kafkaProducer *otelconfluent.Producer
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
	{
		var tp trace.TracerProvider
		var err error
		tp, err = setupTracing(ctx, "Service A Trace Provider")
		if err != nil {
			panic(err)
		}
		kafkaProducer = InitProducer(tp)
	}
	{
		var tp trace.TracerProvider
		var err error
		tp, err = setupTracing(ctx, "Service A Trace Provider")
		if err != nil {
			panic(err)
		}

		go InitConsumer(tp)
	}
	go serviceA(ctx, 8081)
	serviceB(ctx, 8082)
}

func InitConsumer(tp trace.TracerProvider) {
	flag.Parse()

	// Initialize an original Kafka consumer.
	confluentConsumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"group.id":           "example",
		"enable.auto.commit": false,
		"auto.offset.reset":  "earliest",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Initialize OpenTelemetry trace provider and wrap the original kafka consumer.
	consumer := otelconfluent.NewConsumerWithTracing(confluentConsumer, otelconfluent.WithTracerProvider(tp))
	defer func() { _ = consumer.Close() }()

	// Subscribe consumer to topic.
	if err := consumer.Subscribe(otelconfluent.KafkaTopic, nil); err != nil {
		log.Fatal(err)
	}

	handler := func(ctx context.Context, consumer *kafka.Consumer, msg *kafka.Message) error {
		log := logging.NewLogrus(ctx)
		log.Info("message received with key: " + string(msg.Key))
		return nil
	}

	// Read one message from the topic.
	for {
		event := consumer.PollWithHandler(10*1000, handler)
		log.Println(event)
	}

	// Or you can still use the ReadMessage(timeout) or Poll(timeoutMs) methods but you will not
	// be able to obtain the right handling duration because they will only return the Kafka message to you.
	//
	// msg, err := consumer.ReadMessage(10*time.Second)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	//
	// println("message received with key: " + string(msg.Key))
}

func InitProducer(tp trace.TracerProvider) *otelconfluent.Producer {
	flag.Parse()

	// Initialize an original Kafka producer.
	confluentProducer, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
	})
	if err != nil {
		panic(err)
	}

	// Initialize OpenTelemetry trace provider and wrap the original kafka producer.
	producer := otelconfluent.NewProducerWithTracing(confluentProducer, otelconfluent.WithTracerProvider(tp))
	return producer
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
	host := "localhost"
	hostAddress := fmt.Sprintf("%s:%d", host, port)
	err = r.Run(hostAddress)
	if err != nil {
		panic(err)
	}
}

func serviceA_HttpHandler(c *gin.Context) {
	ctx, span := otel.Tracer("myTracer").Start(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.Request.RequestURI))
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "service A",
	})
	log.Info("serviceA_HttpHandler_called")
	defer span.End()

	// Create a kafka message and produce it in topic.
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &otelconfluent.KafkaTopic},
		Key:            []byte("test-key"),
		Value:          []byte("test-value"),
	}

	if err := kafkaProducer.Produce(ctx, msg, nil); err != nil {
		log.Fatal(err)
	}

	//kafkaProducer.Flush(5000)

	log.Infof("message sent with key: " + string(msg.Key))
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
	host := "localhost"
	hostAddress := fmt.Sprintf("%s:%d", host, port)
	err = r.Run(hostAddress)
	if err != nil {
		panic(err)
	}
}

func serviceB_HttpHandler(c *gin.Context) {
	ctx, span := otel.Tracer("myTracer").Start(c.Request.Context(), fmt.Sprintf("%s %s", c.Request.Method, c.Request.RequestURI))
	log := logging.NewLogrus(ctx).WithFields(logrus.Fields{
		"component": "service B"})
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
