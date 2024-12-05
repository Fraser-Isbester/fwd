package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fraser-isbester/fwd/internal/processor"
	"github.com/fraser-isbester/fwd/internal/source"
)

func main() {
	log.Println("Starting...")

	// Initialize processor
	proc, err := processor.NewPubSubProcessor(processor.PubSubConfig{
		// Empty ProjectID will pull the quota project from ADC
		TopicName: "opslog-events",
	})
	if err != nil {
		log.Fatalf("Failed to create processor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processor
	if err := proc.Start(ctx); err != nil {
		log.Fatalf("Failed to start processor: %v", err)
	}

	// Setup webhook source with GitHub handler
	webhookSrc := source.NewWebhookSource(8080)

	// Add GitHub webhook handler
	githubHandler := source.NewGitHubHandler("/webhook/github", "secret")
	webhookSrc.AddHandlerFunc(githubHandler.Path(), githubHandler.CreateHandler(proc.EventChannel()))

	// Start webhook server
	if err := webhookSrc.Start(ctx); err != nil {
		log.Fatalf("Failed to start webhook source: %v", err)
	}

	// Initialize Kubernetes source
	k8sSrc, err := source.NewKubeSource()
	if err != nil {
		log.Fatalf("Failed to create kubernetes source: %v", err)
	}

	// Start Kubernetes source
	if err := k8sSrc.Start(ctx, proc.EventChannel()); err != nil {
		log.Fatalf("Failed to start kubernetes source: %v", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")

	// Shutdown in reverse order
	if err := k8sSrc.Stop(); err != nil {
		log.Printf("Error stopping kubernetes source: %v", err)
	}

	if err := webhookSrc.Stop(); err != nil {
		log.Printf("Error stopping webhook source: %v", err)
	}

	if err := proc.Stop(); err != nil {
		log.Printf("Error stopping processor: %v", err)
	}
}
