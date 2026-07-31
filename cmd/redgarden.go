package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// RunRedgarden is the entry point for `emily redgarden <subcommand>`.
//
// Subcommands:
//
//	bots [N]   — set the persistent bot-pool size (default 20) and restart the live pool
//	status     — show the current live bot count + service state
//
// The live REDGARDEN bot pool is systemd-user-supervised. Its ExecStart= line
// (scripts/run_bot_pool.sh <N>) controls how many bots stay queued at the bot-pool
// matchmaker (:7778, lobby_size 20). N==20 is fully self-sustaining (matches start
// instantly, continuous data) but leaves no slot for a human to join at :7778.
// N<20 leaves (20-N) human slots open at :7778 but no match starts until they fill.
// The always-open player-only pool at :7779 is unaffected either way.
func RunRedgarden(args []string) int {
	if len(args) == 0 {
		return runRedgardenStatus()
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "bots":
		n := 20
		if len(rest) >= 1 {
			parsed, err := strconv.Atoi(rest[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "emily redgarden bots: invalid bot count %q\n", rest[0])
				return 1
			}
			n = parsed
		}
		if n < 0 || n > 20 {
			fmt.Fprintf(os.Stderr, "emily redgarden bots: count must be 0-20 (lobby_size is fixed at 20), got %d\n", n)
			return 1
		}
		return runRedgardenSetBots(n)
	case "status", "":
		return runRedgardenStatus()
	default:
		fmt.Fprintf(os.Stderr, "emily redgarden: unknown subcommand %q\n\n", sub)
		printRedgardenUsage()
		return 1
	}
}

func printRedgardenUsage() {
	fmt.Print(`emily redgarden — REDGARDEN persistent bot-pool control

Subcommands:
  bots [N]   set the live bot-pool size to N (default 20 when omitted) and
             restart redgarden-bot-pool.service to apply it
  status     show the current live bot count + service state

N==20 is fully self-sustaining (matches start immediately, continuous data)
but leaves no human slot open at the bot-pool matchmaker (:7778). N<20 opens
(20-N) human slots at :7778, but no match starts there until they fill. The
player-only pool at :7779 always stays open for humans either way.
`)
}

func redgardenUnitPath() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(u.HomeDir, ".config/systemd/user/redgarden-bot-pool.service"), nil
}

var redgardenExecStartRe = regexp.MustCompile(`(?m)^(ExecStart=.*run_bot_pool\.sh )(\d+)\s*$`)
var redgardenDescRe = regexp.MustCompile(`\((\d+) bots, fully self-sustaining`)

func readRedgardenBotCount(unitPath string) (int, []byte, error) {
	data, err := os.ReadFile(unitPath)
	if err != nil {
		return 0, nil, err
	}
	m := redgardenExecStartRe.FindSubmatch(data)
	if m == nil {
		return 0, data, fmt.Errorf("could not find ExecStart=...run_bot_pool.sh <N> line in %s", unitPath)
	}
	n, err := strconv.Atoi(string(m[2]))
	if err != nil {
		return 0, data, fmt.Errorf("parse bot count: %w", err)
	}
	return n, data, nil
}

func runRedgardenSetBots(n int) int {
	unitPath, err := redgardenUnitPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	_, data, err := readRedgardenBotCount(unitPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily redgarden bots: %v\n", err)
		fmt.Fprintln(os.Stderr, "(is the live unit deployed at ~/.config/systemd/user/redgarden-bot-pool.service?)")
		return 1
	}

	updated := redgardenExecStartRe.ReplaceAll(data, []byte(fmt.Sprintf("${1}%d", n)))
	updated = redgardenDescRe.ReplaceAll(updated, []byte(fmt.Sprintf("(%d bots, fully self-sustaining", n)))

	if err := os.WriteFile(unitPath, updated, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "emily redgarden bots: write %s: %v\n", unitPath, err)
		return 1
	}

	env := redgardenSystemctlEnv()

	reload := exec.Command("systemctl", "--user", "daemon-reload")
	reload.Env = env
	reload.Stdout, reload.Stderr = os.Stdout, os.Stderr
	if err := reload.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "emily redgarden bots: daemon-reload: %v\n", err)
		return 1
	}

	restart := exec.Command("systemctl", "--user", "restart", "redgarden-bot-pool.service")
	restart.Env = env
	restart.Stdout, restart.Stderr = os.Stdout, os.Stderr
	if err := restart.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "emily redgarden bots: restart: %v\n", err)
		return 1
	}

	fmt.Printf("REDGARDEN bot pool set to %d bots and restarted.\n", n)
	if n >= 20 {
		fmt.Println("Pool is fully self-sustaining -- no human slot open at :7778 (join :7779 instead, or lower the count).")
	} else {
		fmt.Printf("%d human slot(s) open at :7778 -- no match starts there until they fill.\n", 20-n)
	}
	return 0
}

func runRedgardenStatus() int {
	unitPath, err := redgardenUnitPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	n, _, err := readRedgardenBotCount(unitPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily redgarden status: %v\n", err)
		return 1
	}
	fmt.Printf("Configured bot count: %d\n", n)
	if n >= 20 {
		fmt.Println("  -> self-sustaining, :7778 has no open human slot")
	} else {
		fmt.Printf("  -> %d human slot(s) open at :7778\n", 20-n)
	}

	env := redgardenSystemctlEnv()
	svcs := []string{"redgarden-bot-pool.service", "redgarden-matchmaker-bots.service", "redgarden-matchmaker-players.service"}
	for _, svc := range svcs {
		c := exec.Command("systemctl", "--user", "is-active", svc)
		c.Env = env
		out, _ := c.Output()
		fmt.Printf("%-36s %s\n", svc, strings.TrimSpace(string(out)))
	}
	return 0
}

// redgardenSystemctlEnv ensures XDG_RUNTIME_DIR is set for `systemctl --user`
// to find the caller's user session bus, even if the parent process's env is bare.
func redgardenSystemctlEnv() []string {
	env := os.Environ()
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		return env
	}
	if u, err := user.Current(); err == nil {
		return append(env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%s", u.Uid))
	}
	return env
}
