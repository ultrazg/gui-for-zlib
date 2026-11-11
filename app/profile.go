package app

import (
	"context"
	"strings"

	"zlib-gui/core"
)

type profileState struct {
	Data    *core.Profile
	Loading bool
	Loaded  bool
	Err     string
	seq     uint64
}

func (a *App) ensureProfile() {
	if a.profile.Loaded || a.profile.Loading || a.session == nil {
		return
	}
	a.loadProfile()
}

// resetAfterAuthChange drops every per-session result: profile data and the
// cached favorites/history pages. Library pages reload on their next visit.
func (a *App) resetAfterAuthChange() {
	a.resetProfile()
	a.library = map[string]*libraryState{}
	a.resetFavorites()
	a.detail.Open = false
	a.detail.Book.Saved = nil
}

// resetProfile invalidates an in-flight load, so a result from the previous
// session cannot land after signing out or signing in again.
func (a *App) resetProfile() {
	a.profile.seq++
	a.profile.Data = nil
	a.profile.Loading = false
	a.profile.Loaded = false
	a.profile.Err = ""
}

func (a *App) loadProfile() {
	p := &a.profile
	p.seq++
	seq := p.seq
	p.Loading = true
	p.Err = ""

	go func() {
		profile, err := a.svc.Profile(context.Background())
		a.update(func() {
			if seq != p.seq {
				return
			}
			p.Loading = false
			p.Loaded = true
			if err != nil {
				p.Err = friendlyError(err)
				return
			}
			p.Data = profile
			a.adoptProfileName(profile.Name)
		})
	}()
}

// adoptProfileName replaces the placeholder a restored session carries (the
// email's local part, or nothing) with the server's nickname, and asks the
// backend to persist it so the next start opens on the right name.
func (a *App) adoptProfileName(name string) {
	name = strings.TrimSpace(name)
	if a.session == nil || name == "" || name == a.session.Name {
		return
	}
	if a.session.Name != "" && !strings.EqualFold(a.session.Name, emailLocalPart(a.session.Email)) {
		return
	}
	a.session.Name = name
	if s, ok := a.svc.(interface{ UpdateSessionName(string) error }); ok {
		_ = s.UpdateSessionName(name)
	}
}

func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}
