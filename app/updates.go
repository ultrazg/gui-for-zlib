package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"zlib-gui/core"
)

type updateCheckState struct {
	Loading bool
	Open    bool
	Failed  bool
	Text    string
	Current string
	Release core.Release
}

func (a *App) updateCheckField(c *ui.Context) {
	u := &a.updateCheck
	t := c.Theme()
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Button(c.Key("check-updates"), "检查更新").Disabled(u.Loading).OnClick(a.startUpdateCheck)
		if u.Loading {
			ui.Spinner(c)
			ui.Text(c, "正在检查更新…").FontSize(12).TextColor(t.TextMuted)
		}
	})
	if u.Text != "" {
		color := t.Success
		if u.Failed {
			color = t.Danger
		}
		ui.Text(c, u.Text).FontSize(12).TextColor(color)
	}
}

func (a *App) startUpdateCheck() {
	u := &a.updateCheck
	if u.Loading {
		return
	}
	u.Text, u.Failed, u.Open = "", false, false
	u.Current = strings.TrimSpace(mygo.App.Version())
	if u.Current == "" {
		u.Text, u.Failed = "当前构建未提供版本号，无法检查更新。", true
		return
	}
	checker, ok := a.svc.(interface {
		CheckForUpdate(context.Context, string) (*core.Release, error)
	})
	if !ok {
		u.Text, u.Failed = "当前后端不支持检查更新。", true
		return
	}
	u.Loading = true
	current, win := u.Current, a.win
	go func() {
		release, err := checker.CheckForUpdate(context.Background(), current)
		a.update(func() {
			if win != nil && win.IsDestroyed() {
				return
			}
			a.finishUpdateCheck(release, err)
		})
	}()
}

// Called on the UI thread after the background request finishes.
func (a *App) finishUpdateCheck(release *core.Release, err error) {
	u := &a.updateCheck
	u.Loading, u.Open, u.Failed = false, false, err != nil
	u.Release = core.Release{}
	switch {
	case err != nil:
		u.Text = "检查更新失败：" + friendlyError(err)
	case release == nil:
		u.Text = "当前已是最新版本（" + u.Current + "）。"
	default:
		u.Release = *release
		u.Text = "发现新版本 " + release.TagName
		u.Open = true
	}
}

func (a *App) updateModal(c *ui.Context) {
	u := &a.updateCheck
	ui.Modal(c.Key("release-notes"), &u.Open, func() {
		t := c.Theme()
		width, height := c.Size()
		ui.Column(c).Width(min(640, width-80)).Height(min(480, height-100)).Gap(12).Children(func() {
			ui.Text(c, "发现新版本 "+u.Release.TagName).FontSize(22).Bold()
			ui.Text(c, "当前版本 "+u.Current).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, "更新说明").Bold()
			ui.Scroll(c.Key(u.Release.TagName)).Grow(1).MinHeight(0).Children(func() {
				if strings.TrimSpace(u.Release.Name) != "" {
					ui.Text(c, u.Release.Name).Bold().Margin(0, 0, 8)
				}
				if strings.TrimSpace(u.Release.Body) == "" {
					ui.Text(c, "此版本暂无更新说明。").TextColor(t.TextMuted)
				} else {
					markdown.View(c, u.Release.Body, nil)
				}
			})
			ui.Row(c).Gap(12).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				ui.Link(c, "前往 GitHub 发布页", u.Release.URL())
				ui.Button(c, "关闭").OnClick(func() { u.Open = false })
			})
		})
	}).Label("更新说明")
}
