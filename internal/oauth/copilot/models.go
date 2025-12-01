package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/charmbracelet/catwalk/pkg/catwalk"
)

// ModelsDevURL is the URL to fetch model metadata from.
const ModelsDevURL = "https://models.dev/api.json"

// ProviderID is the identifier for the GitHub Copilot provider.
const ProviderID = "github-copilot"

// ModelsDevProvider represents a provider in the models.dev API response.
type ModelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	API    string                    `json:"api"`
	Doc    string                    `json:"doc"`
	Models map[string]ModelsDevModel `json:"models"`
}

// ModelsDevModel represents a model in the models.dev API response.
type ModelsDevModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Attachment  bool   `json:"attachment"`
	Reasoning   bool   `json:"reasoning"`
	ToolCall    bool   `json:"tool_call"`
	Temperature bool   `json:"temperature"`
	Knowledge   string `json:"knowledge"`
	ReleaseDate string `json:"release_date"`
	LastUpdated string `json:"last_updated"`
	Modalities  struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	OpenWeights bool `json:"open_weights"`
	Cost        struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
	Limit struct {
		Context int64 `json:"context"`
		Output  int64 `json:"output"`
	} `json:"limit"`
	Status string `json:"status"`
}

// FetchModels fetches GitHub Copilot models from models.dev API.
func FetchModels(ctx context.Context) ([]catwalk.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", ModelsDevURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch models: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read models response: %w", err)
	}

	// The API returns a map of provider ID -> provider data.
	var providers map[string]ModelsDevProvider
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, fmt.Errorf("failed to parse models response: %w", err)
	}

	copilotProvider, ok := providers[ProviderID]
	if !ok {
		return nil, fmt.Errorf("github-copilot provider not found in models.dev API")
	}

	return convertModels(copilotProvider.Models), nil
}

// convertModels converts models.dev models to catwalk models.
func convertModels(models map[string]ModelsDevModel) []catwalk.Model {
	result := make([]catwalk.Model, 0, len(models))

	for _, m := range models {
		// Skip deprecated models.
		if m.Status == "deprecated" {
			continue
		}

		model := catwalk.Model{
			ID:               m.ID,
			Name:             m.Name,
			SupportsImages:   m.Attachment || containsModality(m.Modalities.Input, "image"),
			DefaultMaxTokens: m.Limit.Output,
			ContextWindow:    m.Limit.Context,
			CanReason:        m.Reasoning,
		}

		// Set reasonable defaults if not provided.
		if model.DefaultMaxTokens == 0 {
			model.DefaultMaxTokens = 16384
		}
		if model.ContextWindow == 0 {
			model.ContextWindow = 128000
		}

		result = append(result, model)
	}

	return result
}

func containsModality(modalities []string, target string) bool {
	return slices.Contains(modalities, target)
}

// DefaultModels returns a set of default models if fetching from API fails.
// These are common models known to work with GitHub Copilot.
func DefaultModels() []catwalk.Model {
	return []catwalk.Model{
		{
			ID:               "gpt-4.1",
			Name:             "GPT-4.1",
			SupportsImages:   true,
			DefaultMaxTokens: 16384,
			ContextWindow:    128000,
		},
		{
			ID:               "gpt-4o",
			Name:             "GPT-4o",
			SupportsImages:   true,
			DefaultMaxTokens: 16384,
			ContextWindow:    64000,
		},
		{
			ID:               "gpt-5-mini",
			Name:             "GPT-5-mini",
			SupportsImages:   true,
			CanReason:        true,
			DefaultMaxTokens: 64000,
			ContextWindow:    128000,
		},
		{
			ID:               "grok-code-fast-1",
			Name:             "Grok Code Fast 1",
			SupportsImages:   false,
			CanReason:        true,
			DefaultMaxTokens: 64000,
			ContextWindow:    128000,
		},
	}
}

// GetModels returns Copilot models, falling back to defaults if API fetch fails.
func GetModels(ctx context.Context) []catwalk.Model {
	models, err := FetchModels(ctx)
	if err != nil {
		return DefaultModels()
	}
	if len(models) == 0 {
		return DefaultModels()
	}
	return models
}
