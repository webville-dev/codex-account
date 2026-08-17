package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"nyashachiroro.com/codex-account/internal/platform"
)

func TestResolveEnvOverrides(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := map[string]string{
		"CODEX_HOME":          filepath.Join(home, "c"),
		"PI_CODING_AGENT_DIR": filepath.Join(home, "p"),
		"XDG_DATA_HOME":       filepath.Join(home, "xdg"),
		"OPENCODE_DATA":       filepath.Join(home, "oc"),
		"CODEX_ACCOUNT_DIR":   filepath.Join(home, "acct"),
		"CODEX_ACCOUNTS_HOME": filepath.Join(home, "saves"),
	}
	p, err := platform.Resolve(platform.Env{
		Home:   home,
		Getenv: func(k string) string { return env[k] },
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.CodexAuth != filepath.Join(home, "c", "auth.json") {
		t.Fatalf("codex auth %s", p.CodexAuth)
	}
	if p.PiAuth != filepath.Join(home, "p", "auth.json") {
		t.Fatalf("pi auth %s", p.PiAuth)
	}
	if p.OpenCodeAuth != filepath.Join(home, "oc", "auth.json") {
		t.Fatalf("opencode %s", p.OpenCodeAuth)
	}
	if p.AccountsHome != filepath.Join(home, "saves") {
		t.Fatalf("accounts %s", p.AccountsHome)
	}
	if p.PendingFile != filepath.Join(home, ".pending-refresh.json") {
		t.Fatalf("pending %s", p.PendingFile)
	}
	if p.CurrentFile != filepath.Join(home, "acct", ".current") {
		t.Fatalf("current %s", p.CurrentFile)
	}
}

func TestOpenCodeUsesXDGWhenOpenCodeDataUnset(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p, err := platform.Resolve(platform.Env{
		Home: home,
		Getenv: func(k string) string {
			if k == "XDG_DATA_HOME" {
				return filepath.Join(home, "xdg")
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "xdg", "opencode", "auth.json")
	if p.OpenCodeAuth != want {
		t.Fatalf("got %s", p.OpenCodeAuth)
	}
}

func TestCodexStorageRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := platform.Paths{CodexConfig: filepath.Join(dir, "config.toml")}
	if err := os.WriteFile(p.CodexConfig, []byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCodexStorage(); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestCodexStorageAbsentOrFileOK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := platform.Paths{CodexConfig: filepath.Join(dir, "missing.toml")}
	if err := p.CheckCodexStorage(); err != nil {
		t.Fatal(err)
	}
	p.CodexConfig = filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p.CodexConfig, []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCodexStorage(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.CodexConfig, []byte("cli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCodexStorage(); err != nil {
		t.Fatal(err)
	}
}
