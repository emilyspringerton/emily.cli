package mashupjudge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildPrompt_IncludesSubjectRegistryAndReferenceDate(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	prompt := buildPrompt("Fractal Raccoon", []string{"Fractal", "Raccoon", "Tuxedo"}, asOf)

	for _, want := range []string{"Fractal Raccoon", "Fractal", "Raccoon", "Tuxedo", "2026-08-18", "tuxedo duck"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPrompt_EmptyRegistry(t *testing.T) {
	prompt := buildPrompt("Anything", nil, time.Now())
	if !strings.Contains(prompt, "(none yet)") {
		t.Errorf("expected empty-registry placeholder, got:\n%s", prompt)
	}
}

func TestParseJudgmentResponse_Basic(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	raw := `{"is_compositional_mashup": true, "components": ["Fractal", "Raccoon"], "paraphrase_equivalents": [], "referent_stability": "generic", "reasoning": "compositional"}`

	j, err := parseJudgmentResponse(raw, "Fractal Raccoon", "gemini", asOf)
	if err != nil {
		t.Fatalf("parseJudgmentResponse: %v", err)
	}
	if !j.IsCompositionalMashup {
		t.Error("expected IsCompositionalMashup=true")
	}
	if len(j.Components) != 2 || j.Components[0] != "Fractal" || j.Components[1] != "Raccoon" {
		t.Errorf("unexpected components: %v", j.Components)
	}
	if j.Subject != "Fractal Raccoon" || j.Provider != "gemini" {
		t.Errorf("unexpected Subject/Provider: %q/%q", j.Subject, j.Provider)
	}
	if !j.AsOf.Equal(asOf) {
		t.Errorf("AsOf: got %v want %v", j.AsOf, asOf)
	}
}

func TestParseJudgmentResponse_TuxedoDuckIsNotAMashup(t *testing.T) {
	// Regression for the exact case that killed the lexical-matching
	// approach: a well-behaved model response should be able to say "no."
	raw := `{"is_compositional_mashup": false, "components": [], "paraphrase_equivalents": ["a duck wearing a tuxedo"], "referent_stability": "generic", "reasoning": "tuxedo duck is a real color-morph name, not a mashup of tuxedo and duck"}`
	j, err := parseJudgmentResponse(raw, "tuxedo duck", "gemini", time.Now())
	if err != nil {
		t.Fatalf("parseJudgmentResponse: %v", err)
	}
	if j.IsCompositionalMashup {
		t.Error("expected IsCompositionalMashup=false for tuxedo duck")
	}
	if len(j.ParaphraseEquivalents) != 1 || j.ParaphraseEquivalents[0] != "a duck wearing a tuxedo" {
		t.Errorf("unexpected paraphrase equivalents: %v", j.ParaphraseEquivalents)
	}
}

func TestParseJudgmentResponse_StripsMarkdownFences(t *testing.T) {
	raw := "```json\n{\"is_compositional_mashup\": false, \"referent_stability\": \"fixed\"}\n```"
	j, err := parseJudgmentResponse(raw, "x", "claude", time.Now())
	if err != nil {
		t.Fatalf("parseJudgmentResponse: %v", err)
	}
	if j.ReferentStability != "fixed" {
		t.Errorf("ReferentStability: got %q", j.ReferentStability)
	}
}

func TestParseJudgmentResponse_DefaultsReferentStabilityToGeneric(t *testing.T) {
	j, err := parseJudgmentResponse(`{"is_compositional_mashup": false}`, "x", "gemini", time.Now())
	if err != nil {
		t.Fatalf("parseJudgmentResponse: %v", err)
	}
	if j.ReferentStability != "generic" {
		t.Errorf("expected default \"generic\", got %q", j.ReferentStability)
	}
}

func TestParseJudgmentResponse_RejectsUnrecognizedReferentStability(t *testing.T) {
	_, err := parseJudgmentResponse(`{"referent_stability": "eternal"}`, "x", "gemini", time.Now())
	if err == nil {
		t.Fatal("expected an error for an unrecognized referent_stability value")
	}
}

func TestParseJudgmentResponse_InvalidJSON(t *testing.T) {
	_, err := parseJudgmentResponse("not json at all", "x", "gemini", time.Now())
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestGeminiProvider_Judge_LiveShapedMock(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header: got %q", got)
		}
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": `{"is_compositional_mashup": true, "components": ["Fractal", "Raccoon"], "paraphrase_equivalents": [], "referent_stability": "generic", "reasoning": "ok"}`},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	g := &GeminiProvider{Token: "test-token", Endpoint: srv.URL, Model: "gemini-2.5-flash"}
	j, err := g.Judge("Fractal Raccoon", []string{"Fractal", "Raccoon"}, asOf)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Provider != "gemini" || !j.IsCompositionalMashup {
		t.Errorf("unexpected judgment: %+v", j)
	}
}

func TestGeminiProvider_Judge_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()

	g := &GeminiProvider{Token: "t", Endpoint: srv.URL, Model: "m"}
	_, err := g.Judge("x", nil, time.Now())
	if err == nil {
		t.Fatal("expected an error on non-200 response")
	}
}

func TestClaudeProvider_Judge_LiveShapedMock(t *testing.T) {
	asOf := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key header: got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version header: got %q", got)
		}
		resp := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"is_compositional_mashup": false, "components": [], "paraphrase_equivalents": ["duck wearing a tuxedo"], "referent_stability": "generic", "reasoning": "real breed name"}`},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &ClaudeProvider{APIKey: "test-key", Endpoint: srv.URL, Model: "claude-haiku-4-5-20251001"}
	j, err := c.Judge("tuxedo duck", []string{"tuxedo", "duck"}, asOf)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if j.Provider != "claude" || j.IsCompositionalMashup {
		t.Errorf("unexpected judgment: %+v", j)
	}
}

func TestClaudeProvider_Judge_MissingAPIKey(t *testing.T) {
	c := &ClaudeProvider{}
	_, err := c.Judge("x", nil, time.Now())
	if err == nil {
		t.Fatal("expected an error when APIKey is empty")
	}
}

func TestClaudeProvider_Judge_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &ClaudeProvider{APIKey: "k", Endpoint: srv.URL}
	_, err := c.Judge("x", nil, time.Now())
	if err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("expected a rate-limited error, got %v", err)
	}
}

// Both providers must implement Provider -- compile-time check.
var (
	_ Provider = (*GeminiProvider)(nil)
	_ Provider = (*ClaudeProvider)(nil)
)
