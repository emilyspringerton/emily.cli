// cmd/changelog.go — emily changelog add <repo> <message>
// Appends a dated entry to <repo>/CHANGELOG.md.

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// RunChangelog dispatches emily changelog subcommands.
func RunChangelog(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily changelog: subcommand required (add)")
		return 1
	}
	switch args[0] {
	case "add":
		return runChangelogAdd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily changelog: unknown subcommand %q — try: add\n", args[0])
		return 1
	}
}

func runChangelogAdd(args []string) int {
	fs := flag.NewFlagSet("changelog add", flag.ContinueOnError)
	repoPath := fs.String("path", "", "explicit path to repo root (overrides <repo> positional arg)")
	noCommit := fs.Bool("no-commit", false, "write CHANGELOG.md but skip git commit")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "usage: emily changelog add [--path <dir>] <repo> <message>")
		return 1
	}

	repoName := rest[0]
	message := strings.Join(rest[1:], " ")

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	root := *repoPath
	if root == "" {
		// Derive from EmilyRoot parent: /home/fatbaby/EMILY → /home/fatbaby/<repo>
		root = filepath.Join(filepath.Dir(cfg.EmilyRoot), repoName)
	}

	if _, err := os.Stat(root); err != nil {
		fmt.Fprintf(os.Stderr, "repo %q not found at %s\n", repoName, root)
		return 1
	}

	changelogPath := filepath.Join(root, "CHANGELOG.md")
	today := time.Now().UTC().Format("2006-01-02")

	if err := appendChangelog(changelogPath, today, message); err != nil {
		fmt.Fprintf(os.Stderr, "changelog: %v\n", err)
		return 1
	}

	fmt.Printf("  ✓ CHANGELOG.md updated: %s — %s\n", today, truncate(message, 70))

	if !*noCommit {
		add := exec.Command("git", "-C", root, "add", "CHANGELOG.md")
		if out, err := add.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "git add: %v: %s\n", err, out)
			return 1
		}
		msg := fmt.Sprintf("changelog(%s): %s", repoName, truncate(message, 60))
		commit := exec.Command("git", "-C", root, "commit", "-m", msg)
		if out, err := commit.CombinedOutput(); err != nil {
			if strings.Contains(string(out), "nothing to commit") {
				fmt.Println("  (nothing to commit)")
			} else {
				fmt.Fprintf(os.Stderr, "git commit: %v: %s\n", err, out)
				return 1
			}
		} else {
			fmt.Printf("  ✓ committed in %s\n", repoName)
		}
	}

	if !*noApple && cfg.IDUNAAgentSecret != "" {
		// Soft-fail: Apple posting is best-effort for changelog entries.
		_ = postChangelogApple(cfg, repoName, message)
	}

	return 0
}

// appendChangelog writes `- <message>` under a `## <today>` header in CHANGELOG.md.
// If today's header already exists the bullet is inserted immediately after its header line.
// Otherwise a new section is prepended at the top (after any H1 title line).
func appendChangelog(path, today, message string) error {
	var content string
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		content = string(data)
	}

	newBullet := "- " + message + "\n"
	todayHeader := "## " + today

	if idx := strings.Index(content, todayHeader); idx >= 0 {
		// Today's section exists — insert bullet immediately after the header line.
		end := strings.Index(content[idx:], "\n")
		if end < 0 {
			content += "\n" + newBullet
		} else {
			insertAt := idx + end + 1
			content = content[:insertAt] + newBullet + content[insertAt:]
		}
	} else {
		newSection := todayHeader + "\n\n" + newBullet + "\n"
		if content == "" {
			content = newSection
		} else {
			// Preserve any H1 title at the top; insert new section after it.
			lines := strings.SplitN(content, "\n", 3)
			if strings.HasPrefix(lines[0], "# ") {
				// first line is H1 title
				rest := ""
				if len(lines) > 1 {
					rest = strings.Join(lines[1:], "\n")
					if !strings.HasPrefix(strings.TrimLeft(rest, "\n"), "##") {
						// ensure blank line between H1 and new section
						rest = "\n" + strings.TrimLeft(rest, "\n")
					}
				}
				content = lines[0] + "\n\n" + newSection + rest
			} else {
				content = newSection + content
			}
		}
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func postChangelogApple(cfg *config.Config, repoName, message string) error {
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	_, err := client.PostApple(iduna.ApplePayload{
		SourceRepo: repoName,
		RunID:      fmt.Sprintf("changelog-%d", time.Now().Unix()),
		AppleType:  "completion",
		Title:      fmt.Sprintf("changelog(%s): %s", repoName, truncate(message, 60)),
		Body:       fmt.Sprintf("CHANGELOG updated in %s: %s", repoName, message),
	})
	return err
}
