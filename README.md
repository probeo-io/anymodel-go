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

Concurrent batch requests are streamed from disk — only N requests (default 5) are in-flight at a time, making 10K+ request batches safe without memory spikes.

### BatchBuilder API

An ergonomic interface for building batches — just pass strings, and anymodel handles IDs, system prompt injection, and provider-specific formatting:

```go
batch := client.Batches.Open(am.BatchBuilderConfig{
    Model:  "anthropic/claude-sonnet-4-6",
    System: "You are an expert.",
})

batch.Add("What is an LLC?")
batch.Add("How do I dissolve an LLC?")

err := batch.Submit(ctx)
results, err := batch.Poll(ctx, am.BatchPollOptions{})

fmt.Println(results.Succeeded) // successful responses with per-item costs
fmt.Println(results.Failed)    // failed items
fmt.Println(results.Usage)     // aggregate usage and EstimatedCost

// Retry failed items
retryBatch := batch.Retry(results.Failed)
retryBatch.Submit(ctx)
retryResults, _ := retryBatch.Poll(ctx, am.BatchPollOptions{})
```

### Batch mode

Force concurrent execution instead of native batch APIs (useful when you want flex pricing on individual requests):

```go
results, err := client.Batches.CreateAndPoll(ctx, am.BatchCreateRequest{
    Model:     "openai/gpt-4o",
    BatchMode: "concurrent", // skip native batch, run as individual requests
    Requests: []am.BatchRequestItem{
        {CustomID: "req-1", Messages: []am.Message{{Role: am.RoleUser, Content: "Hello"}}},
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
    Requests: []am.BatchRequestItem{
        {CustomID: "req-1", Messages: []am.Message{{Role: am.RoleUser, Content: "Hello"}}},
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

## Generation Stats

```go
result, err := client.Chat.Completions.Create(ctx, req)
stats := client.Generation.Get(result.ID)
fmt.Println(stats.Latency, stats.TokensPrompt, stats.TokensCompletion)
fmt.Println(stats.TotalCost) // auto-calculated from bundled pricing data
```

### Auto Pricing / Cost Calculation

Pricing for 323 models is baked in at build time from OpenRouter — always current as of last publish. Costs are calculated automatically from token usage with no configuration needed.

```go
// Per-request cost on GenerationStats
stats := client.Generation.Get(result.ID)
fmt.Println(stats.TotalCost) // e.g. 0.0023

// Batch-level cost on BatchUsageSummary
results, _ := client.Batches.CreateAndPoll(ctx, request, am.BatchPollOptions{})
fmt.Println(results.Usage.EstimatedCost) // total across all requests

// Native batch pricing is automatically 50% off
// Utility functions also exported
pricing := am.GetModelPricing("anthropic/claude-sonnet-4-6")
cost := am.CalculateCost("anthropic/claude-sonnet-4-6", promptTokens, completionTokens)
```

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

## See Also

| Package | Description |
|---|---|
| [@probeo/anymodel](https://github.com/probeo-io/anymodel) | TypeScript version of this package |
| [anymodel-py](https://github.com/probeo-io/anymodel-py) | Python version of this package |
| [anyserp-go](https://github.com/probeo-io/anyserp-go) | Unified SERP API router for Go |
| [workflow-go](https://github.com/probeo-io/workflow-go) | Stage-based pipeline engine for Go |

## License

MIT
