// cmd/backlog_test.go

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObsTimestamp_stripsExtension(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"2026-06-11T00-54-39Z.json", "2026-06-11T00-54-39Z"},
		{"2026-06-11T00-00-18Z.json", "2026-06-11T00-00-18Z"},
		{"20260611T005439Z.json", "20260611T005439Z"},
	}
	for _, tc := range cases {
		got := obsTimestamp(tc.file)
		if got != tc.want {
			t.Errorf("obsTimestamp(%q) = %q, want %q", tc.file, got, tc.want)
		}
	}
}

func TestLoadCurated_empty(t *testing.T) {
	m := loadCurated("/nonexistent/path.txt")
	if len(m) != 0 {
		t.Errorf("expected empty map for missing file, got %v", m)
	}
}

func TestLoadCurated_roundtrip(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "curated.txt")

	results := []curatedResult{
		{FileKey: "2026-06-11T00-54-39Z", Timestamp: "2026-06-11T00:54:39Z", Summary: "test one"},
		{FileKey: "2026-06-11T00-52-56Z", Timestamp: "2026-06-11T00:52:56Z", Summary: "test two"},
	}
	if err := markCurated(sf, results); err != nil {
		t.Fatal(err)
	}

	m := loadCurated(sf)
	if _, ok := m["2026-06-11T00-54-39Z"]; !ok {
		t.Error("expected first file key to be in curated set")
	}
	if _, ok := m["2026-06-11T00-52-56Z"]; !ok {
		t.Error("expected second file key to be in curated set")
	}
	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m))
	}
}

func TestAppendToBacklog_createsIntakeSection(t *testing.T) {
	dir := t.TempDir()
	backlog := filepath.Join(dir, "BACKLOG.md")

	// Write a minimal BACKLOG.md with a BACKLOG PROTOCOL section.
	initial := "# GOLDEN BACKLOG\n\n## BACKLOG PROTOCOL\n\nDo things.\n"
	if err := os.WriteFile(backlog, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	results := []curatedResult{
		{Timestamp: "2026-06-11T00:54:39Z", BacklogLine: "- [ ] **test obs** — obs `2026-06-11T00:54:39Z`. CURATED: 2026-06-11."},
	}
	if err := appendToBacklog(backlog, results); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(backlog)
	content := string(data)
	if !strings.Contains(content, "## INTAKE QUEUE") {
		t.Error("expected INTAKE QUEUE section to be created")
	}
	if !strings.Contains(content, "test obs") {
		t.Error("expected backlog line to be present")
	}
	// BACKLOG PROTOCOL must still be present.
	if !strings.Contains(content, "## BACKLOG PROTOCOL") {
		t.Error("BACKLOG PROTOCOL section should be preserved")
	}
}

func TestAppendToBacklog_appendsToExistingIntakeSection(t *testing.T) {
	dir := t.TempDir()
	backlog := filepath.Join(dir, "BACKLOG.md")

	initial := "# BACKLOG\n\n## INTAKE QUEUE (curated by emily backlog curate)\n\nItems here.\n\n- [ ] **old item** — obs `2026-01-01T00:00:00Z`. CURATED: 2026-01-01.\n\n## BACKLOG PROTOCOL\n\nDone.\n"
	if err := os.WriteFile(backlog, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	results := []curatedResult{
		{Timestamp: "2026-06-11T00:54:39Z", BacklogLine: "- [ ] **new item** — obs `2026-06-11T00:54:39Z`. CURATED: 2026-06-11."},
	}
	if err := appendToBacklog(backlog, results); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(backlog)
	content := string(data)
	if !strings.Contains(content, "old item") {
		t.Error("expected old item to be preserved")
	}
	if !strings.Contains(content, "new item") {
		t.Error("expected new item to be appended")
	}
	// New item should come before BACKLOG PROTOCOL.
	newPos := strings.Index(content, "new item")
	protPos := strings.Index(content, "## BACKLOG PROTOCOL")
	if newPos > protPos {
		t.Error("new item should appear before BACKLOG PROTOCOL")
	}
}

func TestListObsFiles_skipsLatestAndHidden(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"2026-06-11T00-54-39Z.json", "latest.json", ".last-processed", "2026-06-10T23-29-39Z.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listObsFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == "latest.json" || strings.HasPrefix(f, ".") {
			t.Errorf("listObsFiles should skip %q", f)
		}
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(files), files)
	}
}

func TestSanitizeBacklogLine(t *testing.T) {
	s := sanitizeBacklogLine("test **bold** in summary")
	if strings.Contains(s, "**") {
		t.Errorf("expected ** to be escaped, got %q", s)
	}
}
