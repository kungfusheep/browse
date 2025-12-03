package html

import (
	"fmt"
	"strings"

	"browse/rules"
)

// FromRules creates a Document from rule-extracted content.
// This provides a clean, structured document based on AI-generated rules,
// formatted to match our terminal browser aesthetic.
func FromRules(result *rules.ApplyResult) *Document {
	if result == nil || len(result.Items) == 0 {
		return nil
	}

	doc := &Document{
		Content: &Node{Type: NodeDocument},
	}

	// Add document header with domain as title
	addDocumentHeader(doc, result)

	// Always use flat list layout for rules-based content
	// This avoids the document renderer's section numbering (1.1, 1.1.1, etc)
	// which looks great for documents but terrible for extracted content
	buildListLayout(doc, result)

	return doc
}

// addDocumentHeader creates a proper header with title and metadata
// Uses paragraphs instead of headings to avoid document section numbering
func addDocumentHeader(doc *Document, result *rules.ApplyResult) {
	// Create title as bold paragraph (not H1 to avoid "1." prefix)
	title := formatDomainAsTitle(result.Domain)
	titlePara := &Node{Type: NodeParagraph}
	titlePara.Children = append(titlePara.Children, &Node{
		Type: NodeStrong,
		Children: []*Node{{
			Type: NodeText,
			Text: strings.ToUpper(title),
		}},
	})
	doc.Content.Children = append(doc.Content.Children, titlePara)

	// Add a subtitle with item count
	subtitle := fmt.Sprintf("%d items", len(result.Items))
	subtitlePara := &Node{Type: NodeParagraph}
	subtitlePara.Children = append(subtitlePara.Children, &Node{
		Type: NodeEmphasis,
		Children: []*Node{{
			Type: NodeText,
			Text: subtitle,
		}},
	})
	doc.Content.Children = append(doc.Content.Children, subtitlePara)
}

// formatDomainAsTitle converts a domain to a nice title
// e.g., "news.ycombinator.com" -> "Hacker News" (special case)
// e.g., "www.bbc.com" -> "BBC"
// e.g., "lobste.rs" -> "Lobsters"
func formatDomainAsTitle(domain string) string {
	// Special cases for well-known sites
	knownSites := map[string]string{
		"news.ycombinator.com": "Hacker News",
		"lobste.rs":            "Lobsters",
		"www.reddit.com":       "Reddit",
		"reddit.com":           "Reddit",
		"www.bbc.com":          "BBC News",
		"bbc.com":              "BBC News",
		"www.theguardian.com":  "The Guardian",
		"theguardian.com":      "The Guardian",
		"www.nytimes.com":      "The New York Times",
		"nytimes.com":          "The New York Times",
		"www.washingtonpost.com": "The Washington Post",
		"www.cnn.com":          "CNN",
		"cnn.com":              "CNN",
		"www.npr.org":          "NPR",
		"npr.org":              "NPR",
		"text.npr.org":         "NPR",
		"lite.cnn.com":         "CNN Lite",
		"apnews.com":           "Associated Press",
		"www.reuters.com":      "Reuters",
		"reuters.com":          "Reuters",
	}

	if title, ok := knownSites[domain]; ok {
		return title
	}

	// Strip www. prefix
	domain = strings.TrimPrefix(domain, "www.")

	// Capitalize first letter of each part
	parts := strings.Split(domain, ".")
	if len(parts) >= 2 {
		// Use the main domain name (before TLD)
		name := parts[0]
		// Title case it
		if len(name) > 0 {
			return strings.ToUpper(string(name[0])) + name[1:]
		}
	}

	return domain
}

// buildListLayout creates a clean list of items (HN, Reddit, Lobsters style)
func buildListLayout(doc *Document, result *rules.ApplyResult) {
	// Create a list node to hold all items
	list := &Node{Type: NodeList}

	for _, item := range result.Items {
		listItem := &Node{Type: NodeListItem}

		// Add title as a link if we have href, otherwise as text
		if item.Href != "" {
			link := &Node{
				Type: NodeLink,
				Href: item.Href,
			}
			link.Children = append(link.Children, &Node{
				Type: NodeText,
				Text: item.Title,
			})
			listItem.Children = append(listItem.Children, link)
		} else {
			listItem.Children = append(listItem.Children, &Node{
				Type: NodeText,
				Text: item.Title,
			})
		}

		// Add metadata based on position preference
		if item.Metadata != "" {
			switch result.MetadataPosition {
			case rules.MetadataInline:
				// Add on same line, dimmed in parentheses
				listItem.Children = append(listItem.Children, &Node{
					Type: NodeText,
					Text: " — " + item.Metadata,
				})
			case rules.MetadataBelow:
				// Add metadata on same item but visually separated
				// Using a line break effect by adding as emphasized suffix
				listItem.Children = append(listItem.Children, &Node{
					Type: NodeText,
					Text: " — ",
				})
				listItem.Children = append(listItem.Children, &Node{
					Type: NodeEmphasis, // Will render underlined/italic
					Children: []*Node{{
						Type: NodeText,
						Text: item.Metadata,
					}},
				})
			case rules.MetadataHidden:
				// Don't add metadata
			default:
				// Default to inline with em-dash
				if item.Metadata != "" {
					listItem.Children = append(listItem.Children, &Node{
						Type: NodeText,
						Text: " — " + item.Metadata,
					})
				}
			}
		}

		list.Children = append(list.Children, listItem)
	}

	doc.Content.Children = append(doc.Content.Children, list)
}
