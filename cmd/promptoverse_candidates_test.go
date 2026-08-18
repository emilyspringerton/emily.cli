package cmd

import (
	"path/filepath"
	"testing"
)

func TestRecordCandidates_SkipsAlreadyInPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.json")
	existingPool := map[string]bool{"claymation": true}
	added, err := recordCandidates(path, "style", []string{"claymation", "origami"}, "seed x", existingPool)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("expected 1 new candidate (origami), got %d", added)
	}
	loaded, err := loadCandidates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Label != "origami" {
		t.Errorf("expected only origami persisted, got %+v", loaded)
	}
}

func TestRecordCandidates_DedupesAcrossCallsCaseInsensitive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.json")
	if _, err := recordCandidates(path, "style", []string{"origami"}, "seed 1", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	added, err := recordCandidates(path, "style", []string{"Origami", "mosaic"}, "seed 2", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("expected only 'mosaic' to be new (Origami is a dup), got %d added", added)
	}
	loaded, err := loadCandidates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 total tracked candidates, got %d: %+v", len(loaded), loaded)
	}
}

func TestRecordCandidates_StyleAndSubjectDedupeIndependently(t *testing.T) {
	// A style candidate and a subject candidate with the same literal
	// label are different things -- dedup must be scoped per Kind, not
	// global across the whole file.
	path := filepath.Join(t.TempDir(), "candidates.json")
	if _, err := recordCandidates(path, "style", []string{"vernacular"}, "seed 1", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	added, err := recordCandidates(path, "subject", []string{"vernacular"}, "seed 2", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("expected the subject candidate to be recorded despite a same-named style candidate existing, got added=%d", added)
	}
	loaded, err := loadCandidates(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 distinct candidates (one style, one subject), got %d: %+v", len(loaded), loaded)
	}
}

func TestCandidateKind_EmptyReadsAsStyle(t *testing.T) {
	// Records written before Kind existed have no field at all -- must
	// still be treated as "style", the only kind that existed then.
	if got := candidateKind(candidateTag{Label: "x"}); got != "style" {
		t.Errorf("expected an empty Kind to read as \"style\", got %q", got)
	}
}

func TestLoadCandidates_NonexistentFileReturnsEmpty(t *testing.T) {
	loaded, err := loadCandidates(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected no candidates, got %+v", loaded)
	}
}
