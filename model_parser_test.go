package anymodel

import "testing"

func TestParseModelString(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		aliases   map[string]string
		wantProv  string
		wantModel string
		wantErr   bool
	}{
		{"basic", "openai/gpt-4o", nil, "openai", "gpt-4o", false},
		{"anthropic", "anthropic/claude-sonnet-4-6", nil, "anthropic", "claude-sonnet-4-6", false},
		{"with alias", "fast", map[string]string{"fast": "anthropic/claude-haiku-4-5"}, "anthropic", "claude-haiku-4-5", false},
		{"no slash", "gpt-4o", nil, "", "", true},
		{"empty provider", "/gpt-4o", nil, "", "", true},
		{"empty model", "openai/", nil, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseModelString(tt.model, tt.aliases)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.Provider != tt.wantProv {
				t.Errorf("provider = %q, want %q", parsed.Provider, tt.wantProv)
			}
			if parsed.Model != tt.wantModel {
				t.Errorf("model = %q, want %q", parsed.Model, tt.wantModel)
			}
		})
	}
}
