package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"zlib-gui/app"
	"zlib-gui/core"
)

// The view runs without a window in tests, which click and type as a user
// would.
func TestView(t *testing.T) {
	a := app.New(core.NewDemo())
	tt := ui.NewTester(a.View, 1180, 760)

	if !tt.HasText("欢迎使用 GUI for zlib") {
		t.Fatalf("texts %q do not show the home page", tt.Texts())
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("下载目录") {
		t.Errorf("texts %q do not show the settings page", tt.Texts())
	}
}

// The real backend must construct and render without network access or a
// saved session.
func TestViewWithRealBackend(t *testing.T) {
	t.Setenv("ZLIB_CONFIG_DIR", t.TempDir())
	t.Setenv("ZLIB_DOMAIN", "https://z-lib.gd")
	t.Setenv("ZLIB_PROXY", "")

	a := app.New(core.NewBackend())
	tt := ui.NewTester(a.View, 1180, 760)

	if !tt.HasText("欢迎使用 GUI for zlib") {
		t.Fatalf("texts %q do not show the home page", tt.Texts())
	}
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("登录后可搜索") {
		t.Errorf("texts %q do not show the signed-out search hint", tt.Texts())
	}
}
