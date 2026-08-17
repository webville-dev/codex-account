package store_test

import (
	"os"
	"testing"
	"time"

	"nyashachiroro.com/codex-account/internal/store"
	"nyashachiroro.com/codex-account/internal/testutil"
	"nyashachiroro.com/codex-account/internal/toolauth"
)

func TestRestoreCodexStashReplacesUncommittedAttempt(t *testing.T) {
	t.Parallel()
	home := testutil.NewHome(t)
	s := store.Store{Paths: home.Paths}
	old := testutil.Grant("workspace-one", "old-refresh", time.Now().Add(time.Hour))
	attempt := testutil.Grant("workspace-two", "attempt-refresh", time.Now().Add(2*time.Hour))
	if err := toolauth.WriteCodexFile(home.Paths.CodexStash, old); err != nil {
		t.Fatal(err)
	}
	if err := toolauth.WriteCodexFile(home.Paths.CodexAuth, attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreCodexStash(); err != nil {
		t.Fatal(err)
	}
	got, err := toolauth.ReadAnyFile(home.Paths.CodexAuth)
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "old-refresh" {
		t.Fatalf("rollback marker did not win: %+v", got)
	}
	if _, err := os.Stat(home.Paths.CodexStash); !os.IsNotExist(err) {
		t.Fatal("restored stash should no longer exist")
	}
}
