package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type searchState struct {
	Query     string
	Extension string

	Books      []core.Book
	Page       int
	Total      int
	TotalPages int
	Count      int
	Selected   int
	List       ui.ListState

	Loading bool
	Started bool
	Err     string
	seq     uint64
}

func (a *App) startSearch(page int) {
	if page < 1 {
		page = 1
	}
	s := &a.search
	s.seq++
	s.Started = true
	s.Err = ""
	if a.session == nil {
		s.Loading = false
		s.Books = nil
		s.Err = "请先登录后再搜索。"
		return
	}
	seq := s.seq
	s.Loading = true

	opt := core.SearchOptions{
		Query:     s.Query,
		Extension: s.Extension,
		Page:      page,
		Count:     10,
	}
	if n, err := strconv.Atoi(a.settings.Count); err == nil && n >= 1 && n <= 100 {
		opt.Count = n
	}
	s.Count = opt.Count

	go func() {
		res, err := a.svc.Search(context.Background(), opt)
		a.update(func() {
			if seq != s.seq {
				return
			}
			s.Loading = false
			if err != nil {
				s.Err = friendlyError(err)
				return
			}
			s.Books = res.Books
			s.Page = res.Page
			s.Total = res.Total
			s.TotalPages = res.TotalPages
			s.Selected = -1
			if s.Extension == "" {
				s.Extension = "全部"
			}
		})
	}()
}

func (a *App) searchPage(c *ui.Context) {
	t := c.Theme()
	s := &a.search
	if s.Extension == "" {
		s.Extension = "全部"
	}

	ui.Column(c).Fill().Padding(18, 24).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, "搜索").FontSize(22).Bold()
				ui.Text(c, "按书名、作者或关键词搜索，双击或回车查看详情。").
					FontSize(12).TextColor(t.TextMuted)
			})
		})

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &s.Query).Label("书名 / 作者 / 关键词").OnSubmit(func() { a.startSearch(1) }).Grow(1)

			filter := ui.Select(c, &s.Extension, extensionOptions())
			if filter.Changed() {
				a.startSearch(1)
			}

			if ui.PrimaryButton(c, "").Children(func() {
				if s.Loading {
					ui.Spinner(c)
				} else {
					ui.Text(c, "搜索")
				}
			}).Disabled(s.Loading || s.Query == "").Clicked() {
				a.startSearch(1)
			}
		})

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if a.session == nil {
				ui.Text(c, "登录后可搜索").
					FontSize(12).TextColor(t.TextMuted)
			}
		})

		if s.Err != "" {
			ui.Text(c, s.Err).TextColor(t.Danger)
		}

		s.List.Key = func(i int) any { return s.Books[i].Key() }
		s.List.Label = func(i int) string { return s.Books[i].Title }
		s.List.Selected = &s.Selected

		list := ui.List(c, &s.List, len(s.Books), func(i int) {
			a.bookRow(c, s.Books[i])
		}).Grow(1).Border(1, t.Border).Radius(8).Padding(4)

		if list.Submitted() && s.Selected >= 0 && s.Selected < len(s.Books) {
			a.showDetail(s.Books[s.Selected])
		}

		if !s.Loading && s.Started && len(s.Books) == 0 {
			ui.Text(c, "没有匹配的书，换个关键词试试。").
				Padding(12).TextColor(t.TextMuted)
		}

		count := s.Count
		if count <= 0 {
			count = 10
		}
		pages := s.TotalPages
		if pages <= 0 {
			pages = (s.Total + count - 1) / count
		}
		if pages < 1 {
			pages = 1
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if s.Page > 1 {
				if ui.Button(c, "首页").Clicked() {
					a.startSearch(1)
				}
			}
			if ui.Button(c, "上一页").Disabled(s.Loading || s.Page <= 1).Clicked() {
				a.startSearch(s.Page - 1)
			}
			ui.Textf(c, "第 %d / %d 页，共 %d 本", s.Page, pages, s.Total).
				FontSize(12).TextColor(t.TextMuted)
			if ui.Button(c, "下一页").Disabled(s.Loading || s.Page >= pages).Clicked() {
				a.startSearch(s.Page + 1)
			}
		})
	})
}

func (a *App) bookRow(c *ui.Context, b core.Book) {
	t := c.Theme()
	row := ui.Row(c).Padding(8, 10).Gap(12).AlignItems(ui.Center)
	row.Children(func() {
		ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
			// An explicit color keeps the title from inheriting the white
			// AccentText the list applies to a chosen row.
			ui.Text(c, b.Title).FontSize(14).Bold().TextColor(t.Text).SingleLine()
			if meta := bookMeta(b); meta != "" {
				ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
		})
		if b.Rating > 0 {
			ui.Textf(c, "★ %.1f", b.Rating).FontSize(12).TextColor(t.Warning)
		}
	})
}

// extensionOptions is the full filter list plus the "all" sentinel.
func extensionOptions() []string {
	out := make([]string, 0, len(core.Extensions)+1)
	out = append(out, "全部")
	out = append(out, core.Extensions...)
	return out
}

// bookMeta joins the non-empty list metadata; upstream leaves some fields
// empty for history and feed entries.
func bookMeta(b core.Book) string {
	parts := make([]string, 0, 4)
	for _, s := range []string{b.Author, b.Year, b.Extension, b.Size} {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

func friendlyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "操作已取消。"
	case errors.Is(err, core.ErrNotLoggedIn):
		return "请先登录。"
	case errors.Is(err, core.ErrInvalidCredentials):
		return "邮箱或密码不正确。"
	case errors.Is(err, core.ErrSessionExpired):
		return "会话已过期，请重新登录。"
	case errors.Is(err, core.ErrNoDomain):
		return "没有可用的镜像：" + err.Error()
	case errors.Is(err, core.ErrBotProtection):
		return "请求被反爬拦截，请换一个镜像或稍后重试。"
	case errors.Is(err, core.ErrDownloadFailed):
		return "下载失败：" + err.Error()
	default:
		return err.Error()
	}
}
