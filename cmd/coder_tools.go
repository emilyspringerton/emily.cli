// cmd/coder_tools.go — the coder agent's tools. Every tool is a direct exec (never a shell), so
// arguments can't be chained or substituted; paths are confined to the workdir; each tool is gated
// by the permission coder.tool.<name> of the human who sent the turn (and re-checked by IDUNA).
package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// splitWords splits like a POSIX shell would for quoting only (no expansion, no operators).
func splitWords(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	has := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

// confineArgs rejects absolute paths and parent traversal in any argument.
func confineArgs(args []string) error {
	for _, a := range args {
		if filepath.IsAbs(a) || a == ".." || strings.HasPrefix(a, "../") || strings.Contains(a, "/../") || strings.HasSuffix(a, "/..") {
			return fmt.Errorf("argument %q leaves the workdir", a)
		}
	}
	return nil
}

// confinePath resolves rel inside dir or fails.
func confinePath(dir, rel string) (string, error) {
	p := filepath.Join(dir, filepath.Clean("/"+rel))
	if p != dir && !strings.HasPrefix(p, dir+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workdir")
	}
	return p, nil
}

var (
	goSubs  = map[string]bool{"build": true, "vet": true, "test": true, "fmt": true, "list": true}
	gitSubs = map[string]bool{"status": true, "diff": true, "log": true, "show": true, "add": true, "commit": true}
	// emily subcommands the agent may drive (a subset of emily.cli, each its own IAM tool)
	emilyTools = map[string]bool{"observe": true, "apples": true, "changelog": true, "backlog": true, "status": true}
)

func clipStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "\n...[truncated]"
	}
	return s
}

func runExec(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	err := c.Run()
	return buf.String(), err
}

// toolSpec is what the model sees.
type toolSpec struct{ Name, Desc string }

var coderToolSpecs = []toolSpec{
	{"observe", "emily observe: post an observation. args e.g. -s info \"summary\""},
	{"apples", "emily apples: list/post Apples. args e.g. list -n 5  |  post -t completion -repo X \"title\""},
	{"changelog", "emily changelog add <repo> \"what changed\""},
	{"backlog", "emily backlog: curate the backlog"},
	{"status", "emily status: cluster/repo status"},
	{"grep", "search files in workdir: grep -rn. args e.g. -rn -i \"TODO\" cmd/   (flags + pattern + relative paths)"},
	{"sed", "stream edit in workdir (GNU sed --sandbox). Print a range: -n '10,40p' file.go. Edit in place: -i 's/old/new/g' file.go"},
	{"go", "go build|vet|test|fmt|list in workdir. args e.g. test ./..."},
	{"git", "git status|diff|log|show|add|commit in workdir (never push)"},
	{"files", "list/read/write files. use op=list|read|write with path (and content for write)"},
	{"claude_files", "read the CLAUDE.md/GEMINI.md instruction files that govern this workdir. No args: outline. section=<heading words>: that section's text"},
	{"finish", "declare the task done with a summary (only after verifying)"},
}

// execTool runs one tool call. args is the raw string for command tools; files/claude_files take
// the structured fields. Returns the (clipped) output and whether it failed.
func (a *coderAgent) execTool(name string, m map[string]any) (string, bool) {
	str := func(k string) string { v, _ := m[k].(string); return v }
	switch name {
	case "files":
		p, err := confinePath(a.dir, str("path"))
		if err != nil {
			return err.Error(), true
		}
		switch str("op") {
		case "list":
			es, err := os.ReadDir(p)
			if err != nil {
				return err.Error(), true
			}
			var names []string
			for _, e := range es {
				n := e.Name()
				if e.IsDir() {
					n += "/"
				}
				names = append(names, n)
			}
			sort.Strings(names)
			return clipStr(strings.Join(names, "\n"), 3000), false
		case "read":
			b, err := os.ReadFile(p)
			if err != nil {
				return err.Error(), true
			}
			return clipStr(string(b), 4000), false
		case "write":
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(str("content")), 0o644); err != nil {
				return err.Error(), true
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(str("content")), str("path")), false
		}
		return "files: op must be list, read or write", true
	case "claude_files":
		return claudeFilesTool(a.dir, str("section")), false
	case "finish":
		return "DONE: " + str("summary"), false
	}

	args, err := splitWords(str("args"))
	if err != nil {
		return err.Error(), true
	}
	var out string
	switch {
	case emilyTools[name]:
		self, _ := os.Executable()
		out, err = runExec(a.dir, 2*time.Minute, self, append([]string{name}, args...)...)
	case name == "grep":
		if err := confineArgs(args); err != nil {
			return err.Error(), true
		}
		out, err = runExec(a.dir, time.Minute, "grep", append([]string{"-I", "--color=never"}, args...)...)
		if err != nil && out == "" {
			return "(no matches)", false // grep exits 1 on no match
		}
	case name == "sed":
		if err := confineArgs(args); err != nil {
			return err.Error(), true
		}
		out, err = runExec(a.dir, time.Minute, "sed", append([]string{"--sandbox"}, args...)...)
	case name == "go":
		if len(args) == 0 || !goSubs[args[0]] {
			return "go: only build, vet, test, fmt, list are allowed", true
		}
		if err := confineArgs(args); err != nil {
			return err.Error(), true
		}
		out, err = runExec(a.dir, 10*time.Minute, "go", args...)
	case name == "git":
		if len(args) == 0 || !gitSubs[args[0]] {
			return "git: only status, diff, log, show, add, commit are allowed (never push)", true
		}
		if err := confineArgs(args); err != nil {
			return err.Error(), true
		}
		out, err = runExec(a.dir, time.Minute, "git", args...)
	default:
		return "unknown tool " + name, true
	}
	if err != nil {
		out += "\n[exit error: " + err.Error() + "]"
	}
	return clipStr(out, 3000), err != nil
}
