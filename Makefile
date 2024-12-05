build:
	docker build -t localhost:5001/fwd:latest .

.PHONY: kind-up
kind-up: ## Bootstraps a kind cluster and pushes your ADC to it
	kind create cluster --name fwd || true
	kubectl create secret generic google-credentials \
		--from-file=credentials.json=${HOME}/.config/gcloud/application_default_credentials.json || true
	# Create initial secrets with placeholder values if they don't exist
	kubectl create secret generic fwd-secrets \
		--from-literal=github-webhook-secret=placeholder \
		--from-literal=terraform-webhook-secret=placeholder || true

.PHONY: update-secrets
update-secrets: ## Updates webhook secrets. Usage: make update-secrets GITHUB_SECRET=xxx TERRAFORM_SECRET=yyy
	@if [ -z "$(GITHUB_SECRET)" ] || [ -z "$(TERRAFORM_SECRET)" ]; then \
		echo "Error: Both GITHUB_SECRET and TERRAFORM_SECRET must be provided"; \
		echo "Usage: make update-secrets GITHUB_SECRET=xxx TERRAFORM_SECRET=yyy"; \
		exit 1; \
	fi
	kubectl create secret generic fwd-secrets \
		--from-literal=github-webhook-secret=$(GITHUB_SECRET) \
		--from-literal=terraform-webhook-secret=$(TERRAFORM_SECRET) \
		--dry-run=client -o yaml | kubectl apply -f -

.PHONY: deploy-kind
deploy-kind: kind-up build ## Builds the image and deploys it to the kind cluster
	kind load docker-image localhost:5001/fwd:latest --name fwd
	kubectl apply -f deploy/
	kubectl rollout restart deployment fwd
