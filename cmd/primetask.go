// cmd/primetask.go — emily prime-task
// Writes a directed task to EMILY/signals/tasks/ for the observation-watcher's
// prime task poller. The watcher picks it up within 10s and invokes Claude Code.
// This closes the operator → Emily Prime → FatBaby directed loop from the CLI.

package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

type primeTaskFile struct {
	Timestamp          string   `json:"timestamp"`
	From               string   `json:"from"`
	To                 string   `json:"to"`
	TaskID             string   `json:"task_id"`
	TaskType           string   `json:"task_type"`
	Priority           string   `json:"priority"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Context            string   `json:"context,omitempty"`
	Deadline           string   `json:"deadline,omitempty"`
}

// criteriaList implements flag.Value for repeated --criteria flags.
type criteriaList []string

func (c *criteriaList) String() string  { return strings.Join(*c, "; ") }
func (c *criteriaList) Set(v string) error { *c = append(*c, v); return nil }

func RunPrimeTask(args []string) int {
	fs := flag.NewFlagSet("prime-task", flag.ContinueOnError)
	taskType := fs.String("type", "operator_directive", "task_type field (e.g. operator_directive, improve_signal, rsi_loop)")
	priority := fs.String("priority", "normal", "priority: low|normal|high|critical")
	context := fs.String("context", "", "strategic context for the task (optional)")
	deadline := fs.String("deadline", "", "optional deadline (free-form text)")
	dryRun := fs.Bool("dry-run", false, "print the task JSON without writing it")
	jsonOut := fs.Bool("json", false, "output JSON confirmation")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")

	var criteria criteriaList
	fs.Var(&criteria, "criteria", "acceptance criterion (repeatable: --criteria 'tests pass' --criteria 'committed')")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	var description string
	if fs.NArg() > 0 {
		description = strings.Join(fs.Args(), " ")
	} else {
		fmt.Fprintln(os.Stderr, "usage: emily prime-task [flags] <description>")
		fs.PrintDefaults()
		return 1
	}

	switch *priority {
	case "low", "normal", "high", "critical":
	default:
		fmt.Fprintf(os.Stderr, "unknown priority %q — use low, normal, high, or critical\n", *priority)
		return 1
	}

	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)
	taskID := fmt.Sprintf("task-%d", rand.Int63())
	// Filename: timestamp with colons removed + task ID (lexicographic sort = chronological)
	fname := fmt.Sprintf("%s-%s.json", strings.ReplaceAll(ts, ":", ""), taskID)

	task := primeTaskFile{
		Timestamp:          ts,
		From:               "operator-cli",
		To:                 "fatbaby-emily",
		TaskID:             taskID,
		TaskType:           *taskType,
		Priority:           *priority,
		Description:        description,
		AcceptanceCriteria: criteria,
		Context:            *context,
		Deadline:           *deadline,
	}
	if len(task.AcceptanceCriteria) == 0 {
		task.AcceptanceCriteria = nil // omit empty array in JSON
	}

	data, _ := json.MarshalIndent(task, "", "  ")
	data = append(data, '\n')

	if *dryRun {
		if *jsonOut {
			fmt.Printf(`{"dry_run":true,"task_id":%q,"filename":%q}`+"\n", taskID, fname)
		} else {
			fmt.Println("[dry-run] would write:")
			fmt.Printf("  file:      EMILY/signals/tasks/%s\n", fname)
			fmt.Printf("  task_id:   %s\n", taskID)
			fmt.Printf("  type:      %s\n", *taskType)
			fmt.Printf("  priority:  %s\n", *priority)
			fmt.Printf("  desc:      %s\n", description)
			fmt.Println()
			fmt.Println(string(data))
		}
		return 0
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	tasksDir := filepath.Join(cfg.EmilyRoot, "signals", "tasks")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir %s: %v\n", tasksDir, err)
		return 3
	}

	fpath := filepath.Join(tasksDir, fname)
	if err := os.WriteFile(fpath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", fpath, err)
		return 3
	}

	// Apple receipt
	var appleID int64
	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		title := fmt.Sprintf("[prime-task] %s", description)
		if len(title) > 100 {
			title = title[:99] + "…"
		}
		body := fmt.Sprintf("task_id: %s\ntype: %s\npriority: %s\ndesc: %s", taskID, *taskType, *priority, description)
		id, _ := client.PostApple(iduna.ApplePayload{
			AppleType:  "prime_task",
			Title:      title,
			Body:       body,
			SourceRepo: "CLI",
			RunID:      taskID,
		})
		appleID = id
	}

	if *jsonOut {
		out := map[string]interface{}{
			"written":   true,
			"path":      fpath,
			"task_id":   taskID,
			"task_type": *taskType,
			"priority":  *priority,
		}
		if appleID > 0 {
			out["apple_id"] = appleID
		}
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(out)
	} else {
		fmt.Printf("✓ prime task written\n")
		fmt.Printf("  path:      %s\n", fpath)
		fmt.Printf("  task_id:   %s\n", taskID)
		fmt.Printf("  type:      %s\n", *taskType)
		fmt.Printf("  priority:  %s\n", *priority)
		fmt.Printf("  desc:      %s\n", description)
		if appleID > 0 {
			fmt.Printf("  apple:     #%d filed to IDUNA\n", appleID)
		}
		fmt.Printf("  watcher:   picks up within 10s → invokes claude on FatBaby\n")
	}
	return 0
}
