package types

import (
	"encoding/json"
	"time"
)

// Event represents our standardized event format
type Event struct {
	// CloudEvents required attributes
	ID          string          `json:"id"`
	Source      string          `json:"source"`
	SpecVersion string          `json:"specversion"`
	Type        string          `json:"type"`
	Time        time.Time       `json:"time"`
	Data        json.RawMessage `json:"data"`
	Subject     string          `json:"subject,omitempty"`
	DataSchema  string          `json:"dataschema,omitempty"`
	Extensions  map[string]any  `json:"extensions,omitempty"`
}
