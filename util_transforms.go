package anymodel

// MiddleOut trims messages from the middle to fit within a token budget.
func MiddleOut(messages []Message, maxTokens int) []Message {
	if len(messages) <= 2 {
		return messages
	}

	totalTokens := 0
	for _, m := range messages {
		totalTokens += estimateTokens(m.Content)
	}

	if totalTokens <= maxTokens {
		return messages
	}

	var system []Message
	var conversation []Message
	for _, m := range messages {
		if m.Role == RoleSystem {
			system = append(system, m)
		} else {
			conversation = append(conversation, m)
		}
	}

	if len(conversation) <= 1 {
		return messages
	}

	systemTokens := 0
	for _, m := range system {
		systemTokens += estimateTokens(m.Content)
	}
	remaining := maxTokens - systemTokens
	recentBudget := int(float64(remaining) * 0.7)

	var recent []Message
	recentTokens := 0
	for i := len(conversation) - 1; i >= 0; i-- {
		t := estimateTokens(conversation[i].Content)
		if recentTokens+t > recentBudget && len(recent) > 0 {
			break
		}
		recent = append([]Message{conversation[i]}, recent...)
		recentTokens += t
	}

	result := make([]Message, 0, len(system)+len(recent))
	result = append(result, system...)
	result = append(result, recent...)
	return result
}

// ApplyTransforms applies named transforms to messages.
func ApplyTransforms(transforms []string, messages []Message, contextLength int) []Message {
	for _, t := range transforms {
		if t == "middle-out" {
			messages = MiddleOut(messages, contextLength)
		}
	}
	return messages
}

func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}
