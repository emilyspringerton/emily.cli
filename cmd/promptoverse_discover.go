// cmd/promptoverse_discover.go — Prompt-o-verse style discovery
//
// Founder direction: "we need a way to expand the styles - in the same way
// you came up with the styles for the baseball card we need to do a step
// where we consider creating a new style if it is the second or later
// generation (always add to the graph first then expand it)" + "using the
// google cloud apis" + "but we should not add frivolous styles so the
// second gen will not necessarily always expand the graph if it doesn't
// make sense to do so."
//
// "Always add to the graph first" means selectStylesForSubject (the
// existing hardcoded + previously-discovered registry) is always tried
// before this file's code runs at all -- discovery only fires when that
// pool ran short for a subject that already has a prior generation (a
// brand-new subject uses the existing registry only, same as the original
// baseball-card batch). "Not frivolous" means the model can decline --
// {"propose": false} -- and does so via the same structured contract rather
// than a caller-side heuristic; declining is the expected common outcome,
// not an error.
//
// A discovered style is persisted to EMILY/var/promptoverse-discovered-
// styles.json (append-only, atomic tmp+rename like the queue file) so it
// becomes part of the durable registry for every future subject, not just
// the one that triggered it.
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// discoveredStyle is one style the model proposed and that passed
// validation, persisted as JSON.
type discoveredStyle struct {
	Label         string    `json:"label"`
	Kind          string    `json:"kind"`     // "historical" | "surreal"
	Template      string    `json:"template"` // contains exactly one %s placeholder for the subject
	DiscoveredFor string    `json:"discovered_for_subject"`
	DiscoveredAt  time.Time `json:"discovered_at"`
}

func discoveredStylesPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseDiscoveredFileName)
}

func loadDiscoveredStyles(path string) ([]discoveredStyle, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var out []discoveredStyle
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode discovered styles: %w", err)
	}
	return out, nil
}

func appendDiscoveredStyle(path string, ds discoveredStyle) error {
	existing, err := loadDiscoveredStyles(path)
	if err != nil {
		return err
	}
	existing = append(existing, ds)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// styleFromDiscovered converts a persisted discoveredStyle into a style
// usable by selectStylesForSubject/drainQueue. Returns ok=false if the
// template is malformed (must contain exactly one %s placeholder) -- a
// defensive check against a corrupted or hand-edited discovered-styles file,
// not expected to trigger for anything this package itself wrote.
func styleFromDiscovered(ds discoveredStyle) (style, bool) {
	if strings.Count(ds.Template, "%s") != 1 {
		return style{}, false
	}
	tmpl := ds.Template
	return style{
		Label:  ds.Label,
		Kind:   ds.Kind,
		Prompt: func(s string) string { return fmt.Sprintf(tmpl, s) },
	}, true
}

// combinedStylePool is "the graph" selectStylesForSubject draws from: the
// hardcoded registry plus every valid discovered style, hardcoded styles
// first so ties in usage still prefer the proven set.
func combinedStylePool(discovered []discoveredStyle) []style {
	pool := make([]style, 0, len(promptoverseStyles)+len(discovered))
	pool = append(pool, promptoverseStyles...)
	for _, ds := range discovered {
		if st, ok := styleFromDiscovered(ds); ok {
			pool = append(pool, st)
		}
	}
	return pool
}

// extractGeminiText pulls the concatenated text parts out of a Vertex AI
// generateContent response. Split out from maybeDiscoverStyle so it's
// testable against a canned response body without a live network call.
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
		return "", fmt.Errorf("decode vertex text response: %w", err)
	}
	var text strings.Builder
	for _, c := range parsed.Candidates {
		for _, p := range c.Content.Parts {
			text.WriteString(p.Text)
		}
	}
	return text.String(), nil
}

// parseStyleProposalJSON decodes and validates the model's raw text
// response into a discoveredStyle. Returns (nil, nil) for an explicit
// decline ({"propose": false}) -- the expected common case, not an error.
// Split out from maybeDiscoverStyle so the validation logic (the actual
// "not frivolous" gate: well-formed, non-empty, not already in the
// registry) is testable without a live network call.
func parseStyleProposalJSON(text string, existingLabels []string, subject string) (*discoveredStyle, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var out struct {
		Propose  bool   `json:"propose"`
		Label    string `json:"label"`
		Kind     string `json:"kind"`
		Template string `json:"template"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("decode style proposal JSON: %w (raw: %s)", err, trimMsgPO([]byte(text)))
	}
	if !out.Propose {
		return nil, nil
	}

	out.Label = strings.TrimSpace(out.Label)
	out.Kind = strings.TrimSpace(strings.ToLower(out.Kind))
	if out.Label == "" || (out.Kind != "historical" && out.Kind != "surreal") || strings.Count(out.Template, "%s") != 1 {
		return nil, fmt.Errorf("model proposed a malformed style, discarding: %+v", out)
	}
	for _, existing := range existingLabels {
		if strings.EqualFold(existing, out.Label) {
			return nil, fmt.Errorf("model proposed a style already in the registry (%q), discarding", out.Label)
		}
	}

	return &discoveredStyle{
		Label:         out.Label,
		Kind:          out.Kind,
		Template:      out.Template,
		DiscoveredFor: subject,
		DiscoveredAt:  time.Now().UTC(),
	}, nil
}

// maybeDiscoverStyle asks Vertex AI's Gemini text model whether a genuinely
// new, non-frivolous, subject-agnostic style is warranted, given the styles
// already in the registry. Returns (nil, nil) if the model declines -- the
// normal, expected outcome most of the time, not an error.
func maybeDiscoverStyle(token, subject string, existingLabels []string) (*discoveredStyle, error) {
	prompt := fmt.Sprintf(`You maintain a reusable "style" registry for Prompt-o-verse, a generative art gallery. A style is a subject-agnostic visual/transformation treatment applicable to ANY subject -- e.g. "claymation", "stained glass cathedral window", "made of candy". It must NOT be specific to one subject.

Styles already in the registry:
%s

A subject just ran out of unused existing styles: %q.

Propose exactly ONE new style ONLY if it is genuinely distinct, general-purpose, and would add real variety -- not a trivial variation of an existing one. If you cannot think of a good one, decline.

Respond with ONLY raw JSON (no markdown fences, no commentary), in exactly one of these two shapes:
{"propose": false}
{"propose": true, "label": "short style name", "kind": "historical", "template": "a full expanded-prompt sentence using %%s exactly once as a placeholder for the subject"}

"kind" must be "historical" (resembles something that really existed/exists) or "surreal" (a whimsical/creative treatment).`,
		strings.Join(existingLabels, ", "), subject)

	text, err := vertexTextGenerate(token, prompt)
	if err != nil {
		return nil, err
	}
	return parseStyleProposalJSON(text, existingLabels, subject)
}

// vertexTextGenerate calls Vertex AI's Gemini text model and returns the
// concatenated text of the response. Shared by maybeDiscoverStyle (model
// proposes both name and template) and expandNamedStyle (caller fixes the
// name, model only writes the template).
func vertexTextGenerate(token, prompt string) (string, error) {
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		promptoverseVertexRegion, promptoverseVertexProject, promptoverseVertexRegion, promptoverseVertexTextModel,
	)
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": prompt}}},
		},
	})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vertex ai (text) %d: %s", resp.StatusCode, trimMsgPO(raw))
	}
	return extractGeminiText(raw)
}

// parseNamedStyleTemplateJSON decodes and validates the model's response to
// expandNamedStyle's prompt -- same fenced-JSON handling and validation
// bar as parseStyleProposalJSON, but there's no "propose"/decline path
// (the name is fixed by the caller) and no "label" field to parse (also
// fixed).
func parseNamedStyleTemplateJSON(text, label, subject string) (*discoveredStyle, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var out struct {
		Kind     string `json:"kind"`
		Template string `json:"template"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("decode style template JSON: %w (raw: %s)", err, trimMsgPO([]byte(text)))
	}
	out.Kind = strings.TrimSpace(strings.ToLower(out.Kind))
	if out.Kind != "historical" && out.Kind != "surreal" {
		return nil, fmt.Errorf("model returned an invalid kind %q", out.Kind)
	}
	if strings.Count(out.Template, "%s") != 1 {
		return nil, fmt.Errorf("model's template is missing the %%s placeholder: %q", out.Template)
	}

	return &discoveredStyle{
		Label:         label,
		Kind:          out.Kind,
		Template:      out.Template,
		DiscoveredFor: subject,
		DiscoveredAt:  time.Now().UTC(),
	}, nil
}

// expandNamedStyle asks Vertex AI's Gemini text model to write a real,
// subject-agnostic template for an EXPLICITLY named style -- used by
// `emily promptoverse add <subject> <count> --tag <label>` to force a
// specific style whether or not it already exists (founder: "princess 4
// --tag gladiator forces a new or already existing style gladiator").
// Unlike maybeDiscoverStyle, there's no decline path: the caller fixed the
// name, so the model's only job is to write a good template for it.
func expandNamedStyle(token, label, subject string) (*discoveredStyle, error) {
	prompt := fmt.Sprintf(`You maintain a reusable "style" registry for Prompt-o-verse, a generative art gallery. A style is a subject-agnostic visual/transformation treatment applicable to ANY subject -- e.g. "claymation", "stained glass cathedral window", "made of candy".

Write a template for a new style called %q. It must generalize to any subject, not be specific to one.

Respond with ONLY raw JSON (no markdown fences, no commentary), in exactly this shape:
{"kind": "historical", "template": "a full expanded-prompt sentence using %%s exactly once as a placeholder for the subject"}

"kind" must be "historical" (resembles something that really existed/exists) or "surreal" (a whimsical/creative treatment).`, label)

	text, err := vertexTextGenerate(token, prompt)
	if err != nil {
		return nil, err
	}
	return parseNamedStyleTemplateJSON(text, label, subject)
}
