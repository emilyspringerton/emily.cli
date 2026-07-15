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
	startIDUNA      := fs.Bool("iduna", false, "also start IDUNA via systemctl --user start iduna.service")
	startAll        := fs.Bool("all", false, "also start newssite and FatBaby pipeline processes")
	withNewssite      := fs.Bool("newssite", false, "start the newssite on :8082")
	withSignalapi     := fs.Bool("signalapi", false, "start signalapi on :9091 (SEC/PR signal reads)")
	withEntityGraph   := fs.Bool("entity-graph", false, "start entity-graph builder")
	withEpsReconciler := fs.Bool("eps-reconciler", false, "start EPS reconciler")
	withEpsProcessor  := fs.Bool("eps-processor", false, "start EPS processor")
	withShankpit      := fs.Bool("shankpit", false, "start shank_go_server on :6969 + emily-bot fill players (never bundled into --all — starts a live game server)")
	withEarnings    := fs.Bool("earnings-alert", false, "install + enable systemd timer for weekly earnings-alert email (Monday 07:30 UTC)")
	botCount        := fs.Int("bots", 2, "number of emily-bot fill players to launch with --shankpit")
	dryRun          := fs.Bool("dry-run", false, "show what would be started without starting anything")
	agiLoop         := fs.Bool("agi", false, "enable AGI loop mode: obs-watcher passes --continue to claude so RSI cycles build persistent context")

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

	failed := false

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
					failed = true
				} else {
					fmt.Println("started via systemctl")
				}
			}
		}
	}

	// Agent layer
	procs := []struct {
		name         string
		pat          string // pgrep -f pattern
		startFn      func(*config.Config, string, bool) (bool, string, error)
		always       bool
		flag         *bool // explicit opt-in flag, if any
		bundledInAll bool  // also started by --all
	}{
		{"observation-watcher", "cmd/observation-watcher --root", func(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
			return startObservationWatcher(cfg, logDir, dryRun, *agiLoop)
		}, true, nil, false},
		{"emily-agent (daemon)", "emily-agent.*--daemon|go run.*emily-agent.*--daemon", startEmilyAgent, true, nil, false},
		{"newssite",       "go run.*cmd/newssite",       startNewssite,      false, withNewssite, true},
		{"signalapi",      "go run.*cmd/signalapi",      startSignalapi,     false, withSignalapi, true},
		{"entity-graph",   "go run.*cmd/entity-graph",   startEntityGraph,   false, withEntityGraph, true},
		{"eps-reconciler", "go run.*cmd/eps-reconciler", startEpsReconciler, false, withEpsReconciler, true},
		{"eps-processor",  "go run.*cmd/eps-processor",  startEpsProcessor,  false, withEpsProcessor, true},
		// shank_go_server is deliberately NOT bundledInAll: --all is meant for
		// the FatBaby pipeline, not for spinning up a live game server + bots.
		{"shank_go_server", "shank_go_server", startShankpit, false, withShankpit, false},
	}

	for _, p := range procs {
		if !p.always {
			explicit := p.flag != nil && *p.flag
			if !explicit && !(*startAll && p.bundledInAll) {
				continue
			}
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
			failed = true
		} else if started {
			fmt.Printf("started — %s\n", note)
		} else {
			fmt.Println(note)
		}
	}

	// After the server is up, launch fill bots with a short delay so the
	// server socket is listening before they try to connect.
	if *withShankpit && *botCount > 0 {
		if !*dryRun {
			time.Sleep(1500 * time.Millisecond)
		}
		for i := 0; i < *botCount; i++ {
			fmt.Printf("  %-26s ", fmt.Sprintf("emily-bot[%d]", i))
			started, note, err2 := startEmilyBot(cfg, logDir, *dryRun, i)
			if err2 != nil {
				fmt.Printf("ERROR: %v\n", err2)
				failed = true
			} else if started {
				fmt.Printf("started — %s\n", note)
			} else {
				fmt.Println(note)
			}
			if !*dryRun && i < *botCount-1 {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// earnings-alert systemd timer install.
	if *withEarnings {
		fmt.Printf("  %-26s ", "earnings-alert.timer")
		if err := runInstallEarningsAlert(cfg, *dryRun); err != nil {
			fmt.Printf("ERROR: %v\n", err)
			failed = true
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
	if !*startAll && !*withEntityGraph && !*dryRun {
		fmt.Printf("  NOTE: use --entity-graph or --all to also start the entity-graph builder.\n")
	}
	if !*startAll && !*withEpsReconciler && !*dryRun {
		fmt.Printf("  NOTE: use --eps-reconciler or --all to also start the EPS reconciler.\n")
	}
	if !*startAll && !*withEpsProcessor && !*dryRun {
		fmt.Printf("  NOTE: use --eps-processor or --all to also start the EPS processor.\n")
	}
	if !*withShankpit && !*dryRun {
		fmt.Printf("  NOTE: use --shankpit to also start SHANKPIT server + %d emily-bot fill players (not included in --all).\n", *botCount)
	}

	fmt.Println()
	fmt.Println("  emily status     — check process state")
	fmt.Println("  emily watch      — tail IDUNA Apples live")
	fmt.Println()
	if failed {
		return 1
	}
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

// startEntityGraph launches the entity-graph builder detached from the terminal.
func startEntityGraph(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	goArgs := []string{"run", "./cmd/entity-graph", "-store", "./var/secwatch"}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(cfg.FatBabyRoot, "var", "logs", "entity-graph.log")
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
		return false, "", fmt.Errorf("start entity-graph: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startEpsReconciler launches the EPS reconciler detached from the terminal.
func startEpsReconciler(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	goArgs := []string{"run", "./cmd/eps-reconciler", "-store", "./var/secwatch", "-eps-dir", "./var/eps"}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(cfg.FatBabyRoot, "var", "logs", "eps-reconciler.log")
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
		return false, "", fmt.Errorf("start eps-reconciler: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startEpsProcessor launches the EPS processor detached from the terminal.
func startEpsProcessor(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	goArgs := []string{
		"run", "./cmd/eps-processor",
		"-body-store", "./var/prwatch-body",
		"-discovery-store", "./var/prwatch",
		"-eps-dir", "./var/eps",
	}

	if dryRun {
		return false, fmt.Sprintf("[dry-run] go %s  (dir: %s)", strings.Join(goArgs, " "), cfg.FatBabyRoot), nil
	}

	logPath := filepath.Join(cfg.FatBabyRoot, "var", "logs", "eps-processor.log")
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
		return false, "", fmt.Errorf("start eps-processor: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// startShankpit builds (if needed) and launches shank_go_server on :6969.
// Uses the prebuilt binary at SHANKPIT/bin/shank_go_server when present;
// builds with GOWORK=off otherwise. Logs to EMILY/var/logs/shankpit.log.
func startShankpit(cfg *config.Config, logDir string, dryRun bool) (bool, string, error) {
	binPath := filepath.Join(cfg.ShankpitRoot, "bin", "shank_go_server")

	if dryRun {
		return false, fmt.Sprintf("[dry-run] %s  (dir: %s)", binPath, cfg.ShankpitRoot), nil
	}

	// Build if binary is missing.
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		buildCmd := exec.Command("go", "build", "-o", binPath, "./apps2/server-go/")
		buildCmd.Dir = cfg.ShankpitRoot
		buildCmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err2 := buildCmd.CombinedOutput(); err2 != nil {
			return false, "", fmt.Errorf("build shank_go_server: %w\n%s", err2, out)
		}
	}

	logPath := filepath.Join(logDir, "shankpit.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	adminToken := os.Getenv("SHANKPIT_ADMIN_TOKEN")
	args := []string{"--admin-port", "6970"}
	if adminToken != "" {
		args = append(args, "--admin-token", adminToken)
	}
	cmd := exec.Command(binPath, args...)
	cmd.Dir = cfg.ShankpitRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start shank_go_server: %w", err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s (admin :6970)", cmd.Process.Pid, logPath), nil
}

// startEmilyBot launches one emily-bot fill player against the local game server.
// Uses the prebuilt binary at SHANKPIT/bin/emily-bot; builds it if absent.
// idx is used to stagger log file names (emily-bot-0.log, emily-bot-1.log, ...).
func startEmilyBot(cfg *config.Config, logDir string, dryRun bool, idx int) (bool, string, error) {
	binPath := filepath.Join(cfg.ShankpitRoot, "bin", "emily-bot")

	if dryRun {
		return false, fmt.Sprintf("[dry-run] %s -host 127.0.0.1 -port 6969", binPath), nil
	}

	// Build if binary is missing.
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		buildCmd := exec.Command("go", "build", "-o", binPath, "./apps2/emily-bot/")
		buildCmd.Dir = cfg.ShankpitRoot
		buildCmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err2 := buildCmd.CombinedOutput(); err2 != nil {
			return false, "", fmt.Errorf("build emily-bot: %w\n%s", err2, out)
		}
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("emily-bot-%d.log", idx))
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, "", fmt.Errorf("open log: %w", err)
	}

	cmd := exec.Command(binPath, "-host", "127.0.0.1", "-port", "6969")
	cmd.Dir = cfg.ShankpitRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return false, "", fmt.Errorf("start emily-bot[%d]: %w", idx, err)
	}
	logFile.Close()
	return true, fmt.Sprintf("pid %d → %s", cmd.Process.Pid, logPath), nil
}

// runInstallEarningsAlert builds the earnings-alert binary (if absent), writes
// ~/.config/systemd/user/earnings-alert.{service,timer}, and enables the timer.
// On dry-run it prints the unit content without writing anything.
func runInstallEarningsAlert(cfg *config.Config, dryRun bool) error {
	home := os.Getenv("HOME")
	fatBabyRoot := cfg.FatBabyRoot
	binPath := filepath.Join(fatBabyRoot, "bin", "earnings-alert")
	calDir := filepath.Join(fatBabyRoot, "var", "earnings-calendar")
	alertTo := os.Getenv("ALERT_TO")
	if alertTo == "" {
		alertTo = "emilyspringerton@gmail.com"
	}

	svcContent := fmt.Sprintf(`[Unit]
Description=Emily earnings-alert — weekly earnings email
After=network-online.target

[Service]
Type=oneshot
WorkingDirectory=%s
ExecStart=%s -cal-dir %s -to %s -days 7
StandardOutput=journal
StandardError=journal
`, fatBabyRoot, binPath, calDir, alertTo)

	timerContent := `[Unit]
Description=Run Emily earnings-alert every Monday at 07:30 UTC

[Timer]
OnCalendar=Mon 07:30 UTC
Persistent=true

[Install]
WantedBy=timers.target
`

	if dryRun {
		fmt.Println("[dry-run]")
		fmt.Printf("  would build: %s\n", binPath)
		fmt.Printf("  would write: %s/.config/systemd/user/earnings-alert.service\n", home)
		fmt.Printf("  would write: %s/.config/systemd/user/earnings-alert.timer\n", home)
		fmt.Println("  would run:  systemctl --user daemon-reload && enable --now earnings-alert.timer")
		return nil
	}

	// Build binary if missing.
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		if err2 := os.MkdirAll(filepath.Dir(binPath), 0o755); err2 != nil {
			return fmt.Errorf("mkdir bin: %w", err2)
		}
		build := exec.Command("go", "build", "-o", binPath, "./cmd/earnings-alert")
		build.Dir = fatBabyRoot
		if out, err2 := build.CombinedOutput(); err2 != nil {
			return fmt.Errorf("build earnings-alert: %w\n%s", err2, out)
		}
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return fmt.Errorf("mkdir systemd/user: %w", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "earnings-alert.service"), []byte(svcContent), 0o644); err != nil {
		return fmt.Errorf("write service: %w", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, "earnings-alert.timer"), []byte(timerContent), 0o644); err != nil {
		return fmt.Errorf("write timer: %w", err)
	}

	exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := exec.Command("systemctl", "--user", "enable", "--now", "earnings-alert.timer").Run(); err != nil {
		fmt.Printf("installed (enable failed: %v — run manually: systemctl --user enable --now earnings-alert.timer)\n", err)
		return nil
	}
	fmt.Println("timer installed + enabled — fires Mon 07:30 UTC")
	return nil
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
