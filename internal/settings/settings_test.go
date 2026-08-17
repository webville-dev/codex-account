package settings_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/webville-dev/codex-account/internal/settings"
)

func TestLoadMissingUsesDefaultPi(t *testing.T) {
	t.Parallel()
	got, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.PrimaryAgent != settings.AgentPi {
		t.Fatalf("%+v", got)
	}
}

func TestLoadPrimaryAgent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"primaryAgent":"Codex"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrimaryAgent != settings.AgentCodex {
		t.Fatalf("%+v", got)
	}
}

func TestLoadEmptyObjectDefaultsPi(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrimaryAgent != settings.AgentPi {
		t.Fatalf("%+v", got)
	}
}

func TestLoadPrimaryAgentOpenCode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"primaryAgent":"OpenCode"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrimaryAgent != settings.AgentOpenCode {
		t.Fatalf("%+v", got)
	}
}

func TestPrimaryAgentsReturnsIndependentCopy(t *testing.T) {
	t.Parallel()
	agents := settings.PrimaryAgents()
	agents[0] = "changed"
	if !settings.IsPrimaryAgent(settings.AgentPi) || settings.IsPrimaryAgent("changed") {
		t.Fatal("caller mutated the supported primary-agent set")
	}
}

func TestLoadRejectsUnknownAgent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"primaryAgent":"zed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsUnknownSetting(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"primary_agent":"codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(path); err == nil {
		t.Fatal("expected error for misspelled setting")
	}
}
