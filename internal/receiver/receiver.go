// Defines webhook receiver logic
package receiver

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/fraser-isbester/fwd/pkg/types"
)

// WebhookConfig holds configuration for the webhook receiver
type WebhookConfig struct {
	Port int
}

// DefaultConfig returns a default webhook configuration
func DefaultConfig() WebhookConfig {
	return WebhookConfig{
		Port: 8080,
	}
}

// WebhookReceiver handles incoming webhook requests
type WebhookReceiver struct {
	config    WebhookConfig
	eventChan chan<- *types.Event
	server    *http.Server
	handlers  map[string]WebhookHandler
}

// WebhookHandler interface defines the contract for source-specific webhook handlers
type WebhookHandler interface {
	Path() string
	HandleWebhook(w http.ResponseWriter, r *http.Request, eventChan chan<- *types.Event)
}

// NewWebhookReceiver creates a new webhook receiver
func NewWebhookReceiver(config WebhookConfig) *WebhookReceiver {
	return &WebhookReceiver{
		config:   config,
		handlers: make(map[string]WebhookHandler),
	}
}

// RegisterHandler adds a new webhook handler for a specific source
func (wr *WebhookReceiver) RegisterHandler(name string, handler WebhookHandler) {
	wr.handlers[name] = handler
}

// Start begins listening for webhook requests
func (wr *WebhookReceiver) Start(ctx context.Context, eventChan chan<- *types.Event) error {
	wr.eventChan = eventChan

	mux := http.NewServeMux()

	// Register all handlers
	for _, handler := range wr.handlers {
		path := handler.Path()
		h := handler // Create a new variable to avoid closure problems
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			h.HandleWebhook(w, r, eventChan)
		})
		log.Printf("Registered webhook handler for path: %s", path)
	}

	wr.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", wr.config.Port),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting webhook receiver on port %d", wr.config.Port)
		if err := wr.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Webhook server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the webhook receiver
func (wr *WebhookReceiver) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return wr.server.Shutdown(ctx)
}

// Name returns the name of this collector
func (wr *WebhookReceiver) Name() string {
	return "webhook"
}
