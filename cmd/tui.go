// cmd/tui.go — emily tui
// Bloomberg-style terminal dashboard for the Einhorn Industrial agent stack.
//
// Layout (four-row grid):
//   Row 0: Header — system name, timestamp, process indicators          (3 lines)
//   Row 1: Main   — repos+tasks | apple feed | process health           (flexible)
//   Row 2: Log    — RSI activity log, streams hotkey command output     (9 lines)
//   Row 3: Footer — version + key hints                                 (1 line)
//
// Hotkeys:
//   F1 — full RSI tic-toc cycle (TIC→TOCK→ENTROPY→ANALYZE), streams to log
//   F2 — run Tyler emily.sh 2 iterations, streams to log
//   F3 — emily start, streams to log
//   r  — force refresh all panels
//   q  — quit

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// tuiLogger appends timestamped, color-tagged lines to a tview.TextView.
// All methods are goroutine-safe.
type tuiLogger struct {
	mu       sync.Mutex
	lines    []string
	maxLines int
	tv       *tview.TextView
	app      *tview.Application
}

func newTUILogger(tv *tview.TextView, app *tview.Application) *tuiLogger {
	return &tuiLogger{tv: tv, app: app, maxLines: 500}
}

// log appends one line. phaseTag is the label shown in brackets (e.g. "TIC", "TOCK").
// color is a tview color tag prefix (e.g. "[yellow]", "[cyan]").
func (l *tuiLogger) log(color, phaseTag, msg string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[darkgray]%s[-] %s[%-4s][-] %s", ts, color, phaseTag, msg)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.maxLines {
		l.lines = l.lines[len(l.lines)-l.maxLines:]
	}
	joined := strings.Join(l.lines, "\n")
	l.mu.Unlock()
	l.app.QueueUpdateDraw(func() {
		l.tv.SetText(joined)
		l.tv.ScrollToEnd()
	})
}

func (l *tuiLogger) logf(color, phase, format string, args ...interface{}) {
	l.log(color, phase, fmt.Sprintf(format, args...))
}

// streamCmd runs a command and writes each stdout/stderr line to the logger.
// Returns the exit error (nil on success).
func (l *tuiLogger) streamCmd(color, phase string, cmd *exec.Cmd) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return err
	}
	pw.Close()

	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			l.log(color, phase, line)
		}
	}
	pr.Close()
	return cmd.Wait()
}

// tuiState holds all data rendered by the dashboard panels.
type tuiState struct {
	repos       []repoStatus
	apples      []iduna.Apple
	processes   []processState
	tasks       tuiTaskState
	rsiLoop     rsiLoopState
	tokenEst    tokenEstimate
	iduna       bool
	refreshedAt time.Time
}

type tuiTaskState struct {
	Count  int
	Oldest string
	Files  []string
}

type rsiLoopState struct {
	Running   bool
	Iteration int
	LastAt    string
	TaskID    string
	Phase     string
}

type tokenEstimate struct {
	TodayK    float64
	LastRunK  float64
	RunsToday int
}

// evictStaleTUI kills any other running `emily tui` process and resets the
// terminal to sane state. tview leaves the terminal in raw/alt-screen mode
// when killed with Ctrl-C, so the next launch gets a broken terminal unless
// we clean up first.
func evictStaleTUI() {
	self := os.Getpid()
	out, err := exec.Command("pgrep", "-f", "emily tui").Output()
	if err != nil {
		return // no matches
	}
	evicted := 0
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid == self {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		proc.Signal(syscall.SIGTERM)
		evicted++
	}
	if evicted > 0 {
		time.Sleep(150 * time.Millisecond)
		// SIGKILL anything that didn't respond to SIGTERM
		for _, field := range strings.Fields(string(out)) {
			pid, err := strconv.Atoi(field)
			if err != nil || pid == self {
				continue
			}
			proc, _ := os.FindProcess(pid)
			if proc != nil {
				proc.Signal(syscall.SIGKILL)
			}
		}
		fmt.Fprintf(os.Stderr, "evicted %d stale emily tui process(es)\n", evicted)
	}
	// Restore terminal regardless — a previous tview session may have left it
	// in raw or alt-screen mode even if the process is already dead.
	exec.Command("stty", "sane").Run()
}

// RunTUI launches the Bloomberg-style terminal dashboard.
func RunTUI(args []string) int {
	evictStaleTUI()
	cfg, _ := config.Resolve()

	app := tview.NewApplication()

	// ── Panels ──────────────────────────────────────────────────────────────
	header := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	repoPanel := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false)

	feedPanel := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(200)

	healthPanel := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false)

	logPanel := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetMaxLines(500).
		SetWrap(true)

	footer := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	for _, tv := range []*tview.TextView{repoPanel, feedPanel, healthPanel} {
		tv.SetBorder(true)
	}
	logPanel.SetBorder(true)

	repoPanel.SetTitle("[ REPOS + TASKS ]").SetTitleAlign(tview.AlignLeft)
	feedPanel.SetTitle("[ APPLE FEED (live) ]").SetTitleAlign(tview.AlignLeft)
	healthPanel.SetTitle("[ SYSTEM HEALTH ]").SetTitleAlign(tview.AlignLeft)
	logPanel.SetTitle("[ RSI ACTIVITY LOG ]").SetTitleAlign(tview.AlignLeft)

	logger := newTUILogger(logPanel, app)

	// ── Layout ───────────────────────────────────────────────────────────────
	// Rows: header(3) | main(flexible) | log(9) | footer(1)
	grid := tview.NewGrid().
		SetRows(3, 0, 9, 1).
		SetColumns(34, 0, 32).
		SetBorders(false)

	grid.AddItem(header, 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(repoPanel, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(feedPanel, 1, 1, 1, 1, 0, 0, false)
	grid.AddItem(healthPanel, 1, 2, 1, 1, 0, 0, false)
	grid.AddItem(logPanel, 2, 0, 1, 3, 0, 0, false)
	grid.AddItem(footer, 3, 0, 1, 3, 0, 0, false)

	// ── State + render ───────────────────────────────────────────────────────
	var state tuiState
	var feedHighWater int64

	refresh := func() {
		state = collectState(cfg)
		for _, a := range state.apples {
			if a.ID > feedHighWater {
				feedHighWater = a.ID
			}
		}
		app.QueueUpdateDraw(func() {
			renderHeader(header, &state)
			renderRepoPanel(repoPanel, &state)
			renderFeedPanel(feedPanel, &state)
			renderHealthPanel(healthPanel, &state)
			renderFooter(footer, &state)
		})
	}

	// Initial render
	state = collectState(cfg)
	renderHeader(header, &state)
	renderRepoPanel(repoPanel, &state)
	renderFeedPanel(feedPanel, &state)
	renderHealthPanel(healthPanel, &state)
	renderFooter(footer, &state)
	logger.log("[darkgray]", "INFO", "emily tui ready — F1=RSI cycle  F2=Tyler  F3=start system  r=refresh  q=quit")

	// 15s data ticker — runs collectState (git, IDUNA, processes)
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			refresh()
		}
	}()
	defer ticker.Stop()

	// 1s clock ticker — redraws only the header with time.Now()
	clockTicker := time.NewTicker(time.Second)
	go func() {
		for range clockTicker.C {
			app.QueueUpdateDraw(func() { renderHeader(header, &state) })
		}
	}()
	defer clockTicker.Stop()

	// ── Keyboard ──────────────────────────────────────────────────────────────
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {

		case tcell.KeyF1:
			// Full RSI tic-toc cycle: TIC → TOCK → ENTROPY → ANALYZE
			go func() {
				claudeRunsDir := filepath.Join(cfg.FatBabyRoot, "claude-runs")

				// ── TIC ──────────────────────────────────────────────────
				logger.log("[yellow]", "TIC", "Dispatching RSI token-efficiency task to Emily Prime...")
				out, err := exec.Command("emily", "prime-task", "--preset", "rsi-token-report").CombinedOutput()
				if err != nil {
					logger.logf("[red]", "ERR", "prime-task failed: %s", firstLine(string(out)))
					return
				}
				taskID := parsePrimeTaskID(string(out))
				logger.logf("[yellow]", "TIC", "task written: %s", taskID)
				logger.log("[yellow]", "TIC", "obs-watcher picks up within 10s → claude --dangerously-skip-permissions")

				// ── TOCK ─────────────────────────────────────────────────
				logger.log("[cyan]", "TOCK", "Polling claude-runs/ for Claude Code completion (max 3 min)...")
				initialRuns := countFilesInDir(claudeRunsDir)
				waitStart := time.Now()
				for {
					current := countFilesInDir(claudeRunsDir)
					if current > initialRuns {
						logger.logf("[cyan]", "TOCK", "Claude run detected — %d total runs in claude-runs/", current)
						break
					}
					elapsed := time.Since(waitStart)
					if elapsed > 3*time.Minute {
						logger.log("[yellow]", "TOCK", "3 min timeout — obs-watcher may not be running (emily start to fix)")
						break
					}
					time.Sleep(10 * time.Second)
					logger.logf("[darkgray]", "TOCK", "...%ds elapsed, waiting for new run file", int(elapsed.Seconds()))
				}

				// ── ENTROPY ──────────────────────────────────────────────
				tylerSh := "/home/fatbaby/TYLER/emily.sh"
				if _, serr := os.Stat(tylerSh); serr == nil {
					pending := tylerBacklogPending()
					if pending == 0 {
						logger.log("[magenta]", "ENTR", "Tyler backlog empty — skipping entropy injection")
					} else {
						logger.logf("[magenta]", "ENTR", "Running Tyler emily.sh 2 iterations (%d tasks pending)...", pending)
						tylerCmd := exec.Command("bash", "-c", "cd /home/fatbaby/TYLER && ./emily.sh 2 2>&1")
						if serr := logger.streamCmd("[magenta]", "ENTR", tylerCmd); serr != nil {
							logger.logf("[yellow]", "ENTR", "Tyler returned non-zero (%v) — continuing", serr)
						} else {
							logger.log("[magenta]", "ENTR", "Tyler entropy injection complete")
						}
					}
				} else {
					logger.log("[darkgray]", "ENTR", "TYLER/emily.sh not found — skipping entropy")
				}

				// ── ANALYZE ──────────────────────────────────────────────
				logger.log("[green]", "ANLZ", "Posting RSI cycle observation to Emily OS...")
				obsMsg := fmt.Sprintf("RSI tic-toc cycle complete — task %s dispatched, Tyler ran 2 builds", taskID)
				obsOut, obsErr := exec.Command("emily", "observe", "-s", "info", obsMsg).CombinedOutput()
				if obsErr != nil {
					logger.logf("[yellow]", "ANLZ", "observe warn (IDUNA offline?): %s", firstLine(string(obsOut)))
				} else {
					logger.log("[green]", "ANLZ", "Observation posted → obs-watcher triggers next cycle")
				}

				logger.log("[green]", " ✓  ", "RSI tic-toc cycle complete — system advancing")
				refresh()
			}()
			return nil

		case tcell.KeyF2:
			// Tyler RSI loop — stream output to log
			go func() {
				tylerSh := "/home/fatbaby/TYLER/emily.sh"
				if _, err := os.Stat(tylerSh); err != nil {
					logger.log("[red]", "ERR", "TYLER/emily.sh not found")
					return
				}
				pending := tylerBacklogPending()
				if pending == 0 {
					logger.log("[magenta]", "ENTR", "Tyler backlog empty — nothing to build")
					return
				}
				logger.logf("[magenta]", "ENTR", "Starting Tyler emily.sh 2 iterations (%d pending tasks)...", pending)
				cmd := exec.Command("bash", "-c", "cd /home/fatbaby/TYLER && ./emily.sh 2 2>&1")
				if err := logger.streamCmd("[magenta]", "ENTR", cmd); err != nil {
					logger.logf("[yellow]", "ENTR", "Tyler finished with error: %v", err)
				} else {
					logger.log("[green]", "ENTR", "Tyler RSI loop complete")
				}
				refresh()
			}()
			return nil

		case tcell.KeyF3:
			// Start Emily OS stack — stream to log
			go func() {
				logger.log("[cyan]", "SYS ", "Starting Emily OS stack (obs-watcher + emily-agent)...")
				cmd := exec.Command("emily", "start")
				if err := logger.streamCmd("[cyan]", "SYS ", cmd); err != nil {
					logger.logf("[yellow]", "SYS ", "emily start returned: %v", err)
				} else {
					logger.log("[green]", "SYS ", "Stack started — use emily status to verify")
				}
				refresh()
			}()
			return nil

		case tcell.KeyF4:
			// Tail rsi-loop.log in suspended terminal
			go func() {
				logPath := "/home/fatbaby/EMILY/var/logs/rsi-loop.log"
				logger.logf("[darkgray]", "LOG ", "Suspending TUI — tailing %s (Ctrl-C to return)", logPath)
				app.Suspend(func() {
					exec.Command("tail", "-f", logPath).Run()
				})
				logger.log("[darkgray]", "LOG ", "Returned from log tail")
			}()
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
			case 'r', 'R':
				go func() {
					logger.log("[darkgray]", "INFO", "Refreshing all panels...")
					refresh()
					logger.log("[darkgray]", "INFO", "Panels refreshed")
				}()
				return nil
			case 'q', 'Q':
				app.Stop()
				return nil
			case 'h', 'H':
				logger.log("[white]", "HELP", "F1=RSI tic-toc  F2=Tyler entropy  F3=start system  F4=tail logs  r=refresh  q=quit")
				return nil
			}
		}
		return event
	})

	if err := app.SetRoot(grid, true).EnableMouse(false).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}

// ── Helpers for hotkey handlers ───────────────────────────────────────────────

func parsePrimeTaskID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "task_id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "task_id:"))
		}
	}
	return "unknown"
}

func countFilesInDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func tylerBacklogPending() int {
	data, err := os.ReadFile("/home/fatbaby/TYLER/BACKLOG.md")
	if err != nil {
		return -1
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "- [ ]") {
			count++
		}
	}
	return count
}

// ── Data collection ───────────────────────────────────────────────────────────

func collectState(cfg *config.Config) tuiState {
	s := tuiState{refreshedAt: time.Now()}

	for _, r := range repoDefs {
		s.repos = append(s.repos, gitRepoStatus(r.Name, r.Path))
	}

	if cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		apples, _ := client.ListApples(iduna.AppleListFilters{Limit: 30})
		s.apples = apples
		if len(apples) > 0 {
			s.iduna = true
		}
	} else {
		_, err := exec.Command("curl", "-sf", "--max-time", "2",
			cfg.IDUNABaseURL+"/api/v1/apples").Output()
		s.iduna = err == nil
	}

	s.processes = collectProcesses(cfg)
	s.tasks = collectTasks(cfg)
	s.rsiLoop = collectRSIState(cfg)
	s.tokenEst = estimateTokens(cfg)
	return s
}

func gitRepoStatus(name, path string) repoStatus {
	rs := repoStatus{Name: name}
	if b, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		rs.Branch = strings.TrimSpace(string(b))
	}
	if h, err := exec.Command("git", "-C", path, "log", "-1", "--format=%h").Output(); err == nil {
		rs.LastCommit = strings.TrimSpace(string(h))
	}
	if st, err := exec.Command("git", "-C", path, "status", "--porcelain").Output(); err == nil {
		count := 0
		for _, l := range strings.Split(strings.TrimSpace(string(st)), "\n") {
			if l != "" {
				count++
			}
		}
		rs.DirtyCount = count
	}
	return rs
}

func collectTasks(cfg *config.Config) tuiTaskState {
	tasksDir := filepath.Join(cfg.EmilyRoot, "signals", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return tuiTaskState{}
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && !strings.HasPrefix(e.Name(), ".") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	ts := tuiTaskState{Count: len(files), Files: files}
	if len(files) > 0 && len(files[0]) >= 18 {
		ts.Oldest = humanAge(files[0])
	}
	return ts
}

func collectRSIState(cfg *config.Config) rsiLoopState {
	b, err := os.ReadFile(filepath.Join(cfg.EmilyRoot, "var", "rsi-loop-state.json"))
	if err != nil {
		return rsiLoopState{}
	}
	var raw struct {
		Iteration int    `json:"iteration"`
		TaskID    string `json:"task_id"`
		Phase     string `json:"phase"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return rsiLoopState{}
	}
	ts := raw.Timestamp
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		ts = humanDuration(time.Since(t)) + " ago"
	}
	return rsiLoopState{Running: true, Iteration: raw.Iteration, LastAt: ts, TaskID: raw.TaskID, Phase: raw.Phase}
}

func estimateTokens(cfg *config.Config) tokenEstimate {
	entries, err := os.ReadDir(filepath.Join(cfg.FatBabyRoot, "claude-runs"))
	if err != nil {
		return tokenEstimate{}
	}
	today := time.Now().UTC().Format("2006-01-02")
	runsToday := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), today) {
			runsToday++
		}
	}
	return tokenEstimate{TodayK: float64(runsToday) * 8.2, LastRunK: 8.2, RunsToday: runsToday}
}

// ── Renderers ─────────────────────────────────────────────────────────────────

func renderHeader(tv *tview.TextView, s *tuiState) {
	idunaColor := "[red]●[-]"
	if s.iduna {
		idunaColor = "[green]●[-]"
	}
	obsColor, agentColor := "[red]●[-]", "[red]●[-]"
	for _, p := range s.processes {
		switch p.Name {
		case "obs-watcher":
			if p.Running {
				obsColor = "[green]●[-]"
			}
		case "emily-agent":
			if p.Running {
				agentColor = "[green]●[-]"
			}
		}
	}
	appleCount := 0
	if len(s.apples) > 0 {
		appleCount = int(s.apples[0].ID)
	}
	tv.SetText(
		fmt.Sprintf("[white::b] EINHORN INDUSTRIAL [-] [yellow::b]◈[-] [white::b]EMILY OS[-]%s[darkgray]%s[-]\n",
			strings.Repeat(" ", 20), time.Now().Format("2006-01-02 15:04:05")) +
			fmt.Sprintf("  IDUNA:%s  OBS:%s  EMILY-AGENT:%s  [darkgray]Apples:%d  Repos:%d[-]",
				idunaColor, obsColor, agentColor, appleCount, len(s.repos)),
	)
}

func renderRepoPanel(tv *tview.TextView, s *tuiState) {
	var sb strings.Builder
	sb.WriteString("[white::b]REPOS[-]\n")
	for _, r := range s.repos {
		dirty := " [green]✓[-]"
		if r.DirtyCount > 0 {
			dirty = fmt.Sprintf(" [red]✗%d[-]", r.DirtyCount)
		}
		name := r.Name
		if len(name) > 10 {
			name = name[:10]
		}
		branch := r.Branch
		if len(branch) > 8 {
			branch = branch[:8]
		}
		sb.WriteString(fmt.Sprintf("  %-10s [cyan]%-8s[-]%s [darkgray]%s[-]\n",
			name, branch, dirty, r.LastCommit))
	}

	sb.WriteString("\n[white::b]PENDING TASKS[-]\n")
	if s.tasks.Count == 0 {
		sb.WriteString("  [green]queue empty[-]\n")
	} else {
		sb.WriteString(fmt.Sprintf("  [yellow]%d task(s)[-] queued\n", s.tasks.Count))
		if s.tasks.Oldest != "" {
			sb.WriteString(fmt.Sprintf("  oldest: [darkgray]%s[-]\n", s.tasks.Oldest))
		}
		for i, f := range s.tasks.Files {
			if i >= 3 {
				sb.WriteString(fmt.Sprintf("  [darkgray]… +%d more[-]\n", len(s.tasks.Files)-3))
				break
			}
			if len(f) > 28 {
				f = f[:25] + "..."
			}
			sb.WriteString(fmt.Sprintf("  [darkgray]%s[-]\n", f))
		}
	}

	sb.WriteString("\n[white::b]TOKEN BUDGET[-]\n")
	if s.tokenEst.RunsToday == 0 {
		sb.WriteString("  [darkgray]no runs today[-]\n")
	} else {
		sb.WriteString(fmt.Sprintf("  runs today:  [cyan]%d[-]\n", s.tokenEst.RunsToday))
		sb.WriteString(fmt.Sprintf("  est. tokens: [cyan]~%.0fk[-]\n", s.tokenEst.TodayK))
		sb.WriteString(fmt.Sprintf("  per run avg: [darkgray]~%.1fk[-]\n", s.tokenEst.LastRunK))
	}

	tv.SetText(sb.String())
}

func renderFeedPanel(tv *tview.TextView, s *tuiState) {
	var sb strings.Builder
	if !s.iduna {
		sb.WriteString("[darkgray]  IDUNA offline — Apple feed unavailable\n\n")
		sb.WriteString("  [cyan]emily start --iduna[-]\n")
		tv.SetText(sb.String())
		return
	}
	if len(s.apples) == 0 {
		sb.WriteString("[darkgray]  No Apples yet — press F1 to begin[-]\n")
		tv.SetText(sb.String())
		return
	}
	for _, a := range s.apples {
		ageStr := "?"
		if t, err := time.Parse(time.RFC3339, a.RecordedAt); err == nil {
			ageStr = humanDuration(time.Since(t))
		}
		typeColor := "[cyan]"
		switch {
		case strings.HasPrefix(a.AppleType, "rsi"):
			typeColor = "[green]"
		case strings.HasPrefix(a.AppleType, "prime_task"):
			typeColor = "[yellow]"
		case strings.HasPrefix(a.AppleType, "signal"):
			typeColor = "[blue]"
		case a.AppleType == "completion" || a.AppleType == "backlog_completion":
			typeColor = "[green]"
		}
		repo := a.SourceRepo
		if len(repo) > 8 {
			repo = repo[:8]
		}
		title := a.Title
		if len(title) > 36 {
			title = title[:33] + "..."
		}
		sb.WriteString(fmt.Sprintf("  [darkgray]#%-4d[-] %-8s %s%-14s[-] [white]%s[-] [darkgray]%s[-]\n",
			a.ID, repo, typeColor, a.AppleType, title, ageStr))
	}
	tv.SetText(sb.String())
	tv.ScrollToBeginning()
}

func renderHealthPanel(tv *tview.TextView, s *tuiState) {
	var sb strings.Builder
	sb.WriteString("[white::b]PROCESSES[-]\n")
	for _, p := range s.processes {
		indicator := "[red]● STOP[-]"
		if p.Running {
			indicator = "[green]● RUN [-]"
		}
		name := p.Name
		if len(name) > 14 {
			name = name[:14]
		}
		note := p.Note
		if len(note) > 10 {
			note = note[:10]
		}
		sb.WriteString(fmt.Sprintf("  %-14s %s [darkgray]%s[-]\n", name, indicator, note))
	}

	sb.WriteString("\n[white::b]RSI LOOP[-]\n")
	if !s.rsiLoop.Running {
		sb.WriteString("  [darkgray]not running[-]\n")
		sb.WriteString("  [darkgray]./scripts/rsi-loop.sh[-]\n")
	} else {
		sb.WriteString(fmt.Sprintf("  iter:  [cyan]%d[-]\n", s.rsiLoop.Iteration))
		sb.WriteString(fmt.Sprintf("  last:  [darkgray]%s[-]\n", s.rsiLoop.LastAt))
		sb.WriteString(fmt.Sprintf("  phase: [cyan]%s[-]\n", s.rsiLoop.Phase))
		tid := s.rsiLoop.TaskID
		if len(tid) > 18 {
			tid = tid[:15] + "..."
		}
		sb.WriteString(fmt.Sprintf("  task:  [darkgray]%s[-]\n", tid))
	}

	sb.WriteString("\n[white::b]ACTIONS[-]\n")
	sb.WriteString("  [yellow][F1][-] RSI tic-toc cycle\n")
	sb.WriteString("  [yellow][F2][-] Tyler entropy\n")
	sb.WriteString("  [yellow][F3][-] start system\n")
	sb.WriteString("  [yellow][F4][-] tail rsi-loop log\n")
	sb.WriteString("  [yellow][r] [-] refresh\n")
	sb.WriteString("  [yellow][q] [-] quit\n")

	tv.SetText(sb.String())
}

func renderFooter(tv *tview.TextView, s *tuiState) {
	tv.SetText(fmt.Sprintf(
		"[darkgray] emily tui v0.6.0 | %s | F1=tic-toc  F2=Tyler  F3=start  F4=logs  r=refresh  q=quit[-]",
		s.refreshedAt.Format("15:04:05"),
	))
}

// ── Utilities ─────────────────────────────────────────────────────────────────

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func humanAge(filename string) string {
	parts := strings.SplitN(filename, "-task-", 2)
	if len(parts) == 0 {
		return "?"
	}
	prefix := parts[0]
	for _, f := range []string{"2006-01-02T150405Z", "2006-01-02T15:04:05Z", time.RFC3339} {
		if t, err := time.Parse(f, prefix); err == nil {
			return humanDuration(time.Since(t)) + " ago"
		}
	}
	return "?"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
