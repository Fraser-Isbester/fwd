package source

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

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type GitHubHandler struct {
	path   string
	secret string
}

func NewGitHubHandler(path, secret string) *GitHubHandler {
	return &GitHubHandler{
		path:   path,
		secret: secret,
	}
}

func (h *GitHubHandler) Path() string {
	return h.path
}

func (h *GitHubHandler) validateSignature(body []byte, signature string) bool {
	if h.secret == "" || signature == "" {
		log.Printf("Debug mode: Skipping signature validation")
		return true
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	signatureHash := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signatureHash), []byte(expectedMAC))
}

func (h *GitHubHandler) CreateHandler(eventChan chan<- *cloudevents.Event) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		eventType := r.Header.Get("X-GitHub-Event")
		if eventType == "" {
			http.Error(w, "Missing X-GitHub-Event header", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if !h.validateSignature(body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		// Parse the raw JSON to extract repository info
		var payload struct {
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// Create CloudEvent
		event := cloudevents.NewEvent()
		event.SetID(r.Header.Get("X-GitHub-Delivery"))
		// Source defined as the originating github repository ex. github.com/cloudevents/sdk-go
		event.SetSource(fmt.Sprintf("github.com/%s", payload.Repository.FullName))
		event.SetType(fmt.Sprintf("com.github.%s", eventType))
		event.SetTime(time.Now().UTC())
		event.SetSpecVersion(cloudevents.VersionV1)
		event.SetDataContentType(r.Header.Get("Content-Type"))

		// Set the raw JSON payload as the data
		if err := event.SetData(cloudevents.ApplicationJSON, json.RawMessage(body)); err != nil {
			http.Error(w, "Failed to set event data", http.StatusInternalServerError)
			return
		}

		// Add GitHub-specific extensions
		event.SetExtension("githubdeliveryid", r.Header.Get("X-GitHub-Delivery"))
		event.SetExtension("githubeventtype", eventType)
		event.SetExtension("githubrepository", payload.Repository.FullName)

		// Try to send the event
		select {
		case eventChan <- &event:
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		}
	}
}
