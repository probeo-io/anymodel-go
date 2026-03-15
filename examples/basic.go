package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	am "github.com/probeo-io/anymodel-go"
)

func main() {
	args := os.Args[1:]
	demos := map[string]func(context.Context){
		"completion": demoCompletion,
		"stream":     demoStream,
		"tools":      demoTools,
		"fallback":   demoFallback,
		"models":     demoModels,
		"stats":      demoStats,
	}

	if len(args) == 0 {
		for _, fn := range demos {
			fn(context.Background())
		}
		return
	}

	for _, name := range args {
		fn, ok := demos[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "Unknown demo: %s\nAvailable: %s\n", name, strings.Join(keys(demos), ", "))
			os.Exit(1)
		}
		fn(context.Background())
	}
}

func demoCompletion(ctx context.Context) {
	fmt.Println("\n=== Chat Completion ===")
	client := am.New(nil)

	temp := 0.7
	result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
		Model:       "anthropic/claude-sonnet-4-6",
		Messages:    []am.Message{{Role: am.RoleUser, Content: "What is the capital of France? Reply in one sentence."}},
		Temperature: &temp,
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println(result.Choices[0].Message.Content)
	fmt.Printf("\nTokens: %+v\n", result.Usage)
}

func demoStream(ctx context.Context) {
	fmt.Println("\n=== Streaming ===")
	client := am.New(nil)

	chunkCh, errCh, err := client.Chat.Completions.CreateStream(ctx, am.ChatCompletionRequest{
		Model:    "openai/gpt-4o",
		Messages: []am.Message{{Role: am.RoleUser, Content: "Write a haiku about programming."}},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				fmt.Println()
				return
			}
			if len(chunk.Choices) > 0 {
				fmt.Print(chunk.Choices[0].Delta.Content)
			}
		case err, ok := <-errCh:
			if ok && err != nil {
				fmt.Printf("\nError: %v\n", err)
			}
			return
		}
	}
}

func demoTools(ctx context.Context) {
	fmt.Println("\n=== Tool Calling ===")
	client := am.New(nil)

	result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []am.Message{{Role: am.RoleUser, Content: "What's the weather in New York City?"}},
		Tools: []am.Tool{{
			Type: "function",
			Function: am.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get current weather for a location",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string", "description": "City name"},
					},
					"required": []string{"location"},
				},
			},
		}},
		ToolChoice: "auto",
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	msg := result.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			fmt.Printf("Tool: %s\n", tc.Function.Name)
			fmt.Printf("Args: %s\n", tc.Function.Arguments)
		}
	} else {
		fmt.Println(msg.Content)
	}
}

func demoFallback(ctx context.Context) {
	fmt.Println("\n=== Fallback Routing ===")
	client := am.New(nil)

	result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
		Models: []string{
			"anthropic/claude-sonnet-4-6",
			"openai/gpt-4o",
			"google/gemini-2.5-pro",
		},
		Route:    "fallback",
		Messages: []am.Message{{Role: am.RoleUser, Content: "Say hello and tell me which model you are."}},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println(result.Choices[0].Message.Content)
	fmt.Printf("\nModel used: %s\n", result.Model)
}

func demoModels(ctx context.Context) {
	fmt.Println("\n=== Available Models ===")
	client := am.New(nil)

	models, err := client.Models.List(ctx, "")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	limit := 10
	if len(models) < limit {
		limit = len(models)
	}
	for _, m := range models[:limit] {
		fmt.Printf("  %s\n", m.ID)
	}
	if len(models) > 10 {
		fmt.Printf("  ... and %d more\n", len(models)-10)
	}
}

func demoStats(ctx context.Context) {
	fmt.Println("\n=== Generation Stats ===")
	client := am.New(nil)

	result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
		Model:    "anthropic/claude-haiku-4-5",
		Messages: []am.Message{{Role: am.RoleUser, Content: "Hi"}},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	stats := client.Generation.Get(result.ID)
	if stats != nil {
		fmt.Printf("  ID: %s\n", stats.ID)
		fmt.Printf("  Model: %s\n", stats.Model)
		fmt.Printf("  Latency: %.2fs\n", stats.Latency)
		fmt.Printf("  Prompt tokens: %d\n", stats.TokensPrompt)
		fmt.Printf("  Completion tokens: %d\n", stats.TokensCompletion)
	} else {
		fmt.Println("  No stats recorded")
	}
}

func keys(m map[string]func(context.Context)) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	return k
}
