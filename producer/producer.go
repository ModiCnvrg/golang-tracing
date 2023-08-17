/**
 * Copyright 2018 Confluent Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kafkaProducer

import (
	"context"
	"flag"
	"go.opentelemetry.io/otel/trace"
	"log"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"opetelemetry-and-go/otelconfluent"
)

var (
	brokers = flag.String("brokers", "localhost:9092", "The Kafka bootstrap servers to connect to, as a comma separated list")
)

func InitProducer(ctx context.Context, tp trace.TracerProvider) {
	flag.Parse()

	// Initialize an original Kafka producer.
	confluentProducer, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": *brokers,
	})
	if err != nil {
		panic(err)
	}

	// Initialize OpenTelemetry trace provider and wrap the original kafka producer.
	producer := otelconfluent.NewProducerWithTracing(confluentProducer, otelconfluent.WithTracerProvider(tp))
	defer producer.Close()

	// Create a kafka message and produce it in topic.
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &otelconfluent.KafkaTopic},
		Key:            []byte("test-key"),
		Value:          []byte("test-value"),
	}

	if err := producer.Produce(ctx, msg, nil); err != nil {
		log.Fatal(err)
	}

	producer.Flush(5000)

	println("message sent with key: " + string(msg.Key))
}
