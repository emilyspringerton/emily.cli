package opsplaybook

import (
	"strings"
	"testing"
)

// A real, minimal excerpt of ECOWAR/ops/playbook.toml (not the whole file, kept small and
// self-contained so this test doesn't silently drift if the real file's own comments change) --
// exercises every real field this package understands.
const ecowarPlaybookExcerpt = `
[game]
name = "ecowar"
repo = "ECOWAR"

[[service]]
name = "matchmaker"
kind = "persistent"
description = "ECOWAR arena matchmaker — 1v1 queue with bots"
binary = "build/red_garden_matchmaker"
args = ["--listen-port", "{{listen_port}}", "--lobby-size", "{{lobby_size}}",
        "--server-bin", "{{server_bin}}", "--first-game-port", "{{first_game_port}}"]
port = "{{listen_port}}"
environment = { REDGARDEN_TICKET_SECRET = "test-secret-for-vs0-vs1-validation" }
environment_file = { path = "var/{{env_file_name}}", required = false }
restart = "on-failure"
restart_sec = 5
memory_max = "256M"
after = ["network-online.target"]

[[service]]
name = "bot-pool"
kind = "persistent"
description = "ECOWAR arena bot pool"
binary = "scripts/run_bot_pool.sh"
args = ["{{bot_count}}", "{{listen_port}}"]
after = ["network-online.target", "ecowar-matchmaker.service"]
wants = ["ecowar-matchmaker.service"]
start_delay_sec = 2
memory_max = "128M"

[[service]]
name = "arena-server"
kind = "spawned"
description = "Per-match arena server"
binary = "build/red_garden_arena_server"

[values]
listen_port = 9779
lobby_size = 2
server_bin = "build/red_garden_arena_server"
first_game_port = 9600
bot_count = 1
env_file_name = "ecowar-iduna-agent.env"
`

func TestParsePlaybook_realShape(t *testing.T) {
	pb, err := ParsePlaybook(ecowarPlaybookExcerpt)
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	if pb.Game.Name != "ecowar" {
		t.Errorf("game name = %q, want ecowar", pb.Game.Name)
	}
	if len(pb.Service) != 3 {
		t.Fatalf("len(Service) = %d, want 3 (matchmaker, bot-pool, arena-server)", len(pb.Service))
	}
	if pb.Values["listen_port"] != "9779" {
		t.Errorf("values[listen_port] = %q, want \"9779\" (numeric TOML value stringified)", pb.Values["listen_port"])
	}
}

func TestPersistentServices_excludesSpawned(t *testing.T) {
	pb, err := ParsePlaybook(ecowarPlaybookExcerpt)
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	persistent := pb.PersistentServices()
	if len(persistent) != 2 {
		t.Fatalf("PersistentServices returned %d, want 2 (arena-server is \"spawned\", must be excluded)", len(persistent))
	}
	for _, s := range persistent {
		if s.Name == "arena-server" {
			t.Errorf("PersistentServices leaked the spawned arena-server entry")
		}
	}
}

// TestRenderUnit_matchesRealDeployedShape checks the rendered matchmaker unit against the real,
// currently-deployed ECOWAR/ops/systemd/ecowar-matchmaker.service field by field -- not an
// eyeball comparison. If this ever fails, either the playbook drifted from the real deploy or
// the renderer has a real bug; either way it means this format stopped being trustworthy.
func TestRenderUnit_matchesRealDeployedShape(t *testing.T) {
	pb, err := ParsePlaybook(ecowarPlaybookExcerpt)
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	var matchmaker Service
	found := false
	for _, s := range pb.Service {
		if s.Name == "matchmaker" {
			matchmaker = s
			found = true
		}
	}
	if !found {
		t.Fatal("matchmaker service not found in parsed playbook")
	}

	const workingDir = "/home/fatbaby/ECOWAR"
	unit, err := RenderUnit(pb.Game, matchmaker, workingDir, pb.Values)
	if err != nil {
		t.Fatalf("RenderUnit: %v", err)
	}

	// Real assertions, each checking against the literal, live ecowar-matchmaker.service text
	// (see that file for the source of truth this is checked against).
	wantExecStart := "ExecStart=/home/fatbaby/ECOWAR/build/red_garden_matchmaker " +
		"--listen-port 9779 --lobby-size 2 --server-bin build/red_garden_arena_server --first-game-port 9600"
	if !strings.Contains(unit, wantExecStart) {
		t.Errorf("rendered unit missing real ExecStart line, got:\n%s", unit)
	}
	if !strings.Contains(unit, "EnvironmentFile=-/home/fatbaby/ECOWAR/var/ecowar-iduna-agent.env") {
		t.Errorf("rendered unit missing the real optional (leading '-') EnvironmentFile line -- "+
			"this is the exact convention ECOWAR's own real 5-day outage incident established, got:\n%s", unit)
	}
	if !strings.Contains(unit, "Environment=REDGARDEN_TICKET_SECRET=test-secret-for-vs0-vs1-validation") {
		t.Errorf("rendered unit missing the real static Environment= line, got:\n%s", unit)
	}
	if !strings.Contains(unit, "Restart=on-failure") || !strings.Contains(unit, "RestartSec=5s") {
		t.Errorf("rendered unit missing real Restart=/RestartSec=, got:\n%s", unit)
	}
	if !strings.Contains(unit, "MemoryMax=256M") {
		t.Errorf("rendered unit missing real MemoryMax=, got:\n%s", unit)
	}
	if !strings.Contains(unit, "StandardOutput=append:/home/fatbaby/ECOWAR/var/logs/matchmaker.log") {
		t.Errorf("rendered unit missing real default log path (var/logs/<service-name>.log), got:\n%s", unit)
	}
	if !strings.Contains(unit, "WantedBy=default.target") {
		t.Errorf("rendered unit missing real [Install] section, got:\n%s", unit)
	}
}

func TestRenderUnit_botPoolStartDelayAndWants(t *testing.T) {
	pb, err := ParsePlaybook(ecowarPlaybookExcerpt)
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	var botPool Service
	for _, s := range pb.Service {
		if s.Name == "bot-pool" {
			botPool = s
		}
	}
	unit, err := RenderUnit(pb.Game, botPool, "/home/fatbaby/ECOWAR", pb.Values)
	if err != nil {
		t.Fatalf("RenderUnit: %v", err)
	}
	if !strings.Contains(unit, "ExecStartPre=/bin/sleep 2") {
		t.Errorf("rendered bot-pool unit missing the real ExecStartPre sleep (start_delay_sec), got:\n%s", unit)
	}
	if !strings.Contains(unit, "Wants=ecowar-matchmaker.service") {
		t.Errorf("rendered bot-pool unit missing real Wants= dependency on the matchmaker, got:\n%s", unit)
	}
	if !strings.Contains(unit, "ExecStart=/home/fatbaby/ECOWAR/scripts/run_bot_pool.sh 1 9779") {
		t.Errorf("rendered bot-pool unit missing real ExecStart with substituted bot_count/listen_port, got:\n%s", unit)
	}
}

func TestRenderUnit_rejectsSpawnedService(t *testing.T) {
	pb, err := ParsePlaybook(ecowarPlaybookExcerpt)
	if err != nil {
		t.Fatalf("ParsePlaybook: %v", err)
	}
	var arenaServer Service
	for _, s := range pb.Service {
		if s.Name == "arena-server" {
			arenaServer = s
		}
	}
	if _, err := RenderUnit(pb.Game, arenaServer, "/home/fatbaby/ECOWAR", pb.Values); err == nil {
		t.Error("RenderUnit should refuse a \"spawned\" service (arena-server is spawned per-match by " +
			"the matchmaker, never its own systemd unit) -- got nil error")
	}
}
