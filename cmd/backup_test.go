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
)

func TestLooksLikeSecret(t *testing.T) {
	cases := map[string]bool{
		"agent-secrets.env": true,
		"secrets.env":       true,
		"webmaster.json":    true,
		"AGENT-SECRETS.ENV": true, // case-insensitive
		"truestore.db":      false,
		"promptoverse.db":   false,
		"BACKLOG.md":        false,
	}
	for name, want := range cases {
		if got := looksLikeSecret(name); got != want {
			t.Errorf("looksLikeSecret(%q) = %v, want %v", name, got, want)
		}
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
