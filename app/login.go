package app

import (
	"context"
	"errors"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

type loginState struct {
	Open     bool
	Email    string
	Password string
	Err      string
	Pending  bool
	seq      uint64
}

func (a *App) openLogin() {
	a.login.Open = true
	a.login.Err = ""
	a.login.Password = ""
	if a.login.Email == "" {
		if l, ok := a.svc.(interface{ LastLoginEmail() string }); ok {
			a.login.Email = l.LastLoginEmail()
		}
	}
}

func (a *App) startLogin() {
	email := strings.TrimSpace(a.login.Email)
	password := a.login.Password
	switch {
	case !strings.Contains(email, "@"):
		a.login.Err = "请输入有效的邮箱地址。"
		return
	case len(password) < 3:
		a.login.Err = "密码至少 3 位。"
		return
	}

	a.login.seq++
	seq := a.login.seq
	a.login.Pending = true
	a.login.Err = ""

	go func() {
		s, err := a.svc.Login(context.Background(), email, password)
		a.update(func() {
			if seq != a.login.seq {
				return
			}
			a.login.Pending = false
			if err != nil {
				a.login.Err = loginError(err)
				return
			}
			a.session = s
			if d, ok := a.svc.(interface{ CurrentDomain() string }); ok {
				a.domain = d.CurrentDomain()
			}
			a.resetAfterAuthChange()
			a.login.Open = false
			a.login.Password = ""
			a.toast("登录成功")
			if a.search.Query != "" {
				a.startSearch(1)
			}
		})
	}()
}

// startCookieImport is the alternative sign-in path when the account has
// EAPI password login disabled ("Authorization failed"): import a Netscape
// cookies.txt exported from a browser session.
func (a *App) startCookieImport() {
	importer, ok := a.svc.(interface {
		ImportCookiesFile(path, domain string) (*core.Session, error)
	})
	if !ok {
		a.login.Err = "当前后端不支持导入 Cookie。"
		return
	}
	if a.win == nil {
		a.login.Err = "窗口未就绪，无法打开文件选择器。"
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win,
		Title:  "选择 Netscape Cookie 文件",
	})
	if err != nil {
		a.login.Err = "打开文件选择器失败：" + err.Error()
		return
	}
	if len(paths) == 0 {
		return
	}

	a.login.seq++
	seq := a.login.seq
	a.login.Pending = true
	a.login.Err = ""
	go func() {
		session, err := importer.ImportCookiesFile(paths[0], "")
		a.update(func() {
			if seq != a.login.seq {
				return
			}
			a.login.Pending = false
			if err != nil {
				a.login.Err = err.Error()
				return
			}
			a.session = session
			a.resetAfterAuthChange()
			a.login.Open = false
			a.toast("已通过 Cookie 登录")
		})
	}()
}

func loginError(err error) string {
	switch {
	case errors.Is(err, core.ErrInvalidCredentials):
		return "邮箱或密码不正确。"
	case errors.Is(err, context.Canceled):
		return "登录已取消。"
	case errors.Is(err, core.ErrNoDomain):
		return "没有可用的镜像，请先在“域名检测”页选择一个可用域名。"
	case errors.Is(err, core.ErrBotProtection):
		return "请求被反爬拦截，请换一个镜像或稍后重试。"
	case errors.Is(err, core.ErrLoginFailed):
		return "登录失败：" + err.Error()
	default:
		return err.Error()
	}
}

func (a *App) loginModal(c *ui.Context) {
	modal := ui.Modal(c, &a.login.Open, func() {
		t := c.Theme()
		ui.Column(c).Width(380).Gap(14).Children(func() {
			ui.Text(c, "登录 zlib").FontSize(20).Bold()
			subtitle := "登录前会先探测当前镜像是否可用。"
			if a.domain != "" {
				subtitle = "当前镜像：" + a.domain + "；登录前会先探测是否可用。"
			}
			ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted)

			ui.Form(c, func() {
				ui.Field(c, "邮箱", func() {
					ui.TextInput(c, &a.login.Email).
						Placeholder("you@example.com").AutoFocus()
				})
				ui.Field(c, "密码", func() {
					field := ui.TextInput(c, &a.login.Password).Password()
					if field.Submitted() {
						a.startLogin()
					}
				})
			})

			if a.login.Err != "" {
				ui.Text(c, a.login.Err).FontSize(12).TextColor(t.Danger)
			}

			ui.Row(c).Gap(10).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				ui.Link(c, "前往注册", core.RegistrationURL(a.domain))
				if ui.Button(c, "导入 Cookie").Clicked() {
					a.startCookieImport()
				}
				if ui.Button(c, "取消").Clicked() {
					a.login.Open = false
				}
				if ui.PrimaryButton(c, "").Disabled(a.login.Pending).Children(func() {
					if a.login.Pending {
						ui.Spinner(c)
					} else {
						ui.Text(c, "登录")
					}
				}).Clicked() {
					a.startLogin()
				}
			})
		})
	})

	// Closing the dialog (Escape, a click outside, or Cancel) abandons a
	// pending sign-in: bump the sequence so the late result is dropped.
	if (modal.Dismissed() || !a.login.Open) && a.login.Pending {
		a.login.seq++
		a.login.Pending = false
	}
}
