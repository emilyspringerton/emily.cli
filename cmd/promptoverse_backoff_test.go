package cmd

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBackoffWaitFor_NoFailuresMeansNoWait(t *testing.T) {
	if got := backoffWaitFor(backoffState{}, time.Now()); got != 0 {
		t.Errorf("expected 0 wait with no failure history, got %v", got)
	}
}

func TestBackoffWaitFor_ScalesWithConsecutiveFailures(t *testing.T) {
	// Founder: "if i run twice or 3 in a row it fails on api overload it
	// should wait a little longer the third time i try to run it."
	now := time.Now()
	one := backoffWaitFor(backoffState{ConsecutiveFailures: 1, LastFailureAt: now}, now)
	two := backoffWaitFor(backoffState{ConsecutiveFailures: 2, LastFailureAt: now}, now)
	three := backoffWaitFor(backoffState{ConsecutiveFailures: 3, LastFailureAt: now}, now)
	if !(one > 0 && two > one && three > two) {
		t.Errorf("expected strictly increasing waits, got 1=%v 2=%v 3=%v", one, two, three)
	}
}

func TestBackoffWaitFor_CapsAtMax(t *testing.T) {
	now := time.Now()
	got := backoffWaitFor(backoffState{ConsecutiveFailures: 1000, LastFailureAt: now}, now)
	if got != promptoverseBackoffMax {
		t.Errorf("expected the wait to cap at %v, got %v", promptoverseBackoffMax, got)
	}
}

func TestBackoffWaitFor_StaleFailureDoesNotPenalize(t *testing.T) {
	now := time.Now()
	old := now.Add(-promptoverseBackoffResetAfter - time.Minute)
	got := backoffWaitFor(backoffState{ConsecutiveFailures: 5, LastFailureAt: old}, now)
	if got != 0 {
		t.Errorf("expected a stale failure (older than reset window) to not penalize a fresh attempt, got %v", got)
	}
}

func TestBackoffState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backoff.json")
	want := backoffState{ConsecutiveFailures: 2, LastFailureAt: time.Now().UTC().Truncate(time.Second)}
	if err := saveBackoffState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadBackoffState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConsecutiveFailures != want.ConsecutiveFailures || !got.LastFailureAt.Equal(want.LastFailureAt) {
		t.Errorf("round-trip mismatch: want %+v, got %+v", want, got)
	}
}

func TestBackoffState_LoadNonexistentFileReturnsZeroValue(t *testing.T) {
	got, err := loadBackoffState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if got.ConsecutiveFailures != 0 {
		t.Errorf("expected a zero-value state, got %+v", got)
	}
}

func TestRecordBackoffFailure_IncrementsAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backoff.json")
	recordBackoffFailure(path)
	recordBackoffFailure(path)
	recordBackoffFailure(path)
	st, err := loadBackoffState(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.ConsecutiveFailures != 3 {
		t.Errorf("expected 3 consecutive failures after 3 calls, got %d", st.ConsecutiveFailures)
	}
}

func TestClearBackoffState_ResetsToZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backoff.json")
	recordBackoffFailure(path)
	recordBackoffFailure(path)
	clearBackoffState(path)
	st, err := loadBackoffState(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.ConsecutiveFailures != 0 {
		t.Errorf("expected a success to reset the counter to 0, got %d", st.ConsecutiveFailures)
	}
}
