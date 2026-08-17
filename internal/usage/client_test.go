package usage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nyashachiroro.com/codex-account/internal/usage"
)

func TestFetchUnauthorizedThenOK(t *testing.T) {
	t.Parallel()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("User-Agent") != "codex-cli" {
			t.Errorf("headers %+v", r.Header)
		}
		n++
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus"})
	}))
	defer srv.Close()
	c := usage.NewClient()
	c.HTTP = srv.Client()
	c.URL = srv.URL
	status, payload, err := c.Fetch(context.Background(), "token", "acct")
	if err != nil || status != 401 {
		t.Fatalf("first %d %v %v", status, payload, err)
	}
	status, payload, err = c.Fetch(context.Background(), "token", "acct")
	if err != nil || status != 200 || payload["plan_type"] != "plus" {
		t.Fatalf("second %d %v %v", status, payload, err)
	}
}
