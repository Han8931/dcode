// Package theme holds d-code's UI color palette in one place, so the terminal
// UI reads as one coherent, modern design instead of scattered ad-hoc ANSI
// codes. Both front-of-house packages (tui chrome and the editor's badges and
// syntax highlighting) source their colors here.
//
// The palette is truecolor (24-bit hex). lipgloss downsamples gracefully on
// 256- or 16-color terminals, so these values render crisply where truecolor is
// available and still degrade cleanly where it isn't. The default is Catppuccin
// Mocha — a warm, low-glare dark scheme — kept as one struct so alternate
// palettes can be added and selected later (a :theme command).
package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette is the app's semantic color set: a ramp of backgrounds (darkest →
// lightest), a ramp of foregrounds (dimmest → brightest), and named accents.
type Palette struct {
	// Backgrounds / surfaces, darkest → lightest.
	Crust    lipgloss.Color // deepest — used as text ON bright accents
	Base     lipgloss.Color // app background tone
	Mantle   lipgloss.Color // slightly raised (status bar, code wash)
	Surface0 lipgloss.Color // panels / headers
	Surface1 lipgloss.Color // selection bar, hovered rows
	Surface2 lipgloss.Color // rules, faint separators

	// Foregrounds, dimmest → brightest.
	Overlay lipgloss.Color // hints, comments, muted metadata
	Subtext lipgloss.Color // secondary text
	Text    lipgloss.Color // primary text

	// Accents.
	Blue     lipgloss.Color // primary accent — focus, prompts, links
	Lavender lipgloss.Color // soft secondary accent
	Sapphire lipgloss.Color
	Teal     lipgloss.Color // the "tutor"/AI voice
	Green    lipgloss.Color // success, Normal mode
	Yellow   lipgloss.Color // notices, types
	Peach    lipgloss.Color // warnings / work-in-progress
	Maroon   lipgloss.Color
	Red      lipgloss.Color // errors
	Pink     lipgloss.Color
	Mauve    lipgloss.Color // keywords, Visual mode
}

// Mocha is Catppuccin Mocha — the default modern dark palette.
var Mocha = Palette{
	Crust:    "#11111b",
	Base:     "#1e1e2e",
	Mantle:   "#181825",
	Surface0: "#313244",
	Surface1: "#45475a",
	Surface2: "#585b70",

	Overlay: "#6c7086",
	Subtext: "#a6adc8",
	Text:    "#cdd6f4",

	Blue:     "#89b4fa",
	Lavender: "#b4befe",
	Sapphire: "#74c7ec",
	Teal:     "#94e2d5",
	Green:    "#a6e3a1",
	Yellow:   "#f9e2af",
	Peach:    "#fab387",
	Maroon:   "#eba0ac",
	Red:      "#f38ba8",
	Pink:     "#f5c2e7",
	Mauve:    "#cba6f7",
}

// Latte is Catppuccin Latte — the light counterpart to Mocha, for bright
// terminals. Surfaces run light→dark (the ramp inverts), text runs dark.
var Latte = Palette{
	Crust:    "#dce0e8",
	Base:     "#eff1f5",
	Mantle:   "#e6e9ef",
	Surface0: "#ccd0da",
	Surface1: "#bcc0cc",
	Surface2: "#acb0be",

	Overlay: "#8c8fa1",
	Subtext: "#5c5f77",
	Text:    "#4c4f69",

	Blue:     "#1e66f5",
	Lavender: "#7287fd",
	Sapphire: "#209fb5",
	Teal:     "#179299",
	Green:    "#40a02b",
	Yellow:   "#df8e1d",
	Peach:    "#fe640b",
	Maroon:   "#e64553",
	Red:      "#d20f39",
	Pink:     "#ea76cb",
	Mauve:    "#8839ef",
}

// Dracula — the classic high-contrast dark scheme; purple is its signature
// accent, so Blue (the primary-accent slot) carries Dracula's purple.
var Dracula = Palette{
	Crust:    "#191a21",
	Base:     "#282a36",
	Mantle:   "#21222c",
	Surface0: "#343746",
	Surface1: "#44475a",
	Surface2: "#565b74",

	Overlay: "#6272a4",
	Subtext: "#b8bcc8",
	Text:    "#f8f8f2",

	Blue:     "#bd93f9",
	Lavender: "#d6acff",
	Sapphire: "#8be9fd",
	Teal:     "#8be9fd",
	Green:    "#50fa7b",
	Yellow:   "#f1fa8c",
	Peach:    "#ffb86c",
	Maroon:   "#ff6e6e",
	Red:      "#ff5555",
	Pink:     "#ff79c6",
	Mauve:    "#ff79c6",
}

// Gruvbox is Gruvbox Dark — warm, retro, low-blue-light.
var Gruvbox = Palette{
	Crust:    "#141617",
	Base:     "#282828",
	Mantle:   "#1d2021",
	Surface0: "#3c3836",
	Surface1: "#504945",
	Surface2: "#665c54",

	Overlay: "#928374",
	Subtext: "#bdae93",
	Text:    "#ebdbb2",

	Blue:     "#83a598",
	Lavender: "#d3869b",
	Sapphire: "#83a598",
	Teal:     "#8ec07c",
	Green:    "#b8bb26",
	Yellow:   "#fabd2f",
	Peach:    "#fe8019",
	Maroon:   "#d65d0e",
	Red:      "#fb4934",
	Pink:     "#d3869b",
	Mauve:    "#d3869b",
}

// Nord — cool, dim, arctic blues.
var Nord = Palette{
	Crust:    "#242933",
	Base:     "#2e3440",
	Mantle:   "#292e39",
	Surface0: "#3b4252",
	Surface1: "#434c5e",
	Surface2: "#4c566a",

	Overlay: "#616e88",
	Subtext: "#d8dee9",
	Text:    "#eceff4",

	Blue:     "#88c0d0",
	Lavender: "#b48ead",
	Sapphire: "#81a1c1",
	Teal:     "#8fbcbb",
	Green:    "#a3be8c",
	Yellow:   "#ebcb8b",
	Peach:    "#d08770",
	Maroon:   "#bf616a",
	Red:      "#bf616a",
	Pink:     "#b48ead",
	Mauve:    "#b48ead",
}

// TokyoNight is Tokyo Night — deep indigo dark scheme.
var TokyoNight = Palette{
	Crust:    "#16161e",
	Base:     "#1a1b26",
	Mantle:   "#16161e",
	Surface0: "#292e42",
	Surface1: "#3b4261",
	Surface2: "#414868",

	Overlay: "#565f89",
	Subtext: "#a9b1d6",
	Text:    "#c0caf5",

	Blue:     "#7aa2f7",
	Lavender: "#9d7cd8",
	Sapphire: "#7dcfff",
	Teal:     "#73daca",
	Green:    "#9ece6a",
	Yellow:   "#e0af68",
	Peach:    "#ff9e64",
	Maroon:   "#db4b4b",
	Red:      "#f7768e",
	Pink:     "#bb9af7",
	Mauve:    "#bb9af7",
}

// DefaultName is the theme used when none is configured.
const DefaultName = "mocha"

// registry maps a theme name (and aliases) to its palette. Names are the keys
// users type after :theme and in config.toml.
var registry = map[string]*Palette{
	"mocha":      &Mocha,
	"catppuccin": &Mocha, // alias
	"latte":      &Latte,
	"light":      &Latte, // alias
	"dracula":    &Dracula,
	"gruvbox":    &Gruvbox,
	"nord":       &Nord,
	"tokyonight": &TokyoNight,
	"tokyo":      &TokyoNight, // alias
}

// Names lists the selectable themes (canonical names only, display order).
func Names() []string {
	return []string{"mocha", "latte", "dracula", "gruvbox", "nord", "tokyonight"}
}

// Current is the active palette. Styles are rebuilt from it whenever Set
// switches themes (each UI package re-applies its styles).
var Current = Mocha

// currentName tracks which theme Current holds, for display.
var currentName = DefaultName

// CurrentName returns the active theme's canonical name.
func CurrentName() string { return currentName }

// Set switches the active palette by name (case-insensitive; "default" means
// the built-in default). It reports whether the name was known — callers must
// then re-apply their packages' styles to make the change visible.
func Set(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "default" {
		n = DefaultName
	}
	p, ok := registry[n]
	if !ok {
		return false
	}
	Current = *p
	// Record the canonical name, not the alias typed.
	for _, canon := range Names() {
		if registry[canon] == p {
			currentName = canon
			break
		}
	}
	return true
}
