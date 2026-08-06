package cmd

import (
	"strings"
	"testing"
)

func TestClaimIndex_GovernsSelf(t *testing.T) {
	docs := []SagaDoc{
		{DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "verified"}}},
	}
	idx := buildClaimIndex(docs)
	gov, chain, ok := idx.governs("A.INV-1")
	if !ok {
		t.Fatal("expected claim to resolve")
	}
	if gov != "A" {
		t.Errorf("governs = %q, want A", gov)
	}
	if len(chain) != 1 || chain[0] != "A" {
		t.Errorf("chain = %v, want [A]", chain)
	}
}

func TestClaimIndex_GovernsUnknownClaim(t *testing.T) {
	idx := buildClaimIndex([]SagaDoc{{DocID: "A", Authority: "golden"}})
	_, _, ok := idx.governs("A.INV-99")
	if ok {
		t.Error("expected unknown claim to not resolve")
	}
}

func TestClaimIndex_PartialAmendMovesGovernance(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
		{Path: "b.md", DocID: "B", Authority: "golden", Amends: []SagaAmends{{Doc: "A", Claims: []string{"A.INV-1"}}}},
	}
	idx := buildClaimIndex(docs)
	gov, chain, ok := idx.governs("A.INV-1")
	if !ok {
		t.Fatal("expected claim to resolve")
	}
	if gov != "B" {
		t.Errorf("governs = %q, want B (amended away from A)", gov)
	}
	if len(chain) != 2 || chain[0] != "A" || chain[1] != "B" {
		t.Errorf("chain = %v, want [A B]", chain)
	}
}

func TestClaimIndex_FullSupersessionMovesGovernance(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "superseded", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
		{Path: "b.md", DocID: "B", Authority: "golden", Supersedes: []string{"A"}},
	}
	idx := buildClaimIndex(docs)
	gov, _, ok := idx.governs("A.INV-1")
	if !ok {
		t.Fatal("expected claim to resolve")
	}
	if gov != "B" {
		t.Errorf("governs = %q, want B (fully superseded A)", gov)
	}
}

func TestClaimIndex_PartialAmendBeatsFullSupersession(t *testing.T) {
	// B fully supersedes A but carves out A.INV-1 to C via its own amends.
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "superseded", Claims: []SagaClaim{
			{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"},
			{ID: "A.INV-2", Type: "INV", RealityBinding: "specified"},
		}},
		{Path: "b.md", DocID: "B", Authority: "golden", Supersedes: []string{"A"}},
		{Path: "c.md", DocID: "C", Authority: "golden", Amends: []SagaAmends{{Doc: "A", Claims: []string{"A.INV-1"}}}},
	}
	idx := buildClaimIndex(docs)
	gov1, _, _ := idx.governs("A.INV-1")
	if gov1 != "C" {
		t.Errorf("A.INV-1 governs = %q, want C (carved out by amendment)", gov1)
	}
	gov2, _, _ := idx.governs("A.INV-2")
	if gov2 != "B" {
		t.Errorf("A.INV-2 governs = %q, want B (fully superseded, untouched by amends)", gov2)
	}
}

func TestFindStructuralConflicts_DanglingAmendedClaim(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "specified"}}},
		{Path: "b.md", DocID: "B", Authority: "golden", Amends: []SagaAmends{{Doc: "A", Claims: []string{"A.INV-1", "A.INV-99"}}}},
	}
	conflicts := findStructuralConflicts(docs)
	found := false
	for _, c := range conflicts {
		if strings.Contains(c, "A.INV-99") && strings.Contains(c, "no document declares") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a dangling-claim-reference conflict for A.INV-99, got: %v", conflicts)
	}
}

func TestFindStructuralConflicts_AuthorityDowngrade(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "verified"}}},
		{Path: "b.md", DocID: "B", Authority: "draft", Amends: []SagaAmends{{Doc: "A", Claims: []string{"A.INV-1"}}}},
	}
	conflicts := findStructuralConflicts(docs)
	found := false
	for _, c := range conflicts {
		if strings.Contains(c, "A.INV-1") && strings.Contains(c, "no golden-or-higher backing") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an authority-downgrade conflict for A.INV-1, got: %v", conflicts)
	}
}

func TestFindStructuralConflicts_CleanCorpusNoConflicts(t *testing.T) {
	docs := []SagaDoc{
		{Path: "a.md", DocID: "A", Authority: "golden", Claims: []SagaClaim{{ID: "A.INV-1", Type: "INV", RealityBinding: "verified"}}},
	}
	conflicts := findStructuralConflicts(docs)
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts in a clean single-doc corpus, got: %v", conflicts)
	}
}

