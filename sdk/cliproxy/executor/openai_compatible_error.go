package executor

import (
	"encoding/json"
	"net/http"
)

// OpenAICompatibleError preserves a specific OpenAI-style error contract
// while still exposing an HTTP-like status code to the caller.
type OpenAICompatibleError struct {
	HTTPStatus int
	Type       string
	Code       string
	Message    string
	HeadersMap http.Header
}

// Error returns a JSON payload so HTTP/websocket handlers can forward the
// intended OpenAI-compatible error structure without rewriting it.
func (e OpenAICompatibleError) Error() string {
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"type":    e.Type,
			"code":    e.Code,
			"message": e.Message,
		},
	})
	if err != nil {
		return e.Message
	}
	return string(body)
}

// StatusCode implements StatusError.
func (e OpenAICompatibleError) StatusCode() int {
	return e.HTTPStatus
}

// Headers exposes optional headers associated with the error.
func (e OpenAICompatibleError) Headers() http.Header {
	if e.HeadersMap == nil {
		return nil
	}
	return e.HeadersMap.Clone()
}

// NewOpenAICompatibleError builds an error with an explicit OpenAI-compatible
// body that should be preserved by downstream handlers.
func NewOpenAICompatibleError(status int, errType, code, message string) OpenAICompatibleError {
	return OpenAICompatibleError{
		HTTPStatus: status,
		Type:       errType,
		Code:       code,
		Message:    message,
	}
}
