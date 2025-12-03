package rules

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ApplyResult contains the extracted content after applying rules.
type ApplyResult struct {
	// Domain this content was extracted from
	Domain string

	// Items are the main content items (articles, list items, etc.)
	Items []ContentItem

	// LayoutType from the rule
	LayoutType string

	// ItemSeparator to use between items
	ItemSeparator string

	// MetadataPosition where to show metadata
	MetadataPosition string
}

// ContentItem represents a single piece of content (article, link, etc.)
type ContentItem struct {
	Title    string
	Href     string
	Metadata string
}

// Apply uses a rule to extract content from HTML.
// Returns nil if the rule doesn't match well or extraction fails.
func Apply(rule *Rule, htmlContent string) *ApplyResult {
	if rule == nil {
		return nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	result := &ApplyResult{
		Domain:           rule.Domain,
		LayoutType:       rule.Layout.Type,
		ItemSeparator:    rule.Layout.ItemSeparator,
		MetadataPosition: rule.Layout.MetadataPosition,
	}

	// Find the content root if specified
	root := doc.Selection
	if rule.Content.Root != "" {
		found := doc.Find(rule.Content.Root)
		if found.Length() > 0 {
			root = found
		}
	}

	// If we have an articles selector, extract each article
	if rule.Content.Articles != "" {
		root.Find(rule.Content.Articles).Each(func(i int, s *goquery.Selection) {
			item := extractItem(s, rule)
			if item.Title != "" {
				result.Items = append(result.Items, item)
			}
		})
	}

	// If no articles found but we have a title selector, try direct extraction
	if len(result.Items) == 0 && rule.Content.Title != "" {
		root.Find(rule.Content.Title).Each(func(i int, s *goquery.Selection) {
			item := ContentItem{
				Title: strings.TrimSpace(s.Text()),
			}
			if href, exists := s.Attr("href"); exists {
				item.Href = href
			}
			if item.Title != "" {
				result.Items = append(result.Items, item)
			}
		})
	}

	// Only return result if we found something useful
	if len(result.Items) == 0 {
		return nil
	}

	return result
}

func extractItem(s *goquery.Selection, rule *Rule) ContentItem {
	var item ContentItem

	// Extract title
	if rule.Content.Title != "" {
		titleSel := s.Find(rule.Content.Title)
		if titleSel.Length() > 0 {
			first := titleSel.First()
			item.Title = strings.TrimSpace(first.Text())

			// Try to get href from the element itself
			if href, exists := first.Attr("href"); exists {
				item.Href = href
			} else {
				// The title selector might point to a container - look for <a> inside
				if link := first.Find("a").First(); link.Length() > 0 {
					if href, exists := link.Attr("href"); exists {
						item.Href = href
					}
				}
				// Or the title might be inside an <a> - check parent
				if item.Href == "" {
					if parent := first.Parent(); parent.Is("a") {
						item.Href, _ = parent.Attr("href")
					}
				}
			}
		}
	}

	// If no title selector or not found, try the element itself
	if item.Title == "" {
		// Check if the element itself is a link
		if s.Is("a") {
			item.Title = strings.TrimSpace(s.Text())
			item.Href, _ = s.Attr("href")
		} else {
			// Try to find any link
			link := s.Find("a").First()
			if link.Length() > 0 {
				item.Title = strings.TrimSpace(link.Text())
				item.Href, _ = link.Attr("href")
			}
		}
	}

	// Final fallback: if we have a title but no href, search harder for a link
	if item.Title != "" && item.Href == "" {
		// Look for any link in the article element
		if link := s.Find("a").First(); link.Length() > 0 {
			if href, exists := link.Attr("href"); exists && href != "" && href != "#" {
				item.Href = href
			}
		}
	}

	// Extract metadata if selector provided
	if rule.Content.Metadata != "" {
		// For HN-style layouts, metadata is in the next sibling row
		if rule.Quirks.TableLayout {
			// Try next sibling
			next := s.Next()
			if next.Length() > 0 {
				metaSel := next.Find(rule.Content.Metadata)
				if metaSel.Length() == 0 {
					// Try the whole next row
					metaSel = next
				}
				item.Metadata = cleanMetadata(metaSel.Text())
			}
		} else {
			metaSel := s.Find(rule.Content.Metadata)
			if metaSel.Length() > 0 {
				item.Metadata = cleanMetadata(metaSel.Text())
			}
		}
	}

	return item
}

func cleanMetadata(s string) string {
	// Clean up whitespace
	s = strings.TrimSpace(s)
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	// Collapse newlines
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
