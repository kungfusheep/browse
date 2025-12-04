// Package config provides configuration loading for Browse using Pkl.
package config

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apple/pkl-go/pkl"
)

//go:embed Config.pkl
var defaultSchema string

// Display settings
type Display struct {
	WideMode             bool `json:"wideMode"`
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
	OpenUrl      string `json:"openUrl"`
	Search       string `json:"search"`
	CopyUrl      string `json:"copyUrl"`
	EditInEditor string `json:"editInEditor"`
	FollowLink   string `json:"followLink"`

	// Overlays
	TableOfContents string `json:"tableOfContents"`
	SiteNavigation  string `json:"siteNavigation"`
	LinkIndex       string `json:"linkIndex"`

	// History & Buffers
	Back       string `json:"back"`
	Forward    string `json:"forward"`
	NewBuffer  string `json:"newBuffer"`
	NextBuffer string `json:"nextBuffer"`
	PrevBuffer string `json:"prevBuffer"`
	BufferList string `json:"bufferList"`

	// Favourites
	AddFavourite   string `json:"addFavourite"`
	FavouritesList string `json:"favouritesList"`

	// Other
	Home               string `json:"home"`
	StructureInspector string `json:"structureInspector"`
	ToggleWideMode     string `json:"toggleWideMode"`
	InputField         string `json:"inputField"`
	ReloadWithJs       string `json:"reloadWithJs"`
	GenerateRules      string `json:"generateRules"`
	EditConfig         string `json:"editConfig"`
}

// Config is the main configuration struct
type Config struct {
	Display     Display     `json:"display"`
	Search      Search      `json:"search"`
	Fetcher     Fetcher     `json:"fetcher"`
	Rendering   Rendering   `json:"rendering"`
	Keybindings Keybindings `json:"keybindings"`
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Display: Display{
			WideMode:             false,
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
			OpenUrl:            "o",
			Search:             "/",
			CopyUrl:            "y",
			EditInEditor:       "E",
			FollowLink:         "f",
			TableOfContents:    "t",
			SiteNavigation:     "n",
			LinkIndex:          "l",
			Back:               "b",
			Forward:            "B",
			NewBuffer:          "T",
			NextBuffer:         "gt",
			PrevBuffer:         "gT",
			BufferList:         "`",
			AddFavourite:       "M",
			FavouritesList:     "'",
			Home:               "H",
			StructureInspector: "s",
			ToggleWideMode:     "w",
			InputField:         "i",
			ReloadWithJs:       "r",
			GenerateRules:      "R",
			EditConfig:         "C",
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
	return filepath.Join(dir, "config.pkl"), nil
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

	// Load and evaluate user config with Pkl
	userCfg, err := loadFromPkl(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", configPath, err)
	}

	// Layer user config on top of defaults
	return merge(cfg, userCfg), nil
}

// loadFromPkl evaluates a Pkl config file and returns the config.
func loadFromPkl(path string) (*Config, error) {
	evaluator, err := pkl.NewEvaluator(context.Background(), pkl.PreconfiguredOptions)
	if err != nil {
		return nil, fmt.Errorf("creating pkl evaluator: %w", err)
	}
	defer evaluator.Close()

	// Evaluate to JSON format - works with untyped Pkl objects
	jsonBytes, err := evaluator.EvaluateExpressionRaw(context.Background(), pkl.FileSource(path), "new JsonRenderer {}.renderValue(this)")
	if err != nil {
		return nil, err
	}

	// Pkl may include prefix bytes before the JSON - find the opening brace
	jsonStr := string(jsonBytes)
	start := 0
	for i, c := range jsonStr {
		if c == '{' {
			start = i
			break
		}
	}
	jsonStr = jsonStr[start:]

	var cfg Config
	if err := json.Unmarshal([]byte(jsonStr), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config JSON: %w", err)
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
	mergeKeybinding(&result.Keybindings.Search, user.Keybindings.Search)
	mergeKeybinding(&result.Keybindings.CopyUrl, user.Keybindings.CopyUrl)
	mergeKeybinding(&result.Keybindings.EditInEditor, user.Keybindings.EditInEditor)
	mergeKeybinding(&result.Keybindings.FollowLink, user.Keybindings.FollowLink)
	mergeKeybinding(&result.Keybindings.TableOfContents, user.Keybindings.TableOfContents)
	mergeKeybinding(&result.Keybindings.SiteNavigation, user.Keybindings.SiteNavigation)
	mergeKeybinding(&result.Keybindings.LinkIndex, user.Keybindings.LinkIndex)
	mergeKeybinding(&result.Keybindings.Back, user.Keybindings.Back)
	mergeKeybinding(&result.Keybindings.Forward, user.Keybindings.Forward)
	mergeKeybinding(&result.Keybindings.NewBuffer, user.Keybindings.NewBuffer)
	mergeKeybinding(&result.Keybindings.NextBuffer, user.Keybindings.NextBuffer)
	mergeKeybinding(&result.Keybindings.PrevBuffer, user.Keybindings.PrevBuffer)
	mergeKeybinding(&result.Keybindings.BufferList, user.Keybindings.BufferList)
	mergeKeybinding(&result.Keybindings.AddFavourite, user.Keybindings.AddFavourite)
	mergeKeybinding(&result.Keybindings.FavouritesList, user.Keybindings.FavouritesList)
	mergeKeybinding(&result.Keybindings.Home, user.Keybindings.Home)
	mergeKeybinding(&result.Keybindings.StructureInspector, user.Keybindings.StructureInspector)
	mergeKeybinding(&result.Keybindings.ToggleWideMode, user.Keybindings.ToggleWideMode)
	mergeKeybinding(&result.Keybindings.InputField, user.Keybindings.InputField)
	mergeKeybinding(&result.Keybindings.ReloadWithJs, user.Keybindings.ReloadWithJs)
	mergeKeybinding(&result.Keybindings.GenerateRules, user.Keybindings.GenerateRules)
	mergeKeybinding(&result.Keybindings.EditConfig, user.Keybindings.EditConfig)

	return &result
}

func mergeKeybinding(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}

// DefaultPkl returns the default configuration as a Pkl string.
// Used for --init-config to generate a user config file.
func DefaultPkl() string {
	return `// Browse configuration
// Save to ~/.config/browse/config.pkl and customize
// Only include settings you want to change from defaults

// Display settings
display = new {
  // Start in wide mode (full terminal width)
  wideMode = false

  // Show scroll percentage in status bar
  showScrollPercentage = true

  // Show URL in status bar
  showUrl = true
}

// Search provider settings
search = new {
  // Default search provider
  defaultProvider = "duckduckgo"
}

// HTTP fetching settings
fetcher = new {
  // User agent string for requests
  userAgent = "Browse/1.0 (Terminal Browser)"

  // Request timeout in seconds
  timeoutSeconds = 30

  // Path to Chrome/Chromium for JS rendering (empty = auto-detect)
  chromePath = ""
}

// Rendering settings
rendering = new {
  // Default width when piping output (not in terminal)
  defaultWidth = 80

  // Enable LaTeX math rendering
  latexEnabled = true

  // Enable table rendering
  tablesEnabled = true
}

// Keybindings - customize your keys here!
keybindings = new {
  // Navigation
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

  // Actions
  openUrl = "o"
  search = "/"
  copyUrl = "y"
  editInEditor = "E"
  followLink = "f"

  // Overlays
  tableOfContents = "t"
  siteNavigation = "n"
  linkIndex = "l"

  // History & Buffers
  back = "b"
  forward = "B"
  newBuffer = "T"
  nextBuffer = "gt"
  prevBuffer = "gT"
  bufferList = "` + "`" + `"

  // Favourites
  addFavourite = "M"
  favouritesList = "'"

  // Other
  home = "H"
  structureInspector = "s"
  toggleWideMode = "w"
  inputField = "i"
  reloadWithJs = "r"
  generateRules = "R"
  editConfig = "C"
}
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
