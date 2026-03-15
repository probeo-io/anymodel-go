package anymodel

import "strings"

// ParsedModel holds the parsed provider and model name.
type ParsedModel struct {
	Provider string
	Model    string
}

// ParseModelString parses a "provider/model" string, resolving aliases first.
func ParseModelString(model string, aliases map[string]string) (*ParsedModel, error) {
	if aliases != nil {
		if resolved, ok := aliases[model]; ok {
			model = resolved
		}
	}

	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, NewError(400, "model must be in format 'provider/model-name', got: "+model, nil)
	}

	return &ParsedModel{Provider: parts[0], Model: parts[1]}, nil
}
