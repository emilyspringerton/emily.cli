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
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// RunKanban dispatches emily kanban subcommands.
func RunKanban(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily kanban: missing subcommand. Try: list, add, move, rm, comment, comments")
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
	case "comment":
		return runKanbanComment(args[1:])
	case "comments":
		return runKanbanComments(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily kanban: unknown subcommand %q — try: list, add, move, rm, comment, comments\n", args[0])
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
	queue := fs.String("queue", "", "filter to one queue: backlog, priority, pending, cruise (default: all)")
	search := fs.String("search", "", "plain substring search over each card's own title and backlog id (kanban card 3454325)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	cards, err := client.ListKanbanCards(*queue, *search)
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
	queue := fs.String("queue", "", "backlog (default), priority, pending, or cruise")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// Kanban card 82821821 (founder real-time): "i dont want to type ticket numbers if i dont
	// want to ... sometimes its too much cognitive load". A single positional arg is now
	// title-only -- the id is left blank and auto-generated server-side. The original two-arg
	// form (explicit id + title, the "sometimes I do want to, like jira projects" half of the
	// same ask) still works exactly as before.
	var backlogItemID, title string
	switch fs.NArg() {
	case 1:
		title = fs.Arg(0)
	case 2:
		backlogItemID = fs.Arg(0)
		title = fs.Arg(1)
	default:
		fmt.Fprintln(os.Stderr, "usage: emily kanban add [--queue priority|pending|cruise] [<backlog-item-id>] <title>")
		fmt.Fprintln(os.Stderr, "       (omit <backlog-item-id> to auto-generate a ticket number)")
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	id, resolvedID, err := client.AddKanbanCard(backlogItemID, title, *queue)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban add: %v\n", err)
		return 1
	}
	fmt.Printf("✓ card #%d added (%s)\n", id, resolvedID)
	return 0
}

func runKanbanMove(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban move <card-id> <backlog|priority|pending|cruise|done>")
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

// runKanbanComment: `emily kanban comment <card-id> <text...>` -- leave a question (or reply) on a card.
// IDUNA records the author from this CLI's own login, so a board user's reply is attributed to them.
func runKanbanComment(args []string) int {
	var id int64
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban comment <card-id> <text>")
		return 1
	}
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "emily kanban comment: invalid card id %q\n", args[0])
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	c, err := client.AddKanbanComment(id, strings.Join(args[1:], " "))
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban comment: %v\n", err)
		return 1
	}
	fmt.Printf("✓ comment #%d on card #%d as %s\n", c.ID, id, c.Author)
	return 0
}

// runKanbanComments: `emily kanban comments <card-id>` -- read the thread (find the replies to your questions).
func runKanbanComments(args []string) int {
	var id int64
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: emily kanban comments <card-id>")
		return 1
	}
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil || id <= 0 {
		fmt.Fprintf(os.Stderr, "emily kanban comments: invalid card id %q\n", args[0])
		return 1
	}
	client, code := kanbanClient()
	if client == nil {
		return code
	}
	items, err := client.ListKanbanComments(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily kanban comments: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Printf("card #%d has no comments\n", id)
		return 0
	}
	for _, c := range items {
		fmt.Printf("[%s] %s: %s\n", c.CreatedAt, c.Author, c.Body)
	}
	return 0
}
