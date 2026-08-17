package toolauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/webville-dev/codex-account/internal/account"
)

const (
	ZedLabel    = "zed-github-account"
	ZedURL      = "https://chatgpt.com/backend-api/codex"
	ZedUsername = "Bearer"
	secretWait  = 30 * time.Second
)

var (
	ErrZedMissing     = errors.New("zed credentials not found")
	ErrZedLocked      = errors.New("keyring locked")
	ErrZedMalformed   = errors.New("malformed zed credentials")
	ErrZedUnavailable = errors.New("secret-tool unavailable")
)

type CredentialStore interface {
	Get(ctx context.Context) (account.Grant, error)
	Put(ctx context.Context, g account.Grant) error
}

type SecretToolStore struct {
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, stdin []byte, args ...string) (stdout []byte, stderr []byte, err error)
	Now      func() time.Time
}

func NewSecretToolStore() *SecretToolStore {
	return &SecretToolStore{}
}

func (s *SecretToolStore) lookPath(name string) (string, error) {
	if s.LookPath != nil {
		return s.LookPath(name)
	}
	return exec.LookPath(name)
}

func (s *SecretToolStore) run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	if s.Run != nil {
		return s.Run(ctx, stdin, args...)
	}
	runCtx, cancel := context.WithTimeout(ctx, secretWait)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "secret-tool", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if runCtx.Err() != nil {
		err = runCtx.Err()
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func (s *SecretToolStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SecretToolStore) Get(ctx context.Context) (account.Grant, error) {
	if _, err := s.lookPath("secret-tool"); err != nil {
		return account.Grant{}, ErrZedUnavailable
	}
	stdout, stderr, err := s.run(ctx, nil, "lookup", "url", ZedURL, "username", ZedUsername)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return account.Grant{}, ErrZedLocked
		}
		if errors.Is(err, context.Canceled) {
			return account.Grant{}, err
		}
		detail := strings.TrimSpace(string(stderr))
		if looksLocked(detail) {
			return account.Grant{}, fmt.Errorf("%w: %s", ErrZedLocked, detail)
		}
		if detail == "" {
			return account.Grant{}, ErrZedMissing
		}
		return account.Grant{}, fmt.Errorf("%w: %s", ErrZedUnavailable, detail)
	}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return account.Grant{}, ErrZedMissing
	}
	g, err := ParseBytes(stdout)
	if err != nil {
		return account.Grant{}, ErrZedMalformed
	}
	if g.RequireLive() != nil {
		return account.Grant{}, ErrZedMalformed
	}
	return g, nil
}

func (s *SecretToolStore) Put(ctx context.Context, g account.Grant) error {
	if _, err := s.lookPath("secret-tool"); err != nil {
		return fmt.Errorf("%w: secret-tool is not on PATH. Install libsecret to manage Zed ChatGPT credentials", ErrZedUnavailable)
	}
	blob, err := CompactJSON(ZedFromGrant(g, s.now()))
	if err != nil {
		return err
	}
	_, stderr, err := s.run(ctx, blob, "store", "--label", ZedLabel, "url", ZedURL, "username", ZedUsername)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: secret-tool timed out. Unlock the keyring and retry", ErrZedLocked)
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		detail := bytes.TrimSpace(stderr)
		if len(detail) == 0 {
			detail = []byte("secret-tool store failed")
		}
		if looksLocked(string(detail)) {
			return fmt.Errorf("%w: %s", ErrZedLocked, detail)
		}
		return fmt.Errorf("failed to write Zed ChatGPT credentials: %s", detail)
	}
	return nil
}

func looksLocked(detail string) bool {
	detail = strings.ToLower(detail)
	return strings.Contains(detail, "locked") || strings.Contains(detail, "unlock")
}

type MemoryStore struct {
	Grant account.Grant
	Err   error
}

func (m *MemoryStore) Get(ctx context.Context) (account.Grant, error) {
	if m.Err != nil {
		return account.Grant{}, m.Err
	}
	if m.Grant.RefreshToken == "" {
		return account.Grant{}, ErrZedMissing
	}
	return m.Grant, nil
}

func (m *MemoryStore) Put(ctx context.Context, g account.Grant) error {
	if m.Err != nil {
		return m.Err
	}
	m.Grant = g
	return nil
}

func LiveGrant(g account.Grant, err error) (account.Grant, bool, error) {
	if errors.Is(err, ErrZedMissing) {
		return account.Grant{}, false, nil
	}
	if err != nil {
		return account.Grant{}, false, err
	}
	if err := g.RequireLive(); err != nil {
		return account.Grant{}, false, fmt.Errorf("invalid live credential: %w", err)
	}
	return g, true, nil
}

func FileLiveGrant(path string) (account.Grant, bool, error) {
	g, err := ReadAnyFile(path)
	if os.IsNotExist(err) {
		return account.Grant{}, false, nil
	}
	if errors.Is(err, account.ErrNotChatGPT) {
		return account.Grant{}, false, nil
	}
	if err != nil {
		return account.Grant{}, false, err
	}
	if err := g.RequireLive(); err != nil {
		return account.Grant{}, false, fmt.Errorf("invalid live credential in %s: %w", path, err)
	}
	return g, true, nil
}
