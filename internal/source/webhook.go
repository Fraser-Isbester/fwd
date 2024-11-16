package source

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

type WebhookSource struct {
	port     int
	server   *http.Server
	handlers map[string]http.HandlerFunc
}

func NewWebhookSource(port int) *WebhookSource {
	log.Printf("Creating webhook source on port %d", port)
	return &WebhookSource{
		port:     port,
		handlers: make(map[string]http.HandlerFunc),
	}
}

func (ws *WebhookSource) AddHandlerFunc(path string, handler http.HandlerFunc) {
	log.Printf("Adding webhook handler for path: %s", path)
	ws.handlers[path] = handler
}

func (ws *WebhookSource) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Add health check endpoint first
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Add all other handlers
	for path, handler := range ws.handlers {
		mux.HandleFunc(path, handler)
	}

	ws.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ws.port),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting webhook server on port %d", ws.port)
		if err := ws.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Webhook server error: %v", err)
		}
	}()

	// Verify server is actually listening
	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", ws.port))
		if err == nil {
			resp.Body.Close()
			log.Printf("Webhook server is up and running")
			return nil
		}
	}

	return fmt.Errorf("webhook server failed to start")
}

func (ws *WebhookSource) Stop() error {
	if ws.server != nil {
		log.Printf("Stopping webhook server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ws.server.Shutdown(ctx)
	}
	return nil
}
