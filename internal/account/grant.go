package account

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNoAccountID    = errors.New("missing chatgpt account id")
	ErrNoRefreshToken = errors.New("missing refresh token")
	ErrNoAccessToken  = errors.New("missing access token")
	ErrNotChatGPT     = errors.New("not a ChatGPT Codex login")
)

// Grant is the in-memory ChatGPT Codex credential. Tool adapters convert
// their native files to and from this type.
type Grant struct {
	AccessToken  string
	IDToken      string
	RefreshToken string
	AccountID    string
	LastRefresh  time.Time
}

type Identity struct {
	AccountID string
	Email     string
	Plan      string
	ClientID  string
}

func (g Grant) Identity() Identity {
	id := Identity{AccountID: strings.TrimSpace(g.AccountID)}
	for _, token := range []string{g.IDToken, g.AccessToken} {
		p := Payload(token)
		if id.AccountID == "" {
			id.AccountID = p.AccountID
		}
		if id.Plan == "" {
			id.Plan = p.Plan
		}
		if id.Email == "" {
			id.Email = p.Email
		}
		if id.ClientID == "" {
			id.ClientID = p.ClientID
		}
	}
	if id.Plan == "" {
		id.Plan = "unknown"
	}
	return id
}

func (g Grant) AccessExpiry() time.Time {
	return Payload(g.AccessToken).Expiry
}

func (g Grant) Valid() error {
	if strings.TrimSpace(g.RefreshToken) == "" {
		return ErrNoRefreshToken
	}
	if strings.TrimSpace(g.AccountID) == "" && strings.TrimSpace(g.Identity().AccountID) == "" {
		return ErrNoAccountID
	}
	return nil
}

func (g Grant) RequireLive() error {
	if err := g.Valid(); err != nil {
		return err
	}
	if strings.TrimSpace(g.AccessToken) == "" {
		return ErrNoAccessToken
	}
	id := g.Identity()
	if id.AccountID == "" {
		return ErrNoAccountID
	}
	g.AccountID = id.AccountID
	return nil
}

func (g Grant) WithIdentity() Grant {
	id := g.Identity()
	if g.AccountID == "" {
		g.AccountID = id.AccountID
	}
	if g.IDToken == "" {
		g.IDToken = g.AccessToken
	}
	return g
}

func (g Grant) ApplyRefresh(access, idToken, refresh string, expiresIn int, now time.Time) Grant {
	if access != "" {
		g.AccessToken = access
	}
	if idToken != "" {
		g.IDToken = idToken
	} else if access != "" && g.IDToken == "" {
		g.IDToken = access
	}
	if refresh != "" {
		g.RefreshToken = refresh
	}
	g.LastRefresh = now.UTC().Truncate(time.Second)
	id := g.Identity()
	if id.AccountID != "" {
		g.AccountID = id.AccountID
	}
	return g
}

func (g Grant) SameAccount(other Grant) bool {
	left := g.Identity().AccountID
	right := other.Identity().AccountID
	return left != "" && left == right
}
