package source

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type KubeSource struct {
	client    *kubernetes.Clientset
	informer  cache.SharedInformer
	eventChan chan<- *cloudevents.Event
}

func NewKubeSource() (*KubeSource, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfigPath := os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			kubeconfigPath = os.ExpandEnv("$HOME/.kube/config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &KubeSource{
		client: clientset,
	}, nil
}

func (k *KubeSource) Start(ctx context.Context, eventChan chan<- *cloudevents.Event) error {
	k.eventChan = eventChan

	factory := informers.NewSharedInformerFactory(k.client, 10*time.Minute)
	k.informer = factory.Core().V1().Events().Informer()

	k.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if event, ok := obj.(*corev1.Event); ok {
				// Only process events that are recent
				if time.Since(event.LastTimestamp.Time) < time.Hour {
					k.handleEvent(event)
				}
			}
		},
		UpdateFunc: func(old, new interface{}) {
			if event, ok := new.(*corev1.Event); ok {
				if time.Since(event.LastTimestamp.Time) < time.Hour {
					k.handleEvent(event)
				}
			}
		},
	})

	go k.informer.Run(ctx.Done())

	// Wait for cache sync
	if !cache.WaitForCacheSync(ctx.Done(), k.informer.HasSynced) {
		return fmt.Errorf("failed to sync cache")
	}

	log.Printf("Started Kubernetes event watcher")
	return nil
}

func (k *KubeSource) handleEvent(event *corev1.Event) {
	// Create CloudEvent
	ce := cloudevents.NewEvent()

	// Required attributes
	ce.SetID(string(event.UID))
	ce.SetType(fmt.Sprintf("com.kubernetes.%s.%s",
		strings.ToLower(event.InvolvedObject.Kind),
		strings.ToLower(event.Reason)))
	// TODO: extract canonical cluster name
	ce.SetSource(fmt.Sprintf("kubernetes://unknown/%s/%s/%s",
		event.InvolvedObject.Namespace,
		event.InvolvedObject.Kind,
		event.InvolvedObject.Name))

	ce.SetTime(event.LastTimestamp.Time)

	// Add key k8s fields as extensions for filtering/routing
	ce.SetExtension("namespace", event.InvolvedObject.Namespace)
	ce.SetExtension("kind", event.InvolvedObject.Kind)
	ce.SetExtension("name", event.InvolvedObject.Name)
	ce.SetExtension("reason", event.Reason)
	ce.SetExtension("type", event.Type) // Normal or Warning
	ce.SetExtension("component", event.Source.Component)

	// Main event data
	data := map[string]interface{}{
		"message":        event.Message,
		"count":          event.Count,
		"firstTimestamp": event.FirstTimestamp.Time,
		"lastTimestamp":  event.LastTimestamp.Time,
		"involvedObject": map[string]string{
			"kind":      event.InvolvedObject.Kind,
			"name":      event.InvolvedObject.Name,
			"namespace": event.InvolvedObject.Namespace,
		},
	}

	if err := ce.SetData(cloudevents.ApplicationJSON, data); err != nil {
		log.Printf("Failed to set event data: %v", err)
		return
	}

	select {
	case k.eventChan <- &ce:
		log.Printf("Published event: [%s] %s/%s: %s",
			event.Type,
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Message)
	default:
		log.Printf("Event channel full, dropping event: %s", ce.ID())
	}
}

func (k *KubeSource) Stop() error {
	return nil // informer shutdown is handled by context cancellation
}
