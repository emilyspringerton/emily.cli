// cmd/promptoverse_candidates.go — persisted GPT-2-harvested candidate
// tags + `emily promptoverse promote`.
//
// Founder direction: "ensure we have the gpt2 tag promotion pipeline
// setup" + "that page has the tags suggested from promotion harvested
// from gpt2." brainstorm (promptoverse_gpt2.go) used to just print
// candidates and discard them; this file gives them somewhere durable to
// live so a later page (and a human) can look at what's been harvested
// and decide what's worth promoting into a real style.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const promptoverseCandidatesFileName = "promptoverse-candidate-tags.json"

// candidateTag is one GPT-2-harvested candidate not yet promoted (or
// already promoted -- Promoted stays true rather than deleting the
// record, so the page can still show it as provenance/history). Kind
// distinguishes a style candidate from a subject candidate (S176-24+:
// "copy all those same patterns for topic discovery") sharing this one
// file/page section rather than needing a second parallel set of types --
// empty Kind on an older record (written before this field existed) reads
// as "style", the only kind that existed then.
type candidateTag struct {
	Label       string    `json:"label"`
	Kind        string    `json:"kind"` // "style" | "subject"
	Seed        string    `json:"seed"` // the seed prompt that produced it, truncated
	HarvestedAt time.Time `json:"harvested_at"`
	Promoted    bool      `json:"promoted,omitempty"`
}

func candidatesPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseCandidatesFileName)
}

func loadCandidates(path string) ([]candidateTag, error) {
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
	var out []candidateTag
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode candidate tags: %w", err)
	}
	return out, nil
}

func saveCandidates(path string, candidates []candidateTag) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(candidates, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// recordCandidates appends newly-harvested labels not already tracked
// (case-insensitive), skipping anything already in the live style pool.
// Best-effort: a persistence failure here is a warning printed by the
// caller, not a reason to fail the whole brainstorm run.
func recordCandidates(path string, kind string, labels []string, seed string, existingPoolLabels map[string]bool) (added int, err error) {
	existing, err := loadCandidates(path)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]bool, len(existing))
	for _, c := range existing {
		if candidateKind(c) == kind {
			seen[strings.ToLower(c.Label)] = true
		}
	}
	now := time.Now().UTC()
	for _, label := range labels {
		key := strings.ToLower(label)
		if seen[key] || existingPoolLabels[key] {
			continue
		}
		seen[key] = true
		existing = append(existing, candidateTag{Label: label, Kind: kind, Seed: truncateForDisplay(seed, 200), HarvestedAt: now})
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, saveCandidates(path, existing)
}

// candidateKind reads c.Kind, treating an empty value (records written
// before Kind existed) as "style" -- the only kind that existed then.
func candidateKind(c candidateTag) string {
	if c.Kind == "" {
		return "style"
	}
	return c.Kind
}

func runPromptOVersePromote(args []string) int {
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
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse promote <label> [--rare]")
		return 1
	}
	label := strings.TrimSpace(rest[0])
	if label == "" {
		fmt.Fprintln(os.Stderr, "emily promptoverse promote: label must not be empty")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	discoveredPath := discoveredStylesPath(cfg)
	discovered, err := loadDiscoveredStyles(discoveredPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", err)
		return 1
	}
	pool := combinedStylePool(discovered)
	if _, ok := styleByLabelInPool(pool, label); ok {
		fmt.Printf("%q is already a real style -- nothing to promote\n", label)
		return 0
	}

	token, tokErr := gcloudAccessToken()
	if tokErr != nil {
		fmt.Fprintf(os.Stderr, "gcloud auth: %v\n", tokErr)
		return 2
	}
	newStyle, expErr := expandNamedStyle(token, label, "(direct promotion, no triggering subject)")
	if expErr != nil {
		fmt.Fprintf(os.Stderr, "failed to create style %q: %v\n", label, expErr)
		return 1
	}
	newStyle.Rare = rare
	if err := appendDiscoveredStyle(discoveredPath, *newStyle); err != nil {
		fmt.Fprintf(os.Stderr, "failed to persist promoted style %q: %v\n", label, err)
		return 1
	}

	// Mark any matching candidate record as promoted so the page reflects
	// it without needing to cross-reference the discovered-styles file.
	candPath := candidatesPath(cfg)
	if candidates, err := loadCandidates(candPath); err == nil {
		changed := false
		for i := range candidates {
			if candidateKind(candidates[i]) == "style" && strings.EqualFold(candidates[i].Label, label) && !candidates[i].Promoted {
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
		rareNote = " (rare -- only occasionally eligible for selection)"
	}
	fmt.Printf("promoted %q into the registry%s\n", label, rareNote)
	return 0
}
