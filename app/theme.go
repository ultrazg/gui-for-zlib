package app

import (
	"github.com/egoist/mygo/ui"
)

const systemTheme = "跟随系统"

// palette maps the app-level theme tokens onto ui.Theme via applyTheme.
type palette struct {
	dark bool

	background     string
	surface        string
	surfaceHover   string
	surfacePressed string
	border         string
	text           string
	textMuted      string
	accent         string
	accentHover    string
	accentPressed  string
	accentText     string
	danger         string
	warning        string
	success        string
	selection      string
	focus          string
	scrollbar      string
}

var themePalettes = map[string]palette{
	"Catppuccin Mocha": {
		dark:       true,
		background: "#1e1e2e", surface: "#313244", surfaceHover: "#45475a",
		surfacePressed: "#585b70", border: "#45475a", text: "#cdd6f4",
		textMuted: "#a6adc8", accent: "#cba6f7", accentHover: "#d7b9ff",
		accentPressed: "#b4befe", accentText: "#1e1e2e", danger: "#f38ba8",
		warning: "#fab387", success: "#a6e3a1", selection: "#585b70",
		focus: "#89b4fa", scrollbar: "#585b70",
	},
	"Gruvbox Dark": {
		dark:       true,
		background: "#282828", surface: "#3c3836", surfaceHover: "#504945",
		surfacePressed: "#665c54", border: "#504945", text: "#ebdbb2",
		textMuted: "#a89984", accent: "#fabd2f", accentHover: "#ffd75f",
		accentPressed: "#d79921", accentText: "#282828", danger: "#fb4934",
		warning: "#fe8019", success: "#b8bb26", selection: "#504945",
		focus: "#83a598", scrollbar: "#665c54",
	},
	"GitHub Light": {
		dark:       false,
		background: "#ffffff", surface: "#f6f8fa", surfaceHover: "#eef1f4",
		surfacePressed: "#e2e6ea", border: "#d0d7de", text: "#1f2328",
		textMuted: "#656d76", accent: "#0969da", accentHover: "#0b74e8",
		accentPressed: "#0550ae", accentText: "#ffffff", danger: "#cf222e",
		warning: "#9a6700", success: "#1a7f37", selection: "#b6d7ff",
		focus: "#0969da", scrollbar: "#c9d1d9",
	},
	"Solarized Light": {
		dark:       false,
		background: "#fdf6e3", surface: "#eee8d5", surfaceHover: "#e6dfc8",
		surfacePressed: "#ddd6c1", border: "#d5cfb8", text: "#073642",
		textMuted: "#657b83", accent: "#268bd2", accentHover: "#2f9ae0",
		accentPressed: "#1a6fa8", accentText: "#ffffff", danger: "#dc322f",
		warning: "#cb4b16", success: "#859900", selection: "#d5e5ef",
		focus: "#268bd2", scrollbar: "#c7bfa8",
	},
}

// applyTheme runs every frame. "跟随系统" leaves the default theme alone so
// it keeps following the desktop's light or dark appearance.
func (a *App) applyTheme(c *ui.Context) {
	p, ok := themePalettes[a.themeName]
	if !ok {
		return
	}
	applyPalette(c, p)
}

func applyPalette(c *ui.Context, p palette) {
	t := *c.Theme()
	t.Dark = p.dark
	t.Background = ui.Hex(p.background)
	t.Surface = ui.Hex(p.surface)
	t.SurfaceHover = ui.Hex(p.surfaceHover)
	t.SurfacePressed = ui.Hex(p.surfacePressed)
	t.Border = ui.Hex(p.border)
	t.Text = ui.Hex(p.text)
	t.TextMuted = ui.Hex(p.textMuted)
	t.Accent = ui.Hex(p.accent)
	t.AccentHover = ui.Hex(p.accentHover)
	t.AccentPressed = ui.Hex(p.accentPressed)
	t.AccentText = ui.Hex(p.accentText)
	t.Danger = ui.Hex(p.danger)
	t.Warning = ui.Hex(p.warning)
	t.Success = ui.Hex(p.success)
	t.Selection = ui.Hex(p.selection)
	t.Focus = ui.Hex(p.focus)
	t.Scrollbar = ui.Hex(p.scrollbar)
	c.SetTheme(&t)
}
