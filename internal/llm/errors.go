package llm

import (
	"errors"
	"fmt"
)

var (
	ErrProviderNotFound = errors.New("provider not found")
	ErrNoAPIKey         = errors.New("no API key configured")
	ErrRateLimited      = errors.New("rate limited")
	ErrContextTooLong   = errors.New("context too long")
)

// ProviderError wraps an error with provider context.
type ProviderError struct {
	Provider string
	Model    string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Model != "" {
		return fmt.Sprintf("%s/%s: %v", e.Provider, e.Model, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Provider, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }
