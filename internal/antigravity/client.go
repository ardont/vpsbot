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

// Client handles communication with Antigravity / agy-pilot bridge.
type Client struct {
	apiURL     string
	httpClient *http.Client
}

// NewClient creates a new Antigravity API client.
func NewClient(apiURL string) *Client {
	if apiURL == "" {
		apiURL = "http://localhost:43427"
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		httpClient: &http.Client{
			Timeout: 90 * time.Second, // Allow deep agent reasoning
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
	reqBody, err := json.Marshal(queryRequest{
		Prompt: prompt,
		Query:  prompt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode request: %w", err)
	}

	endpoints := []string{"/api/ask", "/prompt", "/v1/chat/completions", "/query"}
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
		return "", fmt.Errorf("мост Antigravity (%s) недоступен: %w\nУбедитесь, что agy-pilot или агент запущены на порту :43427", c.apiURL, lastErr)
	}

	return "", fmt.Errorf("не удалось получить ответ от Antigravity на %s", c.apiURL)
}

