// cmd/saga_query.go — emily saga which-doc-governs / status / conflicts
//
// S143-04's "deterministic parts first" query surface, built on the same
// parsed claim ledger `saga lint` already produces. None of this proposes,
// promotes, or edits anything — SAGA never adjudicates (DOC-102 §5). Semantic
// conflict detection (comparing claim *meaning* across docs) is explicitly
// out of scope here — that's S143-05, gated on NORN (S141-01/02). What's
// implemented is structural: given the authority/supersedes/amends graph
// `lint` already validates, resolve which document currently governs a given
// claim, and surface two real structural issues lint doesn't already catch as
// hard errors (see conflictsReport doc comment below).
package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// claimIndex is the claim ledger built once per corpus load: which doc
// declared each claim, and a lookup from doc_id to the parsed doc.
type claimIndex struct {
	byDocID map[string]*SagaDoc
	ownerOf map[string]string // claim ID -> doc_id that first declared it
	docIDs  []string          // sorted, for deterministic iteration
}

func buildClaimIndex(docs []SagaDoc) *claimIndex {
	idx := &claimIndex{byDocID: make(map[string]*SagaDoc), ownerOf: make(map[string]string)}
	for i := range docs {
		d := &docs[i]
		idx.byDocID[d.DocID] = d
		idx.docIDs = append(idx.docIDs, d.DocID)
		for _, c := range d.Claims {
			if _, exists := idx.ownerOf[c.ID]; !exists {
				idx.ownerOf[c.ID] = d.DocID
			}
		}
	}
	sort.Strings(idx.docIDs)
	return idx
}

// governs resolves which doc_id currently governs claimID by walking forward
// through the amends/supersedes graph from the claim's original owner: a
// doc that names (owner, claimID) in its own amends list governs it next
// (partial supersession, checked first); failing that, a doc that fully
// supersedes the current governor takes over. Returns the terminal doc_id,
// the resolution chain (owner first), and false if claimID isn't declared
// anywhere. Cycle-safe via a visited set (a real cycle is itself a lint bug,
// not something this should hang on).
func (idx *claimIndex) governs(claimID string) (docID string, chain []string, ok bool) {
	owner, exists := idx.ownerOf[claimID]
	if !exists {
		return "", nil, false
	}
	current := owner
	chain = []string{current}
	visited := map[string]bool{current: true}

	for {
		moved := false

		// Partial supersession: some doc's amends list explicitly claims
		// this claim ID away from `current`.
		for _, id := range idx.docIDs {
			d := idx.byDocID[id]
			for _, am := range d.Amends {
				if am.Doc != current {
					continue
				}
				if containsStr(am.Claims, claimID) && !visited[id] {
					current = id
					chain = append(chain, current)
					visited[id] = true
					moved = true
				}
				break
			}
			if moved {
				break
			}
		}
		if moved {
			continue
		}

		// Full supersession: some doc supersedes `current` outright and
		// didn't already carve this claim elsewhere (checked above).
		for _, id := range idx.docIDs {
			d := idx.byDocID[id]
			for _, sup := range d.Supersedes {
				if sup == current && !visited[id] {
					current = id
					chain = append(chain, current)
					visited[id] = true
					moved = true
				}
			}
			if moved {
				break
			}
		}
		if !moved {
			break
		}
	}
	return current, chain, true
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func loadDocsForQuery(dirFlag string) ([]SagaDoc, []string, string) {
	dir := dirFlag
	if dir == "" {
		cfg, err := config.Resolve()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(1)
		}
		dir = filepath.Join(cfg.EmilyRoot, "docs", "hq-specs")
	}
	docs, errs := loadSagaDocs(dir)
	return docs, errs, dir
}

func runSagaWhichDocGoverns(args []string) int {
	fs := flag.NewFlagSet("saga which-doc-governs", flag.ContinueOnError)
	specDir := fs.String("dir", "", "spec directory (default: EMILY/docs/hq-specs)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: emily saga which-doc-governs <claim-id>")
		return 1
	}
	claimID := fs.Arg(0)

	docs, parseErrs, dir := loadDocsForQuery(*specDir)
	if len(parseErrs) > 0 {
		for _, e := range parseErrs {
			fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
		}
	}

	idx := buildClaimIndex(docs)
	governor, chain, ok := idx.governs(claimID)
	if !ok {
		fmt.Printf("◈ %q is not declared by any document in %s\n", claimID, dir)
		return 1
	}

	fmt.Printf("◈ %s governs %q\n", governor, claimID)
	if len(chain) > 1 {
		fmt.Printf("  chain: %s\n", joinChain(chain))
	}
	return 0
}

func joinChain(chain []string) string {
	out := chain[0]
	for _, c := range chain[1:] {
		out += " → " + c
	}
	return out
}

func runSagaStatus(args []string) int {
	fs := flag.NewFlagSet("saga status", flag.ContinueOnError)
	specDir := fs.String("dir", "", "spec directory (default: EMILY/docs/hq-specs)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	docs, parseErrs, dir := loadDocsForQuery(*specDir)
	for _, e := range parseErrs {
		fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
	}
	idx := buildClaimIndex(docs)

	if fs.NArg() == 1 {
		return printDocStatus(idx, fs.Arg(0))
	}

	fmt.Printf("\n◈ SAGA STATUS | %s\n\n", dir)
	fmt.Printf("  %-14s %-11s %8s %8s\n", "DOC_ID", "AUTHORITY", "CLAIMS", "RETAINED")
	for _, id := range idx.docIDs {
		d := idx.byDocID[id]
		retained := 0
		for _, c := range d.Claims {
			if gov, _, ok := idx.governs(c.ID); ok && gov == id {
				retained++
			}
		}
		fmt.Printf("  %-14s %-11s %8d %8d\n", id, d.Authority, len(d.Claims), retained)
	}
	fmt.Println()
	return 0
}

func printDocStatus(idx *claimIndex, docID string) int {
	d, ok := idx.byDocID[docID]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown doc_id %q\n", docID)
		return 1
	}
	fmt.Printf("\n◈ %s | authority: %s\n\n", d.DocID, d.Authority)
	if len(d.Claims) == 0 {
		fmt.Println("  no claims declared.")
		return 0
	}
	for _, c := range d.Claims {
		gov, _, _ := idx.governs(c.ID)
		note := ""
		if gov != docID {
			note = fmt.Sprintf("  (now governed by %s)", gov)
		}
		fmt.Printf("  %-24s %-6s %-10s%s\n", c.ID, c.Type, c.RealityBinding, note)
	}
	fmt.Println()
	return 0
}

// authorityRank orders the lifecycle axis for the downgrade check below.
// Not a quality score outside that one comparison.
var authorityRank = map[string]int{"draft": 0, "golden": 1, "amended": 2, "superseded": 3}

func runSagaConflicts(args []string) int {
	fs := flag.NewFlagSet("saga conflicts", flag.ContinueOnError)
	specDir := fs.String("dir", "", "spec directory (default: EMILY/docs/hq-specs)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	docs, parseErrs, dir := loadDocsForQuery(*specDir)
	for _, e := range parseErrs {
		fmt.Fprintf(os.Stderr, "parse error: %s\n", e)
	}

	conflicts := findStructuralConflicts(docs)
	fmt.Printf("\n◈ SAGA CONFLICTS | %s\n", dir)
	fmt.Println("  (structural only — semantic conflict detection is S143-05, not built)")
	fmt.Println()
	if len(conflicts) == 0 {
		fmt.Println("  none found.")
		return 0
	}
	for _, c := range conflicts {
		fmt.Printf("  ✗ %s\n", c)
	}
	fmt.Println()
	return 1
}

// findStructuralConflicts checks two real classes lint's hard-error pass
// (lintSagaDocs) does not already cover:
//
//  1. Dangling claim reference: a doc's amends list names a claim ID that
//     isn't actually declared anywhere in the corpus — claiming to override
//     a claim that doesn't exist. lint only checks the *doc* reference in an
//     amends entry, not each individual claim ID inside it.
//  2. Authority downgrade on governance: a claim originally declared by a
//     golden (or higher-lifecycle) doc is now governed, via amendment, by a
//     doc whose own authority ranks lower (e.g. still draft) — the claim is
//     currently backed by a less-authoritative document than its own
//     history implies, worth a human look even though it's not a hard error.
func findStructuralConflicts(docs []SagaDoc) []string {
	idx := buildClaimIndex(docs)

	allClaimIDs := make(map[string]bool)
	for _, id := range idx.docIDs {
		for _, c := range idx.byDocID[id].Claims {
			allClaimIDs[c.ID] = true
		}
	}

	var out []string
	for _, id := range idx.docIDs {
		d := idx.byDocID[id]
		for _, am := range d.Amends {
			for _, claimID := range am.Claims {
				if !allClaimIDs[claimID] {
					out = append(out, fmt.Sprintf("%s: amends %s for claim %q, but no document declares that claim", d.Path, am.Doc, claimID))
				}
			}
		}
	}

	for claimID, ownerID := range idx.ownerOf {
		gov, _, _ := idx.governs(claimID)
		if gov == ownerID {
			continue
		}
		owner := idx.byDocID[ownerID]
		govDoc := idx.byDocID[gov]
		if authorityRank[owner.Authority] >= authorityRank["golden"] && authorityRank[govDoc.Authority] < authorityRank["golden"] {
			out = append(out, fmt.Sprintf("%s: originally %s in %s, now governed by %s (authority: %s) — claim currently has no golden-or-higher backing", claimID, owner.Authority, ownerID, gov, govDoc.Authority))
		}
	}

	sort.Strings(out)
	return out
}
