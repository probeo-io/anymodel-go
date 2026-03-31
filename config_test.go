package anymodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFile(t *testing.T) {
	t.Run("loads config from file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "anymodel.config.json")
		data, _ := json.Marshal(Config{
			Aliases: map[string]string{"fast": "anthropic/claude-haiku-4-5"},
		})
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := loadConfigFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Aliases["fast"] != "anthropic/claude-haiku-4-5" {
			t.Errorf("alias = %q, want %q", cfg.Aliases["fast"], "anthropic/claude-haiku-4-5")
		}
	})

	t.Run("handles missing file gracefully", func(t *testing.T) {
		_, err := loadConfigFile("/nonexistent/path/config.json")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("interpolates env vars", func(t *testing.T) {
		t.Setenv("__TEST_KEY_GO", "sk-from-env")

		dir := t.TempDir()
		path := filepath.Join(dir, "anymodel.config.json")
		if err := os.WriteFile(path, []byte(`{"openai":{"api_key":"${__TEST_KEY_GO}"}}`), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := loadConfigFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.OpenAI == nil || cfg.OpenAI.APIKey != "sk-from-env" {
			got := ""
			if cfg.OpenAI != nil {
				got = cfg.OpenAI.APIKey
			}
			t.Errorf("apiKey = %q, want %q", got, "sk-from-env")
		}
	})
}

func TestMergeConfig(t *testing.T) {
	t.Run("programmatic overrides base", func(t *testing.T) {
		temp := 0.5
		base := &Config{Defaults: &DefaultsConfig{Temperature: &temp}}
		temp2 := 0.9
		override := &Config{Defaults: &DefaultsConfig{Temperature: &temp2}}
		mergeConfig(base, override)
		if *base.Defaults.Temperature != 0.9 {
			t.Errorf("temperature = %f, want 0.9", *base.Defaults.Temperature)
		}
	})

	t.Run("deep merges provider configs", func(t *testing.T) {
		base := &Config{Anthropic: &ProviderConfig{DefaultModel: "claude-haiku-4-5"}}
		override := &Config{Anthropic: &ProviderConfig{APIKey: "sk-test"}}
		mergeConfig(base, override)
		if base.Anthropic.APIKey != "sk-test" {
			t.Errorf("apiKey = %q, want %q", base.Anthropic.APIKey, "sk-test")
		}
		if base.Anthropic.DefaultModel != "claude-haiku-4-5" {
			t.Errorf("defaultModel = %q, want %q", base.Anthropic.DefaultModel, "claude-haiku-4-5")
		}
	})

	t.Run("merges aliases", func(t *testing.T) {
		base := &Config{Aliases: map[string]string{"a": "x"}}
		override := &Config{Aliases: map[string]string{"b": "y"}}
		mergeConfig(base, override)
		if base.Aliases["a"] != "x" || base.Aliases["b"] != "y" {
			t.Errorf("aliases not merged correctly: %v", base.Aliases)
		}
	})

	t.Run("nil override is no-op", func(t *testing.T) {
		base := &Config{Anthropic: &ProviderConfig{APIKey: "original"}}
		override := &Config{}
		mergeConfig(base, override)
		if base.Anthropic.APIKey != "original" {
			t.Errorf("apiKey = %q, want %q", base.Anthropic.APIKey, "original")
		}
	})
}

func TestApplyEnvConfig(t *testing.T) {
	t.Run("picks up API keys from env vars", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-env-anthropic")

		c := &Config{}
		applyEnvConfig(c)
		if c.Anthropic == nil || c.Anthropic.APIKey != "sk-env-anthropic" {
			got := ""
			if c.Anthropic != nil {
				got = c.Anthropic.APIKey
			}
			t.Errorf("apiKey = %q, want %q", got, "sk-env-anthropic")
		}
	})

	t.Run("picks up multiple provider keys", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-openai")
		t.Setenv("GOOGLE_API_KEY", "sk-google")

		c := &Config{}
		applyEnvConfig(c)
		if c.OpenAI == nil || c.OpenAI.APIKey != "sk-openai" {
			t.Error("openai key not set")
		}
		if c.Google == nil || c.Google.APIKey != "sk-google" {
			t.Error("google key not set")
		}
	})
}

func TestResolveConfig(t *testing.T) {
	t.Run("returns programmatic config", func(t *testing.T) {
		cfg := ResolveConfig(&Config{
			Anthropic: &ProviderConfig{APIKey: "sk-test"},
		})
		if cfg.Anthropic == nil || cfg.Anthropic.APIKey != "sk-test" {
			t.Error("programmatic config not applied")
		}
	})

	t.Run("returns non-nil for nil input", func(t *testing.T) {
		cfg := ResolveConfig(nil)
		if cfg == nil {
			t.Error("expected non-nil config")
		}
	})
}
