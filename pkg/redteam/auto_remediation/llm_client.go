package auto_remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/hashicorp/go-retryablehttp"
)

// Errors
var (
	ErrNoAPIKey           = errors.New("API key not configured")
	ErrAllProvidersFailed = errors.New("all LLM providers failed")
	ErrInvalidResponse    = errors.New("invalid LLM response format")
	ErrRateLimit          = errors.New("rate limit exceeded")
)

// LLMProvider defines supported LLM backends
type LLMProvider string

const (
	ProviderDashScope  LLMProvider = "dashscope"   // Alibaba Cloud Qwen
	ProviderOpenAI     LLMProvider = "openai"      // OpenAI-compatible (DeepSeek fallback)
	ProviderOllama     LLMProvider = "ollama"      // Local Ollama
	ProviderVLLM       LLMProvider = "vllm"        // vLLM serving
)

// ModelConfig holds model-specific settings
type ModelConfig struct {
	Name           string
	MaxTokens      int
	Temperature    float64
	TimeoutSeconds int
}

// Default model configurations
var (
	DashScopeDefault = ModelConfig{
		Name:           "qwen3.5-256b",
		MaxTokens:      8192,
		Temperature:    0.7,
		TimeoutSeconds: 60,
	}
	OpenAIDefault = ModelConfig{
		Name:           "deepseek-chat",
		MaxTokens:      8192,
		Temperature:    0.7,
		TimeoutSeconds: 60,
	}
)

// LLMClient interface for LLM interactions
type LLMClient interface {
	// Complete sends a text prompt and returns raw text completion
	Complete(ctx context.Context, prompt string, options *CompletionOptions) (string, error)
	
	// Chat sends messages and returns chat completion
	Chat(ctx context.Context, messages []Message, options *CompletionOptions) (string, error)
	
	// ChatJSON sends messages and parses JSON response
	ChatJSON(ctx context.Context, messages []Message, outputSchema any, options *CompletionOptions) (any, error)
	
	// Health checks if any provider is available
	Health(ctx context.Context) bool
	
	// LastProvider returns which provider was last used successfully
	LastProvider() string
	
	// Mode returns the capability mode
	Mode() capability.Mode
}

// CompletionOptions configures an LLM request
type CompletionOptions struct {
	Model         string
	MaxTokens     int
	Temperature   float64
	TopP          float64
	Stream        bool
	ResponseFormat ResponseFormat
	RetryAttempts int
	TimeoutSecs   int
}

// ResponseFormat controls structured output
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *JSONSchemaDef  `json:"json_schema,omitempty"`
}

// JSONSchemaDef defines schema for structured output
type JSONSchemaDef struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Required    []string      `json:"required"`
}

// Message represents a chat conversation turn
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ProviderStatus tracks health of each provider
type ProviderStatus struct {
	Provider  LLMProvider
	Available bool
	LastError error
	LastUsed  time.Time
}

// LLMClientImpl implements LLMClient with multi-provider support
type LLMClientImpl struct {
	config      *LLMConfig
	httpClient  *http.Client
	mu          sync.RWMutex
	status      map[LLMProvider]*ProviderStatus
	lastSuccess LLMProvider
	mode        capability.Mode
}

// LLMConfig holds all provider configurations
type LLMConfig struct {
	DashScope  ProviderConfig
	OpenAI     ProviderConfig
	Ollama     ProviderConfig
	VLLM       ProviderConfig
	Priority   []LLMProvider
	EnableAuth bool
	CircuitBreaker *CircuitBreakerConfig
}

// ProviderConfig holds credentials per provider
type ProviderConfig struct {
	BaseURL string
	APIKey  string
	Model   ModelConfig
	Enabled bool
}

// CircuitBreakerConfig prevents cascading failures
type CircuitBreakerConfig struct {
	MaxFailures     int
	ResetTimeout    time.Duration
	TimeoutWindow   time.Duration
}

// NewDefaultConfig loads config from environment variables
func NewDefaultConfig() *LLMConfig {
	return &LLMConfig{
		DashScope: ProviderConfig{
			BaseURL: getEnv("DASHSCOPE_API_BASE", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
			APIKey:  getEnv("DASHSCOPE_API_KEY", ""),
			Model:   DashScopeDefault,
			Enabled: getEnv("DASHSCOPE_API_KEY", "") != "",
		},
		OpenAI: ProviderConfig{
			BaseURL: getEnv("OPENAI_API_BASE", "https://api.deepseek.com/v1"),
			APIKey:  getEnv("OPENAI_API_KEY", ""),
			Model:   OpenAIDefault,
			Enabled: getEnv("OPENAI_API_KEY", "") != "",
		},
		Priority: []LLMProvider{ProviderDashScope, ProviderOpenAI},
		CircuitBreaker: &CircuitBreakerConfig{
			MaxFailures:  3,
			ResetTimeout: time.Minute * 5,
			TimeoutWindow: time.Minute * 10,
		},
	}
}

// NewLLMClient creates a new multi-provider LLM client
func NewLLMClient(config *LLMConfig, capMode capability.Mode) (*LLMClientImpl, error) {
	if config == nil {
		config = NewDefaultConfig()
	}
	
	// Build retryable HTTP client
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Timeout = time.Second * 120
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = time.Second * 2
	retryClient.RetryWaitMax = time.Second * 10
	
	client := &LLMClientImpl{
		config:   config,
		httpClient: retryClient.StandardClient(),
		status:   make(map[LLMProvider]*ProviderStatus),
		mode:     capMode,
	}
	
	// Initialize status for all providers
	for _, provider := range config.Priority {
		client.status[provider] = &ProviderStatus{
			Provider: provider,
			Available: false,
		}
	}
	
	return client, nil
}

// Complete implements LLMClient
func (c *LLMClientImpl) Complete(ctx context.Context, prompt string, opts *CompletionOptions) (string, error) {
	if opts == nil {
		opts = &CompletionOptions{}
	}
	
	messages := []Message{
		{Role: "user", Content: prompt},
	}
	
	return c.Chat(ctx, messages, opts)
}

// Chat implements LLMClient
func (c *LLMClientImpl) Chat(ctx context.Context, messages []Message, opts *CompletionOptions) (string, error) {
	if len(messages) == 0 {
		return "", errors.New("messages list cannot be empty")
	}
	
	// Use default options
	if opts == nil {
		opts = &CompletionOptions{}
	}
	
	var selectedProvider LLMProvider
	var err error
	
	// Try providers in priority order
	for _, provider := range c.config.Priority {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			selectedProvider = provider
			result, err := c.callProvider(ctx, provider, messages, opts)
			if err == nil {
				c.mu.Lock()
				c.lastSuccess = provider
				if c.status[provider] != nil {
					c.status[provider].Available = true
					c.status[provider].LastError = nil
					c.status[provider].LastUsed = time.Now()
				}
				c.mu.Unlock()
				return result, nil
			}
			
			// Log failure but continue trying next provider
			logger.Debugf("Provider %s failed: %v", provider, err)
			
			// Mark as unavailable
			c.mu.Lock()
			if c.status[provider] != nil {
				c.status[provider].LastError = err
				c.status[provider].Available = false
			}
			c.mu.Unlock()
		}
	}
	
	// All providers failed
	return "", fmt.Errorf("%w: tried %v", ErrAllProvidersFailed, c.config.Priority)
}

// ChatJSON implements LLMClient with structured output parsing
func (c *LLMClientImpl) ChatJSON(ctx context.Context, messages []Message, outputSchema any, opts *CompletionOptions) (any, error) {
	// Set response format for JSON mode
	if opts == nil {
		opts = &CompletionOptions{}
	}
	
	jsonSchema, ok := outputSchema.(*JSONSchemaDef)
	if !ok && outputSchema != nil {
		schemaBytes, _ := json.Marshal(outputSchema)
		jsonSchema = &JSONSchemaDef{
			Schema: schemaBytes,
		}
	}
	
	if jsonSchema != nil {
		opts.ResponseFormat = ResponseFormat{
			Type:       "json_object",
			JSONSchema: jsonSchema,
		}
	}
	
	text, err := c.Chat(ctx, messages, opts)
	if err != nil {
		return nil, err
	}
	
	// Parse JSON response with multiple strategies
	return c.parseJSONResponse(text)
}

// parseJSONResponse extracts JSON from LLM response
func (c *LLMClientImpl) parseJSONResponse(text string) (any, error) {
	if text == "" {
		return nil, ErrInvalidResponse
	}
	
	cleaned := trimWhitespace(text)
	
	// Strategy 1: Direct parse
	result, err := tryParseJSON(cleaned)
	if err == nil {
		return result, nil
	}
	
	// Strategy 2: Extract from code blocks
	match := extractCodeBlock(cleaned)
	if match != "" {
		result, err := tryParseJSON(match)
		if err == nil {
			return result, nil
		}
	}
	
	// Strategy 3: Find first JSON object
	result = findJSONObject(cleaned)
	if result != nil {
		return result, nil
	}
	
	return nil, fmt.Errorf("%w: could not extract JSON from: %s", ErrInvalidResponse, truncateString(text, 200))
}

// Health checks overall availability
func (c *LLMClientImpl) Health(ctx context.Context) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	for _, provider := range c.config.Priority {
		if status := c.status[provider]; status != nil && status.Available {
			return true
		}
	}
	return false
}

// LastProvider returns the last successful provider
func (c *LLMClientImpl) LastProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastSuccess != "" {
		return string(c.lastSuccess)
	}
	return "unknown"
}

// Mode returns capability mode
func (c *LLMClientImpl) Mode() capability.Mode {
	return c.mode
}

// callProvider handles actual API call to a specific provider
func (c *LLMClientImpl) callProvider(ctx context.Context, provider LLMProvider, messages []Message, opts *CompletionOptions) (string, error) {
	cfg, err := c.getProviderConfig(provider)
	if err != nil {
		return "", err
	}
	
	// Check circuit breaker
	if c.config.CircuitBreaker != nil {
		if shouldFailover := checkCircuitBreaker(provider, c.config.CircuitBreaker); shouldFailover {
			logger.Debugf("Circuit breaker open for %s, skipping", provider)
			return "", ErrRateLimit
		}
	}
	
	url := fmt.Sprintf("%s/chat/completions", cfg.BaseURL)
	
	// Build request payload
	payload := buildChatPayload(messages, cfg.Model, opts)
	
	// Prepare request
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, io.NopCloser(bytes.NewReader(reqBody)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	}
	
	// Set timeout
	timeout := time.Second * time.Duration(opts.TimeoutSecs)
	if timeout <= 0 {
		timeout = time.Second * 60
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		recordFailure(provider, c.config.CircuitBreaker)
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	// Handle response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		recordFailure(provider, c.config.CircuitBreaker)
		
		// Check for rate limiting
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", ErrRateLimit
		}
		
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateString(string(body), 500))
	}
	
	// Parse response
	var response ChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	
	if len(response.Choices) == 0 {
		return "", errors.New("no choices in response")
	}
	
	choice := response.Choices[0]
	if choice.Message.Content == "" {
		return "", errors.New("empty content in response")
	}
	
	// Record success
	recordSuccess(provider)
	
	return choice.Message.Content, nil
}

// Helper functions

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func trimWhitespace(s string) string {
	return strings.TrimSpace(s)
}

func tryParseJSON(s string) (any, error) {
	var result any
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func extractCodeBlock(text string) string {
	start := strings.Index(text, "```json")
	if start == -1 {
		start = strings.Index(text, "```JSON")
	}
	if start == -1 {
		start = strings.Index(text, "```")
	}
	
	if start == -1 {
		return ""
	}
	
	start += 3
	end := strings.Index(text[start:], "```")
	if end == -1 {
		return ""
	}
	
	return text[start : start+end]
}

func findJSONObject(text string) any {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	
	if start == -1 || end == -1 || end <= start {
		return nil
	}
	
	jsonStr := text[start : end+1]
	result, _ := tryParseJSON(jsonStr)
	return result
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// ChatResponse represents OpenAI-compatible API response
type ChatResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []Choice      `json:"choices"`
	Usage   Usage         `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	LogProbs     any     `json:"logprobs"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Provider-specific implementations would continue here
// Due to space constraints, showing core architecture only