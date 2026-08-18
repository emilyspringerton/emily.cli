package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/mashupjudge"
)

func TestUpsertMashupJudgments_AddsNewAndReplacesExistingByKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judgments.json")

	first := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	if err := upsertMashupJudgments(path, []mashupjudge.Judgment{
		{Subject: "Fractal Raccoon", Provider: "gemini", IsCompositionalMashup: true, AsOf: first},
		{Subject: "tuxedo duck", Provider: "gemini", IsCompositionalMashup: false, AsOf: first},
	}); err != nil {
		t.Fatalf("upsertMashupJudgments (1): %v", err)
	}

	loaded, err := loadMashupJudgments(path)
	if err != nil {
		t.Fatalf("loadMashupJudgments: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 judgments after first write, got %d", len(loaded))
	}

	// A later run re-judges "Fractal Raccoon" with a newer AsOf -- the old
	// entry should be replaced, not duplicated, and the unrelated
	// "tuxedo duck" entry should survive untouched.
	second := first.Add(time.Hour)
	if err := upsertMashupJudgments(path, []mashupjudge.Judgment{
		{Subject: "Fractal Raccoon", Provider: "gemini", IsCompositionalMashup: false, AsOf: second, Reasoning: "revised"},
	}); err != nil {
		t.Fatalf("upsertMashupJudgments (2): %v", err)
	}

	loaded, err = loadMashupJudgments(path)
	if err != nil {
		t.Fatalf("loadMashupJudgments (2): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 judgments after upsert (replace, not duplicate), got %d", len(loaded))
	}
	found := false
	for _, j := range loaded {
		if j.Subject == "Fractal Raccoon" && j.Provider == "gemini" {
			found = true
			if j.IsCompositionalMashup {
				t.Error("expected the revised (second) judgment to have overwritten the first")
			}
			if !j.AsOf.Equal(second) {
				t.Errorf("AsOf: got %v want %v", j.AsOf, second)
			}
		}
	}
	if !found {
		t.Error("Fractal Raccoon/gemini entry missing after upsert")
	}
}

func TestUpsertMashupJudgments_DifferentProvidersCoexist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "judgments.json")
	asOf := time.Now().UTC()
	if err := upsertMashupJudgments(path, []mashupjudge.Judgment{
		{Subject: "Fractal Raccoon", Provider: "gemini", IsCompositionalMashup: true, AsOf: asOf},
		{Subject: "Fractal Raccoon", Provider: "claude", IsCompositionalMashup: false, AsOf: asOf},
	}); err != nil {
		t.Fatalf("upsertMashupJudgments: %v", err)
	}
	loaded, err := loadMashupJudgments(path)
	if err != nil {
		t.Fatalf("loadMashupJudgments: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected both providers' judgments to coexist for A/B comparison, got %d entries", len(loaded))
	}
}

func TestLoadMashupJudgments_MissingFileReturnsEmptyNotError(t *testing.T) {
	loaded, err := loadMashupJudgments(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil/empty result, got %v", loaded)
	}
}
