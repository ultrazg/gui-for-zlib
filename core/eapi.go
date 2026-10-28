package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// decodeEAPIObject parses any EAPI object. Numbers stay json.Number so a
// field that arrives as "4" and another as 4.0 both normalise through
// eapiString instead of silently becoming different Go types.
func decodeEAPIObject(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not a JSON object")
	}
	return m, nil
}

// decodeEAPIObjectChecked additionally requires the "success" field. A body
// without it is not a valid EAPI response.
func decodeEAPIObjectChecked(body []byte, what string) (map[string]any, error) {
	m, err := decodeEAPIObject(body)
	if err != nil {
		return nil, fmt.Errorf("invalid EAPI %s response: %w", what, err)
	}
	if _, ok := m["success"]; !ok {
		return nil, fmt.Errorf("invalid EAPI %s response: missing success field", what)
	}
	return m, nil
}

func eapiSuccess(m map[string]any) bool {
	switch eapiString(m["success"]) {
	case "1", "true", "True":
		return true
	}
	return false
}

// eapiFailure returns the server's error text when success != 1. The text
// is never replaced by a generic message: "daily limit reached" and
// "not authorized" are exactly what the user needs to see. Authentication
// failures are classified so callers can offer a re-login.
func eapiFailure(m map[string]any) error {
	if eapiSuccess(m) {
		return nil
	}
	msg := eapiString(m["error"])
	if msg == "" {
		msg = "server returned success=0 without an error message"
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "session") ||
		strings.Contains(lower, "login") {
		return fmt.Errorf("%w: %s", ErrSessionExpired, msg)
	}
	return fmt.Errorf("%s", msg)
}

// eapiString coerces the union type EAPI uses for scalars into text.
func eapiString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func eapiInt(v any) int {
	s := eapiString(v)
	if s == "" {
		return 0
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(f)
}

func eapiFloat(v any) float64 {
	s := eapiString(v)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// eapiFlag converts bool / 0 / 1 / "true" into a tri-state pointer. A
// missing field stays nil, which is different from false in the JSON
// contract.
func eapiFlag(v any) *bool {
	switch t := v.(type) {
	case nil:
		return nil
	case bool:
		b := t
		return &b
	}
	s := strings.ToLower(eapiString(v))
	switch s {
	case "1", "true", "yes", "on":
		b := true
		return &b
	case "0", "false", "no", "off":
		b := false
		return &b
	}
	return nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// LoginEAPI performs POST /eapi/user/login and stores the returned
// remix_userid / remix_userkey in the client. The password is used for this
// one request and is never retained.
func (c *Client) LoginEAPI(ctx context.Context, email, password string) (*Session, error) {
	form := url.Values{}
	form.Set("email", email)
	form.Set("password", password)

	body, err := c.eapiRequest(ctx, http.MethodPost, "/eapi/user/login", form)
	if err != nil {
		return nil, err
	}
	env, err := decodeEAPIObjectChecked(body, "login")
	if err != nil {
		return nil, err
	}
	if !eapiSuccess(env) {
		msg := eapiString(env["error"])
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "password") &&
			(strings.Contains(lower, "incorrect") || strings.Contains(lower, "wrong") || strings.Contains(lower, "invalid")):
			return nil, fmt.Errorf("%w", ErrInvalidCredentials)
		case strings.Contains(lower, "authorization failed"):
			return nil, fmt.Errorf("%w: %s (this account may have EAPI password login disabled; import browser cookies instead)", ErrLoginFailed, msg)
		default:
			return nil, fmt.Errorf("%w: %s", ErrLoginFailed, msg)
		}
	}

	user := asMap(env["user"])
	if user == nil {
		return nil, fmt.Errorf("invalid EAPI login response: missing user object")
	}
	id := eapiString(user["id"])
	key := eapiString(user["remix_userkey"])
	if key == "" {
		key = eapiString(user["remix_user_key"])
	}
	if id == "" || key == "" {
		return nil, fmt.Errorf("invalid EAPI login response: missing remix_userid or remix_userkey")
	}

	c.SetCookies(map[string]string{
		"remix_userid":  id,
		"remix_userkey": key,
	})

	name := eapiString(user["name"])
	if name == "" {
		name = localPart(email)
	}
	return &Session{ID: id, Email: email, Name: name}, nil
}

func localPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}
