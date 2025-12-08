// Package omnibox provides unified URL + search input parsing.
package omnibox

import (
	"net/url"
	"strings"
)

// Result represents the parsed omnibox input.
type Result struct {
	URL             string // The target URL to navigate to (empty for internal search)
	Query           string // Search query (for internal search)
	IsSearch        bool   // Whether this is a search (vs direct navigation)
	UseInternal     bool   // Use internal search provider (like / search)
	Provider        string // The search provider used (if IsSearch)
	IsAISummary     bool   // Whether this is an AI summary request
	AIPrompt        string // Optional custom prompt for AI (empty = default summary)
	IsDictLookup    bool   // Whether this is a dictionary lookup
	DictWord        string // Word to look up in dictionary
}

// Prefix represents a search prefix configuration.
type Prefix struct {
	Names    []string // Prefix names (e.g., "wp", "wiki", "wikipedia")
	URLFmt   string   // URL format with %s for query (empty if Internal=true)
	Display  string   // Display name (e.g., "Wikipedia")
	Internal bool     // Use internal search provider instead of URL
}

// DefaultPrefixes returns the built-in search prefixes.
func DefaultPrefixes() []Prefix {
	return []Prefix{
		{
			Names:    []string{"ddg", "duckduckgo"},
			Display:  "DuckDuckGo",
			Internal: true,
		},
		{
			Names:    []string{"wp", "wiki", "wikipedia"},
			Display:  "Wikipedia",
			Internal: true,
		},
		{
			Names:    []string{"gh", "github"},
			Display:  "GitHub",
			Internal: true,
		},
		{
			Names:    []string{"hn", "hackernews"},
			Display:  "Hacker News",
			Internal: true,
		},
		{
			Names:    []string{"go", "pkg"},
			Display:  "pkg.go.dev",
			Internal: true,
		},
		{
			Names:    []string{"arch", "archwiki"},
			Display:  "Arch Wiki",
			Internal: true,
		},
		{
			Names:    []string{"mdn"},
			Display:  "MDN Web Docs",
			Internal: true,
		},
		{
			Names:    []string{"man", "manpages"},
			Display:  "Man Pages",
			Internal: true,
		},
		{
			Names:    []string{"dict", "define", "d"},
			Display:  "Dictionary",
			Internal: true,
		},
		{
			Names:    []string{"ai", "sum", "summary"},
			Display:  "AI Summary",
			Internal: true,
		},
	}
}

// Parser handles omnibox input parsing.
type Parser struct {
	prefixes       []Prefix
	defaultSearch  string // URL format for default search
}

// NewParser creates a new omnibox parser with default configuration.
func NewParser() *Parser {
	return &Parser{
		prefixes:      DefaultPrefixes(),
		defaultSearch: "https://duckduckgo.com/?q=%s",
	}
}

// SetDefaultSearch sets the default search URL format.
func (p *Parser) SetDefaultSearch(urlFmt string) {
	p.defaultSearch = urlFmt
}

// AddPrefix adds a custom search prefix.
func (p *Parser) AddPrefix(prefix Prefix) {
	p.prefixes = append(p.prefixes, prefix)
}

// Parse parses omnibox input and returns the result.
func (p *Parser) Parse(input string) Result {
	input = strings.TrimSpace(input)
	if input == "" {
		return Result{}
	}

	// Check for URL schemes first
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return Result{URL: input, IsSearch: false}
	}

	// Check for AI summary prefixes
	inputLower := strings.ToLower(input)
	if inputLower == "sum" || inputLower == "ai" || inputLower == "summary" {
		return Result{IsAISummary: true, Provider: "AI Summary"}
	}
	// AI with custom prompt: "ai <question>" or "sum <question>"
	for _, aiPrefix := range []string{"ai ", "sum ", "summary "} {
		if strings.HasPrefix(inputLower, aiPrefix) {
			prompt := strings.TrimSpace(input[len(aiPrefix):])
			return Result{IsAISummary: true, AIPrompt: prompt, Provider: "AI Summary"}
		}
	}

	// Check for dictionary lookup prefixes
	for _, dictPrefix := range []string{"dict ", "define ", "d "} {
		if strings.HasPrefix(inputLower, dictPrefix) {
			word := strings.TrimSpace(input[len(dictPrefix):])
			if word != "" {
				return Result{IsDictLookup: true, DictWord: word, Provider: "Dictionary"}
			}
		}
	}

	// Check for search prefixes (e.g., "wp cats" or "wiki hello world")
	if idx := strings.Index(input, " "); idx > 0 {
		prefix := strings.ToLower(input[:idx])
		query := strings.TrimSpace(input[idx+1:])

		if query != "" {
			for _, pfx := range p.prefixes {
				for _, name := range pfx.Names {
					if prefix == name {
						if pfx.Internal {
							// Use internal search provider
							return Result{
								Query:       query,
								IsSearch:    true,
								UseInternal: true,
								Provider:    pfx.Display,
							}
						}
						// External URL-based search
						return Result{
							URL:      strings.Replace(pfx.URLFmt, "%s", url.QueryEscape(query), 1),
							IsSearch: true,
							Provider: pfx.Display,
						}
					}
				}
			}
		}
	}

	// Check if it looks like a URL (domain.tld pattern)
	if looksLikeURL(input) {
		return Result{URL: "https://" + input, IsSearch: false}
	}

	// Default: use internal search provider (same as / search)
	return Result{
		Query:       input,
		IsSearch:    true,
		UseInternal: true,
		Provider:    "Search",
	}
}

// looksLikeURL checks if input looks like a URL (has domain.tld pattern).
func looksLikeURL(input string) bool {
	// No spaces allowed in URLs
	if strings.Contains(input, " ") {
		return false
	}

	// Check for common TLDs
	tlds := []string{
		".com", ".org", ".net", ".io", ".dev", ".co", ".me", ".app",
		".edu", ".gov", ".uk", ".de", ".fr", ".jp", ".au", ".ca",
		".info", ".biz", ".tv", ".cc", ".xyz", ".tech", ".ai",
	}
	lower := strings.ToLower(input)
	for _, tld := range tlds {
		if strings.Contains(lower, tld) {
			return true
		}
	}

	// Check for localhost or IP addresses
	if strings.HasPrefix(lower, "localhost") || strings.HasPrefix(lower, "127.") {
		return true
	}

	return false
}

// Prefixes returns the list of available prefixes (for help display).
func (p *Parser) Prefixes() []Prefix {
	return p.prefixes
}
