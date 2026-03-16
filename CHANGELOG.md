# Changelog

All notable changes to this project will be documented in this file.

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
- Batch processing with native OpenAI/Anthropic APIs and concurrent fallback
- HTTP server mode (`anymodel serve`)
- Config file support (`anymodel.config.json`, `~/.anymodel/config.json`)
- Environment variable interpolation in config
- Automatic retry with exponential backoff
- Per-provider rate limit tracking

