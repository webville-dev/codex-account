package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultURL = "https://chatgpt.com/backend-api/wham/usage"

type Client struct {
	HTTP *http.Client
	URL  string
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 30 * time.Second},
		URL:  DefaultURL,
	}
}

type Report struct {
	PlanType     string         `json:"plan_type"`
	Allowed      any            `json:"allowed"`
	LimitReached any            `json:"limit_reached"`
	Windows      []Window       `json:"windows"`
	Additional   []Additional   `json:"additional"`
	Credits      Credits        `json:"credits"`
	SpendControl map[string]any `json:"spend_control"`
}

type Window struct {
	Label              string `json:"label"`
	UsedPercent        any    `json:"used_percent"`
	RemainingPercent   *int   `json:"remaining_percent"`
	LimitWindowSeconds any    `json:"limit_window_seconds"`
	ResetAfterSeconds  any    `json:"reset_after_seconds"`
	ResetAt            any    `json:"reset_at"`
}

type Additional struct {
	LimitName      string   `json:"limit_name"`
	MeteredFeature string   `json:"metered_feature"`
	Allowed        any      `json:"allowed"`
	LimitReached   any      `json:"limit_reached"`
	Windows        []Window `json:"windows"`
}

type Credits struct {
	HasCredits bool `json:"has_credits"`
	Unlimited  bool `json:"unlimited"`
	Balance    any  `json:"balance"`
}

func (c *Client) Fetch(ctx context.Context, access, accountID string) (status int, payload map[string]any, err error) {
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("User-Agent", "codex-cli")
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("usage HTTP %d: %s", resp.StatusCode, trim(string(body)))
	}
	if m, ok := parsed.(map[string]any); ok {
		return resp.StatusCode, m, nil
	}
	return resp.StatusCode, map[string]any{"raw": parsed}, nil
}

func Normalize(payload map[string]any) Report {
	windows, allowed, reached := parseRateLimit(payload["rate_limit"])
	var additional []Additional
	if extra, ok := payload["additional_rate_limits"].([]any); ok {
		for _, item := range extra {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			w, a, r := parseRateLimit(m["rate_limit"])
			additional = append(additional, Additional{
				LimitName:      stringField(m["limit_name"]),
				MeteredFeature: stringField(m["metered_feature"]),
				Allowed:        a,
				LimitReached:   r,
				Windows:        w,
			})
		}
	}
	credits, _ := payload["credits"].(map[string]any)
	spend, _ := payload["spend_control"].(map[string]any)
	if spend == nil {
		spend = map[string]any{}
	}
	plan := stringField(payload["plan_type"])
	if plan == "" {
		plan = "unknown"
	}
	return Report{
		PlanType:     plan,
		Allowed:      allowed,
		LimitReached: reached,
		Windows:      windows,
		Additional:   additional,
		Credits: Credits{
			HasCredits: boolField(credits["has_credits"]),
			Unlimited:  boolField(credits["unlimited"]),
			Balance:    credits["balance"],
		},
		SpendControl: spend,
	}
}

func parseRateLimit(v any) (windows []Window, allowed any, reached any) {
	details, _ := v.(map[string]any)
	if details == nil {
		return nil, nil, nil
	}
	for _, key := range []string{"primary_window", "secondary_window"} {
		if w := parseWindow(details[key]); w != nil {
			windows = append(windows, *w)
		}
	}
	return windows, details["allowed"], details["limit_reached"]
}

func parseWindow(v any) *Window {
	snap, _ := v.(map[string]any)
	if snap == nil {
		return nil
	}
	used := snap["used_percent"]
	return &Window{
		Label:              windowLabel(snap["limit_window_seconds"]),
		UsedPercent:        used,
		RemainingPercent:   remainingPercent(used),
		LimitWindowSeconds: snap["limit_window_seconds"],
		ResetAfterSeconds:  snap["reset_after_seconds"],
		ResetAt:            snap["reset_at"],
	}
}

func remainingPercent(used any) *int {
	n, ok := asInt(used)
	if !ok {
		return nil
	}
	v := 100 - n
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return &v
}

func windowLabel(seconds any) string {
	n, ok := asInt(seconds)
	if !ok {
		return "window"
	}
	switch {
	case n == 18000:
		return "5-hour"
	case n == 604800:
		return "7-day"
	case n >= 86400 && n%86400 == 0:
		return fmt.Sprintf("%d-day", n/86400)
	case n >= 3600 && n%3600 == 0:
		return fmt.Sprintf("%d-hour", n/3600)
	default:
		return fmt.Sprintf("%ds", n)
	}
}

func FormatDuration(seconds any) string {
	n, ok := asInt(seconds)
	if !ok {
		return "?"
	}
	if n < 0 {
		n = 0
	}
	days, rem := n/86400, n%86400
	hours, rem := rem/3600, rem%3600
	minutes := rem / 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		var i int
		_, err := fmt.Sscanf(n, "%d", &i)
		return i, err == nil
	default:
		return 0, false
	}
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func boolField(v any) bool {
	b, _ := v.(bool)
	return b
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

func ErrorFromPayload(status int, payload map[string]any, fallback error) string {
	if fallback != nil && payload == nil {
		return fallback.Error()
	}
	if payload != nil {
		if s := stringField(payload["error"]); s != "" {
			return fmt.Sprintf("usage HTTP %d: %s", status, s)
		}
		if s := stringField(payload["message"]); s != "" {
			return fmt.Sprintf("usage HTTP %d: %s", status, s)
		}
	}
	return fmt.Sprintf("usage HTTP %d: request failed", status)
}
