package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ModelSelector struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ModelRecommendation struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Confidence float64 `json:"confidence"`
}

type selectModelRequest struct {
	Messages  []string `json:"messages"`
	Providers []string `json:"providers"`
	Tradeoff  string   `json:"tradeoff"`
}

type selectModelResponse struct {
	Provider   string  `json:"provider"`
	Model      string  `json:"model"`
	Confidence float64 `json:"confidence"`
}

func NewModelSelector(baseURL string, apiKey string) *ModelSelector {
	return &ModelSelector{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (ms *ModelSelector) SelectModel(ctx context.Context, messages []Message, providers []string, tradeoff string) (*ModelRecommendation, error) {
	messageContents := make([]string, len(messages))
	for i, msg := range messages {
		messageContents[i] = msg.Content
	}

	reqBody := selectModelRequest{
		Messages:  messageContents,
		Providers: providers,
		Tradeoff:  tradeoff,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v2/modelRouter/modelSelect", ms.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if ms.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+ms.apiKey)
	}

	resp, err := ms.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var respBody selectModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &ModelRecommendation{
		Provider:   respBody.Provider,
		Model:      respBody.Model,
		Confidence: respBody.Confidence,
	}, nil
}
