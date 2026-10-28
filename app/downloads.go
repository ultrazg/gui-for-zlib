package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type downloadItem struct {
	Book  core.Book
	Done  int64
	Total int64
	Path  string
	// Status is shown under the progress bar.
	Status string
	Err    string

	Finished bool
	cancel   context.CancelFunc
}

func (d *downloadItem) fraction() float64 {
	if d.Total <= 0 {
		return -1
	}
	return float64(d.Done) / float64(d.Total)
}

type downloadState struct {
	Items []*downloadItem
}

// activeDownloads counts the downloads still running in the background; the
// sidebar shows that number as a badge on the 下载 item.
func (a *App) activeDownloads() int {
	n := 0
	for _, item := range a.downloads.Items {
		if !item.Finished {
			n++
		}
	}
	return n
}

func (a *App) startDownload(b core.Book) {
	if a.session == nil {
		a.openLogin()
		a.toast("请先登录后再下载")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	item := &downloadItem{
		Book:   b,
		Status: "准备中…",
		cancel: cancel,
	}
	a.downloads.Items = append(a.downloads.Items, item)

	dir := a.settings.DownloadDir
	go func() {
		last := -1
		progress := func(done, total int64) {
			if total <= 0 {
				return
			}
			pct := int(float64(done) * 100 / float64(total))
			if pct == last && done != total {
				return
			}
			last = pct
			a.update(func() {
				item.Done = done
				item.Total = total
				item.Status = fmt.Sprintf("下载中 %d%%", pct)
			})
		}

		path, err := a.svc.Download(ctx, b, dir, progress)
		a.update(func() {
			item.Finished = true
			switch {
			case errors.Is(err, context.Canceled):
				item.Status = "已取消"
			case err != nil:
				item.Status = "失败"
				item.Err = friendlyError(err)
			default:
				if item.Total <= 0 {
					item.Total = 1
				}
				item.Done = item.Total
				item.Path = path
				item.Status = "已完成"
				a.toast("下载完成：" + b.Title)
				if a.notify != nil {
					a.notify("download-"+b.Key(), "下载完成", b.Title)
				}
			}
		})
	}()
}

func (a *App) downloadsPage(c *ui.Context) {
	t := c.Theme()
	d := &a.downloads
	remove := -1

	ui.Column(c).Fill().Padding(18, 24).Gap(12).Children(func() {
		ui.Text(c, "下载").FontSize(22).Bold()

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "目录").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, a.settings.DownloadDir).FontSize(12).Grow(1).SingleLine()
			if ui.Button(c, "选择目录").Clicked() {
				a.pickDownloadDir()
			}
		})

		if len(d.Items) == 0 {
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Text(c, "还没有下载任务").FontSize(16).Bold()
				ui.Text(c, "在搜索结果里打开一本书，然后点“下载”。").
					FontSize(12).TextColor(t.TextMuted)
			})
			return
		}

		ui.List(c, nil, len(d.Items), func(i int) {
			item := d.Items[i]
			ui.Column(c).Padding(10, 12).Gap(6).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, item.Book.Title).FontSize(14).Bold().Grow(1).SingleLine()
					ui.Text(c, item.Book.Extension).FontSize(12).TextColor(t.TextMuted)
					if !item.Finished {
						if ui.Button(c, "取消").Clicked() {
							item.cancel()
							item.Status = "取消中…"
						}
					} else if ui.Button(c, "移除").Clicked() {
						remove = i
					}
				})
				ui.Progress(c, item.fraction()).Label(item.Status)
				if item.Err != "" {
					ui.Text(c, item.Err).FontSize(12).TextColor(t.Danger)
				}
				if item.Path != "" {
					ui.Text(c, item.Path).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			})
			ui.Divider(c)
		}).Grow(1).Border(1, t.Border).Radius(8).Padding(4)
	})

	if remove >= 0 && remove < len(d.Items) {
		d.Items = append(d.Items[:remove], d.Items[remove+1:]...)
	}
}

func (a *App) pickDownloadDir() {
	if a.win == nil {
		a.toast("窗口未就绪，无法打开目录选择器")
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       "选择下载目录",
		DefaultPath: a.settings.DownloadDir,
		Directory:   true,
	})
	if err != nil {
		a.toast("打开目录选择器失败：" + err.Error())
		return
	}
	if len(paths) > 0 {
		a.settings.DownloadDir = paths[0]
		a.toast("下载目录已更新")
	}
}
