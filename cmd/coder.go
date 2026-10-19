// cmd/coder.go — emily coder work|lesson|tools
//
// `emily coder work` is the coder agent's worker: it claims chat turns from IDUNA
// (/api/v1/coder/work/claim), runs a tool-calling loop against the vLLM coder engine
// (OpenAI-compatible API), and reports every tool call and the reply back, so the conversation
// shows up live in okemily.com/admin/coder. The agent is powered by emily.cli itself: its tools
// are emily subcommands plus grep/sed/go/git/files in a confined workdir, each gated by the
// coder.tool.<name> permission of the human who sent the turn.
//
//	emily coder work [-engine URL] [-dir WORKDIR] [-verify CMD] [-once]
//	emily coder lesson add "text" | list | rm <id>      (the feedback loop)
//	emily coder worker create                            (mint a least-privilege worker agent; secret shown once)
//	emily coder tools                                    (tools + required permissions)
package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const coderBasePrompt = `You are coder-agent, a worker in the EINHORN_INDUSTRIAL monorepo, powered by emily.cli (your tools are emily subcommands plus grep, sed, go, git, files).
You live in okemily.com/admin/coder and talk with the founder and reviewers. Be brief and concrete.
Rules: follow the instruction files below; search with grep before editing; edit with sed -i or files write; run go test before claiming code works; never push; report failures as failures; do not claim an action you did not see succeed in a tool result. Call finish(summary) when done.`

type coderAgent struct {
	idc    *iduna.Client
	engine string
	model  string
	dir    string
	verify string
	tag    string
}

type coderWork struct {
	ID          int64    `json:"id"`
	SessionID   string   `json:"session_id"`
	Content     string   `json:"content"`
	SenderPerms []string `json:"sender_perms"`
	History     []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"history"`
	Lessons []struct {
		Text string `json:"text"`
	} `json:"lessons"`
}

// toolAllowed mirrors IDUNA's rule: held permission, or the coder.admin / iduna.admin supersets.
func toolAllowed(perms []string, tool string) bool {
	for _, p := range perms {
		if p == "coder.tool."+tool || p == "coder.admin" || p == "iduna.admin" {
			return true
		}
	}
	return false
}

func RunCoder(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily coder <work|lesson|tools>")
		return 1
	}
	switch args[0] {
	case "work":
		return runCoderWork(args[1:])
	case "worker":
		return runCoderWorkerCreate(args[1:])
	case "lesson":
		return runCoderLesson(args[1:])
	case "tools":
		for _, t := range coderToolSpecs {
			fmt.Printf("%-13s coder.tool.%-13s %s\n", t.Name, t.Name, t.Desc)
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown subcommand %q — use work, lesson or tools\n", args[0])
	return 1
}

func coderClient() (*iduna.Client, int) {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return nil, 1
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set")
		return nil, 2
	}
	return iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret), 0
}

func runCoderLesson(args []string) int {
	c, rc := coderClient()
	if c == nil {
		return rc
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "add":
		raw, _, err := c.CoderDo("POST", "/lessons", map[string]string{"text": strings.Join(args[1:], " ")})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(string(raw))
	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: emily coder lesson rm <id>")
			return 1
		}
		if _, _, err := c.CoderDo("DELETE", "/lessons/"+args[1], nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	case "list":
		raw, _, err := c.CoderDo("GET", "/lessons", nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		var d struct {
			Lessons []struct {
				ID     int64
				Text   string
				Author string
			}
		}
		json.Unmarshal(raw, &d)
		for _, l := range d.Lessons {
			fmt.Printf("%d\t%s\t%s\n", l.ID, l.Author, l.Text)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: emily coder lesson add|list|rm")
		return 1
	}
	return 0
}

func runCoderWork(args []string) int {
	fs := flag.NewFlagSet("coder work", flag.ContinueOnError)
	engine := fs.String("engine", envOr("CODER_ENGINE_URL", "http://localhost:18000/v1"), "OpenAI-compatible engine base URL (kubectl port-forward -n emily svc/coder-engine-service 18000:8000)")
	model := fs.String("model", "Qwen/Qwen2.5-Coder-14B-Instruct", "model")
	dir := fs.String("dir", ".", "workdir the tools are confined to")
	verify := fs.String("verify", "", "go-style command (e.g. 'go test ./...') that must pass before finish is accepted")
	once := fs.Bool("once", false, "handle at most one turn then exit")
	poll := fs.Duration("poll", 3*time.Second, "queue poll interval")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	c, rc := coderClient()
	if c == nil {
		return rc
	}
	abs, _ := filepath.Abs(*dir)
	tag := "coder-agent"
	if cfg, err := config.Resolve(); err == nil {
		if t := currentSessionTag(cfg.EmilyRoot); t != "" {
			tag = t
		}
	}
	a := &coderAgent{tag: tag, idc: c, engine: strings.TrimRight(*engine, "/"), model: *model, dir: abs, verify: *verify}
	fmt.Printf("coder worker as %s, workdir %s, engine %s\n", c.AgentName, abs, a.engine)
	for {
		raw, code, err := c.CoderDo("POST", "/work/claim", map[string]string{})
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "claim:", err)
			if *once {
				return 2
			}
		case code == http.StatusNoContent || len(raw) == 0:
			if *once {
				fmt.Println("queue empty")
				return 0
			}
		default:
			var w coderWork
			if json.Unmarshal(raw, &w) == nil && w.ID != 0 {
				ok := a.handle(w)
				c.CoderDo("POST", "/work/"+strconv.FormatInt(w.ID, 10)+"/finish", map[string]bool{"ok": ok})
				if *once {
					return 0
				}
				continue
			}
		}
		time.Sleep(*poll)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (a *coderAgent) report(w coderWork, role, tool, content string) {
	_, _, err := a.idc.CoderDo("POST", "/work/"+strconv.FormatInt(w.ID, 10)+"/events",
		map[string]string{"role": role, "tool": tool, "content": clipStr(content, 6000)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
	}
}

func (a *coderAgent) systemPrompt(w coderWork) string {
	s := coderBasePrompt + "\nWorkdir: " + a.dir + "\nSession tag: " + a.tag
	var allowed []string
	for _, t := range coderToolSpecs {
		if toolAllowed(w.SenderPerms, t.Name) {
			allowed = append(allowed, t.Name)
		}
	}
	s += "\nTools you may use for this user: " + strings.Join(allowed, ", ")
	if len(w.Lessons) > 0 {
		s += "\n\nLESSONS from reviewers (obey):"
		for _, l := range w.Lessons {
			s += "\n- " + l.Text
		}
	}
	return clipStr(s, 3500) + "\n\n" + claudeFilesPrompt(a.dir, 5000)
}

func openAITools(perms []string) []map[string]any {
	str := map[string]any{"type": "string"}
	var out []map[string]any
	for _, t := range coderToolSpecs {
		if !toolAllowed(perms, t.Name) {
			continue
		}
		props, req := map[string]any{"args": str}, []string{"args"}
		switch t.Name {
		case "files":
			props, req = map[string]any{"op": str, "path": str, "content": str}, []string{"op", "path"}
		case "claude_files":
			props, req = map[string]any{"section": str}, nil
		case "finish":
			props, req = map[string]any{"summary": str}, []string{"summary"}
		}
		params := map[string]any{"type": "object", "properties": props}
		if req != nil {
			params["required"] = req
		}
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Desc, "parameters": params}})
	}
	return out
}

func (a *coderAgent) chat(msgs []map[string]any, tools []map[string]any) (map[string]any, error) {
	body := map[string]any{"model": a.model, "messages": msgs, "max_tokens": 1000, "temperature": 0.2}
	if len(tools) > 0 {
		body["tools"], body["tool_choice"] = tools, "auto"
	}
	b, _ := json.Marshal(body)
	cl := &http.Client{Timeout: 5 * time.Minute}
	r, err := cl.Post(a.engine+"/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	var out struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return nil, fmt.Errorf("engine %d: %s", r.StatusCode, clipStr(string(raw), 400))
	}
	return out.Choices[0].Message, nil
}

// trimMsgs keeps system + the newest messages inside the engine's 8k-token window (~3 chars/token).
func trimMsgs(m []map[string]any, budgetChars int) []map[string]any {
	size := func(x []map[string]any) int { b, _ := json.Marshal(x); return len(b) }
	for size(m) > budgetChars && len(m) > 3 {
		m = append(m[:1:1], m[2:]...)
		for len(m) > 2 && m[1]["role"] == "tool" { // never start on an orphaned tool result
			m = append(m[:1:1], m[2:]...)
		}
	}
	return m
}

func (a *coderAgent) handle(w coderWork) bool {
	fmt.Printf("turn %d (%s): %s\n", w.ID, w.SessionID, clipStr(w.Content, 120))
	msgs := []map[string]any{{"role": "system", "content": a.systemPrompt(w)}}
	for _, h := range w.History {
		msgs = append(msgs, map[string]any{"role": h.Role, "content": h.Content})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": w.Content})
	tools := openAITools(w.SenderPerms)

	for i := 0; i < 20; i++ {
		m, err := a.chat(trimMsgs(msgs, 17000), tools)
		if err != nil {
			a.report(w, "assistant", "", "(engine error: "+err.Error()+")")
			return false
		}
		tcs, _ := m["tool_calls"].([]any)
		text, _ := m["content"].(string)
		if len(tcs) == 0 {
			known := map[string]bool{}
			for _, t := range coderToolSpecs {
				known[t.Name] = true
			}
			if tcs = extractToolCalls(text, known); len(tcs) > 0 {
				m["tool_calls"], m["content"] = tcs, ""
			}
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "content": m["content"], "tool_calls": m["tool_calls"]})
		if len(tcs) == 0 {
			a.report(w, "assistant", "", text)
			return true
		}
		for _, t := range tcs {
			tc, _ := t.(map[string]any)
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argStr, _ := fn["arguments"].(string)
			var args map[string]any
			if json.Unmarshal([]byte(argStr), &args) != nil {
				args = map[string]any{}
			}
			var res string
			var isErr bool
			if !toolAllowed(w.SenderPerms, name) {
				res, isErr = "denied: you were not granted coder.tool."+name, true
			} else {
				res, isErr = a.execTool(name, args)
				if name == "finish" && a.verify != "" {
					if fields, _ := splitWords(a.verify); len(fields) > 0 {
						if out, err := runExec(a.dir, 10*time.Minute, fields[0], fields[1:]...); err != nil {
							res, isErr = "REFUSED: verify failed ("+a.verify+"). Fix it first.\n"+clipStr(out, 2000), true
						}
					}
				}
				a.report(w, "tool", name, fmt.Sprintf("%s %s\n→ %s", name, clipStr(argStr, 300), res))
			}
			fmt.Printf("  [%d] %s err=%v\n", i, name, isErr)
			id, _ := tc["id"].(string)
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": id, "content": res})
			if name == "finish" && !isErr {
				a.report(w, "assistant", "", strings.TrimPrefix(res, "DONE: "))
				return true
			}
		}
	}
	a.report(w, "assistant", "", "(stopped: iteration limit reached without finish)")
	return false
}

// runCoderWorkerCreate: `emily coder worker create` mints the identity `emily coder work` runs as.
// It holds exactly coder.worker.claim + coder.worker.report; the caller needs coder.worker.provision.
func runCoderWorkerCreate(args []string) int {
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(os.Stderr, "usage: emily coder worker create")
		return 1
	}
	c, rc := coderClient()
	if c == nil {
		return rc
	}
	raw, _, err := c.CoderDo("POST", "/workers", map[string]string{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var d struct {
		AgentName   string   `json:"agent_name"`
		AgentSecret string   `json:"agent_secret"`
		Permissions []string `json:"permissions"`
	}
	if json.Unmarshal(raw, &d) != nil || d.AgentSecret == "" {
		fmt.Fprintln(os.Stderr, "unexpected response:", string(raw))
		return 2
	}
	fmt.Printf("IDUNA_AGENT_NAME=%s\nIDUNA_AGENT_SECRET=%s\n# permissions: %s\n# shown once; run the worker with these exported: emily coder work\n",
		d.AgentName, d.AgentSecret, strings.Join(d.Permissions, ", "))
	return 0
}
