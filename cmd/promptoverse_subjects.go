// cmd/promptoverse_subjects.go — subject/topic discovery, mirroring the
// entire style-discovery system (promptoverse.go's marble-bag selection +
// rare tier + pity, promptoverse_discover.go's Vertex AI discovery,
// promptoverse_gpt2.go's brainstorm, promptoverse_candidates.go's
// promote) but for SUBJECTS instead of styles.
//
// Founder direction: "and then we need to copy all those same patterns
// for topic discovery."
//
// One real structural difference from styles: a subject has no Kind or
// Template to author -- it's just a string substituted directly into
// whatever style template gets used. That also means, unlike styles,
// subjects need no hardcoded starter registry: real usage data already
// lives in IDUNA (every published node's Subject field), so the pool is
// derived from that plus a discovered-subjects file, not a Go-source
// constant that needs exporting to JSON the way promptoverseStyles did.
package cmd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const promptoverseDiscoveredSubjectsFileName = "promptoverse-discovered-subjects.json"

// discoveredSubject mirrors discoveredStyle's persistence shape, minus the
// fields a subject doesn't need (Kind, Template).
type discoveredSubject struct {
	Label        string    `json:"label"`
	DiscoveredAt time.Time `json:"discovered_at"`
	Rare         bool      `json:"rare,omitempty"`
}

func discoveredSubjectsPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseDiscoveredSubjectsFileName)
}

func loadDiscoveredSubjects(path string) ([]discoveredSubject, error) {
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
	var out []discoveredSubject
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode discovered subjects: %w", err)
	}
	return out, nil
}

func appendDiscoveredSubject(path string, ds discoveredSubject) error {
	existing, err := loadDiscoveredSubjects(path)
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

// subjectPool is every subject known to the system: every distinct
// Subject a published node already carries, plus every discovered
// subject not already covered by that. Order: published-usage subjects
// first (in the order ListPromptOVerseNodes returns them), then
// discovered ones -- mirrors combinedStylePool's "proven set first"
// convention.
func subjectPool(existing []iduna.PromptOVerseNodeSummary, discovered []discoveredSubject) []string {
	seen := make(map[string]bool, len(existing)+len(discovered))
	pool := make([]string, 0, len(existing)+len(discovered))
	for _, n := range existing {
		if n.Subject == "" || seen[n.Subject] {
			continue
		}
		seen[n.Subject] = true
		pool = append(pool, n.Subject)
	}
	for _, ds := range discovered {
		if seen[ds.Label] {
			continue
		}
		seen[ds.Label] = true
		pool = append(pool, ds.Label)
	}
	return pool
}

func rareSubjectLabels(discovered []discoveredSubject) map[string]bool {
	labels := make(map[string]bool, len(discovered))
	for _, ds := range discovered {
		if ds.Rare {
			labels[ds.Label] = true
		}
	}
	return labels
}

// selectSubject picks ONE subject from pool via the same weighted random
// sampling (the "marble bag") selectStylesForSubject uses for styles --
// weight 1/(usage+1), so under-used subjects are more likely, never
// guaranteed. Returns ok=false if every candidate is excluded.
func selectSubject(pool []string, exclude map[string]bool, usage map[string]int, rng *rand.Rand) (string, bool) {
	candidates := make([]string, 0, len(pool))
	for _, s := range pool {
		if !exclude[s] {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	weights := make([]float64, len(candidates))
	total := 0.0
	for i, s := range candidates {
		w := 1.0 / float64(usage[s]+1)
		weights[i] = w
		total += w
	}
	draw := rng.Float64() * total
	idx := len(candidates) - 1
	cum := 0.0
	for i, w := range weights {
		cum += w
		if draw < cum {
			idx = i
			break
		}
	}
	return candidates[idx], true
}

// maybeDiscoverSubject asks Vertex AI's Gemini text model to propose ONE
// new subject, mirroring maybeDiscoverStyle's contract exactly (decline
// path included -- the model can say no, and usually will).
func maybeDiscoverSubject(token string, existingSubjects []string) (*discoveredSubject, error) {
	prompt := fmt.Sprintf(`You maintain a list of "subjects" for Prompt-o-verse, a generative art gallery where each subject gets rendered in various visual styles. A subject is anything picturable -- a person, character, object, or concept -- e.g. "a duck wearing a tuxedo", "Master Chief (Halo)", "a lighthouse keeper", "Aphrodite".

Subjects already used:
%s

Propose exactly ONE new subject ONLY if it is genuinely interesting or fun and clearly distinct from what's already there -- not a trivial variation. If you cannot think of a good one, decline.

Respond with ONLY raw JSON (no markdown fences, no commentary), in exactly one of these two shapes:
{"propose": false}
{"propose": true, "subject": "short subject description"}`, strings.Join(existingSubjects, ", "))

	text, err := vertexTextGenerate(token, prompt)
	if err != nil {
		return nil, err
	}
	return parseSubjectProposalJSON(text, existingSubjects)
}

func parseSubjectProposalJSON(text string, existingSubjects []string) (*discoveredSubject, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var out struct {
		Propose bool   `json:"propose"`
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("decode subject proposal JSON: %w (raw: %s)", err, trimMsgPO([]byte(text)))
	}
	if !out.Propose {
		return nil, nil
	}
	out.Subject = strings.TrimSpace(out.Subject)
	if out.Subject == "" {
		return nil, fmt.Errorf("model proposed an empty subject")
	}
	for _, existing := range existingSubjects {
		if strings.EqualFold(existing, out.Subject) {
			return nil, fmt.Errorf("model proposed a subject already used (%q), discarding", out.Subject)
		}
	}
	return &discoveredSubject{Label: out.Subject, DiscoveredAt: time.Now().UTC()}, nil
}

// pickSubject implements the same layered pattern maybeDiscoverStyle
// established for styles, applied to subjects: rare subjects are excluded
// by default (one shared, pity-adjusted roll makes the whole tier
// eligible), a pity-adjusted roll can propose a brand new subject via
// Vertex AI even when the pool isn't empty (mirrors spontaneous style
// discovery, not just a shortfall fallback), and otherwise the existing
// pool is picked via the same weighted "marble bag" as styles. Mutates
// pity in place -- the caller persists it once, same as the style path.
func pickSubject(cfg *config.Config, existing []iduna.PromptOVerseNodeSummary, rng *rand.Rand, pity *pityState) (string, error) {
	discoveredPath := discoveredSubjectsPath(cfg)
	discovered, err := loadDiscoveredSubjects(discoveredPath)
	if err != nil {
		return "", fmt.Errorf("load discovered subjects: %w", err)
	}
	pool := subjectPool(existing, discovered)

	usage := map[string]int{}
	for _, n := range existing {
		if n.Subject != "" {
			usage[n.Subject]++
		}
	}

	exclude := map[string]bool{}
	if chanceTriggered(rng.Float64(), pityAdjustedChance(promptoverseRareStyleChance, pity.RareSubjectRunsSinceTrigger)) {
		pity.RareSubjectRunsSinceTrigger = 0
	} else {
		pity.RareSubjectRunsSinceTrigger++
		for label := range rareSubjectLabels(discovered) {
			exclude[label] = true
		}
	}

	if chanceTriggered(rng.Float64(), pityAdjustedChance(promptoverseSpontaneousDiscoveryChance, pity.NewSubjectRunsSinceTrigger)) {
		token, tokErr := gcloudAccessToken()
		if tokErr != nil {
			fmt.Fprintf(os.Stderr, "spontaneous subject discovery skipped (gcloud auth: %v)\n", tokErr)
			pity.NewSubjectRunsSinceTrigger++
		} else {
			proposed, discErr := maybeDiscoverSubject(token, pool)
			switch {
			case discErr != nil:
				fmt.Fprintf(os.Stderr, "spontaneous subject discovery attempt failed (continuing without it): %v\n", discErr)
				pity.NewSubjectRunsSinceTrigger++
			case proposed == nil:
				pity.NewSubjectRunsSinceTrigger = 0 // the roll DID trigger, the model just declined -- not a miss
			default:
				pity.NewSubjectRunsSinceTrigger = 0
				if err := appendDiscoveredSubject(discoveredPath, *proposed); err != nil {
					fmt.Fprintf(os.Stderr, "failed to persist discovered subject %q: %v\n", proposed.Label, err)
				} else {
					fmt.Printf("a new subject emerged: %q\n", proposed.Label)
					return proposed.Label, nil
				}
			}
		}
	} else {
		pity.NewSubjectRunsSinceTrigger++
	}

	picked, ok := selectSubject(pool, exclude, usage, rng)
	if !ok {
		return "", fmt.Errorf("no subjects available to pick from, and none could be discovered -- publish at least one node with an explicit subject first")
	}
	return picked, nil
}

func runPromptOVersePromoteSubject(args []string) int {
	rare := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--rare" {
			rare = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse promote-subject <label> [--rare]")
		return 1
	}
	label := strings.TrimSpace(rest[0])
	if label == "" {
		fmt.Fprintln(os.Stderr, "emily promptoverse promote-subject: label must not be empty")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existing, err := client.ListPromptOVerseNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list existing nodes: %v\n", err)
		return 1
	}
	discoveredPath := discoveredSubjectsPath(cfg)
	discovered, err := loadDiscoveredSubjects(discoveredPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered subjects: %v\n", err)
		return 1
	}
	for _, s := range subjectPool(existing, discovered) {
		if strings.EqualFold(s, label) {
			fmt.Printf("%q is already a known subject -- nothing to promote\n", label)
			return 0
		}
	}

	ds := discoveredSubject{Label: label, DiscoveredAt: time.Now().UTC(), Rare: rare}
	if err := appendDiscoveredSubject(discoveredPath, ds); err != nil {
		fmt.Fprintf(os.Stderr, "failed to persist promoted subject %q: %v\n", label, err)
		return 1
	}

	candPath := candidatesPath(cfg)
	if candidates, err := loadCandidates(candPath); err == nil {
		changed := false
		for i := range candidates {
			if candidates[i].Kind == "subject" && strings.EqualFold(candidates[i].Label, label) && !candidates[i].Promoted {
				candidates[i].Promoted = true
				changed = true
			}
		}
		if changed {
			if err := saveCandidates(candPath, candidates); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to mark candidate %q promoted: %v\n", label, err)
			}
		}
	}

	rareNote := ""
	if rare {
		rareNote = " (rare -- only occasionally eligible for auto-pick)"
	}
	fmt.Printf("promoted subject %q%s\n", label, rareNote)
	return 0
}
