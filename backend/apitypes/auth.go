package apitypes

// ExchangeRequest is the body of POST /api/auth/exchange: the
// long-lived UI access token traded for an HttpOnly session cookie.
type ExchangeRequest struct {
	Token string `json:"token"`
}
