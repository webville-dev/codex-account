package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Env struct {
	Getenv func(string) string
	Home   string
}

type Paths struct {
	Home         string
	CodexHome    string
	CodexAuth    string
	CodexConfig  string
	PiHome       string
	PiAuth       string
	OpenCodeHome string
	OpenCodeAuth string
	AccountDir   string
	AccountsHome string
	CurrentFile  string
	PendingFile  string
	CodexStash   string
	LockFile     string
}

func Resolve(env Env) (Paths, error) {
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	home := env.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
	}

	codexHome := firstNonEmpty(getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	piHome := firstNonEmpty(getenv("PI_CODING_AGENT_DIR"), filepath.Join(home, ".pi", "agent"))
	xdgData := firstNonEmpty(getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	openCodeHome := firstNonEmpty(getenv("OPENCODE_DATA"), filepath.Join(xdgData, "opencode"))
	accountDir := firstNonEmpty(getenv("CODEX_ACCOUNT_DIR"), filepath.Join(codexHome, ".codex-account"))
	accountsHome := firstNonEmpty(getenv("CODEX_ACCOUNTS_HOME"), filepath.Join(accountDir, "accounts"))
	storeParent := filepath.Dir(accountsHome)

	return Paths{
		Home:         home,
		CodexHome:    codexHome,
		CodexAuth:    filepath.Join(codexHome, "auth.json"),
		CodexConfig:  filepath.Join(codexHome, "config.toml"),
		PiHome:       piHome,
		PiAuth:       filepath.Join(piHome, "auth.json"),
		OpenCodeHome: openCodeHome,
		OpenCodeAuth: filepath.Join(openCodeHome, "auth.json"),
		AccountDir:   accountDir,
		AccountsHome: accountsHome,
		CurrentFile:  filepath.Join(accountDir, ".current"),
		PendingFile:  filepath.Join(storeParent, ".pending-refresh.json"),
		CodexStash:   filepath.Join(accountDir, ".live-codex-stash.json"),
		LockFile:     filepath.Join(storeParent, ".lock"),
	}, nil
}

func (p Paths) CheckCodexStorage() error {
	data, err := os.ReadFile(p.CodexConfig)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot read Codex credential storage setting from %s: %w", p.CodexConfig, err)
	}
	var cfg struct {
		Store any `toml:"cli_auth_credentials_store"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("cannot read Codex credential storage setting from %s: %w", p.CodexConfig, err)
	}
	mode := "file"
	switch v := cfg.Store.(type) {
	case nil:
		mode = "file"
	case string:
		mode = v
	default:
		mode = fmt.Sprint(v)
	}
	if mode != "file" {
		return fmt.Errorf("codex-account requires cli_auth_credentials_store = \"file\" in %s; found %q", p.CodexConfig, mode)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
