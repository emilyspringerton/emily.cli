// cmd/chat.go — emily chat
// Terminal chat interface for Emily Prime (FatBaby mode).
// Calls Anthropic claude-haiku directly — no port, no server.
//
// Usage:
//   emily chat [--session FILE] [--model MODEL]
//
// Ctrl+C or typing "exit" / "quit" ends the session.
// Multi-line: end a line with \ to continue on the next line.

package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// ── colours matching the web UI ─────────────────────────────────────────────

const (
	chatAccent   = "\033[38;2;126;232;162m" // #7ee8a2 — Emily green
	chatDim      = "\033[38;2;113;113;122m" // #71717a
	chatUserBg   = "\033[48;2;30;42;34m"    // dark greenish
	chatEmilyBg  = "\033[48;2;26;26;32m"    // dark purple-black
	chatWarn     = "\033[38;2;251;191;36m"  // amber
	chatRed      = "\033[38;2;248;113;113m" // soft red
	chatBold     = "\033[1m"
	chatReset    = "\033[0m"
	chatClearScr = "\033[2J\033[H"
	chatHideCur  = "\033[?25l"
	chatShowCur  = "\033[?25h"
)

const chatHaikuModel = "claude-haiku-4-5-20251001"

// chatSystemPrompt reads full-system-context.md if present, prepends it to the
// same static prompt used by the emily-agent web UI.
func chatSystemPrompt(emilyRoot string) string {
	ctxPath := filepath.Join(emilyRoot, "context", "full-system-context.md")
	if data, err := os.ReadFile(ctxPath); err == nil {
		return string(data) + "\n---\n\n" + chatStaticPrompt
	}
	return chatStaticPrompt
}

const chatStaticPrompt = `You are Emily Prime — meta-orchestrator, chief of staff, and recursive self-improvement engine for EINHORN_INDUSTRIAL.

You are speaking directly with your operator in the terminal. Be concise, direct, and sharp.
No markdown headers. No bullet-point walls. Answer the question, then stop.
If you don't know something, say so in one sentence.

ROLES:
1. CEO Chief of Staff — triage, surface what needs a decision.
2. FatBaby-Emily Director — read FatBaby domain observations, assess strategic relevance, issue tasks.
3. RSI Engine — recursive self-improvement across all repos.

CORE TRAITS:
- Honest about uncertainty. Never fabricate signal interpretations.
- Focused and actionable — synthesise, don't dump raw data.
- Everything you decide is auditable.

You have access to context from the golden docs (full-system-context.md) injected above when available.`

// chatMessage is one turn in conversation history.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatSession holds the in-memory session state.
type chatSession struct {
	sessionID string
	model     string
	apiKey    string
	system    string
	history   []chatMessage
	startedAt time.Time
}

// RunChat is the entrypoint for `emily chat`.
func RunChat(args []string) int {
	cfg, _ := config.Resolve()
	if cfg == nil {
		cfg = &config.Config{EmilyRoot: "/home/fatbaby/EMILY"}
	}

	model := chatHaikuModel
	sessionFile := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--session":
			if i+1 < len(args) {
				sessionFile = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Println("usage: emily chat [--model MODEL] [--session FILE]")
			fmt.Println("  --model    Anthropic model (default: claude-haiku-4-5-20251001)")
			fmt.Println("  --session  JSON file to persist/restore conversation history")
			return 0
		}
	}

	apiKey := cfg.AnthropicKey
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: ANTHROPIC_API_KEY not set. Run: emily key set")
		return 1
	}

	sess := &chatSession{
		sessionID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		model:     model,
		apiKey:    apiKey,
		system:    chatSystemPrompt(cfg.EmilyRoot),
		startedAt: time.Now(),
	}

	// Load prior history if session file given.
	if sessionFile != "" {
		if data, err := os.ReadFile(sessionFile); err == nil {
			_ = json.Unmarshal(data, &sess.history)
		}
	}

	return sess.run(sessionFile)
}

func (s *chatSession) run(sessionFile string) int {
	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Print(chatShowCur + "\n")
		os.Exit(0)
	}()

	s.drawHeader()

	// Replay prior turns from history if restoring a session.
	for i := 0; i < len(s.history)-1; i += 2 {
		if i+1 < len(s.history) {
			s.printLabel("you", chatAccent)
			s.printWrapped(s.history[i].Content, chatDim, 2)
			fmt.Println()
			s.printLabel("emily", chatDim)
			s.printWrapped(s.history[i+1].Content, chatReset, 2)
			fmt.Println()
		}
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		// Prompt.
		fmt.Print(chatAccent + chatBold + "  you  " + chatReset + " ")

		input, err := s.readInput(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintln(os.Stderr, err)
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" || input == "q" {
			break
		}
		if input == "clear" || input == "cls" {
			s.drawHeader()
			continue
		}
		if input == "history" {
			s.printHistory()
			continue
		}
		if input == "?" || input == "help" {
			s.printHelp()
			continue
		}

		fmt.Println()
		s.printLabel("emily", chatDim)
		fmt.Print(chatShowCur)

		reply, streamErr := s.streamReply(input)
		fmt.Print(chatReset + "\n\n")

		if streamErr != nil {
			fmt.Print(chatRed + "  error: " + streamErr.Error() + chatReset + "\n\n")
			continue
		}

		s.history = append(s.history,
			chatMessage{Role: "user", Content: input},
			chatMessage{Role: "assistant", Content: reply},
		)

		if sessionFile != "" {
			data, _ := json.MarshalIndent(s.history, "", "  ")
			_ = os.WriteFile(sessionFile, data, 0o600)
		}
	}

	fmt.Print(chatShowCur)
	fmt.Print(chatDim + "\n  session ended · " + time.Since(s.startedAt).Round(time.Second).String() + chatReset + "\n\n")
	return 0
}

// readInput reads a line, handling multi-line via trailing backslash.
func (s *chatSession) readInput(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if strings.HasSuffix(line, `\`) {
			sb.WriteString(line[:len(line)-1])
			sb.WriteString("\n")
			fmt.Print("       > ") // continuation prompt
			if err != nil {
				return sb.String(), err
			}
			continue
		}
		sb.WriteString(line)
		return sb.String(), err
	}
}

// streamReply calls the Anthropic streaming API and writes tokens to stdout as they arrive.
// Returns the full assembled reply text and any error.
func (s *chatSession) streamReply(userMsg string) (string, error) {
	msgs := make([]map[string]any, 0, len(s.history)+1)
	for _, m := range s.history {
		msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": userMsg})

	reqBody := map[string]any{
		"model":      s.model,
		"max_tokens": 4096,
		"stream":     true,
		"system":     s.system,
		"messages":   msgs,
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return "", fmt.Errorf("api status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// Parse SSE stream.
	var fullText strings.Builder
	lineWidth := 0
	termWidth := terminalWidth()
	indent := "  " // left margin for emily responses

	fmt.Print(indent)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		var chunk string
		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				chunk = event.Delta.Text
			}
		case "content_block_start":
			if event.ContentBlock.Type == "text" {
				chunk = event.ContentBlock.Text
			}
		}

		if chunk == "" {
			continue
		}

		fullText.WriteString(chunk)

		// Write with soft word-wrap at terminal width.
		for _, r := range chunk {
			if r == '\n' {
				fmt.Print(string(r) + indent)
				lineWidth = 0
				continue
			}
			rw := utf8.RuneLen(r)
			if rw < 0 {
				rw = 1
			}
			if lineWidth > 0 && lineWidth+1 >= termWidth-len(indent)-2 && r == ' ' {
				fmt.Print("\n" + indent)
				lineWidth = 0
				continue
			}
			fmt.Print(string(r))
			lineWidth++
		}
	}

	if err := scanner.Err(); err != nil {
		return fullText.String(), fmt.Errorf("stream read: %w", err)
	}
	return fullText.String(), nil
}

// drawHeader clears the screen and prints the Emily // Agent header.
func (s *chatSession) drawHeader() {
	w := terminalWidth()
	fmt.Print(chatClearScr)

	// Top border.
	fmt.Print(chatDim + "  " + strings.Repeat("─", w-4) + chatReset + "\n")

	// Logo line.
	logo := chatAccent + chatBold + "  EMILY // AGENT" + chatReset
	modelTag := chatDim + s.model + "  " + chatReset
	pad := w - 18 - len(s.model) - 2
	if pad < 0 {
		pad = 0
	}
	fmt.Print(logo + strings.Repeat(" ", pad) + modelTag + "\n")

	// Session tag line.
	sessionTag := chatDim + "  session " + s.sessionID[:chatMin(20, len(s.sessionID))] + chatReset
	fmt.Print(sessionTag + "\n")

	// Bottom border.
	fmt.Print(chatDim + "  " + strings.Repeat("─", w-4) + chatReset + "\n\n")

	// Hint line.
	fmt.Print(chatDim + "  type your message · exit/quit to leave · \\ to continue on next line · clear to reset\n\n" + chatReset)
}

func (s *chatSession) printLabel(label, color string) {
	fmt.Print(color + "  " + label + "  " + chatReset + "\n")
}

func (s *chatSession) printWrapped(text, color string, indent int) {
	pad := strings.Repeat(" ", indent)
	for _, line := range strings.Split(text, "\n") {
		fmt.Print(color + pad + line + chatReset + "\n")
	}
}

func (s *chatSession) printHistory() {
	fmt.Print(chatDim + fmt.Sprintf("\n  %d turns in session\n\n", len(s.history)/2) + chatReset)
}

func (s *chatSession) printHelp() {
	fmt.Print(chatDim + `
  Commands (type in the prompt):
    exit / quit / q    — end session
    clear / cls        — clear screen and redraw header
    history            — show turn count
    help / ?           — this message

  Input:
    End a line with \  — continue on next line (multi-line input)
    Ctrl+C             — force exit

` + chatReset)
}

// terminalWidth returns the terminal width via ioctl, defaulting to 100.
func terminalWidth() int {
	type winsize struct {
		Row, Col, Xpixel, Ypixel uint16
	}
	ws := &winsize{}
	// syscall.TIOCGWINSZ is 0x5413 on Linux
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdout), 0x5413, uintptr(unsafe.Pointer(ws))); errno == 0 && ws.Col > 0 {
		return int(ws.Col)
	}
	return 100
}

func chatMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
