// cmd/promptoverse_gpt2.go — emily promptoverse brainstorm
//
// Founder direction: "develop a new tool to expand styles - prompt gpt2
// like this - pop art silkscreen, woodcut block print, underwater, outer
// space, robot" + "and then see what it responds with" + "parse out the
// tags it presents."
//
// A separate, standalone brainstorming tool -- NOT wired into `add`'s
// existing Vertex-AI-based style discovery (promptoverse_discover.go),
// which asks Gemini for one structured, validated {label, kind, template}
// proposal at a time. This is the opposite shape on purpose: base GPT-2 has
// no instruction-following to speak of, so the only thing it's good for
// here is raw few-shot list continuation -- feed it a comma-separated seed
// list, let it keep the list going, and parse whatever plausible short
// tags come out. Output is candidates for a human to review, nothing is
// added to the registry automatically.
//
// Empirically (2026-08-17, live against this box's gpt2-alpine-c stack):
// the fine-tuned checkpoint (emily-ft, trained on Emily's own writing)
// drifts into first-person prose almost immediately and is useless for
// this; the base checkpoint (stock gpt2, --model base) reliably continues
// a comma-separated list in the same shape for tens of tokens, though the
// vocabulary it drifts toward has no reason to stay on-topic (it's a
// generic LM, not fine-tuned on style names) -- hence a real parse/filter
// step, not just splitting on commas.
package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func runPromptOVerseBrainstorm(args []string) int {
	fs := flag.NewFlagSet("promptoverse brainstorm", flag.ContinueOnError)
	target := fs.String("target", "styles", "what to brainstorm: styles | subjects")
	seed := fs.String("seed", "", "comma-separated seed list (default: a random sample, see --sample)")
	sampleN := fs.Int("sample", 5, "how many existing items to randomly sample as the seed when --seed isn't given")
	maxTokens := fs.Int("max-tokens", 60, "tokens to generate")
	temperature := fs.Float64("temperature", 0.9, "sampling temperature -- 0.9 empirically kept the base model in list-continuation mode longer than 0.7")
	via := fs.String("via", "server", "endpoint: server (:8088) | proxy (:8679) | emily (:8086)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *target != "styles" && *target != "subjects" {
		fmt.Fprintf(os.Stderr, "emily promptoverse brainstorm: --target must be \"styles\" or \"subjects\", got %q\n", *target)
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	// existingLabels is what the seed samples from AND what parsed
	// candidates get checked against for "already known" -- styles vs
	// subjects have different sources (styles: the compiled registry +
	// discovered; subjects: real published usage + discovered, no
	// hardcoded list the way styles need one).
	var existingLabels []string
	switch *target {
	case "styles":
		discovered, derr := loadDiscoveredStyles(discoveredStylesPath(cfg))
		if derr != nil {
			fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", derr)
			return 1
		}
		for _, st := range combinedStylePool(discovered) {
			existingLabels = append(existingLabels, st.Label)
		}
	case "subjects":
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		nodes, lerr := client.ListPromptOVerseNodes()
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "list existing nodes: %v\n", lerr)
			return 1
		}
		discovered, derr := loadDiscoveredSubjects(discoveredSubjectsPath(cfg))
		if derr != nil {
			fmt.Fprintf(os.Stderr, "load discovered subjects: %v\n", derr)
			return 1
		}
		existingLabels = subjectPool(nodes, discovered)
	}

	seedList := strings.TrimSpace(*seed)
	if seedList == "" {
		// A random SAMPLE, not the full pool every time -- founder: "when i
		// told you about it i gave you an example prompt but we can have
		// any number of 4 [or so] prompts we already have as styles as a
		// perturbation for gpt2 to start spitting out data." Different
		// subsets nudge completions in different directions instead of the
		// same always-full seed producing similar output run after run.
		seedList = strings.Join(sampleLabels(existingLabels, *sampleN, rand.New(rand.NewSource(time.Now().UnixNano()))), ", ")
	}
	// Trailing ", " (not just ",") is what actually kept the base model in
	// list-continuation mode in live testing -- without the space it tended
	// to drift into prose immediately.
	prompt := seedList + ", "

	fmt.Printf("target: %s\nprompt (%s): %q\n\n", *target, *via, truncateForDisplay(prompt, 200))

	text, model, err := gpt2GenerateRaw(*via, prompt, *maxTokens, *temperature)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpt2 request failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "hint: emily gpt2 start --model base")
		return 1
	}

	fmt.Printf("model: %s\nraw completion:\n  %s\n\n", model, text)

	candidates := parseStyleTags(text) // parser is generic (comma/newline list cleanup), reused as-is for subjects
	existing := make(map[string]bool, len(existingLabels))
	for _, l := range existingLabels {
		existing[strings.ToLower(l)] = true
	}

	if len(candidates) == 0 {
		fmt.Println("no plausible tags parsed out of that completion -- try again, a different --seed, or a higher --temperature")
		return 0
	}

	promoteHint := "emily promptoverse promote <label>"
	if *target == "subjects" {
		promoteHint = "emily promptoverse promote-subject <label>"
	}

	fmt.Printf("parsed candidate %s:\n", *target)
	newCount := 0
	for _, c := range candidates {
		if existing[strings.ToLower(c)] {
			fmt.Printf("  - %s (already known)\n", c)
			continue
		}
		newCount++
		fmt.Printf("  - %s\n", c)
	}
	fmt.Printf("\n%d candidate(s) parsed, %d not already known.\n", len(candidates), newCount)

	kind := "style"
	if *target == "subjects" {
		kind = "subject"
	}
	added, recErr := recordCandidates(candidatesPath(cfg), kind, candidates, seedList, existing)
	if recErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to persist candidate %s: %v\n", *target, recErr)
	} else if added > 0 {
		fmt.Printf("saved %d new candidate(s) for later review: %s\n", added, promoteHint)
	}
	fmt.Println("Nothing added automatically -- promote anything worth keeping with `emily promptoverse promote <label> [--rare]`.")
	return 0
}

// sampleLabels picks up to n distinct labels at random, without
// replacement -- if n >= len(labels), returns all of them (shuffled).
// Pure aside from the rng parameter, testable without real randomness.
func sampleLabels(labels []string, n int, rng *rand.Rand) []string {
	pool := append([]string(nil), labels...)
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n < len(pool) {
		pool = pool[:n]
	}
	return pool
}

// parseStyleTags extracts plausible short style-name candidates from a raw
// GPT-2 completion: split on commas/newlines, trim stray punctuation, drop
// anything that's empty, too long, or contains sentence punctuation
// (a sign the model drifted from list-continuation into prose), dedupe
// case-insensitively. Pure function, testable against real captured
// completions without a live model.
func parseStyleTags(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		tag := strings.Trim(strings.TrimSpace(f), `"'•*-–—()[]`)
		tag = strings.TrimSpace(tag)
		if tag == "" || len(tag) > 40 {
			continue
		}
		if strings.ContainsAny(tag, ".!?;:") {
			continue
		}
		if !containsLetter(tag) {
			continue // e.g. a run of underscores/dashes, not a real tag
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	return out
}

func containsLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func truncateForDisplay(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// gpt2GenerateRaw hits the same three possible endpoints as `emily gpt2
// generate` (cmd/gpt2.go's runGPT2Generate) -- duplicated rather than
// shared because that function is a full CLI handler (flag parsing, its
// own stdout formatting), not a reusable call; this is deliberately the
// minimal subset promptoverse needs.
func gpt2GenerateRaw(via, prompt string, maxTokens int, temperature float64) (text, model string, err error) {
	var endpoint string
	headers := map[string]string{}
	switch via {
	case "emily":
		endpoint = "http://localhost:8086/api/v1/gpt2/generate"
	case "proxy":
		endpoint = "http://localhost:8679/generate"
		headers["Authorization"] = "Bearer emily-gpt2-local"
	default:
		endpoint = "http://localhost:8088/generate"
	}

	body, _ := json.Marshal(map[string]any{
		"prompt":      prompt,
		"max_tokens":  maxTokens,
		"temperature": temperature,
	})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("gpt2 %d: %s", resp.StatusCode, trimMsgPO(raw))
	}

	var out struct {
		Text  string `json:"text"`
		Model string `json:"model"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("decode gpt2 response: %w", err)
	}
	if out.Error != "" {
		return "", "", fmt.Errorf("%s", out.Error)
	}
	return out.Text, out.Model, nil
}
