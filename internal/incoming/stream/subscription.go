// Licensed to The Moov Authors under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. The Moov Authors licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package stream

import (
	"context"
	"fmt"

	"github.com/Shopify/sarama"
	"github.com/moov-io/achgateway/internal/kafka"
	"github.com/moov-io/achgateway/internal/service"
	"github.com/moov-io/base/log"

	"gocloud.dev/pubsub"
)

type Subscription interface {
	Receive(ctx context.Context) (*pubsub.Message, error)
	Shutdown(ctx context.Context) error
}

func OpenSubscription(logger log.Logger, cfg *service.Config) (Subscription, error) {
	if cfg.Inbound.InMem != nil {
		sub, err := pubsub.OpenSubscription(context.Background(), cfg.Inbound.InMem.URL)
		if err != nil {
			return nil, err
		}
		logger.Info().Logf("setup %T inmem subscription", sub)
		return sub, nil
	}
	if cfg.Inbound.Kafka != nil {
		sub, err := kafka.OpenSubscription(logger, cfg.Inbound.Kafka)
		if err != nil {
			return nil, err
		}
		logger.Info().Logf("setup %T kafka subscription", sub)
		return &kafkaSubscription{sub: sub}, nil
	}
	return nil, nil
}

type kafkaSubscription struct {
	sub *pubsub.Subscription
}

func (ks *kafkaSubscription) Receive(ctx context.Context) (*pubsub.Message, error) {
	msg, err := ks.sub.Receive(ctx)
	if err != nil {
		var consumerError sarama.ConsumerError
		if ks.sub.ErrorAs(err, &consumerError) {
			return msg, fmt.Errorf("consumer error receiving message: %w", consumerError)
		}
		var consumerErrors sarama.ConsumerErrors
		if ks.sub.ErrorAs(err, &consumerErrors) {
			return msg, fmt.Errorf("consumer errors receiving message: %w", consumerErrors)
		}
		return msg, fmt.Errorf("error receiving message: %w", err)
	}
	return msg, nil
}

func (ks *kafkaSubscription) Shutdown(ctx context.Context) error {
	return ks.sub.Shutdown(ctx)
}
