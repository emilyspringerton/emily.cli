// cmd/start.go — emily start
// Brings up the Emily OS agent stack in the background:
//   1. IDUNA (optional --iduna flag, via systemctl --user)
//   2. observation-watcher (PRRJECT_FATBABY, polls every 10s for observations + prime tasks)
//   3. emily-agent daemon (EMILY RSI loop, ~5m cycles with jitter)
//
// Processes are launched detached from the terminal (new session leader, logs to
// EMILY/var/logs/). emily start is idempotent — already-running processes are skipped.
//
// Usage:
//   emily start               — start observation-watcher + emily-agent
//   emily start --iduna       — also start IDUNA via systemctl --user
//   emily start --dry-run     — show what would be started

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func RunStart(args []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	startIDUNA := fs.Bool("iduna", false, "also start IDUNA via systemctl --user start iduna.service")
	startAll := fs.Bool("all", false, "also start newssite and FatBaby pipeline processes")
	withNewssite  := fs.Bool("newssite", false, "start the newssite on :8082")
	withSignalapi := fs.Bool("signalapi", false, "start signalapi on :9091 (SEC/PR signal reads)")
	dryRun := fs.Bool("dry-run", false, "show what would be started without starting anything")
	agiLoop := fs.Bool("agi", false, "enable AGI loop mode: obs-watcher passes --continue to claude so RSI cycles build persistent context")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	logDir := filepath.Join(cfg.EmilyRoot, "var", "logs")
	if !*dryRun {
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", logDir, err)
			return 3
		}
	}

	fmt.Printf("\n◈ EMILY OS — START | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// IDUNA — backbone auth service
	if *startIDUNA {
		fmt.Print("  iduna.service            ")
		if *dryRun {
			fmt.Println("[dry-run] systemctl --user start iduna.service")
		} else {
			out, _ := exec.Command("systemctl", "--user", "is-active", "iduna.service").Output()
			if strings.TrimSpace(string(out)) == "active" {
				fmt.Println("already running")
			} else {
				if err2 := exec.Command("systemctl", "--user", "start", "iduna.service").Run(); err2 != nil {
					fmt.Printf("WARN: %v\n", err2)
				} else {
					fmt.Println("started via systemctl")
				}
			}
		}
	}

	// Agent layer
	procs := []struct {
		name    string
		pat     string // pgrep -f pattern
		startFn func(*config.Config, string, bool) (bool, string, error)
		always  bool
	}{
		{"observation-watcher", "observation-watcher", func(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
			return startObservationWatcher(cfg, logDir, dryRun, *agiLoop)
		}, true},
		{"emily-agent (daemon)", "emily-agent.*--daemon|go run.*emily-agent.*--daemon", startEmilyAgent, true},
		{"newssite",   "go run.*cmd/newssite",   startNewssite,   false},
		{"signalapi",  "go run.*cmd/signalapi",  startSignalapi,  false},
	}

	for _, p := range procs {
		if !p.always && !*startAll &&
			!(p.name == "newssite" && *withNewssite) &&
			!(p.name == "signalapi" && *withSignalapi) {
			continue
		}
		fmt.Printf("  %-26s ", p.name)
		if !*dryRun {
			if pid, alive := pgrepFirst(p.pat); alive {
				fmt.Printf("already running (pid %d)\n", pid)
				continue
			}
		}
		started, note, err2 := p.startFn(cfg, logDir, *dryRun)
		if err2 != nil {
			fmt.Printf("ERROR: %v\n", err2)
		} else if started {
			fmt.Printf("started — %s\n", note)
		} else {
			fmt.Println(note)
		}
	}

	if !*startIDUNA && !*dryRun {
		fmt.Printf("\n  NOTE: use --iduna to also manage IDUNA. Agents will degrade gracefully if IDUNA is offline.\n")
	}
	if !*startAll && !*withNewssite && !*dryRun {
		fmt.Printf("  NOTE: use --newssite or --all to also start the newssite on :8082.\n")
	}
	if !*startAll && !*withSignalapi && !*dryRun {
		fmt.Printf("  NOTE: use --signalapi or --all to also start signalapi on :9091.\n")
	}

	fmt.Println()
	fmt.Println("  emily status     — check process state")
	fmt.Println("  emily watch      — tail IDUNA Apples live")
	fmt.Println()
	return 0
}

// pgrepFirst runs pgrep -f and returns the first PID found.
func pgrepFirst(pattern string) (int, bool) {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return 0, false
	}
	pid, _ := strconv.Atoi(strings.Fields(strings.TrimSpace(string(out)))[0])
	return pid, pid > 0
}

// startObservationWatcher launches the observation-watcher Go command detached
// from the terminal, with stdout+stderr routed to a log file.
// When agiLoop is true, --continue is passed to claude so RSI cycles build
// persistent context across invocations (the AGI loop pattern).
func startObservationWatcher(cfg *config.Config, logDir string, dryRun bool, agiLoop bool) (bool, string, error) {
	primeTasksDir := filepath.Join(cfg.EmilyRoot, "signals", "tasks")
	goArgs := []string{
		"run", "./cmd/observation-watcher",
		"--root", cfg.FatBabyRoot,
		"--prime-tasks", primeTasksDir,
	}
	if agiLoop {
		goArgs = append(goArgs, "--continue")
	}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(logDir, "observation-watcher.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = cfg.FatBabyRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start observation-watcher: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startEmilyAgent launches the emily-agent RSI daemon detached from the terminal.
// IDUNA credentials are wired from cfg into the child process environment.
func startEmilyAgent(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	agentDir := filepath.Join(cfg.EmilyRoot, "emily-agent")
	goArgs := []string{"run", ".", "--", "--daemon"}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), agentDir), nil
	}

	logPath := filepath.Join(logDir, "emily-agent.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = agentDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = wireIDUNAEnv(os.Environ(), cfg)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start emily-agent: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startNewssite launches the newssite on :8082 detached from the terminal.
// It reads from var/secwatch and loads entity-graph, eps, commentary, and guidance stores.
func startNewssite(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	goArgs := []string{
		"run", "./cmd/newssite",
		"-store", "var/secwatch",
		"-graph-dir", "var/entity-graph",
		"-eps-dir", "var/eps",
		"-commentary-dir", "var/commentary",
		"-guidance-dir", "var/guidance",
		"-earnings-cal-dir", "var/earnings-calendar",
	}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(cfg.FatBabyRoot, "var", "logs", "newssite.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	env := os.Environ()
	if os.Getenv("EMILY_BASE_URL") == "" {
		env = append(env, "EMILY_BASE_URL=http://localhost:8086")
	}

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = cfg.FatBabyRoot
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start newssite: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startSignalapi launches the signalapi SEC/PR signal reader on :9091.
func startSignalapi(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	goArgs := []string{
		"run", "./cmd/signalapi",
		"-addr", ":9091",
		"-store", "var/secwatch",
	}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(cfg.FatBabyRoot, "var", "logs", "signalapi.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command("go", goArgs...)
	cmd.Dir = cfg.FatBabyRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start signalapi: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// wireIDUNAEnv returns env with IDUNA_* vars set from cfg, overriding any
// existing values. This ensures the child process has credentials even if the
// parent shell didn't have them set.
func wireIDUNAEnv(env []string, cfg *config.Config) []string {
	set := func(key, val string) {
		if val == "" {
			return
		}
		prefix := key + "="
		for i, e := range env {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + val
				return
			}
		}
		env = append(env, prefix+val)
	}
	set("IDUNA_BASE_URL", cfg.IDUNABaseURL)
	set("IDUNA_AGENT_NAME", cfg.IDUNAAgentName)
	set("IDUNA_AGENT_SECRET", cfg.IDUNAAgentSecret)
	return env
}
