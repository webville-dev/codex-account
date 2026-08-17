package account

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const openaiAuthClaim = "https://api.openai.com/auth"
const openaiProfileClaim = "https://api.openai.com/profile"

// Claims are unsigned JWT payload fields. Parsing is not verification.
type Claims struct {
	Expiry    time.Time
	Email     string
	Plan      string
	AccountID string
	UserID    string
	ClientID  string
}

func Payload(token string) Claims {
	var out Claims
	if token == "" || strings.Count(token, ".") < 2 {
		return out
	}
	raw, err := decodeSegment(strings.Split(token, ".")[1])
	if err != nil {
		return out
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return out
	}
	out.Expiry = unixTime(payload["exp"])
	out.ClientID = stringField(payload["client_id"])
	out.Email = emailFrom(payload)
	if auth, ok := payload[openaiAuthClaim].(map[string]any); ok {
		out.AccountID = stringField(auth["chatgpt_account_id"])
		out.Plan = stringField(auth["chatgpt_plan_type"])
		out.UserID = stringField(auth["chatgpt_user_id"])
		if out.UserID == "" {
			out.UserID = stringField(auth["user_id"])
		}
	}
	return out
}

func decodeSegment(seg string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(seg); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(seg)
}

func emailFrom(payload map[string]any) string {
	candidates := []any{payload["email"]}
	if profile, ok := payload[openaiProfileClaim].(map[string]any); ok {
		candidates = append(candidates, profile["email"])
	}
	for _, c := range candidates {
		s := strings.TrimSpace(strings.ToLower(stringField(c)))
		if strings.Contains(s, "@") {
			return s
		}
	}
	return ""
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func unixTime(v any) time.Time {
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return time.Time{}
		}
		return time.Unix(int64(n), 0).UTC()
	case json.Number:
		i, err := n.Int64()
		if err != nil || i <= 0 {
			return time.Time{}
		}
		return time.Unix(i, 0).UTC()
	default:
		return time.Time{}
	}
}
