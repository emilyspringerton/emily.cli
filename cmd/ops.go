// cmd/ops.go — emily ops render-unit <playbook.toml> <service> <working-dir>
//
// Real CLI entry point for internal/opsplaybook (EMILY/docs/PARENACLOUD_OPS_NORTHSTAR.md) --
// renders one [[service]] from a real, standardized ops/playbook.toml into real systemd unit
// file text on stdout. Deliberately does NOT write to ~/.config/systemd/user/, run daemon-
// reload, or touch systemctl at all -- see opsplaybook.go's own package doc comment for why
// that's real, separate, live-infra-mutation work this command doesn't do yet.
package cmd

import (
	"fmt"
	"os"

	"github.com/emilyspringerton/emily-cli/internal/opsplaybook"
)

// RunOps dispatches emily ops subcommands.
func RunOps(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily ops: subcommand required (render-unit)")
		return 1
	}
	switch args[0] {
	case "render-unit":
		return runOpsRenderUnit(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily ops: unknown subcommand %q — try: render-unit\n", args[0])
		return 1
	}
}

func runOpsRenderUnit(args []string) int {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: emily ops render-unit <playbook.toml> <service-name> <working-dir>")
		return 1
	}
	playbookPath, serviceName, workingDir := args[0], args[1], args[2]

	text, err := os.ReadFile(playbookPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily ops render-unit: read %s: %v\n", playbookPath, err)
		return 1
	}
	pb, err := opsplaybook.ParsePlaybook(string(text))
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily ops render-unit: %v\n", err)
		return 1
	}

	var svc *opsplaybook.Service
	for i := range pb.Service {
		if pb.Service[i].Name == serviceName {
			svc = &pb.Service[i]
			break
		}
	}
	if svc == nil {
		fmt.Fprintf(os.Stderr, "emily ops render-unit: no service named %q in %s\n", serviceName, playbookPath)
		return 1
	}

	unit, err := opsplaybook.RenderUnit(pb.Game, *svc, workingDir, pb.Values)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily ops render-unit: %v\n", err)
		return 1
	}
	fmt.Print(unit)
	return 0
}
