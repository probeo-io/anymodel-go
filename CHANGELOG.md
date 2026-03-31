# Changelog

All notable changes to this project will be documented in this file.

## [0.8.0] - 2026-03-30

### Added

- **Adaptive concurrency for concurrent batches** — set `concurrencyFallback: 'auto'` to dynamically discover your provider's rate limit ceiling instead of using a fixed concurrency. Uses TCP-style slow-start (exponential ramp: 5 → 10 → 20 → 40 → ...) then switches to AIMD (additive increase / multiplicative decrease) after the first 429 or header-driven backoff. Proactively reads `x-ratelimit-remaining-requests` headers to stay under the limit without hitting 429s.
- **`concurrencyMax` config option** — hard ceiling for auto concurrency. Useful when multiple batch jobs share the same API key: set each to e.g. 50 to divide your rate limit budget predictably.
- **Response metadata on provider adapters** — `sendRequestWithMeta()` method on OpenAI, Anthropic, and Google adapters now returns response headers alongside the completion. Rate limit headers (`x-ratelimit-remaining-requests`, `retry-after`, etc.) are extracted and fed into the existing `RateLimitTracker`, which was previously unwired.
- **`completeWithMeta()` on Router** — internal method that returns `ChatCompletionWithMeta` with rate limit headers. Used by the batch manager for adaptive concurrency; also activates the `RateLimitTracker` for fallback routing decisions.
- Exported `AdaptiveConcurrencyController` class and `AdaptiveConcurrencyOptions` type for advanced usage
- Exported `ResponseMeta` and `ChatCompletionWithMeta` types

### Fixed

- **`max_tokens` → `max_completion_tokens` translation** — newer OpenAI models (gpt-4o, gpt-4o-mini, o1, o3, o4-mini, gpt-5-mini) require `max_completion_tokens` instead of `max_tokens`. anymodel now detects the model and sends the correct parameter automatically. The SDK surface stays unchanged — callers always pass `max_tokens`.
- **Flex discount missing on concurrent batch cost calculations** — `BatchManager.getResults()` only applied the 50% native batch discount, ignoring `service_tier: 'flex'` on concurrent batches. Now correctly applies 50% for both native and concurrent+flex.
- **`service_tier` now persisted on `BatchObject`** — previously lost after batch creation, preventing accurate cost calculation on retrieval.
- Added `gpt-5-mini` to token estimation limits (1M context, 65K max completion tokens)

## [0.7.1] - 2026-03-26

### Fixed

- Batch cost calculations now apply 50% discount for native batch APIs (OpenAI, Anthropic, Google)
- BatchBuilder accounts for flex pricing discount on concurrent batches

## [0.7.0] - 2026-03-24

### Added

- **BatchBuilder API** — ergonomic batch construction with `client.batches.open(config)`
  - `add(prompt)` persists to disk immediately, caller just passes strings
  - `submit()` formats and dispatches to provider (Anthropic/OpenAI/Google format handled automatically)
  - `poll()` returns clean `succeeded`/`failed` results with per-item costs
  - `retry(failed)` creates a new builder pre-loaded with failed items
  - Auto-generated IDs, system prompt injection, provider-specific formatting all hidden
- Poll logging: `logToConsole` option and `ANYMODEL_BATCH_POLL_LOG` env var

## [0.6.0] - 2026-03-19

### Added

- Automatic per-request cost calculation from bundled pricing data (323 models)
- Pricing fetched from OpenRouter at build time — always current as of last publish
- `GenerationStats.total_cost` now calculated automatically from token usage
- `BatchUsageSummary.estimated_cost` now calculated automatically from token usage
- Exported `getModelPricing()`, `calculateCost()`, `PRICING_AS_OF`, `PRICING_MODEL_COUNT`
- `batch_mode` option on `BatchCreateRequest` — set to `"concurrent"` to force individual requests (e.g. for flex pricing)
- `service_tier` support on batch requests (concurrent path only — native batch already discounted)

## [0.5.1] - 2026-03-17

### Changed

- Concurrent batch processing now streams requests from disk instead of holding all in memory — safe for 10K+ request batches
- Batch concurrency gating improved: only N requests (default 5) are in-flight at a time, the rest stay on disk until needed

## [0.5.0] - 2026-03-17

### Added

- Native batch API support for Google Gemini via `batchGenerateContent` (50% cost reduction)
- `GoogleBatchAdapter` with inline and file-based result handling
- Google batch status polling with `JOB_STATE_*` mapping
- Google batch cancellation support
- Automatic `max_tokens` estimation for batch requests — when not explicitly set, calculates a safe value per-request based on estimated input token count and model context/completion limits (~4 chars/token heuristic with 5% safety margin)
- `resolveMaxTokens()` and `estimateTokenCount()` exported utilities for manual use
- Model limits lookup table covering OpenAI, Anthropic, and Google model families
- OpenAI `service_tier` support — set `service_tier: "flex"` on requests for 50% cost reduction with flexible latency
- Configurable HTTP request timeout — 2 minutes default for normal requests, 10 minutes for flex (`service_tier: "flex"`) requests, both settable via `setDefaultTimeout()` and `setFlexTimeout()`

### Changed

- Google adapter `supportsBatch()` now returns `true`
- README updated to reflect Google as a native batch provider

## [0.4.0] - 2026-03-16

### Added

- Native Perplexity provider with static model listing (sonar, sonar-pro, sonar-reasoning, sonar-reasoning-pro, sonar-deep-research, r1-1776)
- Citation passthrough in Perplexity responses
- Cross-language links in README (Python, Go)

### Changed

- Perplexity upgraded from generic OpenAI-compatible adapter to dedicated native provider
- `perplexity/sonar-pro` added to model naming examples in README

## [0.3.0] - 2026-03-15

### Added

- Native batch API support for OpenAI (JSONL upload, 50% cost reduction, 24hr processing window)
- Native batch API support for Anthropic (Message Batches API, up to 10K requests)
- Automatic provider detection — native batch for OpenAI/Anthropic, concurrent fallback for others
- `batch_mode` field on `BatchObject` (`"native"` or `"concurrent"`)
- Fire-and-forget batch submission via `client.batches.create()` — submit now, poll later
- Batch resumability across process restarts for native batches (provider state persisted to disk)
- Batch cancellation at the provider level for native batches
- Per-item error handling for native batch results
- Configurable poll interval via `batch.pollInterval` config or per-call `options.interval`
- `BatchAdapter` interface for implementing custom native batch providers
- High-volume filesystem IO layer (`fs-io`) — concurrency-limited async queues (20 read, 10 write), atomic durable writes with fsync, directory existence caching, path memoization
- Configurable IO concurrency via `io.readConcurrency` and `io.writeConcurrency` in client config (defaults: 20 read, 10 write)
- Exported `configureFsIO`, `readFileQueued`, `writeFileQueued`, `writeFileFlushedQueued`, `appendFileQueued`, `ensureDir`, `joinPath`, `getFsQueueStatus`, `waitForFsQueuesIdle` utilities

### Changed

- Batch storage directory changed from `~/.anymodel/batches/` to `./.anymodel/batches/` (project-local)
- `BatchStore` now fully async — all methods return Promises, using queued IO instead of blocking sync `fs` calls
- `client.batches.get()`, `client.batches.list()`, `client.batches.results()` are now async (return Promises)
- `client.batches.cancel()` is now async (returns `Promise<BatchObject>`)
- Batch metadata writes use atomic temp-file + fsync + rename pattern to prevent corruption on crash

## [0.2.0] - 2026-03-13

### Added

- Built-in providers: Mistral, Groq, DeepSeek, xAI, Together, Fireworks, Perplexity, Ollama
- Dynamic model list fetching from Anthropic and Google APIs
- OpenAI model filter updated to include o1/o3/o4 prefixes
- Release script (`npm run release`)
- CI workflow (lint, test, build on Node 20 and 22)
- npm publish workflow (triggers on GitHub release)
- SECURITY.md, CODE_OF_CONDUCT.md, CONTRIBUTING.md
- `.editorconfig`
- Runnable examples (`examples/basic.ts`)

### Changed

- README expanded with full provider table and examples

## [0.1.0] - 2026-03-13

### Added

- AnyModel SDK client with `chat.completions.create()`, `models.list()`, `generation.get()`
- Provider adapters for OpenAI, Anthropic, and Google/Gemini
- Custom provider support for any OpenAI-compatible endpoint
- Unified tool calling and structured output across all providers
- Fallback routing with `models` array and `route: "fallback"`
- Provider preferences (`order`, `only`, `ignore`)
- Model aliases
- Streaming support (SSE)
- Automatic retry with exponential backoff on 429/502/503
- Per-provider rate limit tracking
- Middle-out context truncation transform
- Config file support (`anymodel.config.json`, `~/.anymodel/config.json`)
- Environment variable interpolation in config (`${ENV_VAR}`)
- Config resolution order: programmatic > local > global > env vars
- Batch processing with disk persistence and `createAndPoll()`
- Generation stats tracking
- HTTP server mode (`anymodel serve`)
- OpenAI SDK-compatible API at `/api/v1`
- CLI entry point
