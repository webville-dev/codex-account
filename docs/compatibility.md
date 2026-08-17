# CLI behavior

The Go CLI is the sole implementation and source of truth. It does not migrate
or preserve saved-account layouts from the former Fish/Python implementation.
Existing accounts should be created again with `codex-account login`.

## Environment overrides

| Variable | Effect | Default |
| --- | --- | --- |
| `CODEX_HOME` | Codex home | `$HOME/.codex` |
| `PI_CODING_AGENT_DIR` | Pi agent home | `$HOME/.pi/agent` |
| `XDG_DATA_HOME` | OpenCode parent | `$HOME/.local/share` |
| `XDG_CONFIG_HOME` | Config parent for this tool | `$HOME/.config` |
| `OPENCODE_DATA` | OpenCode home (wins over XDG) | `$XDG_DATA_HOME/opencode` |
| `CODEX_ACCOUNT_DIR` | Account metadata directory | `$XDG_CONFIG_HOME/codex-account` |
| `CODEX_ACCOUNTS_HOME` | Saved grant directory | `$CODEX_ACCOUNT_DIR/accounts` |

Saved grants are one file per account:

```text
~/.config/codex-account/
  accounts/<name>.json
  settings.json          # {"primaryAgent":"pi"|"codex"|"opencode"}
  .current
  .pending-refresh.json
  .lock
```

`settings.json` is optional. Missing file or omitted `primaryAgent` means `pi`.
Allowed values are `pi`, `codex`, and `opencode` (the agents that can run OAuth).
`-a/--agent` on `login` still overrides the setting.
Unknown settings are rejected so spelling mistakes cannot silently change behavior.

Codex live credentials stay at `$CODEX_HOME/auth.json`. There is no `AUTH_FILE`.

`.current` lives in `CODEX_ACCOUNT_DIR`. `.pending-refresh.json` and the
mutation lock live next to `CODEX_ACCOUNTS_HOME` (its parent directory).

## Commands, aliases, flags

| Command | Aliases | Flags | Default | Mutates | Output |
| --- | --- | --- | --- | --- | --- |
| `help` | `-h`, `--help` | none | — | no | usage on stdout |
| `version` | | none | — | no | build metadata on stdout |
| `completion` | | `bash zsh fish powershell` | — | no | shell script on stdout |
| `list` | | none | — | no | saved accounts; `*` live primary, `p`/`c`/`o` the other OAuth agents; `(none)` if empty |
| `current` | `status` | none | — | no | per-tool line plus `shared` summary |
| `save` | | `-a/--agent/--from`, `--codex`, `--pi`, `--opencode`, `--zed`, `-n/--name` | all live tools when unnamed; primary agent when named | yes | `Saved ...` or `Saved a, b.` |
| `switch` | `use` | `-n/--name` or positional `NAME` | required name | yes | switched all four tools |
| `login` | | `-a/--agent pi\|codex\|opencode` (default `settings.primaryAgent`, else `pi`), `--device/--device-auth`, `-n/--name` | primary-agent OAuth | yes | notes on stderr/stdout; distributes to all four |
| `sync` | | none | — | yes | already-in-sync or synced-from source |
| `refresh` | | `-n/--name` or positional | live primary, then remaining tools | yes | refreshed targets |
| `usage` | `limits`, `quota` | `-a/--agent`, `-n/--name`, `--json`, `--all` | all distinct workspaces | maybe (token refresh) | human windows or JSON array |
| `rm` | `remove`, `delete` | `-n/--name` or positional `NAME` | required name | yes (saved only) | `Removed saved account 'NAME'.` |

An explicit source passed to `save` is exact and never falls back to another
tool. Automatic source selection starts with the primary agent and falls back
through the remaining tools. When grants are equally fresh, the primary agent
wins the tie.

`--name` and `--all` are mutually exclusive on `usage`. Login accepts `pi`,
`codex`, or `opencode`. Save `--from` accepts `pi`, `codex`, `opencode`, `zed`.

Cobra rejects unknown flags and extra positionals. Help and completion must not
create directories, migrate storage, or open the keyring.

Account-name completion on `switch`, `refresh`, `usage`, and `rm` reads saved
`*.json` filenames only.

## Account names

- Emails are lowercased.
- Default slot: `email.plan` where plan is `[a-z0-9]+` from the token.
- Same email+plan, different account ID: stable SHA-256 suffix (`8`, `12`, `16`, then full hex) of the account ID.
- Shorthand: exact filename, else unique `name.` prefix.
- Custom aliases are kept when created by this CLI.
- Refuse overwrite when the destination name belongs to a different workspace.
- Reject empty names, `..`, `/`, names ending in `-backup`, and characters outside `[A-Za-z0-9._-]` (emails have a separate pattern).

## Storage and permissions

| Path | Mode | Notes |
| --- | --- | --- |
| account dir, accounts home | `0700` | created as needed on mutating commands |
| saved grants, live auth files, `.current`, recovery | `0600` | atomic replace (temp + fsync + rename) |
| Codex `config.toml` | read | `cli_auth_credentials_store` must be `file` or absent |

Saved grants use the canonical Codex JSON object. Pi restore only replaces
`openai-codex`. OpenCode restore only replaces `openai`. Other provider keys
stay. Zed uses Linux Secret Service item `zed-github-account` with
`url=https://chatgpt.com/backend-api/codex` and `username=Bearer`.

## Live sync freshness

A valid recovery grant outranks every live copy. Without recovery, the newest
access-token `exp` wins; ties prefer the configured primary agent, then Pi,
OpenCode, Zed, and Codex with the primary removed from that fallback order.
Different ChatGPT account IDs refuse without writes. Corrupt recovery is left
in place with a repair error.

Refreshing a saved account updates matching existing live logins and matching
saves. It does not create missing live files.

Mutating commands (and usage when it refreshes) take one advisory store lock.
`list`, `current`, `help`, `completion`, and `version` do not.

## Failure conditions (strict)

| Condition | Behavior |
| --- | --- |
| Codex store is not `file` | refuse; do not touch credentials |
| Pending recovery exists | block login, switch, refresh, usage, and forced distribution; tell user to `sync` |
| Corrupt recovery file | refuse sync; do not delete the file |
| Recovery account ID mismatches live IDs | refuse; keep recovery |
| Destination write fails after refresh | keep recovery; instruct `sync` |
| Locked/unavailable Zed on switch | fail before local file writes |
| Cross-workspace live sync | refuse without writes |
| Cross-workspace save name | refuse overwrite |
| Ambiguous shorthand | unknown account |
| Every usage target fails | exit 1; print per-account errors |
| Tokens in errors | never print access or refresh tokens |

## Safety regression coverage

| Behavior | Go coverage |
| --- | --- |
| Workspace name collisions | `internal/account` naming test |
| Non-file Codex credential storage | `internal/platform` configuration test |
| Refresh failure and recovery sync | `internal/app` refresh/sync tests |
| Recovery always wins sync | `internal/app` recovery precedence test |
| Refresh does not create missing live destinations | `internal/app` refresh targeting test |
| Pending recovery blocks mutations | `internal/app` recovery guard test |
| Zed read/write failures retain recovery | `internal/app` failure-injection tests |
| Failed Codex login restores the previous auth | `internal/app` login rollback tests |
