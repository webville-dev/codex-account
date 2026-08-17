# codex-account

Save one ChatGPT Codex login and switch it across Pi, Codex, OpenCode, and Zed.

[![CI](https://github.com/webville-dev/codex-account/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/webville-dev/codex-account/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)

Log in once through Pi, Codex, or OpenCode. This CLI converts that grant into each tool's expected format, keeps named copies in `~/.config/codex-account/`, and can refresh or restore them without running `codex login` / `codex logout` by hand.

Linux only. Codex must store credentials in a file (`cli_auth_credentials_store = "file"`, which is the default). Zed uses Secret Service via `secret-tool`.

## Install

Go 1.26 or later:

```sh
go install github.com/webville-dev/codex-account/cmd/codex-account@latest
```

Or download a Linux `amd64` / `arm64` archive from [Releases](https://github.com/webville-dev/codex-account/releases), unpack `codex-account`, and put it on `PATH`.

From a clone:

```sh
git clone https://github.com/webville-dev/codex-account.git
cd codex-account
make build   # writes bin/codex-account
```

If a Fish function named `codex-account` still exists, remove or rename it so the binary wins.

## Usage

```sh
codex-account login                 # OAuth via settings.primaryAgent (default: pi)
codex-account login -a opencode     # or pi / codex
codex-account login --device        # device-code flow

codex-account list
codex-account current
codex-account switch NAME
codex-account usage
codex-account refresh
```

`list` marks the live primary login with `*`. `p` / `c` / `o` are the other OAuth agents.

Prefer this command over raw `codex login`. Do not run `codex logout` if you still want that saved account. Restart Pi, Codex, OpenCode, and Zed after switching.

Full flags, naming rules, and failure behavior: [docs/compatibility.md](docs/compatibility.md).

## Configuration

Optional `~/.config/codex-account/settings.json`:

```json
{ "primaryAgent": "pi" }
```

`primaryAgent` may be `pi`, `codex`, or `opencode`. It chooses the default login UI and wins freshness ties. `login -a` overrides it for one run.

Saved grants: `~/.config/codex-account/accounts/<name>.json`. Live tool files stay where those tools already put them (`$CODEX_HOME/auth.json`, Pi/OpenCode `auth.json`, Zed keyring).

## Development

```sh
make check     # fmt, vet, test, race, build
make test
make race
```

Releases are git tags (`v0.1.0`). See [docs/releasing.md](docs/releasing.md).
