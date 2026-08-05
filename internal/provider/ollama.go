package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/alliebayless/murmur/internal/config"
)

// Ollama talks to a local Ollama server. It is the recommended provider when
// you want AI assistance without anything leaving the machine.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama builds an Ollama classifier, defaulting to the standard local
// endpoint and a small instruct model.
func NewOllama(cfg config.Config) *Ollama {
	base := strings.TrimRight(cfg.AI.BaseURL, "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	m := cfg.AI.Model
	if m == "" {
		m = "llama3.1"
	}
	return &Ollama{baseURL: base, model: m, client: newClient(cfg)}
}

// Name implements Classifier.
func (o *Ollama) Name() string { return "ollama (" + o.model + ")" }

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format"`
	Options  ollamaOptions   `json:"options"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
	Error   string        `json:"error"`
}

// Classify implements Classifier.
func (o *Ollama) Classify(ctx context.Context, req ClassificationRequest) (ClassificationResult, error) {
	payload := ollamaRequest{
		Model:  o.model,
		Stream: false,
		Format: "json", // Ollama's structured-output switch
		Messages: []ollamaMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: userPrompt(req)},
		},
		Options: ollamaOptions{Temperature: 0.1},
	}
	var resp ollamaResponse
	if err := postJSON(ctx, o.client, o.baseURL+"/api/chat", nil, payload, &resp); err != nil {
		return ClassificationResult{}, err
	}
	if resp.Error != "" {
		return ClassificationResult{}, ollamaError(resp.Error, o.model)
	}
	return ParseResult(resp.Message.Content)
}

func ollamaError(msg, model string) error {
	if strings.Contains(msg, "not found") {
		return &ProviderError{Msg: "Ollama does not have the model \"" + model + "\". Run: ollama pull " + model}
	}
	return &ProviderError{Msg: "Ollama error: " + msg}
}

// ProviderError is a provider-side failure with a user-facing message.
type ProviderError struct{ Msg string }

func (e *ProviderError) Error() string { return e.Msg }
