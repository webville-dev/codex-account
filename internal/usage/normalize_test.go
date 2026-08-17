package usage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/webville-dev/codex-account/internal/usage"
)

func TestNormalizeFixtures(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "testdata", "usage")
	cases := map[string]struct {
		plan    string
		windows int
		credits bool
		extra   int
	}{
		"primary.json":    {plan: "plus", windows: 2},
		"credits.json":    {plan: "pro", windows: 1, credits: true},
		"additional.json": {plan: "business", windows: 1, extra: 1},
	}
	for name, want := range cases {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		got := usage.Normalize(payload)
		if got.PlanType != want.plan {
			t.Fatalf("%s plan %s", name, got.PlanType)
		}
		if len(got.Windows) != want.windows {
			t.Fatalf("%s windows %d", name, len(got.Windows))
		}
		if want.credits && !got.Credits.HasCredits {
			t.Fatalf("%s credits", name)
		}
		if len(got.Additional) != want.extra {
			t.Fatalf("%s extra %d", name, len(got.Additional))
		}
	}
}

func TestWindowLabelAndDuration(t *testing.T) {
	t.Parallel()
	if usage.FormatDuration(90061) != "1d 1h 1m" {
		t.Fatalf("%s", usage.FormatDuration(90061))
	}
	if usage.FormatDuration("nope") != "?" {
		t.Fatal("bad duration")
	}
}
