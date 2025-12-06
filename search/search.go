// Package search provides web search functionality with pluggable providers.
package search

import "time"

// Result represents a single search result.
type Result struct {
	Title   string // Page title
	URL     string // Full URL
	Snippet string // Description/snippet text
	Domain  string // Extracted domain for display
}

// Results represents a complete search response with metadata.
type Results struct {
	Query      string    // The search query
	Provider   string    // Provider that executed the search
	Results    []Result  // The actual results
	TotalFound int       // Total results found (may be > len(Results))
	SearchedAt time.Time // When the search was performed
}

// Provider defines the interface for search providers.
type Provider interface {
	// Search performs a web search and returns results.
	Search(query string) (*Results, error)

	// Name returns the provider's display name.
	Name() string
}

// DefaultProvider returns the recommended search provider.
func DefaultProvider() Provider {
	return NewDuckDuckGo()
}

// ProviderByName returns a provider by name.
// Falls back to DuckDuckGo if the name is unrecognized.
func ProviderByName(name string) Provider {
	switch name {
	case "duckduckgo", "ddg", "DuckDuckGo":
		return NewDuckDuckGo()
	case "wikipedia", "wiki", "wp", "Wikipedia":
		return NewWikipedia()
	case "github", "gh", "GitHub":
		return NewGitHub()
	case "hackernews", "hn", "Hacker News":
		return NewHackerNews()
	case "pkggodev", "go", "pkg.go.dev":
		return NewPkgGoDev()
	case "archwiki", "arch", "Arch Wiki":
		return NewArchWiki()
	case "mdn", "MDN Web Docs":
		return NewMDN()
	case "manpages", "man", "Man Pages":
		return NewManPages()
	case "wiktionary", "dict", "Wiktionary":
		return NewWiktionary()
	default:
		// Fall back to DuckDuckGo for unknown providers
		return NewDuckDuckGo()
	}
}
