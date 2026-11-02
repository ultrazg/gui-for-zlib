package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *App) homePage(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).Padding(24, 28).Gap(18).Children(func() {
		ui.Text(c, "欢迎使用 GUI for zlib").FontSize(30).Bold()

		ui.Row(c).Gap(10).Children(func() {
			if ui.PrimaryButton(c, "去搜索").Clicked() {
				a.path.Push("/search")
			}
			if ui.Button(c, "打开下载列表").Clicked() {
				a.path.Push("/downloads")
			}
			if ui.Button(c, "账户").Clicked() {
				a.path.Push("/profile")
			}
		})

		ui.Row(c).Gap(14).AlignItems(ui.Stretch).Children(func() {
			a.homeCard(c, "账户", func() {
				if a.session != nil {
					ui.Text(c, a.session.Email).Bold().SingleLine()
					ui.Text(c, "已登录").FontSize(12).TextColor(t.Success)
				} else {
					ui.Text(c, "未登录").Bold()
					ui.Text(c, "登录后可以搜索、下载并查看配额。").
						FontSize(12).TextColor(t.TextMuted)
					if ui.Button(c, "立即登录").Clicked() {
						a.openLogin()
					}
				}
			})
			a.homeCard(c, "下载", func() {
				ui.Textf(c, "%d 个任务", len(a.downloads.Items)).Bold()
				running := 0
				for _, item := range a.downloads.Items {
					if !item.Finished {
						running++
					}
				}
				ui.Textf(c, "%d 个进行中", running).FontSize(12).TextColor(t.TextMuted)
				if ui.Button(c, "查看下载").Clicked() {
					a.path.Push("/downloads")
				}
			})
		})

		ui.Row(c).Gap(14).AlignItems(ui.Stretch).Children(func() {
			a.homeCard(c, "推荐", func() {
				ui.Text(c, "为你推荐的书").Bold().SingleLine()
				ui.Text(c, "登录后展示个人书单。").
					FontSize(12).TextColor(t.TextMuted)
				if ui.Button(c, "查看推荐").Clicked() {
					a.path.Push("/recommended")
				}
			})
			a.homeCard(c, "最热", func() {
				ui.Text(c, "全站热门书单").Bold().SingleLine()
				ui.Text(c, "看看大家都在读什么。").
					FontSize(12).TextColor(t.TextMuted)
				if ui.Button(c, "查看最热").Clicked() {
					a.path.Push("/popular")
				}
			})
		})
	})
}

func (a *App) homeCard(c *ui.Context, title string, body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(16).Gap(8).Grow(1).Basis(0).
		Background(t.Background).Border(1, t.Border).Radius(10).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().TextColor(t.TextMuted)
		body()
	})
}

func (a *App) profilePage(c *ui.Context) {
	t := c.Theme()
	if a.session == nil {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, "还没有登录").FontSize(22).Bold()
			ui.Text(c, "登录后可以查看下载配额和账户信息。").
				FontSize(12).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "登录查看账户").Clicked() {
				a.openLogin()
			}
		})
		return
	}

	a.ensureProfile()

	ui.Scroll(c).Grow(1).Padding(24, 28).Gap(16).Children(func() {
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Avatar(c, a.session.Name, nil)
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, a.session.Name).FontSize(22).Bold()
				ui.Text(c, a.session.Email).FontSize(12).TextColor(t.TextMuted)
			})
		})

		if a.profile.Loading {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在读取账户…").TextColor(t.TextMuted)
			})
		}
		if a.profile.Err != "" {
			ui.Text(c, a.profile.Err).TextColor(t.Danger)
		}
		if p := a.profile.Data; p != nil {
			ui.Column(c).Gap(8).MaxWidth(560).Children(func() {
				// ui.Text(c, "今日下载配额").Bold()
				if !p.QuotaKnown {
					ui.Text(c, "上游没有返回配额信息。").
						FontSize(12).TextColor(t.TextMuted)
					return
				}
				// value := 0.0
				// if p.DownloadsTotal > 0 {
				// 	value = float64(p.DownloadsLeft) / float64(p.DownloadsTotal)
				// }
				// ui.Progress(c, value).
				// 	Label(fmt.Sprintf("剩余 %d / %d", p.DownloadsLeft, p.DownloadsTotal))
			})

			if p.QuotaKnown {
				ui.Row(c).Gap(14).AlignItems(ui.Stretch).Children(func() {
					a.statCard(c, "每日配额", fmt.Sprint(p.DownloadsTotal))
					a.statCard(c, "今日下载", fmt.Sprint(p.Downloads))
					a.statCard(c, "剩余配额", fmt.Sprint(p.DownloadsLeft))
				})
			}
		}
	})
}

func (a *App) statCard(c *ui.Context, label, value string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(16).Gap(4).Grow(1).Basis(0).
		Background(t.Background).Border(1, t.Border).Radius(10).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(24).Bold()
	})
}
