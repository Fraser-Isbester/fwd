package source

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

type TerraformCloudHandler struct {
	path   string
	secret string
}

func NewTerraformCloudHandler(path, secret string) *TerraformCloudHandler {
	return &TerraformCloudHandler{
		path:   path,
		secret: secret,
	}
}

func (h *TerraformCloudHandler) Path() string {
	return h.path
}

func (h *TerraformCloudHandler) validateSignature(body []byte, signature string) bool {
	if h.secret == "" || signature == "" {
		log.Printf("Debug mode: Skipping signature validation")
		return true
	}

	mac := hmac.New(sha512.New, []byte(h.secret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}

func (h *TerraformCloudHandler) CreateHandler(eventChan chan<- *cloudevents.Event) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Terraform Cloud uses a specific header for webhooks
		signature := r.Header.Get("X-TFE-Notification-Signature")
		if signature == "" && h.secret != "" {
			http.Error(w, "Missing signature header", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if !h.validateSignature(body, signature) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		// Parse the payload to extract workspace and organization info
		var payload struct {
			PayloadVersion int `json:"payload_version"`
			Notification   struct {
				WorkspaceName string `json:"workspace_name"`
				RunURL        string `json:"run_url"`
			} `json:"notification"`
			Organization struct {
				Name string `json:"name"`
			} `json:"organization"`
		}

		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// Create CloudEvent
		event := cloudevents.NewEvent()
		event.SetID(fmt.Sprintf("tfc-%d", time.Now().UnixNano()))
		event.SetSource(fmt.Sprintf("app.terraform.io/%s/%s",
			payload.Organization.Name,
			payload.Notification.WorkspaceName))
		event.SetType("com.hashicorp.terraform.run")
		event.SetTime(time.Now().UTC())
		event.SetSpecVersion(cloudevents.VersionV1)
		event.SetDataContentType("application/json")

		// Set the raw JSON payload as the data
		if err := event.SetData(cloudevents.ApplicationJSON, json.RawMessage(body)); err != nil {
			http.Error(w, "Failed to set event data", http.StatusInternalServerError)
			return
		}

		// Add Terraform-specific extensions
		event.SetExtension("tfworkspace", payload.Notification.WorkspaceName)
		event.SetExtension("tforganization", payload.Organization.Name)
		event.SetExtension("tfrunurl", payload.Notification.RunURL)

		// Try to send the event
		select {
		case eventChan <- &event:
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		}
	}
}
