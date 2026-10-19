package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	got, err := splitWords(`-rn -i "two words" 'it is' plain`)
	want := []string{"-rn", "-i", "two words", "it is", "plain"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := splitWords(`"open`); err == nil {
		t.Fatal("unterminated quote must error")
	}
	// operators are just words: no chaining is possible because nothing is run through a shell
	got, _ = splitWords(`a; rm -rf / && b`)
	if got[0] != "a;" {
		t.Fatalf("operators must stay literal: %q", got)
	}
}

func TestConfine(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "..", "../x", "a/../../x", "a/.."} {
		if confineArgs([]string{bad}) == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if confineArgs([]string{"-rn", "TODO", "cmd/", "a..b"}) != nil {
		t.Error("ordinary args must pass")
	}
	if _, err := confinePath("/work", "../../etc/passwd"); err != nil {
		// Clean("/"+rel) pins traversal to the root of the workdir, so this must resolve inside it
		t.Errorf("traversal should be neutralised inside workdir, got %v", err)
	}
	if p, _ := confinePath("/work", "../../etc/passwd"); !strings.HasPrefix(p, "/work/") {
		t.Errorf("escaped: %s", p)
	}
}

func TestToolAllowed(t *testing.T) {
	if toolAllowed([]string{"coder.chat.send"}, "grep") {
		t.Error("chat.send must not grant tools")
	}
	if !toolAllowed([]string{"coder.tool.grep"}, "grep") || toolAllowed([]string{"coder.tool.grep"}, "sed") {
		t.Error("per-tool grant must be exact")
	}
	if !toolAllowed([]string{"iduna.admin"}, "sed") {
		t.Error("admin superset")
	}
	if n := len(openAITools([]string{"coder.tool.grep"})); n != 1 {
		t.Errorf("model must only be offered granted tools, got %d", n)
	}
}

const sampleClaude = "# Title\nintro\n\n## Setup\nsetup text\n\n## The Emily Way — Operating Protocol\n1. Backlog first\n```\n# not a heading\n```\n2. Apple before done\n\n## Other\nx\n"

func TestClaudeFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(sampleClaude), 0o644)
	sub := filepath.Join(dir, "repo")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "GEMINI.md"), []byte("# G\n## Rules\nbe nice\n"), 0o644)
	files := findClaudeFiles(sub)
	var sawC, sawG bool
	for _, f := range files {
		sawC = sawC || f == filepath.Join(dir, "CLAUDE.md")
		sawG = sawG || f == filepath.Join(sub, "GEMINI.md")
	}
	if !sawC || !sawG {
		t.Fatalf("must walk up and find both: %v", files)
	}
	p := claudeFilesPrompt(sub, 5000)
	if !strings.Contains(p, "Apple before done") || !strings.Contains(p, "# not a heading") {
		t.Errorf("protocol section (incl. fenced '#') must be injected:\n%s", p)
	}
	if strings.Contains(p, "setup text") {
		t.Error("non-protocol sections are fetched on demand, not injected")
	}
	if out := claudeFilesTool(sub, "setup"); !strings.Contains(out, "setup text") {
		t.Errorf("section lookup failed: %s", out)
	}
	if out := claudeFilesTool(sub, ""); !strings.Contains(out, ":: Other") {
		t.Errorf("outline failed: %s", out)
	}
}

func TestGrepSedTools(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTODO fix\nthree\n"), 0o644)
	a := &coderAgent{dir: dir}
	out, isErr := a.execTool("grep", map[string]any{"args": "-rn TODO ."})
	if isErr || !strings.Contains(out, "a.txt:2:TODO fix") {
		t.Fatalf("grep: %q %v", out, isErr)
	}
	if out, isErr := a.execTool("grep", map[string]any{"args": "-rn NOPE ."}); isErr || out != "(no matches)" {
		t.Fatalf("no-match is not an error: %q %v", out, isErr)
	}
	if _, isErr := a.execTool("grep", map[string]any{"args": "-rn x /etc"}); !isErr {
		t.Fatal("absolute path must be refused")
	}
	if out, _ := a.execTool("sed", map[string]any{"args": "-n 2p a.txt"}); strings.TrimSpace(out) != "TODO fix" {
		t.Fatalf("sed print: %q", out)
	}
	if _, isErr := a.execTool("sed", map[string]any{"args": "-i s/TODO/DONE/ a.txt"}); isErr {
		t.Fatal("sed -i failed")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); !strings.Contains(string(b), "DONE fix") {
		t.Fatalf("sed -i did not edit: %q", b)
	}
	// --sandbox: the e (execute) command must be rejected
	if _, isErr := a.execTool("sed", map[string]any{"args": "-i 1e\\ touch\\ pwned a.txt"}); !isErr {
		t.Error("sed e command must be refused by --sandbox")
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("sed executed a command")
	}
	if _, isErr := a.execTool("git", map[string]any{"args": "push origin main"}); !isErr {
		t.Error("git push must be refused")
	}
	if _, isErr := a.execTool("go", map[string]any{"args": "run main.go"}); !isErr {
		t.Error("go run must be refused")
	}
}

func TestTrimMsgsDropsOrphanToolResults(t *testing.T) {
	big := strings.Repeat("x", 4000)
	m := []map[string]any{{"role": "system", "content": "s"},
		{"role": "assistant", "content": big, "tool_calls": 1}, {"role": "tool", "content": big},
		{"role": "user", "content": "q"}, {"role": "assistant", "content": "a"}}
	got := trimMsgs(m, 3000)
	if got[0]["role"] != "system" || (len(got) > 1 && got[1]["role"] == "tool") {
		t.Fatalf("bad trim: %v", got)
	}
}
