package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type doctorState struct {
	Report  *core.DoctorReport
	Loading bool
	Ran     bool
	Err     string
	seq     uint64
}

func (a *App) startDoctor() {
	d := &a.doctor
	d.seq++
	seq := d.seq
	d.Loading = true
	d.Ran = true
	d.Err = ""

	go func() {
		report, err := a.svc.Doctor(context.Background())
		a.update(func() {
			if seq != d.seq {
				return
			}
			d.Loading = false
			if err != nil {
				d.Err = friendlyError(err)
				return
			}
			d.Report = report
			a.toast("域名检测完成")
		})
	}()
}

// useDomain switches the mirror after a successful probe. It runs in the
// click callback, never during view construction.
func (a *App) useDomain(domain string) {
	switcher, ok := a.svc.(interface{ UseDomain(string) error })
	if !ok {
		a.toast("当前后端不支持切换域名")
		return
	}
	if err := switcher.UseDomain(domain); err != nil {
		a.toast("切换失败：" + err.Error())
		return
	}
	a.domain = domain
	a.toast("已切换到 " + domain)
}

func (a *App) doctorPage(c *ui.Context) {
	t := c.Theme()
	d := &a.doctor

	ui.Scroll(c).Grow(1).Padding(24, 28).Gap(14).Children(func() {
		ui.Text(c, "域名检测").FontSize(22).Bold()
		ui.Text(c, "并发探测候选镜像的 EAPI 可用性，不携带账号凭据。").
			FontSize(12).TextColor(t.TextMuted)

		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c, "开始检测").Disabled(d.Loading).Clicked() {
				a.startDoctor()
			}
			if d.Loading {
				ui.Spinner(c)
				ui.Text(c, "正在检测…").TextColor(t.TextMuted)
			}
		})

		if d.Err != "" {
			ui.Text(c, d.Err).TextColor(t.Danger)
		}

		if d.Report == nil {
			if !d.Loading {
				ui.Text(c, "还没有检测结果。").TextColor(t.TextMuted)
			}
			return
		}

		ui.Textf(c, "检测时间：%s", d.Report.Checked.Format("2006-01-02 15:04:05")).
			FontSize(12).TextColor(t.TextMuted)

		ui.Column(c).Gap(0).Children(func() {
			for _, status := range d.Report.Domains {
				row := ui.Row(c).Padding(10, 12).Gap(12).AlignItems(ui.Center)
				row.Children(func() {
					ui.Text(c, status.Domain).Font("monospace").Grow(1)
					ui.Text(c, status.Latency.Round(0).String()).
						FontSize(12).TextColor(t.TextMuted)
					if status.OK {
						ui.Text(c, "正常").FontSize(12).Bold().TextColor(t.Success)
						if ui.Button(c, "使用此域名").Clicked() {
							a.useDomain(status.Domain)
						}
					} else {
						ui.Text(c, fmt.Sprintf("失败：%s", status.Error)).
							FontSize(12).TextColor(t.Danger)
					}
				})
				ui.Divider(c)
			}
		}).Border(1, t.Border).Radius(8)
	})
}
