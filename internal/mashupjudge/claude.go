package mashupjudge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ClaudeProvider judges via Anthropic's Messages API, same request shape
// EMILY/emily-agent/gpt2fallback.go's callHaiku already established
// (x-api-key header, anthropic-version 2023-06-01). Built for
// provider parity per founder direction ("build claude gemini parity for
// that so we can switch to claude or we can even run them in paralell
// for AB testing in the future") -- exercised by tests against a mock
// server only, not live, since no Claude API credits were available at
// build time ("as we dont have any free claude credits to use").
type ClaudeProvider struct {
	APIKey string
	Model  string // e.g. "claude-haiku-4-5-20251001"
	// Endpoint overrides the Anthropic Messages API URL -- tests only.
	Endpoint string

	httpClient *http.Client
}

func (c *ClaudeProvider) Name() string { return "claude" }

func (c *ClaudeProvider) client() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *ClaudeProvider) Judge(subject string, registry []string, asOf time.Time) (Judgment, error) {
	if c.APIKey == "" {
		return Judgment{}, fmt.Errorf("claude: ANTHROPIC_API_KEY not set")
	}
	prompt := buildPrompt(subject, registry, asOf)

	url := c.Endpoint
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}
	model := c.Model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	payload, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 512,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return Judgment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client().Do(req)
	if err != nil {
		return Judgment{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode == http.StatusTooManyRequests {
		return Judgment{}, fmt.Errorf("claude: rate-limited (429)")
	}
	if resp.StatusCode != http.StatusOK {
		return Judgment{}, fmt.Errorf("claude %d: %s", resp.StatusCode, trimForError(string(raw)))
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return Judgment{}, fmt.Errorf("claude: decode response: %w", err)
	}
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return parseJudgmentResponse(block.Text, subject, c.Name(), asOf)
		}
	}
	return Judgment{}, fmt.Errorf("claude: no text block in response")
}
