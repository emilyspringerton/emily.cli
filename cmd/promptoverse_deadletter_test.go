package cmd

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendDeadLetter_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deadletter.jsonl")
	entry := deadLetterEntry{
		Subject: "racially ambiguous swimsuit model", StyleLabel: "ice cream novelty",
		Reason: "IMAGE_PROHIBITED_CONTENT", Message: "Unable to show the generated image...",
		BlockedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := appendDeadLetter(path, entry); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDeadLetters(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Subject != entry.Subject || loaded[0].Reason != entry.Reason {
		t.Errorf("round-trip mismatch: want %+v, got %+v", entry, loaded)
	}
}

func TestAppendDeadLetter_NeverRemovesPriorEntries(t *testing.T) {
	// "dead letter queue should not be retried" / "the data should just be
	// tracked" -- append-only dataset, unlike the work queue which drains.
	path := filepath.Join(t.TempDir(), "deadletter.jsonl")
	for i := 0; i < 3; i++ {
		if err := appendDeadLetter(path, deadLetterEntry{Subject: "s", StyleLabel: "x", Reason: "SAFETY"}); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := loadDeadLetters(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Errorf("expected all 3 appended entries to remain, got %d", len(loaded))
	}
}

func TestLoadDeadLetters_NonexistentFileReturnsEmpty(t *testing.T) {
	loaded, err := loadDeadLetters(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected no entries, got %+v", loaded)
	}
}

func TestContentBlockedError_MatchesSentinelViaErrorsIs(t *testing.T) {
	var err error = &contentBlockedError{Reason: "IMAGE_PROHIBITED_CONTENT", Message: "x"}
	if !errors.Is(err, errVertexContentBlocked) {
		t.Error("expected errors.Is(err, errVertexContentBlocked) to hold for a *contentBlockedError")
	}
}
