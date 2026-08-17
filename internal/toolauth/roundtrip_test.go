package toolauth_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/testutil"
	"nyashachiroro.com/codex-account/internal/toolauth"
)

func Testdata(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "auth"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLiveCredentialRequiresAccessToken(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "auth.json")
	invalid := account.Grant{AccountID: "workspace-one", RefreshToken: "refresh-only"}
	if err := toolauth.WriteCodexFile(path, invalid); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := toolauth.FileLiveGrant(path); err == nil || ok {
		t.Fatalf("refresh-only credential accepted as live: ok=%v err=%v", ok, err)
	}
}

func TestSecretToolTimeoutIsNotReportedAsMissing(t *testing.T) {
	t.Parallel()
	store := &toolauth.SecretToolStore{
		LookPath: func(string) (string, error) { return "/test/secret-tool", nil },
		Run: func(context.Context, []byte, ...string) ([]byte, []byte, error) {
			return nil, nil, context.DeadlineExceeded
		},
	}
	if _, err := store.Get(context.Background()); !errors.Is(err, toolauth.ErrZedLocked) {
		t.Fatalf("timeout classification: %v", err)
	}
}

func TestRoundTripFixtures(t *testing.T) {
	t.Parallel()
	dir := Testdata(t)
	for _, name := range []string{"codex.json", "pi.json", "opencode.json", "zed.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		g, err := toolauth.ParseBytes(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if g.AccountID != "workspace-one" || g.RefreshToken != "test-refresh-workspace-one" {
			t.Fatalf("%s: %+v", name, g)
		}
	}
}

func TestPiPreservesOtherProviders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	src, err := os.ReadFile(filepath.Join(Testdata(t), "pi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	g := testutil.Grant("workspace-two", "new-refresh", time.Now().Add(2*time.Hour))
	if err := toolauth.WritePiFile(path, g, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte("unrelated-provider-must-survive")) {
		t.Fatalf("lost cursor provider: %s", raw)
	}
	got, err := toolauth.ReadPiFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "new-refresh" {
		t.Fatalf("%+v", got)
	}
}

func TestOpenCodePreservesOtherProviders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	src, err := os.ReadFile(filepath.Join(Testdata(t), "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	g := testutil.Grant("workspace-two", "new-refresh", time.Now().Add(2*time.Hour))
	if err := toolauth.WriteOpenCodeFile(path, g, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte("unrelated-opencode-provider")) {
		t.Fatalf("lost provider: %s", raw)
	}
}

func TestMalformedDoesNotPanic(t *testing.T) {
	t.Parallel()
	for _, raw := range [][]byte{[]byte("[]"), []byte("{"), []byte(`{"tokens":"nope"}`)} {
		_, _ = toolauth.ParseBytes(raw)
	}
}

func FuzzParseBytes(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"tokens":{"access_token":"a","refresh_token":"b"}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = toolauth.ParseBytes(raw)
	})
}
