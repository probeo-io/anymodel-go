# anymodel-go

[![Go Reference](https://pkg.go.dev/badge/github.com/probeo-io/anymodel-go.svg)](https://pkg.go.dev/github.com/probeo-io/anymodel-go)
[![License](https://img.shields.io/github/license/probeo-io/anymodel-go)](LICENSE)
[![CI](https://github.com/probeo-io/anymodel-go/actions/workflows/ci.yml/badge.svg)](https://github.com/probeo-io/anymodel-go/actions/workflows/ci.yml)

OpenRouter-compatible LLM router with unified batch support. Self-hosted, zero fees.

Route requests across OpenAI, Anthropic, and Google with a single API. Add any OpenAI-compatible provider. Run as an SDK or standalone HTTP server.

**[pkg.go.dev documentation](https://pkg.go.dev/github.com/probeo-io/anymodel-go)**

## Why anymodel?

One SDK, 11+ providers. No vendor lock-in. Self-hosted with your own keys and infrastructure, so there are zero routing fees (unlike OpenRouter). Native batch APIs for OpenAI, Anthropic, and Google run at 50% cost with zero config. Streaming uses Go channels. Drop-in compatible with the OpenAI SDK via server mode.

## Install

```bash
go get github.com/probeo-io/anymodel-go
```

## Quick Start

Set your API keys as environment variables:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
export GOOGLE_API_KEY=AIza...
```

### SDK Usage

```go
package main

import (
	"context"
	"fmt"

	am "github.com/probeo-io/anymodel-go"
)

func main() {
	client := am.NewClient(am.AnyModelConfig{})
	ctx := context.Background()

	response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []am.Message{{Role: "user", Content: "Hello!"}},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(response.Choices[0].Message.Content)
}
```

### Streaming

```go
stream, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Model:    "openai/gpt-4o",
	Messages: []am.Message{{Role: "user", Content: "Write a haiku"}},
	Stream:   true,
})
if err != nil {
	panic(err)
}

for chunk := range stream {
	fmt.Print(chunk.Choices[0].Delta.Content)
}
```

## Supported Providers

Set the env var and go. Models are auto-discovered from each provider's API.

| Provider | Env Var | Example Model |
|----------|---------|---------------|
| OpenAI | `OPENAI_API_KEY` | `openai/gpt-4o` |
| Anthropic | `ANTHROPIC_API_KEY` | `anthropic/claude-sonnet-4-6` |
| Google | `GOOGLE_API_KEY` | `google/gemini-2.5-pro` |
| Mistral | `MISTRAL_API_KEY` | `mistral/mistral-large-latest` |
| Groq | `GROQ_API_KEY` | `groq/llama-3.3-70b-versatile` |
| DeepSeek | `DEEPSEEK_API_KEY` | `deepseek/deepseek-chat` |
| xAI | `XAI_API_KEY` | `xai/grok-3` |
| Together | `TOGETHER_API_KEY` | `together/meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| Fireworks | `FIREWORKS_API_KEY` | `fireworks/accounts/fireworks/models/llama-v3p3-70b-instruct` |
| Perplexity | `PERPLEXITY_API_KEY` | `perplexity/sonar-pro` |
| Ollama | `OLLAMA_BASE_URL` | `ollama/llama3.3` |

Ollama runs locally with no API key. Set `OLLAMA_BASE_URL` (defaults to `http://localhost:11434/v1`).

## Fallback Routing

Try multiple models in order. If one fails, the next is attempted:

```go
response, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
	Models: []string{
		"anthropic/claude-sonnet-4-6",
		"openai/gpt-4o",
		"google/gemini-2.5-pro",
	},
	Route:    "fallback",
	Messages: []am.Message{{Role: "user", Content: "Hello"}},
})
```

## Batch Processing

Process many requests with native provider batch APIs or concurrent fallback. OpenAI and Google batches run at 50% cost. Anthropic supports async processing for up to 10K requests. Other providers fall back to concurrent execution automatically.

```go
results, err := client.Batches.CreateAndPoll(ctx, am.BatchCreateRequest{
	Model: "openai/gpt-4o-mini",
	Requests: []am.BatchRequest{
		{CustomID: "req-1", Messages: []am.Message{{Role: "user", Content: "Summarize AI"}}},
		{CustomID: "req-2", Messages: []am.Message{{Role: "user", Content: "Summarize ML"}}},
		{CustomID: "req-3", Messages: []am.Message{{Role: "user", Content: "Summarize NLP"}}},
	},
}, am.BatchPollOptions{})
if err != nil {
	panic(err)
}

for _, result := range results.Results {
	fmt.Println(result.CustomID, result.Response.Choices[0].Message.Content)
}
```

Native batches can also be submitted and retrieved later by ID. See [Advanced Usage](docs/ADVANCED.md) for the full batch API including BatchBuilder, adaptive concurrency, submit-and-poll-later, and batch configuration.

## Server Mode

Run as a standalone HTTP server compatible with the OpenAI SDK:

```bash
go run cmd/anymodel/main.go serve --port 4141
```

Point any OpenAI-compatible client at `http://localhost:4141/api/v1` and use `provider/model` format for model names.

## Configuration

```go
client := am.NewClient(am.AnyModelConfig{
	Anthropic: &am.ProviderConfig{APIKey: "sk-ant-..."},
	OpenAI:    &am.ProviderConfig{APIKey: "sk-..."},
	Google:    &am.ProviderConfig{APIKey: "AIza..."},
	Aliases: map[string]string{
		"default": "anthropic/claude-sonnet-4-6",
		"fast":    "anthropic/claude-haiku-4-5",
		"smart":   "anthropic/claude-opus-4-6",
	},
	Defaults: &am.DefaultsConfig{
		Temperature: 0.7,
		MaxTokens:   4096,
		Retries:     2,
		Timeout:     120,
	},
})
```

A config file (`anymodel.config.json`) is also supported with environment variable interpolation. See [Advanced Usage](docs/ADVANCED.md) for details.

## Built-in Resilience

- **Retries**: Automatic retry with exponential backoff on 429/502/503 errors
- **Rate limit tracking**: Per-provider rate limit state from response headers, automatically skips rate-limited providers during fallback
- **Adaptive concurrency**: Auto-discovers your provider's rate limit ceiling using TCP-style slow-start + AIMD
- **Parameter translation**: `max_tokens` automatically sent as `max_completion_tokens` for newer OpenAI models. Unsupported parameters stripped before forwarding.
- **Smart batch defaults**: Automatic `max_tokens` estimation per-request in batches based on input size and model context limits
- **Memory-efficient batching**: Concurrent batch requests are streamed from disk. Only N requests are in-flight at a time, making 10K+ batches safe without memory spikes.

## Advanced

See [Advanced Usage](docs/ADVANCED.md) for tool calling, structured output, BatchBuilder, adaptive concurrency, custom providers, generation stats, auto pricing, and more.

## See Also

| Package | Description |
|---|---|
| **anymodel-go** | **Go version of this package (you are here)** |
| [anymodel](https://github.com/probeo-io/anymodel) | TypeScript version of this package |
| [anymodel-py](https://github.com/probeo-io/anymodel-py) | Python version of this package |
| [@probeo/anyserp](https://github.com/probeo-io/anyserp) | Unified SERP API router for TypeScript |
| [@probeo/workflow](https://github.com/probeo-io/workflow) | Stage-based pipeline engine for TypeScript |

## Support

If anymodel is useful to you, consider giving it a star. It helps others discover the project.

## License

MIT
