// cmd/saga.go — emily saga lint
//
// SAGA (HQ-SPEC-DOC-102) is the documentation curation lifecycle: a claim
// ledger reconciling what the specs say (intent), what's bound (books), and
// what the software does (reality). This file implements build-sequence
// step 1 only (DOC-102 §9): the frontmatter schema's lint checks. See
// EMILY/docs/hq-specs/SAGA_SCHEMA.md for the schema definition this parses.
//
// Deliberately stdlib-only: the frontmatter grammar is a restricted YAML
// subset, hand-parsed rather than pulling in a YAML library, matching this
// codebase's stdlib-first convention elsewhere (IDUNA's WP HTTP API rule,
// REDGARDEN's hand-rolled http_client.h, etc).
package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

var validAuthority = map[string]bool{
	"draft": true, "golden": true, "amended": true, "superseded": true,
}
var validClaimType = map[string]bool{
	"INV": true, "BEH": true, "IFC": true, "MET": true, "POL": true, "NAR": true,
}
var validRealityBinding = map[string]bool{
	"specified": true, "building": true, "running": true, "verified": true, "diverged": true,
}

// SagaClaim is one claim declared in a document's frontmatter.
type SagaClaim struct {
	ID             string
	Type           string
	RealityBinding string
}

// SagaAmends is one partial-supersession entry.
type SagaAmends struct {
	Doc    string
	Claims []string
}

// SagaDoc is one HQ-SPEC document's parsed frontmatter.
type SagaDoc struct {
	Path       string
	DocID      string
	Authority  string
	Supersedes []string
	Amends     []SagaAmends
	Claims     []SagaClaim
}

// RunSaga dispatches emily saga subcommands.
func RunSaga(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily saga <lint|gaps|which-doc-governs|status|conflicts>")
		return 1
	}
	switch args[0] {
	case "lint":
		return runSagaLint(args[1:])
	case "gaps":
		return runSagaGaps(args[1:])
	case "which-doc-governs":
		return runSagaWhichDocGoverns(args[1:])
	case "status":
		return runSagaStatus(args[1:])
	case "conflicts":
		return runSagaConflicts(args[1:])
	}
	fmt.Fprintf(os.Stderr, "emily saga: unknown subcommand %q — try: lint, gaps, which-doc-governs, status, conflicts\n", args[0])
	return 1
}

func runSagaLint(args []string) int {
	fs := flag.NewFlagSet("saga lint", flag.ContinueOnError)
	specDir := fs.String("dir", "", "spec directory (default: EMILY/docs/hq-specs)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	dir := *specDir
	if dir == "" {
		cfg, err := config.Resolve()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			return 1
		}
		dir = filepath.Join(cfg.EmilyRoot, "docs", "hq-specs")
	}

	docs, parseErrs := loadSagaDocs(dir)
	fmt.Printf("\n◈ SAGA LINT | %s\n", dir)
	fmt.Printf("  documents parsed: %d\n\n", len(docs))

	var errs []string
	errs = append(errs, parseErrs...)
	errs = append(errs, lintSagaDocs(docs)...)

	if len(errs) == 0 {
		fmt.Println("  ALL CLEAN — no lint errors.")
		return 0
	}

	fmt.Printf("  %d lint error(s):\n\n", len(errs))
	for _, e := range errs {
		fmt.Printf("  ✗ %s\n", e)
	}
	fmt.Println()
	return 1
}

// lintSagaDocs runs the corpus-wide checks from SAGA_SCHEMA.md §3 against an
// already-parsed set of documents (per-document parse errors are collected
// separately by loadSagaDocs).
func lintSagaDocs(docs []SagaDoc) []string {
	var errs []string

	byDocID := make(map[string]*SagaDoc, len(docs))
	for i := range docs {
		d := &docs[i]
		if existing, ok := byDocID[d.DocID]; ok {
			errs = append(errs, fmt.Sprintf("duplicate doc_id %q: %s and %s", d.DocID, existing.Path, d.Path))
			continue
		}
		byDocID[d.DocID] = d
	}

	claimOwner := make(map[string]string) // claim ID -> path that declared it first

	for _, d := range docs {
		if !validAuthority[d.Authority] {
			errs = append(errs, fmt.Sprintf("%s: invalid authority %q", d.Path, d.Authority))
		}

		for _, sup := range d.Supersedes {
			if _, ok := byDocID[sup]; !ok {
				errs = append(errs, fmt.Sprintf("%s: supersedes dangling doc_id %q", d.Path, sup))
			}
		}

		for _, am := range d.Amends {
			if _, ok := byDocID[am.Doc]; !ok {
				errs = append(errs, fmt.Sprintf("%s: amends dangling doc_id %q", d.Path, am.Doc))
			}
			if len(am.Claims) == 0 {
				errs = append(errs, fmt.Sprintf("%s: amends %q with no enumerated claims (unenumerated inheritance)", d.Path, am.Doc))
			}
		}

		for _, c := range d.Claims {
			if !validClaimType[c.Type] {
				errs = append(errs, fmt.Sprintf("%s: claim %q has invalid type %q", d.Path, c.ID, c.Type))
			}
			if !validRealityBinding[c.RealityBinding] {
				errs = append(errs, fmt.Sprintf("%s: claim %q has invalid reality_binding %q", d.Path, c.ID, c.RealityBinding))
			}
			wantPrefix := d.DocID + "."
			if !strings.HasPrefix(c.ID, wantPrefix) {
				errs = append(errs, fmt.Sprintf("%s: claim %q does not start with owning doc_id prefix %q", d.Path, c.ID, wantPrefix))
			}
			if owner, seen := claimOwner[c.ID]; seen {
				errs = append(errs, fmt.Sprintf("claim ID collision %q: declared in both %s and %s", c.ID, owner, d.Path))
			} else {
				claimOwner[c.ID] = d.Path
			}
		}
	}

	// Orphan goldens: if B fully supersedes A (A not named in B's amends list),
	// A must not still be golden.
	for _, b := range docs {
		for _, supID := range b.Supersedes {
			partial := false
			for _, am := range b.Amends {
				if am.Doc == supID {
					partial = true
					break
				}
			}
			if partial {
				continue
			}
			a, ok := byDocID[supID]
			if ok && a.Authority == "golden" {
				errs = append(errs, fmt.Sprintf("orphan golden: %s (doc_id %s) is fully superseded by %s but still marked golden", a.Path, a.DocID, b.Path))
			}
		}
	}

	sort.Strings(errs)
	return errs
}

// loadSagaDocs reads every .md file in dir and parses its frontmatter.
// Files with no frontmatter block are silently skipped (not every .md in a
// repo is a SAGA-governed spec); files that start a frontmatter block but
// fail to parse it are reported as errors.
func loadSagaDocs(dir string) ([]SagaDoc, []string) {
	var docs []SagaDoc
	var errs []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []string{fmt.Sprintf("read dir %s: %v", dir, err)}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: read failed: %v", path, err))
			continue
		}
		content := string(data)
		if !strings.HasPrefix(content, "---\n") {
			continue // no frontmatter — not a SAGA-governed doc (yet)
		}
		doc, err := parseSagaFrontmatter(content)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		doc.Path = path
		docs = append(docs, *doc)
	}
	return docs, errs
}

// parseSagaFrontmatter parses the restricted-YAML frontmatter block described
// in EMILY/docs/hq-specs/SAGA_SCHEMA.md §1-2.
func parseSagaFrontmatter(content string) (*SagaDoc, error) {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("frontmatter must start with a bare '---' line")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("frontmatter block never closed with a second '---' line")
	}
	body := lines[1:end]

	doc := &SagaDoc{}
	i := 0
	for i < len(body) {
		line := body[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "doc_id:"):
			doc.DocID = strings.TrimSpace(strings.TrimPrefix(trimmed, "doc_id:"))
			i++
		case strings.HasPrefix(trimmed, "authority:"):
			doc.Authority = strings.TrimSpace(strings.TrimPrefix(trimmed, "authority:"))
			i++
		case strings.HasPrefix(trimmed, "supersedes:"):
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "supersedes:"))
			if rest == "[]" || rest == "" {
				i++
			} else if strings.HasPrefix(rest, "[") {
				doc.Supersedes = parseInlineList(rest)
				i++
			} else {
				return nil, fmt.Errorf("line %d: supersedes must be '[]' or an inline '[...]' list", i+2)
			}
		case strings.HasPrefix(trimmed, "amends:"):
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "amends:"))
			i++
			if rest == "[]" || rest == "" {
				if rest == "[]" {
					continue
				}
				// Multi-line amends block: "- doc: X" / "  claims: [...]" pairs.
				for i < len(body) {
					entryLine := strings.TrimSpace(body[i])
					if !strings.HasPrefix(entryLine, "- doc:") {
						break
					}
					am := SagaAmends{Doc: strings.TrimSpace(strings.TrimPrefix(entryLine, "- doc:"))}
					i++
					if i < len(body) {
						claimsLine := strings.TrimSpace(body[i])
						if strings.HasPrefix(claimsLine, "claims:") {
							am.Claims = parseInlineList(strings.TrimSpace(strings.TrimPrefix(claimsLine, "claims:")))
							i++
						}
					}
					doc.Amends = append(doc.Amends, am)
				}
			}
		case strings.HasPrefix(trimmed, "claims:"):
			i++
			for i < len(body) {
				entryLine := strings.TrimSpace(body[i])
				if !strings.HasPrefix(entryLine, "- id:") {
					break
				}
				c := SagaClaim{ID: strings.TrimSpace(strings.TrimPrefix(entryLine, "- id:"))}
				i++
				if i < len(body) && strings.HasPrefix(strings.TrimSpace(body[i]), "type:") {
					c.Type = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body[i]), "type:"))
					i++
				}
				if i < len(body) && strings.HasPrefix(strings.TrimSpace(body[i]), "reality_binding:") {
					c.RealityBinding = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body[i]), "reality_binding:"))
					i++
				}
				doc.Claims = append(doc.Claims, c)
			}
		default:
			return nil, fmt.Errorf("line %d: unrecognized frontmatter line %q", i+2, line)
		}
	}

	if doc.DocID == "" {
		return nil, fmt.Errorf("missing required field doc_id")
	}
	if doc.Authority == "" {
		return nil, fmt.Errorf("missing required field authority")
	}
	return doc, nil
}

// parseInlineList parses "[a, b, c]" into ["a","b","c"]. Empty brackets "[]" -> nil.
func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
