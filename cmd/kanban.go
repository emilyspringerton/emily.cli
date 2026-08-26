// cmd/kanban.go — emily kanban {list|add|move|rm}
//
// CLI/agent half of the kanban prioritization layer (see IDUNA's
// internal/http/handlers/kanban.go for the full founder-quote chain and
// design). Founder real-time: "i can ask the ai agent to work from the
// priority or cruise backlog" -- this is that: `emily kanban list --queue
// priority` tells an agent (or the founder) what's actually prioritized
// right now, on top of BACKLOG.md's own sprint sections.
package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// RunKanban dispatches emily kanban subcommands.
func RunKanban(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily kanban: missing subcommand. Try: list, add, move, rm")
		return 1
	}
	switch args[0] {
	case "list":
		return runKanbanList(args[1:])
	case "add":
		return runKanbanAdd(args[1:])
	case "move":
		return runKanbanMove(args[1:])
	case "rm":
		return runKanbanRemove(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily kanban: unknown subcommand %q — try: list, add, move, rm\n", args[0])
		return 1
	}
}

func kanbanClient() (*iduna.Client, int) {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return nil, 1
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set and not found in secrets file")
		return nil, 2
	}
	return iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret), 0
}

func runKanbanList(args []string) int {
	fs := flag.NewFlagSet("kanban list", flag.ContinueOnError)
	queue := fs.String("queue", "", "filter to one queue: backlog, priority, cruise (default: all)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	cards, err := client.ListKanbanCards(*queue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban list: %v\n", err)
		return 1
	}
	if len(cards) == 0 {
		fmt.Println("(no cards)")
		return 0
	}
	for _, c := range cards {
		fmt.Printf("  #%-4d [%-8s] %-14s %s\n", c.ID, c.Queue, c.BacklogItemID, c.Title)
	}
	return 0
}

func runKanbanAdd(args []string) int {
	fs := flag.NewFlagSet("kanban add", flag.ContinueOnError)
	queue := fs.String("queue", "", "backlog (default), priority, or cruise")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban add [--queue priority|cruise] <backlog-item-id> <title>")
		return 1
	}
	backlogItemID := fs.Arg(0)
	title := fs.Arg(1)
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	id, err := client.AddKanbanCard(backlogItemID, title, *queue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban add: %v\n", err)
		return 1
	}
	fmt.Printf("✓ card #%d added (%s)\n", id, backlogItemID)
	return 0
}

func runKanbanMove(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban move <card-id> <backlog|priority|cruise>")
		return 1
	}
	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "emily kanban move: invalid card id %q\n", args[0])
		return 1
	}
	queue := args[1]
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	if err := client.MoveKanbanCard(id, queue); err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban move: %v\n", err)
		return 1
	}
	fmt.Printf("✓ card #%d moved to %s\n", id, queue)
	return 0
}

func runKanbanRemove(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban rm <card-id>")
		return 1
	}
	var id int64
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "emily kanban rm: invalid card id %q\n", args[0])
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	if err := client.DeleteKanbanCard(id); err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban rm: %v\n", err)
		return 1
	}
	fmt.Printf("✓ card #%d removed\n", id)
	return 0
}
