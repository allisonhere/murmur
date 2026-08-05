package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/alliebayless/murmur/internal/config"
)

// OpenAI talks to any OpenAI-compatible /chat/completions endpoint, which
// covers OpenAI itself plus most local and hosted gateways.
type OpenAI struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewOpenAI builds an OpenAI-compatible classifier. A missing API key is only
// an error when the endpoint is a public one; local gateways often need none.
func NewOpenAI(cfg config.Config) (*OpenAI, error) {
	base := strings.TrimRight(cfg.AI.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	m := cfg.AI.Model
	if m == "" {
		m = "gpt-4o-mini"
	}
	key := cfg.APIKey()
	if key == "" && strings.Contains(base, "api.openai.com") {
		env := cfg.AI.APIKeyEnv
		if env == "" {
			env = "MURMUR_API_KEY"
		}
		return nil, errors.New("no API key found in $" + env + "; export it, point ai.base_url at a local endpoint, or set ai.provider to none")
	}
	return &OpenAI{baseURL: base, model: m, apiKey: key, client: newClient(cfg)}, nil
}

// Name implements Classifier.
func (o *OpenAI) Name() string { return "openai (" + o.model + ")" }

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	Temperature    float64         `json:"temperature"`
	ResponseFormat map[string]any  `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Classify implements Classifier.
func (o *OpenAI) Classify(ctx context.Context, req ClassificationRequest) (ClassificationResult, error) {
	payload := openAIRequest{
		Model:       o.model,
		Temperature: 0.1,
		Messages: []openAIMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: userPrompt(req)},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
	}
	headers := map[string]string{}
	if o.apiKey != "" {
		headers["Authorization"] = "Bearer " + o.apiKey
	}

	var resp openAIResponse
	if err := postJSON(ctx, o.client, o.baseURL+"/chat/completions", headers, payload, &resp); err != nil {
		return ClassificationResult{}, err
	}
	if resp.Error != nil {
		return ClassificationResult{}, &ProviderError{Msg: "AI provider error: " + resp.Error.Message}
	}
	if len(resp.Choices) == 0 {
		return ClassificationResult{}, errors.New("AI provider returned no choices")
	}
	return ParseResult(resp.Choices[0].Message.Content)
}
