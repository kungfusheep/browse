// Package config provides configuration loading for Browse using TOML.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Display settings
type Display struct {
	WideMode             bool `json:"wideMode"`
	FocusMode            bool `json:"focusMode"` // Dim non-focused paragraphs when using paragraph navigation
	ShowScrollPercentage bool `json:"showScrollPercentage"`
	ShowUrl              bool `json:"showUrl"`
}

// Search provider settings
type Search struct {
	DefaultProvider string `json:"defaultProvider"`
}

// HTTP fetching settings
type Fetcher struct {
	UserAgent      string `json:"userAgent"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	ChromePath     string `json:"chromePath"`
}

// Rendering settings
type Rendering struct {
	DefaultWidth  int  `json:"defaultWidth"`
	LatexEnabled  bool `json:"latexEnabled"`
	TablesEnabled bool `json:"tablesEnabled"`
}

// Session settings
type Session struct {
	RestoreSession bool `json:"restoreSession"`
}

// Editor settings
type Editor struct {
	Scheme string `json:"scheme"` // "emacs" or "vim"
}

// Keybindings configuration
type Keybindings struct {
	// Navigation
	Quit          string `json:"quit"`
	ScrollDown    string `json:"scrollDown"`
	ScrollUp      string `json:"scrollUp"`
	HalfPageDown  string `json:"halfPageDown"`
	HalfPageUp    string `json:"halfPageUp"`
	GoTop         string `json:"goTop"`
	GoBottom      string `json:"goBottom"`
	PrevParagraph string `json:"prevParagraph"`
	NextParagraph string `json:"nextParagraph"`
	PrevSection   string `json:"prevSection"`
	NextSection   string `json:"nextSection"`

	// Actions
	OpenUrl       string `json:"openUrl"`
	OpenInBrowser string `json:"openInBrowser"` // open current page in default browser
	Find          string `json:"find"`          // find in page
	CopyUrl       string `json:"copyUrl"`
	EditInEditor  string `json:"editInEditor"`
	FollowLink    string `json:"followLink"`
	DefineWord    string `json:"defineWord"`    // look up word definition

	// Overlays
	TableOfContents string `json:"tableOfContents"`
	SiteNavigation  string `json:"siteNavigation"`
	LinkIndex       string `json:"linkIndex"`

	// History & Buffers
	Back       string `json:"back"`
	Forward    string `json:"forward"`
	Refresh    string `json:"refresh"` // reload current page
	NewBuffer  string `json:"newBuffer"`
	NextBuffer string `json:"nextBuffer"`
	PrevBuffer string `json:"prevBuffer"`
	BufferList string `json:"bufferList"`

	// Favourites
	AddFavourite   string `json:"addFavourite"`
	FavouritesList string `json:"favouritesList"`

	// RSS
	RSSFeeds       string `json:"rssFeeds"`       // Open RSS feed list
	RSSSubscribe   string `json:"rssSubscribe"`   // Subscribe to current page's feed
	RSSUnsubscribe string `json:"rssUnsubscribe"` // Unsubscribe from feed (x like close)
	RSSRefresh     string `json:"rssRefresh"`     // Refresh all feeds

	// Omnibox (unified URL + search)
	Omnibox string `json:"omnibox"`

	// Other
	Home               string `json:"home"`
	StructureInspector string `json:"structureInspector"`
	ToggleWideMode     string `json:"toggleWideMode"`
	ToggleTheme        string `json:"toggleTheme"`  // toggle light/dark theme
	ThemePicker        string `json:"themePicker"`  // open theme picker
	InputField         string `json:"inputField"`
	ReloadWithJs       string `json:"reloadWithJs"`
	GenerateRules      string `json:"generateRules"`
	EditConfig         string `json:"editConfig"`
	AISummary          string `json:"aiSummary"`
	EditorSandbox      string `json:"editorSandbox"`
	TranslatePage      string `json:"translatePage"` // translate page to English
}

// Config is the main configuration struct
type Config struct {
	Display     Display     `json:"display"`
	Search      Search      `json:"search"`
	Fetcher     Fetcher     `json:"fetcher"`
	Rendering   Rendering   `json:"rendering"`
	Session     Session     `json:"session"`
	Editor      Editor      `json:"editor"`
	Keybindings Keybindings `json:"keybindings"`
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Display: Display{
			WideMode:             false,
			FocusMode:            true,
			ShowScrollPercentage: true,
			ShowUrl:              true,
		},
		Search: Search{
			DefaultProvider: "duckduckgo",
		},
		Fetcher: Fetcher{
			UserAgent:      "Browse/1.0 (Terminal Browser)",
			TimeoutSeconds: 30,
			ChromePath:     "",
		},
		Rendering: Rendering{
			DefaultWidth:  80,
			LatexEnabled:  true,
			TablesEnabled: true,
		},
		Session: Session{
			RestoreSession: true,
		},
		Editor: Editor{
			Scheme: "emacs",
		},
		Keybindings: Keybindings{
			Quit:               "q",
			ScrollDown:         "j",
			ScrollUp:           "k",
			HalfPageDown:       "d",
			HalfPageUp:         "u",
			GoTop:              "gg",
			GoBottom:           "G",
			PrevParagraph:      "[",
			NextParagraph:      "]",
			PrevSection:        "{",
			NextSection:        "}",
			OpenUrl:       "o",
			OpenInBrowser: "go",
			Find:          "/",
			CopyUrl:       "y",
			EditInEditor:       "E",
			FollowLink:         "f",
			DefineWord:         "D",
			TableOfContents:    "t",
			SiteNavigation:     "n",
			LinkIndex:          "l",
			Back:               "\x0f", // Ctrl-o (vim jump list style)
			Forward:            "\t",   // Ctrl-i / Tab (vim jump list style)
			Refresh:            "gr",   // reload current page
			NewBuffer:          "T",
			NextBuffer:         "gt",
			PrevBuffer:         "gT",
			BufferList:         "`",
			AddFavourite:       "M",
			FavouritesList:     "'",
			RSSFeeds:           "F",
			RSSSubscribe:       "A",
			RSSUnsubscribe:     "x",
			RSSRefresh:         "gf",
			Omnibox:            "\x0c", // Ctrl-l (browser-style address bar)
			Home:               "H",
			StructureInspector: "s",
			ToggleWideMode:     "w",
			ToggleTheme:        "z",
			ThemePicker:        "P",
			InputField:         "i",
			ReloadWithJs:       "r",
			GenerateRules:      "R",
			EditConfig:         "C",
			AISummary:          "S",
			EditorSandbox:      "~",
			TranslatePage:      "X",
		},
	}
}

// configDir returns the configuration directory path.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "browse"), nil
}

// ConfigPath returns the path to the user's config file.
func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load loads configuration, layering user config on top of defaults.
// Returns the default config if no user config exists.
func Load() (*Config, error) {
	cfg := Default()

	configPath, err := ConfigPath()
	if err != nil {
		return cfg, nil // Return defaults if we can't determine path
	}

	// Check if user config exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return cfg, nil // Return defaults if no user config
	}

	// Load user config with TOML
	userCfg, err := loadFromTOML(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", configPath, err)
	}

	// Layer user config on top of defaults
	return merge(cfg, userCfg), nil
}

// loadFromTOML loads a TOML config file and returns the config.
func loadFromTOML(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config TOML: %w", err)
	}
	return &cfg, nil
}

// merge layers user config on top of defaults.
// Only non-zero values from user config override defaults.
func merge(defaults, user *Config) *Config {
	result := *defaults

	// Display
	if user.Display.WideMode {
		result.Display.WideMode = true
	}
	// Note: booleans are tricky - false could be intentional
	// For now, we only override if the section was specified at all
	// This is a limitation we can improve later

	// Search
	if user.Search.DefaultProvider != "" {
		result.Search.DefaultProvider = user.Search.DefaultProvider
	}

	// Fetcher
	if user.Fetcher.UserAgent != "" {
		result.Fetcher.UserAgent = user.Fetcher.UserAgent
	}
	if user.Fetcher.TimeoutSeconds != 0 {
		result.Fetcher.TimeoutSeconds = user.Fetcher.TimeoutSeconds
	}
	if user.Fetcher.ChromePath != "" {
		result.Fetcher.ChromePath = user.Fetcher.ChromePath
	}

	// Rendering
	if user.Rendering.DefaultWidth != 0 {
		result.Rendering.DefaultWidth = user.Rendering.DefaultWidth
	}

	// Editor
	if user.Editor.Scheme != "" {
		result.Editor.Scheme = user.Editor.Scheme
	}

	// Keybindings - override each if set
	mergeKeybinding(&result.Keybindings.Quit, user.Keybindings.Quit)
	mergeKeybinding(&result.Keybindings.ScrollDown, user.Keybindings.ScrollDown)
	mergeKeybinding(&result.Keybindings.ScrollUp, user.Keybindings.ScrollUp)
	mergeKeybinding(&result.Keybindings.HalfPageDown, user.Keybindings.HalfPageDown)
	mergeKeybinding(&result.Keybindings.HalfPageUp, user.Keybindings.HalfPageUp)
	mergeKeybinding(&result.Keybindings.GoTop, user.Keybindings.GoTop)
	mergeKeybinding(&result.Keybindings.GoBottom, user.Keybindings.GoBottom)
	mergeKeybinding(&result.Keybindings.PrevParagraph, user.Keybindings.PrevParagraph)
	mergeKeybinding(&result.Keybindings.NextParagraph, user.Keybindings.NextParagraph)
	mergeKeybinding(&result.Keybindings.PrevSection, user.Keybindings.PrevSection)
	mergeKeybinding(&result.Keybindings.NextSection, user.Keybindings.NextSection)
	mergeKeybinding(&result.Keybindings.OpenUrl, user.Keybindings.OpenUrl)
	mergeKeybinding(&result.Keybindings.OpenInBrowser, user.Keybindings.OpenInBrowser)
	mergeKeybinding(&result.Keybindings.Find, user.Keybindings.Find)
	mergeKeybinding(&result.Keybindings.CopyUrl, user.Keybindings.CopyUrl)
	mergeKeybinding(&result.Keybindings.EditInEditor, user.Keybindings.EditInEditor)
	mergeKeybinding(&result.Keybindings.FollowLink, user.Keybindings.FollowLink)
	mergeKeybinding(&result.Keybindings.TableOfContents, user.Keybindings.TableOfContents)
	mergeKeybinding(&result.Keybindings.SiteNavigation, user.Keybindings.SiteNavigation)
	mergeKeybinding(&result.Keybindings.LinkIndex, user.Keybindings.LinkIndex)
	mergeKeybinding(&result.Keybindings.Back, user.Keybindings.Back)
	mergeKeybinding(&result.Keybindings.Forward, user.Keybindings.Forward)
	mergeKeybinding(&result.Keybindings.Refresh, user.Keybindings.Refresh)
	mergeKeybinding(&result.Keybindings.NewBuffer, user.Keybindings.NewBuffer)
	mergeKeybinding(&result.Keybindings.NextBuffer, user.Keybindings.NextBuffer)
	mergeKeybinding(&result.Keybindings.PrevBuffer, user.Keybindings.PrevBuffer)
	mergeKeybinding(&result.Keybindings.BufferList, user.Keybindings.BufferList)
	mergeKeybinding(&result.Keybindings.AddFavourite, user.Keybindings.AddFavourite)
	mergeKeybinding(&result.Keybindings.FavouritesList, user.Keybindings.FavouritesList)
	mergeKeybinding(&result.Keybindings.RSSFeeds, user.Keybindings.RSSFeeds)
	mergeKeybinding(&result.Keybindings.RSSSubscribe, user.Keybindings.RSSSubscribe)
	mergeKeybinding(&result.Keybindings.RSSUnsubscribe, user.Keybindings.RSSUnsubscribe)
	mergeKeybinding(&result.Keybindings.RSSRefresh, user.Keybindings.RSSRefresh)
	mergeKeybinding(&result.Keybindings.Omnibox, user.Keybindings.Omnibox)
	mergeKeybinding(&result.Keybindings.Home, user.Keybindings.Home)
	mergeKeybinding(&result.Keybindings.StructureInspector, user.Keybindings.StructureInspector)
	mergeKeybinding(&result.Keybindings.ToggleWideMode, user.Keybindings.ToggleWideMode)
	mergeKeybinding(&result.Keybindings.ToggleTheme, user.Keybindings.ToggleTheme)
	mergeKeybinding(&result.Keybindings.ThemePicker, user.Keybindings.ThemePicker)
	mergeKeybinding(&result.Keybindings.InputField, user.Keybindings.InputField)
	mergeKeybinding(&result.Keybindings.ReloadWithJs, user.Keybindings.ReloadWithJs)
	mergeKeybinding(&result.Keybindings.GenerateRules, user.Keybindings.GenerateRules)
	mergeKeybinding(&result.Keybindings.EditConfig, user.Keybindings.EditConfig)
	mergeKeybinding(&result.Keybindings.AISummary, user.Keybindings.AISummary)
	mergeKeybinding(&result.Keybindings.EditorSandbox, user.Keybindings.EditorSandbox)
	mergeKeybinding(&result.Keybindings.TranslatePage, user.Keybindings.TranslatePage)

	return &result
}

func mergeKeybinding(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}

// DefaultTOML returns the default configuration as a TOML string.
// Used for --init-config to generate a user config file.
func DefaultTOML() string {
	return `# Browse configuration
# Save to ~/.config/browse/config.toml and customize
# Only include settings you want to change from defaults

# Display settings
[display]
wideMode = false              # Start in wide mode (full terminal width)
focusMode = true              # Dim non-focused paragraphs when using paragraph navigation
showScrollPercentage = true   # Show scroll percentage in status bar
showUrl = true                # Show URL in status bar

# Search provider settings
[search]
defaultProvider = "duckduckgo"

# HTTP fetching settings
[fetcher]
userAgent = "Browse/1.0 (Terminal Browser)"
timeoutSeconds = 30
chromePath = ""               # Path to Chrome/Chromium for JS rendering (empty = auto-detect)

# Rendering settings
[rendering]
defaultWidth = 80             # Default width when piping output (not in terminal)
latexEnabled = true           # Enable LaTeX math rendering
tablesEnabled = true          # Enable table rendering

# Session settings
[session]
restoreSession = true         # Restore previous session on startup

# Editor settings
[editor]
scheme = "emacs"              # "emacs" or "vim"

# Keybindings - customize your keys here!
[keybindings]
# Navigation
quit = "q"
scrollDown = "j"
scrollUp = "k"
halfPageDown = "d"
halfPageUp = "u"
goTop = "gg"
goBottom = "G"
prevParagraph = "["
nextParagraph = "]"
prevSection = "{"
nextSection = "}"

# Actions
openUrl = "o"
openInBrowser = "go"          # Open current page in default browser
find = "/"                    # Find in page
copyUrl = "y"
editInEditor = "E"
followLink = "f"
defineWord = "D"              # Look up word definition

# Overlays
tableOfContents = "t"
siteNavigation = "n"
linkIndex = "l"

# History & Buffers
back = "\u000f"               # Ctrl-o (vim jump list style)
forward = "\t"                # Ctrl-i / Tab (vim jump list style)
refresh = "gr"                # Reload current page
newBuffer = "T"
nextBuffer = "gt"
prevBuffer = "gT"
bufferList = "` + "`" + `"

# Favourites
addFavourite = "M"
favouritesList = "'"

# RSS
rssFeeds = "F"                # Open RSS feed list
rssSubscribe = "A"            # Subscribe to current page's feed
rssUnsubscribe = "x"          # Unsubscribe from feed
rssRefresh = "gf"             # Refresh all feeds

# Other
omnibox = "\u000c"            # Ctrl-l (browser-style address bar)
home = "H"
structureInspector = "s"
toggleWideMode = "w"
toggleTheme = "z"             # Toggle light/dark theme
themePicker = "P"             # Open theme picker
inputField = "i"
reloadWithJs = "r"
generateRules = "R"
editConfig = "C"
aiSummary = "S"
editorSandbox = "~"
translatePage = "X"           # Translate page to English
`
}

// KeyMatcher helps match input against configured keybindings.
type KeyMatcher struct {
	pending string // Accumulated prefix (e.g., "g" waiting for second key)
}

// NewKeyMatcher creates a new key matcher.
func NewKeyMatcher() *KeyMatcher {
	return &KeyMatcher{}
}

// Match checks if the input byte matches the given binding.
// Returns (matched, consumed) where consumed means the pending state was used.
func (km *KeyMatcher) Match(input byte, binding string) (matched bool, consumed bool) {
	if len(binding) == 0 {
		return false, false
	}

	// Single character binding
	if len(binding) == 1 {
		if km.pending == "" && input == binding[0] {
			return true, false
		}
		return false, false
	}

	// Multi-character binding (e.g., "gg", "gt", "gT")
	if len(binding) == 2 {
		// Check if we're waiting for second char
		if km.pending != "" && km.pending[0] == binding[0] && input == binding[1] {
			km.pending = ""
			return true, true
		}
		// Check if this is the first char of a sequence
		if km.pending == "" && input == binding[0] {
			// Don't match yet - might be start of sequence
			return false, false
		}
	}

	return false, false
}

// SetPending sets the pending prefix (used when 'g' is pressed).
func (km *KeyMatcher) SetPending(prefix string) {
	km.pending = prefix
}

// ClearPending clears any pending prefix.
func (km *KeyMatcher) ClearPending() {
	km.pending = ""
}

// Pending returns the current pending prefix.
func (km *KeyMatcher) Pending() string {
	return km.pending
}

// IsPending returns true if there's a pending prefix.
func (km *KeyMatcher) IsPending() bool {
	return km.pending != ""
}

// MatchSingle is a simple helper for single-char bindings.
func MatchSingle(input byte, binding string) bool {
	return len(binding) == 1 && input == binding[0]
}

// MatchWithPrefix checks if input completes a two-char binding given a prefix.
func MatchWithPrefix(prefix string, input byte, binding string) bool {
	if len(binding) != 2 || len(prefix) != 1 {
		return false
	}
	return prefix[0] == binding[0] && input == binding[1]
}

// StartsBinding returns true if input is the first char of a multi-char binding.
func StartsBinding(input byte, binding string) bool {
	return len(binding) > 1 && input == binding[0]
}

// FormatError formats a Pkl evaluation error for user display.
func FormatError(err error) string {
	// Pkl errors are already pretty well formatted
	// We can enhance this later with colors/formatting
	return fmt.Sprintf("Configuration error:\n\n%s", err.Error())
}
