# Changelog

All notable changes to this project will be documented in this file.

## [0.3.1] - 2026-03-17

### Changed

- Concurrent batch processing now streams requests from disk via channel instead of holding all in memory — safe for 10K+ request batches
- Only N requests (default 5) are in-flight at a time, the rest stay on disk until needed

## [0.3.0] - 2026-03-17

### Added

- Native batch API support for OpenAI (JSONL file upload, 50% cost reduction, 24hr processing window)
- Native batch API support for Anthropic (Message Batches API, up to 10K requests)
- Native batch API support for Google Gemini via `batchGenerateContent` (50% cost reduction)
- `OpenAIBatchAdapter`, `AnthropicBatchAdapter`, and `GoogleBatchAdapter` implementing `BatchAdapter` interface
- Automatic provider detection — native batch for OpenAI/Anthropic/Google, concurrent fallback for others
- Automatic `max_tokens` estimation for batch requests — when not explicitly set, calculates a safe value per-request based on estimated input token count and model context/completion limits
- `ResolveMaxTokens()` and `EstimateTokenCount()` exported utilities
- OpenAI `service_tier` support — set `ServiceTier: "flex"` on requests for 50% cost reduction with flexible latency
- Configurable HTTP request timeout — 2 minutes default for normal requests, 10 minutes for flex (`service_tier: "flex"`) requests, both settable via `SetDefaultHTTPTimeout()` and `SetFlexHTTPTimeout()`

## [0.2.0] - 2026-03-16

### Added

- Native Perplexity provider with static model listing (sonar, sonar-pro, sonar-reasoning, sonar-reasoning-pro, sonar-deep-research, r1-1776)
- Citation passthrough in Perplexity responses
- Cross-language links in README (Node.js, Python)

### Changed

- Perplexity upgraded from generic OpenAI-compatible adapter to dedicated native provider

## [0.1.1] - 2026-03-15

### Changed

- Go version requirement lowered from 1.26.1 to 1.22 for broader compatibility
- Added CLAUDE.md with Go conventions

## [0.1.0] - 2026-03-15

### Added

- AnyModel Go SDK with `Chat.Completions.Create()`, `Models.List()`, `Generation.Get()`
- Provider adapters for OpenAI, Anthropic, and Google/Gemini
- Built-in providers: Mistral, Groq, DeepSeek, xAI, Together, Fireworks, Perplexity, Ollama
- Custom provider support for any OpenAI-compatible endpoint
- Channel-based streaming with SSE
- Unified tool calling across providers
- Fallback routing with automatic failover
- Batch processing with concurrent fallback
- HTTP server mode (`anymodel serve`)
- Config file support (`anymodel.config.json`, `~/.anymodel/config.json`)
- Environment variable interpolation in config
- Automatic retry with exponential backoff
- Per-provider rate limit tracking

