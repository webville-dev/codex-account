package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/webville-dev/codex-account/internal/account"
	"github.com/webville-dev/codex-account/internal/oauth"
	"github.com/webville-dev/codex-account/internal/platform"
	"github.com/webville-dev/codex-account/internal/toolauth"
	"github.com/webville-dev/codex-account/internal/usage"
)

type ListResult struct {
	Rows         []ListRow
	PrimaryAgent string
}

type ListRow struct {
	Name         string
	Plan         string
	Email        string
	LivePi       bool
	LiveCodex    bool
	LiveOpenCode bool
}

func (s *Service) List(ctx context.Context) (ListResult, error) {
	if err := s.Paths.CheckCodexStorage(); err != nil {
		return ListResult{}, err
	}
	primary, err := s.PrimaryAgent()
	if err != nil {
		return ListResult{}, err
	}
	existing, grants, err := s.saved()
	if err != nil {
		return ListResult{}, err
	}
	pi, piOK, err := s.live(ctx, "pi")
	if err != nil {
		return ListResult{}, fmt.Errorf("read Pi login: %w", err)
	}
	codex, codexOK, err := s.live(ctx, "codex")
	if err != nil {
		return ListResult{}, fmt.Errorf("read Codex login: %w", err)
	}
	opencode, opencodeOK, err := s.live(ctx, "opencode")
	if err != nil {
		return ListResult{}, fmt.Errorf("read OpenCode login: %w", err)
	}
	var piName, codexName, openCodeName string
	if piOK {
		piName = account.PreferredSavedName(pi, existing)
	}
	if codexOK {
		codexName = account.PreferredSavedName(codex, existing)
	}
	if opencodeOK {
		openCodeName = account.PreferredSavedName(opencode, existing)
	}
	files, _ := s.Store.ListFiles()
	rows := make([]ListRow, 0, len(files))
	for _, f := range files {
		g, ok := grants[f.Name]
		if !ok {
			continue
		}
		id := g.Identity()
		rows = append(rows, ListRow{
			Name:         f.Name,
			Plan:         id.Plan,
			Email:        id.Email,
			LivePi:       f.Name == piName,
			LiveCodex:    f.Name == codexName,
			LiveOpenCode: f.Name == openCodeName,
		})
	}
	return ListResult{Rows: rows, PrimaryAgent: primary}, nil
}

type CurrentResult struct {
	Lines  []string
	Shared string
}

func (s *Service) Current(ctx context.Context) (CurrentResult, error) {
	if err := s.Paths.CheckCodexStorage(); err != nil {
		return CurrentResult{}, err
	}
	existing, _, err := s.saved()
	if err != nil {
		return CurrentResult{}, err
	}
	type item struct {
		label string
		g     account.Grant
		ok    bool
		path  string
		zed   bool
	}
	items := []item{
		{"pi", account.Grant{}, false, s.Paths.PiAuth, false},
		{"codex", account.Grant{}, false, s.Paths.CodexAuth, false},
		{"opencode", account.Grant{}, false, s.Paths.OpenCodeAuth, false},
		{"zed", account.Grant{}, false, "secret-service:zed", true},
	}
	var present []account.Grant
	var lines []string
	for i, it := range items {
		g, ok, err := s.live(ctx, it.label)
		if err != nil {
			return CurrentResult{}, fmt.Errorf("read %s login: %w", it.label, err)
		}
		items[i].g, items[i].ok = g, ok
		if !ok {
			if it.zed {
				lines = append(lines, fmt.Sprintf("%-8s no ChatGPT Codex login", it.label))
			} else if _, err := os.Stat(it.path); err == nil {
				lines = append(lines, fmt.Sprintf("%-8s no ChatGPT Codex login", it.label))
			} else {
				lines = append(lines, fmt.Sprintf("%-8s no auth.json", it.label))
			}
			continue
		}
		present = append(present, g)
		id := g.Identity()
		name := account.PreferredSavedName(g, existing)
		shown := name
		if shown == "" {
			shown = "unsaved"
		}
		extra := strings.TrimSpace(id.Plan + " " + id.Email)
		lines = append(lines, fmt.Sprintf("%-8s %s (%s)", it.label, shown, extra))
	}
	shared := "shared no"
	if len(present) >= 2 {
		ids := map[string]struct{}{}
		refreshes := map[string]struct{}{}
		for _, g := range present {
			ids[g.Identity().AccountID] = struct{}{}
			refreshes[g.RefreshToken] = struct{}{}
		}
		switch {
		case len(ids) > 1:
			shared = "shared no (different accounts; switch NAME to share)"
		case len(refreshes) == 1:
			shared = "shared yes"
		default:
			shared = "shared same account, tokens drifted (run sync)"
		}
	}
	return CurrentResult{Lines: lines, Shared: shared}, nil
}

type SaveOptions struct {
	From string
	Name string
}

type SaveResult struct {
	Message string
	Name    string
}

func (s *Service) Save(ctx context.Context, opts SaveOptions) (SaveResult, error) {
	var result SaveResult
	err := s.withLock(ctx, func() error {
		from := opts.From
		fallback := false
		if from == "" {
			if opts.Name == "" {
				from = "both"
			} else {
				primary, err := s.PrimaryAgent()
				if err != nil {
					return err
				}
				from = primary
				fallback = true
			}
		}
		if from == "both" {
			saved, err := s.snapshotLives(ctx)
			if err != nil {
				return err
			}
			if len(saved) == 0 {
				return fmt.Errorf("no ChatGPT Codex login in Pi, Codex, OpenCode, or Zed")
			}
			result.Message = "Saved " + strings.Join(saved, ", ") + "."
			return nil
		}
		g, ok, err := s.liveFrom(ctx, from, fallback)
		if err != nil {
			return err
		}
		if !ok {
			if from == "zed" {
				return fmt.Errorf("no ChatGPT Codex login in Zed")
			}
			path := s.livePath(from)
			return fmt.Errorf("no ChatGPT Codex login in %s", path)
		}
		name := opts.Name
		if name == "" {
			existing, _, err := s.saved()
			if err != nil {
				return err
			}
			name = account.SlotName(g, existing)
			if name == "" {
				return fmt.Errorf("no email in the token. Pass -n/--name")
			}
		}
		if cur, err := toolauth.ReadAnyFile(s.Store.SavedPath(name)); err == nil {
			curID := cur.Identity().AccountID
			if curID != "" && curID != g.Identity().AccountID {
				return fmt.Errorf("saved account name %q already belongs to a different ChatGPT workspace", name)
			}
		}
		if err := toolauth.WriteCodexFile(s.Store.SavedPath(name), g); err != nil {
			return err
		}
		if opts.Name != "" {
			if err := s.Store.SetCurrent(name); err != nil {
				return err
			}
		}
		id := g.Identity()
		label := id.Email
		if label == "" {
			label = id.AccountID
		}
		result.Name = name
		result.Message = fmt.Sprintf("Saved '%s' (%s %s).", name, id.Plan, label)
		return nil
	})
	return result, err
}

func (s *Service) liveFrom(ctx context.Context, from string, fallback bool) (account.Grant, bool, error) {
	order := []string{from}
	if fallback {
		order = livePriority(from)
	}
	for _, agent := range order {
		g, ok, err := s.live(ctx, agent)
		if err != nil {
			return account.Grant{}, false, fmt.Errorf("read %s login: %w", agent, err)
		}
		if ok {
			return g, true, nil
		}
		if !fallback {
			break
		}
	}
	return account.Grant{}, false, nil
}

func (s *Service) livePath(agent string) string {
	switch agent {
	case "pi":
		return s.Paths.PiAuth
	case "codex":
		return s.Paths.CodexAuth
	case "opencode":
		return s.Paths.OpenCodeAuth
	default:
		return agent
	}
}

type SwitchResult struct {
	Message string
}

func (s *Service) Switch(ctx context.Context, name string) (SwitchResult, error) {
	var result SwitchResult
	err := s.withLock(ctx, func() error {
		if err := s.Store.RequireNoPending("switch accounts"); err != nil {
			return err
		}
		resolved, err := s.resolveName(name)
		if err != nil {
			return fmt.Errorf("unknown account %q. Save it first with 'codex-account save'", name)
		}
		if _, err := s.snapshotLives(ctx); err != nil {
			return err
		}
		g, err := toolauth.ReadAnyFile(s.Store.SavedPath(resolved))
		if err != nil {
			return err
		}
		if err := g.RequireLive(); err != nil {
			return fmt.Errorf("saved account %q is not a ChatGPT Codex login", resolved)
		}
		if err := s.writeAll(ctx, g); err != nil {
			return err
		}
		if err := s.Store.SetCurrent(resolved); err != nil {
			return err
		}
		id := g.Identity()
		label := id.Email
		if label == "" {
			label = id.AccountID
		}
		result.Message = fmt.Sprintf("Switched Pi, Codex, OpenCode, and Zed to '%s' (%s %s).\nRestart Pi/Codex/OpenCode/Zed if they are already running.", resolved, id.Plan, label)
		return nil
	})
	return result, err
}

type RemoveResult struct {
	Message string
}

func (s *Service) Remove(ctx context.Context, name string) (RemoveResult, error) {
	var result RemoveResult
	err := s.withLock(ctx, func() error {
		resolved, err := s.resolveName(name)
		if err != nil {
			return fmt.Errorf("unknown account %q", name)
		}
		if err := s.Store.RemoveGrant(resolved); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := s.Store.ClearCurrentIf(resolved); err != nil {
			return err
		}
		result.Message = fmt.Sprintf("Removed saved account '%s'.", resolved)
		return nil
	})
	return result, err
}

type SyncResult struct {
	Message string
}

func (s *Service) Sync(ctx context.Context) (SyncResult, error) {
	var result SyncResult
	err := s.withLock(ctx, func() error {
		lives := map[string]account.Grant{}
		for _, agent := range agents {
			g, ok, err := s.live(ctx, agent)
			if err != nil {
				return fmt.Errorf("read %s login: %w", agent, err)
			}
			if ok {
				lives[agent] = g
			}
		}
		pending, hasPending, err := s.loadPending()
		if err != nil {
			return err
		}
		if hasPending {
			pendingID := pending.Identity().AccountID
			var liveIDs []string
			for _, g := range lives {
				liveIDs = append(liveIDs, g.Identity().AccountID)
			}
			if len(liveIDs) > 0 {
				ok := false
				for _, id := range liveIDs {
					if id == pendingID {
						ok = true
						break
					}
				}
				if !ok {
					return fmt.Errorf("a pending refresh belongs to a different live account. Recovery grant kept at %s", s.Paths.PendingFile)
				}
			}
			lives["recovery"] = pending
		}
		primary, err := s.PrimaryAgent()
		if err != nil {
			return err
		}
		winnerName, winner, err := pickWinner(lives, primary)
		if err != nil {
			return err
		}
		livePresent := map[string]account.Grant{}
		for name, g := range lives {
			if name != "recovery" {
				livePresent[name] = g
			}
		}
		already := false
		if len(livePresent) > 0 {
			refs := map[string]struct{}{}
			for _, g := range livePresent {
				refs[g.RefreshToken] = struct{}{}
			}
			already = len(refs) == 1
		}
		if err := s.writeAll(ctx, winner); err != nil {
			return err
		}
		updated, err := s.updateMatchingSaves(winner)
		if err != nil {
			return err
		}
		if hasPending {
			if err := s.Store.ClearPending(); err != nil {
				return err
			}
		}
		id := winner.Identity()
		label := id.Email
		if label == "" {
			label = id.AccountID
		}
		if !hasPending && already && len(livePresent) == 4 {
			result.Message = fmt.Sprintf("Already in sync (%s).", label)
			return nil
		}
		saves := "none"
		if len(updated) > 0 {
			saves = strings.Join(updated, ", ")
		}
		result.Message = fmt.Sprintf("Synced from %s (%s) to Pi, Codex, OpenCode, and Zed. Updated saves: %s.", winnerName, label, saves)
		return nil
	})
	return result, err
}

func pickWinner(lives map[string]account.Grant, primary string) (string, account.Grant, error) {
	if len(lives) == 0 {
		return "", account.Grant{}, fmt.Errorf("no ChatGPT Codex login in Pi, Codex, OpenCode, or Zed")
	}
	ids := map[string]struct{}{}
	for _, g := range lives {
		ids[g.Identity().AccountID] = struct{}{}
	}
	if len(ids) > 1 {
		var parts []string
		for name, g := range lives {
			id := g.Identity()
			label := id.Email
			if label == "" {
				label = id.AccountID
			}
			if label == "" {
				label = "unknown"
			}
			parts = append(parts, name+"="+label)
		}
		sort.Strings(parts)
		return "", account.Grant{}, fmt.Errorf("Pi, Codex, OpenCode, and Zed are different ChatGPT accounts (%s). Use switch NAME to put the same login on all tools", strings.Join(parts, ", "))
	}
	if recovery, ok := lives["recovery"]; ok {
		return "recovery", recovery, nil
	}
	var bestName string
	var best account.Grant
	for name, g := range lives {
		if bestName == "" {
			bestName, best = name, g
			continue
		}
		if better(g, name, best, bestName, primary) {
			bestName, best = name, g
		}
	}
	return bestName, best, nil
}

func better(g account.Grant, name string, cur account.Grant, curName, primary string) bool {
	ge, ce := g.AccessExpiry(), cur.AccessExpiry()
	if ge.After(ce) {
		return true
	}
	if ce.After(ge) {
		return false
	}
	return liveRank(name, primary) < liveRank(curName, primary)
}

func liveRank(agent, primary string) int {
	for rank, candidate := range livePriority(primary) {
		if agent == candidate {
			return rank
		}
	}
	return len(agents)
}

type RefreshResult struct {
	Message string
}

func (s *Service) Refresh(ctx context.Context, name string) (RefreshResult, error) {
	var result RefreshResult
	err := s.withLock(ctx, func() error {
		if err := s.Store.RequireNoPending("refresh another grant"); err != nil {
			return err
		}
		var g account.Grant
		if name != "" {
			resolved, err := s.resolveName(name)
			if err != nil {
				return fmt.Errorf("unknown account %q", name)
			}
			name = resolved
			var loadErr error
			g, loadErr = toolauth.ReadAnyFile(s.Store.SavedPath(name))
			if loadErr != nil {
				return loadErr
			}
		} else {
			primary, err := s.PrimaryAgent()
			if err != nil {
				return err
			}
			var ok bool
			var liveErr error
			g, ok, liveErr = s.liveFrom(ctx, primary, true)
			if liveErr != nil {
				return liveErr
			}
			if !ok {
				return fmt.Errorf("no ChatGPT Codex login in Pi, Codex, OpenCode, or Zed")
			}
		}
		if g.RefreshToken == "" {
			return fmt.Errorf("selected login has no refresh token")
		}
		tok, err := s.Refresher.Refresh(ctx, g.RefreshToken)
		if err != nil {
			return err
		}
		g = oauth.ApplyRefresh(g, tok, s.now())
		if err := toolauth.WriteCodexFile(s.Paths.PendingFile, g); err != nil {
			return err
		}
		var failures []string
		updated, err := s.updateMatchingSaves(g)
		if err != nil {
			failures = append(failures, "saved grants: "+err.Error())
			updated = nil
		}
		var written []string
		for _, agent := range []string{"pi", "codex", "opencode", "zed"} {
			live, ok, liveErr := s.live(ctx, agent)
			if liveErr != nil {
				failures = append(failures, agent+": "+liveErr.Error())
				continue
			}
			if !ok || !live.SameAccount(g) {
				continue
			}
			if err := s.writeLive(ctx, agent, g); err != nil {
				failures = append(failures, agent+": "+err.Error())
				continue
			}
			written = append(written, agent)
		}
		if len(failures) > 0 {
			return fmt.Errorf("refresh succeeded but some destinations failed: %s. The fresh grant is safe at %s; fix the destination and run 'codex-account sync'", strings.Join(failures, "; "), s.Paths.PendingFile)
		}
		if err := s.Store.ClearPending(); err != nil {
			return err
		}
		id := g.Identity()
		label := id.Email
		if label == "" {
			label = id.AccountID
		}
		targets := append([]string{}, written...)
		targets = append(targets, updated...)
		if len(targets) == 0 {
			targets = []string{"saved only"}
		}
		result.Message = fmt.Sprintf("Refreshed %s (%s).", label, strings.Join(targets, ", "))
		return nil
	})
	return result, err
}

type UsageOptions struct {
	Agent string
	Name  string
	JSON  bool
}

type UsageRow struct {
	OK           bool               `json:"ok"`
	Agent        string             `json:"agent"`
	Name         string             `json:"name"`
	Email        string             `json:"email"`
	Plan         string             `json:"plan"`
	AccountID    string             `json:"account_id"`
	Refreshed    bool               `json:"refreshed"`
	Path         string             `json:"path"`
	Agents       []string           `json:"agents"`
	Live         bool               `json:"live"`
	Error        string             `json:"error,omitempty"`
	PlanType     string             `json:"plan_type,omitempty"`
	Allowed      any                `json:"allowed,omitempty"`
	LimitReached any                `json:"limit_reached,omitempty"`
	Windows      []usage.Window     `json:"windows,omitempty"`
	Additional   []usage.Additional `json:"additional,omitempty"`
	Credits      *usage.Credits     `json:"credits,omitempty"`
	SpendControl map[string]any     `json:"spend_control,omitempty"`
}

func (s *Service) Usage(ctx context.Context, opts UsageOptions) ([]UsageRow, error) {
	var rows []UsageRow
	err := s.withLock(ctx, func() error {
		if err := s.Store.RequireNoPending("query usage"); err != nil {
			return err
		}
		name := opts.Name
		if name != "" {
			resolved, err := s.resolveName(name)
			if err != nil {
				return fmt.Errorf("unknown account %q", name)
			}
			name = resolved
		}
		targets, err := s.usageTargets(ctx, opts.Agent, name)
		if err != nil {
			return err
		}
		for _, t := range targets {
			row := s.fetchUsage(ctx, t)
			row.Agents = t.agents
			row.Live = t.live
			row.Name = t.display
			if row.Email == "" && len(t.emails) > 0 {
				row.Email = t.emails[0]
			}
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		anyOK := false
		for _, r := range rows {
			if r.OK {
				anyOK = true
				break
			}
		}
		if !anyOK {
			return rows, ErrUsageFailed
		}
	}
	return rows, nil
}

var ErrUsageFailed = fmt.Errorf("usage failed")

type usageTarget struct {
	source  usageSource
	display string
	agents  []string
	live    bool
	emails  []string
}

type usageSource struct {
	agent     string
	name      string
	path      string
	live      bool
	accountID string
	userID    string
	email     string
	plan      string
	slot      string
	grant     account.Grant
}

func (s *Service) usageTargets(ctx context.Context, agentFilter, name string) ([]usageTarget, error) {
	if name != "" {
		path := s.Store.SavedPath(name)
		src, err := s.sourceIdentity("saved", path, name, false)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s", path)
		}
		display := src.slot
		if display == "" {
			display = src.email
		}
		if display == "" {
			display = name
		}
		return []usageTarget{{
			source:  src,
			display: display,
			agents:  []string{"saved"},
			emails:  []string{src.email},
		}}, nil
	}
	want := agents
	if agentFilter != "" {
		want = []string{agentFilter}
	}
	primary, err := s.PrimaryAgent()
	if err != nil {
		return nil, err
	}
	var sources []usageSource
	seen := map[string]struct{}{}
	for _, agent := range want {
		src, err := s.liveSource(ctx, agent)
		if err != nil {
			return nil, fmt.Errorf("read %s login: %w", agent, err)
		}
		if src.path == "" {
			continue
		}
		sources = append(sources, src)
		if agent != "zed" {
			if resolved, e := abs(src.path); e == nil {
				seen[resolved] = struct{}{}
			}
		}
	}
	files, _ := s.Store.ListFiles()
	for _, f := range files {
		resolved, err := abs(f.Path)
		if err == nil {
			if _, ok := seen[resolved]; ok {
				continue
			}
		}
		src, err := s.sourceIdentity("saved", f.Path, f.Name, false)
		if err != nil {
			continue
		}
		sources = append(sources, src)
		if resolved != "" {
			seen[resolved] = struct{}{}
		}
	}
	groups := map[string][]usageSource{}
	var order []string
	for _, src := range sources {
		key := usageGroupKey(src)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], src)
	}
	var targets []usageTarget
	for _, key := range order {
		grouped := groups[key]
		chosen := pickUsageSource(grouped, primary)
		var agentsPresent []string
		seenAgent := map[string]struct{}{}
		live := false
		for _, src := range grouped {
			if _, ok := seenAgent[src.agent]; !ok {
				seenAgent[src.agent] = struct{}{}
				agentsPresent = append(agentsPresent, src.agent)
			}
			if src.live {
				live = true
			}
		}
		sort.Strings(agentsPresent)
		display := chosen.slot
		if display == "" {
			display = chosen.email
		}
		if display == "" {
			display = chosen.name
		}
		if display == "" {
			display = "unknown"
		}
		if display == "live" {
			display = chosen.slot
			if display == "" {
				display = chosen.email
			}
			if display == "" {
				display = "unsaved"
			}
		}
		targets = append(targets, usageTarget{
			source:  chosen,
			display: display,
			agents:  agentsPresent,
			live:    live,
			emails:  []string{display},
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].live != targets[j].live {
			return targets[i].live
		}
		return targets[i].display < targets[j].display
	})
	if len(targets) == 0 {
		return nil, fmt.Errorf("no saved or live Codex accounts found")
	}
	return targets, nil
}

func usageGroupKey(src usageSource) string {
	switch {
	case src.userID != "" && src.accountID != "":
		return "user:" + src.userID + ":ws:" + src.accountID
	case src.userID != "":
		return "user:" + src.userID
	case src.email != "" && src.accountID != "":
		return "email:" + src.email + ":ws:" + src.accountID
	case src.email != "":
		return "email:" + src.email
	case src.name != "" && src.name != "live":
		return "name:" + src.name
	default:
		return "path:" + src.agent + ":" + src.path
	}
}

func pickUsageSource(sources []usageSource, primary string) usageSource {
	best := sources[0]
	for _, src := range sources[1:] {
		if usageRank(src, primary) < usageRank(best, primary) {
			best = src
		}
	}
	return best
}

func usageRank(src usageSource, primary string) string {
	live := "1"
	if src.live {
		live = "0"
	}
	pref := "1"
	if src.agent == primary {
		pref = "0"
	}
	return live + pref + src.name
}

func (s *Service) liveSource(ctx context.Context, agent string) (usageSource, error) {
	g, ok, err := s.live(ctx, agent)
	if err != nil {
		return usageSource{}, err
	}
	if !ok {
		return usageSource{}, nil
	}
	path := s.livePath(agent)
	if agent == "zed" {
		path = "secret-service:zed"
	}
	id := g.Identity()
	return usageSource{
		agent:     agent,
		name:      "live",
		path:      path,
		live:      true,
		accountID: id.AccountID,
		userID:    id.UserID,
		email:     id.Email,
		plan:      id.Plan,
		slot:      account.SlotBase(g),
		grant:     g,
	}, nil
}

func (s *Service) sourceIdentity(agent, path, name string, live bool) (usageSource, error) {
	g, err := toolauth.ReadAnyFile(path)
	if err != nil {
		return usageSource{}, err
	}
	id := g.Identity()
	return usageSource{
		agent:     agent,
		name:      name,
		path:      path,
		live:      live,
		accountID: id.AccountID,
		userID:    id.UserID,
		email:     id.Email,
		plan:      id.Plan,
		slot:      account.SlotBase(g),
		grant:     g,
	}, nil
}

func (s *Service) fetchUsage(ctx context.Context, t usageTarget) UsageRow {
	src := t.source
	row := UsageRow{
		Agent:     src.agent,
		Name:      t.display,
		Path:      src.path,
		Email:     src.email,
		Plan:      src.plan,
		AccountID: src.accountID,
	}
	g := src.grant
	if src.agent == "zed" || strings.Contains(src.path, "secret-service:zed") {
		if live, ok, err := s.live(ctx, "zed"); err == nil && ok {
			g = live
		} else if err != nil {
			row.Error = "read zed login: " + err.Error()
			return row
		}
	}
	var err error
	g, row.Refreshed, err = s.ensureFresh(ctx, src, g)
	if err != nil {
		row.Error = "refresh failed: " + err.Error()
		return row
	}
	status, payload, fetchErr := s.UsageClient.Fetch(ctx, g.AccessToken, g.Identity().AccountID)
	if status == 401 {
		tok, rerr := s.Refresher.Refresh(ctx, g.RefreshToken)
		if rerr != nil {
			row.Error = "usage unauthorized, refresh retry failed: " + rerr.Error()
			return row
		}
		g = oauth.ApplyRefresh(g, tok, s.now())
		row.Refreshed = true
		if err := s.persistUsageRefresh(ctx, src, g); err != nil {
			row.Error = "refresh failed: " + err.Error()
			return row
		}
		status, payload, fetchErr = s.UsageClient.Fetch(ctx, g.AccessToken, g.Identity().AccountID)
	}
	if status != 200 || payload == nil {
		if fetchErr != nil {
			row.Error = usage.ErrorFromPayload(status, payload, fetchErr)
		} else {
			row.Error = usage.ErrorFromPayload(status, payload, nil)
		}
		return row
	}
	rep := usage.Normalize(payload)
	id := g.Identity()
	row.OK = true
	row.Email = id.Email
	row.Plan = id.Plan
	row.AccountID = id.AccountID
	row.PlanType = rep.PlanType
	row.Allowed = rep.Allowed
	row.LimitReached = rep.LimitReached
	row.Windows = rep.Windows
	row.Additional = rep.Additional
	credits := rep.Credits
	row.Credits = &credits
	row.SpendControl = rep.SpendControl
	return row
}

func (s *Service) ensureFresh(ctx context.Context, src usageSource, g account.Grant) (account.Grant, bool, error) {
	if g.RefreshToken == "" {
		return g, false, fmt.Errorf("%s auth has no refresh token", src.agent)
	}
	if !needsRefresh(g, s.now()) {
		return g, false, nil
	}
	tok, err := s.Refresher.Refresh(ctx, g.RefreshToken)
	if err != nil {
		return g, false, err
	}
	g = oauth.ApplyRefresh(g, tok, s.now())
	if err := s.persistUsageRefresh(ctx, src, g); err != nil {
		return g, true, err
	}
	return g, true, nil
}

func (s *Service) persistUsageRefresh(ctx context.Context, src usageSource, g account.Grant) error {
	if err := toolauth.WriteCodexFile(s.Paths.PendingFile, g); err != nil {
		return err
	}
	if err := s.persistGrant(ctx, src, g); err != nil {
		return err
	}
	if err := s.syncRelated(ctx, src, g); err != nil {
		return err
	}
	return s.Store.ClearPending()
}

func (s *Service) persistGrant(ctx context.Context, src usageSource, g account.Grant) error {
	if src.agent == "zed" || src.path == "secret-service:zed" {
		return s.Zed.Put(ctx, g)
	}
	switch src.path {
	case s.Paths.PiAuth:
		return toolauth.WritePiFile(src.path, g, s.now())
	case s.Paths.CodexAuth:
		return toolauth.WriteCodexFile(src.path, g)
	case s.Paths.OpenCodeAuth:
		return toolauth.WriteOpenCodeFile(src.path, g, s.now())
	default:
		return toolauth.WriteCodexFile(src.path, g)
	}
}

func (s *Service) syncRelated(ctx context.Context, src usageSource, g account.Grant) error {
	id := g.Identity().AccountID
	if id == "" {
		return nil
	}
	for _, agent := range agents {
		if agent == "zed" && (src.agent == "zed" || src.path == "secret-service:zed") {
			continue
		}
		live, ok, err := s.live(ctx, agent)
		if err != nil {
			return fmt.Errorf("read %s login: %w", agent, err)
		}
		if !ok || live.Identity().AccountID != id {
			continue
		}
		if agent != "zed" && src.path == s.livePath(agent) {
			continue
		}
		if err := s.writeLive(ctx, agent, g); err != nil {
			return err
		}
	}
	_, err := s.updateMatchingSaves(g)
	return err
}

type LoginOptions struct {
	Agent  string
	Device bool
	Name   string
}

type LoginResult struct {
	Notes   []string
	Message string
}

func (s *Service) Login(ctx context.Context, opts LoginOptions) (LoginResult, error) {
	var result LoginResult
	err := s.withLock(ctx, func() error {
		if err := s.Store.RequireNoPending("login"); err != nil {
			return err
		}
		agent := opts.Agent
		if agent == "" {
			var err error
			agent, err = s.PrimaryAgent()
			if err != nil {
				return err
			}
		}
		agent = strings.ToLower(agent)
		switch agent {
		case "codex":
			if _, err := s.snapshotLives(ctx); err != nil {
				return err
			}
			return s.loginCodex(ctx, opts, &result)
		case "pi", "opencode":
			if _, err := s.snapshotLives(ctx); err != nil {
				return err
			}
			return s.loginOAuth(ctx, opts, &result, agent)
		default:
			return fmt.Errorf("login supports only 'pi', 'codex', or 'opencode'; the resulting grant is copied to every tool")
		}
	})
	return result, err
}

func (s *Service) loginOAuth(ctx context.Context, opts LoginOptions, result *LoginResult, agent string) error {
	label := "Pi openai-codex"
	home := s.Paths.PiHome
	others := "Codex, OpenCode, and Zed"
	writeFirst := func(g account.Grant) error {
		return toolauth.WritePiFile(s.Paths.PiAuth, g, s.now())
	}
	if agent == "opencode" {
		label = "OpenCode ChatGPT"
		home = s.Paths.OpenCodeHome
		others = "Pi, Codex, and Zed"
		writeFirst = func(g account.Grant) error {
			return toolauth.WriteOpenCodeFile(s.Paths.OpenCodeAuth, g, s.now())
		}
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if opts.Name != "" {
		result.Notes = append(result.Notes, fmt.Sprintf("Starting %s login for '%s'.", label, opts.Name))
	} else {
		result.Notes = append(result.Notes, fmt.Sprintf("Starting %s login. The slot will be named from the ChatGPT email.", label))
	}
	loginCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		loginCtx, cancel = context.WithTimeout(ctx, oauth.DefaultTimeout)
		defer cancel()
	}
	if s.OAuth.Prompt == nil {
		s.OAuth.Prompt = s.Stderr
	}
	if s.OAuth.OpenURL == nil {
		s.OAuth.OpenURL = s.openURL
	}
	originator := oauth.OriginatorPi
	if agent == "opencode" {
		originator = oauth.OriginatorOpenCode
	}
	var g account.Grant
	var err error
	if opts.Device {
		g, err = s.OAuth.DeviceLoginFor(loginCtx, originator)
	} else {
		g, err = s.OAuth.BrowserLoginFor(loginCtx, originator)
	}
	if err != nil {
		return err
	}
	if err := writeFirst(g); err != nil {
		return err
	}
	if err := s.writeAll(ctx, g); err != nil {
		return err
	}
	if err := s.saveAfterLogin(ctx, g, opts.Name); err != nil {
		return err
	}
	result.Message = fmt.Sprintf("Wrote the same grant to %s. Restart Pi/Codex/OpenCode/Zed if they are already running.", others)
	return nil
}

func (s *Service) loginCodex(ctx context.Context, opts LoginOptions, result *LoginResult) error {
	if _, err := s.Runner.LookPath("codex"); err != nil {
		return fmt.Errorf("'codex' is not on PATH")
	}
	hidden, err := s.Store.HideCodexAuth()
	if err != nil {
		return err
	}
	if hidden {
		result.Notes = append(result.Notes, "Hid the current Codex login so a new login will not revoke it.")
	}
	if opts.Name != "" {
		result.Notes = append(result.Notes, fmt.Sprintf("Starting Codex CLI login for '%s'.", opts.Name))
	} else {
		result.Notes = append(result.Notes, "Starting Codex CLI login. The slot will be named from the ChatGPT email.")
	}
	args := []string{"login"}
	if opts.Device {
		args = []string{"login", "--device-auth"}
	}
	runErr := s.Runner.Run(ctx, "codex", args, platform.RunOptions{
		Stdin:  s.Stdin,
		Stdout: s.Stdout,
		Stderr: s.Stderr,
	})
	restore := func(cause error) error {
		if !hidden {
			return cause
		}
		if restoreErr := s.Store.UnhideCodexAuth(); restoreErr != nil {
			return fmt.Errorf("%v; previous Codex login could not be restored: %w", cause, restoreErr)
		}
		result.Notes = append(result.Notes, "Login failed; restored the previous Codex login.")
		return cause
	}
	if runErr != nil {
		return restore(runErr)
	}
	g, ok, err := s.live(ctx, "codex")
	if err != nil {
		return restore(fmt.Errorf("read new Codex login: %w", err))
	}
	if !ok {
		return restore(fmt.Errorf("Codex login did not produce a ChatGPT Codex credential"))
	}
	if err := s.Store.DropCodexStash(); err != nil {
		return fmt.Errorf("commit new Codex login: %w", err)
	}
	if err := s.writeAll(ctx, g); err != nil {
		return err
	}
	if err := s.saveAfterLogin(ctx, g, opts.Name); err != nil {
		return err
	}
	result.Message = "Wrote the same grant to Pi, OpenCode, and Zed. Restart Pi/Codex/OpenCode/Zed if they are already running."
	return nil
}

func (s *Service) saveAfterLogin(ctx context.Context, g account.Grant, name string) error {
	explicit := name != ""
	if !explicit {
		existing, _, err := s.saved()
		if err != nil {
			return err
		}
		name = account.SlotName(g, existing)
		if name == "" {
			return fmt.Errorf("no email in the token. Pass -n/--name")
		}
	} else {
		if cur, err := toolauth.ReadAnyFile(s.Store.SavedPath(name)); err == nil {
			if curID := cur.Identity().AccountID; curID != "" && curID != g.Identity().AccountID {
				return fmt.Errorf("saved account name %q already belongs to a different ChatGPT workspace", name)
			}
		}
	}
	if err := toolauth.WriteCodexFile(s.Store.SavedPath(name), g); err != nil {
		return err
	}
	if explicit {
		return s.Store.SetCurrent(name)
	}
	return nil
}

func (s *Service) openURL(ctx context.Context, rawURL string) error {
	if s.Runner == nil {
		return nil
	}
	if _, err := s.Runner.LookPath("xdg-open"); err != nil {
		return nil
	}
	return s.Runner.Run(ctx, "xdg-open", []string{rawURL}, platform.RunOptions{
		Stdout: s.Stderr,
		Stderr: s.Stderr,
	})
}

func abs(path string) (string, error) {
	return filepath.Abs(path)
}

func FormatUsageHuman(rows []UsageRow) string {
	if len(rows) == 0 {
		return "No Codex usage targets.\n"
	}
	var b strings.Builder
	for _, row := range rows {
		prefix := " "
		if row.Live {
			prefix = "*"
		}
		plan := row.Plan
		if plan == "" {
			plan = row.PlanType
		}
		if plan == "" {
			plan = "unknown"
		}
		fmt.Fprintf(&b, "%s %s\n", prefix, account.Heading(row.Name, plan, row.Email))
		if !row.OK {
			err := row.Error
			if err == "" {
				err = "failed"
			}
			fmt.Fprintf(&b, "  error  %s\n\n", err)
			continue
		}
		if len(row.Windows) == 0 {
			fmt.Fprintf(&b, "  (no rate-limit windows)\n")
		}
		for _, w := range row.Windows {
			remaining := w.RemainingPercent
			remainText := "unknown"
			if remaining != nil {
				remainText = fmt.Sprintf("%d%% remaining", *remaining)
			}
			label := w.Label
			if label == "" {
				label = "window"
			}
			fmt.Fprintf(&b, "  %-8s %-16s resets in %s\n", label, remainText, usage.FormatDuration(w.ResetAfterSeconds))
		}
		if row.Credits != nil {
			if row.Credits.Unlimited {
				fmt.Fprintf(&b, "  credits  unlimited\n")
			} else if row.Credits.HasCredits && row.Credits.Balance != nil && row.Credits.Balance != "" && row.Credits.Balance != 0 && row.Credits.Balance != "0" {
				fmt.Fprintf(&b, "  credits  %v\n", row.Credits.Balance)
			}
		}
		for _, extra := range row.Additional {
			label := extra.LimitName
			if label == "" {
				label = extra.MeteredFeature
			}
			if label == "" {
				label = "extra"
			}
			if len(extra.Windows) > 0 {
				for _, w := range extra.Windows {
					remainText := "unknown"
					if w.RemainingPercent != nil {
						remainText = fmt.Sprintf("%d%% remaining", *w.RemainingPercent)
					}
					fmt.Fprintf(&b, "  %-8s %-16s resets in %s\n", label, remainText, usage.FormatDuration(w.ResetAfterSeconds))
				}
			} else if extra.LimitReached == true {
				fmt.Fprintf(&b, "  %-8s limit reached\n", label)
			}
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

func FormatUsageJSON(rows []UsageRow) (string, error) {
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
