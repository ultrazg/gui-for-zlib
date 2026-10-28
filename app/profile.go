package app

import (
	"context"

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
		})
	}()
}
