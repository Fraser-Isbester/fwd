
build:
	docker build -t localhost:5001/fwd:latest .

.PHONY: kind-up
kind-up: ## Bootstraps a kind cluster and pushes your ADC to it
	kind create cluster --name fwd || true
	kubectl create secret generic google-credentials \
	  --from-file=credentials.json=${HOME}/.config/gcloud/application_default_credentials.json || true


.PHONY: deploy-kind
deploy-kind: kind-up build ## Builds the image and deploys it to the kind cluster
	kind load docker-image localhost:5001/fwd:latest --name fwd
	kubectl apply -f deploy/
	kubectl rollout restart deployment fwd
