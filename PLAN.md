# Codex Account Go CLI

## Direction

The Go CLI is the sole implementation and source of truth. There is no
compatibility requirement for the former Fish/Python implementation or its
saved-account layouts. Users can authenticate again with `codex-account login`.

Do not add legacy migrations, Python parity harnesses, hidden helper commands,
or compatibility branches. New behavior should be specified and tested in Go.

## Goal

Provide one safe executable for saving and distributing a ChatGPT Codex grant
across Pi, Codex, OpenCode, and Zed. A user logs in once through Pi, Codex, or
OpenCode; this tool converts that grant into each tool's expected format.

## Architecture

```text
cmd/codex-account/   process entry point
internal/cli/        Cobra commands, flags, rendering, completion
internal/app/        workflows and safety invariants
internal/account/    canonical Grant, identity, JWT metadata, naming
internal/toolauth/   Codex, Pi, OpenCode, and Zed adapters
internal/store/      account store, recovery, current pointer, lock, stash
internal/settings/   settings.json loading, defaults, and validation
internal/oauth/      browser/device login and refresh client
internal/usage/      usage HTTP client and response normalization
internal/platform/   paths, environment, subprocesses, clock
internal/fileutil/   atomic secret-file operations
```

Cobra handlers remain thin. They validate input, call `app.Service`, and render
results. Credential conversion, filesystem mutation, recovery, OAuth, and
Secret Service behavior belong outside `internal/cli`.

## Supported public interface

- `list`
- `current` (`status`)
- `save`
- `switch` (`use`)
- `login`
- `sync`
- `refresh`
- `usage` (`limits`, `quota`)
- `rm` (`remove`, `delete`)
- `completion`
- `version`

Environment overrides:

- `CODEX_HOME`
- `PI_CODING_AGENT_DIR`
- `XDG_DATA_HOME`
- `XDG_CONFIG_HOME`
- `OPENCODE_DATA`
- `CODEX_ACCOUNT_DIR`
- `CODEX_ACCOUNTS_HOME`

Codex credentials always live at `$CODEX_HOME/auth.json` and require
`cli_auth_credentials_store = "file"` when that setting is present.
Saved grants and settings default to `$XDG_CONFIG_HOME/codex-account`.
`settings.json` may select `pi`, `codex`, or `opencode` as `primaryAgent`; a
missing setting defaults to `pi`.

## Safety invariants

### Credential validity

- A live or distributable grant requires access, refresh, and account-ID data.
- Malformed live credentials are errors, not equivalent to missing credentials.
- Only a genuinely missing credential may be skipped.
- Pi and OpenCode writes preserve unrelated provider entries.
- Files are written atomically with mode `0600`; private directories use
  `0700`.

### Recovery

- After OpenAI rotates a refresh token, write `.pending-refresh.json`
  immediately before changing normal destinations.
- A valid pending recovery grant always wins `sync`, regardless of access-token
  expiry.
- Recovery is cleared only after every required destination and matching save
  succeeds.
- Login, switch, refresh, and refresh-capable usage operations are blocked while
  recovery is pending.
- Locked, unavailable, or malformed Zed credentials are failures and must not
  cause recovery to be cleared.

### Codex login transaction

- Snapshot current grants before invoking `codex login`.
- Rename the current Codex auth to a stash before login.
- The stash is a rollback marker: while it exists, the new login is not
  committed.
- On subprocess failure, interruption, missing output, or invalid output,
  restore the stash over any attempted auth file.
- Delete the stash only after the new Codex credential is fully validated.

### Concurrency

- Mutating workflows hold the account-store advisory lock.
- Read-only help and completion do not create directories, migrate data, open
  the keyring, or use the network.
- Network requests and subprocesses use contexts and bounded timeouts.

## Required tests

The permanent Go suite must cover:

- Every tool format and preservation of unrelated Pi/OpenCode providers.
- Missing, malformed, and incomplete live credentials.
- Workspace naming collisions and explicit alias conflicts.
- Atomic writes and permissions.
- Concurrent mutation lock exclusion.
- Sync freshness and configured-primary tie order for ordinary live grants.
- Unconditional recovery precedence over live grants.
- Corrupt and cross-workspace recovery refusal.
- Refresh failures retaining recovery.
- Zed missing, locked, malformed, unavailable, and timeout states.
- Saved-account refresh not creating missing live destinations.
- Usage refresh persistence and unauthorized retry.
- Browser OAuth state/PKCE and device polling/cancellation.
- Codex login success, subprocess failure, interruption, invalid output, and
  stash restoration over partial output.
- Cobra parsing, irrelevant flags, aliases, completion, and output formatting.
- Fuzzing JWT payloads, credential JSON, and account names.

Tests must use temporary homes, fake HTTP servers, injected runners, and fake
credential stores. They must never touch real credentials, Secret Service,
browsers, or OpenAI endpoints.

## Verification commands

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/codex-account
```

Before release, also run `gofmt`, inspect file permissions in an isolated home,
exercise a fake locked-keyring failure, and verify that errors never contain
access or refresh tokens.

## Release checklist

- [ ] Decide and configure the durable Git repository at the module path
      `nyashachiroro.com/codex-account`.
- [x] Add CI for test, race, vet, build, and formatting checks.
- [x] Build a versioned release binary.
- [ ] Test all commands with isolated homes and a disposable credential store.
- [ ] Back up real credentials before the first manual mutating test.
- [ ] Complete one real Pi, Codex, and OpenCode login.
- [ ] Complete one real refresh and recovery sync.
- [ ] Package the binary for dotfiles installation.
- [ ] Generate Fish completion with `codex-account completion fish`.
- [ ] Ensure no Fish function shadows the installed binary.
- [ ] Document uninstall and rollback procedures.

## Definition of done

- One Go executable owns all supported workflows.
- No Fish/Python runtime or legacy account migration is required.
- Recovery cannot be silently replaced by a stale live grant.
- Zed errors cannot be mistaken for an absent login during mutation.
- Failed Codex logins restore the previous credential.
- Invalid grants cannot be distributed.
- All unit, race, vet, build, isolated acceptance, and packaging checks pass.
