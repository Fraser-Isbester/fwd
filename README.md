# fwd

Simple event forwarding service that collects events from multiple sources and forwards them as CloudEvents to Google Cloud Pub/Sub.

## Local Development

```bash
# Start local cluster and deploy
make deploy-kind

# Update webhook secrets (required for GitHub/Terraform webhooks)
make update-secrets GITHUB_SECRET=xxx TERRAFORM_SECRET=yyy
```

## Architecture
Fan-in style event collection and forwarding. Sources accept events, map them to CloudEvents format and forward them to processors.

```
               ┌──────────┐
               │          │
     ─events─▶ │   k8s    │
               │          │
               └────┬─────┘     ┌──────────┐
                    │           │          │
                    ├─events──▶ │  pubsub  │
                    │           │          │
               ┌────┴─────┐     └──────────┘
 GitHub     -> │          │
 Terraform  -> │ webhooks │
               │          │
               └──────────┘
```

## Event Sources

- **Kubernetes**: Watches cluster events
- **GitHub**: Webhook endpoint for repository events (`/webhook/github`)
- **Terraform Cloud**: Webhook endpoint for workspace notifications (`/webhook/terraform`)

## Configuration

Environment variables:
- `GITHUB_WEBHOOK_SECRET`: Secret for GitHub webhook validation
- `TERRAFORM_WEBHOOK_SECRET`: Secret for Terraform Cloud webhook validation

See `deploy/` directory for Kubernetes deployment configuration.
