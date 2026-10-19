// cmd/coder_claudefiles.go — reads CLAUDE.md / GEMINI.md files into the agent's behavior.
// The agent's context is small (8k tokens) and the real files are large, so the system prompt gets
// (a) the file list with a heading outline and (b) the bodies of the operating-protocol sections,
// and the claude_files tool pulls any other section on demand.
package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var instructionNames = []string{"CLAUDE.md", "GEMINI.md"}

// findClaudeFiles returns instruction files from the user's ~/.claude, then root-most to nearest
// directory up from dir (nearest last, so it reads as the most specific).
func findClaudeFiles(dir string) []string {
	var dirs []string
	for d := dir; ; d = filepath.Dir(d) {
		dirs = append([]string{d}, dirs...)
		if d == filepath.Dir(d) {
			break
		}
	}
	var out []string
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
			out = append(out, filepath.Join(home, ".claude", "CLAUDE.md"))
		}
	}
	for _, d := range dirs {
		for _, n := range instructionNames {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

var headingRe = regexp.MustCompile(`^(#{1,3})\s+(.+?)\s*$`)

type mdSection struct {
	Title string
	Body  string
}

// parseSections splits markdown into sections at #/##/### headings (fenced code is respected).
func parseSections(md string) []mdSection {
	var secs []mdSection
	var cur *mdSection
	inFence := false
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(body.String())
			secs = append(secs, *cur)
		}
		body.Reset()
	}
	for _, ln := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
		}
		if m := headingRe.FindStringSubmatch(ln); m != nil && !inFence {
			flush()
			cur = &mdSection{Title: m[2]}
			continue
		}
		body.WriteString(ln + "\n")
	}
	flush()
	return secs
}

// protocolSection marks sections that define how to behave (injected into the system prompt).
func protocolSection(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, "operating protocol") || strings.Contains(t, "emily way") || strings.Contains(t, "standing instruction")
}

// claudeFilesPrompt builds the system-prompt block: outline of every file + protocol sections.
func claudeFilesPrompt(dir string, budget int) string {
	var b strings.Builder
	for _, p := range findClaudeFiles(dir) {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		secs := parseSections(string(raw))
		b.WriteString("\n## " + p + "\nsections: ")
		var titles []string
		for _, s := range secs {
			titles = append(titles, s.Title)
		}
		b.WriteString(clipStr(strings.Join(titles, " | "), 700) + "\n")
		for _, s := range secs {
			if protocolSection(s.Title) && b.Len() < budget {
				b.WriteString("### " + s.Title + "\n" + clipStr(s.Body, 2200) + "\n")
			}
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return clipStr("INSTRUCTION FILES that govern this workdir (call claude_files with section=<words> to read any section in full):"+b.String(), budget)
}

// claudeFilesTool: no section -> outline of every file; section -> matching section text.
func claudeFilesTool(dir, section string) string {
	files := findClaudeFiles(dir)
	if len(files) == 0 {
		return "no CLAUDE.md/GEMINI.md found above " + dir
	}
	section = strings.ToLower(strings.TrimSpace(section))
	var b strings.Builder
	for _, p := range files {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, s := range parseSections(string(raw)) {
			if section == "" {
				b.WriteString(p + " :: " + s.Title + "\n")
			} else if strings.Contains(strings.ToLower(s.Title), section) {
				b.WriteString("[" + p + " :: " + s.Title + "]\n" + s.Body + "\n\n")
			}
		}
	}
	if b.Len() == 0 {
		return "no section matching " + section
	}
	return clipStr(b.String(), 4000)
}
