package mashupjudge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiProvider judges via Vertex AI's Gemini text model, using the same
// gcloud-ADC auth path already established for style/subject discovery
// (cmd/promptoverse_discover.go's vertexTextGenerate) -- the active
// default provider, since no Claude API credits were available when this
// was built.
type GeminiProvider struct {
	// Token is a bearer token from `gcloud auth print-access-token`.
	// Fetching it is the caller's job (matches gcloudAccessToken() in
	// cmd/promptoverse.go) so this package has no gcloud dependency.
	Token   string
	Project string
	Region  string
	Model   string
	// Endpoint overrides the Vertex AI URL -- tests only.
	Endpoint string

	httpClient *http.Client
}

func (g *GeminiProvider) Name() string { return "gemini" }

func (g *GeminiProvider) client() *http.Client {
	if g.httpClient != nil {
		return g.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (g *GeminiProvider) Judge(subject string, registry []string, asOf time.Time) (Judgment, error) {
	prompt := buildPrompt(subject, registry, asOf)

	url := g.Endpoint
	if url == "" {
		url = fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
			g.Region, g.Project, g.Region, g.Model,
		)
	}
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": prompt}}},
		},
	})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return Judgment{}, err
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client().Do(req)
	if err != nil {
		return Judgment{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		return Judgment{}, fmt.Errorf("gemini (vertex) %d: %s", resp.StatusCode, trimForError(string(raw)))
	}

	text, err := extractGeminiText(raw)
	if err != nil {
		return Judgment{}, err
	}
	return parseJudgmentResponse(text, subject, g.Name(), asOf)
}

// extractGeminiText pulls the concatenated text out of a Vertex AI
// generateContent response. Small, standalone copy of the same shape
// cmd/promptoverse_discover.go's extractGeminiText already implements --
// duplicated rather than imported because cmd imports internal/config
// and other cmd-only state this package should stay independent of; both
// copies parse the same stable third-party response shape.
func extractGeminiText(raw []byte) (string, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode vertex response: %w", err)
	}
	for _, c := range parsed.Candidates {
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				return p.Text, nil
			}
		}
	}
	return "", fmt.Errorf("no text in vertex response: %s", trimForError(string(raw)))
}
