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

// WriteEmilySecret writes or updates a KEY=VALUE pair in the emily secrets file.
// Creates the file (and var/ directory) if absent. Existing KEY lines are replaced.
func WriteEmilySecret(secretsFile, key, value string) error {
	dir := secretsFile[:strings.LastIndex(secretsFile, "/")]
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Read existing lines.
	var lines []string
	if data, err := os.ReadFile(secretsFile); err == nil {
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
	lines = append(lines, fmt.Sprintf("export %s=%s", key, value))
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(secretsFile, []byte(content), 0o600)
}

// RemoveEmilySecret removes a KEY line from the emily secrets file.
func RemoveEmilySecret(secretsFile, key string) error {
	data, err := os.ReadFile(secretsFile)
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
	return os.WriteFile(secretsFile, []byte(content), 0o600)
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
