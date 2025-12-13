package search

import (
	"fmt"
	"strings"

	"browse/sites"
)

// ToHTML converts search results into a beautifully formatted HTML document.
func (r *Results) ToHTML() string {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("<h1>Search: %s</h1>\n", escapeHTML(r.Query)))
	sb.WriteString(fmt.Sprintf("<p>%d results from %s</p>\n\n", len(r.Results), r.Provider))

	if len(r.Results) == 0 {
		sb.WriteString("<p>No results found. Try different search terms.</p>\n")
		return sb.String()
	}

	// Results as a clean list
	for i, result := range r.Results {
		// Quality flair based on domain score
		flair := qualityFlair(result.Domain)

		// Result number and title as heading (with flair)
		sb.WriteString(fmt.Sprintf("<h2><a href=\"%s\">%d. %s%s</a></h2>\n",
			escapeHTML(result.URL),
			i+1,
			escapeHTML(result.Title),
			flair))

		// Display domain
		sb.WriteString(fmt.Sprintf("<p><strong>%s</strong></p>\n", escapeHTML(result.Domain)))

		// Snippet
		if result.Snippet != "" {
			sb.WriteString(fmt.Sprintf("<p>%s</p>\n", escapeHTML(result.Snippet)))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// escapeHTML escapes special HTML characters.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// qualityFlair returns a flair suffix based on text-browser compatibility.
// ★ for excellent compatibility (95+), ✓ for good compatibility (80-94).
func qualityFlair(domain string) string {
	domain = strings.TrimPrefix(domain, "www.")
	if info, known := sites.Lookup(domain); known {
		if info.Score >= 95 {
			return " ★"
		} else if info.Score >= 80 {
			return " ✓"
		}
	}
	return ""
}
