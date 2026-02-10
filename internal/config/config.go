package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server     ServerConfig     `toml:"server"`
	Database   DatabaseConfig   `toml:"database"`
	Auth       AuthConfig       `toml:"auth"`
	LLM        LLMConfig        `toml:"llm"`
	Federation FederationConfig `toml:"federation"`
	Instance   InstanceConfig   `toml:"instance"`
}

type ServerConfig struct {
	Addr string `toml:"addr"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type AuthConfig struct {
	JWTSecret      string `toml:"jwt_secret"`
	TokenExpiryMin int    `toml:"token_expiry_min"`
}

type LLMConfig struct {
	GeminiAPIKey    string `toml:"gemini_api_key"`
	MistralAPIKey   string `toml:"mistral_api_key"`
	OpenRouterKey   string `toml:"openrouter_api_key"`
	GroqAPIKey      string `toml:"groq_api_key"`
	AnthropicAPIKey string `toml:"anthropic_api_key"`
	HuggingFaceKey  string `toml:"huggingface_api_key"`
}

type FederationConfig struct {
	Enabled bool `toml:"enabled"`
}

type InstanceConfig struct {
	ID   string `toml:"id"`
	Name string `toml:"name"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Database: DatabaseConfig{
			Path: "data/nodes.db",
		},
		Auth: AuthConfig{
			JWTSecret:      "change-me-in-production",
			TokenExpiryMin: 1440, // 24h
		},
		Instance: InstanceConfig{
			ID:   "local",
			Name: "horostracker-local",
		},
		Federation: FederationConfig{
			Enabled: false,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}
