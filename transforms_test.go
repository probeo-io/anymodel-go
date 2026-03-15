package anymodel

import (
	"strings"
	"testing"
)

func TestMiddleOut(t *testing.T) {
	t.Run("short messages unchanged", func(t *testing.T) {
		msgs := []Message{{Role: RoleUser, Content: "hello"}, {Role: RoleAssistant, Content: "hi"}}
		result := MiddleOut(msgs, 1000)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
	})

	t.Run("long messages trimmed", func(t *testing.T) {
		msgs := []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleUser, Content: strings.Repeat("old ", 100)},
			{Role: RoleAssistant, Content: strings.Repeat("old reply ", 100)},
			{Role: RoleUser, Content: strings.Repeat("middle ", 100)},
			{Role: RoleAssistant, Content: strings.Repeat("mid reply ", 100)},
			{Role: RoleUser, Content: "recent question"},
			{Role: RoleAssistant, Content: "recent answer"},
		}
		result := MiddleOut(msgs, 50)
		if len(result) >= len(msgs) {
			t.Errorf("expected fewer messages, got %d (original %d)", len(result), len(msgs))
		}
		if result[0].Role != RoleSystem {
			t.Error("expected system message first")
		}
	})
}
