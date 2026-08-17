// cmd/promptoverse_backoff.go — Prompt-o-verse adaptive backoff
//
// Founder direction: "keep a local variable to estimate the retry for
// rerunning even on the first gen like if i run twice or 3 in a row it
// fails on api overload it should wait a little longer the third time i
// try to run it ya know? - add a --force flag to skip that functionality"
// + "like even on the first try" + "it should check that var to determine
// if it queries right away or does a preemptive backoff."
//
// Persists a small (consecutive_failures, last_failure_at) record to
// EMILY/var/promptoverse-backoff.json. drainQueue consults it BEFORE
// making its first request of a run (not just between retries within one
// run) -- three separate `add`/`work` invocations in a row that each hit a
// 429 should make the third one wait longer before it even tries, not just
// react after failing again. Any success resets the counter (the overload
// has cleared); a failure old enough (see promptoverseBackoffResetAfter)
// is treated as stale and doesn't penalize a fresh attempt. --force skips
// the preemptive wait for one run without disabling the bookkeeping.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const (
	promptoverseBackoffFileName = "promptoverse-backoff.json"
	// promptoverseBackoffStep is the extra wait added per consecutive
	// failure, capped at promptoverseBackoffMax -- linear, not exponential,
	// so it stays predictable ("wait a LITTLE longer" each time, not a
	// runaway multiplier).
	promptoverseBackoffStep = 30 * time.Second
	promptoverseBackoffMax  = 5 * time.Minute
	// promptoverseBackoffResetAfter: a failure older than this doesn't
	// count against a new attempt -- otherwise one bad batch would keep
	// penalizing runs indefinitely, long after the overload has cleared.
	promptoverseBackoffResetAfter = 15 * time.Minute
)

type backoffState struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastFailureAt       time.Time `json:"last_failure_at"`
}

func backoffStatePath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseBackoffFileName)
}

func loadBackoffState(path string) (backoffState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return backoffState{}, nil
	}
	if err != nil {
		return backoffState{}, err
	}
	if len(data) == 0 {
		return backoffState{}, nil
	}
	var st backoffState
	if err := json.Unmarshal(data, &st); err != nil {
		return backoffState{}, fmt.Errorf("decode backoff state: %w", err)
	}
	return st, nil
}

func saveBackoffState(path string, st backoffState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// backoffWaitFor computes the extra, preemptive wait to apply before the
// FIRST request of a new drain, given the persisted failure history --
// pure function, testable without real time.Sleep/file IO. Returns 0 if
// there's no recent failure history to account for.
func backoffWaitFor(st backoffState, now time.Time) time.Duration {
	if st.ConsecutiveFailures <= 0 {
		return 0
	}
	if now.Sub(st.LastFailureAt) > promptoverseBackoffResetAfter {
		return 0
	}
	wait := time.Duration(st.ConsecutiveFailures) * promptoverseBackoffStep
	if wait > promptoverseBackoffMax {
		wait = promptoverseBackoffMax
	}
	return wait
}

// recordBackoffFailure and clearBackoffState always update the persisted
// state, even when --force skipped this run's preemptive wait -- force
// only opts THIS run out of waiting, it doesn't stop the tool from learning
// for the next one. Best-effort: a persistence error here is a warning,
// not a reason to fail the whole drain over bookkeeping.
func recordBackoffFailure(path string) {
	st, err := loadBackoffState(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read backoff state: %v\n", err)
		st = backoffState{}
	}
	st.ConsecutiveFailures++
	st.LastFailureAt = time.Now().UTC()
	if err := saveBackoffState(path, st); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not persist backoff state: %v\n", err)
	}
}

func clearBackoffState(path string) {
	if err := saveBackoffState(path, backoffState{}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not clear backoff state: %v\n", err)
	}
}
