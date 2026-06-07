// cmd/install.go — emily install
// Prints recommended cron/systemd entries; --write installs them.

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func RunInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	cron := fs.Bool("cron", false, "show (and with --write, install) crontab entries")
	write := fs.Bool("write", false, "actually install the entries (default: print only)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if !*cron {
		fmt.Fprintln(os.Stderr, "usage: emily install [--cron] [--write]")
		fmt.Fprintln(os.Stderr, "  --cron   show recommended crontab entries")
		fmt.Fprintln(os.Stderr, "  --write  install them (appends to crontab)")
		return 1
	}

	cfg, _ := config.Resolve()

	entries := buildCronEntries(cfg)

	if !*write {
		fmt.Println("# emily.cli recommended crontab entries")
		fmt.Println("# Add with: emily install --cron --write")
		fmt.Println()
		for _, e := range entries {
			fmt.Println(e)
		}
		fmt.Println()
		fmt.Println("# Run: emily install --cron --write  to install automatically.")
		return 0
	}

	// Read existing crontab
	existing := ""
	if out, err := exec.Command("crontab", "-l").Output(); err == nil {
		existing = string(out)
	}

	// Only add entries that aren't already present
	var toAdd []string
	for _, e := range entries {
		if !strings.Contains(existing, e) {
			toAdd = append(toAdd, e)
		}
	}

	if len(toAdd) == 0 {
		fmt.Println("✓ all entries already present in crontab — nothing to do")
		return 0
	}

	newCrontab := strings.TrimRight(existing, "\n") + "\n" +
		"# emily.cli — added by emily install\n" +
		strings.Join(toAdd, "\n") + "\n"

	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error installing crontab: %v\n", err)
		return 4
	}

	fmt.Printf("✓ %d crontab entr%s installed\n", len(toAdd), map[bool]string{true: "y", false: "ies"}[len(toAdd) == 1])
	for _, e := range toAdd {
		fmt.Printf("  + %s\n", e)
	}
	return 0
}

func buildCronEntries(cfg *config.Config) []string {
	return []string{
		// Sync FatBaby observations to IDUNA every 10 minutes
		fmt.Sprintf("*/10 * * * * %s sync --quiet 2>/dev/null", emilyBin()),
		// Tyler RSI loop every 4 hours (if exists)
		"0 */4 * * * /home/fatbaby/TYLER/scripts/cron-emily.sh 2>/dev/null",
	}
}

func emilyBin() string {
	if p, err := exec.LookPath("emily"); err == nil {
		return p
	}
	return os.Getenv("HOME") + "/.local/bin/emily"
}
