package color_test

import (
	"os"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/color"
)

// setColor sets EMILY_COLOR and re-initialises the enabled var via a fresh
// package import isn't possible in the same process, so we test the observable
// contract: when disabled all functions return the string unchanged.
// When enabled they return strings containing ANSI escape sequences.
//
// Note: the enabled var is initialised at package init time from os.Getenv.
// These tests must set the env BEFORE the package is first used. Since Go
// tests within the same binary share one init run, we can't flip enabled
// between subtests. Instead we verify the two invariants that always hold:
//
//   1. When EMILY_COLOR is NOT "1" (the default test environment), all
//      functions return the input string unchanged — no escape codes injected.
//   2. When EMILY_COLOR IS "1" (set before the test binary loads), functions
//      return strings that contain ANSI CSI prefix \033[.
//
// We run invariant (1) unconditionally (color is off by default in CI).
// Invariant (2) is verified in a subprocess test if the env is pre-set.

func TestSeverity_disabled_returnsUnchanged(t *testing.T) {
	if os.Getenv("EMILY_COLOR") == "1" {
		t.Skip("EMILY_COLOR=1: running in color mode, skip disabled-mode tests")
	}

	cases := []struct{ s, level string }{
		{"error message", "error"},
		{"warn message", "warn"},
		{"info message", "info"},
		{"unknown message", "unknown"},
	}
	for _, tc := range cases {
		got := color.Severity(tc.s, tc.level)
		if got != tc.s {
			t.Errorf("Severity(%q, %q) = %q; want %q (disabled mode should be identity)", tc.s, tc.level, got, tc.s)
		}
	}
}

func TestWarn_disabled_returnsUnchanged(t *testing.T) {
	if os.Getenv("EMILY_COLOR") == "1" {
		t.Skip("running in color mode")
	}
	s := "dirty count"
	if got := color.Warn(s); got != s {
		t.Errorf("Warn(%q) = %q; want %q", s, got, s)
	}
}

func TestBold_disabled_returnsUnchanged(t *testing.T) {
	if os.Getenv("EMILY_COLOR") == "1" {
		t.Skip("running in color mode")
	}
	s := "bold text"
	if got := color.Bold(s); got != s {
		t.Errorf("Bold(%q) = %q; want %q", s, got, s)
	}
}

func TestCyan_disabled_returnsUnchanged(t *testing.T) {
	if os.Getenv("EMILY_COLOR") == "1" {
		t.Skip("running in color mode")
	}
	s := "cyan text"
	if got := color.Cyan(s); got != s {
		t.Errorf("Cyan(%q) = %q; want %q", s, got, s)
	}
}

func TestEnabled_disabled(t *testing.T) {
	if os.Getenv("EMILY_COLOR") == "1" {
		t.Skip("running in color mode")
	}
	if color.Enabled() {
		t.Error("Enabled() = true; want false when EMILY_COLOR != '1'")
	}
}

// TestSeverity_enabled_containsANSI runs only when EMILY_COLOR=1.
// The binary can be tested with: EMILY_COLOR=1 go test ./internal/color/...
func TestSeverity_enabled_containsANSI(t *testing.T) {
	if os.Getenv("EMILY_COLOR") != "1" {
		t.Skip("set EMILY_COLOR=1 to run color-mode tests")
	}
	if !color.Enabled() {
		t.Fatal("Enabled() = false but EMILY_COLOR=1 is set — init order issue?")
	}

	cases := []struct {
		level string
	}{
		{"error"},
		{"warn"},
		{"info"},
	}
	for _, tc := range cases {
		got := color.Severity("msg", tc.level)
		if !strings.Contains(got, "\033[") {
			t.Errorf("Severity(%q) should contain ANSI escape: got %q", tc.level, got)
		}
		if !strings.Contains(got, "msg") {
			t.Errorf("Severity(%q) should still contain the original string: got %q", tc.level, got)
		}
		// Must end with reset sequence so colors don't bleed
		if !strings.HasSuffix(got, "\033[0m") {
			t.Errorf("Severity(%q) should end with reset sequence: got %q", tc.level, got)
		}
	}
}

func TestWarn_enabled_containsANSI(t *testing.T) {
	if os.Getenv("EMILY_COLOR") != "1" {
		t.Skip("set EMILY_COLOR=1 to run color-mode tests")
	}
	got := color.Warn("dirty")
	if !strings.Contains(got, "\033[") {
		t.Errorf("Warn should contain ANSI escape: got %q", got)
	}
}
