package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolvePrefersLdflags(t *testing.T) {
	t.Parallel()
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "aaaaaaaa"},
			{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
		},
	}
	v, c, b := resolve("0.1.0", "bbbbbbbb", "2026-01-01T00:00:00Z", info, true)
	if v != "0.1.0" || c != "bbbbbbbb" || b != "2026-01-01T00:00:00Z" {
		t.Fatalf("%s %s %s", v, c, b)
	}
}

func TestResolveGoInstallModuleVersion(t *testing.T) {
	t.Parallel()
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	v, _, _ := resolve("dev", "none", "unknown", info, true)
	if v != "v1.2.3" {
		t.Fatalf("version %q", v)
	}
}

func TestResolveLocalDevelFromVCS(t *testing.T) {
	t.Parallel()
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdef123456"},
			{Key: "vcs.time", Value: "2026-08-17T00:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	v, c, b := resolve("dev", "none", "unknown", info, true)
	if v != "devel-abcdef1-dirty" {
		t.Fatalf("version %q", v)
	}
	if c != "abcdef123456" {
		t.Fatalf("commit %q", c)
	}
	if b != "2026-08-17T00:00:00Z" {
		t.Fatalf("built %q", b)
	}
}

func TestLineContainsName(t *testing.T) {
	t.Parallel()
	if got := Line(); !strings.HasPrefix(got, "codex-account ") {
		t.Fatalf("%q", got)
	}
}
