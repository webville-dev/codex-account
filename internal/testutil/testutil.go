package testutil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/webville-dev/codex-account/internal/account"
	"github.com/webville-dev/codex-account/internal/platform"
)

func JWT(accountID, email, plan string, exp time.Time) string {
	header, _ := json.Marshal(map[string]string{"alg": "none"})
	payload, _ := json.Marshal(map[string]any{
		"exp":       exp.Unix(),
		"email":     email,
		"client_id": "app_EMoamEEZ73f0CkXaXp7hrann",
		"https://api.openai.com/auth": map[string]string{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  plan,
		},
	})
	enc := func(b []byte) string {
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(header) + "." + enc(payload) + ".sig"
}

func Grant(accountID, refresh string, exp time.Time) account.Grant {
	if exp.IsZero() {
		exp = time.Now().Add(time.Hour)
	}
	access := JWT(accountID, "person@example.com", "business", exp)
	return account.Grant{
		AccessToken:  access,
		IDToken:      access,
		RefreshToken: refresh,
		AccountID:    accountID,
		LastRefresh:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

type TestHome struct {
	Root  string
	Env   map[string]string
	Paths platform.Paths
}

func NewHome(t *testing.T) TestHome {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{
		"HOME":                root,
		"CODEX_HOME":          filepath.Join(root, "codex"),
		"PI_CODING_AGENT_DIR": filepath.Join(root, "pi"),
		"OPENCODE_DATA":       filepath.Join(root, "opencode"),
		"CODEX_ACCOUNT_DIR":   filepath.Join(root, "store"),
	}
	paths, err := platform.Resolve(platform.Env{
		Home:   root,
		Getenv: func(k string) string { return env[k] },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{paths.CodexHome, paths.PiHome, paths.OpenCodeHome, paths.AccountDir, paths.AccountsHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return TestHome{Root: root, Env: env, Paths: paths}
}

func EqualJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("want JSON: %v\n%s", err, want)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("JSON mismatch\ngot:  %s\nwant: %s", gb, wb)
	}
}

type FixedClock struct {
	T time.Time
}

func (c FixedClock) Now() time.Time { return c.T }

func (c FixedClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
