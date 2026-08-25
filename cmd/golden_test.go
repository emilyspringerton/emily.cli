package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func writeTestGoldenConfig(t *testing.T, repos []goldenRepo) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "golden-repos.json")
	c := goldenRepoConfig{Repos: repos}
	if err := saveGoldenRepoConfig(path, &c); err != nil {
		t.Fatalf("saveGoldenRepoConfig: %v", err)
	}
	return path
}

func TestLoadGoldenRepoConfig_RoundTrip(t *testing.T) {
	path := writeTestGoldenConfig(t, []goldenRepo{
		{Name: "EMILY", GitHub: "emilyspringerton/EMILY", Enabled: true},
		{Name: "REDGARDEN", GitHub: "emilyspringerton/REDGARDEN", Enabled: false},
	})
	c, err := loadGoldenRepoConfig(path)
	if err != nil {
		t.Fatalf("loadGoldenRepoConfig: %v", err)
	}
	if len(c.Repos) != 2 {
		t.Fatalf("want 2 repos, got %d", len(c.Repos))
	}
	if c.Repos[0].Name != "EMILY" || !c.Repos[0].Enabled {
		t.Errorf("repo[0] = %+v, want EMILY enabled", c.Repos[0])
	}
	if c.Repos[1].Name != "REDGARDEN" || c.Repos[1].Enabled {
		t.Errorf("repo[1] = %+v, want REDGARDEN disabled", c.Repos[1])
	}
}

func TestRunGoldenToggle_EnableAndDisable(t *testing.T) {
	path := writeTestGoldenConfig(t, []goldenRepo{
		{Name: "REDGARDEN", GitHub: "emilyspringerton/REDGARDEN", Enabled: false},
	})

	if code := runGoldenToggle(path, []string{"REDGARDEN"}, true); code != 0 {
		t.Fatalf("enable: exit code %d", code)
	}
	c, _ := loadGoldenRepoConfig(path)
	if !c.Repos[0].Enabled {
		t.Error("expected REDGARDEN enabled after toggle")
	}

	if code := runGoldenToggle(path, []string{"redgarden"}, false); code != 0 { // case-insensitive match
		t.Fatalf("disable: exit code %d", code)
	}
	c, _ = loadGoldenRepoConfig(path)
	if c.Repos[0].Enabled {
		t.Error("expected REDGARDEN disabled after toggle")
	}
}

func TestRunGoldenToggle_UnknownRepoFails(t *testing.T) {
	path := writeTestGoldenConfig(t, []goldenRepo{
		{Name: "EMILY", GitHub: "emilyspringerton/EMILY", Enabled: true},
	})
	if code := runGoldenToggle(path, []string{"NOT_A_REAL_REPO"}, true); code == 0 {
		t.Error("expected non-zero exit for an unknown repo name")
	}
}

func TestFetchLatestRun_ParsesRealShapedResponse(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ghRunsResponse{WorkflowRuns: []ghRun{
			{Status: "completed", Conclusion: "success", HeadSHA: "abc1234567", Name: "CI", HTMLURL: "https://example.com/run/1"},
		}})
	}))
	defer srv.Close()

	run, err := fetchLatestRun(srv.Client(), srv.URL, "emilyspringerton/EMILY", "test-token")
	if err != nil {
		t.Fatalf("fetchLatestRun: %v", err)
	}
	if run == nil || run.Conclusion != "success" || run.HeadSHA != "abc1234567" {
		t.Errorf("unexpected run: %+v", run)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("expected Authorization header to carry the token, got %q", gotAuth)
	}
	if gotPath != "/repos/emilyspringerton/EMILY/actions/runs" {
		t.Errorf("unexpected request path: %q", gotPath)
	}
}

func TestFetchLatestRun_NoRunsReturnsNilNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ghRunsResponse{})
	}))
	defer srv.Close()

	run, err := fetchLatestRun(srv.Client(), srv.URL, "emilyspringerton/EMPTY", "")
	if err != nil {
		t.Fatalf("fetchLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("expected nil run for an empty workflow_runs list, got %+v", run)
	}
}

func TestFetchLatestRun_NonOKStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fetchLatestRun(srv.Client(), srv.URL, "emilyspringerton/PRIVATE", "")
	if err == nil {
		t.Fatal("expected an error for a non-200 response, got nil")
	}
}

func TestFetchLatestRun_NoTokenOmitsAuthHeader(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") != ""
		json.NewEncoder(w).Encode(ghRunsResponse{})
	}))
	defer srv.Close()

	if _, err := fetchLatestRun(srv.Client(), srv.URL, "emilyspringerton/EMILY", ""); err != nil {
		t.Fatalf("fetchLatestRun: %v", err)
	}
	if sawAuth {
		t.Error("expected no Authorization header when no token is configured (unauthenticated public API path)")
	}
}

func TestRunGoldenStatus_NoEnabledReposIsFine(t *testing.T) {
	path := writeTestGoldenConfig(t, []goldenRepo{
		{Name: "REDGARDEN", GitHub: "emilyspringerton/REDGARDEN", Enabled: false},
	})
	// No enabled repos means no network calls at all -- must not hang or fail.
	if code := runGoldenStatus(path); code != 0 {
		t.Errorf("expected exit 0 with nothing enabled, got %d", code)
	}
}

func TestGoldenRepoConfigPath_UnderEmilyContext(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("EMILY_ROOT", dir)
	defer os.Unsetenv("EMILY_ROOT")

	cfg, err := config.Resolve()
	if err != nil {
		t.Fatalf("config.Resolve: %v", err)
	}
	got := goldenRepoConfigPath(cfg)
	want := filepath.Join(dir, "context", "golden-repos.json")
	if got != want {
		t.Errorf("goldenRepoConfigPath = %q, want %q (sibling of golden-docs-index.md)", got, want)
	}
}
