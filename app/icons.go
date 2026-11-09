package app

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed icons/*.svg
var iconFiles embed.FS

// The sidebar's icons. SidebarItem takes an *ui.SVG, so these are SVGs
// rather than SF Symbols: the sfsymbols plugin returns an element or an
// image, which the sidebar's icon slot cannot hold. Each is drawn in the
// spirit of the SF Symbol named beside it, so the app looks the same on
// every platform.
var (
	gearshape          = mustIcon("gearshape")             // gearshape
	house              = mustIcon("house")                 // house
	magnifyingglass    = mustIcon("magnifyingglass")       // magnifyingglass
	star               = mustIcon("star")                  // star
	trophy             = mustIcon("trophy")                // trophy
	heart              = mustIcon("heart")                 // heart
	clock              = mustIcon("clock")                 // clock
	squareAndArrowDown = mustIcon("square-and-arrow-down") // square.and.arrow.down
	personCropCircle   = mustIcon("person-crop-circle")    // person.crop.circle
	network            = mustIcon("network")               // network
)

func mustIcon(name string) *ui.SVG {
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		panic("app: " + err.Error())
	}
	return ui.MustParseSVG(data)
}
