package search

import (
	"fmt"
	"strings"
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
		// Result number and title as heading
		sb.WriteString(fmt.Sprintf("<h2><a href=\"%s\">%d. %s</a></h2>\n",
			escapeHTML(result.URL),
			i+1,
			escapeHTML(result.Title)))

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
