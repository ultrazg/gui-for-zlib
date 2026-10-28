package core

import (
	"context"
	"fmt"
	"net/http"
)

// FetchProfile maps GET /eapi/user/profile. The response contains display
// fields only; credentials never come back from this endpoint.
func (c *Client) FetchProfile(ctx context.Context) (*Profile, error) {
	body, err := c.eapiRequest(ctx, http.MethodGet, "/eapi/user/profile", nil)
	if err != nil {
		return nil, err
	}
	env, err := decodeEAPIObjectChecked(body, "profile")
	if err != nil {
		return nil, err
	}
	if err := eapiFailure(env); err != nil {
		return nil, err
	}
	user := asMap(env["user"])
	if user == nil {
		return nil, fmt.Errorf("invalid EAPI profile response: missing user object")
	}

	// Two naming schemes refer to the same numbers.
	today := user["downloads_today"]
	if eapiString(today) == "" {
		today = user["dailyDownloadsCount"]
	}
	limit := user["downloads_limit"]
	if eapiString(limit) == "" {
		limit = user["dailyDownloadLimit"]
	}
	quotaKnown := eapiString(limit) != ""
	allowed := eapiInt(limit)
	used := eapiInt(today)
	remaining := allowed - used
	if remaining < 0 {
		remaining = 0
	}
	if !quotaKnown {
		allowed, remaining = 0, 0
	}

	domains := []string{}
	if list, ok := user["personalDomains"].([]any); ok {
		for _, item := range list {
			if s := eapiString(item); s != "" {
				domains = append(domains, s)
			}
		}
	}

	return &Profile{
		ID:              eapiString(user["id"]),
		Email:           eapiString(user["email"]),
		Name:            eapiString(user["name"]),
		DownloadsTotal:  allowed,
		DownloadsLeft:   remaining,
		Downloads:       used,
		DailyReset:      eapiString(user["daily_reset"]),
		Premium:         eapiFlag(user["isPremium"]),
		Confirmed:       eapiFlag(user["confirmed"]),
		DonationsActive: eapiFlag(user["donations_active"]),
		DonationsExpire: eapiString(user["donations_expire"]),
		PersonalDomains: domains,
		QuotaKnown:      quotaKnown,
	}, nil
}
