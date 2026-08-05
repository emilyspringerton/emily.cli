package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// RunSurvival is the entry point for `emily survival <subcommand>`.
//
// Subcommands:
//
//	logs [-n N] [-f]   — show/tail server.log (default: tail -f)
//	status             — systemd-user unit state
//	restart            — restart the live server via systemd --user
//
// EINHORN_SURVIVAL is a first-class repo in the monorepo (Paper Minecraft
// server, live on mc.okemily.com), same operating pattern as SHANKPIT/
// REDGARDEN: a systemd --user unit, no root needed.
func RunSurvival(args []string) int {
	if len(args) == 0 {
		return runSurvivalLogs(nil)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "logs":
		return runSurvivalLogs(rest)
	case "status":
		return runSurvivalStatus()
	case "restart":
		return runSurvivalRestart()
	default:
		fmt.Fprintf(os.Stderr, "emily survival: unknown subcommand %q\n\n", sub)
		printSurvivalUsage()
		return 1
	}
}

func printSurvivalUsage() {
	fmt.Print(`emily survival — EINHORN_SURVIVAL (Paper Minecraft) server ops

Subcommands:
  logs [-n N] [-f=false]   tail server.log (default: last 40 lines, follow)
  status                   systemd --user unit state for einhorn-survival.service
  restart                  systemctl --user restart einhorn-survival.service

Env:
  SURVIVAL_ROOT   repo root (default /home/fatbaby/EINHORN_SURVIVAL)
`)
}

func survivalLogPath() (string, error) {
	cfg, err := config.Resolve()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(cfg.SurvivalRoot, "server", "server.log"), nil
}

func runSurvivalLogs(args []string) int {
	fs := flag.NewFlagSet("survival logs", flag.ContinueOnError)
	lines := fs.Int("n", 40, "number of lines to show before following")
	follow := fs.Bool("f", true, "follow the log (tail -f); -f=false prints the last -n lines and exits")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	logPath, err := survivalLogPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stat(logPath); err != nil {
		fmt.Fprintf(os.Stderr, "emily survival logs: %s not found (%v)\n", logPath, err)
		return 1
	}

	tailArgs := []string{"-n", strconv.Itoa(*lines)}
	if *follow {
		tailArgs = append(tailArgs, "-f")
	}
	tailArgs = append(tailArgs, logPath)

	c := exec.Command("tail", tailArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return 1
		}
		fmt.Fprintf(os.Stderr, "emily survival logs: %v\n", err)
		return 1
	}
	return 0
}

func runSurvivalStatus() int {
	c := exec.Command("systemctl", "--user", "status", "einhorn-survival.service", "--no-pager")
	c.Env = redgardenSystemctlEnv()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	_ = c.Run() // systemctl exits non-zero for inactive units; that's still real status output
	return 0
}

func runSurvivalRestart() int {
	c := exec.Command("systemctl", "--user", "restart", "einhorn-survival.service")
	c.Env = redgardenSystemctlEnv()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "emily survival restart: %v\n", err)
		return 1
	}
	fmt.Println("einhorn-survival.service restarted.")
	return 0
}
