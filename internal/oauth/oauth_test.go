package oauth_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/webville-dev/codex-account/internal/oauth"
	"github.com/webville-dev/codex-account/internal/testutil"
)

func TestRefreshAndSanitize(t *testing.T) {
	t.Parallel()
	var sawForm bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sawForm = strings.Contains(string(body), "grant_type=refresh_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testutil.JWT("workspace-one", "person@example.com", "plus", time.Now().Add(time.Hour)),
			"refresh_token": "new-refresh",
			"id_token":      "id",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	c := oauth.NewClient()
	c.HTTP = srv.Client()
	c.Endpoints.TokenURL = srv.URL
	tok, err := c.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if !sawForm || tok.RefreshToken != "new-refresh" {
		t.Fatalf("%+v form=%v", tok, sawForm)
	}
}

func TestTokenResponseDefaultsMissingExpiry(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testutil.JWT("workspace-one", "person@example.com", "plus", time.Now().Add(time.Hour)),
			"refresh_token": "new-refresh",
		})
	}))
	defer srv.Close()
	c := oauth.NewClient()
	c.HTTP = srv.Client()
	c.Endpoints.TokenURL = srv.URL
	tok, err := c.Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresIn != 3600 {
		t.Fatalf("default expiry = %d", tok.ExpiresIn)
	}
}

func TestAuthorizeURLOriginator(t *testing.T) {
	t.Parallel()
	c := oauth.NewClient()
	pi := c.AuthorizeURL("challenge", "state")
	if !strings.Contains(pi, "originator=pi") {
		t.Fatalf("default originator missing: %s", pi)
	}
	oc := c.AuthorizeURLFor("challenge", "state", oauth.OriginatorOpenCode)
	if !strings.Contains(oc, "originator=opencode") || strings.Contains(oc, "originator=pi") {
		t.Fatalf("opencode originator missing: %s", oc)
	}
	piAgain := c.AuthorizeURL("challenge", "state")
	if !strings.Contains(piAgain, "originator=pi") {
		t.Fatalf("OpenCode call changed the default originator: %s", piAgain)
	}
}

func TestBrowserLogin(t *testing.T) {
	t.Parallel()
	access := testutil.JWT("workspace-one", "person@example.com", "plus", time.Now().Add(time.Hour))
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": "browser-refresh",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	c := oauth.NewClient()
	c.HTTP = tokenSrv.Client()
	c.Endpoints.TokenURL = tokenSrv.URL
	c.Endpoints.CallbackAddr = addr
	c.Listen = func(network, a string) (net.Listener, error) { return ln, nil }
	c.OpenURL = func(ctx context.Context, raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		cb := "http://" + addr + "/auth/callback?code=abc&state=" + u.Query().Get("state")
		go func() {
			time.Sleep(20 * time.Millisecond)
			_, _ = http.Get(cb)
		}()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	g, err := c.BrowserLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.RefreshToken != "browser-refresh" || g.AccountID != "workspace-one" {
		t.Fatalf("%+v", g)
	}
}

func TestBrowserLoginReportsProviderCallbackError(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	c := oauth.NewClient()
	c.Endpoints.CallbackAddr = addr
	c.Listen = func(network, a string) (net.Listener, error) { return ln, nil }
	c.OpenURL = func(ctx context.Context, raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		callback := "http://" + addr + "/auth/callback?error=access_denied&error_description=user+cancelled&state=" + u.Query().Get("state")
		go func() {
			time.Sleep(20 * time.Millisecond)
			resp, getErr := http.Get(callback)
			if getErr == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.BrowserLoginFor(ctx, oauth.OriginatorOpenCode)
	if err == nil || !strings.Contains(err.Error(), "OpenCode login failed: user cancelled") {
		t.Fatalf("unexpected callback error: %v", err)
	}
}

func TestDeviceLoginPendingThenSuccess(t *testing.T) {
	t.Parallel()
	access := testutil.JWT("workspace-one", "person@example.com", "plus", time.Now().Add(time.Hour))
	n := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/usercode", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_auth_id": "dev1",
			"user_code":      "ABCD",
			"interval":       0.01,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "deviceauth_authorization_pending"}})
			return
		}
		if strings.Contains(r.URL.Path, "oauth") || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  access,
				"refresh_token": "device-refresh",
				"expires_in":    3600,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_code": "code",
			"code_verifier":      "verifier",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  access,
				"refresh_token": "device-refresh",
				"expires_in":    3600,
			})
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "usercode"):
			mux.ServeHTTP(w, r)
		case r.Header.Get("Content-Type") == "application/x-www-form-urlencoded":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  access,
				"refresh_token": "device-refresh",
				"expires_in":    3600,
			})
		default:
			n++
			if n == 1 {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "deviceauth_authorization_pending"}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"authorization_code": "code",
				"code_verifier":      "verifier",
			})
		}
	}))
	defer srv.Close()
	c := oauth.NewClient()
	c.HTTP = srv.Client()
	c.Clock = testutil.FixedClock{T: time.Now()}
	c.Endpoints.DeviceUserCode = srv.URL + "/usercode"
	c.Endpoints.DeviceToken = srv.URL + "/device"
	c.Endpoints.TokenURL = srv.URL + "/oauth"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	g, err := c.DeviceLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if g.RefreshToken != "device-refresh" {
		t.Fatalf("%+v", g)
	}
}
