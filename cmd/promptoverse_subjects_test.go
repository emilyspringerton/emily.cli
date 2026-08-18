package cmd

import (
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func TestSubjectPool_DedupesAndCombinesSources(t *testing.T) {
	existing := []iduna.PromptOVerseNodeSummary{
		{Subject: "princess", Label: "gladiator"},
		{Subject: "princess", Label: "anime"}, // same subject again -- must not duplicate
		{Subject: "a duck", Label: "claymation"},
		{Subject: "", Label: "underwater"}, // blank subject must be skipped
	}
	discovered := []discoveredSubject{
		{Label: "Aphrodite"},
		{Label: "princess"}, // already covered by published usage -- must not duplicate
	}
	pool := subjectPool(existing, discovered)
	want := map[string]bool{"princess": true, "a duck": true, "Aphrodite": true}
	if len(pool) != len(want) {
		t.Fatalf("expected %d distinct subjects, got %d: %v", len(want), len(pool), pool)
	}
	for _, s := range pool {
		if !want[s] {
			t.Errorf("unexpected subject %q in pool", s)
		}
	}
}

func TestRareSubjectLabels_OnlyRareFlagged(t *testing.T) {
	discovered := []discoveredSubject{
		{Label: "a lighthouse keeper", Rare: true},
		{Label: "a duck"},
	}
	labels := rareSubjectLabels(discovered)
	if !labels["a lighthouse keeper"] {
		t.Error("expected the rare-flagged subject to be in the set")
	}
	if labels["a duck"] {
		t.Error("expected the non-rare subject to NOT be in the set")
	}
}

func TestSelectSubject_SkipsExcluded(t *testing.T) {
	pool := []string{"a", "b", "c"}
	exclude := map[string]bool{"a": true, "b": true}
	rng := rand.New(rand.NewSource(1))
	got, ok := selectSubject(pool, exclude, map[string]int{}, rng)
	if !ok || got != "c" {
		t.Errorf("expected the only non-excluded subject 'c', got %q ok=%v", got, ok)
	}
}

func TestSelectSubject_AllExcludedReturnsFalse(t *testing.T) {
	pool := []string{"a", "b"}
	exclude := map[string]bool{"a": true, "b": true}
	rng := rand.New(rand.NewSource(1))
	_, ok := selectSubject(pool, exclude, map[string]int{}, rng)
	if ok {
		t.Error("expected ok=false when every subject is excluded")
	}
}

func TestSelectSubject_FavorsLeastUsedButNotAlways(t *testing.T) {
	pool := []string{"overused", "fresh"}
	usage := map[string]int{"overused": 50, "fresh": 0}
	const trials = 200
	freshWins, overusedWinsAtLeastOnce := 0, false
	for i := 0; i < trials; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		got, ok := selectSubject(pool, map[string]bool{}, usage, rng)
		if !ok {
			t.Fatal("expected a pick")
		}
		if got == "fresh" {
			freshWins++
		} else {
			overusedWinsAtLeastOnce = true
		}
	}
	if freshWins < trials*7/10 {
		t.Errorf("expected the under-used subject to win a strong majority, won %d/%d", freshWins, trials)
	}
	if !overusedWinsAtLeastOnce {
		t.Error("expected the over-used subject to occasionally win too, got the fresh one every trial")
	}
}

func TestParseSubjectProposalJSON_Decline(t *testing.T) {
	ds, err := parseSubjectProposalJSON(`{"propose": false}`, []string{"a duck"})
	if err != nil {
		t.Fatalf("a decline should not be an error: %v", err)
	}
	if ds != nil {
		t.Errorf("expected nil on decline, got %+v", ds)
	}
}

func TestParseSubjectProposalJSON_ValidProposal(t *testing.T) {
	ds, err := parseSubjectProposalJSON(`{"propose": true, "subject": "a lighthouse keeper"}`, []string{"a duck"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds == nil || ds.Label != "a lighthouse keeper" {
		t.Errorf("expected the proposed subject, got %+v", ds)
	}
}

func TestParseSubjectProposalJSON_RejectsDuplicate(t *testing.T) {
	_, err := parseSubjectProposalJSON(`{"propose": true, "subject": "A Duck"}`, []string{"a duck"})
	if err == nil {
		t.Fatal("expected an error for a proposal duplicating an existing subject (case-insensitive)")
	}
}

func TestParseSubjectProposalJSON_RejectsEmptySubject(t *testing.T) {
	_, err := parseSubjectProposalJSON(`{"propose": true, "subject": "   "}`, []string{})
	if err == nil {
		t.Fatal("expected an error for an empty proposed subject")
	}
}

func TestDiscoveredSubjects_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subjects.json")
	ds := discoveredSubject{Label: "a lighthouse keeper", Rare: true}
	if err := appendDiscoveredSubject(path, ds); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDiscoveredSubjects(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Label != "a lighthouse keeper" || !loaded[0].Rare {
		t.Errorf("round-trip mismatch, got %+v", loaded)
	}
}

func TestLoadDiscoveredSubjects_NonexistentFileReturnsEmpty(t *testing.T) {
	loaded, err := loadDiscoveredSubjects(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected no subjects, got %+v", loaded)
	}
}
