package account

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	emailName = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}(\.[A-Za-z][A-Za-z0-9]*)?$`)
	aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type ExistingAccount struct {
	Name      string
	AccountID string
}

func NormalizeName(name string) string {
	if strings.Contains(name, "@") {
		return strings.ToLower(name)
	}
	return name
}

func ValidateName(name string) error {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid account name %q", name)
	}
	if emailName.MatchString(name) {
		return nil
	}
	if !aliasName.MatchString(name) {
		return fmt.Errorf("invalid account name %q. Use an email, or letters, numbers, dots, dashes, or underscores", name)
	}
	if strings.HasSuffix(name, "-backup") {
		return fmt.Errorf("account names cannot end with '-backup'")
	}
	return nil
}

func PlanSlug(plan string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(plan)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func SlotBase(g Grant) string {
	id := g.Identity()
	if id.Email == "" {
		return ""
	}
	return id.Email + "." + PlanSlug(id.Plan)
}

// SlotName chooses email.plan, or a stable account-ID suffix when that name
// already belongs to a different workspace.
func SlotName(g Grant, existing []ExistingAccount) string {
	base := SlotBase(g)
	if base == "" {
		return ""
	}
	wantID := g.Identity().AccountID
	if owner, found := accountIDFor(existing, base); !found || owner == "" || owner == wantID {
		return base
	}
	digest := sha256.Sum256([]byte(wantID))
	hexDigest := hex.EncodeToString(digest[:])
	for _, n := range []int{8, 12, 16, len(hexDigest)} {
		candidate := base + "." + hexDigest[:n]
		if owner, found := accountIDFor(existing, candidate); !found || owner == wantID {
			return candidate
		}
	}
	return base + "." + hexDigest
}

func ResolveSavedName(name string, names []string) (string, bool) {
	for _, n := range names {
		if n == name {
			return n, true
		}
	}
	var matches []string
	prefix := name + "."
	for _, n := range names {
		if n == name || strings.HasPrefix(n, prefix) {
			matches = append(matches, n)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func PreferredSavedName(g Grant, existing []ExistingAccount) string {
	id := g.Identity().AccountID
	if id == "" {
		return ""
	}
	wanted := SlotName(g, existing)
	var names []string
	for _, e := range existing {
		if e.AccountID == id {
			names = append(names, e.Name)
		}
	}
	for _, n := range names {
		if n == wanted {
			return n
		}
	}
	email := g.Identity().Email
	for _, n := range names {
		if n == email {
			return n
		}
	}
	if email != "" {
		var prefixed []string
		p := email + "."
		for _, n := range names {
			if strings.HasPrefix(n, p) {
				prefixed = append(prefixed, n)
			}
		}
		if len(prefixed) == 1 {
			return prefixed[0]
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func Heading(name, plan, email string) string {
	slug := PlanSlug(plan)
	shown := name
	if shown == "" {
		shown = email
	}
	if shown == "" {
		shown = "unknown"
	}
	suffix := "." + slug
	if email != "" && (name == email || name == email+suffix) {
		shown = email
	} else if strings.HasSuffix(shown, suffix) && len(shown) > len(suffix) {
		shown = shown[:len(shown)-len(suffix)]
	}
	return shown + "  [" + slug + "]"
}

func accountIDFor(existing []ExistingAccount, name string) (string, bool) {
	for _, e := range existing {
		if e.Name == name {
			return e.AccountID, true
		}
	}
	return "", false
}
