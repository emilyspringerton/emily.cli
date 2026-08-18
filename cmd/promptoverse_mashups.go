// cmd/promptoverse_mashups.go — LLM-judgment mashup/hybrid detection.
//
// A first implementation attempt (pure lexical word-bag/substring
// matching) was abandoned mid-build once tested against real founder
// counterexamples -- see internal/mashupjudge's package doc comment and
// EMILY/docs/NORTHSTAR_PROMPT_O_VERSE.md §9 for the full history. This
// command is the follow-up: ask an LLM instead of a string-comparison
// rule. Founder, real-time: "i think the ontology problem could be
// solved with a very clever query... llm query... lean on claude or
// gemini api for now... build claude gemini parity for that so we can
// switch to claude or we can even run them in paralell for AB testing in
// the future" -- Gemini/Vertex is the active default (no Claude API
// credits available at build time), Claude is implemented for parity.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
	"github.com/emilyspringerton/emily-cli/internal/mashupjudge"
)

const (
	promptoverseMashupJudgmentsFileName      = "promptoverse-mashup-judgments.json"
	promptoverseStyleMashupJudgmentsFileName = "promptoverse-style-mashup-judgments.json"
)

func mashupJudgmentsPath(cfg *config.Config, fileName string) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", fileName)
}

func loadMashupJudgments(path string) ([]mashupjudge.Judgment, error) {
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
	var out []mashupjudge.Judgment
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode mashup judgments: %w", err)
	}
	return out, nil
}

// upsertMashupJudgments replaces any existing (Subject, Provider) entry
// with the new judgment (a fresh AsOf supersedes a stale one -- see the
// fixed point / zero point discussion in internal/mashupjudge) and
// appends anything genuinely new, then writes the whole set back sorted
// for stable diffs.
func upsertMashupJudgments(path string, updates []mashupjudge.Judgment) error {
	existing, err := loadMashupJudgments(path)
	if err != nil {
		return err
	}
	byKey := make(map[string]mashupjudge.Judgment, len(existing)+len(updates))
	order := make([]string, 0, len(existing)+len(updates))
	for _, j := range existing {
		key := j.Subject + "\x00" + j.Provider
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = j
	}
	for _, j := range updates {
		key := j.Subject + "\x00" + j.Provider
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = j
	}
	sort.Strings(order)
	out := make([]mashupjudge.Judgment, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func mashupProviders(cfg *config.Config, which string) ([]mashupjudge.Provider, error) {
	var providers []mashupjudge.Provider
	wantGemini := which == "gemini" || which == "all"
	wantClaude := which == "claude" || which == "all"

	if wantGemini {
		token, err := gcloudAccessToken()
		if err != nil {
			return nil, fmt.Errorf("gemini provider: %w", err)
		}
		providers = append(providers, &mashupjudge.GeminiProvider{
			Token:   token,
			Project: promptoverseVertexProject,
			Region:  promptoverseVertexRegion,
			Model:   promptoverseVertexTextModel,
		})
	}
	if wantClaude {
		if cfg.AnthropicKey == "" {
			if which == "claude" {
				return nil, fmt.Errorf("claude provider: ANTHROPIC_API_KEY not set")
			}
			// which == "all": skip Claude silently rather than fail the
			// whole run -- matches the founder's stated constraint ("no
			// free claude credits") without making --provider all unusable.
			fmt.Fprintln(os.Stderr, "mashups: skipping claude provider, ANTHROPIC_API_KEY not set")
		} else {
			providers = append(providers, &mashupjudge.ClaudeProvider{APIKey: cfg.AnthropicKey})
		}
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("no usable provider for --provider %q", which)
	}
	return providers, nil
}

// mashupRegistry returns the full label pool for the given target
// ("subjects" or "styles") plus the JSON tag used to key cached
// judgments for that target -- subjects and styles are two separate
// namespaces (a subject and a style can coincidentally share a label
// without being the same thing), so judgments are kept in two separate
// cache files rather than one shared one.
func mashupRegistry(cfg *config.Config, client *iduna.Client, target string) (registry []string, cacheFileName string, err error) {
	switch target {
	case "subjects":
		existing, err := client.ListPromptOVerseNodes()
		if err != nil {
			return nil, "", fmt.Errorf("list existing nodes: %w", err)
		}
		discovered, err := loadDiscoveredSubjects(discoveredSubjectsPath(cfg))
		if err != nil {
			return nil, "", fmt.Errorf("load discovered subjects: %w", err)
		}
		return subjectPool(existing, discovered), promptoverseMashupJudgmentsFileName, nil
	case "styles":
		discoveredPath := discoveredStylesPath(cfg)
		discovered, err := loadDiscoveredStyles(discoveredPath)
		if err != nil {
			return nil, "", fmt.Errorf("load discovered styles: %w", err)
		}
		pool := combinedStylePool(discovered)
		labels := make([]string, 0, len(pool))
		for _, st := range pool {
			labels = append(labels, st.Label)
		}
		return labels, promptoverseStyleMashupJudgmentsFileName, nil
	default:
		return nil, "", fmt.Errorf("--target must be subjects or styles (got %q)", target)
	}
}

func runPromptOVerseMashups(args []string) int {
	provider := "gemini"
	target := "subjects"
	subjectFilter := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse mashups: --provider requires a value (gemini|claude|all)")
				return 1
			}
			provider = args[i+1]
			i++
		case "--target":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse mashups: --target requires a value (subjects|styles)")
				return 1
			}
			target = args[i+1]
			i++
		case "--subject":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse mashups: --subject requires a value")
				return 1
			}
			subjectFilter = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "emily promptoverse mashups: unknown flag %q\n", args[i])
			return 1
		}
	}
	if provider != "gemini" && provider != "claude" && provider != "all" {
		fmt.Fprintf(os.Stderr, "emily promptoverse mashups: --provider must be gemini, claude, or all (got %q)\n", provider)
		return 1
	}
	if target != "subjects" && target != "styles" {
		fmt.Fprintf(os.Stderr, "emily promptoverse mashups: --target must be subjects or styles (got %q)\n", target)
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	registry, cacheFileName, err := mashupRegistry(cfg, client, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if len(registry) == 0 {
		fmt.Printf("no %s in the registry yet -- nothing to judge\n", target)
		return 0
	}

	subjects := registry
	if subjectFilter != "" {
		found := false
		for _, s := range registry {
			if strings.EqualFold(s, subjectFilter) {
				subjects = []string{s}
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "emily promptoverse mashups: %q is not in the current %s registry\n", subjectFilter, target)
			return 1
		}
	}

	providers, err := mashupProviders(cfg, provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	asOf := time.Now().UTC()
	judgmentsPath := mashupJudgmentsPath(cfg, cacheFileName)
	var results []mashupjudge.Judgment
	mashupCount := 0
	for _, subject := range subjects {
		otherSubjects := make([]string, 0, len(registry)-1)
		for _, s := range registry {
			if s != subject {
				otherSubjects = append(otherSubjects, s)
			}
		}
		for _, p := range providers {
			j, err := p.Judge(subject, otherSubjects, asOf)
			if err != nil {
				fmt.Fprintf(os.Stderr, "mashups: %s (%s): %v\n", subject, p.Name(), err)
				continue
			}
			results = append(results, j)
			if j.IsCompositionalMashup || len(j.ParaphraseEquivalents) > 0 {
				mashupCount++
				fmt.Printf("%s [%s]: mashup=%v components=%v paraphrases=%v (%s)\n",
					subject, p.Name(), j.IsCompositionalMashup, j.Components, j.ParaphraseEquivalents, j.ReferentStability)
			}
		}
	}

	if len(results) == 0 {
		fmt.Println("no judgments produced")
		return 1
	}
	if err := upsertMashupJudgments(judgmentsPath, results); err != nil {
		fmt.Fprintf(os.Stderr, "save judgments: %v\n", err)
		return 1
	}
	fmt.Printf("judged %d subject/provider pair(s), %d flagged as mashup or paraphrase-equivalent, saved to %s\n",
		len(results), mashupCount, judgmentsPath)
	return 0
}
