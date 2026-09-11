// Package opsplaybook parses a game repo's real, standardized ops/playbook.toml (see
// EMILY/docs/PARENACLOUD_OPS_NORTHSTAR.md for the full format spec and why this exists —
// found while working backwards through the DEADWEIGHT/multi-tenant asks: every real game repo
// in this monorepo's own C/SDL2 arena lineage already hand-writes near-identical
// ops/systemd/*.service files, ECOWAR's own directory still literally containing several
// REDGARDEN-named copies never renamed after the fork) and renders it into real systemd unit
// file text.
//
// Real, deliberate scope: this package only renders unit text. It does not clone a repo, run a
// build, write a file to ~/.config/systemd/user/, or call systemctl — that's real, live
// infrastructure-mutation work belonging to whatever CLI surface actually applies a playbook
// (PARENACLOUD_NORTHSTAR.md's own still-open CLI-surface question), not this library.
package opsplaybook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Game names the repo a playbook belongs to. Informational plus a real default namespace for
// generated unit names/log paths.
type Game struct {
	Name string `toml:"name"`
	Repo string `toml:"repo"`
}

// EnvironmentFile mirrors the real, found-the-hard-way `EnvironmentFile=[-]path` convention —
// see ECOWAR/ops/systemd/ecowar-matchmaker.service's own header comment for the real 5-day
// outage a REQUIRED EnvironmentFile on a missing file caused. Required defaults to false
// (Go's own zero value) so a playbook author has to explicitly opt INTO the dangerous form,
// not the other way around.
type EnvironmentFile struct {
	Path     string `toml:"path"`
	Required bool   `toml:"required"`
}

// Service is one real, independent process. Kind is the one field a reader should check first
// — see this package's own doc comment and PARENACLOUD_OPS_NORTHSTAR.md for why "spawned"
// services (arena_server, spawned per-match by a matchmaker's own --server-bin) are real,
// documented entries here but never get their own rendered unit.
type Service struct {
	Name            string            `toml:"name"`
	Kind            string            `toml:"kind"` // "persistent" | "spawned"
	Description     string            `toml:"description"`
	Binary          string            `toml:"binary"`
	Args            []string          `toml:"args"`
	Port            string            `toml:"port"`
	UserLevel       *bool             `toml:"user_level"` // nil = default true, see EffectiveUserLevel
	Environment     map[string]string `toml:"environment"`
	EnvironmentFile *EnvironmentFile  `toml:"environment_file"`
	Restart         string            `toml:"restart"`
	RestartSec      int               `toml:"restart_sec"`
	MemoryMax       string            `toml:"memory_max"`
	LogPath         string            `toml:"log_path"`
	After           []string          `toml:"after"`
	Wants           []string          `toml:"wants"`
	StartDelaySec   int               `toml:"start_delay_sec"`
}

// EffectiveUserLevel returns the real, established-by-every-example default (true) when a
// playbook doesn't set it explicitly.
func (s Service) EffectiveUserLevel() bool {
	if s.UserLevel == nil {
		return true
	}
	return *s.UserLevel
}

// Playbook is one real, parsed ops/playbook.toml.
type Playbook struct {
	Game     Game              `toml:"game"`
	Service  []Service         `toml:"service"`
	Values   map[string]string `toml:"values"`
}

// ParsePlaybook parses real playbook TOML text (not a file path -- callers own their own file
// I/O, matching every other real parsing function in this codebase, e.g. golden.go's own JSON
// helpers).
func ParsePlaybook(text string) (*Playbook, error) {
	var pb Playbook
	// [values] is declared as a flat map above but TOML numbers/bools decode as their own
	// native types, not strings -- decode into a raw map first, then stringify every value for
	// real, uniform {{placeholder}} substitution regardless of the author's own literal type
	// (`lobby_size = 20` and `env_file_name = "x.env"` both need to substitute as plain text).
	var raw struct {
		Game    Game                   `toml:"game"`
		Service []Service              `toml:"service"`
		Values  map[string]interface{} `toml:"values"`
	}
	if _, err := toml.Decode(text, &raw); err != nil {
		return nil, fmt.Errorf("opsplaybook: parse: %w", err)
	}
	pb.Game = raw.Game
	pb.Service = raw.Service
	pb.Values = make(map[string]string, len(raw.Values))
	for k, v := range raw.Values {
		pb.Values[k] = fmt.Sprintf("%v", v)
	}
	return &pb, nil
}

// PersistentServices returns only the real systemd-unit-shaped services, in playbook order —
// "spawned" entries are real, documented, and deliberately excluded (see Service's own doc
// comment on Kind).
func (pb *Playbook) PersistentServices() []Service {
	out := make([]Service, 0, len(pb.Service))
	for _, s := range pb.Service {
		if s.Kind == "persistent" {
			out = append(out, s)
		}
	}
	return out
}

// substitute replaces every {{key}} in s with values[key]. A placeholder with no matching value
// is left verbatim (a real, deliberate choice: silently emitting an empty string for a typo'd
// placeholder name would be far worse than a unit file that visibly still says "{{typo}}" and
// fails loudly, matching this codebase's own "never fail silently" discipline).
func substitute(s string, values map[string]string) string {
	out := s
	for k, v := range values {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func substituteAll(list []string, values map[string]string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = substitute(s, values)
	}
	return out
}

// RenderUnit renders one persistent service into real systemd unit file text, substituting
// every {{placeholder}} against values (a playbook's own [values] merged with any real per-
// install override the caller applies first — merging itself is the caller's job, not this
// function's, so a caller can implement "playbook defaults + a small override file" however its
// own CLI surface wants to). workingDir is the real, absolute path this service's repo is
// checked out at on the target box (not stored in the playbook itself, which is repo-portable
// and knows nothing about any one real deploy's own filesystem layout).
//
// Real, deliberate scope: this function does NOT resolve cross-service unit-name references
// inside After/Wants (e.g. "ecowar-matchmaker.service") — a playbook author writes the real,
// final unit name directly, matching how every hand-written example in this monorepo already
// does it (Service.Name is the LOCAL name; the full generated unit name is
// "<game.name>-<service.name>.service", and a playbook's own After/Wants entries for a SIBLING
// service in the same playbook are expected to already spell that out).
func RenderUnit(game Game, svc Service, workingDir string, values map[string]string) (string, error) {
	if svc.Kind != "persistent" {
		return "", fmt.Errorf("opsplaybook: RenderUnit: service %q has kind %q, not \"persistent\" -- "+
			"a \"spawned\" service is documentation only and never gets a unit, see Service's own doc comment", svc.Name, svc.Kind)
	}
	unitName := fmt.Sprintf("%s-%s", game.Name, svc.Name)
	binary := substitute(svc.Binary, values)
	args := substituteAll(svc.Args, values)
	execStart := workingDir + "/" + binary
	if len(args) > 0 {
		execStart += " " + strings.Join(args, " ")
	}

	restart := svc.Restart
	if restart == "" {
		restart = "on-failure"
	}
	restartSec := svc.RestartSec
	if restartSec == 0 {
		restartSec = 5
	}
	logPath := substitute(svc.LogPath, values)
	if logPath == "" {
		logPath = fmt.Sprintf("var/logs/%s.log", svc.Name)
	}
	after := svc.After
	if len(after) == 0 {
		after = []string{"network-online.target"}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by emily ops render-unit from ops/playbook.toml (service %q) -- do not\n", svc.Name)
	fmt.Fprintf(&b, "# hand-edit, a re-render will overwrite this. See EMILY/docs/PARENACLOUD_OPS_NORTHSTAR.md.\n")
	if svc.Description != "" {
		fmt.Fprintf(&b, "#\n# %s\n", substitute(svc.Description, values))
	}
	b.WriteString("\n[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", substitute(svc.Description, values))
	fmt.Fprintf(&b, "After=%s\n", strings.Join(after, " "))
	if len(svc.Wants) > 0 {
		fmt.Fprintf(&b, "Wants=%s\n", strings.Join(svc.Wants, " "))
	}

	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", workingDir)

	// Environment keys sorted for deterministic, diffable output -- a map has no natural order
	// and re-rendering the identical playbook should never produce a spurious diff.
	envKeys := make([]string, 0, len(svc.Environment))
	for k := range svc.Environment {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		fmt.Fprintf(&b, "Environment=%s=%s\n", k, substitute(svc.Environment[k], values))
	}
	if svc.EnvironmentFile != nil {
		prefix := ""
		if !svc.EnvironmentFile.Required {
			prefix = "-" // the real, found-the-hard-way "optional" convention -- see ECOWAR's own incident
		}
		path := substitute(svc.EnvironmentFile.Path, values)
		fmt.Fprintf(&b, "EnvironmentFile=%s%s/%s\n", prefix, workingDir, path)
	}
	if svc.StartDelaySec > 0 {
		fmt.Fprintf(&b, "ExecStartPre=/bin/sleep %d\n", svc.StartDelaySec)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", execStart)
	fmt.Fprintf(&b, "Restart=%s\n", restart)
	fmt.Fprintf(&b, "RestartSec=%ds\n", restartSec)
	fmt.Fprintf(&b, "StandardOutput=append:%s/%s\n", workingDir, logPath)
	fmt.Fprintf(&b, "StandardError=append:%s/%s\n", workingDir, logPath)
	if svc.MemoryMax != "" {
		fmt.Fprintf(&b, "MemoryMax=%s\n", svc.MemoryMax)
	}

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	_ = unitName // real unit filename convention (<unitName>.service) is the caller's own concern when writing to disk
	return b.String(), nil
}
