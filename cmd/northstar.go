// cmd/northstar.go — emily northstar <repo>
// Reads and prints the NORTHSTAR.md for the named repo.
// Looks in <base>/<repo>/docs/NORTHSTAR.md and <base>/<repo>/docs2/NORTHSTAR.md.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// RunNorthstar implements `emily northstar <repo>`.
func RunNorthstar(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(`emily northstar <repo>

Print the NORTHSTAR.md for the named repo. Reads from:
  <base>/<repo>/docs/NORTHSTAR.md
  <base>/<repo>/docs2/NORTHSTAR.md  (SHANKPIT uses docs2/)

Known repos: PRRJECT_FATBABY, EMILY, IDUNA, MJOLNIR, SHANKPIT, TYLER, APPLES,
             EDIS, PITVIPER, EmilyOS, GoblinFoxDragon, emily.cli

Examples:
  emily northstar SHANKPIT
  emily northstar EMILY
  emily northstar PRRJECT_FATBABY
`)
		return 0
	}

	repoName := args[0]

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	base := filepath.Dir(cfg.FatBabyRoot)

	// Canonical path resolution: normalise common aliases.
	switch strings.ToUpper(repoName) {
	case "FATBABY", "PRRJECT_FATBABY":
		repoName = "PRRJECT_FATBABY"
	case "EMILY_CLI", "EMILY.CLI":
		repoName = "emily.cli"
	}

	candidates := []string{
		filepath.Join(base, repoName, "docs", "NORTHSTAR.md"),
		filepath.Join(base, repoName, "docs2", "NORTHSTAR.md"),
		filepath.Join(base, repoName, "NORTHSTAR.md"),
	}

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			fmt.Printf("# %s — %s\n\n", repoName, p)
			fmt.Println(string(data))
			return 0
		}
	}

	fmt.Fprintf(os.Stderr, "northstar: no NORTHSTAR.md found for %q\n", repoName)
	fmt.Fprintf(os.Stderr, "  tried:\n")
	for _, p := range candidates {
		fmt.Fprintf(os.Stderr, "    %s\n", p)
	}
	return 1
}
