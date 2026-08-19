package edrbypass

import "time"

// Evidence represents captured proof of EDR bypass behavior
type Evidence struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Success     bool                   `json:"success"`
	Timestamp   time.Time              `json:"timestamp,omitempty"`
}
