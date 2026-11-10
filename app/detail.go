package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type detailState struct {
	Open bool
	Book core.Book

	// Cache by book and source URL, including covers resolved from details.
	// Failed loads settle too, but are retried when the book is reopened.
	cover       *ui.Bitmap
	coverKey    string
	coverSource string
	coverDone   bool
	coverErr    error
	coverSeq    uint64
	coverCancel context.CancelFunc
}

func (a *App) showDetail(b core.Book) {
	b.Cover = strings.TrimSpace(b.Cover)
	if a.detail.Book.ID != b.ID {
		a.cancelFavoriteRead(a.detail.Book.ID)
	}
	a.detail.Book = b
	a.detail.Open = true
	if a.session != nil {
		a.loadFavoriteStatus(b)
	}
	key := b.Key()
	d := &a.detail
	if d.coverKey == key && d.coverSource == b.Cover && (!d.coverDone || d.coverErr == nil) {
		return
	}
	a.loadCover(b, key)
}

// loadCover fetches and decodes the cover off the main thread; a stale
// result (a newer book opened first) is dropped by the sequence number.
func (a *App) loadCover(b core.Book, key string) {
	d := &a.detail
	if d.coverCancel != nil {
		d.coverCancel()
		d.coverCancel = nil
	}
	d.coverSeq++
	seq := d.coverSeq
	d.coverKey = key
	d.coverSource = b.Cover
	d.cover = nil
	d.coverDone = false
	d.coverErr = nil

	_, canFetchDetails := a.svc.(core.BookFetcher)
	if b.Cover == "" && (!canFetchDetails || b.ID == "" || b.Hash == "") {
		d.coverDone = true
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	d.coverCancel = cancel
	go func() {
		bm, err := a.fetchCover(ctx, b)
		cancel()
		a.update(func() {
			if seq == d.coverSeq && d.coverKey == key {
				d.cover = bm
				d.coverDone = true
				d.coverErr = err
				d.coverCancel = nil
			}
		})
	}()
}

// History can omit covers or retain an expired URL. Ask for the current book
// metadata only when the list's cover is missing or could not be decoded.
func (a *App) fetchCover(ctx context.Context, b core.Book) (*ui.Bitmap, error) {
	var err error
	if b.Cover != "" {
		var bm *ui.Bitmap
		bm, err = a.fetchCoverBitmap(ctx, b.Cover)
		if err == nil {
			return bm, nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	fetcher, ok := a.svc.(core.BookFetcher)
	if !ok || b.ID == "" || b.Hash == "" {
		return nil, err
	}
	details, detailErr := fetcher.FetchBook(ctx, b.ID, b.Hash)
	if detailErr != nil {
		return nil, detailErr
	}
	if details == nil {
		return nil, fmt.Errorf("book detail response is empty")
	}
	coverURL := strings.TrimSpace(details.Cover)
	if coverURL == "" || coverURL == b.Cover {
		return nil, err
	}
	return a.fetchCoverBitmap(ctx, coverURL)
}

func (a *App) fetchCoverBitmap(ctx context.Context, rawURL string) (*ui.Bitmap, error) {
	data, err := a.svc.FetchImage(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
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
