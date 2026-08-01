package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// SessionResponse represents a validated session response from the auth service
type SessionResponse struct {
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	AuthProvider string    `json:"auth_provider"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func handleSessionValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "method_not_allowed", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, "unauthorized", "Missing authorization header", http.StatusUnauthorized)
		return
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, "invalid_token", "Invalid authorization header format", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Mock validation - accept known tokens or return error
	switch token {
	case "valid_session_token":
		// Return a valid session
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SessionResponse{
			UserID:       "user_12345",
			Email:        "test@example.com",
			DisplayName:  "Test User",
			AuthProvider: "google",
			CreatedAt:    time.Now().Add(-24 * time.Hour),
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		})
	case "expired_session_token":
		writeError(w, "session_expired", "Session has expired", http.StatusUnauthorized)
	case "invalid_session_token":
		writeError(w, "invalid_token", "Invalid session token", http.StatusUnauthorized)
	default:
		writeError(w, "invalid_token", "Invalid session token", http.StatusUnauthorized)
	}
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "method_not_allowed", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, "unauthorized", "Missing authorization header", http.StatusUnauthorized)
		return
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, "invalid_token", "Invalid authorization header format", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Mock user info response
	switch token {
	case "valid_session_token", "expired_session_token":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SessionResponse{
			UserID:       "user_12345",
			Email:        "test@example.com",
			DisplayName:  "Test User",
			AuthProvider: "google",
			CreatedAt:    time.Now().Add(-24 * time.Hour),
			ExpiresAt:    time.Now().Add(24 * time.Hour),
		})
	default:
		writeError(w, "invalid_token", "Invalid session token", http.StatusUnauthorized)
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, "method_not_allowed", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Always return success for logout
	w.WriteHeader(http.StatusNoContent)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func writeError(w http.ResponseWriter, errCode, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   errCode,
		Message: message,
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "1080"
	}

	// Register auth service endpoints
	http.HandleFunc("/v1/sessions/validate", handleSessionValidate)
	http.HandleFunc("/v1/userinfo", handleUserInfo)
	http.HandleFunc("/v1/logout", handleLogout)
	http.HandleFunc("/health", healthCheck)

	fmt.Printf("Mock Auth Service listening on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
