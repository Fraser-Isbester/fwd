package source

import (
	"context"
	"encoding/json"
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

	fmt.Println(event)

	// Set attributes
	ce.SetID(string(event.UID))
	ce.SetType(fmt.Sprintf("com.kubernetes.%s.%s",
		strings.ToLower(event.InvolvedObject.Kind),
		strings.ToLower(event.Reason)))
	ce.SetTime(event.LastTimestamp.Time)
	ce.SetSpecVersion(cloudevents.VersionV1)
	ce.SetDataContentType("application/json")
	ce.SetSubject(fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name))

	// Source defined as: kubernetes://{ cluster name }/{ namespace }
	// TODO: extract canonical cluster name
	namespace := event.InvolvedObject.Namespace
	if namespace == "" {
		ce.SetSource("kubernetes://unknown")
	} else {
		ce.SetSource(fmt.Sprintf("kubernetes://unknown/%s", namespace))
	}

	// Add key k8s fields as extensions for filtering/routing
	ce.SetExtension("namespace", event.InvolvedObject.Namespace)
	ce.SetExtension("kind", event.InvolvedObject.Kind)
	ce.SetExtension("name", event.InvolvedObject.Name)
	ce.SetExtension("reason", event.Reason)
	ce.SetExtension("type", event.Type) // Normal or Warning
	ce.SetExtension("component", event.Source.Component)

	// Set the raw JSON payload as the data
	eventJSON, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event to JSON: %v", err)
		return
	}
	if err := ce.SetData(cloudevents.ApplicationJSON, eventJSON); err != nil {
		log.Printf("Failed to set event data: %v", err)
		return
	}

	select {
	case k.eventChan <- &ce:
	default:
		log.Printf("Event channel full, dropping event: %s", ce.ID())
	}
}

func (k *KubeSource) Stop() error {
	return nil // informer shutdown is handled by context cancellation
}
