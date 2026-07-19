package redteam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// llm_openai.go is the REAL LLMClient: an OpenAI-compatible chat client that
// backs the planner with a live model (OpenAI, DashScope, Ollama, vLLM, or the
// platform's self-hosted model scheduled on pkg/scheduler GPUs). It reports real
// when an endpoint+model are configured, else simulated - honest by construction.

// OpenAICompatClient calls an OpenAI-compatible /v1/chat/completions endpoint.
type OpenAICompatClient struct {
	BaseURL    string // e.g. https://api.openai.com, http://localhost:11434 (ollama), vLLM
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// NewOpenAICompatClient builds a client. A real endpoint+model yields real mode.
func NewOpenAICompatClient(baseURL, apiKey, model string) *OpenAICompatClient {
	return &OpenAICompatClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Mode reports real when an endpoint and model are configured, else simulated.
func (c *OpenAICompatClient) Mode() capability.Mode {
	if c.BaseURL != "" && c.Model != "" {
		return capability.ModeReal
	}
	return capability.ModeSimulated
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Complete sends the prompt as a single user message and returns the assistant
// content. Deterministic sampling (temperature 0) for reproducible planning.
func (c *OpenAICompatClient) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
		Temperature: 0,
	})
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("redteam: llm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("redteam: llm status %d: %s", resp.StatusCode, bounded(string(body), 200))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("redteam: decode llm response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("redteam: llm returned no choices")
	}
	return cr.Choices[0].Message.Content, nil
}
