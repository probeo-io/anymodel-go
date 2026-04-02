# Advanced Usage

This document covers features beyond the basics in the [README](../README.md).

## Model Naming

Models use `provider/model` format:

```
anthropic/claude-sonnet-4-6
openai/gpt-4o
google/gemini-2.5-pro
mistral/mistral-large-latest
groq/llama-3.3-70b-versatile
deepseek/deepseek-chat
xai/grok-3
perplexity/sonar-pro
ollama/llama3.3
```

### Flex Pricing (OpenAI)

Get 50% off OpenAI requests with flexible latency:

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:       "openai/gpt-4o",
	Messages:    []am.Message{{Role: "user", Content: "Hello!"}},
	ServiceTier: "flex",
})
```

## Tool Calling

Works across all providers with a unified interface:

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:    "anthropic/claude-sonnet-4-6",
	Messages: []am.Message{{Role: "user", Content: "What's the weather in NYC?"}},
	Tools: []am.Tool{
		{
			Type: "function",
			Function: am.FunctionDef{
				Name:        "get_weather",
				Description: "Get current weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{"type": "string"},
					},
					"required": []string{"location"},
				},
			},
		},
	},
	ToolChoice: "auto",
})
if err != nil {
	panic(err)
}

if response.Choices[0].Message.ToolCalls != nil {
	for _, call := range response.Choices[0].Message.ToolCalls {
		fmt.Println(call.Function.Name, call.Function.Arguments)
	}
}
```

## Structured Output

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:          "openai/gpt-4o",
	Messages:       []am.Message{{Role: "user", Content: "List 3 colors"}},
	ResponseFormat: &am.ResponseFormat{Type: "json_object"},
})
```

## Batch Processing (Full API)

### Submit now, check later

Submit a batch and get back an ID immediately. No need to keep the process running for native batches (OpenAI, Anthropic, Google):

```go
// Submit and get the batch ID
batch, err := client.Batches.Create(ctx, am.BatchCreateRequest{
	Model: "anthropic/claude-haiku-4-5",
	Requests: []am.BatchRequest{
		{CustomID: "req-1", Messages: []am.Message{{Role: "user", Content: "Summarize AI"}}},
		{CustomID: "req-2", Messages: []am.Message{{Role: "user", Content: "Summarize ML"}}},
	},
})
if err != nil {
	panic(err)
}
fmt.Println(batch.ID)        // "batch-abc123"
fmt.Println(batch.BatchMode) // "native" or "concurrent"

// Check status any time, even after a process restart
status, err := client.Batches.Get("batch-abc123")
fmt.Println(status.Status) // "pending", "processing", "completed", "failed"

// Wait for results when you're ready (reconnects to provider API)
results, err := client.Batches.Poll(ctx, "batch-abc123", am.BatchPollOptions{})

// Or get results directly if already completed
results, err = client.Batches.Results("batch-abc123")
```

### List and cancel

```go
// List all batches on disk
all, err := client.Batches.List()
for _, b := range all {
	fmt.Println(b.ID, b.BatchMode, b.Status, b.ProviderName)
}

// Cancel a running batch (also cancels at the provider for native batches)
err = client.Batches.Cancel(ctx, "batch-abc123")
```

### BatchBuilder API

An ergonomic interface for building batches. Pass strings, and anymodel handles IDs, system prompt injection, and provider-specific formatting:

```go
batch := client.Batches.Open(am.BatchBuilderConfig{
	Model:  "anthropic/claude-sonnet-4-6",
	System: "You are an expert.",
})

batch.Add("What is an LLC?")
batch.Add("How do I dissolve an LLC?")

err := batch.Submit(ctx)
if err != nil {
	panic(err)
}
results, err := batch.Poll(ctx, am.BatchPollOptions{})

fmt.Println(results.Succeeded) // successful responses with per-item costs
fmt.Println(results.Failed)    // failed items
fmt.Println(results.Usage)     // aggregate usage and estimated_cost

// Retry failed items
retryBatch := batch.Retry(results.Failed)
err = retryBatch.Submit(ctx)
retryResults, err := retryBatch.Poll(ctx, am.BatchPollOptions{})
```

### Batch mode

Force concurrent execution instead of native batch APIs (useful when you want flex pricing on individual requests):

```go
results, err := client.Batches.CreateAndPoll(ctx, am.BatchCreateRequest{
	Model:     "openai/gpt-4o",
	BatchMode: "concurrent", // skip native batch, run as individual requests
	Requests: []am.BatchRequest{
		{CustomID: "req-1", Messages: []am.Message{{Role: "user", Content: "Hello"}}},
	},
}, am.BatchPollOptions{})
```

### Service tier on batch requests

Use flex pricing on concurrent batches for 50% cost savings:

```go
results, err := client.Batches.CreateAndPoll(ctx, am.BatchCreateRequest{
	Model:       "openai/gpt-4o",
	BatchMode:   "concurrent",
	ServiceTier: "flex", // flex pricing on each concurrent request
	Requests: []am.BatchRequest{
		{CustomID: "req-1", Messages: []am.Message{{Role: "user", Content: "Hello"}}},
	},
}, am.BatchPollOptions{})
```

### Poll logging

Enable console logging during batch polling to monitor progress:

```go
// Per-call option
results, err := client.Batches.CreateAndPoll(ctx, request, am.BatchPollOptions{
	LogToConsole: true,
})

// Or enable globally via environment variable
// ANYMODEL_BATCH_POLL_LOG=1
```

### Adaptive Concurrency

For concurrent batches, anymodel can automatically discover your provider's rate limit ceiling instead of using a fixed concurrency:

```go
client := am.NewClient(am.AnyModelConfig{
	Batch: &am.BatchConfig{
		ConcurrencyFallback: "auto",
	},
})
```

This uses TCP-style slow-start (exponential ramp: 5 -> 10 -> 20 -> 40 -> ...) to quickly find your ceiling, then switches to AIMD (additive increase / multiplicative decrease) for fine-tuning. It reads `x-ratelimit-remaining-requests` headers proactively and backs off on 429s. An OpenAI Tier 4 account at 10,000 RPM will ramp to ~160 concurrent in about 155 requests instead of being stuck at 5.

Use `ConcurrencyMax` to set a hard ceiling. Useful when multiple batch jobs share the same API key:

```go
client := am.NewClient(am.AnyModelConfig{
	Batch: &am.BatchConfig{
		ConcurrencyFallback: "auto",
		ConcurrencyMax:      50, // each job caps at 50, two jobs = 100 total
	},
})
```

### Batch configuration

```go
client := am.NewClient(am.AnyModelConfig{
	Batch: &am.BatchConfig{
		PollInterval:        10000, // default poll interval in ms (default: 5000)
		ConcurrencyFallback: 10,   // concurrent request limit for non-native providers (default: 5)
		// ConcurrencyFallback: "auto", // or auto-discover from provider rate limits
		// ConcurrencyMax:      50,     // hard ceiling for auto mode
	},
	IO: &am.IOConfig{
		ReadConcurrency:  30, // concurrent file reads (default: 20)
		WriteConcurrency: 15, // concurrent file writes (default: 10)
	},
})

// Override poll interval per call
results, err := client.Batches.CreateAndPoll(ctx, request, am.BatchPollOptions{
	Interval: 3000, // poll every 3s for this batch
	OnProgress: func(batch am.BatchStatus) {
		fmt.Printf("%d/%d done\n", batch.Completed, batch.Total)
	},
})
```

Batches are persisted to `./.anymodel/batches/` in the current working directory and survive process restarts.

### Automatic max_tokens

When `max_tokens` isn't set on a batch request, anymodel automatically calculates a safe value per-request based on the estimated input size and the model's context window. This prevents truncated responses and context overflow errors without requiring you to hand-tune each request in a large batch. The estimation uses a ~4 chars/token heuristic with a 5% safety margin.

## Models Endpoint

```go
models, err := client.Models.List(ctx)
anthropicModels, err := client.Models.List(ctx, am.ModelsListOptions{Provider: "anthropic"})
```

## Generation Stats

```go
response, err := client.Chat.Completions.Create(ctx, req)
stats := client.Generation.Get(response.ID)
fmt.Println(stats.Latency, stats.TokensPrompt, stats.TokensCompletion)
fmt.Println(stats.TotalCost) // auto-calculated from bundled pricing data
```

### Auto Pricing / Cost Calculation

Pricing for 323 models is baked in at build time from OpenRouter. Costs are calculated automatically from token usage with no configuration needed.

```go
// Per-request cost on GenerationStats
stats := client.Generation.Get(response.ID)
fmt.Println(stats.TotalCost) // e.g. 0.0023

// Batch-level cost on BatchUsageSummary
results, err := client.Batches.CreateAndPoll(ctx, request, am.BatchPollOptions{})
fmt.Println(results.Usage.EstimatedCost) // total across all requests

// Native batch pricing is automatically 50% off
// Utility functions also exported
pricing := am.GetModelPricing("openai/gpt-4o")
cost := am.CalculateCost("openai/gpt-4o", promptTokens, completionTokens)
fmt.Println(am.PricingAsOf, am.PricingModelCount)
```

## Custom Providers

Add any OpenAI-compatible endpoint:

```go
client := am.NewClient(am.AnyModelConfig{
	Custom: map[string]am.CustomProviderConfig{
		"ollama": {
			BaseURL: "http://localhost:11434/v1",
			Models:  []string{"llama3.3", "mistral"},
		},
		"together": {
			BaseURL: "https://api.together.xyz/v1",
			APIKey:  "your-key",
		},
	},
})

response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:    "ollama/llama3.3",
	Messages: []am.Message{{Role: "user", Content: "Hello from Ollama"}},
})
```

## Provider Preferences

Control which providers are used and in what order:

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Models: []string{"anthropic/claude-sonnet-4-6", "openai/gpt-4o", "google/gemini-2.5-pro"},
	Route:  "fallback",
	Provider: &am.ProviderPreferences{
		Order:  []string{"anthropic", "openai"},
		Ignore: []string{"google"},
	},
	Messages: []am.Message{{Role: "user", Content: "Hello"}},
})
```

## Transforms

Automatically truncate long conversations to fit within context windows:

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:      "anthropic/claude-sonnet-4-6",
	Messages:   veryLongConversation,
	Transforms: []string{"middle-out"},
})
```

`middle-out` preserves the system prompt and most recent messages, removing from the middle.

## Config File

Create `anymodel.config.json` in your project root:

```json
{
  "anthropic": {
    "apiKey": "${ANTHROPIC_API_KEY}"
  },
  "aliases": {
    "default": "anthropic/claude-sonnet-4-6",
    "fast": "anthropic/claude-haiku-4-5"
  },
  "defaults": {
    "temperature": 0.7,
    "max_tokens": 4096
  },
  "batch": {
    "pollInterval": 5000,
    "concurrencyFallback": 5
  },
  "io": {
    "readConcurrency": 20,
    "writeConcurrency": 10
  }
}
```

`${ENV_VAR}` references are interpolated from environment variables.

### Config Resolution Order

1. Programmatic options (highest priority)
2. Local `anymodel.config.json`
3. Global `~/.anymodel/config.json`
4. Environment variables (lowest priority)

Configs are deep-merged, not replaced.

## Server Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/chat/completions` | Chat completion (streaming supported) |
| GET | `/api/v1/models` | List available models |
| GET | `/api/v1/generation/:id` | Get generation stats |
| POST | `/api/v1/batches` | Create a batch |
| GET | `/api/v1/batches` | List batches |
| GET | `/api/v1/batches/:id` | Get batch status |
| GET | `/api/v1/batches/:id/results` | Get batch results |
| POST | `/api/v1/batches/:id/cancel` | Cancel a batch |
| GET | `/health` | Health check |

## Server with OpenAI SDK

```go
import (
	"context"

	openai "github.com/sashabaranov/go-openai"
)

config := openai.DefaultConfig("unused")
config.BaseURL = "http://localhost:4141/api/v1"
client := openai.NewClientWithConfig(config)

response, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
	Model: "anthropic/claude-sonnet-4-6",
	Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: "Hello via server"},
	},
})
```

## Examples

See [`examples/basic.go`](../examples/basic.go) for runnable demos of completions, streaming, tool calling, fallback routing, batch processing, and generation stats.

```bash
# Run all examples
go run examples/basic.go

# Run a specific example
go run examples/basic.go stream
go run examples/basic.go tools
go run examples/basic.go batch
```

## Roadmap

- [ ] **A/B testing**: split routing (% traffic to each model) and compare mode (same request to multiple models, return all responses with stats)
- [x] **Cost tracking**: per-request and aggregate cost calculation from bundled pricing data (323 models from OpenRouter)
- [ ] **Caching**: response caching with configurable TTL for identical requests
- [x] **Native batch APIs**: OpenAI Batch API (JSONL upload, 50% cost), Anthropic Message Batches (10K requests, async), and Google Gemini Batch (50% cost). Auto-detects provider and routes to native API, falls back to concurrent for other providers
- [x] **Adaptive concurrency**: auto-discover provider rate limit ceilings via TCP slow-start + AIMD, with hard cap support for multi-job workloads
- [ ] **Result export**: `saveResults()` to write batch results to a configurable output directory
- [ ] **Prompt logging**: optional request/response logging for debugging and evaluation
