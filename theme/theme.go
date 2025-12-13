// Package theme provides color theming for the terminal browser.
package theme

import "browse/render"

// Color represents an RGB color that can render to ANSI.
type Color struct {
	R, G, B uint8
}

// Theme defines the color palette for the browser.
// Document content uses terminal attributes (bold/dim/underline) not colors.
// Themes control the UI chrome: background, labels, accents, feedback.
type Theme struct {
	Name string
	Dark bool // true if this is a dark theme

	// Base colors
	Background    Color // terminal background (ignored if TransparentBg is true)
	TransparentBg bool  // if true, use terminal's native background
	Foreground    Color // default text
	Dim           Color // dimmed/comment text

	// Labels (jump hints)
	Label      Color // untyped portion of label
	LabelTyped Color // typed portion of label
	LabelDim   Color // non-matching labels

	// Accent
	Accent Color // wave spinner, active indicators

	// Feedback
	Error   Color
	Warning Color
	Success Color
	Info    Color

	// Quality indicators
	Gold Color // Known-good sites flair
}

// Style creates a render.Style with the given foreground color.
func (c Color) Style() render.Style {
	return render.Style{
		FgRGB:    [3]uint8{c.R, c.G, c.B},
		UseFgRGB: true,
	}
}

// StyleBg creates a render.Style with the given background color.
func (c Color) StyleBg() render.Style {
	return render.Style{
		BgRGB:    [3]uint8{c.R, c.G, c.B},
		UseBgRGB: true,
	}
}

// StyleFgBg creates a render.Style with foreground and background colors.
func StyleFgBg(fg, bg Color) render.Style {
	return render.Style{
		FgRGB:    [3]uint8{fg.R, fg.G, fg.B},
		UseFgRGB: true,
		BgRGB:    [3]uint8{bg.R, bg.G, bg.B},
		UseBgRGB: true,
	}
}

// BaseStyle returns the base render.Style for the theme.
// If TransparentBg is true, no colors are set (terminal defaults used).
func (t *Theme) BaseStyle() render.Style {
	if t.TransparentBg {
		return render.Style{} // use terminal's native colors
	}
	return StyleFgBg(t.Foreground, t.Background)
}

// Hex creates a Color from a hex string like "#RRGGBB" or "RRGGBB".
func Hex(s string) Color {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return Color{}
	}
	return Color{
		R: hexByte(s[0:2]),
		G: hexByte(s[2:4]),
		B: hexByte(s[4:6]),
	}
}

func hexByte(s string) uint8 {
	var v uint8
	for _, c := range s {
		v *= 16
		switch {
		case c >= '0' && c <= '9':
			v += uint8(c - '0')
		case c >= 'a' && c <= 'f':
			v += uint8(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v += uint8(c - 'A' + 10)
		}
	}
	return v
}

// RGB creates a Color from RGB values.
func RGB(r, g, b uint8) Color {
	return Color{R: r, G: g, B: b}
}

// Built-in themes
var (
	// Default - uses terminal's native background, works with any terminal theme
	DefaultDark = &Theme{
		Name:          "default-dark",
		Dark:          true,
		TransparentBg: true, // use terminal's own background
		Foreground:    Hex("e0e0e0"),
		Dim:           Hex("666666"),
		Label:         Hex("d7d700"), // yellow
		LabelTyped:    Hex("5fd75f"), // green
		LabelDim:      Hex("4a4a4a"),
		Accent:        Hex("5fd7d7"), // cyan
		Error:         Hex("d75f5f"),
		Warning:       Hex("d7af5f"),
		Success:       Hex("5fd75f"),
		Info:          Hex("5f87d7"),
		Gold:          Hex("ffd700"), // gold
	}

	DefaultLight = &Theme{
		Name:       "default-light",
		Dark:       false,
		Background: Hex("fafafa"),
		Foreground: Hex("1a1a1a"),
		Dim:        Hex("888888"),
		Label:      Hex("b58900"), // darker yellow
		LabelTyped: Hex("2e7d32"), // green
		LabelDim:   Hex("cccccc"),
		Accent:     Hex("00838f"), // teal
		Error:      Hex("c62828"),
		Warning:    Hex("f57c00"),
		Success:    Hex("2e7d32"),
		Info:       Hex("1565c0"),
		Gold:       Hex("b8860b"), // dark goldenrod
	}

	// Solarized - Ethan Schoonover's precision colors
	SolarizedDark = &Theme{
		Name:       "solarized-dark",
		Dark:       true,
		Background: Hex("002b36"), // base03
		Foreground: Hex("839496"), // base0
		Dim:        Hex("586e75"), // base01
		Label:      Hex("b58900"), // yellow
		LabelTyped: Hex("859900"), // green
		LabelDim:   Hex("073642"), // base02
		Accent:     Hex("2aa198"), // cyan
		Error:      Hex("dc322f"), // red
		Warning:    Hex("cb4b16"), // orange
		Success:    Hex("859900"), // green
		Info:       Hex("268bd2"),
		Gold:          Hex("b58900"), // blue
	}

	SolarizedLight = &Theme{
		Name:       "solarized-light",
		Dark:       false,
		Background: Hex("fdf6e3"), // base3
		Foreground: Hex("657b83"), // base00
		Dim:        Hex("93a1a1"), // base1
		Label:      Hex("b58900"), // yellow
		LabelTyped: Hex("859900"), // green
		LabelDim:   Hex("eee8d5"), // base2
		Accent:     Hex("2aa198"), // cyan
		Error:      Hex("dc322f"), // red
		Warning:    Hex("cb4b16"), // orange
		Success:    Hex("859900"), // green
		Info:       Hex("268bd2"),
		Gold:          Hex("b58900"), // blue
	}

	// Nord - Arctic, north-bluish color palette
	Nord = &Theme{
		Name:       "nord",
		Dark:       true,
		Background: Hex("2e3440"), // nord0
		Foreground: Hex("d8dee9"), // nord4
		Dim:        Hex("4c566a"), // nord3
		Label:      Hex("ebcb8b"), // nord13 (yellow)
		LabelTyped: Hex("a3be8c"), // nord14 (green)
		LabelDim:   Hex("3b4252"), // nord1
		Accent:     Hex("88c0d0"), // nord8 (frost)
		Error:      Hex("bf616a"), // nord11 (red)
		Warning:    Hex("d08770"), // nord12 (orange)
		Success:    Hex("a3be8c"), // nord14 (green)
		Info:       Hex("81a1c1"),
		Gold:          Hex("ebcb8b"), // nord9 (frost)
	}

	// Dracula - Dark theme with vivid colors
	Dracula = &Theme{
		Name:       "dracula",
		Dark:       true,
		Background: Hex("282a36"),
		Foreground: Hex("f8f8f2"),
		Dim:        Hex("6272a4"), // comment
		Label:      Hex("f1fa8c"), // yellow
		LabelTyped: Hex("50fa7b"), // green
		LabelDim:   Hex("44475a"), // current line
		Accent:     Hex("8be9fd"), // cyan
		Error:      Hex("ff5555"), // red
		Warning:    Hex("ffb86c"), // orange
		Success:    Hex("50fa7b"), // green
		Info:       Hex("bd93f9"),
		Gold:          Hex("f1fa8c"), // purple
	}

	// Gruvbox - Retro groove color scheme
	GruvboxDark = &Theme{
		Name:       "gruvbox-dark",
		Dark:       true,
		Background: Hex("282828"), // bg
		Foreground: Hex("ebdbb2"), // fg
		Dim:        Hex("928374"), // gray
		Label:      Hex("fabd2f"), // yellow
		LabelTyped: Hex("b8bb26"), // green
		LabelDim:   Hex("3c3836"), // bg1
		Accent:     Hex("8ec07c"), // aqua
		Error:      Hex("fb4934"), // red
		Warning:    Hex("fe8019"), // orange
		Success:    Hex("b8bb26"), // green
		Info:       Hex("83a598"),
		Gold:          Hex("d79921"), // blue
	}

	GruvboxLight = &Theme{
		Name:       "gruvbox-light",
		Dark:       false,
		Background: Hex("fbf1c7"), // bg
		Foreground: Hex("3c3836"), // fg
		Dim:        Hex("928374"), // gray
		Label:      Hex("b57614"), // yellow
		LabelTyped: Hex("79740e"), // green
		LabelDim:   Hex("ebdbb2"), // bg1
		Accent:     Hex("427b58"), // aqua
		Error:      Hex("9d0006"), // red
		Warning:    Hex("af3a03"), // orange
		Success:    Hex("79740e"), // green
		Info:       Hex("076678"),
		Gold:          Hex("b57614"), // blue
	}

	// Tokyo Night - Clean dark theme inspired by Tokyo city lights
	TokyoNight = &Theme{
		Name:       "tokyo-night",
		Dark:       true,
		Background: Hex("1a1b26"),
		Foreground: Hex("a9b1d6"),
		Dim:        Hex("565f89"), // comment
		Label:      Hex("e0af68"), // yellow
		LabelTyped: Hex("9ece6a"), // green
		LabelDim:   Hex("24283b"), // bg highlight
		Accent:     Hex("7dcfff"), // cyan
		Error:      Hex("f7768e"), // red
		Warning:    Hex("ff9e64"), // orange
		Success:    Hex("9ece6a"), // green
		Info:       Hex("7aa2f7"),
		Gold:          Hex("e0af68"), // blue
	}

	// Monokai - Classic dark theme
	Monokai = &Theme{
		Name:       "monokai",
		Dark:       true,
		Background: Hex("272822"),
		Foreground: Hex("f8f8f2"),
		Dim:        Hex("75715e"), // comment
		Label:      Hex("e6db74"), // yellow
		LabelTyped: Hex("a6e22e"), // green
		LabelDim:   Hex("3e3d32"), // line highlight
		Accent:     Hex("66d9ef"), // cyan
		Error:      Hex("f92672"), // red/pink
		Warning:    Hex("fd971f"), // orange
		Success:    Hex("a6e22e"), // green
		Info:       Hex("ae81ff"),
		Gold:          Hex("e6db74"), // purple
	}

	// Rose Pine - All natural pine, faux fur and a bit of soho vibes
	RosePine = &Theme{
		Name:       "rose-pine",
		Dark:       true,
		Background: Hex("191724"), // base
		Foreground: Hex("e0def4"), // text
		Dim:        Hex("6e6a86"), // muted
		Label:      Hex("f6c177"), // gold
		LabelTyped: Hex("31748f"), // pine
		LabelDim:   Hex("26233a"), // surface
		Accent:     Hex("9ccfd8"), // foam
		Error:      Hex("eb6f92"), // love
		Warning:    Hex("f6c177"), // gold
		Success:    Hex("31748f"), // pine
		Info:       Hex("c4a7e7"),
		Gold:          Hex("f6c177"), // iris
	}

	RosePineMoon = &Theme{
		Name:       "rose-pine-moon",
		Dark:       true,
		Background: Hex("232136"), // base
		Foreground: Hex("e0def4"), // text
		Dim:        Hex("6e6a86"), // muted
		Label:      Hex("f6c177"), // gold
		LabelTyped: Hex("3e8fb0"), // pine
		LabelDim:   Hex("2a273f"), // surface
		Accent:     Hex("9ccfd8"), // foam
		Error:      Hex("eb6f92"), // love
		Warning:    Hex("f6c177"), // gold
		Success:    Hex("3e8fb0"), // pine
		Info:       Hex("c4a7e7"),
		Gold:          Hex("f6c177"), // iris
	}

	RosePineDawn = &Theme{
		Name:       "rose-pine-dawn",
		Dark:       false,
		Background: Hex("faf4ed"), // base
		Foreground: Hex("575279"), // text
		Dim:        Hex("9893a5"), // muted
		Label:      Hex("ea9d34"), // gold
		LabelTyped: Hex("286983"), // pine
		LabelDim:   Hex("f2e9e1"), // surface
		Accent:     Hex("56949f"), // foam
		Error:      Hex("b4637a"), // love
		Warning:    Hex("ea9d34"), // gold
		Success:    Hex("286983"), // pine
		Info:       Hex("907aa9"),
		Gold:          Hex("ea9d34"), // iris
	}

	// Catppuccin - Soothing pastel theme
	CatppuccinMocha = &Theme{
		Name:       "catppuccin-mocha",
		Dark:       true,
		Background: Hex("1e1e2e"), // base
		Foreground: Hex("cdd6f4"), // text
		Dim:        Hex("6c7086"), // overlay0
		Label:      Hex("f9e2af"), // yellow
		LabelTyped: Hex("a6e3a1"), // green
		LabelDim:   Hex("313244"), // surface0
		Accent:     Hex("89dceb"), // sky
		Error:      Hex("f38ba8"), // red
		Warning:    Hex("fab387"), // peach
		Success:    Hex("a6e3a1"), // green
		Info:       Hex("89b4fa"),
		Gold:          Hex("f9e2af"), // blue
	}

	CatppuccinLatte = &Theme{
		Name:       "catppuccin-latte",
		Dark:       false,
		Background: Hex("eff1f5"), // base
		Foreground: Hex("4c4f69"), // text
		Dim:        Hex("9ca0b0"), // overlay0
		Label:      Hex("df8e1d"), // yellow
		LabelTyped: Hex("40a02b"), // green
		LabelDim:   Hex("ccd0da"), // surface0
		Accent:     Hex("04a5e5"), // sky
		Error:      Hex("d20f39"), // red
		Warning:    Hex("fe640b"), // peach
		Success:    Hex("40a02b"), // green
		Info:       Hex("1e66f5"),
		Gold:          Hex("df8e1d"), // blue
	}

	// One Dark - Atom's iconic dark theme
	OneDark = &Theme{
		Name:       "one-dark",
		Dark:       true,
		Background: Hex("282c34"),
		Foreground: Hex("abb2bf"),
		Dim:        Hex("5c6370"), // comment
		Label:      Hex("e5c07b"), // yellow
		LabelTyped: Hex("98c379"), // green
		LabelDim:   Hex("3e4451"), // gutter
		Accent:     Hex("56b6c2"), // cyan
		Error:      Hex("e06c75"), // red
		Warning:    Hex("d19a66"), // orange
		Success:    Hex("98c379"), // green
		Info:       Hex("61afef"),
		Gold:          Hex("e5c07b"), // blue
	}

	// Kanagawa - Inspired by Katsushika Hokusai's The Great Wave
	Kanagawa = &Theme{
		Name:       "kanagawa",
		Dark:       true,
		Background: Hex("1f1f28"), // sumiInk1
		Foreground: Hex("dcd7ba"), // fujiWhite
		Dim:        Hex("727169"), // fujiGray
		Label:      Hex("c0a36e"), // boatYellow2
		LabelTyped: Hex("76946a"), // autumnGreen
		LabelDim:   Hex("2a2a37"), // sumiInk3
		Accent:     Hex("7e9cd8"), // crystalBlue
		Error:      Hex("c34043"), // autumnRed
		Warning:    Hex("ffa066"), // surimiOrange
		Success:    Hex("76946a"), // autumnGreen
		Info:       Hex("7fb4ca"),
		Gold:          Hex("c0a36e"), // springBlue
	}

	// Everforest - Green-tinted, easy on the eyes
	EverforestDark = &Theme{
		Name:       "everforest-dark",
		Dark:       true,
		Background: Hex("2d353b"), // bg0
		Foreground: Hex("d3c6aa"), // fg
		Dim:        Hex("859289"), // grey1
		Label:      Hex("dbbc7f"), // yellow
		LabelTyped: Hex("a7c080"), // green
		LabelDim:   Hex("3d484d"), // bg1
		Accent:     Hex("83c092"), // aqua
		Error:      Hex("e67e80"), // red
		Warning:    Hex("e69875"), // orange
		Success:    Hex("a7c080"), // green
		Info:       Hex("7fbbb3"),
		Gold:          Hex("dbbc7f"), // blue
	}

	EverforestLight = &Theme{
		Name:       "everforest-light",
		Dark:       false,
		Background: Hex("fdf6e3"), // bg0
		Foreground: Hex("5c6a72"), // fg
		Dim:        Hex("939f91"), // grey1
		Label:      Hex("dfa000"), // yellow
		LabelTyped: Hex("8da101"), // green
		LabelDim:   Hex("f3ead3"), // bg1
		Accent:     Hex("35a77c"), // aqua
		Error:      Hex("f85552"), // red
		Warning:    Hex("f57d26"), // orange
		Success:    Hex("8da101"), // green
		Info:       Hex("3a94c5"),
		Gold:          Hex("dfa000"), // blue
	}

	// Ayu - Clean, modern theme
	AyuDark = &Theme{
		Name:       "ayu-dark",
		Dark:       true,
		Background: Hex("0d1017"), // background
		Foreground: Hex("bfbdb6"), // foreground
		Dim:        Hex("636a72"), // comment
		Label:      Hex("ffb454"), // yellow/func
		LabelTyped: Hex("aad94c"), // green
		LabelDim:   Hex("131721"), // line
		Accent:     Hex("39bae6"), // tag/cyan
		Error:      Hex("d95757"), // error
		Warning:    Hex("ffb454"), // yellow
		Success:    Hex("aad94c"), // green
		Info:       Hex("59c2ff"),
		Gold:          Hex("ffb454"), // blue
	}

	AyuMirage = &Theme{
		Name:       "ayu-mirage",
		Dark:       true,
		Background: Hex("1f2430"), // background
		Foreground: Hex("cbccc6"), // foreground
		Dim:        Hex("5c6773"), // comment
		Label:      Hex("ffd580"), // yellow
		LabelTyped: Hex("bae67e"), // green
		LabelDim:   Hex("232834"), // line
		Accent:     Hex("5ccfe6"), // cyan
		Error:      Hex("ff6666"), // error
		Warning:    Hex("ffcc66"), // yellow
		Success:    Hex("bae67e"), // green
		Info:       Hex("73d0ff"),
		Gold:          Hex("ffd580"), // blue
	}

	AyuLight = &Theme{
		Name:       "ayu-light",
		Dark:       false,
		Background: Hex("fafafa"), // background
		Foreground: Hex("575f66"), // foreground
		Dim:        Hex("abb0b6"), // comment
		Label:      Hex("f2ae49"), // yellow
		LabelTyped: Hex("86b300"), // green
		LabelDim:   Hex("f0f0f0"), // line
		Accent:     Hex("55b4d4"), // cyan
		Error:      Hex("f51818"), // error
		Warning:    Hex("fa8d3e"), // orange
		Success:    Hex("86b300"), // green
		Info:       Hex("399ee6"),
		Gold:          Hex("f2ae49"), // blue
	}
)

// All contains all built-in themes for iteration.
var All = []*Theme{
	DefaultDark,
	DefaultLight,
	SolarizedDark,
	SolarizedLight,
	Nord,
	Dracula,
	GruvboxDark,
	GruvboxLight,
	TokyoNight,
	Monokai,
	RosePine,
	RosePineMoon,
	RosePineDawn,
	CatppuccinMocha,
	CatppuccinLatte,
	OneDark,
	Kanagawa,
	EverforestDark,
	EverforestLight,
	AyuDark,
	AyuMirage,
	AyuLight,
}

// Current is the active theme.
var Current = DefaultDark

// currentIndex tracks position in All for cycling.
var currentIndex = 0

// Set changes to a specific theme by name.
func Set(name string) bool {
	for i, t := range All {
		if t.Name == name {
			Current = t
			currentIndex = i
			return true
		}
	}
	return false
}

// Next cycles to the next theme.
func Next() {
	currentIndex = (currentIndex + 1) % len(All)
	Current = All[currentIndex]
}

// Prev cycles to the previous theme.
func Prev() {
	currentIndex = (currentIndex - 1 + len(All)) % len(All)
	Current = All[currentIndex]
}

// Toggle switches between light and dark variants if available.
// If current theme has no variant, cycles to next theme of opposite type.
func Toggle() {
	// Try to find the variant of current theme
	baseName := Current.Name
	if Current.Dark {
		// Look for light version
		lightName := baseName
		if len(baseName) > 5 && baseName[len(baseName)-5:] == "-dark" {
			lightName = baseName[:len(baseName)-5] + "-light"
		}
		if Set(lightName) {
			return
		}
	} else {
		// Look for dark version
		darkName := baseName
		if len(baseName) > 6 && baseName[len(baseName)-6:] == "-light" {
			darkName = baseName[:len(baseName)-6] + "-dark"
		}
		if Set(darkName) {
			return
		}
	}
	// No variant found, just cycle
	Next()
}
