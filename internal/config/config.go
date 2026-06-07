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
	FatBabyRoot      string
	EmilyRoot        string
	SecretsFile      string
}

// Resolve reads env vars and auto-populates missing fields from the secrets file.
func Resolve() (*Config, error) {
	cfg := &Config{
		IDUNABaseURL:   envOr("IDUNA_BASE_URL", "http://localhost:8080"),
		IDUNAAgentName: envOr("IDUNA_AGENT_NAME", "EMILY-PRIME"),
		FatBabyRoot:    envOr("FATBABY_ROOT", "/home/fatbaby/PRRJECT_FATBABY"),
		EmilyRoot:      envOr("EMILY_ROOT", "/home/fatbaby/EMILY"),
		SecretsFile:    envOr("IDUNA_SECRETS", "/home/fatbaby/IDUNA/var/agent-secrets.env"),
	}

	cfg.IDUNAAgentSecret = os.Getenv("IDUNA_AGENT_SECRET")
	if cfg.IDUNAAgentSecret == "" {
		secret, err := readSecretFromFile(cfg.SecretsFile, cfg.IDUNAAgentName)
		if err == nil && secret != "" {
			cfg.IDUNAAgentSecret = secret
		}
	}

	return cfg, nil
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
