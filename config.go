package anymodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

var envVarRegex = regexp.MustCompile(`\$\{([^}]+)}`)

// ResolveConfig merges configuration from env, global, local, and programmatic sources.
func ResolveConfig(programmatic *Config) *Config {
	result := &Config{}
	applyEnvConfig(result)

	if home, err := os.UserHomeDir(); err == nil {
		if global, err := loadConfigFile(filepath.Join(home, ".anymodel", "config.json")); err == nil {
			mergeConfig(result, global)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if local, err := loadConfigFile(filepath.Join(cwd, "anymodel.config.json")); err == nil {
			mergeConfig(result, local)
		}
	}
	if programmatic != nil {
		mergeConfig(result, programmatic)
	}
	return result
}

func applyEnvConfig(c *Config) {
	envMap := map[string]**ProviderConfig{
		"OPENAI_API_KEY":     &c.OpenAI,
		"ANTHROPIC_API_KEY":  &c.Anthropic,
		"GOOGLE_API_KEY":     &c.Google,
		"MISTRAL_API_KEY":    &c.Mistral,
		"GROQ_API_KEY":       &c.Groq,
		"DEEPSEEK_API_KEY":   &c.DeepSeek,
		"XAI_API_KEY":        &c.XAI,
		"TOGETHER_API_KEY":   &c.Together,
		"FIREWORKS_API_KEY":  &c.Fireworks,
		"PERPLEXITY_API_KEY": &c.Perplexity,
	}
	for envVar, field := range envMap {
		if key := os.Getenv(envVar); key != "" {
			if *field == nil {
				*field = &ProviderConfig{}
			}
			(*field).APIKey = key
		}
	}
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		if c.Ollama == nil {
			c.Ollama = &ProviderConfig{}
		}
		c.Ollama.BaseURL = baseURL
	}
}

func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := envVarRegex.ReplaceAllStringFunc(string(data), func(match string) string {
		varName := match[2 : len(match)-1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match
	})
	var cfg Config
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func mergeConfig(base, override *Config) {
	mergeProvider(&base.OpenAI, override.OpenAI)
	mergeProvider(&base.Anthropic, override.Anthropic)
	mergeProvider(&base.Google, override.Google)
	mergeProvider(&base.Mistral, override.Mistral)
	mergeProvider(&base.Groq, override.Groq)
	mergeProvider(&base.DeepSeek, override.DeepSeek)
	mergeProvider(&base.XAI, override.XAI)
	mergeProvider(&base.Together, override.Together)
	mergeProvider(&base.Fireworks, override.Fireworks)
	mergeProvider(&base.Perplexity, override.Perplexity)
	mergeProvider(&base.Ollama, override.Ollama)

	if override.Custom != nil {
		if base.Custom == nil {
			base.Custom = make(map[string]CustomProviderConfig)
		}
		for k, v := range override.Custom {
			base.Custom[k] = v
		}
	}
	if override.Aliases != nil {
		if base.Aliases == nil {
			base.Aliases = make(map[string]string)
		}
		for k, v := range override.Aliases {
			base.Aliases[k] = v
		}
	}
	if override.Defaults != nil {
		if base.Defaults == nil {
			base.Defaults = &DefaultsConfig{}
		}
		if override.Defaults.Temperature != nil {
			base.Defaults.Temperature = override.Defaults.Temperature
		}
		if override.Defaults.MaxTokens != nil {
			base.Defaults.MaxTokens = override.Defaults.MaxTokens
		}
		if override.Defaults.Retries != nil {
			base.Defaults.Retries = override.Defaults.Retries
		}
		if override.Defaults.Timeout != nil {
			base.Defaults.Timeout = override.Defaults.Timeout
		}
		if len(override.Defaults.Transforms) > 0 {
			base.Defaults.Transforms = override.Defaults.Transforms
		}
	}
	if override.Batch != nil {
		if base.Batch == nil {
			base.Batch = &BatchConfig{}
		}
		if override.Batch.Dir != "" {
			base.Batch.Dir = override.Batch.Dir
		}
		if override.Batch.PollInterval > 0 {
			base.Batch.PollInterval = override.Batch.PollInterval
		}
		if override.Batch.ConcurrencyFallback > 0 {
			base.Batch.ConcurrencyFallback = override.Batch.ConcurrencyFallback
		}
		if override.Batch.ConcurrencyMax > 0 {
			base.Batch.ConcurrencyMax = override.Batch.ConcurrencyMax
		}
	}
}

func mergeProvider(base **ProviderConfig, override *ProviderConfig) {
	if override == nil {
		return
	}
	if *base == nil {
		*base = &ProviderConfig{}
	}
	if override.APIKey != "" {
		(*base).APIKey = override.APIKey
	}
	if override.DefaultModel != "" {
		(*base).DefaultModel = override.DefaultModel
	}
	if override.BaseURL != "" {
		(*base).BaseURL = override.BaseURL
	}
}

// BuiltInProviders maps provider slugs to their base URLs.
var BuiltInProviders = map[string]string{
	"mistral":    "https://api.mistral.ai/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"deepseek":   "https://api.deepseek.com",
	"xai":        "https://api.x.ai/v1",
	"together":   "https://api.together.xyz/v1",
	"fireworks":  "https://api.fireworks.ai/inference/v1",
}
