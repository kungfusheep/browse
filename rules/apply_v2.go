package rules

import (
	"strings"

	"browse/template"

	"github.com/PuerkitoBio/goquery"
)

// ApplyV2Result contains the rendered output from template-based rules.
type ApplyV2Result struct {
	// The rendered content (ready for display)
	Content string

	// Extracted links for navigation
	Links []ExtractedLink

	// The page type that was matched
	PageTypeName string

	// Raw extracted data (for debugging)
	Data map[string]any
}

// ExtractedLink represents a navigable link.
type ExtractedLink struct {
	Text string
	Href string
}

// ApplyV2 extracts content using named selectors and renders with a template.
// This is the new template-based system.
func ApplyV2(rule *Rule, url, html string) (*ApplyV2Result, error) {
	if !rule.HasPageTypes() {
		return nil, nil // Not a v2 rule
	}

	// Find matching page type
	pageType := rule.GetPageType(url, html)
	if pageType == nil {
		return nil, nil // No matching page type
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	// Extract data using named selectors
	data := extractWithSelectors(doc, pageType.Selectors)

	// Add metadata to data
	data["_domain"] = rule.Domain
	data["_url"] = url
	data["_pageType"] = pageType.Description

	// Collect links from extracted data
	links := collectLinks(data)

	// Render with template
	engine := template.New()
	content, err := engine.Render(pageType.Template, data)
	if err != nil {
		return nil, err
	}

	return &ApplyV2Result{
		Content:      content,
		Links:        links,
		PageTypeName: findPageTypeName(rule, pageType),
		Data:         data,
	}, nil
}

// selectorTree represents hierarchical selectors.
// e.g., "Sections.items.text" becomes a tree:
// Sections -> items (array) -> text (leaf)
type selectorTree struct {
	selector string                  // CSS selector for this node
	isArray  bool                    // Whether this extracts multiple elements
	children map[string]*selectorTree // Nested selectors
}

// buildSelectorTree organizes flat selectors into a hierarchy.
func buildSelectorTree(selectors map[string]string) map[string]*selectorTree {
	root := make(map[string]*selectorTree)

	for name, selector := range selectors {
		parts := strings.Split(name, ".")
		current := root

		for i, part := range parts {
			isLast := i == len(parts)-1

			if current[part] == nil {
				current[part] = &selectorTree{
					children: make(map[string]*selectorTree),
				}
			}

			if isLast {
				// This is the actual selector
				current[part].selector = selector
				current[part].isArray = strings.HasSuffix(selector, "[]")
			}

			current = current[part].children
		}
	}

	return root
}

// extractWithSelectors extracts data using the named selectors.
// Supports multi-level dot notation: "Sections.items.text" extracts text inside
// items arrays inside each Section.
func extractWithSelectors(doc *goquery.Document, selectors map[string]string) map[string]any {
	tree := buildSelectorTree(selectors)
	return extractFromTree(doc.Selection, tree)
}

// extractFromTree recursively extracts data from the selector tree.
func extractFromTree(s *goquery.Selection, tree map[string]*selectorTree) map[string]any {
	data := make(map[string]any)

	for name, node := range tree {
		if node.selector == "" {
			continue // No selector at this level (intermediate node only)
		}

		selector := node.selector
		isArray := node.isArray

		if isArray {
			selector = strings.TrimSuffix(selector, "[]")
		}

		extractType, attrName := parseExtractType(selector)
		selector = cleanSelector(selector)

		selection := s.Find(selector)

		if isArray {
			// Explicitly marked as array - get ALL matches
			var items []map[string]any
			selection.Each(func(i int, elem *goquery.Selection) {
				item := extractElement(elem, extractType, attrName)

				// Recursively extract nested children within this element
				if len(node.children) > 0 {
					nested := extractFromTree(elem, node.children)
					for k, v := range nested {
						// If child is a map with href, flatten to text but only promote href if it's the "text" field
						if childMap, ok := v.(map[string]any); ok {
							// Only promote href from "text" field (main content link)
							if k == "text" {
								if childHref, ok := childMap["href"].(string); ok && childHref != "" {
									item["href"] = childHref
								}
							}
							// Flatten to text for template access
							if childText, ok := childMap["text"].(string); ok {
								item[k] = childText
							} else {
								item[k] = v
							}
						} else {
							item[k] = v
						}
					}
				}

				items = append(items, item)
			})
			data[name] = items
		} else if selection.Length() >= 1 {
			// Not marked as array - only take FIRST match
			selection = selection.First()
			// Single element
			item := extractElement(selection, extractType, attrName)

			// Recursively extract nested children
			if len(node.children) > 0 {
				nested := extractFromTree(selection, node.children)
				for k, v := range nested {
					// If child is a map with href, flatten to text but only promote href if it's the "text" field
					if childMap, ok := v.(map[string]any); ok {
						// Only promote href from "text" field (main content link)
						if k == "text" {
							if childHref, ok := childMap["href"].(string); ok && childHref != "" {
								item["href"] = childHref
							}
						}
						// Flatten to text for template access
						if childText, ok := childMap["text"].(string); ok {
							item[k] = childText
						} else {
							item[k] = v
						}
					} else {
						item[k] = v
					}
				}
			}

			// Flatten leaf nodes to string if just text (no href)
			if len(node.children) == 0 {
				if text, ok := item["text"].(string); ok {
					if _, hasHref := item["href"]; !hasHref {
						data[name] = text
						continue
					}
				}
				if href, ok := item["href"].(string); ok && extractType == "attr" {
					data[name] = href
					continue
				}
			}
			data[name] = item
		}
	}

	return data
}

// parseExtractType parses extraction type from selector.
func parseExtractType(selector string) (extractType, attrName string) {
	extractType = "text"

	if strings.Contains(selector, "@") {
		parts := strings.SplitN(selector, "@", 2)
		attrName = strings.TrimSpace(parts[1])
		extractType = "attr"
	} else if strings.HasSuffix(selector, "|html") {
		extractType = "html"
	}

	return
}

// cleanSelector removes modifiers from selector.
func cleanSelector(selector string) string {
	if idx := strings.Index(selector, "@"); idx != -1 {
		selector = strings.TrimSpace(selector[:idx])
	}
	selector = strings.TrimSuffix(selector, "|html")
	return strings.TrimSpace(selector)
}

// extractElement extracts data from a single element.
func extractElement(s *goquery.Selection, extractType, attrName string) map[string]any {
	item := make(map[string]any)

	switch extractType {
	case "attr":
		if val, exists := s.Attr(attrName); exists {
			item[attrName] = val
		}
	case "html":
		html, _ := s.Html()
		item["html"] = strings.TrimSpace(html)
	default: // text
		item["text"] = strings.TrimSpace(s.Text())
	}

	// Always try to extract href - check multiple sources
	var href string
	var hrefFound bool

	// 1. Check if element itself has href
	href, hrefFound = s.Attr("href")

	// 2. Check for descendant <a> element
	if !hrefFound {
		if link := s.Find("a").First(); link.Length() > 0 {
			href, hrefFound = link.Attr("href")
			// Also get link text if we don't have text yet
			if _, hasText := item["text"]; !hasText {
				item["text"] = strings.TrimSpace(link.Text())
			}
		}
	}

	// 3. Check ancestor <a> elements (common pattern: headline inside <a>)
	if !hrefFound {
		ancestor := s.ParentsFiltered("a").First()
		if ancestor.Length() > 0 {
			href, hrefFound = ancestor.Attr("href")
		}
	}

	if hrefFound && href != "" {
		item["href"] = href
	}

	// Extract common attributes
	if title, exists := s.Attr("title"); exists {
		item["title"] = title
	}
	if class, exists := s.Attr("class"); exists {
		item["class"] = class
	}

	return item
}

// collectLinks gathers all links from the extracted data.
func collectLinks(data map[string]any) []ExtractedLink {
	var links []ExtractedLink
	collectLinksRecursive(data, &links)
	return links
}

func collectLinksRecursive(data any, links *[]ExtractedLink) {
	switch v := data.(type) {
	case map[string]any:
		// Check if this map has href
		if href, ok := v["href"].(string); ok && href != "" {
			text := ""
			if t, ok := v["text"].(string); ok {
				text = t
			} else if t, ok := v["title"].(string); ok {
				text = t
			}
			*links = append(*links, ExtractedLink{Text: text, Href: href})
		}
		// Recurse into values
		for _, val := range v {
			collectLinksRecursive(val, links)
		}
	case []map[string]any:
		for _, item := range v {
			collectLinksRecursive(item, links)
		}
	case []any:
		for _, item := range v {
			collectLinksRecursive(item, links)
		}
	}
}

// findPageTypeName finds the name of a page type in the rule.
func findPageTypeName(rule *Rule, pt *PageType) string {
	for name, pageType := range rule.PageTypes {
		if pageType == pt {
			return name
		}
	}
	return "unknown"
}
