package account_test

import (
	"testing"
	"time"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/testutil"
)

func TestPayloadExtractsClaims(t *testing.T) {
	t.Parallel()
	token := testutil.JWT("workspace-one", "Person@Example.com", "plus", time.Unix(9999999999, 0))
	got := account.Payload(token)
	if got.AccountID != "workspace-one" || got.Email != "person@example.com" || got.Plan != "plus" {
		t.Fatalf("%+v", got)
	}
	if got.Expiry.Unix() != 9999999999 {
		t.Fatalf("exp %v", got.Expiry)
	}
}

func TestPayloadNeverPanics(t *testing.T) {
	t.Parallel()
	for _, tok := range []string{"", "a", "a.b", "a.b.c", "....", "eyJ.!!! .x"} {
		_ = account.Payload(tok)
	}
}

func FuzzPayload(f *testing.F) {
	f.Add("")
	f.Add("a.b.c")
	f.Add(testutil.JWT("id", "a@b.co", "plus", time.Now()))
	f.Fuzz(func(t *testing.T, token string) {
		_ = account.Payload(token)
	})
}

func TestSlotNameCollisionIsStable(t *testing.T) {
	t.Parallel()
	first := testutil.Grant("workspace-one", "refresh-one", time.Now().Add(time.Hour))
	second := testutil.Grant("workspace-two", "refresh-two", time.Now().Add(time.Hour))
	firstName := account.SlotName(first, nil)
	if firstName != "person@example.com.business" {
		t.Fatalf("got %q", firstName)
	}
	existing := []account.ExistingAccount{{Name: firstName, AccountID: "workspace-one"}}
	secondName := account.SlotName(second, existing)
	if secondName == firstName {
		t.Fatal("expected distinct names")
	}
	if secondName != account.SlotName(second, existing) {
		t.Fatal("suffix must be stable")
	}
	if wantPrefix := "person@example.com.business."; len(secondName) <= len(wantPrefix) || secondName[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("got %q", secondName)
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()
	ok := []string{"work", "person@example.com", "person@example.com.plus", "a_b-c.d"}
	for _, name := range ok {
		if err := account.ValidateName(account.NormalizeName(name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	bad := []string{"", "a/b", "foo..bar", "weird name", "x-backup"}
	for _, name := range bad {
		if err := account.ValidateName(name); err == nil {
			t.Fatalf("expected error for %q", name)
		}
	}
}

func FuzzValidateName(f *testing.F) {
	f.Add("work")
	f.Add("a@b.co")
	f.Fuzz(func(t *testing.T, name string) {
		_ = account.ValidateName(name)
	})
}

func TestResolveSavedName(t *testing.T) {
	t.Parallel()
	names := []string{"person@example.com.business", "person@example.com.plus"}
	if got, ok := account.ResolveSavedName("person@example.com.business", names); !ok || got != names[0] {
		t.Fatalf("exact: %q %v", got, ok)
	}
	if _, ok := account.ResolveSavedName("person@example.com", names); ok {
		t.Fatal("ambiguous shorthand")
	}
	if got, ok := account.ResolveSavedName("other", []string{"other.plus"}); !ok || got != "other.plus" {
		t.Fatalf("shorthand: %q %v", got, ok)
	}
}
