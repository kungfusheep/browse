// Package html provides minimal HTML parsing for article content extraction.
package html

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

// Node represents a content node in the document.
type Node struct {
	Type     NodeType
	Text     string
	Children []*Node
	Href     string // for links
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
)

// Parse extracts article content from HTML.
func Parse(r io.Reader) (*Node, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	root := &Node{Type: NodeDocument}

	// Find the article element
	article := findElement(doc, "article")
	if article == nil {
		article = findElement(doc, "main")
	}
	if article == nil {
		article = findElement(doc, "body")
	}
	if article == nil {
		article = doc
	}

	extractContent(article, root)
	return root, nil
}

// ParseString parses HTML from a string.
func ParseString(s string) (*Node, error) {
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

func extractContent(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			switch c.Data {
			case "h1":
				node := &Node{Type: NodeHeading1, Text: textContent(c)}
				parent.Children = append(parent.Children, node)

			case "h2":
				node := &Node{Type: NodeHeading2, Text: textContent(c)}
				parent.Children = append(parent.Children, node)

			case "h3", "h4", "h5", "h6":
				node := &Node{Type: NodeHeading3, Text: textContent(c)}
				parent.Children = append(parent.Children, node)

			case "p":
				node := &Node{Type: NodeParagraph}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "blockquote":
				node := &Node{Type: NodeBlockquote}
				extractContent(c, node)
				parent.Children = append(parent.Children, node)

			case "ul", "ol":
				node := &Node{Type: NodeList}
				extractList(c, node)
				parent.Children = append(parent.Children, node)

			case "pre":
				node := &Node{Type: NodeCodeBlock, Text: textContent(c)}
				parent.Children = append(parent.Children, node)

			case "article", "main", "section", "div", "header", "footer":
				extractContent(c, parent)
			}

		case html.TextNode:
			// Skip whitespace-only text at document level
		}
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
