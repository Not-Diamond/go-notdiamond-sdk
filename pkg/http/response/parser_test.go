package response

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		responseBody  string
		expectedModel string
		expectedText  string
		expectError   bool
		errorContains string
	}{
		{
			name: "valid OpenAI response",
			responseBody: `{
				"choices": [
					{
						"message": {
							"content": "I am GPT-4"
						}
					}
				]
			}`,
			expectedModel: "gpt-4o",
			expectedText:  "I am GPT-4",
			expectError:   false,
		},
		{
			name: "valid Vertex AI response",
			responseBody: `{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"text": "I am Gemini Pro"
								}
							]
						}
					}
				]
			}`,
			expectedModel: "gemini-pro",
			expectedText:  "I am Gemini Pro",
			expectError:   false,
		},
		{
			name:          "invalid JSON response",
			responseBody:  `{"invalid": json`,
			expectError:   true,
			errorContains: "failed to parse response",
		},
		{
			name:          "empty response",
			responseBody:  `{}`,
			expectError:   true,
			errorContains: "response did not contain any candidates",
		},
		{
			name: "OpenAI response with empty choices",
			responseBody: `{
				"choices": []
			}`,
			expectError:   true,
			errorContains: "response did not contain any candidates",
		},
		{
			name: "Vertex response with empty candidates",
			responseBody: `{
				"candidates": []
			}`,
			expectError:   true,
			errorContains: "response did not contain any candidates",
		},
		{
			name: "Vertex response with recitation",
			responseBody: `{
				"candidates": [
					{
						"content": {},
						"finishReason": "RECITATION"
					}
				]
			}`,
			expectError:   true,
			errorContains: "response was blocked due to content recitation",
		},
		{
			name: "Vertex response with empty content parts",
			responseBody: `{
				"candidates": [
					{
						"content": {
							"parts": []
						}
					}
				]
			}`,
			expectError:   true,
			errorContains: "response candidate did not contain any content parts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := time.Now()
			result, err := Parse([]byte(tt.responseBody), startTime)

			// Check error cases
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error containing %q, got %q", tt.errorContains, err.Error())
				}
				return
			}

			// Check success cases
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Fatal("expected result but got nil")
			}

			if result.Model != tt.expectedModel {
				t.Errorf("expected model %q, got %q", tt.expectedModel, result.Model)
			}

			if result.Response != tt.expectedText {
				t.Errorf("expected response text %q, got %q", tt.expectedText, result.Response)
			}

			if result.TimeTaken <= 0 {
				t.Error("expected positive duration")
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}
