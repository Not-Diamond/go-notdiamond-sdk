package notdiamond

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	http_client "github.com/Not-Diamond/go-notdiamond/pkg/http/client"
	"github.com/Not-Diamond/go-notdiamond/pkg/model"
	"github.com/Not-Diamond/go-notdiamond/pkg/redis"
	"github.com/alicebob/miniredis/v2"
)

func TestInit(t *testing.T) {
	// Set up miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tests := []struct {
		name      string
		config    model.Config
		wantErr   bool
		errString string
	}{
		{
			name: "valid ordered models config",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"openai/gpt-4"},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
		{
			name: "valid weighted models config",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.WeightedModels{
					"openai/gpt-4": 1.0,
				},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid - no clients",
			config: model.Config{
				Models: model.OrderedModels{"openai/gpt-4"},
			},
			wantErr:   true,
			errString: "at least one client must be provided",
		},
		{
			name: "invalid - no models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: nil,
			},
			wantErr:   true,
			errString: "models must be either notdiamond.OrderedModels or map[string]float64, got <nil>",
		},
		{
			name: "invalid - incorrect model format",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"invalid-model-format"},
			},
			wantErr:   true,
			errString: "invalid model format: invalid-model-format (expected 'provider/model' or 'provider/model/region')",
		},
		{
			name: "invalid - unknown provider",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"unknown/gpt-4"},
			},
			wantErr:   true,
			errString: "invalid provider in model unknown/gpt-4: unknown provider: unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Init(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errString != "" && !strings.Contains(err.Error(), tt.errString) {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.errString)
				return
			}
			if err == nil {
				if client == nil {
					t.Error("Init() returned nil client with no error")
					return
				}
				// Clean up
				if err := client.HttpClient.MetricsTracker.Close(); err != nil {
					t.Errorf("Failed to close metrics tracker: %v", err)
				}
			}
		})
	}
}

func TestClientKey(t *testing.T) {
	// Test that ClientKey returns the expected value
	key := ClientKey()

	// Verify that the key matches http_client.ClientKey
	if key != http_client.ClientKey {
		t.Errorf("ClientKey() = %v, want %v", key, http_client.ClientKey)
	}
}

func TestInitWithRegion(t *testing.T) {
	// Set up miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tests := []struct {
		name      string
		config    model.Config
		wantErr   bool
		errString string
	}{
		{
			name: "valid config with region in ordered models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"openai/gpt-4/us-east1"},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with region in weighted models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.WeightedModels{
					"openai/gpt-4/us-east1": 1.0,
				},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple regions in ordered models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"openai/gpt-4/us-east1", "vertex/gemini-pro/us-central1"},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple regions in weighted models",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.WeightedModels{
					"openai/gpt-4/us-east1":         0.7,
					"vertex/gemini-pro/us-central1": 0.3,
				},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Init(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errString != "" && !strings.Contains(err.Error(), tt.errString) {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.errString)
				return
			}
			if err == nil {
				if client == nil {
					t.Error("Init() returned nil client with no error")
					return
				}

				// Test modelProviders map to ensure region handling is correct
				if len(client.ModelProviders) == 0 {
					t.Error("Init() returned client with empty modelProviders map")
					return
				}

				// Clean up
				if err := client.HttpClient.MetricsTracker.Close(); err != nil {
					t.Errorf("Failed to close metrics tracker: %v", err)
				}
			}
		})
	}
}

func TestInitHttpClientError(t *testing.T) {
	// We need to use the real implementation but with invalid Redis config to trigger errors

	tests := []struct {
		name      string
		config    model.Config
		wantErr   bool
		errString string
	}{
		{
			name: "invalid redis config",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{"openai/gpt-4"},
				// Invalid Redis config (no server running at this address)
				RedisConfig: &redis.Config{
					Addr:     "localhost:63999", // Using a port that's likely not in use
					Password: "",
					DB:       0,
				},
			},
			wantErr: true,
		},
		{
			name: "valid vertex AI config",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models:          model.OrderedModels{"openai/gpt-4"},
				VertexProjectID: "test-project",
				VertexLocation:  "us-central1",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Init(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && client == nil {
				t.Error("Init() returned nil client with no error")
				return
			}
			if err == nil {
				// Clean up
				if err := client.HttpClient.MetricsTracker.Close(); err != nil {
					t.Errorf("Failed to close metrics tracker: %v", err)
				}
			}
		})
	}
}

func TestInitModelProviders(t *testing.T) {
	// Set up miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}
	defer mr.Close()

	tests := []struct {
		name           string
		config         model.Config
		wantErr        bool
		errString      string
		checkProviders map[string]map[string]bool
	}{
		{
			name: "multiple providers for same model",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.OrderedModels{
					"openai/gpt-4",
					"azure/gpt-4",
				},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
			checkProviders: map[string]map[string]bool{
				"gpt-4": {
					"openai": true,
					"azure":  true,
				},
			},
		},
		{
			name: "multiple models with regions",
			config: model.Config{
				Clients: []http.Request{
					{
						Host: "api.openai.com",
						URL: &url.URL{
							Scheme: "https",
							Host:   "api.openai.com",
							Path:   "/v1/chat/completions",
						},
					},
				},
				Models: model.WeightedModels{
					"openai/gpt-4/us-east1": 0.5,
					"azure/gpt-4/eastus":    0.5,
				},
				RedisConfig: &redis.Config{
					Addr:     mr.Addr(),
					Password: "",
					DB:       0,
				},
			},
			wantErr: false,
			checkProviders: map[string]map[string]bool{
				"gpt-4": {
					"openai": true,
					"azure":  true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := Init(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errString != "" && !strings.Contains(err.Error(), tt.errString) {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.errString)
				return
			}
			if err == nil {
				if client == nil {
					t.Error("Init() returned nil client with no error")
					return
				}

				// Check that ModelProviders map is correctly populated
				if tt.checkProviders != nil {
					for model, expectedProviders := range tt.checkProviders {
						providers, ok := client.ModelProviders[model]
						if !ok {
							t.Errorf("Model %s not found in ModelProviders map", model)
							continue
						}

						for provider, expected := range expectedProviders {
							if providers[provider] != expected {
								t.Errorf("ModelProviders[%s][%s] = %v, want %v",
									model, provider, providers[provider], expected)
							}
						}
					}
				}

				// Clean up
				if err := client.HttpClient.MetricsTracker.Close(); err != nil {
					t.Errorf("Failed to close metrics tracker: %v", err)
				}
			}
		})
	}
}
