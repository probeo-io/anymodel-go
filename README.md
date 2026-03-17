# anymodel-go

Go SDK for **anymodel** — a unified LLM router across OpenAI, Anthropic, Google, and more.

## Install

```bash
go get github.com/probeo-io/anymodel-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    am "github.com/probeo-io/anymodel-go"
)

func main() {
    client := am.New(nil) // reads API keys from env vars

    result, err := client.Chat.Completions.Create(context.Background(), am.ChatCompletionRequest{
        Model:    "anthropic/claude-sonnet-4-6",
        Messages: []am.Message{{Role: am.RoleUser, Content: "Hello!"}},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Choices[0].Message.Content)
}
```

## Features

- **Unified API** — One interface for OpenAI, Anthropic, Google, Mistral, Groq, DeepSeek, xAI, Together, Fireworks, Perplexity, Ollama, and custom endpoints
- **Streaming** — Channel-based streaming with SSE
- **Tool Calling** — Unified function calling across providers
- **Fallback Routing** — Automatic failover across models/providers
- **Batch Processing** — Native batch APIs (OpenAI, Anthropic, Google) + concurrent fallback
- **HTTP Server** — OpenAI-compatible `/api/v1` endpoints
- **Rate Limiting** — Per-provider tracking with automatic backoff
- **Retry** — Exponential backoff with jitter on 429/5xx
- **Config** — Env vars, JSON config files, and programmatic config

## Streaming

```go
chunkCh, errCh, err := client.Chat.Completions.CreateStream(ctx, am.ChatCompletionRequest{
    Model:    "openai/gpt-4o",
    Messages: []am.Message{{Role: am.RoleUser, Content: "Write a haiku."}},
})
if err != nil {
    panic(err)
}

for chunk := range chunkCh {
    fmt.Print(chunk.Choices[0].Delta.Content)
}
```

### Flex Pricing (OpenAI)

Get 50% off OpenAI requests with flexible latency:

```go
result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
    Model:       "openai/gpt-4o",
    Messages:    []am.Message{{Role: am.RoleUser, Content: "Hello!"}},
    ServiceTier: "flex",
})
```

## Fallback Routing

```go
result, err := client.Chat.Completions.Create(ctx, am.ChatCompletionRequest{
    Models: []string{
        "anthropic/claude-sonnet-4-6",
        "openai/gpt-4o",
        "google/gemini-2.5-pro",
    },
    Route:    "fallback",
    Messages: []am.Message{{Role: am.RoleUser, Content: "Hello!"}},
})
```

## Batch Processing

```go
results, err := client.Batches.CreateAndPoll(ctx, am.BatchCreateRequest{
    Model: "anthropic/claude-haiku-4-5",
    Requests: []am.BatchRequestItem{
        {CustomID: "req-1", Messages: []am.Message{{Role: am.RoleUser, Content: "What is AI?"}}},
        {CustomID: "req-2", Messages: []am.Message{{Role: am.RoleUser, Content: "What is ML?"}}},
    },
}, am.BatchPollOptions{
    OnProgress: func(b *am.BatchObject) {
        fmt.Printf("Progress: %d/%d\n", b.Completed, b.Total)
    },
})
```

When `max_tokens` isn't set on a batch request, anymodel automatically calculates a safe value per-request based on the estimated input size and the model's context window. This prevents truncated responses and context overflow errors without requiring you to hand-tune each request in a large batch.

## HTTP Server

```bash
go run ./cmd/anymodel serve --port 4141
```

Then use any OpenAI-compatible client:

```bash
curl http://localhost:4141/api/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "anthropic/claude-sonnet-4-6", "messages": [{"role": "user", "content": "Hi"}]}'
```

## Configuration

API keys are read from environment variables:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
export GOOGLE_API_KEY=AIza...
```

Or configure programmatically:

```go
client := am.New(&am.Config{
    Anthropic: &am.ProviderConfig{APIKey: "sk-ant-..."},
    Aliases:   map[string]string{"default": "anthropic/claude-sonnet-4-6"},
    Defaults:  &am.DefaultsConfig{Temperature: floatPtr(0.7), Timeout: floatPtr(120)}, // default: 2 min normal, 10 min flex
})
```

## Supported Providers

| Provider | Env Var | Batch |
|----------|---------|-------|
| OpenAI | `OPENAI_API_KEY` | Native |
| Anthropic | `ANTHROPIC_API_KEY` | Native |
| Google | `GOOGLE_API_KEY` | Native |
| Mistral | `MISTRAL_API_KEY` | Concurrent |
| Groq | `GROQ_API_KEY` | Concurrent |
| DeepSeek | `DEEPSEEK_API_KEY` | Concurrent |
| xAI | `XAI_API_KEY` | Concurrent |
| Together | `TOGETHER_API_KEY` | Concurrent |
| Fireworks | `FIREWORKS_API_KEY` | Concurrent |
| Perplexity | `PERPLEXITY_API_KEY` | Concurrent |
| Ollama | `OLLAMA_BASE_URL` | Concurrent |

## Also Available

- **Node.js**: [`@probeo/anymodel`](https://github.com/probeo-io/anymodel) on npm
- **Python**: [`anymodel-py`](https://github.com/probeo-io/anymodel-py) on PyPI

## License

MIT
