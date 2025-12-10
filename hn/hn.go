// Package hn provides Hacker News comment thread parsing.
// It registers itself with the sites package to handle HN URLs.
package hn

import (
	"fmt"
	"strings"

	"browse/html"
	"browse/sites"

	gohtml "golang.org/x/net/html"
)

func init() {
	sites.Register(&handler{})
}

// handler implements sites.Handler for Hacker News.
type handler struct {
	lastURL string // Track URL for parsing decisions
}

func (h *handler) Name() string {
	return "Hacker News"
}

func (h *handler) Match(url string) bool {
	if IsHNURL(url) {
		h.lastURL = url
		return true
	}
	return false
}

func (h *handler) Parse(rawHTML string) (*html.Document, error) {
	if IsCommentPage(h.lastURL) {
		return ParseComments(rawHTML)
	}
	return ParseFrontPage(rawHTML)
}

// IsCommentPage returns true if the URL is a HN comment/item page.
func IsCommentPage(url string) bool {
	return strings.Contains(url, "news.ycombinator.com/item")
}

// IsHNURL returns true if the URL is any Hacker News page.
func IsHNURL(url string) bool {
	return strings.Contains(url, "news.ycombinator.com")
}

// IsFrontPage returns true if the URL is the HN front page or a listing page.
func IsFrontPage(url string) bool {
	return IsHNURL(url) && !IsCommentPage(url)
}

// HN's iconic orange color
const hnOrange = "#ff6600"

// ParseComments parses a HN comment page and returns a document.
func ParseComments(rawHTML string) (*html.Document, error) {
	root, err := gohtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil, err
	}

	// Extract story title for the header
	storyTitle := extractStoryTitle(root)
	if storyTitle == "" {
		storyTitle = "Hacker News"
	}

	doc := &html.Document{
		Title:      storyTitle,
		ThemeColor: hnOrange,
		Content:    &html.Node{Type: html.NodeDocument},
	}

	// Extract the story header (title, points, etc.)
	if story := extractStoryHeader(root); story != nil {
		doc.Content.Children = append(doc.Content.Children, story)
	}

	// Extract all comments
	comments := extractComments(root)
	doc.Content.Children = append(doc.Content.Children, comments...)

	return doc, nil
}

// Comment represents a parsed HN comment.
type Comment struct {
	Author  string
	Age     string
	Text    string
	Indent  int
	ID      string
	Deleted bool
}

// extractStoryTitle extracts just the story title from a HN page.
func extractStoryTitle(root *gohtml.Node) string {
	var title string
	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode && n.Data == "span" {
			class := getAttr(n, "class")
			if strings.Contains(class, "titleline") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						title = strings.TrimSpace(textContent(c))
						return
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	return title
}

func extractStoryHeader(root *gohtml.Node) *html.Node {
	// Look for the fatitem table or athing row
	var titleText, titleHref string
	var points, commentCount, author, age string

	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode {
			class := getAttr(n, "class")

			// Title is in span.titleline > a
			if n.Data == "span" && strings.Contains(class, "titleline") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						titleText = strings.TrimSpace(textContent(c))
						titleHref = getAttr(c, "href")
						break
					}
				}
			}

			// Points in span.score
			if n.Data == "span" && strings.Contains(class, "score") {
				points = strings.TrimSpace(textContent(n))
			}

			// Author in a.hnuser
			if n.Data == "a" && strings.Contains(class, "hnuser") {
				author = strings.TrimSpace(textContent(n))
			}

			// Age in span.age
			if n.Data == "span" && strings.Contains(class, "age") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						age = strings.TrimSpace(textContent(c))
						break
					}
				}
			}

			// Comment count - look for link with "comments" text
			if n.Data == "a" {
				text := strings.TrimSpace(textContent(n))
				if strings.Contains(text, "comment") {
					commentCount = text
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)

	if titleText == "" {
		return nil
	}

	// Build the header section
	container := &html.Node{Type: html.NodeDocument}

	// Title as H1
	heading := &html.Node{Type: html.NodeHeading1}
	if titleHref != "" && !strings.HasPrefix(titleHref, "item?") {
		link := &html.Node{Type: html.NodeLink, Href: titleHref}
		link.Children = append(link.Children, &html.Node{Type: html.NodeText, Text: titleText})
		heading.Children = append(heading.Children, link)
	} else {
		heading.Children = append(heading.Children, &html.Node{Type: html.NodeText, Text: titleText})
	}
	container.Children = append(container.Children, heading)

	// Metadata line
	var metaParts []string
	if points != "" {
		metaParts = append(metaParts, points)
	}
	if author != "" {
		metaParts = append(metaParts, "by "+author)
	}
	if age != "" {
		metaParts = append(metaParts, age)
	}
	if commentCount != "" {
		metaParts = append(metaParts, commentCount)
	}

	if len(metaParts) > 0 {
		meta := &html.Node{Type: html.NodeParagraph}
		meta.Children = append(meta.Children, &html.Node{
			Type: html.NodeText,
			Text: strings.Join(metaParts, " • "),
		})
		container.Children = append(container.Children, meta)
	}

	// Add a separator
	container.Children = append(container.Children, &html.Node{Type: html.NodeHR})

	return container
}

// extractComments extracts all comments from the page.
func extractComments(root *gohtml.Node) []*html.Node {
	var comments []*html.Node

	// Find all comment rows (class="comtr")
	var commentRows []*gohtml.Node
	var collectRows func(*gohtml.Node)
	collectRows = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode && n.Data == "tr" {
			if class := getAttr(n, "class"); strings.Contains(class, "comtr") {
				commentRows = append(commentRows, n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectRows(c)
		}
	}
	collectRows(root)

	// Process each comment row
	for _, row := range commentRows {
		comment := parseCommentRow(row)
		if comment != nil {
			node := commentToNode(comment)
			if node != nil {
				comments = append(comments, node)
			}
		}
	}

	return comments
}

// parseCommentRow extracts comment data from a table row.
func parseCommentRow(row *gohtml.Node) *Comment {
	comment := &Comment{}

	// Get indentation level
	comment.Indent = getIndentLevel(row)

	// Get comment ID
	comment.ID = getAttr(row, "id")

	// Extract metadata and text
	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode {
			class := getAttr(n, "class")

			// Author
			if n.Data == "a" && strings.Contains(class, "hnuser") {
				comment.Author = strings.TrimSpace(textContent(n))
			}

			// Age
			if n.Data == "span" && strings.Contains(class, "age") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						comment.Age = strings.TrimSpace(textContent(c))
						break
					}
				}
			}

			// Comment text
			if (n.Data == "div" || n.Data == "span") && strings.Contains(class, "commtext") {
				if strings.Contains(class, "c00") || !strings.Contains(class, "c") {
					// Regular comment (c00 = normal color, or no color class)
					comment.Text = extractCommentText(n)
				} else {
					// Faded/dead comment
					comment.Text = extractCommentText(n)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)

	// Check for deleted comment
	if comment.Author == "" && comment.Text == "" {
		// Look for [deleted] or [flagged] text
		text := textContent(row)
		if strings.Contains(text, "[deleted]") || strings.Contains(text, "[flagged]") || strings.Contains(text, "[dead]") {
			comment.Deleted = true
			comment.Text = "[deleted]"
		}
	}

	if comment.Author == "" && comment.Text == "" {
		return nil
	}

	return comment
}

// getIndentLevel extracts the indentation level from a comment row.
func getIndentLevel(row *gohtml.Node) int {
	var indent int
	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode {
			if n.Data == "td" && strings.Contains(getAttr(n, "class"), "ind") {
				// Look for img with width attribute
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "img" {
						if w := getAttr(c, "width"); w != "" {
							var width int
							fmt.Sscanf(w, "%d", &width)
							indent = width / 40
							return
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)
	return indent
}

// extractCommentText extracts and formats comment text.
func extractCommentText(n *gohtml.Node) string {
	var sb strings.Builder
	var extract func(*gohtml.Node)
	extract = func(node *gohtml.Node) {
		switch node.Type {
		case gohtml.TextNode:
			sb.WriteString(node.Data)
		case gohtml.ElementNode:
			switch node.Data {
			case "p":
				if sb.Len() > 0 {
					sb.WriteString("\n\n")
				}
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					extract(c)
				}
			case "br":
				sb.WriteString("\n")
			case "a":
				// Include link text
				linkText := textContent(node)
				href := getAttr(node, "href")
				if href != "" && linkText != href {
					sb.WriteString(linkText)
					sb.WriteString(" [")
					sb.WriteString(href)
					sb.WriteString("]")
				} else {
					sb.WriteString(linkText)
				}
			case "code", "pre":
				sb.WriteString("`")
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					extract(c)
				}
				sb.WriteString("`")
			case "i", "em":
				sb.WriteString("_")
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					extract(c)
				}
				sb.WriteString("_")
			default:
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					extract(c)
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extract(c)
	}
	return strings.TrimSpace(sb.String())
}

// commentToNode converts a Comment to a document Node.
func commentToNode(c *Comment) *html.Node {
	if c == nil {
		return nil
	}

	// Create visual indent with whitespace for hierarchy
	// Each indent level adds 3 spaces (similar to HN's visual style)
	indentSpaces := strings.Repeat("   ", c.Indent)

	// Build the comment block
	block := &html.Node{Type: html.NodeBlockquote}

	// Author and age line with small marker
	if c.Author != "" {
		metaText := c.Author
		if c.Age != "" {
			metaText += " • " + c.Age
		}

		meta := &html.Node{
			Type:   html.NodeParagraph,
			Prefix: indentSpaces + "▸ ", // Small marker at comment start
		}

		// Author in bold
		strong := &html.Node{Type: html.NodeStrong}
		strong.Children = append(strong.Children, &html.Node{Type: html.NodeText, Text: metaText})
		meta.Children = append(meta.Children, strong)
		block.Children = append(block.Children, meta)
	}

	// Comment text - just indented, no markers
	if c.Text != "" {
		// Handle multi-paragraph text
		paragraphs := strings.Split(c.Text, "\n\n")
		for _, para := range paragraphs {
			para = strings.TrimSpace(para)
			if para == "" {
				continue
			}

			textNode := &html.Node{
				Type:   html.NodeParagraph,
				Prefix: indentSpaces + "  ", // Just indent to align with text after marker
			}
			textNode.Children = append(textNode.Children, &html.Node{
				Type: html.NodeText,
				Text: para,
			})
			block.Children = append(block.Children, textNode)
		}
	}

	if len(block.Children) == 0 {
		return nil
	}

	return block
}

// Helper functions

func getAttr(n *gohtml.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func textContent(n *gohtml.Node) string {
	var sb strings.Builder
	var extract func(*gohtml.Node)
	extract = func(node *gohtml.Node) {
		if node.Type == gohtml.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return sb.String()
}

// Story represents a parsed HN front page story.
type Story struct {
	Rank        string
	Title       string
	ArticleHref string
	Site        string
	Points      string
	Author      string
	Age         string
	CommentHref string
	Comments    string
}

// ParseFrontPage parses a HN front/listing page and returns a document.
func ParseFrontPage(rawHTML string) (*html.Document, error) {
	root, err := gohtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil, err
	}

	doc := &html.Document{
		Title:      "Hacker News",
		ThemeColor: hnOrange,
		Content:    &html.Node{Type: html.NodeDocument},
	}

	// Extract all story rows
	stories := extractStories(root)

	// Create a list for all stories
	list := &html.Node{Type: html.NodeList}

	// Convert stories to list items
	for _, story := range stories {
		item := storyToListItem(story)
		if item != nil {
			list.Children = append(list.Children, item)
		}
	}

	doc.Content.Children = append(doc.Content.Children, list)

	return doc, nil
}

// extractStories extracts all stories from the front page.
func extractStories(root *gohtml.Node) []*Story {
	var stories []*Story

	// Find all story rows (class="athing submission")
	var storyRows []*gohtml.Node
	var collectRows func(*gohtml.Node)
	collectRows = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode && n.Data == "tr" {
			class := getAttr(n, "class")
			if strings.Contains(class, "athing") && strings.Contains(class, "submission") {
				storyRows = append(storyRows, n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectRows(c)
		}
	}
	collectRows(root)

	// Process each story row with its following subtext row
	for _, row := range storyRows {
		story := parseStoryRow(row)
		if story != nil {
			// Find the next sibling tr with subtext
			for sib := row.NextSibling; sib != nil; sib = sib.NextSibling {
				if sib.Type == gohtml.ElementNode && sib.Data == "tr" {
					parseSubtextRow(sib, story)
					break
				}
			}
			stories = append(stories, story)
		}
	}

	return stories
}

// parseStoryRow extracts story data from the main story row.
func parseStoryRow(row *gohtml.Node) *Story {
	story := &Story{}

	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode {
			class := getAttr(n, "class")

			// Rank number
			if n.Data == "span" && strings.Contains(class, "rank") {
				story.Rank = strings.TrimSpace(textContent(n))
			}

			// Title and article link in span.titleline > a
			if n.Data == "span" && strings.Contains(class, "titleline") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						story.Title = strings.TrimSpace(textContent(c))
						story.ArticleHref = getAttr(c, "href")
						break
					}
				}
			}

			// Site domain in span.sitestr
			if n.Data == "span" && strings.Contains(class, "sitestr") {
				story.Site = strings.TrimSpace(textContent(n))
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)

	if story.Title == "" {
		return nil
	}

	return story
}

// parseSubtextRow extracts metadata from the subtext row.
func parseSubtextRow(row *gohtml.Node, story *Story) {
	var find func(*gohtml.Node)
	find = func(n *gohtml.Node) {
		if n.Type == gohtml.ElementNode {
			class := getAttr(n, "class")

			// Points in span.score
			if n.Data == "span" && strings.Contains(class, "score") {
				story.Points = strings.TrimSpace(textContent(n))
			}

			// Author in a.hnuser
			if n.Data == "a" && strings.Contains(class, "hnuser") {
				story.Author = strings.TrimSpace(textContent(n))
			}

			// Age in span.age > a
			if n.Data == "span" && strings.Contains(class, "age") {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == gohtml.ElementNode && c.Data == "a" {
						story.Age = strings.TrimSpace(textContent(c))
						break
					}
				}
			}

			// Comments link - look for 'a' with href containing "item?id="
			if n.Data == "a" {
				href := getAttr(n, "href")
				if strings.Contains(href, "item?id=") {
					text := strings.TrimSpace(textContent(n))
					// Check if it's the comments link (not the age link)
					if strings.Contains(text, "comment") || strings.Contains(text, "discuss") {
						story.CommentHref = href
						story.Comments = text
					} else if story.CommentHref == "" && !strings.Contains(text, "ago") {
						// Might be "discuss" or just the item link
						story.CommentHref = href
						story.Comments = text
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)
}

// storyToListItem converts a Story to a list item Node.
func storyToListItem(s *Story) *html.Node {
	if s == nil || s.Title == "" {
		return nil
	}

	// Create a list item for this story
	item := &html.Node{Type: html.NodeListItem}

	// Title with article link
	if s.ArticleHref != "" {
		// Make href absolute if it's relative
		href := s.ArticleHref
		if strings.HasPrefix(href, "item?") {
			href = "https://news.ycombinator.com/" + href
		}

		titleLink := &html.Node{
			Type: html.NodeLink,
			Href: href,
		}
		titleLink.Children = append(titleLink.Children, &html.Node{
			Type: html.NodeText,
			Text: s.Title,
		})
		item.Children = append(item.Children, titleLink)
	} else {
		item.Children = append(item.Children, &html.Node{
			Type: html.NodeText,
			Text: s.Title,
		})
	}

	// Site domain (if external link)
	if s.Site != "" {
		item.Children = append(item.Children, &html.Node{
			Type: html.NodeText,
			Text: " (" + s.Site + ")",
		})
	}

	// Build metadata string
	var metaParts []string
	if s.Points != "" {
		metaParts = append(metaParts, s.Points)
	}
	if s.Author != "" {
		metaParts = append(metaParts, "by "+s.Author)
	}
	if s.Age != "" {
		metaParts = append(metaParts, s.Age)
	}

	// Add metadata inline
	if len(metaParts) > 0 {
		item.Children = append(item.Children, &html.Node{
			Type: html.NodeText,
			Text: " — " + strings.Join(metaParts, " • "),
		})
	}

	// Add comments link
	if s.CommentHref != "" {
		item.Children = append(item.Children, &html.Node{
			Type: html.NodeText,
			Text: " • ",
		})

		// Make href absolute
		href := s.CommentHref
		if strings.HasPrefix(href, "item?") {
			href = "https://news.ycombinator.com/" + href
		}

		commentsLink := &html.Node{
			Type: html.NodeLink,
			Href: href,
		}
		commentsText := s.Comments
		if commentsText == "" {
			commentsText = "discuss"
		}
		commentsLink.Children = append(commentsLink.Children, &html.Node{
			Type: html.NodeText,
			Text: commentsText,
		})
		item.Children = append(item.Children, commentsLink)
	}

	return item
}
