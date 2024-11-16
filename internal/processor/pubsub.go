package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/fraser-isbester/fwd/pkg/types"
)

// PubSubConfig holds configuration for the PubSub processor
type PubSubConfig struct {
	ProjectID string
	TopicName string
	// Buffer size for the incoming event channel
	BufferSize int
	// PubSub specific settings
	BatchSize    int           // Number of messages to batch
	BatchBytes   int           // Size of message batches in bytes
	BatchTimeout time.Duration // How long to wait before sending a batch
}

// DefaultPubSubConfig returns sensible defaults
func DefaultPubSubConfig() PubSubConfig {
	return PubSubConfig{
		BufferSize:   1000,
		BatchSize:    100,
		BatchBytes:   1000000, // 1MB
		BatchTimeout: 100 * time.Millisecond,
	}
}

// PubSubProcessor implements Processor interface for Google Cloud Pub/Sub
type PubSubProcessor struct {
	config    PubSubConfig
	client    *pubsub.Client
	topic     *pubsub.Topic
	sources   []DataSource
	eventChan chan *types.Event
	wg        sync.WaitGroup
}

// NewPubSubProcessor creates a new GCP Pub/Sub processor
func NewPubSubProcessor(config PubSubConfig) (*PubSubProcessor, error) {
	ctx := context.Background()

	client, err := pubsub.NewClient(ctx, config.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %v", err)
	}

	topic := client.Topic(config.TopicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to check if topic exists: %v", err)
	}

	if !exists {
		fmt.Printf("Creating topic %s\n", config.TopicName)
		topic, err = client.CreateTopic(ctx, config.TopicName)
		if err != nil {
			client.Close()
			return nil, fmt.Errorf("failed to create topic: %v", err)
		}
	}

	// Configure publisher settings
	topic.PublishSettings = pubsub.PublishSettings{
		ByteThreshold:  config.BatchBytes,
		CountThreshold: config.BatchSize,
		DelayThreshold: config.BatchTimeout,
	}

	return &PubSubProcessor{
		config:    config,
		client:    client,
		topic:     topic,
		eventChan: make(chan *types.Event, config.BufferSize),
	}, nil
}

// RegisterSource implements Processor interface
func (p *PubSubProcessor) RegisterSource(source DataSource) {
	p.sources = append(p.sources, source)
}

// Start implements Processor interface
func (p *PubSubProcessor) Start(ctx context.Context) error {
	// Start all registered sources
	for _, source := range p.sources {
		if err := source.Start(ctx, p.eventChan); err != nil {
			return fmt.Errorf("failed to start source %s: %v", source.Name(), err)
		}
	}

	// Start processing events
	p.wg.Add(1)
	go p.processEvents(ctx)

	return nil
}

func (p *PubSubProcessor) processEvents(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case event, ok := <-p.eventChan:
			if !ok {
				return
			}

			if err := p.publishEvent(ctx, event); err != nil {
				log.Printf("Error publishing event to Pub/Sub: %v", err)
				// TODO: Consider implementing retry logic or dead letter queue
				continue
			}

		case <-ctx.Done():
			return
		}
	}
}

func (p *PubSubProcessor) publishEvent(ctx context.Context, event *types.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %v", err)
	}

	msg := &pubsub.Message{
		Data: data,
		Attributes: map[string]string{
			"type":   event.Type,
			"source": event.Source,
			"id":     event.ID,
		},
	}

	result := p.topic.Publish(ctx, msg)

	id, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to publish message: %v", err)
	}

	log.Printf("Published event %s to Pub/Sub with message ID: %s", event.ID, id)
	return nil
}

// Stop implements Processor interface
func (p *PubSubProcessor) Stop() error {
	// Stop all sources first
	for _, source := range p.sources {
		if err := source.Stop(); err != nil {
			log.Printf("Error stopping source %s: %v", source.Name(), err)
		}
	}

	// Close the event channel
	close(p.eventChan)

	// Wait for event processing to complete
	p.wg.Wait()

	// Flush any pending pub/sub messages and clean up resources
	p.topic.Stop()
	return p.client.Close()
}

// Name implements Processor interface
func (p *PubSubProcessor) Name() string {
	return fmt.Sprintf("pubsub-%s", p.config.TopicName)
}
