package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func TestLooksLikeSecret(t *testing.T) {
	cases := map[string]bool{
		"agent-secrets.env":         true,
		"secrets.env":               true,
		"webmaster.json":            true,
		"AGENT-SECRETS.ENV":         true, // case-insensitive
		"moltbook-credentials.json": true, // gfd target (2026-09-12) -- real creds, missed the pre-existing checks
		"MOLTBOOK-CREDENTIALS.JSON": true, // case-insensitive
		"truestore.db":              false,
		"promptoverse.db":           false,
		"BACKLOG.md":                false,
		"mud-chars.json":            false,
	}
	for name, want := range cases {
		if got := looksLikeSecret(name); got != want {
			t.Errorf("looksLikeSecret(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestBackupTargets_GFDTargetPointsAtGFDVarAndIsFiltered guards two things at once (2026-09-12,
// SSH_TRANSPORT_IDENTITY_SPEC.md §5 / Stage 3's own "data path is backed up" requirement): the
// "gfd" target exists and points at the real data path (GoblinFoxDragon/var, not the whole repo
// -- source is already safe in git), and it is NOT the encrypted target (matching fatbaby/
// promptoverse, not iduna -- the one real credential file in that tree is excluded by
// looksLikeSecret above, not by encryption).
func TestBackupTargets_GFDTargetPointsAtGFDVarAndIsFiltered(t *testing.T) {
	targets := backupTargets(&config.Config{})
	var found *backupTarget
	for i := range targets {
		if targets[i].Name == "gfd" {
			found = &targets[i]
		}
	}
	if found == nil {
		t.Fatal("backupTargets: no \"gfd\" target found")
	}
	if found.Encrypt {
		t.Error("gfd target should not be encrypted (matches fatbaby/promptoverse convention)")
	}
	if len(found.Paths) != 1 || found.Paths[0] != "/home/fatbaby/GoblinFoxDragon/var" {
		t.Errorf("gfd target Paths = %v, want exactly [/home/fatbaby/GoblinFoxDragon/var]", found.Paths)
	}
}

// TestBackupTargets_GFDSecretsTargetIsEncryptedAndRespectsOverride guards the fix for the real,
// found-live gap (2026-09-12, EMILY/BACKLOG.md SECTION 402): ~/.config/gfd-mud/env had no backup
// coverage anywhere -- neither the "gfd" target (only ever GoblinFoxDragon/var) nor anything else
// -- and silently vanished on a $HOME reset, breaking every real SSH login until it surfaced as
// an incorrect guest-mode fallback. The new "gfd-secrets" target must exist, must be encrypted
// (this is a real credential, same class as "iduna"'s own targets, not curated app state), and
// must honor GFD_MUD_ENV_PATH so a test (or a future non-default $HOME) never hardcodes
// /home/fatbaby.
func TestBackupTargets_GFDSecretsTargetIsEncryptedAndRespectsOverride(t *testing.T) {
	t.Setenv("GFD_MUD_ENV_PATH", "/tmp/fake-gfd-mud-env-for-test")
	targets := backupTargets(&config.Config{})
	var found *backupTarget
	for i := range targets {
		if targets[i].Name == "gfd-secrets" {
			found = &targets[i]
		}
	}
	if found == nil {
		t.Fatal(`backupTargets: no "gfd-secrets" target found`)
	}
	if !found.Encrypt {
		t.Error("gfd-secrets target must be encrypted -- it holds a real IDUNA agent credential")
	}
	if len(found.Paths) != 1 || found.Paths[0] != "/tmp/fake-gfd-mud-env-for-test" {
		t.Errorf("gfd-secrets target Paths = %v, want exactly the GFD_MUD_ENV_PATH override", found.Paths)
	}
}

// TestGFDMudEnvPath_DefaultsUnderHomeConfig guards the real default path (no env override) --
// the exact file gfd-mud.service's own EnvironmentFile= line reads, so a drift here would quietly
// back up the wrong file.
func TestGFDMudEnvPath_DefaultsUnderHomeConfig(t *testing.T) {
	t.Setenv("GFD_MUD_ENV_PATH", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir resolvable in this environment")
	}
	want := filepath.Join(home, ".config", "gfd-mud", "env")
	if got := gfdMudEnvPath(); got != want {
		t.Errorf("gfdMudEnvPath() = %q, want %q", got, want)
	}
}

// TestLooksLikeSecret_BareEnvFilenameIsNotFiltered guards a real, easy-to-miss edge case: the
// gfd-secrets target's own file is literally named "env" (no leading dot), not "*.env" --
// looksLikeSecret's own ".env" suffix check must NOT match it, or tarGzPaths would silently
// exclude the one file the whole "gfd-secrets" target exists to back up, defeating the fix while
// looking like it worked (the archive would just be empty).
func TestLooksLikeSecret_BareEnvFilenameIsNotFiltered(t *testing.T) {
	if looksLikeSecret("env") {
		t.Error(`looksLikeSecret("env") = true -- this would silently empty out the gfd-secrets backup target`)
	}
}

func tarEntryNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	return names
}

func TestTarGzPaths_ExcludesSecretFilesAndLogsDir(t *testing.T) {
	src := t.TempDir()
	varDir := filepath.Join(src, "var")
	mustWrite(t, filepath.Join(varDir, "truestore.db"), "real data")
	mustWrite(t, filepath.Join(varDir, "agent-secrets.env"), "SUPER_SECRET=1")
	mustWrite(t, filepath.Join(varDir, "logs", "iduna.log"), "log line")
	mustWrite(t, filepath.Join(varDir, "logs", "nested", "more.log"), "nested log")

	dst := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := tarGzPaths(dst, []string{varDir}); err != nil {
		t.Fatal(err)
	}

	names := tarEntryNames(t, dst)
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
		if filepath.Base(n) == "agent-secrets.env" {
			t.Errorf("expected agent-secrets.env to be excluded, found entry %q", n)
		}
		if n == "var/logs" || filepath.Dir(n) == "var/logs" || filepath.Dir(filepath.Dir(n)) == "var/logs" {
			t.Errorf("expected the whole logs/ subtree to be excluded, found entry %q", n)
		}
	}
	if !found["var/truestore.db"] {
		t.Errorf("expected the real data file to be included, got entries: %v", names)
	}
}

func TestTarGzPaths_BareSecretFileTopLevelIsSkippedNotFatal(t *testing.T) {
	// Regression: an early version used "return nil" instead of "continue"
	// for a bare-file top-level path that looked like a secret, which
	// would have silently aborted the ENTIRE archive (skipping every
	// remaining path in the list), not just that one file.
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "webmaster.json")
	realFile := filepath.Join(dir, "real.txt")
	mustWrite(t, secretFile, "{}")
	mustWrite(t, realFile, "real content")

	dst := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := tarGzPaths(dst, []string{secretFile, realFile}); err != nil {
		t.Fatal(err)
	}

	names := tarEntryNames(t, dst)
	foundReal := false
	for _, n := range names {
		if filepath.Base(n) == "webmaster.json" {
			t.Errorf("expected webmaster.json to be excluded, found %q", n)
		}
		if filepath.Base(n) == "real.txt" {
			foundReal = true
		}
	}
	if !foundReal {
		t.Error("expected real.txt to still be archived after the excluded path before it in the list")
	}
}

func TestTarGzPaths_MissingPathIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	mustWrite(t, real, "content")

	dst := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := tarGzPaths(dst, []string{filepath.Join(dir, "does-not-exist"), real}); err != nil {
		t.Fatal(err)
	}
	names := tarEntryNames(t, dst)
	if len(names) != 1 || filepath.Base(names[0]) != "real.txt" {
		t.Errorf("expected only real.txt archived, got %v", names)
	}
}

func TestEncryptDecryptFile_RoundTrips(t *testing.T) {
	src := filepath.Join(t.TempDir(), "plain.bin")
	plaintext := []byte("this is real backup content, not actually random")
	if err := os.WriteFile(src, plaintext, 0o644); err != nil {
		t.Fatal(err)
	}

	key := make([]byte, backupKeySizeBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	enc := filepath.Join(t.TempDir(), "enc.bin")
	if err := encryptFile(src, enc, key); err != nil {
		t.Fatal(err)
	}

	encBytes, err := os.ReadFile(enc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encBytes, plaintext) {
		t.Error("expected the encrypted file to NOT contain the plaintext verbatim")
	}

	dec := filepath.Join(t.TempDir(), "dec.bin")
	if err := decryptFile(enc, dec, key); err != nil {
		t.Fatal(err)
	}
	decBytes, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decBytes, plaintext) {
		t.Errorf("round-trip mismatch: want %q, got %q", plaintext, decBytes)
	}
}

func TestDecryptFile_WrongKeyFails(t *testing.T) {
	src := filepath.Join(t.TempDir(), "plain.bin")
	if err := os.WriteFile(src, []byte("secret data"), 0o644); err != nil {
		t.Fatal(err)
	}
	key1 := make([]byte, backupKeySizeBytes)
	key2 := make([]byte, backupKeySizeBytes)
	rand.Read(key1)
	rand.Read(key2)

	enc := filepath.Join(t.TempDir(), "enc.bin")
	if err := encryptFile(src, enc, key1); err != nil {
		t.Fatal(err)
	}
	dec := filepath.Join(t.TempDir(), "dec.bin")
	if err := decryptFile(enc, dec, key2); err == nil {
		t.Fatal("expected decryption with the wrong key to fail")
	}
}

func TestLoadOrCreateBackupKey_GeneratesThenReusesSameKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "key.hex")
	key1, err := loadOrCreateBackupKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != backupKeySizeBytes {
		t.Fatalf("expected a %d-byte key, got %d", backupKeySizeBytes, len(key1))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected the key file to be 0600, got %v", info.Mode().Perm())
	}

	key2, err := loadOrCreateBackupKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("expected the second call to reuse the same persisted key, got a different one")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
