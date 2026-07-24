package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseManifest_Basic(t *testing.T) {
	content := `- claim: FIN-098.INV-2
  verified_by:
    - test: ledger/payment_gate_test.go::TestNoUnapprovedMovement
    - telemetry: back_office.metric.unapproved_payment_attempts == 0
- claim: SIM-100.BEH-4
  verified_by: []
`
	entries, err := parseManifest(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Claim != "FIN-098.INV-2" {
		t.Errorf("entries[0].Claim = %q, want FIN-098.INV-2", entries[0].Claim)
	}
	if len(entries[0].VerifiedBy) != 2 {
		t.Fatalf("expected 2 anchors, got %d", len(entries[0].VerifiedBy))
	}
	if entries[0].VerifiedBy[0].Kind != "test" || entries[0].VerifiedBy[0].Ref != "ledger/payment_gate_test.go::TestNoUnapprovedMovement" {
		t.Errorf("anchor 0 = %+v", entries[0].VerifiedBy[0])
	}
	if entries[0].VerifiedBy[1].Kind != "telemetry" {
		t.Errorf("anchor 1 kind = %q, want telemetry", entries[0].VerifiedBy[1].Kind)
	}
	if entries[1].Claim != "SIM-100.BEH-4" || len(entries[1].VerifiedBy) != 0 {
		t.Errorf("entries[1] = %+v, want empty verified_by", entries[1])
	}
}

func TestParseManifest_MissingVerifiedBy(t *testing.T) {
	_, err := parseManifest("- claim: FOO.INV-1\n")
	if err == nil {
		t.Fatal("expected an error for a claim with no verified_by line")
	}
}

func TestLoadManifest_MissingFileIsEmptyNotError(t *testing.T) {
	entries, err := loadManifest(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing manifest file should not be an error, got: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected zero entries, got %d", len(entries))
	}
}

func TestTestRefExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foo_test.go")
	if err := os.WriteFile(path, []byte("package foo\nfunc TestBar(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !testRefExists(dir, "foo_test.go::TestBar") {
		t.Error("expected testRefExists to find TestBar")
	}
	if testRefExists(dir, "foo_test.go::TestMissing") {
		t.Error("expected testRefExists to reject a test name that isn't in the file")
	}
	if testRefExists(dir, "nope_test.go::TestBar") {
		t.Error("expected testRefExists to reject a nonexistent file")
	}
}

func TestExtractProcessSurface(t *testing.T) {
	dir := t.TempDir()
	readme := "# Repo\n\n## All processes\n\n" +
		"| Process | Purpose |\n|---|---|\n" +
		"| `cmd/secwatch` | Polls SEC EDGAR -> `filing_discovered` events |\n" +
		"| `cmd/prwatch` | Polls PR Newswire -> `pr_discovered` events |\n\n" +
		"## Unrelated later section\n\ncontains `some_other_token` that must not be picked up.\n"
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	surface, err := extractProcessSurface(dir)
	if err != nil {
		t.Fatalf("extractProcessSurface: %v", err)
	}
	want := map[string]bool{"filing_discovered": true, "pr_discovered": true}
	if len(surface) != len(want) {
		t.Fatalf("surface = %v, want exactly %v", surface, want)
	}
	for _, s := range surface {
		if !want[s] {
			t.Errorf("unexpected surface token %q", s)
		}
	}
	for s := range want {
		found := false
		for _, got := range surface {
			if got == s {
				found = true
			}
		}
		if !found {
			t.Errorf("missing expected surface token %q", s)
		}
	}
	// cmd/secwatch and cmd/prwatch (slashes) must be excluded, and the
	// later, unrelated section's token must not leak in.
	for _, s := range surface {
		if s == "some_other_token" {
			t.Error("extractProcessSurface leaked a token from beyond the All processes section")
		}
	}
}

func TestExtractProcessSurface_NoSectionIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Repo\nno processes section here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractProcessSurface(dir); err == nil {
		t.Fatal("expected an error when README has no '## All processes' section")
	}
}

func TestManifestSearchText(t *testing.T) {
	entries := []ManifestEntry{
		{Claim: "FOO.INV-1", VerifiedBy: []ManifestAnchor{{Kind: "test", Ref: "pkg/foo_test.go::TestFilingDiscovered"}}},
	}
	text := manifestSearchText(entries)
	if !strings.Contains(text, "FOO.INV-1") || !strings.Contains(text, "TestFilingDiscovered") {
		t.Errorf("manifestSearchText = %q, missing expected substrings", text)
	}
}
