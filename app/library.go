package app

import (
	"context"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type libraryState struct {
	Books      []core.Book
	Page       int
	Total      int
	TotalPages int
	Count      int

	Selected int
	List     ui.ListState

	Loading bool
	Loaded  bool
	Err     string
	seq     uint64
}

func (a *App) libraryPage(c *ui.Context, kind string) {
	t := c.Theme()
	ls := a.library[kind]
	if ls == nil {
		ls = &libraryState{Count: 10}
		a.library[kind] = ls
	}
	a.ensureLibrary(kind)

	title, subtitle := "收藏", "你在账户中收藏的书。"
	switch kind {
	case "history":
		title, subtitle = "历史", "最近下载过的书。"
	case "recommended":
		title, subtitle = "推荐", "为你推荐的书。"
	case "popular":
		title, subtitle = "最热", "全站最热门的书。"
	}

	ui.Column(c).Fill().Padding(18, 24).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(22).Bold()
				ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted)
			})
			if ui.Button(c, "").Disabled(ls.Loading).Children(func() {
				if ls.Loading {
					ui.Spinner(c)
					ui.Text(c, "正在加载...").FontSize(12)
				} else {
					ui.Text(c, "刷新").FontSize(12)
				}
			}).Clicked() {
				a.loadLibrary(kind, ls.Page)
			}
		})

		if ls.Err != "" {
			ui.Text(c, ls.Err).TextColor(t.Danger)
		}

		if !ls.Loading && ls.Loaded && len(ls.Books) == 0 {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "暂无内容").FontSize(16).Bold()
			})
			return
		}

		ls.List.Key = func(i int) any { return ls.Books[i].Key() }
		ls.List.Label = func(i int) string { return ls.Books[i].Title }
		ls.List.Selected = &ls.Selected

		list := ui.List(c, &ls.List, len(ls.Books), func(i int) {
			a.bookRow(c, ls.Books[i])
		}).Grow(1).Border(1, t.Border).Radius(8).Padding(4)

		if list.Submitted() && ls.Selected >= 0 && ls.Selected < len(ls.Books) {
			a.showDetail(ls.Books[ls.Selected])
		}

		count := ls.Count
		if count <= 0 {
			count = 10
		}
		pages := ls.TotalPages
		if pages <= 0 {
			pages = (ls.Total + count - 1) / count
		}
		if pages < 1 {
			pages = 1
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "上一页").Disabled(ls.Loading || ls.Page <= 1).Clicked() {
				a.loadLibrary(kind, ls.Page-1)
			}
			ui.Textf(c, "第 %d / %d 页", max(ls.Page, 1), pages).
				FontSize(12).TextColor(t.TextMuted)
			if ui.Button(c, "下一页").Disabled(ls.Loading || ls.Page >= pages).Clicked() {
				a.loadLibrary(kind, ls.Page+1)
			}
		})
	})
}

func (a *App) ensureLibrary(kind string) {
	ls := a.library[kind]
	if ls == nil || ls.Loaded || ls.Loading {
		return
	}
	a.loadLibrary(kind, 1)
}

func (a *App) loadLibrary(kind string, page int) {
	if page < 1 {
		page = 1
	}
	ls := a.library[kind]
	if ls == nil {
		ls = &libraryState{Count: 10}
		a.library[kind] = ls
	}
	ls.seq++
	seq := ls.seq
	ls.Loading = true
	ls.Err = ""

	count := ls.Count
	if count <= 0 {
		count = 10
	}

	go func() {
		var (
			res *core.BookPage
			err error
		)
		switch kind {
		case "history":
			res, err = a.svc.History(context.Background(), page, count)
		case "recommended":
			res, err = a.svc.Recommended(context.Background(), page, count)
		case "popular":
			res, err = a.svc.Popular(context.Background(), page, count)
		default:
			res, err = a.svc.Favorites(context.Background(), page, count)
		}
		a.update(func() {
			if seq != ls.seq || a.library[kind] != ls {
				return
			}
			ls.Loading = false
			ls.Loaded = true
			if err != nil {
				ls.Err = friendlyError(err)
				return
			}
			if kind == "favorites" && len(res.Books) == 0 && page > 1 &&
				(res.TotalPages == 0 || page > res.TotalPages) {
				// Removing the last book on the last page can shrink pagination.
				a.loadLibrary(kind, max(1, res.TotalPages))
				return
			}
			ls.Books = res.Books
			ls.Page = res.Page
			ls.Total = res.Total
			ls.TotalPages = res.TotalPages
			ls.Selected = -1
		})
	}()
}
