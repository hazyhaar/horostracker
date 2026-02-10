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
	Bot        BotConfig        `toml:"bot"`
	Federation FederationConfig `toml:"federation"`
	Instance   InstanceConfig   `toml:"instance"`
}

type ServerConfig struct {
	Addr     string `toml:"addr"`      // Listen address — TCP + UDP same port (e.g. ":8080")
	CertFile string `toml:"cert_file"` // TLS cert path (empty = self-signed dev cert)
	KeyFile  string `toml:"key_file"`  // TLS key path
}

type DatabaseConfig struct {
	Path        string `toml:"path"`
	FlowsPath   string `toml:"flows_path"`
	MetricsPath string `toml:"metrics_path"`
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

type BotConfig struct {
	Handle            string `toml:"handle"`              // bot username (default: "horostracker")
	Enabled           bool   `toml:"enabled"`             // auto-create bot account on startup
	CreditPerDay      int    `toml:"credit_per_day"`      // daily credit allowance
	DefaultProvider   string `toml:"default_provider"`    // preferred LLM provider
	DefaultModel      string `toml:"default_model"`       // preferred model
}

type FederationConfig struct {
	Enabled          bool     `toml:"enabled"`
	InstanceURL      string   `toml:"instance_url"`       // public URL for this instance
	SignatureAlgo    string   `toml:"signature_algorithm"` // ECDSA or Ed25519
	PrivateKeyPath   string   `toml:"private_key_path"`   // PEM key for signing nodes
	PublicKeyID      string   `toml:"public_key_id"`      // key identifier
	VerifySignatures bool     `toml:"verify_signatures"`  // reject unsigned foreign nodes
	PeerInstances    []string `toml:"peer_instances"`     // known peer URLs
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
			Path:        "data/nodes.db",
			FlowsPath:   "data/flows.db",
			MetricsPath: "data/metrics.db",
		},
		Auth: AuthConfig{
			JWTSecret:      "change-me-in-production",
			TokenExpiryMin: 1440, // 24h
		},
		Bot: BotConfig{
			Handle:       "horostracker",
			Enabled:      true,
			CreditPerDay: 1000,
		},
		Instance: InstanceConfig{
			ID:   "local",
			Name: "horostracker-local",
		},
		Federation: FederationConfig{
			Enabled:          false,
			SignatureAlgo:    "Ed25519",
			VerifySignatures: true,
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
