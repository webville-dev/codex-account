package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/webville-dev/codex-account/internal/account"
	"github.com/webville-dev/codex-account/internal/oauth"
	"github.com/webville-dev/codex-account/internal/platform"
	"github.com/webville-dev/codex-account/internal/settings"
	"github.com/webville-dev/codex-account/internal/store"
	"github.com/webville-dev/codex-account/internal/toolauth"
	"github.com/webville-dev/codex-account/internal/usage"
)

const refreshSkew = 120 * time.Second

var (
	agents     = []string{"pi", "codex", "opencode", "zed"}
	writeOrder = []string{"zed", "codex", "pi", "opencode"}
)

type Service struct {
	Paths       platform.Paths
	Store       store.Store
	Zed         toolauth.CredentialStore
	Refresher   oauth.TokenRefresher
	UsageClient *usage.Client
	OAuth       *oauth.Client
	Runner      platform.Runner
	Clock       platform.Clock
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

func New(cfg Service) *Service {
	s := cfg
	s.Store.Paths = s.Paths
	if s.Clock == nil {
		s.Clock = platform.RealClock{}
	}
	if s.Runner == nil {
		s.Runner = platform.ExecRunner{}
	}
	if s.Zed == nil {
		s.Zed = toolauth.NewSecretToolStore()
	}
	if s.Refresher == nil {
		s.Refresher = oauth.NewClient()
	}
	if s.OAuth == nil {
		if c, ok := s.Refresher.(*oauth.Client); ok {
			s.OAuth = c
		} else {
			s.OAuth = oauth.NewClient()
		}
	}
	if s.UsageClient == nil {
		s.UsageClient = usage.NewClient()
	}
	if s.Stdin == nil {
		s.Stdin = os.Stdin
	}
	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if s.Stderr == nil {
		s.Stderr = os.Stderr
	}
	return &s
}

func (s *Service) now() time.Time { return s.Clock.Now() }

func (s *Service) PrimaryAgent() (string, error) {
	f, err := settings.Load(s.Paths.SettingsFile)
	if err != nil {
		return "", err
	}
	return f.PrimaryAgent, nil
}

func livePriority(primary string) []string {
	preferred := []string{primary, "pi", "opencode", "zed", "codex"}
	out := make([]string, 0, len(preferred))
	seen := map[string]struct{}{}
	for _, agent := range preferred {
		if _, ok := seen[agent]; ok {
			continue
		}
		seen[agent] = struct{}{}
		out = append(out, agent)
	}
	return out
}

func (s *Service) withLock(ctx context.Context, fn func() error) error {
	unlock, err := store.Lock(ctx, s.Paths.LockFile)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.Paths.CheckCodexStorage(); err != nil {
		return err
	}
	if err := s.Store.EnsureDirs(); err != nil {
		return err
	}
	if note, err := s.Store.RestoreCodexStash(); err != nil {
		return err
	} else if note != "" {
		fmt.Fprintln(s.Stdout, note)
	}
	return fn()
}

func (s *Service) saved() ([]account.ExistingAccount, map[string]account.Grant, error) {
	files, err := s.Store.ListFiles()
	if err != nil {
		return nil, nil, err
	}
	existing := make([]account.ExistingAccount, 0, len(files))
	grants := make(map[string]account.Grant, len(files))
	for _, f := range files {
		g, err := toolauth.ReadAnyFile(f.Path)
		if err != nil {
			continue
		}
		grants[f.Name] = g
		existing = append(existing, account.ExistingAccount{
			Name:      f.Name,
			AccountID: g.Identity().AccountID,
		})
	}
	return existing, grants, nil
}

func (s *Service) live(ctx context.Context, agent string) (account.Grant, bool, error) {
	switch agent {
	case "zed":
		g, err := s.Zed.Get(ctx)
		return toolauth.LiveGrant(g, err)
	case "pi":
		return toolauth.FileLiveGrant(s.Paths.PiAuth)
	case "codex":
		return toolauth.FileLiveGrant(s.Paths.CodexAuth)
	case "opencode":
		return toolauth.FileLiveGrant(s.Paths.OpenCodeAuth)
	default:
		return account.Grant{}, false, fmt.Errorf("unknown agent %q", agent)
	}
}

func (s *Service) writeLive(ctx context.Context, agent string, g account.Grant) error {
	now := s.now()
	switch agent {
	case "zed":
		return s.Zed.Put(ctx, g)
	case "pi":
		return toolauth.WritePiFile(s.Paths.PiAuth, g, now)
	case "codex":
		return toolauth.WriteCodexFile(s.Paths.CodexAuth, g)
	case "opencode":
		return toolauth.WriteOpenCodeFile(s.Paths.OpenCodeAuth, g, now)
	default:
		return fmt.Errorf("unknown agent %q", agent)
	}
}

func (s *Service) writeAll(ctx context.Context, g account.Grant) error {
	if err := g.RequireLive(); err != nil {
		return fmt.Errorf("refusing to distribute invalid credential: %w", err)
	}
	for _, agent := range writeOrder {
		if err := s.writeLive(ctx, agent, g); err != nil {
			return fmt.Errorf("%s: %w", agent, err)
		}
	}
	return nil
}

func (s *Service) snapshotLives(ctx context.Context) ([]string, error) {
	existing, _, err := s.saved()
	if err != nil {
		return nil, err
	}
	var saved []string
	seen := map[string]struct{}{}
	primary, err := s.PrimaryAgent()
	if err != nil {
		return nil, err
	}
	for _, agent := range livePriority(primary) {
		g, ok, err := s.live(ctx, agent)
		if err != nil {
			return saved, fmt.Errorf("read %s login: %w", agent, err)
		}
		if !ok {
			continue
		}
		name := account.SlotName(g, existing)
		if name == "" {
			continue
		}
		path := s.Store.SavedPath(name)
		if cur, err := toolauth.ReadAnyFile(path); err == nil {
			// The configured primary is visited first. Preserve the earlier
			// grant unless this source is strictly fresher.
			if !g.AccessExpiry().After(cur.AccessExpiry()) {
				if _, dup := seen[name]; !dup {
					saved = append(saved, name)
					seen[name] = struct{}{}
				}
				continue
			}
		}
		if err := toolauth.WriteCodexFile(path, g); err != nil {
			return saved, err
		}
		existing = append(existing, account.ExistingAccount{Name: name, AccountID: g.Identity().AccountID})
		if _, dup := seen[name]; !dup {
			saved = append(saved, name)
			seen[name] = struct{}{}
		}
	}
	return saved, nil
}

func (s *Service) updateMatchingSaves(g account.Grant) ([]string, error) {
	existing, grants, err := s.saved()
	if err != nil {
		return nil, err
	}
	var updated []string
	seen := map[string]struct{}{}
	wanted := account.SlotName(g, existing)
	if wanted != "" {
		if err := toolauth.WriteCodexFile(s.Store.SavedPath(wanted), g); err != nil {
			return nil, err
		}
		updated = append(updated, wanted)
		seen[wanted] = struct{}{}
	}
	id := g.Identity().AccountID
	if id == "" {
		return updated, nil
	}
	for name, saved := range grants {
		if _, ok := seen[name]; ok {
			continue
		}
		if saved.Identity().AccountID == id {
			if err := toolauth.WriteCodexFile(s.Store.SavedPath(name), g); err != nil {
				return updated, err
			}
			updated = append(updated, name)
		}
	}
	return updated, nil
}

func (s *Service) resolveName(name string) (string, error) {
	resolved, ok := account.ResolveSavedName(name, s.Store.Names())
	if !ok {
		return "", fmt.Errorf("unknown account %q", name)
	}
	return resolved, nil
}

func (s *Service) CompleteNames() []string {
	return s.Store.Names()
}

func (s *Service) loadPending() (account.Grant, bool, error) {
	if !s.Store.PendingExists() {
		return account.Grant{}, false, nil
	}
	g, err := toolauth.ReadCodexFile(s.Paths.PendingFile)
	if err != nil || g.RequireLive() != nil {
		return account.Grant{}, false, fmt.Errorf("cannot read refresh recovery grant at %s; repair or remove it before syncing", s.Paths.PendingFile)
	}
	return g, true, nil
}

func (s *Service) maybeWriteLive(ctx context.Context, agent string, live account.Grant, present bool, g account.Grant, fillMissing bool) (bool, error) {
	if !present {
		if !fillMissing {
			return false, nil
		}
		return true, s.writeLive(ctx, agent, g)
	}
	if live.SameAccount(g) {
		return true, s.writeLive(ctx, agent, g)
	}
	return false, nil
}

func needsRefresh(g account.Grant, now time.Time) bool {
	if g.AccessToken == "" {
		return true
	}
	exp := g.AccessExpiry()
	if exp.IsZero() {
		return false
	}
	return !exp.After(now.Add(refreshSkew))
}
