package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SystemDirective instructs the AI model to reply immediately with direct text
// and refrain from invoking interactive prompt tools like ask_question.
const SystemDirective = "Отвечай сразу готовым текстом. Не используй инструменты, требующие клика мышкой (ask_question). Выдавай прямой результат."

// Client handles communication with Antigravity / agy-pilot bridge.
type Client struct {
	apiURL     string
	httpClient *http.Client
}

// NewClient creates a new Antigravity API client.
func NewClient(apiURL string) *Client {
	if apiURL == "" {
		apiURL = "http://localhost:43430"
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // 5 minutes timeout for deep agent reasoning & long tasks
		},
	}
}

type queryRequest struct {
	Prompt string `json:"prompt"`
	Query  string `json:"query,omitempty"`
}

type queryResponse struct {
	Response string `json:"response"`
	Text     string `json:"text,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Ask sends a user question to Antigravity and returns the response.
func (c *Client) Ask(ctx context.Context, prompt string) (string, error) {
	// Prepend system directive to prevent interactive widgets (ask_question)
	fullPrompt := prompt
	if !strings.Contains(prompt, SystemDirective) {
		fullPrompt = fmt.Sprintf("[%s]\n\n%s", SystemDirective, prompt)
	}

	reqBody, err := json.Marshal(queryRequest{
		Prompt: fullPrompt,
		Query:  fullPrompt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode request: %w", err)
	}

	endpoints := []string{"/query", "/api/ask", "/prompt", "/v1/chat/completions"}
	var lastErr error

	for _, ep := range endpoints {
		url := c.apiURL + ep
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
		if err != nil {
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var qResp queryResponse
			if err := json.Unmarshal(body, &qResp); err == nil {
				if qResp.Response != "" {
					return qResp.Response, nil
				}
				if qResp.Text != "" {
					return qResp.Text, nil
				}
				if qResp.Message != "" {
					return qResp.Message, nil
				}
			}
			// Fallback to raw string body
			return strings.TrimSpace(string(body)), nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("мост Antigravity (%s) недоступен: %w\nУбедитесь, что agy-pilot или агент запущены на порту %s", c.apiURL, lastErr, c.apiURL)
	}

	return "", fmt.Errorf("не удалось получить ответ от Antigravity на %s", c.apiURL)
}

