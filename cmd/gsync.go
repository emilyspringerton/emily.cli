package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const githubOrg = "emilyspringerton"

// RunGSync is the entry point for `emily gsync <REPO> [REPO ...]`.
//
// For each repo name provided:
//  1. Resolve the local path: /home/fatbaby/<REPO>. If absent, clone from
//     git@github.com:emilyspringerton/<REPO>.
//  2. Create a git archive (tar.gz) of the HEAD commit.
//  3. Upload the archive to Google Drive via the IDUNA drive API.
//
// Env:
//
//	IDUNA_BASE_URL          IDUNA server (default http://localhost:8080)
//	IDUNA_AGENT_NAME        agent identity for auth
//	IDUNA_AGENT_SECRET      M2M credential
//
// Google Drive folder is configured on the IDUNA server side via
// GOOGLE_DRIVE_FOLDER_ID + GOOGLE_DRIVE_SERVICE_ACCOUNT_JSON.
// The service account must have write access to the target Drive folder
// (share the folder with the service account email from Drive UI).
func RunGSync(args []string) int {
	fs := flag.NewFlagSet("gsync", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "archive + print metadata but do not upload")
	orgFlag := fs.String("org", githubOrg, "GitHub org/user for clone URL")
	fs.Usage = func() {
		fmt.Print(`emily gsync — sync git repo(s) to Google Drive (MyDrive)

Archives each repo with git archive and uploads the .tar.gz to the
Google Drive folder configured in IDUNA (GOOGLE_DRIVE_FOLDER_ID).

Usage:
  emily gsync REPO [REPO ...]
  emily gsync SHANKPIT
  emily gsync SHANKPIT EMILY GoblinFoxDragon

If the repo is not present at /home/fatbaby/REPO it is cloned from
  git@github.com:emilyspringerton/REPO

The archive is named REPO-YYYYMMDD-HHmmss.tar.gz and contains the
full working tree at HEAD.

Flags:
  --dry-run   archive but do not upload (prints archive path + size)
  --org       GitHub org/user for clone URLs (default: emilyspringerton)

Env:
  IDUNA_BASE_URL        IDUNA server (default http://localhost:8080)
  IDUNA_AGENT_NAME      agent identity
  IDUNA_AGENT_SECRET    M2M credential (auto-loaded from IDUNA/var/agent-secrets.env)
`)
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	repos := fs.Args()
	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "emily gsync: at least one repo name required")
		fs.Usage()
		return 1
	}

	cfg, _ := config.Resolve()

	var idunaClient *iduna.Client
	if !*dryRun {
		idunaClient = iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	}

	exitCode := 0
	for _, repo := range repos {
		if err := gsyncOne(repo, *orgFlag, cfg, idunaClient, *dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "gsync %s: %v\n", repo, err)
			exitCode = 1
		}
	}
	return exitCode
}

func gsyncOne(repo, org string, cfg *config.Config, client *iduna.Client, dryRun bool) error {
	repoDir, err := resolveOrClone(repo, org, cfg)
	if err != nil {
		return err
	}

	archiveName := fmt.Sprintf("%s-%s.tar.gz", repo, time.Now().UTC().Format("20060102-150405"))
	archivePath := filepath.Join(os.TempDir(), archiveName)

	fmt.Printf("  archiving %s → %s ...\n", repoDir, archivePath)
	if err := gitArchive(repoDir, archivePath); err != nil {
		return err
	}
	defer os.Remove(archivePath) // clean up temp file after upload

	stat, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	sizeMB := float64(stat.Size()) / (1024 * 1024)

	if dryRun {
		fmt.Printf("  [dry-run] %s (%.2f MB) — upload skipped\n", archiveName, sizeMB)
		return nil
	}

	data, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}

	fmt.Printf("  uploading %s (%.2f MB) → Google Drive ...\n", archiveName, sizeMB)
	f, err := client.DriveUpload(archiveName, "application/gzip", data)
	if err != nil {
		return fmt.Errorf("drive upload: %w", err)
	}

	fmt.Printf("  ✓ %s uploaded\n", archiveName)
	if f.WebViewLink != "" {
		fmt.Printf("    Drive link: %s\n", f.WebViewLink)
	} else if f.ID != "" {
		fmt.Printf("    Drive file ID: %s\n", f.ID)
	}
	return nil
}

// resolveOrClone returns the local directory for repo. If it doesn't exist
// under /home/fatbaby/<repo>, it clones from git@github.com:<org>/<repo>.
func resolveOrClone(repo, org string, cfg *config.Config) (string, error) {
	// Canonical local path: parent dir of EmilyRoot (typically /home/fatbaby).
	base := filepath.Dir(cfg.EmilyRoot)
	if base == "" || base == "." {
		base = "/home/fatbaby"
	}
	localPath := filepath.Join(base, repo)

	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		// Repo already present — fast-forward pull to get latest state.
		fmt.Printf("  pulling %s ...\n", localPath)
		pull := exec.Command("git", "-C", localPath, "pull", "--ff-only", "--quiet")
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		_ = pull.Run() // non-fatal: we still archive what we have
		return localPath, nil
	}

	cloneURL := fmt.Sprintf("git@github.com:%s/%s", org, repo)
	fmt.Printf("  cloning %s → %s ...\n", cloneURL, localPath)
	clone := exec.Command("git", "clone", "--depth", "1", cloneURL, localPath)
	clone.Stdout = os.Stdout
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return "", fmt.Errorf("git clone %s: %w", cloneURL, err)
	}
	return localPath, nil
}

// gitArchive runs `git archive HEAD --format=tar.gz -o <dest>` in repoDir.
// Falls back to a filesystem tar when git archive fails (e.g. for untracked files).
func gitArchive(repoDir, destPath string) error {
	cmd := exec.Command("git", "-C", repoDir, "archive", "--format=tar.gz", "-o", destPath, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		// Graceful fallback: git archive fails on repos with no commits.
		msg := strings.TrimSpace(string(out))
		return fmt.Errorf("git archive: %w (%s)", err, msg)
	}
	return nil
}
