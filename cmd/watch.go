// cmd/watch.go — emily watch
// Polls IDUNA for new Apples, prints them as they arrive. Like tail -f for the agent log.
// Runs until Ctrl-C.

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func RunWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Int("interval", 5, "poll interval in seconds")
	fs.IntVar(interval, "i", 5, "poll interval in seconds")
	repo := fs.String("repo", "", "filter by source_repo")
	fs.StringVar(repo, "r", "", "filter by source_repo")
	appleType := fs.String("type", "", "filter by apple_type")
	fs.StringVar(appleType, "t", "", "filter by apple_type")
	quiet := fs.Bool("quiet", false, "suppress header and poll messages")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Positional arg is a convenience repo filter
	if fs.NArg() > 0 && *repo == "" {
		*repo = fs.Arg(0)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set — cannot connect to IDUNA")
		return 2
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)

	if !*quiet {
		fmt.Printf("\n◈ EMILY OS — WATCH | %s | poll every %ds\n", cfg.IDUNABaseURL, *interval)
		if *repo != "" {
			fmt.Printf("  filter: repo=%s\n", *repo)
		}
		if *appleType != "" {
			fmt.Printf("  filter: type=%s\n", *appleType)
		}
		fmt.Println("  ctrl-c to stop\n")
		fmt.Println("  ─────────────────────────────────────────────────────────────")
	}

	// Bootstrap: find the current highest ID so we only show NEW apples going forward.
	var lastID int64
	if apples, err := client.ListApples(iduna.AppleListFilters{
		Limit:      50,
		SourceRepo: *repo,
		AppleType:  *appleType,
	}); err == nil && len(apples) > 0 {
		lastID = apples[0].ID // IDUNA returns newest-first
		if !*quiet {
			fmt.Printf("  bootstrapped at Apple #%d — watching for new apples...\n\n", lastID)
		}
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to IDUNA: %v\n", err)
		return 4
	} else {
		if !*quiet {
			fmt.Println("  no existing apples — watching for first apple...\n")
		}
	}

	// Signal handling for clean Ctrl-C exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(*interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			if !*quiet {
				fmt.Println("\n  stopped.")
			}
			return 0

		case <-ticker.C:
			apples, err := client.ListApples(iduna.AppleListFilters{
				Limit:      50,
				SourceRepo: *repo,
				AppleType:  *appleType,
			})
			if err != nil {
				if !*quiet {
					fmt.Fprintf(os.Stderr, "  [poll error: %v]\n", err)
				}
				continue
			}

			// Collect apples with ID > lastID, reversed to print oldest-first.
			var newOnes []iduna.Apple
			for _, a := range apples {
				if a.ID > lastID {
					newOnes = append(newOnes, a)
				}
			}
			// apples are newest-first; reverse for chronological output
			for i, j := 0, len(newOnes)-1; i < j; i, j = i+1, j-1 {
				newOnes[i], newOnes[j] = newOnes[j], newOnes[i]
			}

			for _, a := range newOnes {
				printWatchApple(a)
				if a.ID > lastID {
					lastID = a.ID
				}
			}
		}
	}
}

func printWatchApple(a iduna.Apple) {
	ts := a.RecordedAt
	if len(ts) >= 19 {
		ts = ts[:19]
	}
	ts = strings.Replace(ts, "T", " ", 1)

	repo := fmt.Sprintf("[%-12s]", a.SourceRepo)
	atype := truncate(a.AppleType, 22)
	title := truncate(a.Title, 55)

	fmt.Printf("  +#%4d  %s  %s  %-22s  %s\n", a.ID, ts, repo, atype, title)
}
