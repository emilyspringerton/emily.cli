package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RunShankpit is the entry point for `emily shankpit <subcommand>`.
//
// Subcommands:
//
//	status              — print server status (players, scenes, uptime)
//	players             — list connected players
//	kick <id>           — disconnect player by ID
//	observe             — file an Emily observation from current server state
//	restart             — SIGTERM + restart the Go server process
//
// Flags:
//
//	--admin-url   admin HTTP base URL (default http://localhost:6970)
//	--token       Bearer token for write operations
func RunShankpit(args []string) int {
	fs := flag.NewFlagSet("shankpit", flag.ContinueOnError)
	defaultAdminURL := os.Getenv("SHANKPIT_ADMIN_URL")
	if defaultAdminURL == "" {
		defaultAdminURL = "http://localhost:6970"
	}
	adminURL := fs.String("admin-url", defaultAdminURL, "SHANKPIT admin HTTP URL")
	token := fs.String("token", os.Getenv("SHANKPIT_ADMIN_TOKEN"), "Bearer token for write ops")
	fs.Usage = func() {
		fmt.Print(`emily shankpit — SHANKPIT server admin + observability

Subcommands:
  status              current server state (players, scenes, uptime)
  players             list all connected players with position
  kick <id>           disconnect player by client ID
  observe             file an Emily observation from server state
  restart             graceful server restart (SIGTERM + relaunch)

Flags:
  --admin-url   admin HTTP base URL (default http://localhost:6970)
  --token       Bearer token for write ops (or SHANKPIT_ADMIN_TOKEN env)
`)
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	sub := fs.Arg(0)
	rest := fs.Args()

	c := &shankpitClient{baseURL: strings.TrimRight(*adminURL, "/"), token: *token}

	switch sub {
	case "status", "":
		return c.runStatus()
	case "players":
		return c.runPlayers()
	case "kick":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: emily shankpit kick <player-id>")
			return 1
		}
		return c.runKick(rest[1])
	case "observe":
		return c.runObserve()
	case "restart":
		return runShankpitRestart()
	default:
		fmt.Fprintf(os.Stderr, "emily shankpit: unknown subcommand %q\n", sub)
		fs.Usage()
		return 1
	}
}

// shankpitClient talks to the admin HTTP API.
type shankpitClient struct {
	baseURL string
	token   string
	hc      *http.Client
}

func (c *shankpitClient) client() *http.Client {
	if c.hc == nil {
		c.hc = &http.Client{Timeout: 5 * time.Second}
	}
	return c.hc
}

func (c *shankpitClient) get(path string, out interface{}) error {
	resp, err := c.client().Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("connect to SHANKPIT admin: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API %s: %d %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (c *shankpitClient) post(path string) ([]byte, int, error) {
	req, _ := http.NewRequest("POST", c.baseURL+path, nil)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("connect to SHANKPIT admin: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

type adminStatus struct {
	Status    string         `json:"status"`
	Uptime    string         `json:"uptime"`
	UptimeSec int            `json:"uptime_sec"`
	Players   int            `json:"players"`
	Scenes    map[string]int `json:"scenes"`
	ServerAt  string         `json:"server_at"`
	AdminAt   string         `json:"admin_at"`
}

type adminPlayer struct {
	ID      uint8   `json:"id"`
	Addr    string  `json:"addr"`
	SceneID int     `json:"scene_id"`
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	Z       float32 `json:"z"`
	Yaw     float32 `json:"yaw"`
}

func (c *shankpitClient) runStatus() int {
	var s adminStatus
	if err := c.get("/admin/status", &s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("SHANKPIT server — %s\n", s.Status)
	fmt.Printf("  Uptime:   %s\n", s.Uptime)
	fmt.Printf("  UDP:      %s   Admin: %s\n", s.ServerAt, s.AdminAt)
	fmt.Printf("  Players:  %d\n", s.Players)
	if len(s.Scenes) > 0 {
		fmt.Printf("  Scenes:   ")
		for sceneID, count := range s.Scenes {
			fmt.Printf("scene%s=%d  ", sceneID, count)
		}
		fmt.Println()
	}
	return 0
}

func (c *shankpitClient) runPlayers() int {
	var players []adminPlayer
	if err := c.get("/admin/players", &players); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(players) == 0 {
		fmt.Println("No players connected.")
		return 0
	}
	fmt.Printf("%-4s  %-21s  %-7s  %-12s  %s\n", "ID", "Address", "Scene", "Position", "Yaw")
	fmt.Println(strings.Repeat("─", 64))
	for _, p := range players {
		pos := fmt.Sprintf("%.1f,%.1f,%.1f", p.X, p.Y, p.Z)
		fmt.Printf("%-4d  %-21s  %-7d  %-12s  %.1f°\n", p.ID, p.Addr, p.SceneID, pos, p.Yaw)
	}
	return 0
}

func (c *shankpitClient) runKick(idStr string) int {
	if _, err := strconv.Atoi(idStr); err != nil {
		fmt.Fprintf(os.Stderr, "kick: invalid player ID %q\n", idStr)
		return 1
	}
	body, code, err := c.post("/admin/kick?id=" + idStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if code != http.StatusOK {
		fmt.Fprintf(os.Stderr, "kick failed (%d): %s\n", code, string(body))
		return 1
	}
	fmt.Printf("Kicked player %s.\n", idStr)
	return 0
}

func (c *shankpitClient) runObserve() int {
	var s adminStatus
	if err := c.get("/admin/status", &s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var players []adminPlayer
	_ = c.get("/admin/players", &players)

	summary := fmt.Sprintf(
		"SHANKPIT server: %s, %d player(s) online, uptime %s",
		s.Status, s.Players, s.Uptime,
	)
	if len(players) > 0 {
		pids := make([]string, 0, len(players))
		for _, p := range players {
			pids = append(pids, fmt.Sprintf("id=%d scene=%d", p.ID, p.SceneID))
		}
		summary += " [" + strings.Join(pids, ", ") + "]"
	}

	cmd := exec.Command("emily", "observe", "-s", "info", summary)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "observe: %v\n", err)
		return 1
	}
	return 0
}

func runShankpitRestart() int {
	fmt.Println("Sending SIGTERM to shank_go_server...")
	kill := exec.Command("pkill", "-TERM", "-f", "shank_go_server")
	_ = kill.Run() // ignore if not running

	time.Sleep(1 * time.Second)

	// Delegate to emily start --shankpit for a clean relaunch.
	cmd := exec.Command("emily", "start", "--shankpit")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "restart: %v\n", err)
		return 1
	}
	fmt.Println("SHANKPIT server relaunching.")
	return 0
}
