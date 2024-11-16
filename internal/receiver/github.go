package receiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fraser-isbester/fwd/pkg/types"
)

// GitHubWebhookHandler handles GitHub-specific webhooks
type GitHubWebhookHandler struct {
	secret string
	debug  bool
}

func NewGitHubWebhookHandler(secret string) *GitHubWebhookHandler {
	log.Printf("Initializing GitHub webhook handler with secret configured: %v", secret != "")
	return &GitHubWebhookHandler{
		secret: secret,
		debug:  true, // Enable debug mode for testing
	}
}

func (h *GitHubWebhookHandler) Path() string {
	return "/webhook/github"
}

func (h *GitHubWebhookHandler) validateSignature(signature string, body []byte) bool {
	// In debug mode, skip signature validation if no signature is provided
	if h.debug && signature == "" {
		log.Printf("Debug mode: Skipping signature validation")
		return true
	}

	if h.secret == "" {
		log.Printf("No webhook secret configured, skipping signature validation")
		return true
	}

	log.Printf("Validating signature: %s", signature)
	if !strings.HasPrefix(signature, "sha256=") {
		log.Printf("Invalid signature format")
		return false
	}

	signatureHash := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	valid := hmac.Equal([]byte(signatureHash), []byte(expectedMAC))
	log.Printf("Signature validation result: %v", valid)
	return valid
}

func (h *GitHubWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request, eventChan chan<- *types.Event) {
	log.Printf("Received GitHub webhook request: %s %s", r.Method, r.URL.Path)

	// Log all headers for debugging
	log.Printf("Request headers:")
	for name, values := range r.Header {
		log.Printf("  %s: %v", name, values)
	}

	if r.Method != http.MethodPost {
		log.Printf("Invalid method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		log.Printf("Missing X-GitHub-Event header")
		http.Error(w, "Missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}
	log.Printf("GitHub event type: %s", eventType)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("Received payload: %s", string(body))

	// Only validate signature if not in debug mode or if signature is provided
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.debug || signature != "" {
		if !h.validateSignature(signature, body) {
			log.Printf("Invalid signature")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		deliveryID = fmt.Sprintf("gh_%d", time.Now().UnixNano())
		log.Printf("Generated delivery ID: %s", deliveryID)
	} else {
		log.Printf("GitHub delivery ID: %s", deliveryID)
	}

	var payload json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("Invalid JSON payload: %v", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	var repoName string
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err == nil {
		if repo, ok := payloadMap["repository"].(map[string]interface{}); ok {
			if name, ok := repo["full_name"].(string); ok {
				repoName = name
				log.Printf("Extracted repository name: %s", repoName)
			}
		}
	}

	event := &types.Event{
		ID:          deliveryID,
		Source:      fmt.Sprintf("github.com/%s", repoName),
		SpecVersion: "1.0",
		Type:        fmt.Sprintf("github.%s", eventType),
		Time:        time.Now().UTC(),
		Data:        payload,
		Extensions: map[string]any{
			"delivery_id": deliveryID,
			"event_type":  eventType,
			"repository":  repoName,
			"headers": map[string]string{
				"User-Agent":   r.Header.Get("User-Agent"),
				"Content-Type": r.Header.Get("Content-Type"),
			},
		},
	}

	// Log the event we're about to send
	eventJSON, _ := json.MarshalIndent(event, "", "  ")
	log.Printf("Sending event to channel: %s", string(eventJSON))

	select {
	case eventChan <- event:
		log.Printf("Successfully sent event to channel")
		w.WriteHeader(http.StatusAccepted)
	default:
		log.Printf("Channel full, failed to send event")
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
	}
}
