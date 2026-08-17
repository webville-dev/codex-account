package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nyashachiroro.com/codex-account/internal/fileutil"
	"nyashachiroro.com/codex-account/internal/platform"
)

type Store struct {
	Paths platform.Paths
}

type NamedPath struct {
	Name string
	Path string
}

func (s Store) SavedPath(name string) string {
	return filepath.Join(s.Paths.AccountsHome, name+".json")
}

func (s Store) EnsureDirs() error {
	for _, dir := range []string{s.Paths.AccountDir, s.Paths.AccountsHome} {
		if err := fileutil.MkdirSecret(dir); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) Current() (string, bool, error) {
	data, err := os.ReadFile(s.Paths.CurrentFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	name := strings.TrimSpace(string(data))
	return name, name != "", nil
}

func (s Store) SetCurrent(name string) error {
	return fileutil.WriteFile(s.Paths.CurrentFile, []byte(name+"\n"), fileutil.FileMode)
}

func (s Store) ClearCurrentIf(name string) error {
	cur, ok, err := s.Current()
	if err != nil || !ok {
		return err
	}
	if cur != name {
		return nil
	}
	return os.Remove(s.Paths.CurrentFile)
}

func (s Store) RemoveGrant(name string) error {
	return os.Remove(s.SavedPath(name))
}

func (s Store) ListFiles() ([]NamedPath, error) {
	seen := map[string]struct{}{}
	var out []NamedPath
	add := func(name, path string) {
		if _, ok := seen[name]; ok {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seen[name] = struct{}{}
		out = append(out, NamedPath{Name: name, Path: path})
	}

	entries, err := os.ReadDir(s.Paths.AccountsHome)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".auth.json") {
			continue
		}
		add(strings.TrimSuffix(e.Name(), ".json"), filepath.Join(s.Paths.AccountsHome, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s Store) Names() []string {
	files, _ := s.ListFiles()
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	return names
}

func (s Store) PendingExists() bool {
	_, err := os.Stat(s.Paths.PendingFile)
	return err == nil
}

func (s Store) ClearPending() error {
	err := os.Remove(s.Paths.PendingFile)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s Store) RequireNoPending(action string) error {
	if !s.PendingExists() {
		return nil
	}
	return fmt.Errorf("cannot %s while a refresh recovery grant exists at %s; run 'codex-account sync' first", action, s.Paths.PendingFile)
}

func (s Store) HideCodexAuth() (bool, error) {
	if _, err := os.Stat(s.Paths.CodexAuth); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.Rename(s.Paths.CodexAuth, s.Paths.CodexStash); err != nil {
		return false, err
	}
	_ = os.Chmod(s.Paths.CodexStash, fileutil.FileMode)
	return true, nil
}

func (s Store) RestoreCodexStash() (string, error) {
	if _, err := os.Stat(s.Paths.CodexStash); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	// The stash is the rollback marker. If it still exists, the previous login
	// was never committed, so restore it even if an attempted auth file exists.
	if err := os.Rename(s.Paths.CodexStash, s.Paths.CodexAuth); err != nil {
		return "", err
	}
	_ = os.Chmod(s.Paths.CodexAuth, fileutil.FileMode)
	return "Restored Codex auth left behind by an interrupted login.", nil
}

func (s Store) UnhideCodexAuth() error {
	if _, err := os.Stat(s.Paths.CodexStash); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Rename atomically replaces any partial auth file left by a failed login.
	if err := os.Rename(s.Paths.CodexStash, s.Paths.CodexAuth); err != nil {
		return err
	}
	return os.Chmod(s.Paths.CodexAuth, fileutil.FileMode)
}

func (s Store) DropCodexStash() error {
	err := os.Remove(s.Paths.CodexStash)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
