// Package rules provides AI-generated extraction rules for websites.
// Rules tell the HTML parser how to extract and display content optimally
// for each domain.
package rules

import "time"

// Rule defines extraction and display rules for a domain.
// Supports both legacy single-template mode and new multi-page-type mode.
type Rule struct {
	// Metadata
	Domain      string    `json:"domain" yaml:"domain"`
	Version     int       `json:"version" yaml:"version"`
	GeneratedBy string    `json:"generated_by,omitempty" yaml:"generated_by,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitempty" yaml:"generated_at,omitempty"`
	Verified    bool      `json:"verified,omitempty" yaml:"verified,omitempty"`

	// --- New: Page Types (v2) ---
	// Multiple page types per domain, each with its own selectors and template
	PageTypes map[string]*PageType `json:"page_types,omitempty" yaml:"page_types,omitempty"`

	// --- Legacy: Single page mode (v1) ---
	// Content extraction
	Content ContentRules `json:"content" yaml:"content"`

	// Display hints
	Layout LayoutHints `json:"layout,omitempty" yaml:"layout,omitempty"`

	// Special handling flags
	Quirks Quirks `json:"quirks,omitempty" yaml:"quirks,omitempty"`
}

// PageType defines extraction and display for a specific type of page.
// e.g., "home", "article", "search", "listing"
type PageType struct {
	// How to identify this page type
	Matcher PageMatcher `json:"matcher" yaml:"matcher"`

	// Named selectors for extracting content
	// e.g., {"headline": "h1.title", "items": ".story-item", "body": "article p"}
	Selectors map[string]string `json:"selectors" yaml:"selectors"`

	// Go template for rendering the extracted content
	Template string `json:"template" yaml:"template"`

	// Optional metadata
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PageMatcher determines if a page matches this page type.
type PageMatcher struct {
	// URL pattern (regex): "^/news/article-"
	URLPattern string `json:"url_pattern,omitempty" yaml:"url_pattern,omitempty"`

	// URL contains substring: "?q="
	URLContains string `json:"url_contains,omitempty" yaml:"url_contains,omitempty"`

	// CSS selector that must exist on page: "article.full-story"
	HasElement string `json:"has_element,omitempty" yaml:"has_element,omitempty"`

	// CSS selector that must NOT exist: ".paywall-overlay"
	NotElement string `json:"not_element,omitempty" yaml:"not_element,omitempty"`

	// Default page type if no other matches
	IsDefault bool `json:"is_default,omitempty" yaml:"is_default,omitempty"`
}

// HasPageTypes returns true if this rule uses the new page type system.
func (r *Rule) HasPageTypes() bool {
	return len(r.PageTypes) > 0
}

// GetPageType returns the best matching page type for a URL and HTML content.
func (r *Rule) GetPageType(url, html string) *PageType {
	if !r.HasPageTypes() {
		return nil
	}

	var defaultType *PageType

	for _, pt := range r.PageTypes {
		if pt.Matcher.IsDefault {
			defaultType = pt
			continue
		}

		if pt.Matcher.Matches(url, html) {
			return pt
		}
	}

	return defaultType
}

// Matches checks if the matcher matches the given URL and HTML.
func (m *PageMatcher) Matches(url, html string) bool {
	// URL pattern match (regex)
	if m.URLPattern != "" {
		// Import regexp at runtime to avoid global init
		matched, _ := matchRegex(m.URLPattern, url)
		if !matched {
			return false
		}
	}

	// URL contains match
	if m.URLContains != "" {
		if !containsString(url, m.URLContains) {
			return false
		}
	}

	// Element existence check
	if m.HasElement != "" {
		if !htmlContainsSelector(html, m.HasElement) {
			return false
		}
	}

	// Element absence check
	if m.NotElement != "" {
		if htmlContainsSelector(html, m.NotElement) {
			return false
		}
	}

	// If we have at least one condition and all passed, it's a match
	return m.URLPattern != "" || m.URLContains != "" || m.HasElement != "" || m.NotElement != ""
}

// ContentRules define how to extract content from the page.
type ContentRules struct {
	// Root selector for main content area (CSS selector)
	Root string `json:"root,omitempty" yaml:"root,omitempty"`

	// Article selector for individual articles/items
	Articles string `json:"articles,omitempty" yaml:"articles,omitempty"`

	// Title extraction
	Title         string `json:"title,omitempty" yaml:"title,omitempty"`
	TitleInParent bool   `json:"title_in_parent,omitempty" yaml:"title_in_parent,omitempty"`

	// Metadata extraction (author, date, etc.)
	Metadata string `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	// Link extraction for list-style pages
	Links string `json:"links,omitempty" yaml:"links,omitempty"`

	// Elements to skip/ignore
	Skip []string `json:"skip,omitempty" yaml:"skip,omitempty"`

	// Elements that indicate navigation (to be extracted separately)
	Navigation []string `json:"navigation,omitempty" yaml:"navigation,omitempty"`
}

// LayoutHints suggest how to display the content.
type LayoutHints struct {
	// Type of layout: article, list, table, newspaper, forum
	Type string `json:"type,omitempty" yaml:"type,omitempty"`

	// Whether items should have separators between them
	ItemSeparator string `json:"item_separator,omitempty" yaml:"item_separator,omitempty"`

	// Whether to show metadata inline or below
	MetadataPosition string `json:"metadata_position,omitempty" yaml:"metadata_position,omitempty"`

	// Max width for content (0 = use full width)
	MaxWidth int `json:"max_width,omitempty" yaml:"max_width,omitempty"`

	// Indentation for nested content
	IndentNested bool `json:"indent_nested,omitempty" yaml:"indent_nested,omitempty"`
}

// Quirks handle site-specific oddities.
type Quirks struct {
	// Deduplicate repeated text in headings (BBC style)
	DedupeHeadings bool `json:"dedupe_headings,omitempty" yaml:"dedupe_headings,omitempty"`

	// Site uses table-based layout (HN style)
	TableLayout bool `json:"table_layout,omitempty" yaml:"table_layout,omitempty"`

	// Table row class that indicates content items
	TableRowClass string `json:"table_row_class,omitempty" yaml:"table_row_class,omitempty"`

	// Skip rows matching these classes
	SkipRowClasses []string `json:"skip_row_classes,omitempty" yaml:"skip_row_classes,omitempty"`

	// Content is lazy-loaded and needs JS
	NeedsJS bool `json:"needs_js,omitempty" yaml:"needs_js,omitempty"`

	// Site has aggressive bot detection
	BotDetection bool `json:"bot_detection,omitempty" yaml:"bot_detection,omitempty"`
}

// LayoutType constants
const (
	LayoutArticle   = "article"   // Long-form single article
	LayoutList      = "list"      // List of items/links (HN, Reddit)
	LayoutNewspaper = "newspaper" // Mixed headlines and summaries (BBC, Guardian)
	LayoutForum     = "forum"     // Discussion threads
	LayoutTable     = "table"     // Tabular data
)

// SeparatorType constants
const (
	SeparatorNone    = ""
	SeparatorLine    = "line"    // ───
	SeparatorBlank   = "blank"   // Empty line
	SeparatorDot     = "dot"     // • • •
	SeparatorNewline = "newline" // Just a newline
)

// MetadataPosition constants
const (
	MetadataInline = "inline" // Same line as title: "Title (5 points, 3 comments)"
	MetadataBelow  = "below"  // Line below title
	MetadataHidden = "hidden" // Don't show
)

// PageTypeNames for common page types
const (
	PageTypeHome    = "home"
	PageTypeArticle = "article"
	PageTypeSearch  = "search"
	PageTypeListing = "listing"
	PageTypeDefault = "default"
)
