package anymodel

import "fmt"

// ValidateRequest validates a chat completion request.
func ValidateRequest(req *ChatCompletionRequest) error {
	if req.Model == "" && len(req.Models) == 0 {
		return NewError(400, "model or models[] is required", nil)
	}
	if len(req.Messages) == 0 {
		return NewError(400, "messages must be a non-empty array", nil)
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return NewError(400, fmt.Sprintf("temperature must be between 0 and 2, got %f", *req.Temperature), nil)
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return NewError(400, fmt.Sprintf("top_p must be between 0 and 1, got %f", *req.TopP), nil)
	}
	if req.TopLogprobs != nil {
		if *req.TopLogprobs < 0 || *req.TopLogprobs > 20 {
			return NewError(400, fmt.Sprintf("top_logprobs must be between 0 and 20, got %d", *req.TopLogprobs), nil)
		}
		if req.Logprobs == nil || !*req.Logprobs {
			return NewError(400, "top_logprobs requires logprobs to be true", nil)
		}
	}
	if len(req.Stop) > 4 {
		return NewError(400, fmt.Sprintf("stop may contain at most 4 sequences, got %d", len(req.Stop)), nil)
	}
	if len(req.Models) > 0 && req.Route != "fallback" {
		return NewError(400, "route must be 'fallback' when models[] is provided", nil)
	}
	return nil
}
