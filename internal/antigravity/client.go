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
		apiURL = "http://localhost:43430"
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		httpClient: &http.Client{
			Timeout: 240 * time.Second,
		},
	}
}

type QueryRequest struct {
	Prompt         string `json:"prompt"`
	Query          string `json:"query,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ResetSession   bool   `json:"reset_session,omitempty"`
}

type Response struct {
	Text           string
	ConversationID string
	Question       string
	Options        []string
}

type queryResponse struct {
	Response       string   `json:"response"`
	Text           string   `json:"text,omitempty"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Question       string   `json:"question,omitempty"`
	Options        []string `json:"options,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// Ask sends a user question to Antigravity and returns the response.
func (c *Client) Ask(ctx context.Context, prompt string, convID string, reset bool) (*Response, error) {
	reqBody, err := json.Marshal(QueryRequest{
		Prompt:         prompt,
		Query:          prompt,
		ConversationID: convID,
		ResetSession:   reset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	url := c.apiURL + "/api/ask"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("мост Antigravity (%s) недоступен: %w\nУбедитесь, что agy-pilot запущен на порту :43430", c.apiURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusOK {
		var qResp queryResponse
		if err := json.Unmarshal(body, &qResp); err == nil {
			replyText := qResp.Response
			if replyText == "" {
				replyText = qResp.Text
			}
			return &Response{
				Text:           replyText,
				ConversationID: qResp.ConversationID,
				Question:       qResp.Question,
				Options:        qResp.Options,
			}, nil
		}
		return &Response{
			Text: strings.TrimSpace(string(body)),
		}, nil
	}

	return nil, fmt.Errorf("ошибка Antigravity (HTTP %d): %s", resp.StatusCode, string(body))
}
