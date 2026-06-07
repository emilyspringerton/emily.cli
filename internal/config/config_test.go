package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func TestResolve_defaults(t *testing.T) {
	// Clear env to get defaults
	os.Unsetenv("IDUNA_BASE_URL")
	os.Unsetenv("IDUNA_AGENT_NAME")
	os.Unsetenv("IDUNA_AGENT_SECRET")
	os.Unsetenv("FATBABY_ROOT")
	os.Unsetenv("EMILY_ROOT")
	os.Unsetenv("IDUNA_SECRETS")

	cfg, err := config.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if cfg.IDUNABaseURL != "http://localhost:8080" {
		t.Errorf("IDUNABaseURL default: got %q", cfg.IDUNABaseURL)
	}
	if cfg.IDUNAAgentName != "EMILY-PRIME" {
		t.Errorf("IDUNAAgentName default: got %q", cfg.IDUNAAgentName)
	}
	if cfg.FatBabyRoot != "/home/fatbaby/PRRJECT_FATBABY" {
		t.Errorf("FatBabyRoot default: got %q", cfg.FatBabyRoot)
	}
	if cfg.EmilyRoot != "/home/fatbaby/EMILY" {
		t.Errorf("EmilyRoot default: got %q", cfg.EmilyRoot)
	}
}

func TestResolve_envOverride(t *testing.T) {
	t.Setenv("IDUNA_BASE_URL", "http://iduna.test:9999")
	t.Setenv("IDUNA_AGENT_NAME", "TYLER")
	t.Setenv("IDUNA_AGENT_SECRET", "supersecret")
	t.Setenv("FATBABY_ROOT", "/tmp/test-fb")
	t.Setenv("EMILY_ROOT", "/tmp/test-emily")

	cfg, _ := config.Resolve()

	if cfg.IDUNABaseURL != "http://iduna.test:9999" {
		t.Errorf("IDUNABaseURL env: got %q", cfg.IDUNABaseURL)
	}
	if cfg.IDUNAAgentName != "TYLER" {
		t.Errorf("IDUNAAgentName env: got %q", cfg.IDUNAAgentName)
	}
	if cfg.IDUNAAgentSecret != "supersecret" {
		t.Errorf("IDUNAAgentSecret env: got %q", cfg.IDUNAAgentSecret)
	}
	if cfg.FatBabyRoot != "/tmp/test-fb" {
		t.Errorf("FatBabyRoot env: got %q", cfg.FatBabyRoot)
	}
}

func TestResolve_secretFromFile(t *testing.T) {
	dir := t.TempDir()
	secretsFile := filepath.Join(dir, "agent-secrets.env")
	os.WriteFile(secretsFile, []byte(
		"export IDUNA_SECRET_EMILY_PRIME=abc123secret\n"+
			"export IDUNA_SECRET_TYLER=tylerpass\n",
	), 0o600)

	os.Unsetenv("IDUNA_AGENT_SECRET")
	t.Setenv("IDUNA_SECRETS", secretsFile)
	t.Setenv("IDUNA_AGENT_NAME", "EMILY-PRIME")

	cfg, _ := config.Resolve()
	if cfg.IDUNAAgentSecret != "abc123secret" {
		t.Errorf("secret from file: got %q want %q", cfg.IDUNAAgentSecret, "abc123secret")
	}
}

func TestResolve_secretFromFile_tyler(t *testing.T) {
	dir := t.TempDir()
	secretsFile := filepath.Join(dir, "agent-secrets.env")
	os.WriteFile(secretsFile, []byte(
		"export IDUNA_SECRET_EMILY_PRIME=primesecret\n"+
			"export IDUNA_SECRET_TYLER=tylerpass456\n",
	), 0o600)

	os.Unsetenv("IDUNA_AGENT_SECRET")
	t.Setenv("IDUNA_SECRETS", secretsFile)
	t.Setenv("IDUNA_AGENT_NAME", "TYLER")

	cfg, _ := config.Resolve()
	if cfg.IDUNAAgentSecret != "tylerpass456" {
		t.Errorf("TYLER secret from file: got %q want %q", cfg.IDUNAAgentSecret, "tylerpass456")
	}
}

func TestResolve_secretEnvTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	secretsFile := filepath.Join(dir, "agent-secrets.env")
	os.WriteFile(secretsFile, []byte("export IDUNA_SECRET_EMILY_PRIME=fromfile\n"), 0o600)

	t.Setenv("IDUNA_AGENT_SECRET", "fromenv")
	t.Setenv("IDUNA_SECRETS", secretsFile)
	t.Setenv("IDUNA_AGENT_NAME", "EMILY-PRIME")

	cfg, _ := config.Resolve()
	if cfg.IDUNAAgentSecret != "fromenv" {
		t.Errorf("env should take precedence: got %q want %q", cfg.IDUNAAgentSecret, "fromenv")
	}
}

func TestResolve_secretFromFileQuoted(t *testing.T) {
	dir := t.TempDir()
	secretsFile := filepath.Join(dir, "agent-secrets.env")
	// Quoted value variant
	os.WriteFile(secretsFile, []byte(`export IDUNA_SECRET_EMILY_PRIME="quoted-secret"`+"\n"), 0o600)

	os.Unsetenv("IDUNA_AGENT_SECRET")
	t.Setenv("IDUNA_SECRETS", secretsFile)
	t.Setenv("IDUNA_AGENT_NAME", "EMILY-PRIME")

	cfg, _ := config.Resolve()
	if cfg.IDUNAAgentSecret != "quoted-secret" {
		t.Errorf("quoted secret from file: got %q want %q", cfg.IDUNAAgentSecret, "quoted-secret")
	}
}

func TestResolve_missingSecretsFile(t *testing.T) {
	os.Unsetenv("IDUNA_AGENT_SECRET")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/path/secrets.env")

	cfg, err := config.Resolve()
	// Should not error — missing secrets file is a soft failure
	if err != nil {
		t.Fatalf("Resolve should not error on missing secrets file: %v", err)
	}
	if cfg.IDUNAAgentSecret != "" {
		t.Errorf("should be empty when secrets file missing: got %q", cfg.IDUNAAgentSecret)
	}
}
