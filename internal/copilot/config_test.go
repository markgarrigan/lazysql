package copilot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jorgerojas26/lazysql/models"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Enabled {
		t.Fatal("Copilot must be disabled by default")
	}
	if c.AllowRowData {
		t.Fatal("row data must be off by default")
	}
	if c.AuthMethod != AuthMethodDevice {
		t.Fatalf("default auth method should be device, got %q", c.AuthMethod)
	}
	if c.Model != DefaultModel {
		t.Fatalf("default model should be %q, got %q", DefaultModel, c.Model)
	}
	if c.MaxRows != DefaultMaxRows {
		t.Fatalf("default max rows should be %d, got %d", DefaultMaxRows, c.MaxRows)
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize(models.CopilotConfig{AuthMethod: "bogus", Model: "", MaxRows: 0})
	if got.AuthMethod != AuthMethodDevice {
		t.Fatalf("invalid auth method should normalize to device, got %q", got.AuthMethod)
	}
	if got.Model != DefaultModel {
		t.Fatalf("empty model should normalize to default, got %q", got.Model)
	}
	if got.MaxRows != DefaultMaxRows {
		t.Fatalf("non-positive max rows should normalize to default, got %d", got.MaxRows)
	}

	kept := Normalize(models.CopilotConfig{AuthMethod: AuthMethodPAT, Model: "gpt-4o-mini", MaxRows: 10})
	if kept.AuthMethod != AuthMethodPAT || kept.Model != "gpt-4o-mini" || kept.MaxRows != 10 {
		t.Fatalf("valid values must be preserved, got %+v", kept)
	}
}

func TestTokenStoreSaveLoadPermissions(t *testing.T) {
	dir := t.TempDir()
	store := &TokenStore{Dir: dir}

	if err := store.Save("secret-token"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got != "secret-token" {
		t.Fatalf("expected saved token, got %q", got)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, defaultTokenFileName))
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("expected 0o600 permissions, got %o", perm)
		}
	}
}

func TestTokenStoreDelete(t *testing.T) {
	store := &TokenStore{Dir: t.TempDir()}
	if err := store.Save("t"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected error loading deleted token")
	}
	// Deleting a missing file must not error.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on missing file should be nil, got %v", err)
	}
}

func TestResolveTokenValueEnv(t *testing.T) {
	t.Setenv("LAZYSQL_TEST_TOKEN", "from-env")
	if got := ResolveTokenValue("${env:LAZYSQL_TEST_TOKEN}"); got != "from-env" {
		t.Fatalf("expected env-sourced token, got %q", got)
	}
	if got := ResolveTokenValue("plain"); got != "plain" {
		t.Fatalf("plain token should pass through, got %q", got)
	}
}

func TestTokenStoreLoadResolvesEnv(t *testing.T) {
	t.Setenv("LAZYSQL_TEST_TOKEN2", "resolved")
	store := &TokenStore{Dir: t.TempDir()}
	if err := store.Save("${env:LAZYSQL_TEST_TOKEN2}"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got != "resolved" {
		t.Fatalf("expected env-resolved token from disk, got %q", got)
	}
}
