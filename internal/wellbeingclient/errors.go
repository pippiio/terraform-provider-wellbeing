package wellbeingclient

import (
	"encoding/json"
	"fmt"
	"strings"
)

// APIError is a non-2xx response from the Wellbeing API.
//
// ApiOperationID is worth preserving on every error: the API documentation asks
// that it be logged, and it is the only handle Wellbeing support can use to
// trace a failed import.
type APIError struct {
	StatusCode       int
	Message          string
	ApiOperationID   string
	ValidationErrors []string
	Body             string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "wellbeing api: status %d", e.StatusCode)

	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if len(e.ValidationErrors) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(e.ValidationErrors, "; "))
	}
	if e.Message == "" && len(e.ValidationErrors) == 0 && e.Body != "" {
		fmt.Fprintf(&b, ": %s", e.Body)
	}
	if e.ApiOperationID != "" {
		fmt.Fprintf(&b, " (ApiOperationId: %s)", e.ApiOperationID)
	}
	return b.String()
}

// parseAPIError decodes the several error shapes the API uses: a validation
// envelope, a {"Message": ...} object, a bare JSON string, or non-JSON.
func parseAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{
		StatusCode: statusCode,
		Body:       strings.TrimSpace(string(body)),
	}

	var envelope struct {
		ApiOperationID   string   `json:"ApiOperationId"`
		ValidationErrors []string `json:"ValidationErrors"`
		Message          string   `json:"Message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		apiErr.ApiOperationID = envelope.ApiOperationID
		apiErr.ValidationErrors = envelope.ValidationErrors
		apiErr.Message = envelope.Message
		return apiErr
	}

	var bare string
	if err := json.Unmarshal(body, &bare); err == nil {
		apiErr.Message = bare
		return apiErr
	}

	return apiErr
}
