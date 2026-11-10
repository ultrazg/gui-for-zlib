package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type detailState struct {
	Open bool
	Book core.Book

	// cover caches the fetched cover bitmap for the current book. key
	// identifies the book it belongs to; done means the fetch settled, so a
	// reopened book is not refetched. Cover URLs are empty in the demo
	// backend, which shows the placeholder instead.
	cover     *ui.Bitmap
	coverKey  string
	coverDone bool
	coverSeq  uint64
}

func (a *App) showDetail(b core.Book) {
	if a.detail.Book.ID != b.ID {
		a.cancelFavoriteRead(a.detail.Book.ID)
	}
	a.detail.Book = b
	a.detail.Open = true
	if a.session != nil {
		a.loadFavoriteStatus(b)
	}
	key := b.Key()
	if b.Cover == "" {
		a.detail.cover = nil
		a.detail.coverKey = key
		a.detail.coverDone = true
		return
	}
	if a.detail.coverKey != key || !a.detail.coverDone {
		a.loadCover(b, key)
	}
}

// loadCover fetches and decodes the cover off the main thread; a stale
// result (a newer book opened first) is dropped by the sequence number.
func (a *App) loadCover(b core.Book, key string) {
	d := &a.detail
	d.coverSeq++
	seq := d.coverSeq
	d.coverKey = key
	d.cover = nil
	d.coverDone = false

	rawURL := b.Cover
	go func() {
		data, err := a.svc.FetchImage(context.Background(), rawURL)
		var bm *ui.Bitmap
		if err == nil {
			bm, err = ui.DecodeBitmap(data)
		}
		a.update(func() {
			if seq == d.coverSeq && d.coverKey == key {
				d.cover = bm
				d.coverDone = true
			}
		})
	}()
}

func (a *App) detailModal(c *ui.Context) {
	modal := ui.Modal(c.Key("book-detail"), &a.detail.Open, func() {
		t := c.Theme()
		b := a.detail.Book

		ui.Column(c).Width(560).Gap(12).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
				a.coverBox(c, b)
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
					ui.Text(c, b.Title).FontSize(22).Bold()
					ui.Textf(c, "%s · %s · %s", b.Author, b.Year, b.Publisher).
						FontSize(13).TextColor(t.TextMuted)
					ui.Row(c).Gap(8).Wrap().Children(func() {
						if b.Extension != "" {
							ui.Badge(c, b.Extension)
						}
						if b.Language != "" {
							ui.Badge(c, b.Language)
						}
						if b.Size != "" {
							ui.Badge(c, b.Size)
						}
						if b.Rating > 0 {
							ui.Badge(c, fmt.Sprintf("★ %.1f", b.Rating))
						}
						if b.Pages > 0 {
							ui.Badge(c, fmt.Sprintf("%d 页", b.Pages))
						}
					})
				})
			})

			ui.Divider(c)
			ui.Text(c, "简介").Bold()
			if b.Description == "" {
				ui.Text(c, "暂无简介").TextColor(t.TextMuted)
			} else {
				ui.Scroll(c).MaxHeight(240).Children(func() {
					ui.Text(c, b.Description).TextColor(t.TextMuted)
				})
			}

			ui.Row(c).Gap(10).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if a.session == nil {
					ui.Text(c, "登录后可下载").FontSize(12).TextColor(t.Warning)
				}
				if ui.Button(c, "关闭").Clicked() {
					a.detail.Open = false
				}
				a.favoriteButton(c, b)
				switch {
				case a.session == nil:
					if ui.PrimaryButton(c, "登录后下载").Clicked() {
						a.detail.Open = false
						a.openLogin()
					}
				default:
					if ui.PrimaryButton(c, "下载").Clicked() {
						a.detail.Open = false
						a.startDownload(b)
					}
				}
			})
		})
	})
	if modal.Dismissed() || !a.detail.Open {
		a.cancelFavoriteRead(a.detail.Book.ID)
	}
}

// coverBox shows the fetched cover, or a fixed-size placeholder while it is
// missing, so the layout never jumps.
func (a *App) coverBox(c *ui.Context, b core.Book) ui.Element {
	t := c.Theme()
	const (
		cw, ch = 120, 170
	)
	if bm := a.detail.cover; bm != nil {
		return ui.Image(c, bm).Size(cw, ch).Fit(ui.Cover).Radius(6).Border(1, t.Border)
	}
	return ui.Box(c).Width(cw).Height(ch).Shrink(0).Radius(6).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Row(c).Fill().Center().Children(func() {
			ui.Text(c, coverPlaceholder(b)).FontSize(40).TextColor(t.TextMuted)
		})
	})
}

// coverPlaceholder is the cover area's stand-in: the title's first letter,
// as a memory of the book when there is no image.
func coverPlaceholder(b core.Book) string {
	for _, r := range b.Title {
		if r != ' ' && r != 0 {
			return string(r)
		}
	}
	return "书"
}
