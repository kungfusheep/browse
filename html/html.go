// Package html provides minimal HTML parsing for article content extraction.
package html

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

// Document represents a parsed HTML document with separate content and navigation.
type Document struct {
	Content    *Node   // Main article content
	Navigation []*Node // Navigation elements (nav, header, footer links)
}

// Node represents a content node in the document.
type Node struct {
	Type     NodeType
	Text     string
	Children []*Node
	Href     string // for links

	// Form fields
	FormAction string // for forms
	FormMethod string // GET or POST
	InputName  string // for inputs
	InputType  string // text, submit, etc.
	InputValue string // default value or button label
}

// NodeType identifies the kind of content node.
type NodeType int

const (
	NodeDocument NodeType = iota
	NodeHeading1
	NodeHeading2
	NodeHeading3
	NodeParagraph
	NodeBlockquote
	NodeList
	NodeListItem
	NodeCode
	NodeCodeBlock
	NodeLink
	NodeText
	NodeStrong
	NodeEmphasis
	NodeForm
	NodeInput
	NodeNavSection // A navigation section (from nav, header, footer)
)

// Parse extracts article content from HTML, returning a Document with
// main content and navigation elements separated.
func Parse(r io.Reader) (*Document, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	result := &Document{
		Content: &Node{Type: NodeDocument},
	}

	// Find the body element to extract navigation from the whole page
	body := findElement(doc, "body")
	if body == nil {
		body = doc
	}

	// First pass: extract all navigation elements from the entire body
	extractNavigation(body, result)

	// Find the article element for main content
	article := findElement(doc, "article")
	if article == nil {
		article = findElement(doc, "main")
	}
	if article == nil {
		article = body
	}

	// Second pass: extract content (navigation will be skipped)
	extractContentOnly(article, result.Content)
	return result, nil
}

// ParseString parses HTML from a string.
func ParseString(s string) (*Document, error) {
	return Parse(strings.NewReader(s))
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// extractNavigation walks the tree and extracts all navigation elements.
func extractNavigation(n *html.Node, doc *Document) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			switch c.Data {
			case "nav", "header", "footer", "aside", "menu":
				navNode := extractNavLinks(c)
				if navNode != nil && len(navNode.Children) > 0 {
					doc.Navigation = append(doc.Navigation, navNode)
				}
			default:
				// Recurse into other elements to find nested nav elements
				extractNavigation(c, doc)
			}
		}
	}
}

// extractContentOnly extracts content, skipping navigation elements.
func extractContentOnly(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			switch c.Data {
			case "h1":
				node := &Node{Type: NodeHeading1, Text: textContent(c)}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h2":
				node := &Node{Type: NodeHeading2, Text: textContent(c)}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h3", "h4", "h5", "h6":
				node := &Node{Type: NodeHeading3, Text: textContent(c)}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "p":
				node := &Node{Type: NodeParagraph}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "blockquote":
				node := &Node{Type: NodeBlockquote}
				extractContentOnly(c, node)
				parent.Children = append(parent.Children, node)

			case "ul", "ol":
				node := &Node{Type: NodeList}
				extractList(c, node)
				parent.Children = append(parent.Children, node)

			case "pre":
				node := &Node{Type: NodeCodeBlock, Text: textContent(c)}
				parent.Children = append(parent.Children, node)

			case "nav", "header", "footer", "aside", "menu":
				// Skip navigation elements (already extracted)
				continue

			case "article", "main", "section", "div", "span",
				"center", "nobr", "table", "tbody", "tr", "td", "th", "b", "i", "u", "font":
				extractContentOnly(c, parent)

			case "a":
				// Standalone link (not inside a paragraph) - treat as a paragraph with a link
				text := strings.TrimSpace(textContent(c))
				if text != "" {
					node := &Node{Type: NodeParagraph}
					link := &Node{Type: NodeLink, Href: getAttr(c, "href")}
					link.Children = append(link.Children, &Node{Type: NodeText, Text: text})
					node.Children = append(node.Children, link)
					parent.Children = append(parent.Children, node)
				}

			case "form":
				// Extract form with its action
				formNode := &Node{
					Type:       NodeForm,
					FormAction: getAttr(c, "action"),
					FormMethod: strings.ToUpper(getAttr(c, "method")),
				}
				if formNode.FormMethod == "" {
					formNode.FormMethod = "GET"
				}
				extractContentOnly(c, formNode)
				parent.Children = append(parent.Children, formNode)

			case "input":
				inputType := getAttr(c, "type")
				if inputType == "" {
					inputType = "text"
				}
				// Only capture visible text inputs and submit buttons
				if inputType == "text" || inputType == "search" || inputType == "submit" {
					node := &Node{
						Type:       NodeInput,
						InputName:  getAttr(c, "name"),
						InputType:  inputType,
						InputValue: getAttr(c, "value"),
						Text:       getAttr(c, "placeholder"),
					}
					if node.Text == "" {
						node.Text = getAttr(c, "title")
					}
					parent.Children = append(parent.Children, node)
				}
			}

		case html.TextNode:
			// Capture significant text content not wrapped in elements
			text := strings.TrimSpace(c.Data)
			if text != "" && len(text) > 1 {
				// Create an implicit paragraph for loose text
				node := &Node{Type: NodeParagraph}
				node.Children = append(node.Children, &Node{Type: NodeText, Text: text})
				parent.Children = append(parent.Children, node)
			}
		}
	}
}

// extractNavLinks extracts all links from a navigation element.
func extractNavLinks(n *html.Node) *Node {
	navNode := &Node{
		Type: NodeNavSection,
		Text: getNavLabel(n), // Try to get aria-label or similar
	}

	var extractLinks func(*html.Node)
	extractLinks = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			text := strings.TrimSpace(textContent(node))
			href := getAttr(node, "href")
			if text != "" && href != "" {
				link := &Node{
					Type: NodeLink,
					Href: href,
					Text: text,
				}
				navNode.Children = append(navNode.Children, link)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extractLinks(c)
		}
	}

	extractLinks(n)
	return navNode
}

// getNavLabel tries to find a label for the navigation element.
func getNavLabel(n *html.Node) string {
	// Try aria-label first
	if label := getAttr(n, "aria-label"); label != "" {
		return label
	}
	// Try the element name
	switch n.Data {
	case "nav":
		return "Navigation"
	case "header":
		return "Header"
	case "footer":
		return "Footer"
	case "aside":
		return "Sidebar"
	case "menu":
		return "Menu"
	default:
		return "Links"
	}
}

func extractList(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" {
			item := &Node{Type: NodeListItem}
			extractInline(c, item)
			parent.Children = append(parent.Children, item)
		}
	}
}

func extractInline(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			text := c.Data
			if text != "" {
				parent.Children = append(parent.Children, &Node{Type: NodeText, Text: text})
			}

		case html.ElementNode:
			switch c.Data {
			case "a":
				link := &Node{Type: NodeLink, Href: getAttr(c, "href")}
				extractInline(c, link)
				parent.Children = append(parent.Children, link)

			case "strong", "b":
				node := &Node{Type: NodeStrong}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "em", "i":
				node := &Node{Type: NodeEmphasis}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "code":
				parent.Children = append(parent.Children, &Node{Type: NodeCode, Text: textContent(c)})

			default:
				extractInline(c, parent)
			}
		}
	}
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return strings.TrimSpace(sb.String())
}

func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// extractHeadingLink finds a link inside a heading element and adds it as a child.
func extractHeadingLink(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "a" {
			link := &Node{Type: NodeLink, Href: getAttr(c, "href")}
			parent.Children = append(parent.Children, link)
			return
		}
		// Check nested elements
		extractHeadingLink(c, parent)
	}
}

// PlainText returns the plain text content of a node and its children.
func (n *Node) PlainText() string {
	var sb strings.Builder
	n.appendPlainText(&sb)
	return sb.String()
}

func (n *Node) appendPlainText(sb *strings.Builder) {
	if n.Text != "" {
		sb.WriteString(n.Text)
	}
	for _, child := range n.Children {
		child.appendPlainText(sb)
	}
}
