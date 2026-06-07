// cmd/tui.go — emily tui
// Bloomberg-style terminal dashboard for the Einhorn Industrial agent stack.
//
// Layout (three-column grid):
//   Header:  system name, timestamp, process indicators
//   Left:    repo status, pending tasks, token budget
//   Center:  live Apple feed (auto-refreshing)
//   Right:   process health, RSI loop state, action hotkeys
//   Footer:  last refresh time + key hints
//
// Hotkeys: F1=fire RSI task, F2=run Tyler, F3=start system, r=refresh, q=quit

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rivo/tview"
	"github.com/gdamore/tcell/v2"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

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
	Count   int
	Oldest  string // human age of oldest task
	Files   []string
}

type rsiLoopState struct {
	Running    bool
	Iteration  int
	LastAt     string
	TaskID     string
	Phase      string
}

type tokenEstimate struct {
	TodayK   float64
	LastRunK float64
	RunsToday int
}

// RunTUI launches the Bloomberg-style terminal dashboard.
func RunTUI(args []string) int {
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

	footer := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	// Panel borders
	for _, tv := range []*tview.TextView{repoPanel, feedPanel, healthPanel} {
		tv.SetBorder(true)
	}
	repoPanel.SetTitle("[ REPOS + TASKS ]").SetTitleAlign(tview.AlignLeft)
	feedPanel.SetTitle("[ APPLE FEED (live) ]").SetTitleAlign(tview.AlignLeft)
	healthPanel.SetTitle("[ SYSTEM HEALTH ]").SetTitleAlign(tview.AlignLeft)

	// ── Layout ───────────────────────────────────────────────────────────────
	// Grid: header row (2 lines), main three columns, footer row (1 line)
	grid := tview.NewGrid().
		SetRows(3, 0, 1).
		SetColumns(34, 0, 32).
		SetBorders(false)

	grid.AddItem(header, 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(repoPanel, 1, 0, 1, 1, 0, 0, false)
	grid.AddItem(feedPanel, 1, 1, 1, 1, 0, 0, false)
	grid.AddItem(healthPanel, 1, 2, 1, 1, 0, 0, false)
	grid.AddItem(footer, 2, 0, 1, 3, 0, 0, false)

	// ── State + render ───────────────────────────────────────────────────────
	var state tuiState
	var feedHighWater int64

	refresh := func() {
		state = collectState(cfg)
		// Accumulate apple feed — only append new entries
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

	// Initial render (synchronous so screen isn't blank)
	state = collectState(cfg)
	renderHeader(header, &state)
	renderRepoPanel(repoPanel, &state)
	renderFeedPanel(feedPanel, &state)
	renderHealthPanel(healthPanel, &state)
	renderFooter(footer, &state)

	// Background refresh ticker (every 15s)
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			refresh()
		}
	}()
	defer ticker.Stop()

	// ── Keyboard ──────────────────────────────────────────────────────────────
	statusMsg := ""
	showStatus := func(msg string) {
		statusMsg = msg
		app.QueueUpdateDraw(func() { renderFooter(footer, &state) })
		time.AfterFunc(4*time.Second, func() {
			statusMsg = ""
			app.QueueUpdateDraw(func() { renderFooter(footer, &state) })
		})
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF1:
			go func() {
				showStatus("Firing RSI token-efficiency task...")
				out, err := exec.Command("emily", "prime-task", "--preset", "rsi-token-report").CombinedOutput()
				if err != nil {
					showStatus(fmt.Sprintf("F1 error: %s", strings.TrimSpace(string(out))))
				} else {
					showStatus("RSI task dispatched → obs-watcher picks up in 10s")
				}
				refresh()
			}()
			return nil
		case tcell.KeyF2:
			go func() {
				showStatus("Starting Tyler RSI loop (2 iterations)...")
				cmd := exec.Command("bash", "-c",
					"cd /home/fatbaby/TYLER && ./emily.sh 2 >/tmp/tyler-entropy.log 2>&1")
				if err := cmd.Start(); err != nil {
					showStatus(fmt.Sprintf("F2 error: %v", err))
				} else {
					showStatus(fmt.Sprintf("Tyler started (pid %d) → /tmp/tyler-entropy.log", cmd.Process.Pid))
				}
			}()
			return nil
		case tcell.KeyF3:
			go func() {
				showStatus("Starting Emily OS stack...")
				out, err := exec.Command("emily", "start").CombinedOutput()
				if err != nil {
					showStatus(fmt.Sprintf("start error: %s", firstLine(string(out))))
				} else {
					showStatus("System started — obs-watcher + emily-agent running")
					refresh()
				}
			}()
			return nil
		case tcell.KeyF4:
			go func() {
				showStatus("Tailing EMILY/var/logs/rsi-loop.log (Ctrl-C to stop)...")
				app.Suspend(func() {
					exec.Command("tail", "-f",
						"/home/fatbaby/EMILY/var/logs/rsi-loop.log",
					).Run()
				})
			}()
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case 'r', 'R':
				go func() {
					showStatus("Refreshing...")
					refresh()
					showStatus("Refreshed")
				}()
				return nil
			case 'q', 'Q':
				app.Stop()
				return nil
			case 'h', 'H':
				showStatus("F1=fire RSI  F2=tyler  F3=start system  F4=tail logs  r=refresh  q=quit")
				return nil
			}
		}
		return event
	})

	_ = statusMsg // used via closure
	getStatus := func() string { return statusMsg }
	renderFooterFn := func() {
		app.QueueUpdateDraw(func() {
			tv := footer
			s := getStatus()
			if s != "" {
				tv.SetText(fmt.Sprintf("[yellow] ◈ %s", s))
			} else {
				renderFooter(tv, &state)
			}
		})
	}
	_ = renderFooterFn

	if err := app.SetRoot(grid, true).EnableMouse(false).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}

// ── Data collection ───────────────────────────────────────────────────────────

func collectState(cfg *config.Config) tuiState {
	s := tuiState{refreshedAt: time.Now()}

	// Repos
	for _, r := range repoDefs {
		s.repos = append(s.repos, gitRepoStatus(r.Name, r.Path))
	}

	// IDUNA + Apples
	if cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		apples, _ := client.ListApples(iduna.AppleListFilters{Limit: 30})
		s.apples = apples
		if len(apples) > 0 {
			s.iduna = true
		}
	} else {
		// Try anonymous ping
		_, err := exec.Command("curl", "-sf", "--max-time", "2",
			cfg.IDUNABaseURL+"/api/v1/apples").Output()
		s.iduna = err == nil
	}

	// Processes (reuse status.go's collectProcesses)
	s.processes = collectProcesses(cfg)

	// Pending tasks in EMILY/signals/tasks/
	s.tasks = collectTasks(cfg)

	// RSI loop state
	s.rsiLoop = collectRSIState(cfg)

	// Token estimate from claude-runs/
	s.tokenEst = estimateTokens(cfg)

	return s
}

func gitRepoStatus(name, path string) repoStatus {
	rs := repoStatus{Name: name}

	// Branch
	if b, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		rs.Branch = strings.TrimSpace(string(b))
	}

	// Last commit (7-char hash)
	if h, err := exec.Command("git", "-C", path, "log", "-1", "--format=%h %s").Output(); err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(h)), " ", 2)
		if len(parts) > 0 {
			rs.LastCommit = parts[0]
		}
	}

	// Dirty count
	if st, err := exec.Command("git", "-C", path, "status", "--porcelain").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(st)), "\n")
		count := 0
		for _, l := range lines {
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
	if len(files) > 0 {
		oldest := files[0]
		// Parse timestamp from filename prefix (RFC3339 with colons removed)
		// Format: 2026-06-07T091421Z-task-...json
		if len(oldest) >= 18 {
			ts.Oldest = humanAge(oldest)
		} else {
			ts.Oldest = "?"
		}
	}
	return ts
}

func collectRSIState(cfg *config.Config) rsiLoopState {
	stateFile := filepath.Join(cfg.EmilyRoot, "var", "rsi-loop-state.json")
	b, err := os.ReadFile(stateFile)
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
	return rsiLoopState{
		Running:   true,
		Iteration: raw.Iteration,
		LastAt:    ts,
		TaskID:    raw.TaskID,
		Phase:     raw.Phase,
	}
}

func estimateTokens(cfg *config.Config) tokenEstimate {
	runsDir := filepath.Join(cfg.FatBabyRoot, "claude-runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return tokenEstimate{}
	}
	today := time.Now().UTC().Format("2006-06-07")
	runsToday := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), today) {
			runsToday++
		}
	}
	// Rough estimate: ~8k tokens per Claude Code run on average
	return tokenEstimate{
		TodayK:    float64(runsToday) * 8.2,
		LastRunK:  8.2,
		RunsToday: runsToday,
	}
}

// ── Renderers ─────────────────────────────────────────────────────────────────

func renderHeader(tv *tview.TextView, s *tuiState) {
	idunaColor := "[red]●[-]"
	if s.iduna {
		idunaColor = "[green]●[-]"
	}

	obsColor := "[red]●[-]"
	agentColor := "[red]●[-]"
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

	line1 := fmt.Sprintf(
		"[white::b] EINHORN INDUSTRIAL [-] [yellow::b]◈[-] [white::b]EMILY OS[-]"+
			"%s"+
			"  [darkgray]%s[-]",
		strings.Repeat(" ", 20),
		s.refreshedAt.Format("2006-01-02 15:04:05"),
	)
	line2 := fmt.Sprintf(
		"  IDUNA:%s  OBS:%s  EMILY-AGENT:%s  "+
			"[darkgray]Apples:%d  Repos:%d[-]",
		idunaColor, obsColor, agentColor,
		appleCount, len(s.repos),
	)
	tv.SetText(line1 + "\n" + line2)
}

func renderRepoPanel(tv *tview.TextView, s *tuiState) {
	var sb strings.Builder

	sb.WriteString("[white::b]REPOS[-]\n")
	for _, r := range s.repos {
		dirty := ""
		if r.DirtyCount > 0 {
			dirty = fmt.Sprintf(" [red]✗ %d[-]", r.DirtyCount)
		} else {
			dirty = " [green]✓[-]"
		}
		branch := r.Branch
		if len(branch) > 8 {
			branch = branch[:8]
		}
		name := r.Name
		if len(name) > 10 {
			name = name[:10]
		}
		sb.WriteString(fmt.Sprintf("  %-10s [cyan]%-8s[-]%s [darkgray]%s[-]\n",
			name, branch, dirty, r.LastCommit))
	}

	sb.WriteString("\n[white::b]PENDING TASKS[-]\n")
	if s.tasks.Count == 0 {
		sb.WriteString("  [green]queue empty[-]\n")
	} else {
		sb.WriteString(fmt.Sprintf("  [yellow]%d task(s)[-] in queue\n", s.tasks.Count))
		if s.tasks.Oldest != "" {
			sb.WriteString(fmt.Sprintf("  oldest: [darkgray]%s[-]\n", s.tasks.Oldest))
		}
		for i, f := range s.tasks.Files {
			if i >= 3 {
				sb.WriteString(fmt.Sprintf("  [darkgray]… +%d more[-]\n", len(s.tasks.Files)-3))
				break
			}
			short := f
			if len(short) > 28 {
				short = short[:25] + "..."
			}
			sb.WriteString(fmt.Sprintf("  [darkgray]%s[-]\n", short))
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
		sb.WriteString("  Start IDUNA:\n")
		sb.WriteString("  [cyan]emily start --iduna[-]\n")
		tv.SetText(sb.String())
		return
	}

	if len(s.apples) == 0 {
		sb.WriteString("[darkgray]  No Apples yet — fire an RSI task to begin[-]\n")
		tv.SetText(sb.String())
		return
	}

	for _, a := range s.apples {
		var ageStr string
		if t, err := time.Parse(time.RFC3339, a.RecordedAt); err == nil {
			ageStr = humanDuration(time.Since(t))
		} else {
			ageStr = "?"
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
		sb.WriteString(fmt.Sprintf(
			"  [darkgray]#%-4d[-] %-8s %s%-14s[-] [white]%s[-] [darkgray]%s[-]\n",
			a.ID, repo, typeColor, a.AppleType, title, ageStr,
		))
	}

	tv.SetText(sb.String())
	// Scroll to top (newest)
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
		sb.WriteString("  [darkgray]start: ./scripts/rsi-loop.sh[-]\n")
	} else {
		sb.WriteString(fmt.Sprintf("  iter: [cyan]%d[-]\n", s.rsiLoop.Iteration))
		sb.WriteString(fmt.Sprintf("  last: [darkgray]%s[-]\n", s.rsiLoop.LastAt))
		sb.WriteString(fmt.Sprintf("  phase:[cyan]%s[-]\n", s.rsiLoop.Phase))
		tid := s.rsiLoop.TaskID
		if len(tid) > 18 {
			tid = tid[:15] + "..."
		}
		sb.WriteString(fmt.Sprintf("  task: [darkgray]%s[-]\n", tid))
	}

	sb.WriteString("\n[white::b]ACTIONS[-]\n")
	sb.WriteString("  [yellow][F1][-] fire RSI task\n")
	sb.WriteString("  [yellow][F2][-] run tyler (2 iters)\n")
	sb.WriteString("  [yellow][F3][-] start system\n")
	sb.WriteString("  [yellow][F4][-] tail rsi-loop log\n")
	sb.WriteString("  [yellow][r] [-] refresh now\n")
	sb.WriteString("  [yellow][q] [-] quit\n")
	sb.WriteString("  [yellow][h] [-] help\n")

	tv.SetText(sb.String())
}

func renderFooter(tv *tview.TextView, s *tuiState) {
	tv.SetText(fmt.Sprintf(
		"[darkgray] emily tui v0.6.0 | refreshed %s | F1=RSI  F2=Tyler  F3=start  r=refresh  q=quit[-]",
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
	// Filenames are timestamp-prefixed: 2026-06-07T091421Z-task-...
	// Try to parse the prefix as a time
	parts := strings.SplitN(filename, "-task-", 2)
	if len(parts) == 0 {
		return "?"
	}
	prefix := parts[0] // e.g. "2026-06-07T091421Z"
	for _, f := range []string{"2006-01-02T150405Z", "2006-01-02T15:04:05Z", time.RFC3339} {
		if t, err := time.Parse(f, prefix); err == nil {
			return humanDuration(time.Since(t)) + " ago"
		}
	}
	return "?"
}

func firstLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return s
	}
	return lines[0]
}

