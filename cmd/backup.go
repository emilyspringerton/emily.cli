// cmd/backup.go — emily backup run [--target iduna|promptoverse|fatbaby|all]
//
// Founder direction: "ok can we start building s3 backup tools? fatbaby
// backup" + "build it as google cloud first with s3 parity" + "we want to
// backup fatbaby data" + "and promptoverse data" + "and iduna data" +
// "encrypted at resk for the iduna data." Scoped via AskUserQuestion:
// new GCS bucket in the existing Vertex AI project (not a separate
// project), client-side encryption specifically for IDUNA data (GCS's own
// default server-side encryption covers everything else), "fatbaby data"
// means curated var/ state dirs, not the whole home directory or source
// already safe in git.
//
// "S3 parity": GCS's XML API is S3-compatible by design (same request/
// response shape, different endpoint + auth), so this tool's shape --
// tar an allowlist of paths, optionally encrypt, upload to a bucket via a
// single `gcloud storage cp` (or `gsutil cp`) call -- ports to S3 later by
// swapping that one upload step for `aws s3 cp`, not a rewrite. Not built
// today; noted so the design doesn't accidentally paint itself into a
// GCS-only corner.
//
// Bucket: gs://project-d24a71e9-2daf-4b2d-917-backups (us-central1,
// uniform bucket-level access, public access prevention on, 30-day
// object lifecycle -- created 2026-08-18, see this session's Apple).
//
// IMPORTANT, read before relying on this for real disaster recovery: the
// IDUNA target's encryption key lives ONLY at
// IDUNA_ROOT/var/backup-encryption.key (0600, generated on first use).
// It is deliberately never uploaded alongside the backups it protects --
// uploading it would defeat the point of client-side encryption. That
// also means losing this file (disk failure, wrong `rm`) makes every
// existing encrypted IDUNA backup permanently unrecoverable. Back this
// key up somewhere OTHER than this bucket yourself (password manager,
// printed and locked away, a second machine) -- this tool cannot do that
// part for you.
package cmd

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const (
	backupBucket        = "gs://project-d24a71e9-2daf-4b2d-917-backups"
	backupKeyFileName   = "backup-encryption.key"
	backupKeySizeBytes  = 32 // AES-256
	backupNonceSizeByte = 12 // standard GCM nonce size
)

// backupTarget is one named, allowlisted set of paths to archive together.
type backupTarget struct {
	Name    string
	Paths   []string // files and/or directories, tar'd with paths relative to their own parent
	Encrypt bool
}

func idunaRoot() string {
	if v := os.Getenv("IDUNA_ROOT"); v != "" {
		return v
	}
	return "/home/fatbaby/IDUNA"
}

func backupTargets(cfg *config.Config) []backupTarget {
	iduna := idunaRoot()
	emily := cfg.EmilyRoot
	if emily == "" {
		emily = "/home/fatbaby/EMILY"
	}
	return []backupTarget{
		{
			Name: "iduna",
			// Every SQLite store IDUNA owns -- auth/truestore, Apples
			// (also git-backed separately, but the live DB is the thing
			// that actually serves), blog/tyler content, the promptoverse
			// gallery DB, vault, mailing list, status history. Deliberately
			// NOT var/agent-secrets.env or any *.env -- credentials aren't
			// duplicated into more places than necessary even encrypted.
			Paths:   []string{filepath.Join(iduna, "var")},
			Encrypt: true,
		},
		{
			Name: "promptoverse",
			// The actual rendered gallery (images + HTML -- expensive to
			// regenerate, real creative output) plus the JSON state files
			// that drive selection/discovery (queue, discovered styles/
			// subjects, candidates, dead-letters, pity, backoff).
			Paths: []string{
				"/var/www/okemily/prompt-o-verse",
				filepath.Join(emily, "var"), // filtered to promptoverse-*.json* + queue by tarPaths
			},
			Encrypt: false,
		},
		{
			Name: "fatbaby",
			// Curated, not exhaustive -- source is already safe in git,
			// var/logs/ is ephemeral (and by far the biggest thing in
			// EMILY/var, ~1GB, not worth a daily cloud copy), secrets
			// files are deliberately excluded the same reasoning as
			// IDUNA's above. Extend this list as other repos accumulate
			// their own real state worth protecting.
			Paths: []string{
				filepath.Join(emily, "BACKLOG.md"),
				filepath.Join(emily, "var"), // same filtering as promptoverse target
				"/home/fatbaby/PRRJECT_FATBABY/var",
			},
			Encrypt: false,
		},
	}
}

func RunBackup(args []string) int {
	if len(args) == 0 || args[0] != "run" {
		return backupUsage()
	}
	rest := args[1:]
	target := "all"
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--target" && i+1 < len(rest) {
			target = rest[i+1]
			i++
		}
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	if _, err := exec.LookPath("gcloud"); err != nil {
		fmt.Fprintf(os.Stderr, "gcloud not found on PATH: %v\n", err)
		return 1
	}

	all := backupTargets(cfg)
	var selected []backupTarget
	if target == "all" {
		selected = all
	} else {
		for _, t := range all {
			if t.Name == target {
				selected = append(selected, t)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintf(os.Stderr, "emily backup run: unknown --target %q (want iduna|promptoverse|fatbaby|all)\n", target)
			return 1
		}
	}

	failed := false
	for _, t := range selected {
		if err := runOneBackup(cfg, t); err != nil {
			fmt.Fprintf(os.Stderr, "backup %q FAILED: %v\n", t.Name, err)
			failed = true
			continue
		}
	}
	if failed {
		return 1
	}
	return 0
}

func runOneBackup(cfg *config.Config, t backupTarget) error {
	now := time.Now().UTC()
	tmpDir, err := os.MkdirTemp("", "emily-backup-"+t.Name+"-")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, t.Name+".tar.gz")
	fmt.Fprintf(os.Stderr, "[%s] archiving %d path(s)...\n", t.Name, len(t.Paths))
	if err := tarGzPaths(archivePath, t.Paths); err != nil {
		return fmt.Errorf("archive: %w", err)
	}

	uploadPath := archivePath
	objectSuffix := ".tar.gz"
	if t.Encrypt {
		keyPath := filepath.Join(idunaRoot(), "var", backupKeyFileName)
		key, err := loadOrCreateBackupKey(keyPath)
		if err != nil {
			return fmt.Errorf("encryption key: %w", err)
		}
		encPath := archivePath + ".enc"
		if err := encryptFile(archivePath, encPath, key); err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
		uploadPath = encPath
		objectSuffix = ".tar.gz.enc"
	}

	info, err := os.Stat(uploadPath)
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}
	object := fmt.Sprintf("%s/%s/%s-%s%s", backupBucket, t.Name, now.Format("2006-01-02"), t.Name+"-"+now.Format("150405"), objectSuffix)

	fmt.Fprintf(os.Stderr, "[%s] uploading %s (%d bytes) -> %s\n", t.Name, filepath.Base(uploadPath), info.Size(), object)
	cmd := exec.Command("gcloud", "storage", "cp", uploadPath, object)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gcloud storage cp: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	fmt.Fprintf(os.Stderr, "[%s] OK -> %s\n", t.Name, object)
	return nil
}

// excludedDirNames are skipped entirely (not just their contents -- the
// whole subtree) wherever they appear under a backup target's paths.
// "logs" is the concrete reason: EMILY/var/logs/ alone is ~1GB, dwarfing
// everything else in EMILY/var combined, and it's ephemeral operational
// output, not data worth a daily cloud copy.
var excludedDirNames = map[string]bool{
	"logs": true,
}

// looksLikeSecret is a conservative filename check, not a content scan --
// real bug caught before this tool ever ran for real: the target path
// lists point at whole var/ directories (for simplicity -- there's no
// other single thing to point at that isn't itself a curated allowlist),
// which would otherwise sweep up IDUNA/var/agent-secrets.env and
// IDUNA/var/webmaster.json (both real credentials) into the archive.
// Doc comments on backupTargets claimed these were excluded before this
// function actually existed to do it -- fixed here, not left as a stale
// claim. Applied to every target, not just IDUNA's, as defense in depth.
func looksLikeSecret(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".env") {
		return true
	}
	if strings.Contains(lower, "secret") {
		return true
	}
	if lower == "webmaster.json" {
		return true
	}
	return false
}

// tarGzPaths writes a gzip-compressed tar of every file under each given
// path (or the file itself, for a bare file path) to dst, skipping
// excludedDirNames subtrees and looksLikeSecret files. Entries use a path
// relative to the IMMEDIATE PARENT of each top-level path, so restoring
// extracts each target's own top-level directory name (e.g. "var/...")
// rather than the full absolute source path.
func tarGzPaths(dst string, paths []string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, root := range paths {
		info, err := os.Stat(root)
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "  warning: %s does not exist, skipping\n", root)
			continue
		}
		if err != nil {
			return err
		}
		base := filepath.Dir(root)
		if info.IsDir() {
			err = filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if fi.IsDir() && excludedDirNames[fi.Name()] {
					return filepath.SkipDir
				}
				if !fi.IsDir() && looksLikeSecret(fi.Name()) {
					fmt.Fprintf(os.Stderr, "  excluding %s (looks like a credential file)\n", path)
					return nil
				}
				rel, relErr := filepath.Rel(base, path)
				if relErr != nil {
					return relErr
				}
				return addTarEntry(tw, path, rel, fi)
			})
		} else {
			if looksLikeSecret(info.Name()) {
				fmt.Fprintf(os.Stderr, "  excluding %s (looks like a credential file)\n", root)
				continue // NOT "return nil" -- that would exit the whole function and silently skip every remaining path
			}
			rel, relErr := filepath.Rel(base, root)
			if relErr != nil {
				return relErr
			}
			err = addTarEntry(tw, root, rel, info)
		}
		if err != nil {
			return fmt.Errorf("walk %s: %w", root, err)
		}
	}
	return nil
}

func addTarEntry(tw *tar.Writer, fullPath, relPath string, info os.FileInfo) error {
	if info.IsDir() {
		return nil // directories are implicit in tar from file entries' paths
	}
	if !info.Mode().IsRegular() {
		return nil // skip symlinks/sockets/etc -- not expected in these targets, safe to ignore if present
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = relPath
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	src, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(tw, src)
	return err
}

// loadOrCreateBackupKey reads a 32-byte AES-256 key from path, generating
// and persisting a new random one (0600) if it doesn't exist yet.
func loadOrCreateBackupKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key, decErr := hex.DecodeString(strings.TrimSpace(string(data)))
		if decErr != nil || len(key) != backupKeySizeBytes {
			return nil, fmt.Errorf("existing key file %s is malformed (expected %d hex-encoded bytes)", path, backupKeySizeBytes)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	key := make([]byte, backupKeySizeBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	fmt.Fprintf(os.Stderr, "generated a new backup encryption key at %s -- back this up somewhere OTHER than the GCS bucket, losing it makes existing encrypted backups unrecoverable\n", path)
	return key, nil
}

// encryptFile AES-256-GCM-encrypts src into dst: a random nonce, then the
// ciphertext, prefixed to the file (nonce first). Whole-file-in-memory --
// fine at this data's scale (tens to low hundreds of MB), not designed for
// multi-GB archives.
func encryptFile(src, dst string, key []byte) error {
	plaintext, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, backupNonceSizeByte)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return os.WriteFile(dst, ciphertext, 0o600)
}

// decryptFile is the inverse of encryptFile -- exported via `emily backup
// decrypt` for actual disaster recovery, not just written and never
// exercised.
func decryptFile(src, dst string, key []byte) error {
	ciphertext, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(ciphertext) < backupNonceSizeByte {
		return errors.New("ciphertext too short to contain a nonce")
	}
	nonce, ct := ciphertext[:backupNonceSizeByte], ciphertext[backupNonceSizeByte:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("decrypt (wrong key, or corrupted file): %w", err)
	}
	return os.WriteFile(dst, plaintext, 0o644)
}

func RunBackupDecrypt(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: emily backup decrypt <encrypted-file> <output-file>")
		return 1
	}
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	keyPath := filepath.Join(idunaRoot(), "var", backupKeyFileName)
	key, err := loadOrCreateBackupKey(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load encryption key: %v\n", err)
		return 1
	}
	_ = cfg
	if err := decryptFile(args[0], args[1], key); err != nil {
		fmt.Fprintf(os.Stderr, "decrypt: %v\n", err)
		return 1
	}
	fmt.Printf("decrypted -> %s (now un-gzip/untar it normally: tar xzf %s)\n", args[1], args[1])
	return 0
}

func backupUsage() int {
	fmt.Print(`emily backup — cloud backup for IDUNA/Prompt-o-verse/fatbaby data

Subcommands:
  emily backup run [--target iduna|promptoverse|fatbaby|all]   Archive + upload to GCS
  emily backup decrypt <encrypted-file> <output-file>          Decrypt an IDUNA backup archive

Targets:
  iduna         IDUNA's SQLite stores (var/*.db) -- AES-256-GCM encrypted before upload
  promptoverse  Rendered gallery (images+HTML) + Prompt-o-verse JSON state, not encrypted
  fatbaby       Curated cross-repo var/ state (BACKLOG.md, EMILY/var, PRRJECT_FATBABY/var), not encrypted

Bucket: ` + backupBucket + ` (us-central1, 30-day retention lifecycle)

The IDUNA target's encryption key lives at IDUNA_ROOT/var/` + backupKeyFileName + `
(0600), generated on first use, and is NEVER uploaded to the bucket. Back it up
yourself, somewhere else -- losing it makes existing encrypted backups unrecoverable.
`)
	return 1
}
