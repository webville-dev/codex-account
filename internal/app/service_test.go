package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/app"
	"nyashachiroro.com/codex-account/internal/oauth"
	"nyashachiroro.com/codex-account/internal/platform"
	"nyashachiroro.com/codex-account/internal/testutil"
	"nyashachiroro.com/codex-account/internal/toolauth"
)

type refreshStub struct {
	access string
}

func (r refreshStub) Refresh(ctx context.Context, refreshToken string) (oauth.TokenResponse, error) {
	return oauth.TokenResponse{
		AccessToken:  r.access,
		IDToken:      r.access,
		RefreshToken: "new-refresh",
		ExpiresIn:    7200,
	}, nil
}

type zedStub struct {
	grant account.Grant
	get   error
	put   error
}

func (z *zedStub) Get(ctx context.Context) (account.Grant, error) {
	if z.get != nil {
		return account.Grant{}, z.get
	}
	if z.grant.RefreshToken == "" {
		return account.Grant{}, toolauth.ErrZedMissing
	}
	return z.grant, nil
}

type runnerStub struct {
	run     func() error
	lookErr error
}

func (r runnerStub) LookPath(string) (string, error) {
	if r.lookErr != nil {
		return "", r.lookErr
	}
	return "/test/codex", nil
}

func (r runnerStub) Run(context.Context, string, []string, platform.RunOptions) error {
	if r.run == nil {
		return nil
	}
	return r.run()
}

func (z *zedStub) Put(ctx context.Context, g account.Grant) error {
	if z.put != nil {
		return z.put
	}
	z.grant = g
	return nil
}

func newSvc(t *testing.T, zed toolauth.CredentialStore, refresher oauth.TokenRefresher) (*app.Service, testutil.TestHome) {
	t.Helper()
	home := testutil.NewHome(t)
	if zed == nil {
		zed = &zedStub{}
	}
	if refresher == nil {
		refresher = refreshStub{access: testutil.JWT("workspace-one", "person@example.com", "business", time.Now().Add(2*time.Hour))}
	}
	svc := app.New(app.Service{
		Paths:     home.Paths,
		Zed:       zed,
		Refresher: refresher,
		Clock:     testutil.FixedClock{T: time.Now()},
		Stdin:     strings.NewReader(""),
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	})
	return svc, home
}

func writePrimaryAgent(t *testing.T, home testutil.TestHome, agent string) {
	t.Helper()
	data := []byte(fmt.Sprintf("{\"primaryAgent\":%q}\n", agent))
	if err := os.WriteFile(home.Paths.SettingsFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshFailureKeepsRecoveryAndSyncConsumesIt(t *testing.T) {
	t.Parallel()
	zed := &zedStub{put: errors.New("locked keyring")}
	svc, home := newSvc(t, zed, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Minute))
	zed.grant = old
	if err := toolauth.WritePiFile(home.Paths.PiAuth, old, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(filepath.Join(home.Paths.AccountsHome, "person@example.com.business.json"), old); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Refresh(context.Background(), "")
	if err == nil {
		t.Fatal("expected refresh destination failure")
	}
	pending, err := toolauth.ReadAnyFile(home.Paths.PendingFile)
	if err != nil {
		t.Fatal(err)
	}
	if pending.RefreshToken != "new-refresh" {
		t.Fatalf("pending %+v", pending)
	}
	zed.put = nil
	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home.Paths.PendingFile); !os.IsNotExist(err) {
		t.Fatal("pending should be consumed")
	}
	if zed.grant.RefreshToken != "new-refresh" {
		t.Fatalf("zed %+v", zed.grant)
	}
	codex, err := toolauth.ReadAnyFile(home.Paths.CodexAuth)
	if err != nil || codex.RefreshToken != "new-refresh" {
		t.Fatalf("codex %+v %v", codex, err)
	}
	if !strings.Contains(res.Message, "recovery") && !strings.Contains(res.Message, "Synced") {
		t.Fatalf("message %q", res.Message)
	}
}

func TestRefreshSavedDoesNotFillMissingLiveDestinations(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Minute))
	name := "person@example.com.business"
	if err := toolauth.WriteCodexFile(filepath.Join(home.Paths.AccountsHome, name+".json"), old); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home.Paths.CodexAuth, home.Paths.PiAuth, home.Paths.OpenCodeAuth} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist", path)
		}
	}
	if _, err := os.Stat(home.Paths.PendingFile); !os.IsNotExist(err) {
		t.Fatal("pending should be cleared")
	}
	got, err := toolauth.ReadAnyFile(filepath.Join(home.Paths.AccountsHome, name+".json"))
	if err != nil || got.RefreshToken != "new-refresh" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPendingRefreshBlocksMutations(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	recovery := testutil.Grant("workspace-one", "fresh-refresh", time.Now().Add(time.Hour))
	if err := toolauth.WriteCodexFile(home.Paths.PendingFile, recovery); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(context.Background(), app.LoginOptions{Agent: "pi"}); err == nil {
		t.Fatal("login should be blocked")
	}
	got, err := toolauth.ReadAnyFile(home.Paths.PendingFile)
	if err != nil || got.RefreshToken != "fresh-refresh" {
		t.Fatalf("recovery mutated: %+v %v", got, err)
	}
}

func TestUsageRefreshFailureKeepsRecoveryGrant(t *testing.T) {
	t.Parallel()
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(-time.Hour))
	zed := &zedStub{grant: old, put: errors.New("locked keyring")}
	svc, home := newSvc(t, zed, refreshStub{
		access: testutil.JWT("workspace-one", "person@example.com", "business", time.Now().Add(2*time.Hour)),
	})
	target := filepath.Join(home.Paths.AccountsHome, "person@example.com.business.json")
	if err := toolauth.WriteCodexFile(target, old); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Usage(context.Background(), app.UsageOptions{Name: "person@example.com.business"})
	pending, err := toolauth.ReadAnyFile(home.Paths.PendingFile)
	if err != nil {
		t.Fatal(err)
	}
	if pending.RefreshToken != "new-refresh" {
		t.Fatalf("pending %+v", pending)
	}
}

func TestListCurrentDoNotCreateDirs(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	_ = os.RemoveAll(home.Paths.AccountsHome)
	_ = os.RemoveAll(home.Paths.AccountDir)
	svc := app.New(app.Service{
		Paths:  home.Paths,
		Zed:    &zedStub{},
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if _, err := svc.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home.Paths.AccountsHome); !os.IsNotExist(err) {
		t.Fatal("list created accounts dir")
	}
}

func TestSwitchWritesAllFormatsAndPreservesProviders(t *testing.T) {
	t.Parallel()
	zed := &zedStub{}
	svc, home := newSvc(t, zed, nil)
	if err := os.WriteFile(home.Paths.PiAuth, []byte(`{"cursor":{"type":"api","key":"keep-me"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(home.Paths.OpenCodeAuth, []byte(`{"anthropic":{"type":"api"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g := testutil.Grant("workspace-one", "r1", time.Now().Add(time.Hour))
	name := "person@example.com.business"
	if err := toolauth.WriteCodexFile(filepath.Join(home.Paths.AccountsHome, name+".json"), g); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Switch(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	piRaw, _ := os.ReadFile(home.Paths.PiAuth)
	if !strings.Contains(string(piRaw), "keep-me") {
		t.Fatalf("pi lost provider: %s", piRaw)
	}
	ocRaw, _ := os.ReadFile(home.Paths.OpenCodeAuth)
	if !strings.Contains(string(ocRaw), "anthropic") {
		t.Fatalf("opencode lost provider: %s", ocRaw)
	}
	if zed.grant.RefreshToken != "r1" {
		t.Fatalf("zed %+v", zed.grant)
	}
}

func TestLockedZedFailsBeforeLocalSwitchWrites(t *testing.T) {
	t.Parallel()
	zed := &zedStub{put: errors.New("locked")}
	svc, home := newSvc(t, zed, nil)
	g := testutil.Grant("workspace-one", "r1", time.Now().Add(time.Hour))
	name := "person@example.com.business"
	if err := toolauth.WriteCodexFile(filepath.Join(home.Paths.AccountsHome, name+".json"), g); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Switch(context.Background(), name); err == nil {
		t.Fatal("expected zed failure")
	}
	if _, err := os.Stat(home.Paths.CodexAuth); !os.IsNotExist(err) {
		t.Fatal("codex should be untouched")
	}
}

func TestRemoveLeavesLiveFiles(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	g := testutil.Grant("workspace-one", "r1", time.Now().Add(time.Hour))
	if err := toolauth.WritePiFile(home.Paths.PiAuth, g, time.Now()); err != nil {
		t.Fatal(err)
	}
	name := "person@example.com.business"
	if err := toolauth.WriteCodexFile(filepath.Join(home.Paths.AccountsHome, name+".json"), g); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Remove(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home.Paths.PiAuth); err != nil {
		t.Fatal("live pi removed")
	}
	if _, err := os.Stat(filepath.Join(home.Paths.AccountsHome, name+".json")); !os.IsNotExist(err) {
		t.Fatal("saved grant still present")
	}
}

func TestDifferentAccountIDsRefuseSync(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	a := testutil.Grant("workspace-one", "r1", time.Now().Add(time.Hour))
	b := testutil.Grant("workspace-two", "r2", time.Now().Add(2*time.Hour))
	if err := toolauth.WritePiFile(home.Paths.PiAuth, a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, b); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(context.Background()); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestRecoveryAlwaysWinsEvenWithOlderAccessExpiry(t *testing.T) {
	t.Parallel()
	zed := &zedStub{}
	svc, home := newSvc(t, zed, nil)
	live := testutil.Grant("workspace-one", "superseded-refresh", time.Now().Add(4*time.Hour))
	recovery := testutil.Grant("workspace-one", "rotated-refresh", time.Now().Add(time.Hour))
	if err := toolauth.WritePiFile(home.Paths.PiAuth, live, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.PendingFile, recovery); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := toolauth.ReadAnyFile(home.Paths.PiAuth)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "rotated-refresh" {
		t.Fatalf("sync selected stale live grant: %+v", got)
	}
	if _, err := os.Stat(home.Paths.PendingFile); !os.IsNotExist(err) {
		t.Fatal("successful recovery sync should clear the journal")
	}
}

func TestRefreshKeepsRecoveryWhenZedCannotBeRead(t *testing.T) {
	t.Parallel()
	zed := &zedStub{get: toolauth.ErrZedLocked}
	svc, home := newSvc(t, zed, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Minute))
	if err := toolauth.WritePiFile(home.Paths.PiAuth, old, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "zed") {
		t.Fatalf("expected Zed read failure, got %v", err)
	}
	pending, err := toolauth.ReadAnyFile(home.Paths.PendingFile)
	if err != nil {
		t.Fatal(err)
	}
	if pending.RefreshToken != "new-refresh" {
		t.Fatalf("rotated recovery was not retained: %+v", pending)
	}
}

func TestFailedCodexLoginRestoresStashOverAttemptedAuth(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Hour))
	attempt := testutil.Grant("workspace-two", "attempt-refresh", time.Now().Add(2*time.Hour))
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, old); err != nil {
		t.Fatal(err)
	}
	svc.Runner = runnerStub{run: func() error {
		if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, attempt); err != nil {
			return err
		}
		return errors.New("login interrupted")
	}}
	if _, err := svc.Login(context.Background(), app.LoginOptions{Agent: "codex"}); err == nil {
		t.Fatal("expected login failure")
	}
	got, err := toolauth.ReadAnyFile(home.Paths.CodexAuth)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "old-refresh" {
		t.Fatalf("previous login was not restored: %+v", got)
	}
	if _, err := os.Stat(home.Paths.CodexStash); !os.IsNotExist(err) {
		t.Fatal("stash should be consumed by rollback")
	}
}

func TestSuccessfulCodexProcessWithInvalidAuthRestoresStash(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Hour))
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, old); err != nil {
		t.Fatal(err)
	}
	svc.Runner = runnerStub{run: func() error {
		invalid := account.Grant{AccountID: "workspace-two", RefreshToken: "attempt-refresh"}
		return toolauth.WriteCodexFile(home.Paths.CodexAuth, invalid)
	}}
	if _, err := svc.Login(context.Background(), app.LoginOptions{Agent: "codex"}); err == nil {
		t.Fatal("invalid auth should fail login")
	}
	got, err := toolauth.ReadAnyFile(home.Paths.CodexAuth)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "old-refresh" {
		t.Fatalf("previous login was not restored: %+v", got)
	}
}

func TestSuccessfulCodexLoginCommitsOnlyAfterValidation(t *testing.T) {
	t.Parallel()
	zed := &zedStub{}
	svc, home := newSvc(t, zed, nil)
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Hour))
	fresh := testutil.Grant("workspace-two", "fresh-refresh", time.Now().Add(2*time.Hour))
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, old); err != nil {
		t.Fatal(err)
	}
	svc.Runner = runnerStub{run: func() error {
		return toolauth.WriteCodexFile(home.Paths.CodexAuth, fresh)
	}}
	if _, err := svc.Login(context.Background(), app.LoginOptions{Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	got, err := toolauth.ReadAnyFile(home.Paths.CodexAuth)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "fresh-refresh" || zed.grant.RefreshToken != "fresh-refresh" {
		t.Fatalf("new login was not distributed: codex=%+v zed=%+v", got, zed.grant)
	}
	if _, err := os.Stat(home.Paths.CodexStash); !os.IsNotExist(err) {
		t.Fatal("validated login should remove the rollback stash")
	}
}

func TestPrimaryAgentDefaultsToPi(t *testing.T) {
	t.Parallel()
	svc, _ := newSvc(t, &zedStub{}, nil)
	got, err := svc.PrimaryAgent()
	if err != nil || got != "pi" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestSaveExplicitSourceDoesNotFallBack(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	writePrimaryAgent(t, home, "codex")
	pi := testutil.Grant("workspace-one", "pi-refresh", time.Now().Add(time.Hour))
	if err := toolauth.WritePiFile(home.Paths.PiAuth, pi, time.Now()); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Save(context.Background(), app.SaveOptions{From: "codex", Name: "work"})
	if err == nil || !strings.Contains(err.Error(), home.Paths.CodexAuth) {
		t.Fatalf("expected exact Codex source error, got %v", err)
	}
	if _, err := os.Stat(svc.Store.SavedPath("work")); !os.IsNotExist(err) {
		t.Fatalf("explicit Codex source unexpectedly saved Pi grant: %v", err)
	}
}

func TestSnapshotTieKeepsPrimaryGrant(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	writePrimaryAgent(t, home, "codex")
	expires := time.Now().Add(time.Hour)
	pi := testutil.Grant("workspace-one", "pi-refresh", expires)
	codex := testutil.Grant("workspace-one", "codex-refresh", expires)
	if err := toolauth.WritePiFile(home.Paths.PiAuth, pi, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, codex); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Save(context.Background(), app.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := toolauth.ReadAnyFile(svc.Store.SavedPath("person@example.com.business"))
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "codex-refresh" {
		t.Fatalf("snapshot selected non-primary equal-expiry grant: %+v", got)
	}
}

func TestSyncTieUsesPrimaryAgent(t *testing.T) {
	for _, primary := range []string{"codex", "opencode"} {
		primary := primary
		t.Run(primary, func(t *testing.T) {
			t.Parallel()
			svc, home := newSvc(t, &zedStub{}, nil)
			writePrimaryAgent(t, home, primary)
			expires := time.Now().Add(time.Hour)
			pi := testutil.Grant("workspace-one", "pi-refresh", expires)
			preferred := testutil.Grant("workspace-one", primary+"-refresh", expires)
			if err := toolauth.WritePiFile(home.Paths.PiAuth, pi, time.Now()); err != nil {
				t.Fatal(err)
			}
			switch primary {
			case "codex":
				if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, preferred); err != nil {
					t.Fatal(err)
				}
			case "opencode":
				if err := toolauth.WriteOpenCodeFile(home.Paths.OpenCodeAuth, preferred, time.Now()); err != nil {
					t.Fatal(err)
				}
			}

			result, err := svc.Sync(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got, err := toolauth.ReadAnyFile(home.Paths.PiAuth)
			if err != nil {
				t.Fatal(err)
			}
			if got.RefreshToken != primary+"-refresh" || !strings.Contains(result.Message, primary) {
				t.Fatalf("sync did not select primary %s grant: %+v, %q", primary, got, result.Message)
			}
		})
	}
}

func TestLoginUsesPrimaryAgentFromSettings(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	writePrimaryAgent(t, home, "codex")
	svc.Runner = runnerStub{lookErr: errors.New("not found")}
	_, err := svc.Login(context.Background(), app.LoginOptions{})
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("expected Codex login path, got %v", err)
	}
}

func TestLoginUsesOpenCodePrimaryAgent(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	writePrimaryAgent(t, home, "opencode")

	access := testutil.JWT("workspace-one", "person@example.com", "plus", time.Now().Add(time.Hour))
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": "opencode-refresh",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(tokenSrv.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	var originator string
	svc.OAuth.HTTP = tokenSrv.Client()
	svc.OAuth.Endpoints.TokenURL = tokenSrv.URL
	svc.OAuth.Endpoints.CallbackAddr = addr
	svc.OAuth.Listen = func(network, a string) (net.Listener, error) { return ln, nil }
	svc.OAuth.OpenURL = func(ctx context.Context, raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		originator = u.Query().Get("originator")
		cb := "http://" + addr + "/auth/callback?code=abc&state=" + u.Query().Get("state")
		go func() {
			time.Sleep(20 * time.Millisecond)
			_, _ = http.Get(cb)
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := svc.Login(ctx, app.LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if originator != "opencode" {
		t.Fatalf("originator %q", originator)
	}
	if !strings.Contains(res.Notes[0], "OpenCode") {
		t.Fatalf("notes %+v", res.Notes)
	}
	got, err := toolauth.ReadAnyFile(home.Paths.OpenCodeAuth)
	if err != nil || got.RefreshToken != "opencode-refresh" {
		t.Fatalf("opencode %+v %v", got, err)
	}
	pi, err := toolauth.ReadAnyFile(home.Paths.PiAuth)
	if err != nil || pi.RefreshToken != "opencode-refresh" {
		t.Fatalf("pi %+v %v", pi, err)
	}
}

func TestLoginRejectsInvalidPrimaryAgent(t *testing.T) {
	t.Parallel()
	svc, home := newSvc(t, &zedStub{}, nil)
	writePrimaryAgent(t, home, "zed")
	_, err := svc.Login(context.Background(), app.LoginOptions{})
	if err == nil || !strings.Contains(err.Error(), "primaryAgent") {
		t.Fatalf("got %v", err)
	}
}
