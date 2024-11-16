package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fraser-isbester/fwd/internal/processor"
	"github.com/fraser-isbester/fwd/internal/receiver"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	// Create processor configuration
	config := processor.DefaultPubSubConfig()
	config.ProjectID = "main-414707"
	config.TopicName = "opslog-events"

	// Initialize the processor
	proc, err := processor.NewPubSubProcessor(config)
	if err != nil {
		log.Fatalf("Failed to create processor: %v", err)
	}

	// Setup and register webhook receiver
	webhookReceiver := receiver.NewWebhookReceiver(receiver.DefaultConfig())
	githubHandler := receiver.NewGitHubWebhookHandler("secret")
	webhookReceiver.RegisterHandler("github", githubHandler)
	proc.RegisterSource(webhookReceiver)

	// Start the processor
	if err := proc.Start(ctx); err != nil {
		log.Fatalf("Failed to start processor: %v", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	cancel()
	if err := proc.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
}
