package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webville-dev/codex-account/internal/app"
	"github.com/webville-dev/codex-account/internal/cli"
	"github.com/webville-dev/codex-account/internal/testutil"
	"github.com/webville-dev/codex-account/internal/toolauth"
)

func TestUnknownCommandAndMissingFlagValue(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{
		Paths:  home.Paths,
		Zed:    &toolauth.MemoryStore{},
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	var errBuf bytes.Buffer
	code := cli.Execute(context.Background(), []string{"nope"}, strings.NewReader(""), io.Discard, &errBuf, svc)
	if code == 0 || !strings.Contains(errBuf.String(), "Error:") {
		t.Fatalf("unknown command: %d %s", code, errBuf.String())
	}
	errBuf.Reset()
	code = cli.Execute(context.Background(), []string{"save", "--name"}, strings.NewReader(""), io.Discard, &errBuf, svc)
	if code == 0 || errBuf.Len() == 0 {
		t.Fatalf("missing flag value should be a normal CLI error: %d %s", code, errBuf.String())
	}
}

func TestHelpDoesNotCreateDirs(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var out bytes.Buffer
	code := cli.Execute(context.Background(), []string{"help"}, strings.NewReader(""), &out, io.Discard, svc)
	if code != 0 {
		t.Fatalf("help exit %d", code)
	}
	if !strings.Contains(out.String(), "codex-account") {
		t.Fatalf("help: %s", out.String())
	}
}

func TestListGoldenEmpty(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var out bytes.Buffer
	code := cli.Execute(context.Background(), []string{"list"}, strings.NewReader(""), &out, io.Discard, svc)
	if code != 0 || strings.TrimSpace(out.String()) != "(none)" {
		t.Fatalf("%d %q", code, out.String())
	}
}

func TestListMarksConfiguredPrimary(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	if err := os.WriteFile(home.Paths.SettingsFile, []byte("{\"primaryAgent\":\"codex\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	pi := testutil.Grant("pi-workspace", "pi-refresh", expires)
	codex := testutil.Grant("codex-workspace", "codex-refresh", expires)
	if err := toolauth.WritePiFile(home.Paths.PiAuth, pi, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, codex); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.AccountsHome+"/pi-work.json", pi); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.AccountsHome+"/codex-work.json", codex); err != nil {
		t.Fatal(err)
	}

	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var out bytes.Buffer
	code := cli.Execute(context.Background(), []string{"list"}, strings.NewReader(""), &out, io.Discard, svc)
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "* codex-work") || !strings.Contains(out.String(), "p pi-work") {
		t.Fatalf("primary markers missing:\n%s", out.String())
	}
}

func TestListMarksOpenCodePrimary(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	if err := os.WriteFile(home.Paths.SettingsFile, []byte("{\"primaryAgent\":\"opencode\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour)
	pi := testutil.Grant("pi-workspace", "pi-refresh", expires)
	opencode := testutil.Grant("opencode-workspace", "opencode-refresh", expires)
	if err := toolauth.WritePiFile(home.Paths.PiAuth, pi, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteOpenCodeFile(home.Paths.OpenCodeAuth, opencode, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.AccountsHome+"/pi-work.json", pi); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.AccountsHome+"/opencode-work.json", opencode); err != nil {
		t.Fatal(err)
	}

	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var out bytes.Buffer
	code := cli.Execute(context.Background(), []string{"list"}, strings.NewReader(""), &out, io.Discard, svc)
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "* opencode-work") || !strings.Contains(out.String(), "p pi-work") {
		t.Fatalf("primary markers missing:\n%s", out.String())
	}
}

func TestVersionAndAliases(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var out bytes.Buffer
	if code := cli.Execute(context.Background(), []string{"version"}, strings.NewReader(""), &out, io.Discard, svc); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "codex-account") {
		t.Fatalf("%s", out.String())
	}
	out.Reset()
	g := testutil.Grant("workspace-one", "r1", time.Now().Add(time.Hour))
	_ = toolauth.WriteCodexFile(home.Paths.AccountsHome+"/person@example.com.business.json", g)
	if code := cli.Execute(context.Background(), []string{"status"}, strings.NewReader(""), &out, io.Discard, svc); code != 0 {
		t.Fatal(out.String())
	}
}

func TestUnknownUsageAllFlag(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var errBuf bytes.Buffer
	code := cli.Execute(context.Background(), []string{"usage", "--all"}, strings.NewReader(""), io.Discard, &errBuf, svc)
	if code == 0 || !strings.Contains(errBuf.String(), "unknown flag") {
		t.Fatalf("%d %s", code, errBuf.String())
	}
}
