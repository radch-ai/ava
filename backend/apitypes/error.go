package apitypes

// ErrorResponse is every non-2xx body: {message}, which the ky
// client surfaces verbatim in thrown errors.
type ErrorResponse struct {
	Message string `json:"message"`
}
