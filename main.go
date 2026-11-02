package main

import (
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"zlib-gui/app"
	"zlib-gui/core"
)

// The GUI runs the real backend: core.NewBackend wires the HTTP/EAPI/download
// core behind the same core.Service interface the views already use;
// core.NewDemo remains available for tests.
func main() {
	// One instance at a time: a second launch forwards its arguments (and any
	// zlib:// deep link) to the running app, which brings itself to the front,
	// then this process exits without creating a window.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	a := app.New(core.NewBackend())

	// Deep links are delivered on the main thread: the URL the app was
	// launched with once the window is up, and URLs a second instance
	// forwarded (OnSecondInstance runs first, the URLs after).
	mygo.App.OnOpenURL(a.HandleDeepLink)
	// A second launch should surface the running window rather than start a
	// copy.
	mygo.App.OnSecondInstance(func(_ []string, _ string) { a.BringToFront() })
	// Clicking a download-done notification opens the downloads page.
	mygo.App.OnNotificationClick(func(id string) {
		if strings.HasPrefix(id, "download-") {
			a.OpenDownloads()
		}
	})

	mygo.App.WhenReady(func() {
		// Re-registering every start is cheap; mygo.json's urlSchemes covers
		// packaged builds, this also makes the scheme work when running with
		// `go run .`.
		if err := mygo.App.RegisterURLScheme("zlib"); err != nil {
			log.Printf("mygo: register deep link scheme: %v", err)
		}

		a.Attach(mygo.NewWindow(mygo.WindowOptions{
			Title:     "GUI for zlib",
			Width:     1180,
			Height:    760,
			MinWidth:  900,
			MinHeight: 600,
			// Opens where the user left it last time.
			StateKey: "main",
			// The sidebar lets the material show through (Windows 11 Mica /
			// macOS sidebar material); the content pane stays opaque so book
			// lists keep their contrast. View adapts via ui.Context.Vibrancy.
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: 50,
			Vibrancy:       mygo.VibrancySidebar,
			// The window shows the interface MyGo draws, not a web page.
			Content: ui.View(a.View),
		}))
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
