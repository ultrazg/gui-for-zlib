package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

var ErrFavoriteStateUnknown = errors.New("favorite state is unknown")

// SetFavorite uses the Android EAPI's save/unsave contract. The path takes
// only a numeric book ID, not the id:hash reference used by downloads.
func (c *Client) SetFavorite(ctx context.Context, id string, saved bool) error {
	id = normalizeDigits(id)
	if id == "" {
		return ErrNoID
	}
	if !c.LoggedIn() {
		return ErrNotLoggedIn
	}
	action := "unsave"
	if saved {
		action = "save"
	}
	body, err := c.eapiMutation(ctx, "/eapi/user/book/"+url.PathEscape(id)+"/"+action)
	if err != nil {
		return err
	}
	env, err := decodeEAPIObjectChecked(body, "favorite")
	if err != nil {
		return err
	}
	return eapiFailure(env)
}

// FavoriteStatus refreshes the server's flag when details expose it. Older
// responses omit the flag, so use membership in the account's paginated
// favorites instead. An incomplete list must not be mistaken for "not saved".
func (c *Client) FavoriteStatus(ctx context.Context, book Book) (bool, error) {
	id := normalizeDigits(book.ID)
	if id == "" {
		return false, ErrNoID
	}
	if !c.LoggedIn() {
		return false, ErrNotLoggedIn
	}
	if book.Hash != "" {
		details, err := c.FetchBook(ctx, id, book.Hash)
		if err != nil {
			return false, err
		}
		if details.Saved != nil {
			return *details.Saved, nil
		}
	}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		res, err := c.SavedBooks(ctx, page, maxSearchCount)
		if err != nil {
			return false, err
		}
		for _, candidate := range res.Books {
			if candidate.ID == id {
				return true, nil
			}
		}
		if res.Page != page {
			return false, fmt.Errorf("%w: unexpected saved-books page", ErrFavoriteStateUnknown)
		}
		if res.TotalPages > 0 {
			if page >= res.TotalPages {
				return false, nil
			}
			continue
		}
		if len(res.Books) == 0 {
			return false, nil
		}
		return false, fmt.Errorf("%w: missing saved-books pagination", ErrFavoriteStateUnknown)
	}
}
