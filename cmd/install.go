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
	// --edis is handled separately (it has its own sub-flags).
	for _, a := range args {
		if a == "--edis" {
			rest := make([]string, 0, len(args))
			for _, b := range args {
				if b != "--edis" {
					rest = append(rest, b)
				}
			}
			return runInstallEDIS(rest)
		}
	}

	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	cron := fs.Bool("cron", false, "show (and with --write, install) crontab entries")
	systemd := fs.Bool("systemd", false, "generate systemd user unit for emily sync --watch")
	system := fs.Bool("system", false, "generate emily-system.service: starts agent stack on login/reboot")
	idunaSystemd := fs.Bool("iduna-systemd", false, "deploy IDUNA's systemd user unit from IDUNA/scripts/iduna.service")
	write := fs.Bool("write", false, "actually install (crontab or systemd unit)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *idunaSystemd {
		return runInstallIDUNASystemd(*write)
	}
	if *system {
		return runInstallSystemStart(*write)
	}
	if *systemd {
		return runInstallSystemd(*write)
	}

	if !*cron {
		fmt.Fprintln(os.Stderr, "usage: emily install [--cron|--systemd|--system|--iduna-systemd|--edis] [--write]")
		fmt.Fprintln(os.Stderr, "  --cron            show recommended crontab entries")
		fmt.Fprintln(os.Stderr, "  --systemd         generate systemd user unit for emily sync --watch")
		fmt.Fprintln(os.Stderr, "  --system          generate emily-system.service (start on boot: obs-watcher + emily-agent)")
		fmt.Fprintln(os.Stderr, "  --iduna-systemd   deploy IDUNA systemd user unit from IDUNA/scripts/iduna.service")
		fmt.Fprintln(os.Stderr, "  --edis            full EDIS stack install (WordPress + nginx + DIS + custodian agent)")
		fmt.Fprintln(os.Stderr, "  --write           actually install")
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

func runInstallSystemd(write bool) int {
	bin := emilyBin()
	home := os.Getenv("HOME")
	unitName := "emily-sync.service"
	unitPath := home + "/.config/systemd/user/" + unitName

	unit := fmt.Sprintf(`[Unit]
Description=emily sync — FatBaby observations → IDUNA daemon
After=network.target

[Service]
Type=simple
ExecStart=%s sync --watch --quiet
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`, bin)

	if !write {
		fmt.Printf("# %s\n", unitPath)
		fmt.Println("# Install with: emily install --systemd --write")
		fmt.Println("# Then: systemctl --user enable --now emily-sync.service")
		fmt.Println()
		fmt.Print(unit)
		return 0
	}

	dir := home + "/.config/systemd/user"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", dir, err)
		return 4
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", unitPath, err)
		return 4
	}
	fmt.Printf("✓ unit written: %s\n", unitPath)
	fmt.Println("  next steps:")
	fmt.Println("    systemctl --user daemon-reload")
	fmt.Println("    systemctl --user enable --now emily-sync.service")
	fmt.Println("    systemctl --user status emily-sync.service")
	return 0
}

// runInstallSystemStart generates (and optionally installs) emily-system.service,
// which calls `emily start` on login/reboot — bringing up the full agent layer.
// IDUNA is expected to be managed separately via its own unit.
func runInstallSystemStart(write bool) int {
	bin := emilyBin()
	home := os.Getenv("HOME")
	unitName := "emily-system.service"
	unitPath := home + "/.config/systemd/user/" + unitName

	unit := fmt.Sprintf(`[Unit]
Description=Emily OS agent stack — observation-watcher + emily-agent daemon
After=network-online.target iduna.service
Wants=iduna.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%s start --iduna
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`, bin)

	if !write {
		fmt.Printf("# %s\n", unitPath)
		fmt.Println("# Install with: emily install --system --write")
		fmt.Println("# Then:")
		fmt.Println("#   systemctl --user daemon-reload")
		fmt.Println("#   systemctl --user enable --now emily-system.service")
		fmt.Println("# Note: also run: emily install --iduna-systemd --write  to manage IDUNA")
		fmt.Println()
		fmt.Print(unit)
		return 0
	}

	dir := home + "/.config/systemd/user"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", dir, err)
		return 4
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", unitPath, err)
		return 4
	}
	fmt.Printf("✓ unit written: %s\n", unitPath)
	fmt.Println("  next steps:")
	fmt.Println("    systemctl --user daemon-reload")
	fmt.Println("    systemctl --user enable --now emily-system.service")
	fmt.Println("    systemctl --user status emily-system.service")
	fmt.Println()
	fmt.Println("  also install IDUNA if not yet done:")
	fmt.Println("    emily install --iduna-systemd --write")
	return 0
}

// runInstallIDUNASystemd copies IDUNA's service file into the user systemd directory.
func runInstallIDUNASystemd(write bool) int {
	home := os.Getenv("HOME")
	srcPath := "/home/fatbaby/IDUNA/scripts/iduna.service"
	dstPath := home + "/.config/systemd/user/iduna.service"

	content, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", srcPath, err)
		fmt.Fprintln(os.Stderr, "  (is IDUNA checked out at /home/fatbaby/IDUNA?)")
		return 4
	}

	if !write {
		fmt.Printf("# Source:      %s\n", srcPath)
		fmt.Printf("# Destination: %s\n", dstPath)
		fmt.Println("# Install with: emily install --iduna-systemd --write")
		fmt.Println("# Then:")
		fmt.Println("#   systemctl --user daemon-reload")
		fmt.Println("#   systemctl --user enable --now iduna.service")
		fmt.Println()
		fmt.Println(string(content))
		return 0
	}

	dir := home + "/.config/systemd/user"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", dir, err)
		return 4
	}
	if err := os.WriteFile(dstPath, content, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", dstPath, err)
		return 4
	}
	fmt.Printf("✓ IDUNA unit written: %s\n", dstPath)
	fmt.Println("  next steps:")
	fmt.Println("    systemctl --user daemon-reload")
	fmt.Println("    systemctl --user enable --now iduna.service")
	fmt.Println("    systemctl --user status iduna.service")
	return 0
}
