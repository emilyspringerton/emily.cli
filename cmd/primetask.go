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

// webAuditNewssitePreset asks Emily Prime to audit the FatBaby newssite as a real browser client.
// Dispatch with: emily prime-task --preset web-audit-newssite
var webAuditNewssitePreset = struct {
	taskType    string
	priority    string
	description string
	context     string
	criteria    []string
}{
	taskType: "web_audit",
	priority: "normal",
	description: "Audit the FatBaby newssite (http://localhost:8082) as a real HTTP client using the web_audit_url tool. " +
		"Check: (1) homepage loads with HTTP 200, title and h1 are present; " +
		"(2) run with check_links=true to find all same-host links and detect any returning 4xx/5xx; " +
		"(3) try at least 3 ticker pages (e.g. /ticker/AAPL, /ticker/AMZN, /ticker/MSFT) and verify they load; " +
		"(4) check the signal API at http://localhost:8083/signals?ticker=AAPL if it responds; " +
		"(5) post findings as a signal_observation Apple via emily observe. " +
		"If the newssite is unreachable, note it as a warning (do not fail the task — the pipeline may be stopped).",
	context: "Emily Prime now has the web_audit_url tool which lets her act as a real HTTP client against the product URLs. " +
		"The FatBaby newssite (:8082) and SignalAPI (:8083) are the front door for the products, and they will eventually be surfaced " +
		"inside the MJOLNIR Android app WebView. This audit validates that the front door is operational and reports any broken links " +
		"or missing content. The newssite is server-rendered Go HTML templates — no JavaScript execution needed.",
	criteria: []string{
		"web_audit_url called on http://localhost:8082 with check_links=true",
		"at least 3 ticker pages verified or noted as unreachable",
		"signal API at :8083 tested",
		"findings posted as signal_observation Apple via emily observe",
		"summary includes HTTP status, title, link count, and any broken links",
	},
}

// rsiTokenReportPreset is the canned RSI token-efficiency report task.
// Dispatch with: emily prime-task --preset rsi-token-report
var rsiTokenReportPreset = struct {
	taskType    string
	priority    string
	description string
	context     string
	criteria    []string
}{
	taskType: "rsi_report",
	priority: "high",
	description: "Write a report on optimal token use for recursive self-improvement across the full Emily OS system. " +
		"Analyse current token spend per cycle in emily-agent (cron.go RSI loop), observation-watcher invocations, " +
		"and prime-task → Claude Code dispatches. Identify the highest-ROI improvements: " +
		"e.g. prompt compression, caching, batching observations, gating trivial cycles. " +
		"Produce a prioritised action list with estimated token savings per item. " +
		"Then implement the top item as a concrete code change, commit it, and post findings as an IDUNA Apple.",
	context: "This is a tic-toc RSI loop: Emily Prime writes the report, then Claude Code implements the top recommendation, " +
		"then Emily Prime reviews the diff and iterates. Each iteration should move the needle on actual token efficiency.",
	criteria: []string{
		"report covers emily-agent RSI loop, observation-watcher, and prime-task dispatch",
		"at least 3 prioritised improvements with estimated token impact",
		"top item implemented as committed code change",
		"IDUNA Apple posted with report summary and diff link",
	},
}

func RunPrimeTask(args []string) int {
	fs := flag.NewFlagSet("prime-task", flag.ContinueOnError)
	taskType := fs.String("type", "operator_directive", "task_type field (e.g. operator_directive, improve_signal, rsi_loop)")
	priority := fs.String("priority", "normal", "priority: low|normal|high|critical")
	context := fs.String("context", "", "strategic context for the task (optional)")
	deadline := fs.String("deadline", "", "optional deadline (free-form text)")
	dryRun := fs.Bool("dry-run", false, "print the task JSON without writing it")
	jsonOut := fs.Bool("json", false, "output JSON confirmation")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")
	preset := fs.String("preset", "", "use a canned task preset: rsi-token-report | entity-graph-refinement | eps-coverage-review | web-audit-newssite")

	var criteria criteriaList
	fs.Var(&criteria, "criteria", "acceptance criterion (repeatable: --criteria 'tests pass' --criteria 'committed')")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Apply preset before reading free-form args
	if *preset == "rsi-token-report" {
		if *taskType == "operator_directive" { // only override default
			*taskType = rsiTokenReportPreset.taskType
		}
		if *priority == "normal" {
			*priority = rsiTokenReportPreset.priority
		}
		if *context == "" {
			*context = rsiTokenReportPreset.context
		}
		if len(criteria) == 0 {
			criteria = rsiTokenReportPreset.criteria
		}
	} else if *preset == "web-audit-newssite" {
		if *taskType == "operator_directive" {
			*taskType = webAuditNewssitePreset.taskType
		}
		if *priority == "normal" {
			*priority = webAuditNewssitePreset.priority
		}
		if *context == "" {
			*context = webAuditNewssitePreset.context
		}
		if len(criteria) == 0 {
			criteria = webAuditNewssitePreset.criteria
		}
	} else if *preset != "" {
		fmt.Fprintf(os.Stderr, "unknown preset %q — available: rsi-token-report, web-audit-newssite\n", *preset)
		return 1
	}

	var description string
	if fs.NArg() > 0 {
		description = strings.Join(fs.Args(), " ")
	} else if *preset == "rsi-token-report" {
		description = rsiTokenReportPreset.description
	} else if *preset == "web-audit-newssite" {
		description = webAuditNewssitePreset.description
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
