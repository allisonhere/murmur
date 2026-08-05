package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/alliebayless/murmur/internal/config"
)

// userPrompt renders the request as the JSON payload the model sees.
func userPrompt(req ClassificationRequest) string {
	req.FormattingRules = FormattingRules
	if len(req.ContentTypes) == 0 {
		req.ContentTypes = []string{"task", "idea", "journal", "project", "reference", "question", "bookmark", "note"}
	}
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return req.Thought
	}
	return string(data)
}

func timeout(cfg config.Config) time.Duration {
	if cfg.AI.TimeoutSeconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(cfg.AI.TimeoutSeconds) * time.Second
}

// postJSON performs a JSON round trip and turns transport failures into
// messages a user can act on.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return describeTransportError(url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("the AI provider rejected the credentials (HTTP %d); check the environment variable named by ai.api_key_env", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("AI provider returned HTTP %d: %.200s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	return nil
}

func describeTransportError(url string, err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("the AI provider at %s timed out; Murmur will use deterministic routing", url)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if strings.Contains(err.Error(), "connection refused") {
		return fmt.Errorf("cannot reach the AI provider at %s; is it running?", url)
	}
	return fmt.Errorf("AI provider request failed: %w", err)
}

func newClient(cfg config.Config) *http.Client {
	return &http.Client{Timeout: timeout(cfg)}
}
