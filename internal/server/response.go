package server

// SessionResponse represents the response returned for session validation requests.
// This matches the auth service's response format exactly.
type SessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"user_id,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}
