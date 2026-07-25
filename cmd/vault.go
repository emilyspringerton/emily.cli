// cmd/vault.go — emily vault init|unlock|lock|status|add|get|list|delete
//
// IDUNA Vault VS0 (EMILY/BACKLOG.md S170-03b): a founder-only password
// manager. Every vault endpoint is loopback-only on the IDUNA side (see
// IDUNA/internal/http/handlers/vault.go doc comment) -- this CLI is meant to
// run on the same box as IDUNA, same operational shape as
// cmd/mailing-list-unlock. Passphrases are always read interactively with
// echo disabled, never accepted as a flag/arg (would leak into shell
// history and `ps` output).
package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"golang.org/x/term"
)

func RunVault(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily vault <init|unlock|lock|status|add|get|list|delete> [flags]")
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "init":
		return runVaultInit(rest)
	case "unlock":
		return runVaultUnlock(rest)
	case "lock":
		return runVaultLock(rest)
	case "status":
		return runVaultStatus(rest)
	case "add":
		return runVaultAdd(rest)
	case "get":
		return runVaultGet(rest)
	case "list":
		return runVaultList(rest)
	case "delete":
		return runVaultDelete(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q — use init, unlock, lock, status, add, get, list, or delete\n", sub)
		return 1
	}
}

func vaultBaseURL() (string, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return "", err
	}
	return cfg.IDUNABaseURL, nil
}

func readPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	return string(passBytes), nil
}

func vaultPost(path string, payload any) (map[string]any, int, error) {
	base, err := vaultBaseURL()
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.Post(base+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode, nil
}

func vaultGetJSON(path string) (io.Reader, int, error) {
	base, err := vaultBaseURL()
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.Get(base + path)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	io.Copy(buf, resp.Body)
	return buf, resp.StatusCode, nil
}

func vaultDo(method, path string, payload any) (map[string]any, int, error) {
	base, err := vaultBaseURL()
	if err != nil {
		return nil, 0, err
	}
	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out, resp.StatusCode, nil
}

func runVaultInit(args []string) int {
	pass, err := readPassphrase("New vault passphrase (min 12 chars): ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	confirm, err := readPassphrase("Confirm passphrase: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if pass != confirm {
		fmt.Fprintln(os.Stderr, "passphrases did not match, aborting")
		return 1
	}
	out, status, err := vaultPost("/api/v1/vault/init", map[string]string{"passphrase": pass})
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Println(out["status"])
	return 0
}

func runVaultUnlock(args []string) int {
	pass, err := readPassphrase("Vault passphrase: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	out, status, err := vaultPost("/api/v1/vault/unlock", map[string]string{"passphrase": pass})
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Println(out["status"])
	return 0
}

func runVaultLock(args []string) int {
	out, status, err := vaultDo(http.MethodPost, "/api/v1/vault/lock", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Println(out["status"])
	return 0
}

func runVaultStatus(args []string) int {
	body, status, err := vaultGetJSON("/api/v1/vault/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	var out map[string]any
	json.NewDecoder(body).Decode(&out)
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Printf("initialized: %v\n", out["initialized"])
	fmt.Printf("locked:      %v\n", out["locked"])
	return 0
}

// runVaultAdd parses `-type`, `-name`, and any number of `-field key=value`
// flags into the item payload. A login might be:
//
//	emily vault add -type login -name "AWS Root" \
//	  -field username=root -field password=hunter2 -field url=https://aws.amazon.com
func runVaultAdd(args []string) int {
	fs := flag.NewFlagSet("vault add", flag.ContinueOnError)
	itemType := fs.String("type", "", "item type: login, note, api_key, totp, document (required)")
	name := fs.String("name", "", "item name/label (required)")
	var fieldFlags stringSliceFlag
	fs.Var(&fieldFlags, "field", "key=value field, repeatable")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *itemType == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "usage: emily vault add -type <type> -name <name> [-field key=value ...]")
		return 1
	}
	fields := map[string]string{}
	for _, kv := range fieldFlags {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "invalid -field %q, want key=value\n", kv)
			return 1
		}
		fields[parts[0]] = parts[1]
	}

	out, status, err := vaultPost("/api/v1/vault/items", map[string]any{
		"item_type": *itemType,
		"name":      *name,
		"fields":    fields,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Printf("✓ item added, id=%v\n", out["id"])
	return 0
}

func runVaultGet(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily vault get <id>")
		return 1
	}
	if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
		fmt.Fprintf(os.Stderr, "error: %q is not a valid item id\n", args[0])
		return 1
	}
	body, status, err := vaultGetJSON("/api/v1/vault/items/" + args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	var raw map[string]any
	json.NewDecoder(body).Decode(&raw)
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, raw["error"])
		return 1
	}
	fmt.Printf("\n◈ Vault item #%v\n", raw["id"])
	fmt.Printf("  type: %v\n", raw["item_type"])
	fmt.Printf("  name: %v\n", raw["name"])
	if fields, ok := raw["fields"].(map[string]any); ok {
		for k, v := range fields {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
	fmt.Println()
	return 0
}

func runVaultList(args []string) int {
	body, status, err := vaultGetJSON("/api/v1/vault/items")
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	var out map[string]any
	json.NewDecoder(body).Decode(&out)
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	items, _ := out["items"].([]any)
	fmt.Printf("\n◈ IDUNA VAULT | %d item(s)\n\n", len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fmt.Printf("  #%-4v %-10v %s\n", item["id"], item["item_type"], item["name"])
	}
	fmt.Println()
	return 0
}

func runVaultDelete(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily vault delete <id>")
		return 1
	}
	if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
		fmt.Fprintf(os.Stderr, "error: %q is not a valid item id\n", args[0])
		return 1
	}
	out, status, err := vaultDo(http.MethodDelete, "/api/v1/vault/items/"+args[0], nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(os.Stderr, "failed (%d): %v\n", status, out["error"])
		return 1
	}
	fmt.Println(out["status"])
	return 0
}

// stringSliceFlag implements flag.Value for repeatable -field key=value flags.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}
