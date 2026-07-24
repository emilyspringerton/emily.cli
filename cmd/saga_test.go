package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSagaFrontmatter_Basic(t *testing.T) {
	content := `---
doc_id: DOC-102
authority: draft
supersedes: []
amends: []
claims:
  - id: DOC-102.POL-1
    type: POL
    reality_binding: specified
  - id: DOC-102.INV-1
    type: INV
    reality_binding: verified
---
# Title
body text
`
	doc, err := parseSagaFrontmatter(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.DocID != "DOC-102" {
		t.Errorf("DocID = %q, want DOC-102", doc.DocID)
	}
	if doc.Authority != "draft" {
		t.Errorf("Authority = %q, want draft", doc.Authority)
	}
	if len(doc.Claims) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(doc.Claims))
	}
	if doc.Claims[0].ID != "DOC-102.POL-1" || doc.Claims[0].Type != "POL" || doc.Claims[0].RealityBinding != "specified" {
		t.Errorf("claim 0 = %+v", doc.Claims[0])
	}
	if doc.Claims[1].RealityBinding != "verified" {
		t.Errorf("claim 1 reality_binding = %q, want verified", doc.Claims[1].RealityBinding)
	}
}

func TestParseSagaFrontmatter_SupersedesAndAmends(t *testing.T) {
	content := `---
doc_id: DOC-103
authority: golden
supersedes: [DOC-102]
amends:
  - doc: DOC-101
    claims: [DOC-101.INV-1, DOC-101.BEH-2]
claims: []
---
`
	doc, err := parseSagaFrontmatter(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Supersedes) != 1 || doc.Supersedes[0] != "DOC-102" {
		t.Errorf("Supersedes = %v", doc.Supersedes)
	}
	if len(doc.Amends) != 1 || doc.Amends[0].Doc != "DOC-101" {
		t.Fatalf("Amends = %+v", doc.Amends)
	}
	if len(doc.Amends[0].Claims) != 2 {
		t.Errorf("Amends[0].Claims = %v", doc.Amends[0].Claims)
	}
}

func TestParseSagaFrontmatter_MissingDocID(t *testing.T) {
	content := `---
authority: draft
claims: []
---
`
	_, err := parseSagaFrontmatter(content)
	if err == nil {
		t.Fatal("expected error for missing doc_id, got nil")
	}
}

func TestParseSagaFrontmatter_UnclosedBlock(t *testing.T) {
	content := "---\ndoc_id: X\n"
	_, err := parseSagaFrontmatter(content)
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter, got nil")
	}
}

func TestLintSagaDocs_InvalidAuthority(t *testing.T) {
	docs := []SagaDoc{{Path: "a.md", DocID: "A", Authority: "bogus"}}
	errs := lintSagaDocs(docs)
	if len(errs) == 0 {
		t.Fatal("expected a lint error for invalid authority")
	}
}

func TestLintSagaDocs_ClaimTypeAndBindingValidation(t *testing.T) {
	docs := []SagaDoc{{
		Path: "a.md", DocID: "A", Authority: "draft",
		Claims: []SagaClaim{{ID: "A.XXX-1", Type: "XXX", RealityBinding: "bogus"}},
	}}
	errs := lintSagaDocs(docs)
	if len(errs) != 2 {
		t.Fatalf("expected 2 lint errors (bad type + bad reality_binding), got %d: %v", len(errs), errs)
	}
}

func TestLintSagaDocs_ClaimPrefixMismatch(t *testing.T) {
	docs := []SagaDoc{{
		Path: "a.md", DocID: "A", Authority: "draft",
		Claims: []SagaClaim{{ID: "B.INV-1", Type: "INV", RealityBinding: "specified"}},
	}}
	errs := lintSagaDocs(docs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "does not start with owning doc_id prefix") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a prefix-mismatch error, got: %v", errs)
	}
}

func TestLintSagaDocs_ClaimIDCollision(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "draft", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
		{Path: "a2.md", DocID: "A", Authority: "draft", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
	}
	errs := lintSagaDocs(docs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "duplicate doc_id") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a duplicate doc_id error (same doc_id used twice), got: %v", errs)
	}
}

func TestLintSagaDocs_DanglingSupersedes(t *testing.T) {
	docs := []SagaDoc{{
		Path: "a.md", DocID: "A", Authority: "draft", Supersedes: []string{"NONEXISTENT"},
	}}
	errs := lintSagaDocs(docs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "dangling doc_id") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a dangling-supersedes error, got: %v", errs)
	}
}

func TestLintSagaDocs_UnenumeratedAmends(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "draft"},
		{Path: "b.md", DocID: "B", Authority: "draft", Amends: []SagaAmends{{Doc: "A", Claims: nil}}},
	}
	errs := lintSagaDocs(docs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "unenumerated inheritance") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unenumerated-inheritance error, got: %v", errs)
	}
}

// TestLintSagaDocs_OrphanGolden verifies the DOC-102 §8 "orphan goldens" check:
// a document fully superseded by another must not still claim golden status.
func TestLintSagaDocs_OrphanGolden(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden"},
		{Path: "b.md", DocID: "B", Authority: "golden", Supersedes: []string{"A"}},
	}
	errs := lintSagaDocs(docs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "orphan golden") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an orphan-golden error, got: %v", errs)
	}
}

// TestLintSagaDocs_PartialSupersessionNotOrphan verifies that a partial
// supersession (A named in B's amends list too) does NOT trip the orphan
// check -- A legitimately keeps some claims live under its own golden status.
func TestLintSagaDocs_PartialSupersessionNotOrphan(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
		{
			Path: "b.md", DocID: "B", Authority: "golden",
			Supersedes: []string{"A"},
			Amends:     []SagaAmends{{Doc: "A", Claims: []string{"A.INV-1"}}},
		},
	}
	errs := lintSagaDocs(docs)
	for _, e := range errs {
		if strings.Contains(e, "orphan golden") {
			t.Errorf("did not expect an orphan-golden error for a partial supersession, got: %v", errs)
		}
	}
}

func TestLoadSagaDocs_RealSpecDirectory(t *testing.T) {
	// Integration check against the actual EMILY/docs/hq-specs directory --
	// this is the corpus S143-01 retrofits. Skips gracefully if this repo
	// layout isn't present (e.g. running the test in isolation elsewhere).
	dir := filepath.Join("..", "..", "EMILY", "docs", "hq-specs")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("EMILY/docs/hq-specs not found at %s, skipping integration check: %v", dir, err)
	}
	docs, parseErrs := loadSagaDocs(dir)
	if len(parseErrs) != 0 {
		t.Errorf("unexpected parse errors in real corpus: %v", parseErrs)
	}
	if len(docs) == 0 {
		t.Error("expected at least one parsed doc in the real hq-specs corpus")
	}
	lintErrs := lintSagaDocs(docs)
	if len(lintErrs) != 0 {
		t.Errorf("unexpected lint errors in real corpus: %v", lintErrs)
	}
}
