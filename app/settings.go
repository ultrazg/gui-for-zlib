package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type settingsState struct {
	DownloadDir string
	Extension   string
	Count       string

	// 网络代理
	ProxyEnabled bool
	ProxyScheme  string
	ProxyAddr    string
	ProxyPort    string
}

// proxyTestState drives the 连接测试 button. The seq guard keeps a stale
// result from overwriting a newer run, exactly like doctorState.
type proxyTestState struct {
	Loading bool
	OK      bool
	Text    string
	seq     uint64
}

// proxySchemeOptions are the proxy type choices. The values ARE the scheme
// strings, mirroring the extensionOptions idiom.
func proxySchemeOptions() []string { return []string{"socks5", "http"} }

func (a *App) settingsPage(c *ui.Context) {
	t := c.Theme()
	s := &a.settings

	ui.Scroll(c).Grow(1).Padding(24, 28).Gap(16).Children(func() {
		ui.Text(c, "设置").FontSize(22).Bold()
		ui.Text(c, "通用").Bold()
		ui.Text(c, "下载目录、默认格式与每页数量写入 config.json，重启后恢复。").
			FontSize(12).TextColor(t.TextMuted)

		ui.Column(c).MaxWidth(640).Gap(12).Children(func() {
			ui.Form(c, func() {
				ui.Field(c, "下载目录", func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &s.DownloadDir).Grow(1)
						if ui.Button(c, "浏览目录").Clicked() {
							a.pickDownloadDir()
						}
					})
				})
				ui.Field(c, "默认格式", func() {
					ui.Select(c, &s.Extension, extensionOptions())
				})
				ui.Field(c, "每页数量", func() {
					ui.TextInput(c, &s.Count).Placeholder("10")
				}).Description("1–100，用于搜索与列表分页。")
			})

			ui.Divider(c)
			ui.Text(c, "网络代理").Bold()
			ui.Text(c, "通过 SOCKS5 或 HTTP 代理转发全部网络请求（搜索、推荐、封面、下载、域名检测与登录预检）；点击「保存设置」后生效。").
				FontSize(12).TextColor(t.TextMuted)
			ui.Form(c, func() {
				ui.Field(c, "启用代理", func() {
					if ui.Switch(c, &s.ProxyEnabled).Changed() {
						a.proxyTest.Text = ""
					}
				})
				ui.Field(c, "代理类型", func() {
					ui.Select(c, &s.ProxyScheme, proxySchemeOptions()).Disabled(!s.ProxyEnabled)
				})
				ui.Field(c, "代理地址", func() {
					ui.TextInput(c, &s.ProxyAddr).Placeholder("127.0.0.1").Disabled(!s.ProxyEnabled)
				})
				ui.Field(c, "代理端口", func() {
					ui.TextInput(c, &s.ProxyPort).Placeholder("7890").Disabled(!s.ProxyEnabled)
				})
				ui.Field(c, "连接测试", func() {
					a.proxyTestField(c, t)
				})
			})

			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c, "保存设置").Clicked() {
					n, err := strconv.Atoi(s.Count)
					if err != nil || n < 1 || n > 100 {
						a.toast("每页数量需在 1–100 之间")
						return
					}
					if !a.applyProxyFromSettings() {
						return
					}
					a.search.Extension = s.Extension
					if a.search.Started {
						a.startSearch(1)
					}
					if a.persistSettings() {
						a.toast("设置已保存")
					} else {
						a.toast("设置已保存（仅本次运行）")
					}
				}
			})
		})

		ui.Divider(c)
		ui.Text(c, "本地文件").Bold()
		ui.Text(c, "会话与配置位于 ~/.config/zlib（session.json / config.json）").FontSize(12).TextColor(t.TextMuted)

		ui.Divider(c)
		ui.Text(c, "关于").Bold()
		// The upstream project's name is a link inside the sentence; the
		// surrounding text keeps the line's dimmed style.
		ui.RichText(c).FontSize(12).TextColor(t.TextMuted).Children(func() {
			ui.Text(c, "一款基于 ")
			ui.Link(c, "heartleo/zlib", core.UpstreamURL)
			ui.Text(c, " 的 Z-Library 桌面客户端。")
		})
		// The version line is skipped in builds without package info (a bare
		// `go run .` or a test binary); mygo dev/build inject it from mygo.json.
		if v := mygo.App.Version(); v != "" {
			ui.Text(c, "版本 "+v).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Link(c, "GitHub", core.ProjectURL)
		})
		a.updateCheckField(c)
	})
}

// proxyTestField renders the 测试连接 button plus a spinner and inline result.
func (a *App) proxyTestField(c *ui.Context, t *ui.Theme) {
	p := &a.proxyTest
	editable := a.settings.ProxyEnabled &&
		strings.TrimSpace(a.settings.ProxyAddr) != "" &&
		strings.TrimSpace(a.settings.ProxyPort) != ""

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		btn := ui.Button(c, "测试连接")
		btn.Disabled(p.Loading || !editable)
		if btn.Clicked() {
			a.startProxyTest()
		}
		if p.Loading {
			ui.Spinner(c)
			ui.Text(c, "正在测试").FontSize(12).TextColor(t.TextMuted)
		} else if p.Text != "" {
			color := t.Success
			if !p.OK {
				color = t.Danger
			}
			ui.Text(c, p.Text).FontSize(12).TextColor(color)
		}
	})
}

// startProxyTest probes the CURRENT form values (not the last saved ones) in
// a goroutine; results land on the main thread via a.update, guarded by seq.
func (a *App) startProxyTest() {
	p := &a.proxyTest
	tester, ok := a.svc.(interface {
		TestProxy(context.Context, string) (time.Duration, error)
	})
	if !ok {
		a.toast("当前后端不支持代理测试")
		return
	}
	proxy, err := core.ProxyURLFromParts(a.settings.ProxyScheme, a.settings.ProxyAddr, a.settings.ProxyPort)
	if err != nil {
		p.OK = false
		p.Text = friendlyError(err)
		return
	}
	p.seq++
	seq := p.seq
	p.Loading = true
	p.Text = ""
	go func() {
		latency, err := tester.TestProxy(context.Background(), proxy)
		a.update(func() {
			if seq != p.seq {
				return
			}
			p.Loading = false
			if err != nil {
				p.OK = false
				p.Text = friendlyError(err)
				return
			}
			p.OK = true
			p.Text = fmt.Sprintf("连接成功（%s）", latency.Round(time.Millisecond))
		})
	}()
}

// applyProxyFromSettings pushes the form's current proxy onto the backend
// before the config is persisted. It returns false (aborting the save) only
// when an enabled proxy fails to parse or apply, mirroring the page-count
// validation just above.
func (a *App) applyProxyFromSettings() bool {
	setter, ok := a.svc.(interface{ SetProxy(string) error })
	if !ok {
		if a.settings.ProxyEnabled {
			a.toast("当前后端不支持代理设置，已保存但不生效")
		}
		return true
	}
	if !a.settings.ProxyEnabled {
		if err := setter.SetProxy(""); err != nil {
			a.toast("代理设置无效：" + friendlyError(err))
			return false
		}
		return true
	}
	proxy, err := core.ProxyURLFromParts(a.settings.ProxyScheme, a.settings.ProxyAddr, a.settings.ProxyPort)
	if err != nil {
		a.toast("代理设置无效：" + friendlyError(err))
		return false
	}
	if err := setter.SetProxy(proxy); err != nil {
		a.toast("代理设置无效：" + friendlyError(err))
		return false
	}
	return true
}
