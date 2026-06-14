// cmd/tui.go — emily tui (pure stdlib, zero external dependencies)
// Bloomberg-style terminal dashboard using ANSI escape codes.
//
// Layout:
//   Row 0: Header  — system name, timestamp, indicators        (2 lines)
//   Row 1: Main    — repos+tasks | apple feed | health/fatbaby (flexible)
//   Row 2: Log     — RSI activity log                          (9 lines)
//   Row 3: CmdBar  — operator command input                    (1 line)
//   Row 4: Footer  — version + key hints                       (1 line)
//
// Hotkeys:
//   F1 — full RSI tic-toc cycle (TIC→TOCK→ENTROPY→ANALYZE)
//   F2 — Tyler emily.sh 2 iterations
//   F3 — emily start
//   F4 — tail rsi-loop.log (suspends TUI)
//   F5 — fire 1 rsi-loop.sh iteration
//   :  — focus command input bar
//   r  — force refresh all panels
//   q  — quit
//   t  — toggle col2 (apple feed / obs tail)
//   b  — toggle col3 (system health / fatbaby signals)
//   h  — help

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
	"unsafe"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// ── ANSI helpers ──────────────────────────────────────────────────────────────

const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiRed      = "\033[31m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiCyan     = "\033[36m"
	ansiMagenta  = "\033[35m"
	ansiBlue     = "\033[34m"
	ansiWhite    = "\033[97m"
	ansiDarkGray = "\033[90m"

	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
	ansiAltScreen  = "\033[?1049h"
	ansiNormScreen = "\033[?1049l"
	ansiClearFull  = "\033[2J\033[H"
)

func ac(color, s string) string { return color + s + ansiReset }

// ansiVLen returns visible character count, skipping ANSI escape sequences.
func ansiVLen(s string) int {
	n := 0
	inEsc := false
	for _, c := range s {
		switch {
		case c == '\033':
			inEsc = true
		case inEsc && c == 'm':
			inEsc = false
		case !inEsc:
			n++
		}
	}
	return n
}

// ansiTruncPad truncates s to `width` visible chars and pads with spaces.
func ansiTruncPad(s string, width int) string {
	var b strings.Builder
	vis := 0
	inEsc := false
	for _, c := range s {
		if c == '\033' {
			inEsc = true
			b.WriteRune(c)
		} else if inEsc {
			b.WriteRune(c)
			if c == 'm' {
				inEsc = false
			}
		} else if vis < width {
			b.WriteRune(c)
			vis++
		}
	}
	b.WriteString(ansiReset)
	if vis < width {
		b.WriteString(strings.Repeat(" ", width-vis))
	}
	return b.String()
}

// ── Terminal control ──────────────────────────────────────────────────────────

func termSetRaw() {
	cmd := exec.Command("stty", "raw", "-echo")
	cmd.Stdin = os.Stdin
	cmd.Run()
}

func termRestore() {
	cmd := exec.Command("stty", "sane")
	cmd.Stdin = os.Stdin
	cmd.Run()
}

type winsize struct{ Row, Col, Xpixel, Ypixel uint16 }

func termSize() (cols, rows int) {
	var ws winsize
	syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(os.Stdout.Fd()),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)))
	if ws.Col == 0 || ws.Row == 0 {
		return 80, 24
	}
	return int(ws.Col), int(ws.Row)
}

// ── Key input ─────────────────────────────────────────────────────────────────

type keyCode int

const (
	keyNone keyCode = iota
	keyF1
	keyF2
	keyF3
	keyF4
	keyF5
	keyEnter
	keyEscape
	keyBackspace
	keyRune
)

type keyEvent struct {
	code keyCode
	ch   rune
}

// readKeys reads raw key events from r and sends them to ch.
func readKeys(r *bufio.Reader, ch chan<- keyEvent) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			close(ch)
			return
		}
		if b != '\x1b' {
			switch {
			case b == '\r' || b == '\n':
				ch <- keyEvent{code: keyEnter}
			case b == 127 || b == 8:
				ch <- keyEvent{code: keyBackspace}
			case b >= 32:
				ch <- keyEvent{code: keyRune, ch: rune(b)}
			}
			continue
		}
		// ESC — peek for sequences
		next, err := r.ReadByte()
		if err != nil {
			ch <- keyEvent{code: keyEscape}
			return
		}
		if next != '[' && next != 'O' {
			ch <- keyEvent{code: keyEscape}
			r.UnreadByte()
			continue
		}
		if next == 'O' {
			// VT100 F-keys: ESC O P/Q/R/S
			third, _ := r.ReadByte()
			switch third {
			case 'P':
				ch <- keyEvent{code: keyF1}
			case 'Q':
				ch <- keyEvent{code: keyF2}
			case 'R':
				ch <- keyEvent{code: keyF3}
			case 'S':
				ch <- keyEvent{code: keyF4}
			}
			continue
		}
		// CSI: ESC [ ...
		var seq []byte
		for {
			c, e := r.ReadByte()
			if e != nil {
				break
			}
			seq = append(seq, c)
			if c >= 0x40 && c <= 0x7E {
				break
			}
		}
		switch string(seq) {
		case "11~":
			ch <- keyEvent{code: keyF1}
		case "12~":
			ch <- keyEvent{code: keyF2}
		case "13~":
			ch <- keyEvent{code: keyF3}
		case "14~":
			ch <- keyEvent{code: keyF4}
		case "15~":
			ch <- keyEvent{code: keyF5}
		}
	}
}

// ── Logger ────────────────────────────────────────────────────────────────────

type tuiLogger struct {
	mu       sync.Mutex
	lines    []string
	maxLines int
	notify   func()
}

func newTUILogger(notify func()) *tuiLogger {
	return &tuiLogger{maxLines: 500, notify: notify}
}

func (l *tuiLogger) log(color, phaseTag, msg string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("%s%s%s %s[%-4s]%s %s",
		ansiDarkGray, ts, ansiReset,
		color, phaseTag, ansiReset,
		msg)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.maxLines {
		l.lines = l.lines[len(l.lines)-l.maxLines:]
	}
	l.mu.Unlock()
	if l.notify != nil {
		l.notify()
	}
}

func (l *tuiLogger) logf(color, phase, format string, args ...interface{}) {
	l.log(color, phase, fmt.Sprintf(format, args...))
}

func (l *tuiLogger) tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) <= n {
		cp := make([]string, len(l.lines))
		copy(cp, l.lines)
		return cp
	}
	cp := make([]string, n)
	copy(cp, l.lines[len(l.lines)-n:])
	return cp
}

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
	sc := bufio.NewScanner(pr)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			l.log(color, phase, line)
		}
	}
	pr.Close()
	return cmd.Wait()
}

// ── TUI state types ───────────────────────────────────────────────────────────

type tuiState struct {
	repos          []repoStatus
	apples         []iduna.Apple
	processes      []processState
	fatbabySummary fatBabySummary
	tasks          tuiTaskState
	rsiLoop        rsiLoopState
	tokenEst       tokenEstimate
	iduna          bool
	fatbabyMode    bool
	refreshedAt    time.Time
}

type fatBabySummary struct {
	Processes    []processState
	NodeCount    int
	SignalCount  int
	RecentSignal string
	EpsCount     int
}

type signalRecord struct {
	Type       string  `json:"type"`
	Ticker     string  `json:"ticker"`
	Entity     string  `json:"entity"`
	Severity   string  `json:"severity"`
	Score      float64 `json:"score"`
	FilingDate string  `json:"filing_date"`
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
	TodayK           float64
	LastRunK         float64
	RunsToday        int
	HasActual        bool
	EmilyPrimeTodayK float64
	EmilyPrimeCycles int
}

const tuiPIDFile = "/tmp/emily-tui.pid"

// evictStaleTUI kills any previous emily tui instance and resets the terminal.
func evictStaleTUI() {
	self := os.Getpid()
	if raw, err := os.ReadFile(tuiPIDFile); err == nil {
		if old, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && old != self {
			if proc, err := os.FindProcess(old); err == nil {
				proc.Signal(syscall.SIGTERM)
				time.Sleep(100 * time.Millisecond)
				proc.Signal(syscall.SIGKILL)
				fmt.Fprintf(os.Stderr, "emily tui: evicted stale pid %d\n", old)
			}
		}
	}
	os.WriteFile(tuiPIDFile, []byte(strconv.Itoa(self)), 0o644)
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		tty.WriteString("\033[?1049l\033[?25h\033[0m")
		tty.Close()
	}
	exec.Command("stty", "sane").Run()
}

// ── RunTUI ────────────────────────────────────────────────────────────────────

func RunTUI(args []string) int {
	evictStaleTUI()
	defer os.Remove(tuiPIDFile)
	cfg, _ := config.Resolve()

	fatbabyMode := false
	for _, a := range args {
		if a == "--fatbaby" || a == "-fatbaby" {
			fatbabyMode = true
		}
	}

	fmt.Print(ansiAltScreen)
	fmt.Print(ansiHideCursor)
	termSetRaw()
	defer func() {
		termRestore()
		fmt.Print(ansiShowCursor)
		fmt.Print(ansiNormScreen)
		fmt.Print(ansiReset)
	}()

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		tty = os.Stdin
	}

	var stateMu sync.Mutex
	var state tuiState
	col2ShowObs := false
	col3ShowFatBaby := fatbabyMode
	cmdMode := false
	var cmdBuf []rune

	drawCh := make(chan struct{}, 4)
	requestDraw := func() {
		select {
		case drawCh <- struct{}{}:
		default:
		}
	}

	logger := newTUILogger(requestDraw)

	redraw := func() {
		stateMu.Lock()
		s := state
		stateMu.Unlock()
		s.fatbabyMode = col3ShowFatBaby

		cols, rows := termSize()
		if cols < 40 {
			cols = 40
		}

		var out strings.Builder
		out.WriteString(ansiClearFull)
		out.WriteString(ansiHideCursor)

		// Header
		out.WriteString(buildHeader(&s, cols))
		out.WriteString(ac(ansiDarkGray, strings.Repeat("─", cols)) + "\n")

		// Main area
		mainRows := rows - 3 - 9 - 1 - 1 // header(2)+sep(1), log(9), cmd(1), footer(1)
		if mainRows < 3 {
			mainRows = 3
		}
		const leftW = 34
		const rightW = 32
		midW := cols - leftW - rightW - 4
		if midW < 8 {
			midW = 8
		}

		leftLines := buildRepoPanel(&s, leftW, mainRows)
		var midLines []string
		if col2ShowObs {
			midLines = buildObsPanel(cfg.FatBabyRoot, midW, mainRows)
		} else {
			midLines = buildFeedPanel(&s, midW, mainRows)
		}
		var rightLines []string
		if col3ShowFatBaby {
			rightLines = buildFatBabyPanel(&s, rightW, mainRows)
		} else {
			rightLines = buildHealthPanel(&s, rightW, mainRows)
		}

		sep := ac(ansiDarkGray, "│")
		for i := 0; i < mainRows; i++ {
			l := ansiTruncPad(colLine(leftLines, i), leftW)
			m := ansiTruncPad(colLine(midLines, i), midW)
			r := ansiTruncPad(colLine(rightLines, i), rightW)
			out.WriteString(l + " " + sep + " " + m + " " + sep + " " + r + "\n")
		}

		// Log panel
		out.WriteString(ac(ansiDarkGray, "── LOG "+strings.Repeat("─", cols-7)) + "\n")
		logLines := logger.tail(9)
		for i := 0; i < 9; i++ {
			out.WriteString(ansiTruncPad(colLine(logLines, i), cols) + "\n")
		}

		// Command bar
		if cmdMode {
			bar := ac(ansiDarkGray, "CMD: ") + ac(ansiWhite, string(cmdBuf)) + "█"
			out.WriteString(ansiTruncPad(bar, cols) + "\n")
		} else {
			out.WriteString(strings.Repeat(" ", cols) + "\n")
		}

		// Footer
		out.WriteString(buildFooter(&s, col3ShowFatBaby, cols))

		fmt.Print(out.String())
	}

	// Initial collection
	func() {
		s := collectState(cfg)
		s.fatbabyMode = col3ShowFatBaby
		if col3ShowFatBaby {
			s.fatbabySummary = collectFatBabySummary(cfg)
		}
		stateMu.Lock()
		state = s
		stateMu.Unlock()
	}()

	readyMsg := "F1=RSI  F2=Tyler  F3=start  F4=logs  F5=rsi-iter  t=obs  b=fatbaby  :=cmd  r=refresh  q=quit"
	if fatbabyMode {
		readyMsg = "[fatbaby] b=toggle  t=obs  :=cmd  r=refresh  q=quit"
	}
	logger.log(ansiDarkGray, "INFO", readyMsg)
	redraw()

	ticker15s := time.NewTicker(15 * time.Second)
	clockTick := time.NewTicker(time.Second)
	defer ticker15s.Stop()
	defer clockTick.Stop()

	keyCh := make(chan keyEvent, 16)
	go readKeys(bufio.NewReader(tty), keyCh)

	doRefresh := func() {
		go func() {
			s := collectState(cfg)
			if col3ShowFatBaby {
				s.fatbabySummary = collectFatBabySummary(cfg)
			}
			stateMu.Lock()
			state = s
			stateMu.Unlock()
			requestDraw()
		}()
	}

	quit := false
	for !quit {
		select {
		case ke, ok := <-keyCh:
			if !ok {
				quit = true
				break
			}
			if cmdMode {
				switch ke.code {
				case keyEscape:
					cmdMode = false
					cmdBuf = cmdBuf[:0]
				case keyEnter:
					text := strings.TrimSpace(string(cmdBuf))
					cmdMode = false
					cmdBuf = cmdBuf[:0]
					if text != "" {
						go dispatchTUICmd(text, logger, cfg, func() { doRefresh() })
					}
				case keyBackspace:
					if len(cmdBuf) > 0 {
						cmdBuf = cmdBuf[:len(cmdBuf)-1]
					}
				case keyRune:
					cmdBuf = append(cmdBuf, ke.ch)
				}
				redraw()
				continue
			}
			// Normal mode hotkeys
			switch ke.code {
			case keyF1:
				go tuiF1RSI(logger, cfg, func() { doRefresh() })
			case keyF2:
				go tuiF2Tyler(logger, func() { doRefresh() })
			case keyF3:
				go tuiF3Start(logger, func() { doRefresh() })
			case keyF4:
				// Suspend: restore terminal, run tail, re-enter raw
				logPath := "/home/fatbaby/EMILY/var/logs/rsi-loop.log"
				logger.logf(ansiDarkGray, "LOG ", "suspending → %s (ctrl-c to return)", logPath)
				fmt.Print(ansiShowCursor)
				fmt.Print(ansiNormScreen)
				termRestore()
				tail := exec.Command("tail", "-f", logPath)
				tail.Stdin, tail.Stdout, tail.Stderr = os.Stdin, os.Stdout, os.Stderr
				tail.Run()
				fmt.Print(ansiAltScreen)
				fmt.Print(ansiHideCursor)
				termSetRaw()
				logger.log(ansiDarkGray, "LOG ", "returned from tail")
			case keyF5:
				go tuiF5RSIIter(logger, func() { doRefresh() })
			case keyRune:
				switch ke.ch {
				case ':':
					cmdMode = true
				case 'r', 'R':
					logger.log(ansiDarkGray, "INFO", "refreshing...")
					doRefresh()
				case 'q', 'Q':
					quit = true
				case 't', 'T':
					col2ShowObs = !col2ShowObs
					if col2ShowObs {
						logger.log(ansiDarkGray, "INFO", "col2 → OBS TAIL")
					} else {
						logger.log(ansiDarkGray, "INFO", "col2 → APPLE FEED")
					}
				case 'b', 'B':
					col3ShowFatBaby = !col3ShowFatBaby
					if col3ShowFatBaby {
						logger.log(ansiDarkGray, "INFO", "col3 → FATBABY SIGNALS")
						go func() {
							fb := collectFatBabySummary(cfg)
							stateMu.Lock()
							state.fatbabySummary = fb
							stateMu.Unlock()
							requestDraw()
						}()
					} else {
						logger.log(ansiDarkGray, "INFO", "col3 → SYSTEM HEALTH")
					}
				case 'h', 'H':
					logger.log(ansiWhite, "HELP", "F1=RSI  F2=Tyler  F3=start  F4=logs  F5=rsi-iter  t=obs  b=fatbaby  :=cmd  r=refresh  q=quit")
				}
			}
			redraw()

		case <-ticker15s.C:
			doRefresh()

		case <-clockTick.C:
			requestDraw()

		case <-drawCh:
			redraw()
		}
	}

	return 0
}

// colLine returns lines[i] or "" if out of bounds.
func colLine(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// ── Hotkey handlers (run in goroutines) ──────────────────────────────────────

func tuiF1RSI(logger *tuiLogger, cfg *config.Config, refresh func()) {
	claudeRunsDir := filepath.Join(cfg.FatBabyRoot, "claude-runs")

	logger.log(ansiYellow, "TIC", "Dispatching RSI token-efficiency task to Emily Prime...")
	out, err := exec.Command("emily", "prime-task", "--preset", "rsi-token-report").CombinedOutput()
	if err != nil {
		logger.logf(ansiRed, "ERR", "prime-task failed: %s", firstLine(string(out)))
		return
	}
	taskID := parsePrimeTaskID(string(out))
	logger.logf(ansiYellow, "TIC", "task written: %s", taskID)
	logger.log(ansiYellow, "TIC", "obs-watcher picks up within 10s")

	logger.log(ansiCyan, "TOCK", "Polling claude-runs/ for completion (max 3 min)...")
	initialLatest := latestFileInDir(claudeRunsDir)
	waitStart := time.Now()
	for {
		if cur := latestFileInDir(claudeRunsDir); cur != "" && cur != initialLatest {
			logger.logf(ansiCyan, "TOCK", "run detected → %s", cur)
			break
		}
		elapsed := time.Since(waitStart)
		if elapsed > 3*time.Minute {
			logger.log(ansiYellow, "TOCK", "3 min timeout — obs-watcher may not be running")
			break
		}
		time.Sleep(10 * time.Second)
		logger.logf(ansiDarkGray, "TOCK", "...%ds elapsed", int(elapsed.Seconds()))
	}

	tylerSh := "/home/fatbaby/TYLER/emily.sh"
	if _, serr := os.Stat(tylerSh); serr == nil {
		pending := tylerBacklogPending()
		if pending == 0 {
			logger.log(ansiMagenta, "ENTR", "Tyler backlog empty — skipping entropy injection")
		} else {
			logger.logf(ansiMagenta, "ENTR", "Running Tyler emily.sh 2 iterations (%d pending)...", pending)
			cmd := exec.Command("bash", "-c", "cd /home/fatbaby/TYLER && ./emily.sh 2 2>&1")
			if serr := logger.streamCmd(ansiMagenta, "ENTR", cmd); serr != nil {
				logger.logf(ansiYellow, "ENTR", "Tyler returned non-zero (%v) — continuing", serr)
			} else {
				logger.log(ansiMagenta, "ENTR", "Tyler entropy injection complete")
			}
		}
	} else {
		logger.log(ansiDarkGray, "ENTR", "TYLER/emily.sh not found — skipping")
	}

	logger.log(ansiGreen, "ANLZ", "Posting RSI cycle observation...")
	obsMsg := fmt.Sprintf("RSI tic-toc cycle complete — task %s dispatched, Tyler ran 2 builds", taskID)
	obsOut, obsErr := exec.Command("emily", "observe", "-s", "info", obsMsg).CombinedOutput()
	if obsErr != nil {
		logger.logf(ansiYellow, "ANLZ", "observe warn (IDUNA offline?): %s", firstLine(string(obsOut)))
	} else {
		logger.log(ansiGreen, "ANLZ", "observation posted → obs-watcher triggers next cycle")
	}

	logger.log(ansiGreen, " ✓  ", "RSI tic-toc cycle complete — system advancing")
	refresh()
}

func tuiF2Tyler(logger *tuiLogger, refresh func()) {
	tylerSh := "/home/fatbaby/TYLER/emily.sh"
	if _, err := os.Stat(tylerSh); err != nil {
		logger.log(ansiRed, "ERR", "TYLER/emily.sh not found")
		return
	}
	pending := tylerBacklogPending()
	if pending == 0 {
		logger.log(ansiMagenta, "ENTR", "Tyler backlog empty — nothing to build")
		return
	}
	logger.logf(ansiMagenta, "ENTR", "Tyler emily.sh 2 iterations (%d pending)...", pending)
	cmd := exec.Command("bash", "-c", "cd /home/fatbaby/TYLER && ./emily.sh 2 2>&1")
	if err := logger.streamCmd(ansiMagenta, "ENTR", cmd); err != nil {
		logger.logf(ansiYellow, "ENTR", "Tyler: %v", err)
	} else {
		logger.log(ansiGreen, "ENTR", "Tyler RSI loop complete")
	}
	refresh()
}

func tuiF3Start(logger *tuiLogger, refresh func()) {
	logger.log(ansiCyan, "SYS ", "Starting Emily OS stack...")
	if err := logger.streamCmd(ansiCyan, "SYS ", exec.Command("emily", "start")); err != nil {
		logger.logf(ansiYellow, "SYS ", "emily start: %v", err)
	} else {
		logger.log(ansiGreen, "SYS ", "stack started")
	}
	refresh()
}

func tuiF5RSIIter(logger *tuiLogger, refresh func()) {
	rsiSh := "/home/fatbaby/EMILY/scripts/rsi-loop.sh"
	if _, err := os.Stat(rsiSh); err != nil {
		logger.log(ansiRed, "ERR", "EMILY/scripts/rsi-loop.sh not found")
		return
	}
	logger.log(ansiGreen, "RSI ", "Firing rsi-loop.sh (1 iteration)...")
	cmd := exec.Command("bash", "-c", "cd /home/fatbaby/EMILY && MAX_ITERS=1 SKIP_WAIT=1 ./scripts/rsi-loop.sh 2>&1")
	if err := logger.streamCmd(ansiGreen, "RSI ", cmd); err != nil {
		logger.logf(ansiYellow, "RSI ", "rsi-loop: %v", err)
	} else {
		logger.log(ansiGreen, "RSI ", "iteration complete")
	}
	refresh()
}

// ── Panel builders ────────────────────────────────────────────────────────────

func buildHeader(s *tuiState, cols int) string {
	idunaInd := ac(ansiRed, "●")
	obsInd := ac(ansiRed, "●")
	agentInd := ac(ansiRed, "●")
	if s.iduna {
		idunaInd = ac(ansiGreen, "●")
	}
	for _, p := range s.processes {
		switch p.Name {
		case "obs-watcher", "observation-watcher":
			if p.Running {
				obsInd = ac(ansiGreen, "●")
			}
		case "emily-agent":
			if p.Running {
				agentInd = ac(ansiGreen, "●")
			}
		}
	}
	appleCount := 0
	if len(s.apples) > 0 {
		appleCount = int(s.apples[0].ID)
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	title := ansiBold + " EINHORN INDUSTRIAL " + ansiReset + ac(ansiYellow+ansiBold, "◈") + " " + ansiBold + "EMILY OS" + ansiReset
	titleLen := ansiVLen(title)
	pad := cols - titleLen - len(ts) - 1
	if pad < 1 {
		pad = 1
	}
	line1 := title + strings.Repeat(" ", pad) + ac(ansiDarkGray, ts)
	line2 := fmt.Sprintf("  IDUNA:%s  OBS:%s  AGENT:%s  %sApples:%d  Repos:%d%s",
		idunaInd, obsInd, agentInd, ansiDarkGray, appleCount, len(s.repos), ansiReset)
	return line1 + "\n" + line2 + "\n"
}

func buildRepoPanel(s *tuiState, width, height int) []string {
	var lines []string
	lines = append(lines, ansiBold+"REPOS"+ansiReset)
	for _, r := range s.repos {
		dirty := " " + ac(ansiGreen, "✓")
		if r.DirtyCount > 0 {
			dirty = " " + ac(ansiRed, fmt.Sprintf("✗%d", r.DirtyCount))
		}
		name := r.Name
		if len(name) > 10 {
			name = name[:10]
		}
		branch := r.Branch
		if len(branch) > 8 {
			branch = branch[:8]
		}
		lines = append(lines, fmt.Sprintf("  %-10s %s%s%s %s%s%s%s",
			name,
			ansiCyan, branch, ansiReset,
			dirty,
			ansiDarkGray, r.LastCommit, ansiReset))
	}

	lines = append(lines, "")
	lines = append(lines, ansiBold+"TASKS"+ansiReset)
	if s.tasks.Count == 0 {
		lines = append(lines, "  "+ac(ansiGreen, "queue empty"))
	} else {
		lines = append(lines, fmt.Sprintf("  %s%d task(s)%s queued", ansiYellow, s.tasks.Count, ansiReset))
		if s.tasks.Oldest != "" {
			lines = append(lines, fmt.Sprintf("  oldest: %s%s%s", ansiDarkGray, s.tasks.Oldest, ansiReset))
		}
		for i, f := range s.tasks.Files {
			if i >= 3 {
				lines = append(lines, fmt.Sprintf("  %s…+%d more%s", ansiDarkGray, len(s.tasks.Files)-3, ansiReset))
				break
			}
			maxLen := width - 4
			if len(f) > maxLen && maxLen > 3 {
				f = f[:maxLen-3] + "..."
			}
			lines = append(lines, fmt.Sprintf("  %s%s%s", ansiDarkGray, f, ansiReset))
		}
	}

	lines = append(lines, "")
	lines = append(lines, ansiBold+"TOKENS"+ansiReset)
	te := s.tokenEst
	if te.RunsToday == 0 && te.EmilyPrimeCycles == 0 {
		lines = append(lines, "  "+ac(ansiDarkGray, "no runs today"))
	} else {
		if te.RunsToday > 0 {
			q := ac(ansiDarkGray, "(est.)")
			if te.HasActual {
				q = ac(ansiGreen, "(actual)")
			}
			lines = append(lines, fmt.Sprintf("  claude: %s%d runs %.0fk%s %s", ansiCyan, te.RunsToday, te.TodayK, ansiReset, q))
			lines = append(lines, fmt.Sprintf("  last:   %s%.1fk%s", ansiDarkGray, te.LastRunK, ansiReset))
		}
		if te.EmilyPrimeCycles > 0 {
			lines = append(lines, fmt.Sprintf("  prime:  %s%d cycles %.0fk%s %s(IDUNA)%s",
				ansiCyan, te.EmilyPrimeCycles, te.EmilyPrimeTodayK, ansiReset, ansiGreen, ansiReset))
		}
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func buildFeedPanel(s *tuiState, width, height int) []string {
	var lines []string
	if !s.iduna {
		lines = append(lines, ac(ansiDarkGray, "  IDUNA offline"))
		lines = append(lines, "  "+ac(ansiCyan, "emily start --iduna"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	if len(s.apples) == 0 {
		lines = append(lines, ac(ansiDarkGray, "  no apples — press F1 to begin"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	for _, a := range s.apples {
		ageStr := "?"
		if t, err := time.Parse(time.RFC3339, a.RecordedAt); err == nil {
			ageStr = humanDuration(time.Since(t))
		}
		typeColor := ansiCyan
		switch {
		case strings.HasPrefix(a.AppleType, "rsi"):
			typeColor = ansiGreen
		case strings.HasPrefix(a.AppleType, "prime_task"):
			typeColor = ansiYellow
		case strings.HasPrefix(a.AppleType, "signal"):
			typeColor = ansiBlue
		case a.AppleType == "completion" || a.AppleType == "backlog_completion":
			typeColor = ansiGreen
		}
		repo := a.SourceRepo
		if len(repo) > 8 {
			repo = repo[:8]
		}
		title := a.Title
		maxT := width - 42
		if maxT < 5 {
			maxT = 5
		}
		if len(title) > maxT {
			title = title[:maxT-3] + "..."
		}
		lines = append(lines, fmt.Sprintf("  %s#%-4d%s %-8s %s%-14s%s %s %s%s%s",
			ansiDarkGray, a.ID, ansiReset,
			repo,
			typeColor, a.AppleType, ansiReset,
			title,
			ansiDarkGray, ageStr, ansiReset))
		if len(lines) >= height {
			break
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func buildObsPanel(fatbabyRoot string, width, height int) []string {
	var lines []string
	obsDir := filepath.Join(fatbabyRoot, "var", "emily-observations")
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		lines = append(lines, ac(ansiRed, "  obs dir unreadable"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	var latestPath string
	var latestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latestPath = filepath.Join(obsDir, e.Name())
		}
	}
	if latestPath == "" {
		lines = append(lines, ac(ansiDarkGray, "  no observation files"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	data, err := os.ReadFile(latestPath)
	if err != nil {
		lines = append(lines, ac(ansiRed, "  read error"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	lines = append(lines, ac(ansiDarkGray, "  "+filepath.Base(latestPath)))
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		lines = append(lines, "  "+l)
		if len(lines) >= height {
			break
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func buildHealthPanel(s *tuiState, width, height int) []string {
	var lines []string
	lines = append(lines, ansiBold+"PROCESSES"+ansiReset)
	for _, p := range s.processes {
		indicator := ac(ansiRed, "● STOP")
		if p.Running {
			indicator = ac(ansiGreen, "● RUN ")
		}
		name := p.Name
		if len(name) > 14 {
			name = name[:14]
		}
		note := p.Note
		if len(note) > 10 {
			note = note[:10]
		}
		lines = append(lines, fmt.Sprintf("  %-14s %s %s%s%s", name, indicator, ansiDarkGray, note, ansiReset))
	}

	lines = append(lines, "")
	lines = append(lines, ansiBold+"RSI LOOP"+ansiReset)
	if !s.rsiLoop.Running {
		lines = append(lines, "  "+ac(ansiDarkGray, "not running"))
		lines = append(lines, "  "+ac(ansiYellow, "[F5]")+" fire 1 iteration")
	} else {
		lines = append(lines, fmt.Sprintf("  iter:  %s%d%s", ansiCyan, s.rsiLoop.Iteration, ansiReset))
		lines = append(lines, fmt.Sprintf("  last:  %s%s%s", ansiDarkGray, s.rsiLoop.LastAt, ansiReset))
		lines = append(lines, fmt.Sprintf("  phase: %s%s%s", ansiCyan, s.rsiLoop.Phase, ansiReset))
		tid := s.rsiLoop.TaskID
		if len(tid) > 18 {
			tid = tid[:15] + "..."
		}
		lines = append(lines, fmt.Sprintf("  task:  %s%s%s", ansiDarkGray, tid, ansiReset))
	}

	lines = append(lines, "")
	lines = append(lines, ansiBold+"ACTIONS"+ansiReset)
	for _, row := range [][2]string{
		{"[F1]", "RSI tic-toc"},
		{"[F2]", "Tyler entropy"},
		{"[F3]", "start system"},
		{"[F4]", "tail log"},
		{"[F5]", "RSI iteration"},
		{"[:]", "cmd bar"},
		{"[r]", "refresh"},
		{"[q]", "quit"},
	} {
		lines = append(lines, fmt.Sprintf("  %s%s%s %s", ansiYellow, row[0], ansiReset, row[1]))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func buildFatBabyPanel(s *tuiState, width, height int) []string {
	fb := &s.fatbabySummary
	var lines []string
	lines = append(lines, ansiBold+"FATBABY PROCS"+ansiReset)
	for _, p := range fb.Processes {
		indicator := ac(ansiRed, "● STOP")
		if p.Running {
			indicator = ac(ansiGreen, "● RUN ")
		}
		name := p.Name
		if len(name) > 14 {
			name = name[:14]
		}
		lines = append(lines, fmt.Sprintf("  %-14s %s", name, indicator))
	}
	lines = append(lines, "")
	lines = append(lines, ansiBold+"ENTITY GRAPH"+ansiReset)
	lines = append(lines, fmt.Sprintf("  nodes:   %s%d%s", ansiCyan, fb.NodeCount, ansiReset))
	lines = append(lines, fmt.Sprintf("  signals: %s%d%s", ansiCyan, fb.SignalCount, ansiReset))
	if fb.RecentSignal != "" {
		recent := fb.RecentSignal
		if len(recent) > 22 {
			recent = recent[:19] + "..."
		}
		lines = append(lines, fmt.Sprintf("  latest:  %s%s%s", ansiDarkGray, recent, ansiReset))
	}
	lines = append(lines, "")
	lines = append(lines, ansiBold+"EPS"+ansiReset)
	lines = append(lines, fmt.Sprintf("  cases:   %s%d%s", ansiCyan, fb.EpsCount, ansiReset))
	lines = append(lines, "")
	lines = append(lines, ansiBold+"ACTIONS"+ansiReset)
	lines = append(lines, fmt.Sprintf("  %s[b]%s toggle health", ansiYellow, ansiReset))
	lines = append(lines, fmt.Sprintf("  %s[r]%s refresh", ansiYellow, ansiReset))
	lines = append(lines, fmt.Sprintf("  %s[q]%s quit", ansiYellow, ansiReset))
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func buildFooter(s *tuiState, fatbaby bool, cols int) string {
	suffix := ""
	if fatbaby {
		suffix = "  " + ac(ansiYellow, "[FATBABY]")
	}
	ts := s.refreshedAt.Format("15:04:05")
	base := ac(ansiDarkGray, " emily tui v0.9.0 | "+ts+" | F1=tic-toc F2=Tyler F3=start F4=logs b=fatbaby :=cmd r=refresh q=quit")
	return base + suffix
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
		s.iduna = len(apples) > 0
	} else {
		_, err := exec.Command("curl", "-sf", "--max-time", "2",
			cfg.IDUNABaseURL+"/api/v1/apples").Output()
		s.iduna = err == nil
	}
	s.processes = collectProcesses(cfg)
	s.tasks = collectTasks(cfg)
	s.rsiLoop = collectRSIState(cfg)
	s.tokenEst = estimateTokens(cfg)
	if cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		s.tokenEst = mergeIDUNATokens(s.tokenEst, fetchTokenSpendFromIDUNA(client))
	}
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
	type runReport struct {
		TokensUsed int64 `json:"tokens_used"`
	}
	sortedEntries := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			sortedEntries = append(sortedEntries, e)
		}
	}
	sort.Slice(sortedEntries, func(i, j int) bool {
		return sortedEntries[i].Name() > sortedEntries[j].Name()
	})
	var runsToday int
	var todayTokens, lastTokens int64
	hasActual := false
	for _, e := range sortedEntries {
		if !strings.HasPrefix(e.Name(), today) {
			continue
		}
		runsToday++
		data, err := os.ReadFile(filepath.Join(cfg.FatBabyRoot, "claude-runs", e.Name()))
		if err != nil {
			continue
		}
		var r runReport
		if json.Unmarshal(data, &r) == nil && r.TokensUsed > 0 {
			todayTokens += r.TokensUsed
			if !hasActual {
				lastTokens = r.TokensUsed
				hasActual = true
			}
		}
	}
	const estPerRunK = 8.2
	if hasActual {
		return tokenEstimate{
			TodayK: float64(todayTokens) / 1000, LastRunK: float64(lastTokens) / 1000,
			RunsToday: runsToday, HasActual: true,
		}
	}
	return tokenEstimate{TodayK: float64(runsToday) * estPerRunK, LastRunK: estPerRunK, RunsToday: runsToday}
}

func fetchTokenSpendFromIDUNA(client *iduna.Client) tokenEstimate {
	apples, err := client.ListApples(iduna.AppleListFilters{SourceRepo: "emily", Limit: 100})
	if err != nil {
		return tokenEstimate{}
	}
	today := time.Now().UTC().Format("2006-01-02")
	var totalTokens int64
	cycles := 0
	for _, a := range apples {
		if !strings.HasPrefix(a.RecordedAt, today) {
			continue
		}
		full, err := client.GetApple(a.ID)
		if err != nil || full == nil || len(full.Metadata) == 0 {
			continue
		}
		var meta struct {
			TokensUsed int64 `json:"tokens_used"`
		}
		if json.Unmarshal(full.Metadata, &meta) == nil && meta.TokensUsed > 0 {
			totalTokens += meta.TokensUsed
			cycles++
		}
	}
	if cycles == 0 {
		return tokenEstimate{}
	}
	return tokenEstimate{EmilyPrimeTodayK: float64(totalTokens) / 1000, EmilyPrimeCycles: cycles}
}

func mergeIDUNATokens(base, idunaPrime tokenEstimate) tokenEstimate {
	base.EmilyPrimeTodayK = idunaPrime.EmilyPrimeTodayK
	base.EmilyPrimeCycles = idunaPrime.EmilyPrimeCycles
	return base
}

func collectFatBabySummary(cfg *config.Config) fatBabySummary {
	s := fatBabySummary{Processes: collectFatBabyProcesses(cfg)}
	signalsPath := filepath.Join(cfg.FatBabyRoot, "var", "entity-graph", "signals.ndjson")
	if data, err := os.ReadFile(signalsPath); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		s.SignalCount = len(lines)
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) == "" {
				continue
			}
			var sig signalRecord
			if json.Unmarshal([]byte(lines[i]), &sig) == nil {
				s.RecentSignal = fmt.Sprintf("%s %s", sig.Ticker, sig.Type)
			}
			break
		}
	}
	if data, err := os.ReadFile(filepath.Join(cfg.FatBabyRoot, "var", "entity-graph", "nodes.ndjson")); err == nil {
		s.NodeCount = strings.Count(string(data), "\n")
	}
	if entries, err := os.ReadDir(filepath.Join(cfg.FatBabyRoot, "var", "eps")); err == nil {
		s.EpsCount = len(entries)
	}
	return s
}

// ── Command dispatcher ────────────────────────────────────────────────────────

func dispatchTUICmd(input string, logger *tuiLogger, cfg *config.Config, refresh func()) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	verb := parts[0]
	rest := strings.TrimSpace(strings.TrimPrefix(input, verb))

	switch verb {
	case "pt", "prime-task":
		if rest == "" {
			logger.log(ansiYellow, "CMD ", "usage: pt <task description>")
			return
		}
		logger.logf(ansiYellow, "CMD ", "prime-task: %s", rest)
		out, err := exec.Command("emily", "prime-task", rest).CombinedOutput()
		if err != nil {
			logger.logf(ansiRed, "ERR", "prime-task: %s", firstLine(string(out)))
			return
		}
		logger.logf(ansiYellow, "CMD ", "dispatched: %s", parsePrimeTaskID(string(out)))
		refresh()
	case "eo", "observe":
		if rest == "" {
			logger.log(ansiYellow, "CMD ", "usage: eo <observation>")
			return
		}
		logger.logf(ansiGreen, "CMD ", "observe: %s", rest)
		out, err := exec.Command("emily", "observe", "-s", "info", rest).CombinedOutput()
		if err != nil {
			logger.logf(ansiYellow, "CMD ", "observe warn: %s", firstLine(string(out)))
		} else {
			_ = out
			logger.log(ansiGreen, "CMD ", "observation posted")
		}
	case "tyler":
		n := "2"
		if len(parts) > 1 {
			n = parts[1]
		}
		if _, err := os.Stat("/home/fatbaby/TYLER/emily.sh"); err != nil {
			logger.log(ansiRed, "ERR", "TYLER/emily.sh not found")
			return
		}
		logger.logf(ansiMagenta, "CMD ", "Tyler %s iterations...", n)
		cmd := exec.Command("bash", "-c", "cd /home/fatbaby/TYLER && ./emily.sh "+n+" 2>&1")
		if err := logger.streamCmd(ansiMagenta, "ENTR", cmd); err != nil {
			logger.logf(ansiYellow, "CMD ", "Tyler: %v", err)
		} else {
			logger.log(ansiGreen, "CMD ", "Tyler complete")
		}
		refresh()
	case "start":
		logger.log(ansiCyan, "CMD ", "emily start...")
		if err := logger.streamCmd(ansiCyan, "SYS ", exec.Command("emily", "start")); err != nil {
			logger.logf(ansiYellow, "CMD ", "start: %v", err)
		}
		refresh()
	case "refresh", "r":
		logger.log(ansiDarkGray, "CMD ", "refreshing...")
		refresh()
	default:
		logger.logf(ansiYellow, "CMD ", "unknown %q — try: pt, eo, tyler [N], start, refresh", verb)
	}
}

// ── Utilities ─────────────────────────────────────────────────────────────────

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

func latestFileInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var latest os.DirEntry
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if latest == nil || info.ModTime().After(latestTime) {
			latest = e
			latestTime = info.ModTime()
		}
	}
	if latest == nil {
		return ""
	}
	return latest.Name()
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
