package anymodel

import "fmt"

// Error is an error from anymodel with an HTTP-like status code and provider metadata.
type Error struct {
	Code     int            `json:"code"`
	Msg      string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("anymodel error %d: %s", e.Code, e.Msg)
}

// NewError creates a new Error.
func NewError(code int, message string, metadata map[string]any) *Error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return &Error{Code: code, Msg: message, Metadata: metadata}
}

// ToMap returns the error as a JSON-serializable map.
func (e *Error) ToMap() map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":     e.Code,
			"message":  e.Msg,
			"metadata": e.Metadata,
		},
	}
}
