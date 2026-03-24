// Package anymodel provides a unified LLM router across OpenAI, Anthropic, Google, and more.
package anymodel

// Role is the role of a message participant.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// FinishReason indicates why the model stopped generating.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishError         FinishReason = "error"
)

// BatchStatus represents the lifecycle state of a batch.
type BatchStatus string

const (
	BatchPending    BatchStatus = "pending"
	BatchProcessing BatchStatus = "processing"
	BatchCompleted  BatchStatus = "completed"
	BatchFailed     BatchStatus = "failed"
	BatchCancelled  BatchStatus = "cancelled"
)

// BatchMode indicates how the batch is being processed.
type BatchMode string

const (
	BatchNative     BatchMode = "native"
	BatchConcurrent BatchMode = "concurrent"
)

// ── Messages ────────────────────────────────────────────────────────────────

// ImageURL is an image reference in a content part.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// ContentPart is a piece of a multi-part message.
type ContentPart struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`       // when type == "text"
	ImageURL *ImageURL `json:"image_url,omitempty"`  // when type == "image_url"
}

// ToolCallFunction describes the function a tool call is invoking.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

// Message is a chat message in the OpenAI format.
type Message struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"` // multi-part content (used internally)
}

// ── Tools ───────────────────────────────────────────────────────────────────

// FunctionDefinition describes a callable function.
type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Tool defines an available tool.
type Tool struct {
	Type     string             `json:"type"` // "function"
	Function FunctionDefinition `json:"function"`
}

// ToolChoiceObject forces a specific function call.
type ToolChoiceObject struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// ── Response Format ─────────────────────────────────────────────────────────

// ResponseFormat controls the output structure.
type ResponseFormat struct {
	Type       string          `json:"type"` // "text", "json_object", "json_schema"
	JSONSchema *JSONSchemaDef  `json:"json_schema,omitempty"`
}

// JSONSchemaDef defines a JSON schema for structured output.
type JSONSchemaDef struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema,omitempty"`
	Strict bool           `json:"strict,omitempty"`
}

// ── Provider Preferences ────────────────────────────────────────────────────

// ProviderPreferences controls model ordering and filtering.
type ProviderPreferences struct {
	Order             []string `json:"order,omitempty"`
	Only              []string `json:"only,omitempty"`
	Ignore            []string `json:"ignore,omitempty"`
	AllowFallbacks    *bool    `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool    `json:"require_parameters,omitempty"`
	Sort              string   `json:"sort,omitempty"` // "price", "throughput", "latency"
}

// ── Chat Completion Request ─────────────────────────────────────────────────

// ChatCompletionRequest is the unified request format.
type ChatCompletionRequest struct {
	// Required
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`

	// Standard optional
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	TopK             *int            `json:"top_k,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	RepetitionPenalty *float64       `json:"repetition_penalty,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
	Stop             []string        `json:"stop,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	Logprobs         *bool           `json:"logprobs,omitempty"`
	TopLogprobs      *int            `json:"top_logprobs,omitempty"`
	ResponseFormat   *ResponseFormat `json:"response_format,omitempty"`
	Tools            []Tool          `json:"tools,omitempty"`
	ToolChoice       any             `json:"tool_choice,omitempty"` // string or ToolChoiceObject
	User             string          `json:"user,omitempty"`
	ServiceTier      string          `json:"service_tier,omitempty"` // "auto" (default) or "flex" (OpenAI flex pricing)

	// Anymodel-specific
	Models     []string             `json:"models,omitempty"`
	Route      string               `json:"route,omitempty"` // "fallback"
	Transforms []string             `json:"transforms,omitempty"`
	Provider   *ProviderPreferences `json:"provider,omitempty"`
}

// ── Chat Completion Response ────────────────────────────────────────────────

// Usage reports token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChoice is a single completion choice.
type ChatCompletionChoice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
	Logprobs     any          `json:"logprobs,omitempty"`
}

// ChatCompletion is a non-streaming completion response.
type ChatCompletion struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"` // "chat.completion"
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

// ── Streaming ───────────────────────────────────────────────────────────────

// ChunkDelta is the incremental content in a streaming chunk.
type ChunkDelta struct {
	Role      Role       `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChunkChoice is a single choice in a streaming chunk.
type ChunkChoice struct {
	Index        int           `json:"index"`
	Delta        ChunkDelta    `json:"delta"`
	FinishReason *FinishReason `json:"finish_reason"`
	Logprobs     any           `json:"logprobs,omitempty"`
}

// ChatCompletionChunk is a streaming response chunk.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"` // "chat.completion.chunk"
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

// ── Models ──────────────────────────────────────────────────────────────────

// ModelPricing is cost per token.
type ModelPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// ModelArchitecture describes the model's capabilities.
type ModelArchitecture struct {
	Modality          string   `json:"modality"`
	InputModalities   []string `json:"input_modalities"`
	OutputModalities  []string `json:"output_modalities"`
	Tokenizer         string   `json:"tokenizer"`
}

// ModelTopProvider is the top provider's config for a model.
type ModelTopProvider struct {
	ContextLength      int  `json:"context_length"`
	MaxCompletionTokens int  `json:"max_completion_tokens"`
	IsModerated        bool `json:"is_moderated"`
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Created             int64             `json:"created"`
	Description         string            `json:"description"`
	ContextLength       int               `json:"context_length"`
	Pricing             ModelPricing      `json:"pricing"`
	Architecture        ModelArchitecture `json:"architecture"`
	TopProvider         ModelTopProvider  `json:"top_provider"`
	SupportedParameters []string          `json:"supported_parameters"`
}

// ── Generation Stats ────────────────────────────────────────────────────────

// GenerationStats records metrics for a single generation.
type GenerationStats struct {
	ID               string       `json:"id"`
	Model            string       `json:"model"`
	ProviderName     string       `json:"provider_name"`
	TotalCost        float64      `json:"total_cost"`
	TokensPrompt     int          `json:"tokens_prompt"`
	TokensCompletion int          `json:"tokens_completion"`
	Latency          float64      `json:"latency"`
	GenerationTime   float64      `json:"generation_time"`
	CreatedAt        string       `json:"created_at"`
	FinishReason     FinishReason `json:"finish_reason"`
	Streamed         bool         `json:"streamed"`
}

// ── Batch ───────────────────────────────────────────────────────────────────

// BatchRequestItem is a single request in a batch.
type BatchRequestItem struct {
	CustomID       string          `json:"custom_id"`
	Messages       []Message       `json:"messages"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	TopK           *int            `json:"top_k,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     any             `json:"tool_choice,omitempty"`
	ServiceTier    string          `json:"service_tier,omitempty"`
}

// BatchCreateOptions are shared options applied to all requests in a batch.
type BatchCreateOptions struct {
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	TopK           *int            `json:"top_k,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Tools          []Tool          `json:"tools,omitempty"`
	ToolChoice     any             `json:"tool_choice,omitempty"`
	ServiceTier    string          `json:"service_tier,omitempty"`
}

// BatchCreateRequest describes a new batch to create.
type BatchCreateRequest struct {
	Model     string             `json:"model"`
	Requests  []BatchRequestItem `json:"requests"`
	BatchMode string             `json:"batch_mode,omitempty"` // "native", "concurrent", or empty for auto-detect
	Options   *BatchCreateOptions `json:"options,omitempty"`
	Webhook   string             `json:"webhook,omitempty"`
}

// BatchObject is the metadata for a batch.
type BatchObject struct {
	ID           string      `json:"id"`
	Object       string      `json:"object"` // "batch"
	Status       BatchStatus `json:"status"`
	Model        string      `json:"model"`
	ProviderName string      `json:"provider_name"`
	BatchMode    BatchMode   `json:"batch_mode"`
	Total        int         `json:"total"`
	Completed    int         `json:"completed"`
	Failed       int         `json:"failed"`
	CreatedAt    string      `json:"created_at"`
	CompletedAt  *string     `json:"completed_at"`
	ExpiresAt    *string     `json:"expires_at"`
}

// BatchError describes an error in a batch result.
type BatchError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// BatchResultItem is a single result within a batch.
type BatchResultItem struct {
	CustomID string          `json:"custom_id"`
	Status   string          `json:"status"` // "success" or "error"
	Response *ChatCompletion `json:"response"`
	Error    *BatchError     `json:"error"`
}

// BatchUsageSummary aggregates token usage across a batch.
type BatchUsageSummary struct {
	TotalPromptTokens     int     `json:"total_prompt_tokens"`
	TotalCompletionTokens int     `json:"total_completion_tokens"`
	EstimatedCost         float64 `json:"estimated_cost"`
}

// BatchResults is the final output of a completed batch.
type BatchResults struct {
	ID           string            `json:"id"`
	Status       BatchStatus       `json:"status"`
	Results      []BatchResultItem `json:"results"`
	UsageSummary BatchUsageSummary `json:"usage_summary"`
}

// BatchPollOptions controls batch polling behavior.
type BatchPollOptions struct {
	Interval     float64                  `json:"interval,omitempty"`       // seconds
	Timeout      float64                  `json:"timeout,omitempty"`        // seconds, 0 = indefinite
	LogToConsole bool                     `json:"log_to_console,omitempty"` // log poll progress to stdout
	OnProgress   func(batch *BatchObject) `json:"-"`
}

// ── Config ──────────────────────────────────────────────────────────────────

// ProviderConfig holds credentials for a built-in provider.
type ProviderConfig struct {
	APIKey       string `json:"api_key,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	BaseURL      string `json:"base_url,omitempty"` // for ollama
}

// CustomProviderConfig holds settings for an OpenAI-compatible endpoint.
type CustomProviderConfig struct {
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key,omitempty"`
	Models  []string `json:"models,omitempty"`
}

// DefaultsConfig holds default request parameters.
type DefaultsConfig struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Retries     *int     `json:"retries,omitempty"`
	Timeout     *float64 `json:"timeout,omitempty"`
	Transforms  []string `json:"transforms,omitempty"`
}

// RoutingConfig holds fallback routing settings.
type RoutingConfig struct {
	FallbackOrder  []string `json:"fallback_order,omitempty"`
	AllowFallbacks *bool    `json:"allow_fallbacks,omitempty"`
}

// BatchConfig holds batch processing settings.
type BatchConfig struct {
	Dir                 string  `json:"dir,omitempty"`
	PollInterval        float64 `json:"poll_interval,omitempty"`         // seconds
	ConcurrencyFallback int     `json:"concurrency_fallback,omitempty"`
	RetentionDays       int     `json:"retention_days,omitempty"`
}

// IOConfig holds filesystem concurrency settings.
type IOConfig struct {
	ReadConcurrency  int `json:"read_concurrency,omitempty"`
	WriteConcurrency int `json:"write_concurrency,omitempty"`
}

// Config is the top-level configuration for AnyModel.
type Config struct {
	Anthropic  *ProviderConfig                `json:"anthropic,omitempty"`
	OpenAI     *ProviderConfig                `json:"openai,omitempty"`
	Google     *ProviderConfig                `json:"google,omitempty"`
	Mistral    *ProviderConfig                `json:"mistral,omitempty"`
	Groq       *ProviderConfig                `json:"groq,omitempty"`
	DeepSeek   *ProviderConfig                `json:"deepseek,omitempty"`
	XAI        *ProviderConfig                `json:"xai,omitempty"`
	Together   *ProviderConfig                `json:"together,omitempty"`
	Fireworks  *ProviderConfig                `json:"fireworks,omitempty"`
	Perplexity *ProviderConfig                `json:"perplexity,omitempty"`
	Ollama     *ProviderConfig                `json:"ollama,omitempty"`
	Custom     map[string]CustomProviderConfig `json:"custom,omitempty"`
	Aliases    map[string]string              `json:"aliases,omitempty"`
	Defaults   *DefaultsConfig                `json:"defaults,omitempty"`
	Routing    *RoutingConfig                 `json:"routing,omitempty"`
	Batch      *BatchConfig                   `json:"batch,omitempty"`
	IO         *IOConfig                      `json:"io,omitempty"`
}
