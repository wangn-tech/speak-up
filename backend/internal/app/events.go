package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

func RunOutboxPublisher(ctx context.Context, store Store, brokers []string) {
	if len(brokers) == 0 {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := store.PendingOutbox(ctx, 20)
			if err != nil {
				slog.Error("load outbox", "error", err)
				continue
			}
			for _, event := range events {
				writer := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: event.Topic, Balancer: &kafka.Hash{}}
				err = writer.WriteMessages(ctx, kafka.Message{Key: []byte(event.Key), Value: event.Payload})
				_ = writer.Close()
				if err != nil {
					slog.Error("publish outbox", "event_id", event.EventID, "error", err)
					continue
				}
				if err = store.MarkOutboxPublished(ctx, event.EventID); err != nil {
					slog.Error("mark outbox published", "event_id", event.EventID, "error", err)
				}
			}
		}
	}
}

func RunEvaluationConsumer(ctx context.Context, store Store, brokers []string) {
	if len(brokers) == 0 {
		return
	}
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: "evaluation.completed", GroupID: "speakup-gateway", MinBytes: 1, MaxBytes: 10e6})
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			slog.Error("close evaluation reader", "error", closeErr)
		}
	}()
	for {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("read evaluation completion", "error", err)
			continue
		}
		var completed EvaluationCompleted
		if err = json.Unmarshal(message.Value, &completed); err != nil {
			slog.Error("decode evaluation completion", "error", err)
			_ = reader.CommitMessages(ctx, message)
			continue
		}
		if err = store.CompleteEvaluation(ctx, completed); err != nil {
			slog.Error("persist evaluation completion", "event_id", completed.EventID, "error", err)
			continue
		}
		if err = reader.CommitMessages(ctx, message); err != nil {
			slog.Error("commit evaluation completion", "error", err)
		}
	}
}
