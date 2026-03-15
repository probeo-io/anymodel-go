# anymodel-go

Go SDK for anymodel — unified LLM API across OpenAI, Anthropic, Google, and OpenAI-compatible providers.

## Build & Test

```bash
go build ./...     # compile all packages
go test ./...      # run tests
go vet ./...       # static analysis
gofmt -l .         # check formatting
```

## Architecture

Flat package structure — all core code lives in the root `anymodel` package to avoid import cycles.

- `client.go` — Entry point: `anymodel.New(cfg)` returns a `*Client` with namespace accessors (`Chat.Completions`, `Models`, `Generation`, `Batches`)
- `types.go` — All shared types: `ChatCompletionRequest`, `ChatCompletion`, `ChatCompletionChunk`, `Message`, `Tool`, `Config`, batch types, etc.
- `config.go` — Config resolution: merges env vars, `~/.anymodel/config.json`, local `anymodel.config.json`, and programmatic config
- `router.go` — Request routing, default application, parameter stripping, fallback logic, retry, and generation stats recording
- `provider_adapter.go` — `Adapter` interface (every provider implements this), `BatchAdapter` interface, and `Registry` for provider lookup
- `provider_openai.go` — OpenAI adapter (also the base for OpenAI-compatible providers)
- `provider_anthropic.go` — Anthropic adapter with Claude-specific request/response mapping
- `provider_google.go` — Google Gemini adapter
- `provider_custom.go` — Generic adapter for OpenAI-compatible endpoints (Mistral, Groq, DeepSeek, etc.)
- `provider_sse.go` — SSE stream parser, returns `<-chan SSEEvent`
- `errors.go` — `Error` type with HTTP status codes and provider metadata
- `util_transforms.go` — Message transforms (e.g., `middle-out` context window trimming)
- `util_retry.go` — Generic retry with exponential backoff
- `util_rate_limiter.go` — Per-provider rate limit tracking
- `util_model_parser.go` — Parses `"provider/model"` strings, resolves aliases
- `util_validate.go` — Request validation
- `util_id.go` — ID generation
- `util_generation_stats.go` — In-memory generation stats store
- `batch_manager.go` — Batch orchestration (native provider batches or concurrent fallback)
- `batch_store.go` — Filesystem-based batch persistence
- `server/server.go` — HTTP server exposing the client as a REST API (OpenAI-compatible endpoints)
- `cmd/anymodel/main.go` — CLI entry point
- `examples/basic.go` — Usage example

## Key Design Decisions

- **Flat package**: All core types and logic in the root `anymodel` package. Only `server/`, `cmd/`, and `examples/` are separate packages. This prevents import cycles and keeps the API surface simple.
- **Channel-based streaming**: `SendStreamingRequest` returns `(<-chan ChatCompletionChunk, <-chan error)`. Consumers range over the chunk channel; errors arrive on a separate channel.
- **Adapter interface**: Providers implement `Adapter` (and optionally `BatchAdapter`). The `Registry` stores them by slug. The `Router` resolves models, applies transforms, strips unsupported params, and dispatches to the correct adapter.
- **OpenAI as base format**: All request/response types use OpenAI's schema. Non-OpenAI providers (Anthropic, Google) translate to/from this format internally.
- **Model strings**: Models are addressed as `"provider/model"` (e.g., `"openai/gpt-4o"`). Aliases map short names to full model strings.
- **Config layering**: env vars -> global config -> local config -> programmatic config, each layer overriding the previous.

## Adding a New Provider

1. Create `provider_<name>.go` in the root package.
2. Define a struct that implements `Adapter`:
   ```go
   type MyAdapter struct { ... }
   func (a *MyAdapter) Name() string
   func (a *MyAdapter) SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error)
   func (a *MyAdapter) SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error)
   func (a *MyAdapter) ListModels(ctx context.Context) ([]ModelInfo, error)
   func (a *MyAdapter) SupportsParameter(param string) bool
   func (a *MyAdapter) SupportsBatch() bool
   ```
3. If the provider is OpenAI-compatible, use `NewCustomAdapter` instead (see `provider_custom.go`).
4. Add the provider's config field to `Config` in `types.go` and wire it up in `registerProviders()` in `client.go`.
5. Add the env var mapping in `applyEnvConfig()` in `config.go`.
6. If it uses a standard base URL, add it to `BuiltInProviders` in `config.go`.
