package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func TestWriteEnvFile_ExportPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := config.WriteEnvFile(path, "FOO", "bar", true); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != "export FOO=bar\n" {
		t.Fatalf("got %q, want %q", got, "export FOO=bar\n")
	}
}

func TestWriteEnvFile_NoExportPrefix(t *testing.T) {
	// Systemd's EnvironmentFile= doesn't understand "export " — this is the
	// format IDUNA's ~/.config/iduna/env needs.
	path := filepath.Join(t.TempDir(), "iduna-env")
	if err := config.WriteEnvFile(path, "MAILCHIMP_API_KEY", "abc123-us21", false); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != "MAILCHIMP_API_KEY=abc123-us21\n" {
		t.Fatalf("got %q, want no export prefix", got)
	}
}

func TestWriteEnvFile_ReplacesExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	config.WriteEnvFile(path, "A", "1", false)
	config.WriteEnvFile(path, "B", "2", false)
	config.WriteEnvFile(path, "A", "updated", false)

	if v := config.ReadEnvValue(path, "A"); v != "updated" {
		t.Errorf("A: got %q want %q", v, "updated")
	}
	if v := config.ReadEnvValue(path, "B"); v != "2" {
		t.Errorf("B should be untouched: got %q want %q", v, "2")
	}
}

func TestRemoveEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	config.WriteEnvFile(path, "KEEP", "1", true)
	config.WriteEnvFile(path, "DROP", "2", true)

	if err := config.RemoveEnvFile(path, "DROP"); err != nil {
		t.Fatalf("RemoveEnvFile: %v", err)
	}
	if v := config.ReadEnvValue(path, "DROP"); v != "" {
		t.Errorf("DROP should be gone: got %q", v)
	}
	if v := config.ReadEnvValue(path, "KEEP"); v != "1" {
		t.Errorf("KEEP should remain: got %q", v)
	}
}

func TestRemoveEnvFile_MissingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if err := config.RemoveEnvFile(path, "X"); err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
}

func TestReadEnvValue_MissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env")
	config.WriteEnvFile(path, "A", "1", false)
	if v := config.ReadEnvValue(path, "NOPE"); v != "" {
		t.Errorf("expected empty for missing key, got %q", v)
	}
}
