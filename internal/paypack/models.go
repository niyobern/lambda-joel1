package paypack

import "time"

// AuthResponse captures the payload returned by the Paypack authorization endpoint.
type AuthResponse struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	Expires int    `json:"expires"`
}

// Transaction represents a payment transaction returned by Paypack.
type Transaction struct {
	Ref       string         `json:"ref"`
	Status    string         `json:"status,omitempty"`
	Amount    float64        `json:"amount"`
	Fee       float64        `json:"fee,omitempty"`
	Kind      string         `json:"kind"`
	Provider  string         `json:"provider"`
	Client    string         `json:"client,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Merchant  string         `json:"merchant,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
}

// TransactionNotFound models the error payload delivered when a transaction cannot be located.
type TransactionNotFound struct {
	Message string `json:"message"`
}
