// CLAUDE:SUMMARY Factory that builds a multi-provider LLM Client from config (activates only providers with API keys)
package llm

import "github.com/hazyhaar/horostracker/internal/config"

// NewFromConfig creates a multi-provider LLM client from the application config.
// Only providers with configured API keys are activated.
func NewFromConfig(cfg config.LLMConfig) *Client {
	var providers []Provider

	if cfg.GeminiAPIKey != "" {
		providers = append(providers, NewGeminiProvider(cfg.GeminiAPIKey))
	}

	if cfg.MistralAPIKey != "" {
		providers = append(providers, NewOpenAIProvider(OpenAIConfig{
			Name:         "mistral",
			BaseURL:      "https://api.mistral.ai/v1",
			APIKey:       cfg.MistralAPIKey,
			Models:       []string{"mistral-large-latest", "mistral-small-latest", "codestral-latest"},
			DefaultModel: "mistral-small-latest",
		}))
	}

	if cfg.GroqAPIKey != "" {
		providers = append(providers, NewOpenAIProvider(OpenAIConfig{
			Name:         "groq",
			BaseURL:      "https://api.groq.com/openai/v1",
			APIKey:       cfg.GroqAPIKey,
			Models:       []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768"},
			DefaultModel: "llama-3.3-70b-versatile",
		}))
	}

	if cfg.OpenRouterKey != "" {
		providers = append(providers, NewOpenAIProvider(OpenAIConfig{
			Name:         "openrouter",
			BaseURL:      "https://openrouter.ai/api/v1",
			APIKey:       cfg.OpenRouterKey,
			Models:       []string{"deepseek/deepseek-chat", "qwen/qwen-2.5-72b-instruct", "meta-llama/llama-3.3-70b-instruct"},
			DefaultModel: "deepseek/deepseek-chat",
		}))
	}

	if cfg.AnthropicAPIKey != "" {
		providers = append(providers, NewAnthropicProvider(cfg.AnthropicAPIKey))
	}

	if cfg.HuggingFaceKey != "" {
		providers = append(providers, NewOpenAIProvider(OpenAIConfig{
			Name:         "huggingface",
			BaseURL:      "https://api-inference.huggingface.co/models",
			APIKey:       cfg.HuggingFaceKey,
			Models:       []string{"meta-llama/Llama-3.3-70B-Instruct", "Qwen/Qwen2.5-72B-Instruct"},
			DefaultModel: "meta-llama/Llama-3.3-70B-Instruct",
		}))
	}

	// DeepSeek direct (free tier available)
	if key := lookupDeepSeekKey(cfg); key != "" {
		providers = append(providers, NewOpenAIProvider(OpenAIConfig{
			Name:         "deepseek",
			BaseURL:      "https://api.deepseek.com/v1",
			APIKey:       key,
			Models:       []string{"deepseek-chat", "deepseek-reasoner"},
			DefaultModel: "deepseek-chat",
		}))
	}

	return New(providers)
}

// lookupDeepSeekKey checks for a DeepSeek API key in the config.
// DeepSeek can also be accessed via OpenRouter, but direct is preferred.
func lookupDeepSeekKey(cfg config.LLMConfig) string {
	// Could be added as a separate config field later;
	// for now, DeepSeek is accessible via OpenRouter
	return ""
}
