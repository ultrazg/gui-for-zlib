package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

// favoriteState is the last state confirmed by the account, not a local
// collection. Requests and results belong to this entry for this sign-in.
type favoriteState struct {
	Saved   bool
	Known   bool
	Loading bool
	Saving  bool
	Err     string
	seq     uint64
	cancel  context.CancelFunc
}

func (a *App) loadFavoriteStatus(book core.Book) {
	if a.session == nil {
		return
	}
	state := a.favorites[book.ID]
	if state == nil {
		state = &favoriteState{}
		a.favorites[book.ID] = state
	}
	if state.Saving {
		return
	}
	a.cancelFavoriteRead(book.ID)
	state.seq++
	seq := state.seq
	state.Loading = true
	state.Err = ""
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	state.cancel = cancel

	go func() {
		saved, err := a.svc.FavoriteStatus(ctx, book)
		cancel()
		a.update(func() {
			if a.favorites[book.ID] != state || state.seq != seq {
				return
			}
			state.Loading = false
			state.cancel = nil
			if err != nil {
				state.Err = "读取收藏状态失败：" + friendlyError(err)
				return
			}
			state.Saved, state.Known = saved, true
			a.applyFavoriteStatus(book.ID, saved)
		})
	}()
}

func (a *App) toggleFavorite(book core.Book) {
	if a.session == nil {
		a.detail.Open = false
		a.openLogin()
		return
	}
	state := a.favorites[book.ID]
	if state == nil || !state.Known || state.Loading || state.Saving {
		return
	}
	saved := !state.Saved
	state.seq++
	seq := state.seq
	state.Saving = true
	state.Err = ""
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	state.cancel = cancel

	go func() {
		err := a.svc.SetFavorite(ctx, book.ID, saved)
		cancel()
		a.update(func() {
			if a.favorites[book.ID] != state || state.seq != seq {
				return
			}
			state.Saving = false
			state.cancel = nil
			if err != nil {
				state.Err = "收藏操作失败：" + friendlyError(err)
				return
			}
			state.Saved, state.Known = saved, true
			a.applyFavoriteStatus(book.ID, saved)
			if saved {
				a.toast("已收藏到账户")
			} else {
				a.toast("已取消账户收藏")
			}
			// Reload the account collection and invalidate its profile count.
			if list := a.library["favorites"]; list != nil {
				a.loadLibrary("favorites", max(1, list.Page))
			}
			a.resetProfile()
		})
	}()
}

// applyFavoriteStatus updates copies in visible lists without mutating a
// shared bool pointer that another book response may still hold.
func (a *App) applyFavoriteStatus(id string, saved bool) {
	set := func(book *core.Book) {
		if book.ID == id {
			value := saved
			book.Saved = &value
		}
	}
	set(&a.detail.Book)
	for i := range a.search.Books {
		set(&a.search.Books[i])
	}
	for _, list := range a.library {
		for i := range list.Books {
			set(&list.Books[i])
		}
	}
}

func (a *App) cancelFavoriteRead(id string) {
	if state := a.favorites[id]; state != nil && state.Loading {
		if state.cancel != nil {
			state.cancel()
		}
		state.seq++
		state.Loading = false
		state.cancel = nil
	}
}

func (a *App) resetFavorites() {
	for _, state := range a.favorites {
		if state.cancel != nil {
			state.cancel()
		}
		state.seq++
	}
	a.favorites = map[string]*favoriteState{}
}

func (a *App) favoriteButton(c *ui.Context, book core.Book) {
	if a.session == nil {
		ui.Button(c.Key("favorite-book"), "登录后收藏").OnClick(func() {
			a.detail.Open = false
			a.openLogin()
		})
		return
	}
	state := a.favorites[book.ID]
	label, accessible := "收藏", "收藏此书"
	if state != nil && state.Saved {
		label, accessible = "取消收藏", "取消收藏此书"
	}
	ui.Button(c.Key("favorite-book"), label).Label(accessible).
		Disabled(state == nil || !state.Known || state.Loading || state.Saving).
		OnClick(func() { a.toggleFavorite(book) })
}
