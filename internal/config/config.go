// internal/config/config.go
// Environment variable resolution and secrets file parsing.
// Zero external dependencies — stdlib only.

package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config holds resolved runtime configuration.
type Config struct {
	IDUNABaseURL     string
	IDUNAAgentName   string
	IDUNAAgentSecret string
	AnthropicKey     string
	FatBabyRoot      string
	EmilyRoot        string
	ShankpitRoot     string
	RedgardenRoot    string
	SurvivalRoot     string
	SecretsFile      string
	EmilySecretsFile string
}

// Resolve reads env vars and auto-populates missing fields from the secrets files.
// If ANTHROPIC_API_KEY is found in the emily secrets file but not in the environment,
// it is injected into the process environment so that callers using os.Getenv directly
// (e.g. emily backlog promote, emily context build) also see it.
func Resolve() (*Config, error) {
	emilyRoot := envOr("EMILY_ROOT", "/home/fatbaby/EMILY")
	cfg := &Config{
		IDUNABaseURL:     envOr("IDUNA_BASE_URL", "http://localhost:8080"),
		IDUNAAgentName:   envOr("IDUNA_AGENT_NAME", "EMILY-PRIME"),
		FatBabyRoot:      envOr("FATBABY_ROOT", "/home/fatbaby/PRRJECT_FATBABY"),
		EmilyRoot:        emilyRoot,
		ShankpitRoot:     envOr("SHANKPIT_ROOT", "/home/fatbaby/SHANKPIT"),
		RedgardenRoot:    envOr("REDGARDEN_ROOT", "/home/fatbaby/REDGARDEN"),
		SurvivalRoot:     envOr("SURVIVAL_ROOT", "/home/fatbaby/EINHORN_SURVIVAL"),
		SecretsFile:      envOr("IDUNA_SECRETS", "/home/fatbaby/IDUNA/var/agent-secrets.env"),
		EmilySecretsFile: envOr("EMILY_SECRETS", emilyRoot+"/var/emily-secrets.env"),
	}

	cfg.IDUNAAgentSecret = os.Getenv("IDUNA_AGENT_SECRET")
	if cfg.IDUNAAgentSecret == "" {
		secret, err := readSecretFromFile(cfg.SecretsFile, cfg.IDUNAAgentName)
		if err == nil && secret != "" {
			cfg.IDUNAAgentSecret = secret
		}
	}

	// Load ANTHROPIC_API_KEY from env; fall back to emily-secrets.env.
	cfg.AnthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.AnthropicKey == "" {
		if key, err := readEnvFile(cfg.EmilySecretsFile, "ANTHROPIC_API_KEY"); err == nil && key != "" {
			cfg.AnthropicKey = key
			// Inject so callers using os.Getenv see it without extra wiring.
			_ = os.Setenv("ANTHROPIC_API_KEY", key)
		}
	}

	return cfg, nil
}

// ReadEmilySecretsFile returns the path to the emily secrets file for the given emily root.
func ReadEmilySecretsFile(emilyRoot string) string {
	return envOr("EMILY_SECRETS", emilyRoot+"/var/emily-secrets.env")
}

// WriteEmilySecret writes or updates a KEY=VALUE pair in the emily secrets file,
// with an "export " prefix — this file is consumed by `source`-ing it in shell
// scripts, which requires export for child-process visibility.
func WriteEmilySecret(secretsFile, key, value string) error {
	return WriteEnvFile(secretsFile, key, value, true)
}

// RemoveEmilySecret removes a KEY line from the emily secrets file.
func RemoveEmilySecret(secretsFile, key string) error {
	return RemoveEnvFile(secretsFile, key)
}

// WriteEnvFile writes or updates a KEY=VALUE pair in an arbitrary env file.
// exportPrefix controls whether lines are written as "export KEY=VALUE" (for
// files meant to be shell-sourced) or plain "KEY=VALUE" (for files consumed by
// systemd's EnvironmentFile=, which does NOT understand an "export " prefix —
// IDUNA/EMILY's own service units are exactly this case). Creates the file
// (and parent directory) if absent. Existing KEY lines are replaced.
func WriteEnvFile(path, key, value string, exportPrefix bool) error {
	dir := path[:strings.LastIndex(path, "/")]
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if l == "" {
				continue
			}
			bare := strings.TrimPrefix(l, "export ")
			if strings.HasPrefix(bare, key+"=") {
				continue // will be replaced
			}
			lines = append(lines, l)
		}
	}
	prefix := ""
	if exportPrefix {
		prefix = "export "
	}
	lines = append(lines, fmt.Sprintf("%s%s=%s", prefix, key, value))
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// RemoveEnvFile removes a KEY line (with or without an "export " prefix) from
// an arbitrary env file. No-op if the file doesn't exist.
func RemoveEnvFile(path, key string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to remove
		}
		return err
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if l == "" {
			continue
		}
		bare := strings.TrimPrefix(l, "export ")
		if strings.HasPrefix(bare, key+"=") {
			continue
		}
		lines = append(lines, l)
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

// ReadEnvValue returns the value of key from an arbitrary env file (with or
// without an "export " prefix), or "" if not present / file doesn't exist.
func ReadEnvValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		bare := strings.TrimPrefix(strings.TrimSpace(l), "export ")
		if v, ok := strings.CutPrefix(bare, key+"="); ok {
			return v
		}
	}
	return ""
}

// readEnvFile reads a single KEY=value from a shell-style env file (export KEY=value).
func readEnvFile(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		if strings.HasPrefix(line, key+"=") {
			val := strings.SplitN(line, "=", 2)[1]
			return strings.Trim(val, `"'`), nil
		}
	}
	return "", fmt.Errorf("%s not found in %s", key, path)
}

// readSecretFromFile parses the IDUNA secrets env file for the secret matching agentName.
// Lines look like: export IDUNA_SECRET_EMILY_PRIME=abc123...
// This is a text parser, not a shell source — safe from injection.
func readSecretFromFile(path, agentName string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Convert agent name to env key suffix: "EMILY-PRIME" → "EMILY_PRIME"
	suffix := strings.ReplaceAll(strings.ToUpper(agentName), "-", "_")
	key := "IDUNA_SECRET_" + suffix

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Strip "export " prefix if present
		line = strings.TrimPrefix(line, "export ")
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		val := strings.SplitN(line, "=", 2)[1]
		// Strip surrounding quotes if any
		val = strings.Trim(val, `"'`)
		return val, nil
	}
	return "", fmt.Errorf("key %s not found in %s", key, path)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
