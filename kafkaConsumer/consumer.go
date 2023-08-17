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

package kafkaConsumer

import (
	"context"
	"flag"
	"log"
	"opetelemetry-and-go/logging"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"go.opentelemetry.io/otel/trace"
	"opetelemetry-and-go/otelconfluent"
)

var (
	brokers = "localhost:9092"
)

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
		_, err = consumer.ReadMessageWithHandler(10*time.Second, handler)
		if err != nil {
			log.Fatal(err)
		}
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
