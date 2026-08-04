// cmd/iduna_account.go — emily iduna create-account
//
// Wraps IDUNA's POST /api/v1/auth/email/register (character_name variant) so a real,
// disposable DragonsNShit test account can be minted from one command instead of a raw
// SQLite INSERT or a hand-typed curl call (2026-08-04, founder: "i need a way to create
// dragonsnshit accounts for testing - i need iduna login i think it should live in iduna
// create account for dragonsnshit"). Creates a real player + login credential + character
// atomically; prints email/password to log in with at apps2/mud or battlegrounds_gui's own
// IDUNA-backed login screen — same identity monorepo-wide, no SQL access needed.
package cmd

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func RunIduna(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily iduna create-account <character-name> [flags]")
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "create-account":
		return runIdunaCreateAccount(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily iduna: unknown subcommand %q — use create-account\n", sub)
		return 1
	}
}

func runIdunaCreateAccount(args []string) int {
	fs := flag.NewFlagSet("iduna create-account", flag.ContinueOnError)
	job := fs.String("job", "WAR", "starting job_main for the character")
	email := fs.String("email", "", "login email (default: auto-generated disposable address)")
	password := fs.String("password", "", "login password (default: auto-generated)")
	fs.Usage = func() {
		fmt.Print(`emily iduna create-account <character-name> — mint a real DragonsNShit test account

Creates a real IDUNA player + login credential + DragonsNShit character in one atomic
request. Prints the email/password to log in with at battlegrounds_gui or apps2/mud's own
IDUNA-backed login screen — same identity monorepo-wide, no SQL access needed.

Flags:
  --job        starting job_main (default WAR)
  --email      login email (default: auto-generated disposable address)
  --password   login password (default: auto-generated, 20 hex chars)
`)
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	name := fs.Arg(0)
	if name == "" {
		fs.Usage()
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	em := *email
	if em == "" {
		em = fmt.Sprintf("test-%s-%s@dragonsnshit.test", strings.ToLower(name), randHex(4))
	}
	pw := *password
	if pw == "" {
		pw = randHex(10)
	}

	body, _ := json.Marshal(map[string]string{
		"email":          em,
		"password":       pw,
		"display_name":   name,
		"character_name": name,
		"character_job":  *job,
	})

	url := strings.TrimRight(cfg.IDUNABaseURL, "/") + "/api/v1/auth/email/register"
	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "request to %s failed: %v\n", url, err)
		return 1
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "iduna returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return 1
	}

	var out map[string]string
	if err := json.Unmarshal(respBody, &out); err != nil {
		fmt.Fprintf(os.Stderr, "could not parse iduna response: %v\n", err)
		return 1
	}

	fmt.Println("Real DragonsNShit test account created:")
	fmt.Printf("  character:    %s (job %s)\n", name, *job)
	fmt.Printf("  character_id: %s\n", out["character_id"])
	fmt.Printf("  player_id:    %s\n", out["player_id"])
	fmt.Printf("  email:        %s\n", em)
	fmt.Printf("  password:     %s\n", pw)
	fmt.Println()
	fmt.Println("Log in with this email/password at battlegrounds_gui's login screen (or")
	fmt.Println("apps2/mud's own IDUNA-backed login) — same IDUNA identity monorepo-wide.")
	return 0
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
