package fileutil_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nyashachiroro.com/codex-account/internal/fileutil"
)

func TestWriteJSONModeAndContents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := fileutil.WriteJSON(path, map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil || got["k"] != "v" {
		t.Fatalf("%s %v", raw, err)
	}
}

type boom struct{}

func (boom) MarshalJSON() ([]byte, error) { return nil, errors.New("boom") }

func TestWriteJSONCleansTempOnFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fileutil.WriteJSON(path, boom{}); err == nil {
		t.Fatal("expected error")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "keep\n" {
		t.Fatalf("dest changed: %s", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "auth.json" {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}

func TestOverlayPreservesOtherKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := fileutil.WriteJSON(path, map[string]any{"keep": "yes"}); err != nil {
		t.Fatal(err)
	}
	if err := fileutil.OverlayJSON(path, "openai-codex", map[string]string{"type": "oauth"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["keep"] != "yes" {
		t.Fatalf("%v", got)
	}
}
