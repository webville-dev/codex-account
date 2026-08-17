package toolauth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/fileutil"
)

type CodexFile struct {
	AuthMode    string      `json:"auth_mode"`
	Tokens      CodexTokens `json:"tokens"`
	LastRefresh string      `json:"last_refresh,omitempty"`
}

type CodexTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type PiOAuth struct {
	Type      string `json:"type"`
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	Expires   int64  `json:"expires"`
	AccountID string `json:"accountId"`
}

type ZedBlob struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAtMS  int64  `json:"expires_at_ms"`
	AccountID    string `json:"account_id"`
	Email        string `json:"email"`
}

func GrantFromCodex(f CodexFile) account.Grant {
	g := account.Grant{
		AccessToken:  f.Tokens.AccessToken,
		IDToken:      f.Tokens.IDToken,
		RefreshToken: f.Tokens.RefreshToken,
		AccountID:    f.Tokens.AccountID,
	}
	if t, err := time.Parse(time.RFC3339, f.LastRefresh); err == nil {
		g.LastRefresh = t.UTC()
	}
	return g.WithIdentity()
}

func CodexFromGrant(g account.Grant, now time.Time) CodexFile {
	g = g.WithIdentity()
	idToken := g.IDToken
	if idToken == "" {
		idToken = g.AccessToken
	}
	last := g.LastRefresh
	if last.IsZero() {
		last = lastRefreshFromAccess(g, now)
	}
	return CodexFile{
		AuthMode: "chatgpt",
		Tokens: CodexTokens{
			IDToken:      idToken,
			AccessToken:  g.AccessToken,
			RefreshToken: g.RefreshToken,
			AccountID:    g.Identity().AccountID,
		},
		LastRefresh: last.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func GrantFromPi(oauth PiOAuth) account.Grant {
	g := account.Grant{
		AccessToken:  oauth.Access,
		IDToken:      oauth.Access,
		RefreshToken: oauth.Refresh,
		AccountID:    oauth.AccountID,
	}
	if oauth.Expires > 0 {
		exp := time.UnixMilli(oauth.Expires).UTC()
		g.LastRefresh = exp.Add(-864000 * time.Second)
		if g.LastRefresh.Before(time.Unix(0, 0)) {
			g.LastRefresh = time.Unix(0, 0)
		}
	}
	return g.WithIdentity()
}

func PiFromGrant(g account.Grant, now time.Time) PiOAuth {
	g = g.WithIdentity()
	exp := g.AccessExpiry()
	var expires int64
	if !exp.IsZero() {
		expires = exp.UnixMilli()
	} else {
		expires = now.UnixMilli() + 864000*1000
	}
	return PiOAuth{
		Type:      "oauth",
		Access:    g.AccessToken,
		Refresh:   g.RefreshToken,
		Expires:   expires,
		AccountID: g.Identity().AccountID,
	}
}

func GrantFromZed(z ZedBlob) account.Grant {
	g := account.Grant{
		AccessToken:  z.AccessToken,
		IDToken:      z.AccessToken,
		RefreshToken: z.RefreshToken,
		AccountID:    z.AccountID,
	}
	if z.ExpiresAtMS > 0 {
		exp := time.UnixMilli(z.ExpiresAtMS).UTC()
		g.LastRefresh = exp.Add(-864000 * time.Second)
		if g.LastRefresh.Before(time.Unix(0, 0)) {
			g.LastRefresh = time.Unix(0, 0)
		}
	}
	return g.WithIdentity()
}

func ZedFromGrant(g account.Grant, now time.Time) ZedBlob {
	g = g.WithIdentity()
	id := g.Identity()
	exp := g.AccessExpiry()
	var expires int64
	if !exp.IsZero() {
		expires = exp.UnixMilli()
	} else {
		expires = now.UnixMilli() + 864000*1000
	}
	return ZedBlob{
		AccessToken:  g.AccessToken,
		RefreshToken: g.RefreshToken,
		ExpiresAtMS:  expires,
		AccountID:    id.AccountID,
		Email:        id.Email,
	}
}

func lastRefreshFromAccess(g account.Grant, now time.Time) time.Time {
	exp := g.AccessExpiry()
	if !exp.IsZero() {
		t := exp.Add(-864000 * time.Second)
		if t.Before(time.Unix(0, 0)) {
			return time.Unix(0, 0)
		}
		return t
	}
	if !g.LastRefresh.IsZero() {
		return g.LastRefresh
	}
	return now
}

func ParseBytes(data []byte) (account.Grant, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return account.Grant{}, err
	}
	if looksLikeZed(raw) {
		var z ZedBlob
		if err := json.Unmarshal(data, &z); err != nil {
			return account.Grant{}, err
		}
		return GrantFromZed(z), nil
	}
	if looksLikeCodex(raw) {
		var f CodexFile
		if err := json.Unmarshal(data, &f); err != nil {
			return account.Grant{}, err
		}
		return GrantFromCodex(f), nil
	}
	oauth, ok := piOAuthFrom(raw)
	if !ok {
		return account.Grant{}, account.ErrNotChatGPT
	}
	return GrantFromPi(oauth), nil
}

func ReadAnyFile(path string) (account.Grant, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return account.Grant{}, err
	}
	return ParseBytes(data)
}

func ReadCodexFile(path string) (account.Grant, error) {
	return ReadAnyFile(path)
}

func WriteCodexFile(path string, g account.Grant) error {
	now := time.Now()
	canonical := CodexFromGrant(g, now)
	existing, err := os.ReadFile(path)
	out := map[string]any{}
	if err == nil {
		_ = json.Unmarshal(existing, &out)
	}
	out["auth_mode"] = canonical.AuthMode
	out["tokens"] = canonical.Tokens
	if canonical.LastRefresh != "" {
		out["last_refresh"] = canonical.LastRefresh
	}
	return fileutil.WriteJSON(path, out)
}

func WritePiFile(path string, g account.Grant, now time.Time) error {
	return fileutil.OverlayJSON(path, "openai-codex", PiFromGrant(g, now))
}

func WriteOpenCodeFile(path string, g account.Grant, now time.Time) error {
	return fileutil.OverlayJSON(path, "openai", PiFromGrant(g, now))
}

func ReadPiFile(path string) (account.Grant, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return account.Grant{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return account.Grant{}, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	oauth, ok := piOAuthFrom(raw)
	if !ok {
		return account.Grant{}, account.ErrNotChatGPT
	}
	return GrantFromPi(oauth), nil
}

func ReadOpenCodeFile(path string) (account.Grant, error) {
	return ReadPiFile(path)
}

func looksLikeCodex(raw map[string]any) bool {
	tokens, _ := raw["tokens"].(map[string]any)
	if tokens == nil {
		return false
	}
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	return access != "" || refresh != ""
}

func looksLikeZed(raw map[string]any) bool {
	if looksLikeCodex(raw) {
		return false
	}
	if _, ok := piOAuthFrom(raw); ok {
		return false
	}
	access, _ := raw["access_token"].(string)
	refresh, _ := raw["refresh_token"].(string)
	return access != "" && refresh != ""
}

func piOAuthFrom(raw map[string]any) (PiOAuth, bool) {
	if asOAuth(raw) {
		return mapToPi(raw), true
	}
	for _, key := range []string{"openai-codex", "openai"} {
		nested, _ := raw[key].(map[string]any)
		if nested == nil {
			continue
		}
		access, _ := nested["access"].(string)
		refresh, _ := nested["refresh"].(string)
		if access != "" || refresh != "" {
			return mapToPi(nested), true
		}
	}
	return PiOAuth{}, false
}

func asOAuth(raw map[string]any) bool {
	typ, _ := raw["type"].(string)
	access, _ := raw["access"].(string)
	refresh, _ := raw["refresh"].(string)
	return typ == "oauth" && access != "" && refresh != ""
}

func mapToPi(raw map[string]any) PiOAuth {
	oauth := PiOAuth{
		Type:      stringField(raw["type"]),
		Access:    stringField(raw["access"]),
		Refresh:   stringField(raw["refresh"]),
		AccountID: stringField(raw["accountId"]),
	}
	switch n := raw["expires"].(type) {
	case float64:
		oauth.Expires = int64(n)
	case json.Number:
		v, _ := n.Int64()
		oauth.Expires = v
	}
	if oauth.Type == "" {
		oauth.Type = "oauth"
	}
	return oauth
}

func stringField(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func CompactJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
