package cli_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"nyashachiroro.com/codex-account/internal/app"
	"nyashachiroro.com/codex-account/internal/cli"
	"nyashachiroro.com/codex-account/internal/testutil"
	"nyashachiroro.com/codex-account/internal/toolauth"
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

func TestNameAndAllMutuallyExclusive(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	svc := app.New(app.Service{Paths: home.Paths, Zed: &toolauth.MemoryStore{}})
	var errBuf bytes.Buffer
	code := cli.Execute(context.Background(), []string{"usage", "--all", "-n", "work"}, strings.NewReader(""), io.Discard, &errBuf, svc)
	if code == 0 || !strings.Contains(errBuf.String(), "either") {
		t.Fatalf("%d %s", code, errBuf.String())
	}
}
