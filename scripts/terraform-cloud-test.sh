!#/bin/bash

# Generate HMAC signature (if using webhook secret)
PAYLOAD='{"payload_version":1,"notification":{"workspace_name":"prod","run_url":"https://app.terraform.io/app/workspace/runs/1"},"organization":{"name":"acme"}}'
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha512 -hmac "your-webhook-secret" | cut -d' ' -f2)

echo "Payload: $PAYLOAD"
echo "Signature: $SIGNATURE"

# Send request
curl -X POST http://localhost:8080/webhook/terraform \
  -H "Content-Type: application/json" \
  -H "X-TFE-Notification-Signature: $SIGNATURE" \
  -d "$PAYLOAD"
