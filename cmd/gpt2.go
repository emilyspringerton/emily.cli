// cmd/gpt2.go — emily gpt2
//
// Manages the Emily Prime GPT-2 inference stack:
//   serve.py  (:8088) — Python HF inference server (loads checkpoint once)
//   broker    (:8679) — FatBaby broker proxy (bearer-token auth, routes to :8088)
//
// Usage:
//   emily gpt2 start     [--port N] [--model ft|base] [--dry-run]
//   emily gpt2 proxy     [--port N] [--upstream N] [--routes path] [--dry-run]
//   emily gpt2 status
//   emily gpt2 tokenizer [--dry-run]

package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func RunGPT2(args []string) int {
	if len(args) == 0 {
		printGPT2Usage()
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "start":
		return runGPT2Start(rest)
	case "proxy":
		return runGPT2Proxy(rest)
	case "status":
		return runGPT2Status(rest)
	case "tokenizer":
		return runGPT2Tokenizer(rest)
	case "generate", "gen":
		return runGPT2Generate(rest)
	case "health":
		return runGPT2Health(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily gpt2: unknown subcommand %q\n", sub)
		printGPT2Usage()
		return 1
	}
}

// gpt2Root derives the gpt2-alpine-c repo path from the emily root (sibling dirs).
func gpt2Root(cfg *config.Config) string {
	if v := os.Getenv("GPT2_ROOT"); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(cfg.EmilyRoot), "gpt2-alpine-c")
}

// runGPT2Start launches scripts/serve.py in the background on :8088.
func runGPT2Start(args []string) int {
	fs := flag.NewFlagSet("gpt2 start", flag.ContinueOnError)
	port   := fs.Int("port", 8088, "inference server port")
	model  := fs.String("model", "ft", "model to load: ft (fine-tuned) or base")
	dryRun := fs.Bool("dry-run", false, "print what would be started without starting")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	root    := gpt2Root(cfg)
	logDir  := filepath.Join(cfg.EmilyRoot, "var", "logs")
	script  := filepath.Join(root, "scripts", "serve.py")
	logPath := filepath.Join(logDir, "gpt2-serve.log")
	pat     := fmt.Sprintf("serve.py.*--port.*%d|serve.py.*%d", *port, *port)
	if *port == 8088 {
		// default port — pattern without explicit --port flag
		pat = "gpt2-alpine-c/scripts/serve.py"
	}

	fmt.Printf("\n◈ EMILY OS — GPT-2 START | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  model:  %s\n  port:   :%d\n  script: %s\n\n", *model, *port, script)

	if _, pid, alive := pgrepPat(pat); alive {
		fmt.Printf("  gpt2-serve   already running (pid %d)\n\n", pid)
		return 0
	}

	pyArgs := []string{script, "--model", *model, "--port", fmt.Sprintf("%d", *port)}
	if *dryRun {
		fmt.Printf("  [dry-run] python3 %s\n\n", joinArgs(pyArgs))
		return 0
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", logDir, err)
		return 3
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open log: %v\n", err)
		return 3
	}
	cmd := exec.Command("python3", pyArgs...)
	cmd.Dir = root
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		fmt.Fprintf(os.Stderr, "start serve.py: %v\n", err)
		return 3
	}
	logFile.Close()
	fmt.Printf("  gpt2-serve   started — pid %d → %s\n", cmd.Process.Pid, logPath)
	fmt.Printf("\n  Health check (after ~3s): curl http://localhost:%d/health\n\n", *port)
	return 0
}

// runGPT2Proxy launches the FatBaby broker proxy on :8679, routing to the inference server.
func runGPT2Proxy(args []string) int {
	fs := flag.NewFlagSet("gpt2 proxy", flag.ContinueOnError)
	port       := fs.Int("port", 8679, "broker proxy listen port")
	upstream   := fs.Int("upstream", 8088, "inference server port to proxy to")
	routesFile := fs.String("routes", "", "broker routes JSON (default: gpt2-alpine-c/config/broker-routes.json)")
	storeDir   := fs.String("store", "", "event store dir (default: EMILY/var/broker-events)")
	dryRun     := fs.Bool("dry-run", false, "print what would be started without starting")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	root := gpt2Root(cfg)
	if *routesFile == "" {
		*routesFile = filepath.Join(root, "config", "broker-routes.json")
	}
	if *storeDir == "" {
		*storeDir = filepath.Join(cfg.EmilyRoot, "var", "broker-events")
	}
	logDir  := filepath.Join(cfg.EmilyRoot, "var", "logs")
	logPath := filepath.Join(logDir, "gpt2-proxy.log")

	fmt.Printf("\n◈ EMILY OS — GPT-2 PROXY | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  listen:   :%d\n  upstream: http://localhost:%d\n  routes:   %s\n\n",
		*port, *upstream, *routesFile)

	if _, pid, alive := pgrepPat(fmt.Sprintf("broker.*%d", *port)); alive {
		fmt.Printf("  gpt2-proxy   already running (pid %d)\n\n", pid)
		return 0
	}

	// Patch upstream port in routes if non-default was requested.
	// (The routes.json hardcodes :8088; advanced users can supply --routes.)
	_ = upstream // routes file is the source of truth; --upstream documents intent

	goArgs := []string{
		"run", "./cmd/broker",
		"--routes", *routesFile,
		"--addr", fmt.Sprintf(":%d", *port),
		"--store", *storeDir,
	}

	if *dryRun {
		fmt.Printf("  [dry-run] (cd %s) go %s\n\n", cfg.FatBabyRoot, joinArgs(goArgs))
		return 0
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", logDir, err)
		return 3
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open log: %v\n", err)
		return 3
	}
	cmd := exec.Command("go", goArgs...)
	cmd.Dir = cfg.FatBabyRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		fmt.Fprintf(os.Stderr, "start broker: %v\n", err)
		return 3
	}
	logFile.Close()
	fmt.Printf("  gpt2-proxy   started — pid %d → %s\n", cmd.Process.Pid, logPath)
	fmt.Printf("\n  Usage:\n")
	fmt.Printf("    curl -X POST http://localhost:%d/generate \\\n", *port)
	fmt.Printf("      -H 'Authorization: Bearer emily-gpt2-local' \\\n")
	fmt.Printf("      -d '{\"prompt\": \"Emily Prime:\", \"max_tokens\": 100}'\n\n")
	return 0
}

// runGPT2Status checks whether serve.py and the broker proxy are running.
func runGPT2Status(args []string) int {
	fmt.Printf("\n◈ EMILY OS — GPT-2 STATUS | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	type proc struct {
		name string
		pat  string
		port string
	}
	procs := []proc{
		{"gpt2-serve  (inference)", "gpt2-alpine-c/scripts/serve.py", ":8088"},
		{"gpt2-proxy  (broker)",    "cmd/broker",                     ":8679"},
	}
	for _, p := range procs {
		pat, pid, alive := pgrepPat(p.pat)
		_ = pat
		if alive {
			fmt.Printf("  %-36s running (pid %d)  %s\n", p.name, pid, p.port)
		} else {
			fmt.Printf("  %-36s stopped\n", p.name)
		}
	}
	fmt.Println()
	return 0
}

// runGPT2Tokenizer builds the binary tokenizer required for --prompt mode.
func runGPT2Tokenizer(args []string) int {
	fs := flag.NewFlagSet("gpt2 tokenizer", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print what would run without running")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	root := gpt2Root(cfg)

	fmt.Printf("\n◈ EMILY OS — GPT-2 TOKENIZER | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  repo: %s\n\n", root)

	if *dryRun {
		fmt.Printf("  [dry-run] (cd %s) make tokenizer\n\n", root)
		return 0
	}

	cmd := exec.Command("make", "tokenizer")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "make tokenizer: %v\n", err)
		return 3
	}
	fmt.Printf("\n  ✓ weights/tokenizer.bin ready\n\n")
	return 0
}

// runGPT2Generate calls POST /generate on the inference server and prints the result.
func runGPT2Generate(args []string) int {
	fs := flag.NewFlagSet("gpt2 generate", flag.ContinueOnError)
	prompt  := fs.String("prompt", "Emily Prime:", "prompt text to send")
	maxTok  := fs.Int("max-tokens", 100, "maximum tokens to generate")
	temp    := fs.Float64("temperature", 0.8, "sampling temperature")
	via     := fs.String("via", "server", "endpoint: server (:8088) | emily (:8086 emily-agent) | proxy (:8679)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// remaining positional args are the prompt
	if rest := fs.Args(); len(rest) > 0 {
		joined := rest[0]
		for _, w := range rest[1:] {
			joined += " " + w
		}
		*prompt = joined
	}

	var endpoint string
	var headers map[string]string
	switch *via {
	case "emily":
		endpoint = "http://localhost:8086/api/v1/gpt2/generate"
	case "proxy":
		endpoint = "http://localhost:8679/generate"
		headers = map[string]string{"Authorization": "Bearer emily-gpt2-local"}
	default: // "server"
		endpoint = "http://localhost:8088/generate"
	}

	fmt.Printf("\n◈ EMILY OS — GPT-2 GENERATE | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  endpoint:    %s\n  prompt:      %q\n  max_tokens:  %d\n  temperature: %.2f\n\n",
		endpoint, *prompt, *maxTok, *temp)

	reqBody, _ := json.Marshal(map[string]any{
		"prompt":      *prompt,
		"max_tokens":  *maxTok,
		"temperature": *temp,
	})
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ request failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "  hint: emily gpt2 start\n\n")
		return 1
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "  ✗ server returned %d: %s\n\n", resp.StatusCode, raw)
		return 1
	}

	var out struct {
		Text   string `json:"text"`
		Tokens int    `json:"tokens"`
		Model  string `json:"model"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// print raw if not JSON
		fmt.Printf("  %s\n\n", raw)
		return 0
	}
	if out.Error != "" {
		fmt.Fprintf(os.Stderr, "  ✗ %s\n\n", out.Error)
		return 1
	}
	fmt.Printf("  model:   %s\n  tokens:  %d\n\n", out.Model, out.Tokens)
	fmt.Printf("  ── output ──────────────────────────────────────────────\n")
	fmt.Printf("  %s%s\n", *prompt, out.Text)
	fmt.Printf("  ────────────────────────────────────────────────────────\n\n")
	return 0
}

// runGPT2Health hits the inference server and proxy health endpoints and reports status.
func runGPT2Health(args []string) int {
	fmt.Printf("\n◈ EMILY OS — GPT-2 HEALTH | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	client := &http.Client{Timeout: 5 * time.Second}
	exitCode := 0

	type check struct {
		name string
		url  string
	}
	checks := []check{
		{"gpt2-serve  (:8088)", "http://localhost:8088/health"},
		{"emily-agent (:8086)", "http://localhost:8086/api/v1/gpt2/health"},
	}

	for _, c := range checks {
		resp, err := client.Get(c.url)
		if err != nil {
			fmt.Printf("  %-28s ✗ unreachable — %v\n", c.name, err)
			exitCode = 1
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// extract model field if present
			var body map[string]any
			json.Unmarshal(raw, &body)
			model, _ := body["model"].(string)
			if model != "" {
				fmt.Printf("  %-28s ✓ ok  model=%s\n", c.name, model)
			} else {
				fmt.Printf("  %-28s ✓ ok\n", c.name)
			}
		} else {
			fmt.Printf("  %-28s ✗ %d  %s\n", c.name, resp.StatusCode, raw)
			exitCode = 1
		}
	}

	// broker proxy — just check port reachability
	proxyResp, err := client.Get("http://localhost:8679/health")
	if err != nil {
		fmt.Printf("  %-28s ✗ unreachable (start with: emily gpt2 proxy)\n", "gpt2-proxy  (:8679)")
		exitCode = 1
	} else {
		proxyResp.Body.Close()
		if proxyResp.StatusCode < 500 {
			fmt.Printf("  %-28s ✓ listening\n", "gpt2-proxy  (:8679)")
		} else {
			fmt.Printf("  %-28s ✗ %d\n", "gpt2-proxy  (:8679)", proxyResp.StatusCode)
			exitCode = 1
		}
	}

	fmt.Println()
	return exitCode
}

// pgrepPat returns the pattern, first PID, and whether any match is alive.
func pgrepPat(pattern string) (string, int, bool) {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil || len(out) == 0 {
		return pattern, 0, false
	}
	var pid int
	fmt.Sscanf(string(out), "%d", &pid)
	return pattern, pid, pid > 0
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}

func printGPT2Usage() {
	fmt.Print(`emily gpt2 — manage the Emily Prime GPT-2 inference stack

Subcommands:
  emily gpt2 start     [--port N] [--model ft|base] [--dry-run]
        Start the Python inference server (scripts/serve.py) on :8088.

  emily gpt2 proxy     [--port N] [--routes path] [--dry-run]
        Start the FatBaby broker proxy on :8679 routing to :8088.
        Bearer token: emily-gpt2-local

  emily gpt2 generate  [--prompt "..."] [--max-tokens N] [--temperature F] [--via server|emily|proxy]
        Call the GPT-2 inference server and print the generated text.
        --via server  (default) hits :8088 directly
        --via emily   hits emily-agent :8086/api/v1/gpt2/generate
        --via proxy   hits broker :8679 with bearer token

  emily gpt2 health
        HTTP health check of :8088, :8086/api/v1/gpt2/health, and :8679.

  emily gpt2 status
        Show whether serve.py and the broker are running (pgrep).

  emily gpt2 tokenizer [--dry-run]
        Build weights/tokenizer.bin (required for gpt2_run --prompt mode).

Env:
  GPT2_ROOT   path to gpt2-alpine-c (default: sibling of EMILY_ROOT)

Examples:
  emily gpt2 start                              # inference server on :8088
  emily gpt2 proxy                              # broker proxy on :8679
  emily gpt2 generate --prompt "Emily Prime:"   # hit :8088 directly
  emily gpt2 generate --via emily "What is RSI?" # hit emily-agent proxy
  emily gpt2 health                             # check all endpoints
  emily gpt2 status                             # check processes
`)
}
