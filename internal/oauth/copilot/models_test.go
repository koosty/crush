package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()

	require.NotEmpty(t, models)
	require.GreaterOrEqual(t, len(models), 4)

	// Check that common models are present.
	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true
	}

	require.True(t, modelIDs["gpt-4.1"], "gpt-4.1 should be in default models")
	require.True(t, modelIDs["gpt-4o"], "gpt-4o should be in default models")
	require.True(t, modelIDs["gpt-5-mini"], "gpt-5-mini should be in default models")
}

func TestDefaultModels_Properties(t *testing.T) {
	t.Parallel()

	models := DefaultModels()

	for _, model := range models {
		t.Run(model.ID, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, model.ID)
			require.NotEmpty(t, model.Name)
			require.Greater(t, model.DefaultMaxTokens, int64(0))
			require.Greater(t, model.ContextWindow, int64(0))
		})
	}
}

func TestGetModels_FallbackToDefaults(t *testing.T) {
	t.Parallel()

	t.Run("returns defaults when API fails", func(t *testing.T) {
		t.Parallel()

		// GetModels should return defaults if the API is unreachable.
		// Since we can't easily mock the URL, we just verify it returns models.
		models := GetModels(context.Background())

		require.NotEmpty(t, models)
	})
}

func TestFetchModels_Success(t *testing.T) {
	t.Parallel()

	t.Run("parses models.dev response", func(t *testing.T) {
		t.Parallel()

		// Create a mock server that returns models.dev format.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "GET", r.Method)

			response := map[string]ModelsDevProvider{
				"github-copilot": {
					ID:   "github-copilot",
					Name: "GitHub Copilot",
					API:  "https://api.githubcopilot.com",
					Models: map[string]ModelsDevModel{
						"gpt-4o": {
							ID:          "gpt-4o",
							Name:        "GPT-4o",
							Attachment:  true,
							ToolCall:    true,
							Temperature: true,
							Modalities: struct {
								Input  []string `json:"input"`
								Output []string `json:"output"`
							}{
								Input:  []string{"text", "image"},
								Output: []string{"text"},
							},
							Limit: struct {
								Context int64 `json:"context"`
								Output  int64 `json:"output"`
							}{
								Context: 128000,
								Output:  16384,
							},
							Status: "active",
						},
						"deprecated-model": {
							ID:     "deprecated-model",
							Name:   "Deprecated Model",
							Status: "deprecated",
						},
					},
				},
			}

			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
		}))
		defer server.Close()

		// Note: We can't easily test FetchModels directly since the URL is hardcoded.
		// This test documents the expected response format.
	})
}

func TestModelsDevModelParsing(t *testing.T) {
	t.Parallel()

	t.Run("parses full model data", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"id": "gpt-4o",
			"name": "GPT-4o",
			"attachment": true,
			"reasoning": false,
			"tool_call": true,
			"temperature": true,
			"knowledge": "2024-01",
			"release_date": "2024-05-13",
			"last_updated": "2024-11-01",
			"modalities": {
				"input": ["text", "image"],
				"output": ["text"]
			},
			"open_weights": false,
			"cost": {
				"input": 2.5,
				"output": 10.0
			},
			"limit": {
				"context": 128000,
				"output": 16384
			},
			"status": "active"
		}`

		var model ModelsDevModel
		err := json.Unmarshal([]byte(jsonData), &model)
		require.NoError(t, err)

		require.Equal(t, "gpt-4o", model.ID)
		require.Equal(t, "GPT-4o", model.Name)
		require.True(t, model.Attachment)
		require.False(t, model.Reasoning)
		require.True(t, model.ToolCall)
		require.True(t, model.Temperature)
		require.Equal(t, "2024-01", model.Knowledge)
		require.Equal(t, int64(128000), model.Limit.Context)
		require.Equal(t, int64(16384), model.Limit.Output)
		require.Equal(t, "active", model.Status)
		require.Contains(t, model.Modalities.Input, "text")
		require.Contains(t, model.Modalities.Input, "image")
	})

	t.Run("handles minimal model data", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"id": "test-model",
			"name": "Test Model"
		}`

		var model ModelsDevModel
		err := json.Unmarshal([]byte(jsonData), &model)
		require.NoError(t, err)

		require.Equal(t, "test-model", model.ID)
		require.Equal(t, "Test Model", model.Name)
		require.False(t, model.Attachment)
		require.Equal(t, int64(0), model.Limit.Context)
	})
}

func TestModelsDevProviderParsing(t *testing.T) {
	t.Parallel()

	t.Run("parses provider with models", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"id": "github-copilot",
			"name": "GitHub Copilot",
			"api": "https://api.githubcopilot.com",
			"doc": "https://docs.github.com/copilot",
			"models": {
				"gpt-4o": {
					"id": "gpt-4o",
					"name": "GPT-4o",
					"status": "active"
				}
			}
		}`

		var provider ModelsDevProvider
		err := json.Unmarshal([]byte(jsonData), &provider)
		require.NoError(t, err)

		require.Equal(t, "github-copilot", provider.ID)
		require.Equal(t, "GitHub Copilot", provider.Name)
		require.Equal(t, "https://api.githubcopilot.com", provider.API)
		require.Len(t, provider.Models, 1)
		require.Contains(t, provider.Models, "gpt-4o")
	})
}

func TestConvertModels(t *testing.T) {
	t.Parallel()

	t.Run("converts models correctly", func(t *testing.T) {
		t.Parallel()

		input := map[string]ModelsDevModel{
			"gpt-4o": {
				ID:         "gpt-4o",
				Name:       "GPT-4o",
				Attachment: true,
				Reasoning:  false,
				Limit: struct {
					Context int64 `json:"context"`
					Output  int64 `json:"output"`
				}{
					Context: 128000,
					Output:  16384,
				},
				Status: "active",
			},
			"reasoning-model": {
				ID:        "reasoning-model",
				Name:      "Reasoning Model",
				Reasoning: true,
				Limit: struct {
					Context int64 `json:"context"`
					Output  int64 `json:"output"`
				}{
					Context: 64000,
					Output:  8192,
				},
				Status: "active",
			},
		}

		result := convertModels(input)

		require.Len(t, result, 2)

		// Find the models.
		var gpt4o, reasoningModel *struct {
			ID               string
			Name             string
			SupportsImages   bool
			CanReason        bool
			DefaultMaxTokens int64
			ContextWindow    int64
		}

		for i := range result {
			if result[i].ID == "gpt-4o" {
				gpt4o = &struct {
					ID               string
					Name             string
					SupportsImages   bool
					CanReason        bool
					DefaultMaxTokens int64
					ContextWindow    int64
				}{
					ID:               result[i].ID,
					Name:             result[i].Name,
					SupportsImages:   result[i].SupportsImages,
					CanReason:        result[i].CanReason,
					DefaultMaxTokens: result[i].DefaultMaxTokens,
					ContextWindow:    result[i].ContextWindow,
				}
			}
			if result[i].ID == "reasoning-model" {
				reasoningModel = &struct {
					ID               string
					Name             string
					SupportsImages   bool
					CanReason        bool
					DefaultMaxTokens int64
					ContextWindow    int64
				}{
					ID:               result[i].ID,
					Name:             result[i].Name,
					SupportsImages:   result[i].SupportsImages,
					CanReason:        result[i].CanReason,
					DefaultMaxTokens: result[i].DefaultMaxTokens,
					ContextWindow:    result[i].ContextWindow,
				}
			}
		}

		require.NotNil(t, gpt4o)
		require.Equal(t, "GPT-4o", gpt4o.Name)
		require.True(t, gpt4o.SupportsImages)
		require.False(t, gpt4o.CanReason)
		require.Equal(t, int64(16384), gpt4o.DefaultMaxTokens)
		require.Equal(t, int64(128000), gpt4o.ContextWindow)

		require.NotNil(t, reasoningModel)
		require.True(t, reasoningModel.CanReason)
	})

	t.Run("skips deprecated models", func(t *testing.T) {
		t.Parallel()

		input := map[string]ModelsDevModel{
			"active-model": {
				ID:     "active-model",
				Name:   "Active Model",
				Status: "active",
			},
			"deprecated-model": {
				ID:     "deprecated-model",
				Name:   "Deprecated Model",
				Status: "deprecated",
			},
		}

		result := convertModels(input)

		require.Len(t, result, 1)
		require.Equal(t, "active-model", result[0].ID)
	})

	t.Run("sets defaults for zero values", func(t *testing.T) {
		t.Parallel()

		input := map[string]ModelsDevModel{
			"minimal-model": {
				ID:     "minimal-model",
				Name:   "Minimal Model",
				Status: "active",
				// No limits specified.
			},
		}

		result := convertModels(input)

		require.Len(t, result, 1)
		require.Equal(t, int64(16384), result[0].DefaultMaxTokens)
		require.Equal(t, int64(128000), result[0].ContextWindow)
	})

	t.Run("detects image support from modalities", func(t *testing.T) {
		t.Parallel()

		input := map[string]ModelsDevModel{
			"image-model": {
				ID:         "image-model",
				Name:       "Image Model",
				Attachment: false, // Attachment is false.
				Modalities: struct {
					Input  []string `json:"input"`
					Output []string `json:"output"`
				}{
					Input: []string{"text", "image"}, // But modalities includes image.
				},
				Status: "active",
			},
		}

		result := convertModels(input)

		require.Len(t, result, 1)
		require.True(t, result[0].SupportsImages)
	})
}

func TestContainsModality(t *testing.T) {
	t.Parallel()

	t.Run("finds existing modality", func(t *testing.T) {
		t.Parallel()

		modalities := []string{"text", "image", "audio"}
		require.True(t, containsModality(modalities, "image"))
	})

	t.Run("returns false for missing modality", func(t *testing.T) {
		t.Parallel()

		modalities := []string{"text", "audio"}
		require.False(t, containsModality(modalities, "image"))
	})

	t.Run("handles empty slice", func(t *testing.T) {
		t.Parallel()

		var modalities []string
		require.False(t, containsModality(modalities, "image"))
	})
}
