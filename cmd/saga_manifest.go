// cmd/saga_manifest.go — emily saga gaps
//
// SAGA (HQ-SPEC-DOC-102) build-sequence step 2: saga.manifest.yaml format +
// a CI gaps report. A manifest is a versioned, per-repo file binding claim
// IDs (from the corpus saga.go already parses) to verification anchors —
// see EMILY/docs/hq-specs/HQ-SPEC-DOC-102-saga-curation-lifecycle.md §2 for
// the exact schema this mirrors.
//
// Two things a gaps report surfaces (DOC-102 §4, the divergence queue):
//   - claim-without-code (vaporware debt): a manifest entry with no bound
//     verification, or a `test:` anchor whose referenced file doesn't exist
//     in the target repo.
//   - code-without-claim (dark matter): a real, running surface with no
//     manifest entry naming it. Scoped honestly for this first pass: the
//     surface is read from the target repo's own README "## All processes"
//     table (a human-curated source, per DOC-102 §2's own
//     human_attestation precedent) rather than automated Go-AST scanning --
//     a naive text/token scan across the whole repo produces far too much
//     noise (struct-field "Type:" matches, heuristic-label strings, etc.,
//     confirmed by hand before settling on this approach) to be a trustworthy
//     signal. Automated exported-API/config-surface scanning is real,
//     separate follow-on work, not silently skipped.
package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// ManifestAnchor is one verification anchor bound to a claim: exactly one
// of Test/Telemetry/HumanAttestation is set, matching the tagged-union
// shape of DOC-102 §2's verified_by entries.
type ManifestAnchor struct {
	Kind string // "test", "telemetry", or "human_attestation"
	Ref  string
}

// ManifestEntry is one claim's binding in a repo's saga.manifest.yaml.
type ManifestEntry struct {
	Claim      string
	VerifiedBy []ManifestAnchor
}

// RunSagaGaps implements `emily saga gaps --repo <path> [--manifest <path>]`.
// SagaGapsReport is the machine-readable shape of `emily saga gaps --json` --
// used by IDUNA's Back Office divergence-queue page (S143-03) so it parses
// structured output instead of scraping the human-readable report below.
type SagaGapsReport struct {
	Repo            string   `json:"repo"`
	ManifestPath    string   `json:"manifest_path"`
	ManifestEntries int      `json:"manifest_entries"`
	Vaporware       []string `json:"vaporware"`
	BrokenRefs      []string `json:"broken_refs"`
	UnknownClaims   []string `json:"unknown_claims"`
	DarkMatter      []string `json:"dark_matter"`
	SurfaceScanErr  string   `json:"surface_scan_error,omitempty"`
	TotalGaps       int      `json:"total_gaps"`
}

func runSagaGaps(args []string) int {
	fs := flag.NewFlagSet("saga gaps", flag.ContinueOnError)
	repoPath := fs.String("repo", "", "repo root to check (required)")
	manifestPath := fs.String("manifest", "", "manifest path (default: <repo>/saga.manifest.yaml)")
	jsonOut := fs.Bool("json", false, "emit a SagaGapsReport as JSON instead of the human-readable report")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *repoPath == "" {
		fmt.Fprintln(os.Stderr, "usage: emily saga gaps --repo <path> [--manifest <path>]")
		return 1
	}
	repo := *repoPath

	mPath := *manifestPath
	if mPath == "" {
		mPath = filepath.Join(repo, "saga.manifest.yaml")
	}

	entries, err := loadManifest(mPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest %s: %v\n", mPath, err)
		return 1
	}

	// Sanity-check claim IDs against the real corpus so a typo'd claim ID
	// in the manifest doesn't silently bind to nothing -- reuses the same
	// doc-corpus parser saga.go's lint already has.
	cfg, cfgErr := config.Resolve()
	var knownClaims map[string]bool
	if cfgErr == nil {
		docs, _ := loadSagaDocs(filepath.Join(cfg.EmilyRoot, "docs", "hq-specs"))
		knownClaims = map[string]bool{}
		for _, d := range docs {
			for _, c := range d.Claims {
				knownClaims[c.ID] = true
			}
		}
	}

	if !*jsonOut {
		fmt.Printf("\n◈ SAGA GAPS | %s\n", repo)
		fmt.Printf("  manifest: %s (%d entries)\n\n", mPath, len(entries))
	}

	var unknownClaims, vaporware, brokenRefs []string
	boundText := manifestSearchText(entries)

	for _, e := range entries {
		if knownClaims != nil && !knownClaims[e.Claim] {
			unknownClaims = append(unknownClaims, e.Claim)
		}
		if len(e.VerifiedBy) == 0 {
			vaporware = append(vaporware, e.Claim)
			continue
		}
		for _, a := range e.VerifiedBy {
			if a.Kind == "test" && !testRefExists(repo, a.Ref) {
				brokenRefs = append(brokenRefs, fmt.Sprintf("%s: test ref %q not found in %s", e.Claim, a.Ref, repo))
			}
		}
	}

	surface, surfaceErr := extractProcessSurface(repo)
	var darkMatter []string
	if surfaceErr == nil {
		for _, s := range surface {
			if !strings.Contains(boundText, s) {
				darkMatter = append(darkMatter, s)
			}
		}
	}

	sort.Strings(unknownClaims)
	sort.Strings(vaporware)
	sort.Strings(brokenRefs)
	sort.Strings(darkMatter)

	total := len(vaporware) + len(brokenRefs) + len(unknownClaims) + len(darkMatter)

	if *jsonOut {
		report := SagaGapsReport{
			Repo: repo, ManifestPath: mPath, ManifestEntries: len(entries),
			Vaporware: vaporware, BrokenRefs: brokenRefs, UnknownClaims: unknownClaims,
			DarkMatter: darkMatter, TotalGaps: total,
		}
		if surfaceErr != nil {
			report.SurfaceScanErr = surfaceErr.Error()
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "encode json: %v\n", err)
			return 1
		}
		if total == 0 {
			return 0
		}
		return 1
	}

	printGapsList("claim-without-code (vaporware debt) — bound to nothing", vaporware)
	printGapsList("broken verification anchors — test ref doesn't exist", brokenRefs)
	printGapsList("claim IDs not found in the doc corpus (typo?)", unknownClaims)
	if surfaceErr != nil {
		fmt.Printf("  (surface scan skipped: %v)\n\n", surfaceErr)
	} else {
		printGapsList("code-without-claim (dark matter) — real surface, no manifest entry", darkMatter)
	}

	if total == 0 {
		fmt.Println("  ALL CLEAN — every manifest entry is bound, every surface item is claimed.")
		return 0
	}
	fmt.Printf("  %d total gap(s) found.\n\n", total)
	return 1
}

func printGapsList(label string, items []string) {
	if len(items) == 0 {
		fmt.Printf("  ✓ %s: none\n\n", label)
		return
	}
	fmt.Printf("  ✗ %s (%d):\n", label, len(items))
	for _, it := range items {
		fmt.Printf("      - %s\n", it)
	}
	fmt.Println()
}

// manifestSearchText concatenates every entry's claim ID and anchor refs
// into one blob, used as the substring-search space for dark-matter
// detection -- a surface item counts as "claimed" if its name literally
// appears in some bound anchor's reference text (e.g. a test name or
// telemetry metric mentioning the event type it covers).
func manifestSearchText(entries []ManifestEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Claim)
		sb.WriteString(" ")
		for _, a := range e.VerifiedBy {
			sb.WriteString(a.Ref)
			sb.WriteString(" ")
		}
	}
	return sb.String()
}

// testRefExists checks presence (not truth, per DOC-102 §2 -- "machine-
// checkable for presence; tests and probes are machine-checkable for
// truth" is a distinct, harder claim this doesn't attempt) of a `path::
// TestName` anchor: the referenced file must exist under repo, and the
// named test function must appear in it as a real Go test declaration.
func testRefExists(repo, ref string) bool {
	parts := strings.SplitN(ref, "::", 2)
	relPath := parts[0]
	fullPath := filepath.Join(repo, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return false
	}
	if len(parts) == 1 {
		return true // file-only reference, no specific test named
	}
	testName := parts[1]
	return strings.Contains(string(data), "func "+testName+"(")
}

// loadManifest parses a saga.manifest.yaml file. A missing file is not an
// error -- it just means zero claims are bound yet, itself a reportable
// finding (DOC-102 §2: "unbound claims are visible debt from day one"),
// not a tool failure.
func loadManifest(path string) ([]ManifestEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseManifest(string(data))
}

// parseManifest hand-parses the restricted YAML shape from DOC-102 §2:
//
//	- claim: FIN-098.INV-2
//	  verified_by:
//	    - test: ledger/payment_gate_test.go::TestNoUnapprovedMovement
//	    - telemetry: back_office.metric.unapproved_payment_attempts == 0
//	- claim: SIM-100.BEH-4
//	  verified_by: []
//
// Stdlib-only, same reasoning as saga.go's own frontmatter parser: this is
// a fixed two-level list shape, not general YAML.
func parseManifest(content string) ([]ManifestEntry, error) {
	lines := strings.Split(content, "\n")
	var entries []ManifestEntry
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		if !strings.HasPrefix(trimmed, "- claim:") {
			return nil, fmt.Errorf("line %d: expected '- claim:', got %q", i+1, lines[i])
		}
		entry := ManifestEntry{Claim: strings.TrimSpace(strings.TrimPrefix(trimmed, "- claim:"))}
		i++
		if i >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i]), "verified_by:") {
			return nil, fmt.Errorf("claim %q: missing verified_by", entry.Claim)
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "verified_by:"))
		i++
		if rest == "[]" {
			entries = append(entries, entry)
			continue
		}
		if rest != "" {
			return nil, fmt.Errorf("claim %q: verified_by must be '[]' or a nested list", entry.Claim)
		}
		for i < len(lines) {
			rawLine := lines[i]
			anchorLine := strings.TrimSpace(rawLine)
			if anchorLine == "" {
				i++
				continue
			}
			// Nested anchor lines are indented under `verified_by:`; a
			// top-level `- claim:` entry starts at column 0 (or at most
			// the same indentation as this block's own `- claim:` line)
			// and must end this claim's anchor list, not be consumed as
			// one more anchor.
			indent := len(rawLine) - len(strings.TrimLeft(rawLine, " "))
			if indent == 0 || !strings.HasPrefix(anchorLine, "- ") {
				break
			}
			body := strings.TrimPrefix(anchorLine, "- ")
			kind, ref, ok := splitAnchorKind(body)
			if !ok {
				return nil, fmt.Errorf("claim %q: unrecognized verified_by entry %q", entry.Claim, anchorLine)
			}
			entry.VerifiedBy = append(entry.VerifiedBy, ManifestAnchor{Kind: kind, Ref: ref})
			i++
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

var anchorKinds = []string{"test", "telemetry", "human_attestation"}

func splitAnchorKind(body string) (kind, ref string, ok bool) {
	for _, k := range anchorKinds {
		prefix := k + ":"
		if strings.HasPrefix(body, prefix) {
			return k, strings.TrimSpace(strings.TrimPrefix(body, prefix)), true
		}
	}
	return "", "", false
}

// tableTokenRE matches a plausible snake_case event-type/identifier token:
// lowercase, digits, underscores only -- naturally excludes `cmd/foo` paths
// (slash) and `dash-named` process/command names (dash), which is exactly
// the noise separation this needs given the README's own table content.
var tableTokenRE = regexp.MustCompile("`([a-z][a-z0-9_]*)`")

// extractProcessSurface reads repo/README.md's "## All processes" section
// and pulls every backtick-quoted snake_case token out of it -- the
// human-curated list of event types/signals this repo's own docs already
// claim to emit. Returns an error (not a panic) if the section can't be
// found, so callers can skip the dark-matter half of the report cleanly
// rather than silently returning zero findings.
func extractProcessSurface(repo string) ([]string, error) {
	path := filepath.Join(repo, "README.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)
	idx := strings.Index(content, "## All processes")
	if idx < 0 {
		return nil, fmt.Errorf("%s has no '## All processes' section", path)
	}
	section := content[idx:]
	// Stop at the next top-level heading so this doesn't pull tokens from
	// unrelated later sections.
	if next := strings.Index(section[len("## All processes"):], "\n## "); next >= 0 {
		section = section[:next+len("## All processes")]
	}

	seen := map[string]bool{}
	var out []string
	for _, m := range tableTokenRE.FindAllStringSubmatch(section, -1) {
		tok := m[1]
		if seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	sort.Strings(out)
	return out, nil
}
