package response

import (
	"encoding/json"
	"fmt"
	"time"
)

type Result struct {
	Model     string
	Response  string
	TimeTaken time.Duration
}

// Parse takes a response body and the time the request started,
// and returns the parsed result from either OpenAI or Vertex AI
func Parse(body []byte, startTime time.Time) (*Result, error) {
	// Try to parse as OpenAI response first
	var openaiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &openaiResponse); err == nil && len(openaiResponse.Choices) > 0 {
		return &Result{
			Model:     "gpt-4o",
			Response:  openaiResponse.Choices[0].Message.Content,
			TimeTaken: time.Since(startTime),
		}, nil
	}

	// If not OpenAI, try to parse as Vertex AI response
	var vertexResponse struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			CitationMetadata *struct {
				Citations []struct {
					StartIndex int `json:"startIndex"`
					EndIndex   int `json:"endIndex"`
				} `json:"citations"`
			} `json:"citationMetadata"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &vertexResponse); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if len(vertexResponse.Candidates) == 0 {
		return nil, fmt.Errorf("response did not contain any candidates: %s", string(body))
	}

	candidate := vertexResponse.Candidates[0]

	// Check if the response was blocked due to recitation
	if candidate.FinishReason == "RECITATION" {
		return nil, fmt.Errorf("response was blocked due to content recitation. Please rephrase your query")
	}

	// Check if we have valid content
	if len(candidate.Content.Parts) == 0 {
		return nil, fmt.Errorf("response candidate did not contain any content parts: %s", string(body))
	}

	return &Result{
		Model:     "gemini-pro",
		Response:  candidate.Content.Parts[0].Text,
		TimeTaken: time.Since(startTime),
	}, nil
}
