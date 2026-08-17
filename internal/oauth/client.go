package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/platform"
)

const (
	DefaultClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	DefaultAuthBase     = "https://auth.openai.com"
	DefaultRedirectURI  = "http://localhost:1455/auth/callback"
	DefaultScope        = "openid profile email offline_access"
	DefaultCallbackAddr = "127.0.0.1:1455"
	DefaultTimeout      = 15 * time.Minute
	httpTimeout         = 30 * time.Second
)

type Endpoints struct {
	ClientID       string
	AuthorizeURL   string
	TokenURL       string
	RedirectURI    string
	DeviceUserCode string
	DeviceToken    string
	DeviceVerify   string
	DeviceRedirect string
	Scope          string
	CallbackAddr   string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		ClientID:       DefaultClientID,
		AuthorizeURL:   DefaultAuthBase + "/oauth/authorize",
		TokenURL:       DefaultAuthBase + "/oauth/token",
		RedirectURI:    DefaultRedirectURI,
		DeviceUserCode: DefaultAuthBase + "/api/accounts/deviceauth/usercode",
		DeviceToken:    DefaultAuthBase + "/api/accounts/deviceauth/token",
		DeviceVerify:   DefaultAuthBase + "/codex/device",
		DeviceRedirect: DefaultAuthBase + "/deviceauth/callback",
		Scope:          DefaultScope,
		CallbackAddr:   DefaultCallbackAddr,
	}
}

type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
}

type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (TokenResponse, error)
}

type URLOpener func(ctx context.Context, rawURL string) error

type Originator string

const (
	OriginatorPi       Originator = "pi"
	OriginatorOpenCode Originator = "opencode"
)

type Client struct {
	HTTP      *http.Client
	Endpoints Endpoints
	Clock     platform.Clock
	OpenURL   URLOpener
	Listen    func(network, addr string) (net.Listener, error)
	Prompt    io.Writer
}

func NewClient() *Client {
	ep := DefaultEndpoints()
	return &Client{
		HTTP:      &http.Client{Timeout: httpTimeout},
		Endpoints: ep,
		Clock:     platform.RealClock{},
		Listen:    net.Listen,
	}
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (TokenResponse, error) {
	form := url.Values{
		"client_id":     {c.Endpoints.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return c.tokenRequest(ctx, form)
}

func (c *Client) ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (account.Grant, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {c.Endpoints.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	tok, err := c.tokenRequest(ctx, form)
	if err != nil {
		return account.Grant{}, err
	}
	return grantFromToken(tok, c.now()), nil
}

func grantFromToken(tok TokenResponse, now time.Time) account.Grant {
	g := account.Grant{
		AccessToken:  tok.AccessToken,
		IDToken:      tok.IDToken,
		RefreshToken: tok.RefreshToken,
		LastRefresh:  now.UTC().Truncate(time.Second),
	}
	if g.IDToken == "" {
		g.IDToken = g.AccessToken
	}
	g = g.WithIdentity()
	return g
}

func (c *Client) now() time.Time {
	if c.Clock != nil {
		return c.Clock.Now()
	}
	return time.Now()
}

func (c *Client) tokenRequest(ctx context.Context, form url.Values) (TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("OpenAI Codex OAuth request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResponse{}, fmt.Errorf("OpenAI Codex OAuth request failed (%d): %s", resp.StatusCode, sanitizeBody(body))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return TokenResponse{}, fmt.Errorf("OpenAI Codex token response was not JSON")
	}
	tok := TokenResponse{
		AccessToken:  stringField(raw["access_token"]),
		RefreshToken: stringField(raw["refresh_token"]),
		IDToken:      stringField(raw["id_token"]),
		ExpiresIn:    intField(raw["expires_in"]),
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return TokenResponse{}, fmt.Errorf("OpenAI Codex token response missing fields")
	}
	if tok.ExpiresIn <= 0 {
		tok.ExpiresIn = 3600
	}
	return tok, nil
}

func sanitizeBody(body []byte) string {
	if bytes.Contains(body, []byte("access_token")) || bytes.Contains(body, []byte("refresh_token")) {
		return "response omitted (contained tokens)"
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return trimErr(string(body))
	}
	if errObj, ok := raw["error"].(map[string]any); ok {
		if code := stringField(errObj["code"]); code != "" {
			return code
		}
		if msg := stringField(errObj["message"]); msg != "" {
			return msg
		}
	}
	if s := stringField(raw["error"]); s != "" {
		return s
	}
	return trimErr(string(body))
}

func trimErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func intField(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func PKCE() (verifier, challenge string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func RandomState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (o Originator) normalized() Originator {
	if o == OriginatorOpenCode {
		return o
	}
	return OriginatorPi
}

func (o Originator) label() string {
	switch o.normalized() {
	case OriginatorOpenCode:
		return "OpenCode"
	default:
		return "Pi"
	}
}

func (c *Client) AuthorizeURL(challenge, state string) string {
	return c.AuthorizeURLFor(challenge, state, OriginatorPi)
}

func (c *Client) AuthorizeURLFor(challenge, state string, originator Originator) string {
	originator = originator.normalized()
	q := url.Values{
		"response_type":              {"code"},
		"client_id":                  {c.Endpoints.ClientID},
		"redirect_uri":               {c.Endpoints.RedirectURI},
		"scope":                      {c.Endpoints.Scope},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"state":                      {state},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {string(originator)},
	}
	u, err := url.Parse(c.Endpoints.AuthorizeURL)
	if err != nil {
		return c.Endpoints.AuthorizeURL + "?" + q.Encode()
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) BrowserLogin(ctx context.Context) (account.Grant, error) {
	return c.BrowserLoginFor(ctx, OriginatorPi)
}

func (c *Client) BrowserLoginFor(ctx context.Context, originator Originator) (account.Grant, error) {
	originator = originator.normalized()
	verifier, challenge, err := PKCE()
	if err != nil {
		return account.Grant{}, err
	}
	state, err := RandomState()
	if err != nil {
		return account.Grant{}, err
	}
	listen := c.Listen
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", c.Endpoints.CallbackAddr)
	if err != nil {
		return account.Grant{}, fmt.Errorf("cannot bind %s for %s login: %w", c.Endpoints.CallbackAddr, originator.label(), err)
	}
	authURL := c.AuthorizeURLFor(challenge, state, originator)
	c.note("Open this URL in your browser:\n%s", authURL)
	if c.OpenURL != nil {
		_ = c.OpenURL(ctx, authURL)
	}

	code, err := waitForCode(ctx, ln, state, originator.label())
	if err != nil {
		return account.Grant{}, err
	}
	g, err := c.ExchangeCode(ctx, code, verifier, c.Endpoints.RedirectURI)
	if err != nil {
		return account.Grant{}, err
	}
	if g.Identity().AccountID == "" {
		return account.Grant{}, fmt.Errorf("failed to extract ChatGPT account id from the %s access token", originator.label())
	}
	return g, nil
}

func (c *Client) note(format string, args ...any) {
	if c.Prompt == nil {
		return
	}
	fmt.Fprintf(c.Prompt, format+"\n", args...)
}

func waitForCode(ctx context.Context, ln net.Listener, state, label string) (string, error) {
	defer ln.Close()
	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "State mismatch. You can close this window.", http.StatusBadRequest)
			select {
			case ch <- result{err: fmt.Errorf("OAuth state mismatch")}:
			default:
			}
			return
		}
		if oauthErr := q.Get("error"); oauthErr != "" {
			detail := strings.TrimSpace(q.Get("error_description"))
			if detail == "" {
				detail = oauthErr
			}
			detail = trimErr(detail)
			http.Error(w, "Authentication failed. You can close this window.", http.StatusBadRequest)
			select {
			case ch <- result{err: fmt.Errorf("%s login failed: %s", label, detail)}:
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code. You can close this window.", http.StatusBadRequest)
			select {
			case ch <- result{err: fmt.Errorf("missing authorization code")}:
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "OpenAI authentication completed. You can close this window.")
		select {
		case ch <- result{code: code}:
		default:
		}
	})
	srv := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s login timed out waiting for the browser callback", label)
		}
		return "", ctx.Err()
	case r := <-ch:
		return r.code, r.err
	}
}

func (c *Client) DeviceLogin(ctx context.Context) (account.Grant, error) {
	return c.DeviceLoginFor(ctx, OriginatorPi)
}

func (c *Client) DeviceLoginFor(ctx context.Context, originator Originator) (account.Grant, error) {
	originator = originator.normalized()
	payload, _ := json.Marshal(map[string]string{"client_id": c.Endpoints.ClientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.DeviceUserCode, bytes.NewReader(payload))
	if err != nil {
		return account.Grant{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return account.Grant{}, fmt.Errorf("OpenAI Codex OAuth request failed: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return account.Grant{}, fmt.Errorf("OpenAI Codex OAuth request failed (%d): %s", resp.StatusCode, sanitizeBody(body))
	}
	var start map[string]any
	if err := json.Unmarshal(body, &start); err != nil {
		return account.Grant{}, fmt.Errorf("invalid OpenAI Codex device code response")
	}
	deviceAuthID := stringField(start["device_auth_id"])
	userCode := stringField(start["user_code"])
	interval := durationField(start["interval"])
	if deviceAuthID == "" || userCode == "" || interval <= 0 {
		return account.Grant{}, fmt.Errorf("invalid OpenAI Codex device code response")
	}
	c.note("Open this URL in your browser:\n%s\nEnter code: %s", c.Endpoints.DeviceVerify, userCode)
	if c.OpenURL != nil {
		_ = c.OpenURL(ctx, c.Endpoints.DeviceVerify)
	}
	if interval < time.Second {
		interval = time.Second
	}
	for {
		if err := c.Clock.Sleep(ctx, interval); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return account.Grant{}, fmt.Errorf("%s device login timed out", originator.label())
			}
			return account.Grant{}, err
		}
		grant, retry, slow, err := c.pollDevice(ctx, deviceAuthID, userCode, originator)
		if err != nil {
			return account.Grant{}, err
		}
		if slow {
			interval += time.Second
			continue
		}
		if retry {
			continue
		}
		return grant, nil
	}
}

func (c *Client) pollDevice(ctx context.Context, deviceAuthID, userCode string, originator Originator) (account.Grant, bool, bool, error) {
	payload, _ := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoints.DeviceToken, bytes.NewReader(payload))
	if err != nil {
		return account.Grant{}, false, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return account.Grant{}, false, false, fmt.Errorf("OpenAI Codex device auth failed: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return account.Grant{}, true, false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := errorCode(body)
		if code == "deviceauth_authorization_pending" {
			return account.Grant{}, true, false, nil
		}
		if code == "slow_down" {
			return account.Grant{}, false, true, nil
		}
		return account.Grant{}, false, false, fmt.Errorf("OpenAI Codex device auth failed (%d): %s", resp.StatusCode, sanitizeBody(body))
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return account.Grant{}, false, false, fmt.Errorf("invalid OpenAI Codex device auth token response")
	}
	authCode := stringField(data["authorization_code"])
	verifier := stringField(data["code_verifier"])
	if authCode == "" || verifier == "" {
		return account.Grant{}, false, false, fmt.Errorf("invalid OpenAI Codex device auth token response")
	}
	g, err := c.ExchangeCode(ctx, authCode, verifier, c.Endpoints.DeviceRedirect)
	if err != nil {
		return account.Grant{}, false, false, err
	}
	if g.Identity().AccountID == "" {
		return account.Grant{}, false, false, fmt.Errorf("failed to extract ChatGPT account id from the %s access token", originator.label())
	}
	return g, false, false, nil
}

func errorCode(body []byte) string {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	switch errVal := raw["error"].(type) {
	case string:
		return errVal
	case map[string]any:
		return stringField(errVal["code"])
	default:
		return ""
	}
}

func durationField(v any) time.Duration {
	switch n := v.(type) {
	case float64:
		return time.Duration(n * float64(time.Second))
	case json.Number:
		f, _ := n.Float64()
		return time.Duration(f * float64(time.Second))
	case string:
		var f float64
		fmt.Sscanf(strings.TrimSpace(n), "%f", &f)
		return time.Duration(f * float64(time.Second))
	default:
		return 0
	}
}

func ApplyRefresh(g account.Grant, tok TokenResponse, now time.Time) account.Grant {
	return g.ApplyRefresh(tok.AccessToken, tok.IDToken, tok.RefreshToken, tok.ExpiresIn, now)
}
