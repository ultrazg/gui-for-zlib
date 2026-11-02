package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/core"
)

func TestUpdateCheckMissingVersion(t *testing.T) {
	previous := mygo.App.Version()
	mygo.App.SetVersion("")
	t.Cleanup(func() { mygo.App.SetVersion(previous) })
	a, tt := newTester(t)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Scroll(800, 600, 0, 1500)
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	if a.updateCheck.Loading || a.updateCheck.Open || !tt.HasText("当前构建未提供版本号") {
		t.Fatalf("missing version state: %+v, texts: %q", a.updateCheck, tt.Texts())
	}
}

func TestUpdateCheckResults(t *testing.T) {
	a, tt := newTester(t)
	a.path.Push("/settings")
	a.updateCheck.Current = "0.9.0"
	a.updateCheck.Loading = true
	tt.Frame()
	if !tt.HasText("正在检查更新") {
		t.Fatalf("loading feedback missing: %q", tt.Texts())
	}
	// A second action while loading must leave the active check intact.
	a.startUpdateCheck()
	if a.updateCheck.Current != "0.9.0" || !a.updateCheck.Loading {
		t.Fatal("a duplicate check changed the in-flight request")
	}
	a.finishUpdateCheck(nil, nil)
	tt.Frame()
	if a.updateCheck.Loading || a.updateCheck.Open || !tt.HasText("当前已是最新版本") {
		t.Fatalf("up-to-date state: %+v", a.updateCheck)
	}
	a.finishUpdateCheck(nil, errors.New("GitHub 请求受限"))
	tt.Frame()
	if a.updateCheck.Open || !tt.HasText("检查更新失败：GitHub 请求受限") {
		t.Fatalf("failed state: %+v, texts: %q", a.updateCheck, tt.Texts())
	}
	a.finishUpdateCheck(&core.Release{TagName: "v0.10.0"}, nil)
	tt.Frame()
	if !a.updateCheck.Open || !tt.HasText("此版本暂无更新说明") {
		t.Fatalf("retry state: %+v, texts: %q", a.updateCheck, tt.Texts())
	}
}

func TestUpdateModalLifecycle(t *testing.T) {
	a, tt := newTester(t)
	a.path.Push("/settings")
	a.updateCheck.Current = "0.9.0"
	release := &core.Release{
		TagName: "v0.10.0", Name: "体验改进",
		Body: "## 下载改进\n\n- 修复下载进度\n- **新增设置**\n\n" + strings.Repeat("更新说明中的长段落。\n\n", 80) + "末尾说明",
	}
	for _, size := range [][2]int{{1180, 760}, {900, 600}} {
		tt.SetSize(size[0], size[1])
		a.finishUpdateCheck(release, nil)
		tt.Frame()
		tt.Frame()
		for _, text := range []string{"发现新版本 v0.10.0", "当前版本 0.9.0", "下载改进", "前往 GitHub 发布页"} {
			if !tt.HasText(text) {
				t.Errorf("modal is missing %q", text)
			}
		}
		// Long notes scroll independently, leaving the close action usable.
		tt.Scroll(float32(size[0]/2), float32(size[1]/2), 0, 10000)
		if err := tt.Click("前往 GitHub 发布页"); err != nil {
			t.Fatal(err)
		}
		urls := tt.OpenedURLs()
		if len(urls) == 0 || urls[len(urls)-1] != release.URL() {
			t.Fatalf("release link opened %q", urls)
		}
		if err := tt.Click("关闭"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		if a.updateCheck.Open {
			t.Fatal("close action left the modal open")
		}
		// Reopening after its elements expired must still support Escape.
		a.finishUpdateCheck(release, nil)
		tt.Frame()
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if a.updateCheck.Open {
			t.Fatal("Escape left the modal open")
		}
	}
}
