package core

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Feed endpoints. All return the same books + pagination shape, so they
// share one implementation.
const (
	pathRecommended = "/eapi/user/book/recommended"
	pathMostPopular = "/eapi/book/most-popular"
	pathSaved       = "/eapi/user/book/saved"
	pathDownloaded  = "/eapi/user/book/downloaded"
)

func (c *Client) feed(ctx context.Context, path string, page, count int) (*BookPage, error) {
	if page < 1 {
		page = 1
	}
	if count < 1 {
		count = defaultSearchCount
	}
	if count > maxSearchCount {
		count = maxSearchCount
	}
	form := url.Values{}
	form.Set("page", strconv.Itoa(page))
	form.Set("limit", strconv.Itoa(count))

	body, err := c.eapiRequest(ctx, http.MethodGet, path, form)
	if err != nil {
		return nil, err
	}
	env, err := decodeEAPIObjectChecked(body, "list")
	if err != nil {
		return nil, err
	}
	if err := eapiFailure(env); err != nil {
		return nil, err
	}
	return c.bookPageFromEnv(env, page, count)
}

// Recommended maps GET /eapi/user/book/recommended.
func (c *Client) Recommended(ctx context.Context, page, count int) (*BookPage, error) {
	return c.feed(ctx, pathRecommended, page, count)
}

// MostPopular maps GET /eapi/book/most-popular.
func (c *Client) MostPopular(ctx context.Context, page, count int) (*BookPage, error) {
	return c.feed(ctx, pathMostPopular, page, count)
}

// SavedBooks maps GET /eapi/user/book/saved (the favorites page).
func (c *Client) SavedBooks(ctx context.Context, page, count int) (*BookPage, error) {
	res, err := c.feed(ctx, pathSaved, page, count)
	if err != nil {
		return nil, err
	}
	for i := range res.Books {
		saved := true
		res.Books[i].Saved = &saved
	}
	return res, nil
}

// DownloadHistory maps GET /eapi/user/book/downloaded. History items get
// the same mapping plus the download date fields the endpoint provides.
func (c *Client) DownloadHistory(ctx context.Context, page, count int) (*BookPage, error) {
	return c.feed(ctx, pathDownloaded, page, count)
}

// DeleteDownloadHistory removes one remote history entry. Only the numeric
// id part is used; local files are untouched.
func (c *Client) DeleteDownloadHistory(ctx context.Context, ref string) error {
	id := ref
	if head, _, ok := strings.Cut(ref, ":"); ok {
		id = head
	}
	id = normalizeDigits(id)
	if id == "" {
		return ErrNoID
	}
	body, err := c.eapiRequest(ctx, http.MethodGet, "/eapi/user/book/"+url.PathEscape(id)+"/delete", nil)
	if err != nil {
		return err
	}
	env, err := decodeEAPIObjectChecked(body, "history delete")
	if err != nil {
		return err
	}
	return eapiFailure(env)
}

// normalizeDigits accepts a pure numeric id. Anything else is rejected:
// silently truncating "12abc" would delete the wrong book.
func normalizeDigits(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s
}
