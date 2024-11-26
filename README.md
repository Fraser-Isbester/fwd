# fwd

Simple event forwarding service that collects Kubernetes events and GitHub webhooks and forwards them as CloudEvents to Google Cloud Pub/Sub.

## Local Development

```bash
make deploy-kind
```

## Architecture
Fan-in style event collection and forwarding. Sources naively accept events, map them to cloudevents and forward them to processors to manage.

```
           ┌──────────┐
           │          │
  ─events─▶│   k8s    │
           │          │
           └────┬─────┘      ┌─────────┐
                │           │          │
                ├─events──▶ │  pubsub  │
                │           │          │
           ┌────┴─────┐     └─────────┘
           │          │
─webhooks─▶│  github  │
           │          │
           └──────────┘
```