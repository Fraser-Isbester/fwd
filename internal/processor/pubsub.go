package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/pubsub"
	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type PubSubConfig struct {
	ProjectID    string
	TopicName    string
	BatchSize    int
	BatchBytes   int
	BatchTimeout time.Duration
}

func DefaultConfig() PubSubConfig {
	return PubSubConfig{
		BatchSize:    100,
		BatchBytes:   1000000, // 1MB
		BatchTimeout: 100 * time.Millisecond,
	}
}

type PubSubProcessor struct {
	config    PubSubConfig
	client    *pubsub.Client
	topic     *pubsub.Topic
	eventChan chan *cloudevents.Event
	done      chan struct{}
}

func NewPubSubProcessor(config PubSubConfig) (*PubSubProcessor, error) {
	ctx := context.Background()

	client, err := pubsub.NewClient(ctx, config.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	topic := client.Topic(config.TopicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to check if topic exists: %w", err)
	}

	if !exists {
		topic, err = client.CreateTopic(ctx, config.TopicName)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to create topic: %w", err)
		}
	}

	topic.PublishSettings = pubsub.PublishSettings{
		ByteThreshold:  config.BatchBytes,
		CountThreshold: config.BatchSize,
		DelayThreshold: config.BatchTimeout,
	}

	return &PubSubProcessor{
		config:    config,
		client:    client,
		topic:     topic,
		eventChan: make(chan *cloudevents.Event, 1000),
		done:      make(chan struct{}),
	}, nil
}

func (p *PubSubProcessor) EventChannel() chan<- *cloudevents.Event {
	return p.eventChan
}

func (p *PubSubProcessor) Start(ctx context.Context) error {
	log.Printf("Starting PubSub processor for topic: %s", p.config.TopicName)
	go p.processEvents(ctx)
	return nil
}

func (p *PubSubProcessor) processEvents(ctx context.Context) {
	defer close(p.done)

	for {
		select {
		case event, ok := <-p.eventChan:
			if !ok {
				log.Printf("Event channel closed, stopping processor")
				return
			}
			if event == nil {
				log.Printf("Received nil event, skipping")
				continue
			}
			if err := p.publishEvent(ctx, event); err != nil {
				log.Printf("Error publishing event: %v", err)
			}
		case <-ctx.Done():
			log.Printf("Context cancelled, stopping processor")
			return
		}
	}
}

func (p *PubSubProcessor) publishEvent(ctx context.Context, event *cloudevents.Event) error {
	if event == nil {
		return fmt.Errorf("cannot publish nil event")
	}

	// Serialize the CloudEvent
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create attributes map for PubSub message
	attrs := map[string]string{
		"ce-id":          event.ID(),
		"ce-source":      event.Source(),
		"ce-type":        event.Type(),
		"ce-specversion": event.SpecVersion(),
		"content-type":   "application/cloudevents+json; charset=UTF-8",
	}

	// Add any CloudEvent extensions as PubSub attributes
	for name, value := range event.Extensions() {
		// Convert extension value to string
		if str, ok := value.(string); ok {
			attrs[name] = str
		} else if strVal, err := json.Marshal(value); err == nil {
			attrs[name] = string(strVal)
		}
	}

	// Create PubSub message
	msg := &pubsub.Message{
		Data:       data,
		Attributes: attrs,
	}

	// Publish and wait for result
	result := p.topic.Publish(ctx, msg)
	id, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	log.Printf("Published event %s with id %s", event.ID(), id)
	return nil
}

func (p *PubSubProcessor) Stop() error {
	log.Printf("Stopping PubSub processor")
	close(p.eventChan)
	<-p.done
	p.topic.Stop()
	return p.client.Close()
}
