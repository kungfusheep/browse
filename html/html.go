// Package html provides minimal HTML parsing for article content extraction.
package html

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"browse/latex"
	"golang.org/x/net/html"
)

// whitespaceRe collapses sequences of whitespace to a single space
var whitespaceRe = regexp.MustCompile(`\s+`)

// Options configures HTML parsing behavior.
type Options struct {
	LatexEnabled  bool // Process LaTeX math expressions
	TablesEnabled bool // Parse and render HTML tables
}

// DefaultOptions returns the default parsing options.
func DefaultOptions() Options {
	return Options{
		LatexEnabled:  true,
		TablesEnabled: true,
	}
}

// Package-level options (set via Configure)
var opts = DefaultOptions()

// Configure sets the package-level parsing options.
func Configure(o Options) {
	opts = o
}

// latexEnabled returns whether LaTeX processing is enabled.
func latexEnabled() bool {
	return opts.LatexEnabled
}

// tablesEnabled returns whether table parsing is enabled.
func tablesEnabled() bool {
	return opts.TablesEnabled
}

// Document represents a parsed HTML document with separate content and navigation.
type Document struct {
	Title      string  // Page title (from <title> tag)
	URL        string  // Source URL of the document
	Lang       string  // Document language (from <html lang="...">)
	ThemeColor string  // Site theme color (from meta tags or bgcolor)
	Content    *Node   // Main article content
	Navigation []*Node // Navigation elements (nav, header, footer links)
}

// Node represents a content node in the document.
type Node struct {
	Type     NodeType
	Text     string
	Children []*Node
	Href     string // for links
	ID       string // element id attribute (for anchor links)

	// Form fields
	FormAction string // for forms
	FormMethod string // GET or POST
	InputName  string // for inputs
	InputType  string // text, submit, etc.
	InputValue string // default value or button label

	// Table cell properties
	IsHeader bool // true for th cells

	// Layout hints
	Prefix string // prefix to add to every line (e.g., "│ " for threaded comments)
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
	NodeImage // Image link (styled distinctively, clickable to open in Quick Look)
	NodeText
	NodeStrong
	NodeEmphasis
	NodeMark // Highlighted/marked text (renders with reverse video)
	NodeMarkInsert // Insert-mode cursor (renders with reverse video + color)
	NodeForm
	NodeInput
	NodeNavSection // A navigation section (from nav, header, footer)
	NodeTable      // A data table
	NodeTableRow   // A table row
	NodeTableCell  // A table cell (th or td)
	NodeAnchor     // An anchor point (ID only, no content)
	NodeHR         // Horizontal rule/separator
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

	// Extract language from <html lang="...">
	if htmlElem := findElement(doc, "html"); htmlElem != nil {
		for _, a := range htmlElem.Attr {
			if a.Key == "lang" && a.Val != "" {
				// Normalize to just the language code (e.g., "en-US" -> "en")
				lang := a.Val
				if idx := strings.Index(lang, "-"); idx > 0 {
					lang = lang[:idx]
				}
				result.Lang = strings.ToLower(lang)
				break
			}
		}
	}

	// Extract theme color from meta tags (priority: theme-color > msapplication-TileColor > bgcolor)
	result.ThemeColor = extractThemeColor(doc)

	// Extract title from <title> tag
	if title := findElement(doc, "title"); title != nil {
		result.Title = extractText(title)
	}

	// Find the body element to extract navigation from the whole page
	body := findElement(doc, "body")
	if body == nil {
		body = doc
	}

	// First pass: extract all navigation elements from the entire body
	extractNavigation(body, result)

	// Find the best content container using multiple strategies
	contentRoot := findContentRoot(doc, body)

	// Second pass: extract content (navigation will be skipped)
	extractContentOnly(contentRoot, result.Content)

	// Post-process: normalize news index pages
	if looksLikeNewsIndex(result.Content) {
		normalizeNewsIndex(result.Content)
	}

	return result, nil
}

// findContentRoot finds the best element to extract content from.
func findContentRoot(doc, body *html.Node) *html.Node {
	// Strategy 1: Semantic elements
	// If we find <main>, check if it contains a single content-rich <article>
	// (like GitHub READMEs). If so, prefer the article. If main contains
	// multiple articles (like NYTimes index), keep the main.
	if main := findElement(doc, "main"); main != nil {
		// Look for article with content-indicator classes inside main
		if article := findContentArticle(main); article != nil {
			return article
		}
		// Check if main has multiple articles FIRST (index page pattern)
		// Must check before findContentDiv since story-body divs may be nested inside articles
		articleCount := countArticles(main)
		if articleCount >= 2 {
			return main // Index page - keep the container
		}
		// Look for div with content-indicator classes inside main (AP News single article pattern)
		if contentDiv := findContentDiv(main); contentDiv != nil {
			return contentDiv
		}
		// Single or no articles - check if there's any article with real content
		if article := findElement(main, "article"); article != nil {
			return article
		}
		return main
	}
	if article := findElement(doc, "article"); article != nil {
		return article
	}

	// Strategy 2: Common content div patterns (role="main", id="content", etc.)
	// Use findContentContainerByID to avoid matching headings with these IDs
	if content := findByAttribute(body, "role", "main"); content != nil {
		return content
	}
	if content := findContentContainerByID(body, "content"); content != nil {
		return content
	}
	if content := findContentContainerByID(body, "main-content"); content != nil {
		return content
	}
	if content := findContentContainerByID(body, "main"); content != nil {
		return content
	}

	// Strategy 3: Find the div with the most paragraph content
	if best := findContentRichDiv(body); best != nil {
		return best
	}

	return body
}

// contentIndicatorClasses are classes that indicate an element contains main content.
var contentIndicatorClasses = []string{
	"markdown-body",     // GitHub
	"entry-content",     // WordPress, GitHub
	"post-content",      // Blogs
	"article-body",      // News sites
	"article-content",   // News sites
	"content-body",      // Generic
	"RichTextStoryBody", // AP News
	"story-body",        // Various news sites
	"article__body",     // BEM-style news sites
	"post__content",     // BEM-style blogs
}

// findContentArticle finds an article element with classes indicating it's main content.
func findContentArticle(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "article" {
		class := getAttr(n, "class")
		for _, c := range contentIndicatorClasses {
			if strings.Contains(class, c) {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findContentArticle(c); found != nil {
			return found
		}
	}
	return nil
}

// findContentDiv finds a div element with classes indicating it's main content.
func findContentDiv(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && (n.Data == "div" || n.Data == "section") {
		class := getAttr(n, "class")
		for _, c := range contentIndicatorClasses {
			if strings.Contains(class, c) {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findContentDiv(c); found != nil {
			return found
		}
	}
	return nil
}

// countArticles counts the number of <article> elements within a node.
func countArticles(n *html.Node) int {
	count := 0
	var countRecursive func(*html.Node)
	countRecursive = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "article" {
			count++
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			countRecursive(c)
		}
	}
	countRecursive(n)
	return count
}

// findByAttribute finds an element with a specific attribute value.
func findByAttribute(n *html.Node, attr, value string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == attr && a.Val == value {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByAttribute(c, attr, value); found != nil {
			return found
		}
	}
	return nil
}

// findByID finds an element by its id attribute.
func findByID(n *html.Node, id string) *html.Node {
	return findByAttribute(n, "id", id)
}

// findContentContainerByID finds a container element (div, section, article, main) by ID.
// This avoids matching headings or other elements that happen to have the same ID.
func findContentContainerByID(n *html.Node, id string) *html.Node {
	if n.Type == html.ElementNode {
		// Only match container-like elements
		switch n.Data {
		case "div", "section", "article", "main", "aside":
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == id {
					return n
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findContentContainerByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

// findContentRichDiv finds the div with the most content-like children.
func findContentRichDiv(n *html.Node) *html.Node {
	var best *html.Node
	bestScore := 0

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "div" || node.Data == "section") {
			score := scoreContentRichness(node)
			if score > bestScore {
				bestScore = score
				best = node
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)

	// Only return if we found something meaningful
	if bestScore >= 3 {
		return best
	}
	return nil
}

// scoreContentRichness scores how likely an element is to be the main content.
func scoreContentRichness(n *html.Node) int {
	score := 0
	paragraphs := 0
	headings := 0
	links := 0
	articles := 0

	var count func(*html.Node, int)
	count = func(node *html.Node, depth int) {
		if depth > 10 {
			return // Don't go too deep
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p":
				paragraphs++
			case "h1", "h2", "h3":
				headings++
			case "a":
				links++
			case "article":
				articles++
			case "nav", "header", "footer", "aside":
				score -= 5 // Penalize containers with nav elements
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			count(c, depth+1)
		}
	}
	count(n, 0)

	// Score based on content indicators
	score += paragraphs * 2
	score += headings * 3
	score += articles * 5
	// Links are neutral - too many might indicate nav, but some are expected

	return score
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

// extractText recursively extracts all text content from a node.
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var result strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		result.WriteString(extractText(c))
	}
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(result.String(), " "))
}

// extractThemeColor extracts the site theme color from meta tags and attributes.
// Priority: msapplication-TileColor > bgcolor > theme-color
// (TileColor and bgcolor tend to be actual brand colors, while theme-color is often white/black)
func extractThemeColor(doc *html.Node) string {
	var themeColor, tileColor, bgColor string

	// Find <head> element for meta tags
	head := findElement(doc, "head")
	if head != nil {
		// Look through meta tags
		var checkMeta func(*html.Node)
		checkMeta = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "meta" {
				name := strings.ToLower(getAttr(n, "name"))
				content := getAttr(n, "content")
				if content != "" {
					switch name {
					case "theme-color":
						if themeColor == "" {
							themeColor = content
						}
					case "msapplication-tilecolor":
						if tileColor == "" {
							tileColor = content
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				checkMeta(c)
			}
		}
		checkMeta(head)
	}

	// Check for bgcolor on body or html (legacy sites like HN)
	if body := findElement(doc, "body"); body != nil {
		if bg := getAttr(body, "bgcolor"); bg != "" && bgColor == "" {
			bgColor = bg
		}
	}
	if htmlElem := findElement(doc, "html"); htmlElem != nil {
		if bg := getAttr(htmlElem, "bgcolor"); bg != "" && bgColor == "" {
			bgColor = bg
		}
	}

	// Return by priority, filtering out unusable colors
	// TileColor is usually the actual brand color
	if tileColor != "" {
		if color := normalizeColor(tileColor); isUsableAccentColor(color) {
			return color
		}
	}
	// bgcolor is usually meaningful (e.g., HN orange)
	if bgColor != "" {
		if color := normalizeColor(bgColor); isUsableAccentColor(color) {
			return color
		}
	}
	// theme-color is often white/black, but use it as fallback
	if themeColor != "" {
		if color := normalizeColor(themeColor); isUsableAccentColor(color) {
			return color
		}
	}
	return ""
}

// isUsableAccentColor checks if a color is distinctive enough to be useful as an accent.
// Filters out white, near-white, black, and near-black colors.
func isUsableAccentColor(hex string) bool {
	if hex == "" {
		return false
	}
	r, g, b, ok := ParseHexColor(hex)
	if !ok {
		return false
	}

	// Calculate luminance (simplified)
	luminance := (int(r) + int(g) + int(b)) / 3

	// Filter out too bright (near white) - threshold ~240
	if luminance > 240 {
		return false
	}
	// Filter out too dark (near black) - threshold ~15
	if luminance < 15 {
		return false
	}

	return true
}

// normalizeColor normalizes a color value to a consistent format.
// Returns lowercase hex color (e.g., "#ff6600") or empty string if invalid.
func normalizeColor(color string) string {
	color = strings.TrimSpace(strings.ToLower(color))

	// Already a hex color
	if strings.HasPrefix(color, "#") {
		// Expand shorthand (#f60 -> #ff6600)
		if len(color) == 4 {
			return "#" + string(color[1]) + string(color[1]) +
				string(color[2]) + string(color[2]) +
				string(color[3]) + string(color[3])
		}
		if len(color) == 7 {
			return color
		}
		return "" // Invalid format
	}

	// Named colors (common ones)
	namedColors := map[string]string{
		"white":   "#ffffff",
		"black":   "#000000",
		"red":     "#ff0000",
		"green":   "#00ff00",
		"blue":    "#0000ff",
		"yellow":  "#ffff00",
		"orange":  "#ffa500",
		"purple":  "#800080",
		"gray":    "#808080",
		"grey":    "#808080",
		"silver":  "#c0c0c0",
		"maroon":  "#800000",
		"navy":    "#000080",
		"teal":    "#008080",
		"aqua":    "#00ffff",
		"fuchsia": "#ff00ff",
		"lime":    "#00ff00",
		"olive":   "#808000",
	}
	if hex, ok := namedColors[color]; ok {
		return hex
	}

	return "" // Unknown format
}

// ExtractThemeColorFromHTML extracts the theme color from raw HTML string.
// This is useful for setting the theme color on documents created by site-specific handlers.
// Uses lightweight string extraction rather than full HTML parsing.
// Priority: msapplication-TileColor > bgcolor > theme-color (matching extractThemeColor)
func ExtractThemeColorFromHTML(htmlContent string) string {
	lower := strings.ToLower(htmlContent)

	// Try msapplication-TileColor first (usually the actual brand color)
	if color := extractMetaContent(lower, htmlContent, "msapplication-tilecolor"); color != "" {
		if normalized := normalizeColor(color); isUsableAccentColor(normalized) {
			return normalized
		}
	}

	// Try bgcolor on body or html (legacy sites like HN)
	if idx := strings.Index(lower, "bgcolor="); idx != -1 {
		// Skip past "bgcolor=" to get the value
		color := extractAttrValue(htmlContent[idx+8:])
		if color != "" {
			if normalized := normalizeColor(color); isUsableAccentColor(normalized) {
				return normalized
			}
		}
	}

	// Try theme-color as fallback (often white/black, but sometimes useful)
	if color := extractMetaContent(lower, htmlContent, "theme-color"); color != "" {
		if normalized := normalizeColor(color); isUsableAccentColor(normalized) {
			return normalized
		}
	}

	return ""
}

// extractMetaContent extracts content attribute from a meta tag with given name.
func extractMetaContent(lowerHTML, originalHTML, metaName string) string {
	// Look for meta name="..." pattern
	searchName := `name="` + metaName + `"`
	idx := strings.Index(lowerHTML, searchName)
	if idx == -1 {
		// Try with single quotes
		searchName = `name='` + metaName + `'`
		idx = strings.Index(lowerHTML, searchName)
	}
	if idx == -1 {
		return ""
	}

	// Find the content attribute nearby (within 100 chars)
	start := idx
	end := idx + 100
	if end > len(lowerHTML) {
		end = len(lowerHTML)
	}
	snippet := lowerHTML[start:end]
	origSnippet := originalHTML[start:end]

	contentIdx := strings.Index(snippet, "content=")
	if contentIdx == -1 {
		return ""
	}

	return extractAttrValue(origSnippet[contentIdx+8:])
}

// extractAttrValue extracts a quoted or unquoted attribute value.
func extractAttrValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}

	quote := s[0]
	if quote == '"' || quote == '\'' {
		end := strings.IndexByte(s[1:], quote)
		if end == -1 {
			return ""
		}
		return s[1 : end+1]
	}

	// Unquoted - ends at space or >
	end := strings.IndexAny(s, " \t\n\r>")
	if end == -1 {
		return s
	}
	return s[:end]
}

// ParseHexColor converts a hex color string to RGB values.
// Returns r, g, b values (0-255) and ok=true if successful.
func ParseHexColor(hex string) (r, g, b uint8, ok bool) {
	hex = strings.TrimPrefix(strings.ToLower(hex), "#")

	// Handle shorthand (#f60 -> ff6600)
	if len(hex) == 3 {
		hex = string(hex[0]) + string(hex[0]) +
			string(hex[1]) + string(hex[1]) +
			string(hex[2]) + string(hex[2])
	}

	if len(hex) != 6 {
		return 0, 0, 0, false
	}

	// Parse each component
	var ri, gi, bi int64
	var err error
	if ri, err = parseHexByte(hex[0:2]); err != nil {
		return 0, 0, 0, false
	}
	if gi, err = parseHexByte(hex[2:4]); err != nil {
		return 0, 0, 0, false
	}
	if bi, err = parseHexByte(hex[4:6]); err != nil {
		return 0, 0, 0, false
	}

	return uint8(ri), uint8(gi), uint8(bi), true
}

func parseHexByte(s string) (int64, error) {
	var result int64
	for _, c := range s {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= int64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= int64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			result |= int64(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex character: %c", c)
		}
	}
	return result, nil
}

// articleData holds extracted data from an article element.
type articleData struct {
	Title       string
	TitleHref   string
	Description string
	Author      string
	Date        string
	Source      string // domain name from title href
}

// shouldExtractAsArticleList checks if a container has enough article children
// to be treated as an article listing page (like a news index).
func shouldExtractAsArticleList(n *html.Node) bool {
	// Strategy 1: Count direct <article> children
	articleCount := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "article" {
			// Skip sponsored/ad articles
			class := getAttr(c, "class")
			if strings.Contains(class, "sponsored") || strings.Contains(class, "ad-") ||
				strings.Contains(class, "promo") || strings.Contains(class, "advertisement") {
				continue
			}
			articleCount++
		}
	}
	if articleCount >= 3 {
		return true
	}

	// Strategy 2: Look for repeated heading+link patterns (story cards)
	// This handles sites like NYTimes where articles contain multiple stories
	storyCount := countStoryCards(n)
	return storyCount >= 3
}

// countStoryCards counts heading+link patterns that look like story cards.
func countStoryCards(n *html.Node) int {
	count := 0
	var countRecursive func(*html.Node, int)
	countRecursive = func(node *html.Node, depth int) {
		if depth > 5 {
			return // Don't go too deep
		}
		if node.Type == html.ElementNode {
			// Look for h2/h3 elements containing links (typical story card pattern)
			if node.Data == "h2" || node.Data == "h3" {
				if link := findFirstLink(node); link != nil {
					href := getAttr(link, "href")
					text := textContent(link)
					// Only count if it's a substantial link (not just "#")
					// Skip numbered titles like "1. Title" - those are search results, not news
					if href != "" && !strings.HasPrefix(href, "#") && len(text) > 10 && !looksLikeNumberedItem(text) {
						count++
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			countRecursive(c, depth+1)
		}
	}
	countRecursive(n, 0)
	return count
}

// looksLikeNumberedItem checks if text starts with a number and period like "1. Title"
func looksLikeNumberedItem(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 3 {
		return false
	}
	// Check for patterns like "1. ", "12. ", "123. "
	for i, ch := range text {
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' && i > 0 && i < len(text)-1 {
			// Found number followed by period
			return text[i+1] == ' '
		}
		break
	}
	return false
}

// extractArticleList extracts multiple articles as a structured list.
func extractArticleList(n *html.Node, parent *Node) {
	// Strategy 1: Story cards (heading+link patterns)
	// This works better for sites like NYTimes where <article> containers hold multiple stories
	list := &Node{Type: NodeList}
	extractStoryCards(n, list)

	// Strategy 2: If story cards didn't work well, try direct <article> children
	if len(list.Children) < 3 {
		list.Children = nil // Reset
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "article" {
				// Skip sponsored/ad articles
				class := getAttr(c, "class")
				if strings.Contains(class, "sponsored") || strings.Contains(class, "ad-") ||
					strings.Contains(class, "promo") || strings.Contains(class, "advertisement") {
					continue
				}

				data := extractArticleEntry(c)
				if data.Title == "" {
					continue // Skip articles without extractable title
				}

				item := articleDataToListItem(data)
				list.Children = append(list.Children, item)
			}
		}
	}

	if len(list.Children) > 0 {
		parent.Children = append(parent.Children, list)
	}
}

// extractStoryCards finds story cards by looking for heading+link patterns.
func extractStoryCards(n *html.Node, list *Node) {
	var extract func(*html.Node, int)
	extract = func(node *html.Node, depth int) {
		if depth > 6 {
			return // Don't go too deep
		}

		if node.Type == html.ElementNode {
			// Skip sponsored/ad containers
			if isAdContent(node) {
				return
			}

			// When we find an h2/h3 with a link, extract it as a story card
			if node.Data == "h2" || node.Data == "h3" {
				// Also check if any ancestor is sponsored
				if isInsideAdContent(node) {
					return
				}

				if link := findFirstLink(node); link != nil {
					href := getAttr(link, "href")
					text := strings.TrimSpace(textContent(link))

					// Only process substantial links
					// Skip numbered items like "1. Title" - those are search results, not news
					if href != "" && !strings.HasPrefix(href, "#") && len(text) > 10 && !looksLikeNumberedItem(text) {
						data := articleData{
							Title:     text,
							TitleHref: href,
							Source:    extractDomain(href),
						}

						// Look for description in next sibling <p>
						data.Description = findSiblingDescription(node)

						// Look for author/date in parent or siblings
						data.Author, data.Date = findNearbyMetadata(node)

						item := articleDataToListItem(data)
						list.Children = append(list.Children, item)
						return // Don't recurse into this heading
					}
				}
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c, depth+1)
		}
	}
	extract(n, 0)
}

// isAdContent checks if a node looks like sponsored/ad content.
func isAdContent(node *html.Node) bool {
	class := strings.ToLower(getAttr(node, "class"))
	return strings.Contains(class, "sponsored") || strings.Contains(class, "ad-") ||
		strings.Contains(class, "promo") || strings.Contains(class, "advertisement")
}

// isInsideAdContent checks if any ancestor of a node is ad content.
func isInsideAdContent(node *html.Node) bool {
	for p := node.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && isAdContent(p) {
			return true
		}
	}
	return false
}

// containsBlockContent checks if a node contains block-level elements.
// Used to detect when an <a> wraps a whole card (heading + paragraph + metadata).
func containsBlockContent(n *html.Node) bool {
	blockTags := map[string]bool{
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"p": true, "div": true, "section": true, "article": true,
	}

	var hasBlock func(*html.Node, int) bool
	hasBlock = func(node *html.Node, depth int) bool {
		if depth > 5 {
			return false
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if blockTags[c.Data] {
					return true
				}
				if hasBlock(c, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return hasBlock(n, 0)
}

// extractBlockLink extracts structured content from an <a> that wraps block content.
// Applies the link to the title only, keeps description/metadata as plain text.
func extractBlockLink(a *html.Node, href string, parent *Node) {
	// Find the title: first heading or first substantial text
	var title string
	var description string
	var metadata []string

	var extract func(*html.Node, int)
	extract = func(node *html.Node, depth int) {
		if depth > 6 {
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch c.Data {
				case "h1", "h2", "h3", "h4", "h5", "h6":
					// Heading becomes the title
					if title == "" {
						title = strings.TrimSpace(textContent(c))
					}
				case "p":
					// First substantial paragraph becomes description
					text := strings.TrimSpace(textContent(c))
					if description == "" && len(text) > 20 {
						description = text
					} else if len(text) > 0 && len(text) < 100 {
						// Short text might be metadata
						metadata = append(metadata, text)
					}
				case "span", "time":
					// Likely metadata (time, category, etc.)
					text := strings.TrimSpace(textContent(c))
					if text != "" && len(text) < 100 {
						metadata = append(metadata, text)
					}
				case "img", "picture", "figure", "svg":
					// Skip images
					continue
				default:
					// Recurse into other containers
					extract(c, depth+1)
				}
			}
		}
	}
	extract(a, 0)

	// If no heading found, use first substantial text as title
	if title == "" && description != "" {
		title = description
		description = ""
	}

	if title == "" {
		// No extractable content, fall back to simple link
		text := strings.TrimSpace(textContent(a))
		if text != "" {
			node := &Node{Type: NodeParagraph}
			link := &Node{Type: NodeLink, Href: href}
			link.Children = append(link.Children, &Node{Type: NodeText, Text: text})
			node.Children = append(node.Children, link)
			parent.Children = append(parent.Children, node)
		}
		return
	}

	// Build structured output: linked title + description + metadata
	para := &Node{Type: NodeParagraph}

	// Title as link
	link := &Node{Type: NodeLink, Href: href}
	link.Children = append(link.Children, &Node{Type: NodeText, Text: title})
	para.Children = append(para.Children, link)

	// Description as plain text (truncated)
	if description != "" {
		desc := truncateDescription(description, 120)
		para.Children = append(para.Children, &Node{Type: NodeText, Text: "\n" + desc})
	}

	// Metadata as dimmed text
	if len(metadata) > 0 {
		// Filter out duplicates and very short items
		seen := make(map[string]bool)
		var filtered []string
		for _, m := range metadata {
			m = strings.TrimSpace(m)
			if len(m) > 2 && !seen[m] && m != title {
				seen[m] = true
				filtered = append(filtered, m)
			}
		}
		if len(filtered) > 0 {
			// Limit metadata items
			if len(filtered) > 3 {
				filtered = filtered[:3]
			}
			para.Children = append(para.Children, &Node{Type: NodeText, Text: "\n" + strings.Join(filtered, " · ")})
		}
	}

	parent.Children = append(parent.Children, para)
}

// findSiblingDescription looks for a <p> sibling after a heading.
func findSiblingDescription(heading *html.Node) string {
	// Look at next siblings for a <p>
	for sib := heading.NextSibling; sib != nil; sib = sib.NextSibling {
		if sib.Type == html.ElementNode {
			if sib.Data == "p" {
				text := strings.TrimSpace(textContent(sib))
				if len(text) > 30 && !strings.HasPrefix(text, "By ") {
					return truncateDescription(text, 100)
				}
			}
			// Also check for description in divs or spans
			if sib.Data == "div" || sib.Data == "span" {
				text := strings.TrimSpace(textContent(sib))
				if len(text) > 50 && len(text) < 300 && !strings.HasPrefix(text, "By ") {
					return truncateDescription(text, 100)
				}
			}
			// Stop if we hit another heading (next story card)
			if sib.Data == "h2" || sib.Data == "h3" || sib.Data == "h4" {
				break
			}
		}
	}

	// Also look in parent's next siblings (for nested structures)
	if heading.Parent != nil {
		for sib := heading.Parent.NextSibling; sib != nil; sib = sib.NextSibling {
			if sib.Type == html.ElementNode && sib.Data == "p" {
				text := strings.TrimSpace(textContent(sib))
				if len(text) > 30 && !strings.HasPrefix(text, "By ") {
					return truncateDescription(text, 100)
				}
			}
			// Stop early
			if sib.Type == html.ElementNode && (sib.Data == "h2" || sib.Data == "h3") {
				break
			}
		}
	}

	return ""
}

// findNearbyMetadata looks for author/date near a heading.
func findNearbyMetadata(heading *html.Node) (author, date string) {
	// Check parent and siblings for metadata elements
	searchArea := heading.Parent
	if searchArea == nil {
		return
	}

	// Look for time elements
	var findMeta func(*html.Node, int) bool
	findMeta = func(node *html.Node, depth int) bool {
		if depth > 3 {
			return false
		}
		if node.Type == html.ElementNode {
			if node.Data == "time" && date == "" {
				dt := getAttr(node, "datetime")
				if dt != "" {
					date = formatDate(dt)
				} else {
					date = strings.TrimSpace(textContent(node))
				}
			}
			class := strings.ToLower(getAttr(node, "class"))
			if (strings.Contains(class, "author") || strings.Contains(class, "byline")) && author == "" {
				text := strings.TrimSpace(textContent(node))
				text = strings.TrimPrefix(text, "By ")
				text = strings.TrimPrefix(text, "by ")
				if text != "" && len(text) < 100 {
					author = text
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findMeta(c, depth+1) {
				return true
			}
		}
		return false
	}
	findMeta(searchArea, 0)

	return author, date
}

// extractArticleEntry extracts structured data from an article element.
func extractArticleEntry(n *html.Node) articleData {
	var data articleData

	// Extract title: first h1/h2/h3 or prominent link
	data.Title, data.TitleHref = findArticleTitle(n)

	// Extract description: first substantial <p> that isn't metadata
	data.Description = findArticleDescription(n)

	// Extract author and date
	data.Author, data.Date = findArticleMetadata(n)

	// Extract source domain from title href
	if data.TitleHref != "" {
		data.Source = extractDomain(data.TitleHref)
	}

	return data
}

// findArticleTitle finds the title and link from an article.
func findArticleTitle(n *html.Node) (title, href string) {
	// First try h1, h2, h3 headings
	var findHeading func(*html.Node) bool
	findHeading = func(node *html.Node) bool {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "h1", "h2", "h3":
				text := strings.TrimSpace(textContent(node))
				if text != "" {
					title = text
					// Look for link inside the heading
					if link := findFirstLink(node); link != nil {
						href = getAttr(link, "href")
					}
					// Also check if parent is a link (common pattern: <a><h3>Title</h3></a>)
					if href == "" && node.Parent != nil &&
						node.Parent.Type == html.ElementNode && node.Parent.Data == "a" {
						href = getAttr(node.Parent, "href")
					}
					return true
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findHeading(c) {
				return true
			}
		}
		return false
	}

	if findHeading(n) && title != "" {
		return title, href
	}

	// Fallback: find first substantial link (>10 chars, likely title)
	var findProminentLink func(*html.Node) bool
	findProminentLink = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "a" {
			text := strings.TrimSpace(textContent(node))
			linkHref := getAttr(node, "href")
			// Skip navigation-style links, look for title-like links
			if len(text) > 10 && linkHref != "" && !strings.HasPrefix(linkHref, "#") {
				title = text
				href = linkHref
				return true
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findProminentLink(c) {
				return true
			}
		}
		return false
	}

	findProminentLink(n)
	return title, href
}

// findFirstLink finds the first <a> element in a subtree.
func findFirstLink(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "a" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstLink(c); found != nil {
			return found
		}
	}
	return nil
}

// findArticleDescription finds the first substantial paragraph.
func findArticleDescription(n *html.Node) string {
	var description string
	var find func(*html.Node) bool
	find = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "p" {
			text := strings.TrimSpace(textContent(node))
			// Skip short text (likely metadata) or text starting with "By" (author line)
			if len(text) > 50 && !strings.HasPrefix(text, "By ") && !strings.HasPrefix(text, "by ") {
				description = truncateDescription(text, 100)
				return true
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if find(c) {
				return true
			}
		}
		return false
	}
	find(n)
	return description
}

// findArticleMetadata extracts author and date from an article.
func findArticleMetadata(n *html.Node) (author, date string) {
	// Look for <time> element for date
	var findTime func(*html.Node) bool
	findTime = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "time" {
			// Prefer datetime attribute, fallback to text content
			dt := getAttr(node, "datetime")
			if dt != "" {
				date = formatDate(dt)
			} else {
				date = strings.TrimSpace(textContent(node))
			}
			return date != ""
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findTime(c) {
				return true
			}
		}
		return false
	}
	findTime(n)

	// Look for author: class containing "author" or "byline", or "By X" pattern
	var findAuthor func(*html.Node) bool
	findAuthor = func(node *html.Node) bool {
		if node.Type == html.ElementNode {
			class := strings.ToLower(getAttr(node, "class"))
			rel := strings.ToLower(getAttr(node, "rel"))

			// Check for author-related classes or rel attribute
			if strings.Contains(class, "author") || strings.Contains(class, "byline") ||
				strings.Contains(rel, "author") {
				text := strings.TrimSpace(textContent(node))
				// Clean up "By " prefix if present
				text = strings.TrimPrefix(text, "By ")
				text = strings.TrimPrefix(text, "by ")
				if text != "" && len(text) < 100 { // Sanity check length
					author = text
					return true
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findAuthor(c) {
				return true
			}
		}
		return false
	}
	findAuthor(n)

	return author, date
}

// truncateDescription truncates text to maxLen chars with ellipsis.
func truncateDescription(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Find last space before maxLen to avoid cutting words
	truncated := s[:maxLen]
	if idx := strings.LastIndex(truncated, " "); idx > maxLen/2 {
		truncated = truncated[:idx]
	}
	return truncated + "..."
}

// formatDate formats a datetime string to a human-readable format.
func formatDate(datetime string) string {
	// Handle ISO format like "2025-12-10T10:30:00Z"
	if len(datetime) >= 10 {
		dateStr := datetime[:10]
		parts := strings.Split(dateStr, "-")
		if len(parts) == 3 {
			months := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun",
				"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
			year := parts[0]
			monthNum := 0
			fmt.Sscanf(parts[1], "%d", &monthNum)
			day := strings.TrimPrefix(parts[2], "0")
			if monthNum >= 1 && monthNum <= 12 {
				return months[monthNum] + " " + day + ", " + year
			}
		}
	}
	return datetime
}

// extractDomain extracts the domain name from a URL.
func extractDomain(url string) string {
	// Skip protocol
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	// Get just the host
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	// Remove www. prefix
	url = strings.TrimPrefix(url, "www.")
	return url
}

// articleDataToListItem converts articleData to a NodeListItem.
func articleDataToListItem(data articleData) *Node {
	item := &Node{Type: NodeListItem}

	// Add title as link
	if data.TitleHref != "" {
		link := &Node{Type: NodeLink, Href: data.TitleHref}
		link.Children = append(link.Children, &Node{Type: NodeText, Text: data.Title})
		item.Children = append(item.Children, link)
	} else {
		item.Children = append(item.Children, &Node{Type: NodeStrong, Children: []*Node{
			{Type: NodeText, Text: data.Title},
		}})
	}

	// Add source domain if external link
	if data.Source != "" {
		item.Children = append(item.Children, &Node{Type: NodeText, Text: " (" + data.Source + ")"})
	}

	// Add description on new line
	if data.Description != "" {
		item.Children = append(item.Children, &Node{Type: NodeText, Text: "\n  " + data.Description})
	}

	// Add author and date on new line
	var metaParts []string
	if data.Author != "" {
		metaParts = append(metaParts, data.Author)
	}
	if data.Date != "" {
		metaParts = append(metaParts, data.Date)
	}
	if len(metaParts) > 0 {
		item.Children = append(item.Children, &Node{Type: NodeText, Text: "\n  " + strings.Join(metaParts, " · ")})
	}

	return item
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
	// Check if this node contains multiple articles (index page pattern)
	// This handles the case when <main> or similar is the content root
	if shouldExtractAsArticleList(n) {
		extractArticleList(n, parent)
		return
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			switch c.Data {
			case "h1":
				node := &Node{Type: NodeHeading1, Text: textContentDeduped(c), ID: getAttr(c, "id")}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h2":
				node := &Node{Type: NodeHeading2, Text: textContentDeduped(c), ID: getAttr(c, "id")}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h3", "h4", "h5", "h6":
				node := &Node{Type: NodeHeading3, Text: textContentDeduped(c), ID: getAttr(c, "id")}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "p":
				node := &Node{Type: NodeParagraph, ID: getAttr(c, "id")}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "blockquote":
				node := &Node{Type: NodeBlockquote, ID: getAttr(c, "id")}
				extractContentOnly(c, node)
				parent.Children = append(parent.Children, node)

			case "ul", "ol":
				node := &Node{Type: NodeList, ID: getAttr(c, "id")}
				extractList(c, node)
				parent.Children = append(parent.Children, node)

			case "pre":
				node := &Node{Type: NodeCodeBlock, Text: preformattedContent(c), ID: getAttr(c, "id")}
				parent.Children = append(parent.Children, node)

			case "details":
				// Disclosure widget - render as always-expanded
				// Extract summary as heading, rest as content
				extractDetails(c, parent)

			case "summary":
				// Summary outside of details context - treat as heading
				node := &Node{Type: NodeHeading3, Text: textContentDeduped(c), ID: getAttr(c, "id")}
				parent.Children = append(parent.Children, node)

			case "nav", "header", "footer", "aside", "menu":
				// Skip navigation elements (already extracted)
				continue

			case "style", "script", "noscript", "template":
				// Skip non-content elements (CSS, JS, etc.)
				continue

			case "img":
				// Extract image as a followable link with distinctive styling
				src := getAttr(c, "src")
				if src != "" {
					alt := getAttr(c, "alt")
					if alt == "" {
						// Use filename from src as fallback
						alt = extractFilename(src)
					}
					if alt == "" {
						alt = "image"
					}
					// Create as NodeImage wrapping a link (opens in Quick Look)
					node := &Node{Type: NodeParagraph}
					img := &Node{Type: NodeImage}
					link := &Node{Type: NodeLink, Href: src}
					link.Children = append(link.Children, &Node{Type: NodeText, Text: "[Image: " + alt + "]"})
					img.Children = append(img.Children, link)
					node.Children = append(node.Children, img)
					parent.Children = append(parent.Children, node)
				}

			case "article":
				// Add a separator before each article (except first)
				if len(parent.Children) > 0 {
					parent.Children = append(parent.Children, &Node{Type: NodeParagraph, Text: "───"})
				}
				extractContentOnly(c, parent)

			case "table":
				// Handle tables by extracting rows (or just content if tables disabled)
				if tablesEnabled() {
					extractTable(c, parent)
				} else {
					extractContentOnly(c, parent) // Fall back to plain text extraction
				}

			case "main", "section", "div", "span",
				"center", "nobr", "tbody", "b", "i", "u", "font":
				// Skip elements with role="navigation" (semantic navigation markers)
				if getAttr(c, "role") == "navigation" {
					continue
				}
				// Capture ID as anchor point before recursing
				if id := getAttr(c, "id"); id != "" {
					parent.Children = append(parent.Children, &Node{Type: NodeAnchor, ID: id})
				}
				// Check for article list (multiple <article> children = index page)
				if shouldExtractAsArticleList(c) {
					extractArticleList(c, parent)
				} else {
					extractContentOnly(c, parent)
				}

			case "tr":
				// Table row - extract cells as a single line
				extractTableRow(c, parent)

			case "td", "th":
				// Table cell - just extract content
				extractContentOnly(c, parent)

			case "a":
				// Capture ID or name as anchor point (for fragment navigation)
				anchorID := getAttr(c, "id")
				if anchorID == "" {
					anchorID = getAttr(c, "name") // Legacy anchor format
				}
				if anchorID != "" {
					parent.Children = append(parent.Children, &Node{Type: NodeAnchor, ID: anchorID})
				}

				href := getAttr(c, "href")

				// Check if this <a> wraps block content (common card pattern)
				// If so, extract structured content with link on title only
				if href != "" && containsBlockContent(c) {
					extractBlockLink(c, href, parent)
					continue
				}

				// Simple inline link - treat as a paragraph with a link
				text := strings.TrimSpace(textContent(c))

				// If no text, check for img alt text (for image links)
				if text == "" {
					if imgNode := findChildImg(c); imgNode != nil {
						altText := getAttr(imgNode, "alt")
						if altText != "" {
							text = "[" + altText + "]"
						}
					}
				}

				// If still no text, use the href as display text
				if text == "" && href != "" {
					text = href
				}

				if text != "" && href != "" {
					node := &Node{Type: NodeParagraph}
					link := &Node{Type: NodeLink, Href: href}
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
				// Process any LaTeX in the text
				if latexEnabled() && latex.ContainsLaTeX(text) {
					text = latex.ProcessText(text)
				}
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
			// If no text, use href as display text
			if text == "" && href != "" {
				text = href
			}
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

// extractTable extracts content from a table element.
func extractTable(n *html.Node, parent *Node) {
	// Skip navigation tables (Wikipedia navboxes, etc.)
	if isNavigationTable(n) {
		return
	}

	// Check if this looks like a HN-style table with "athing" class rows
	if isHNStyleTable(n) {
		extractHNTable(n, parent)
		return
	}

	// Check if this is a data table (has th elements or regular grid structure)
	if isDataTable(n) {
		buildDataTable(n, parent)
		return
	}

	// Fall back to row-by-row extraction for layout tables
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			switch c.Data {
			case "tbody", "thead", "tfoot":
				extractTable(c, parent)
			case "tr":
				extractTableRow(c, parent)
			}
		}
	}
}

// isDataTable checks if a table looks like a data table (vs layout table).
func isDataTable(n *html.Node) bool {
	hasHeader := false
	rowCount := 0
	cellCounts := make(map[int]int) // count rows with each cell count

	var check func(*html.Node)
	check = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "th":
				hasHeader = true
			case "tr":
				rowCount++
				cellCount := countCells(c)
				cellCounts[cellCount]++
			case "thead", "tbody", "tfoot":
				check(c)
			}
		}
	}
	check(n)

	// It's a data table if:
	// 1. Has th elements, OR
	// 2. Has multiple rows with consistent cell counts (grid-like structure)
	if hasHeader {
		return true
	}

	// Check for grid-like structure (most rows have same cell count)
	if rowCount >= 2 {
		for count, rows := range cellCounts {
			if count >= 2 && rows >= 2 {
				return true
			}
		}
	}

	return false
}

func countCells(tr *html.Node) int {
	count := 0
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			count++
		}
	}
	return count
}

// buildDataTable creates a proper table structure with NodeTable, NodeTableRow, NodeTableCell.
func buildDataTable(n *html.Node, parent *Node) {
	table := &Node{Type: NodeTable}

	var processRows func(*html.Node)
	processRows = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "thead", "tbody", "tfoot":
				processRows(c)
			case "tr":
				row := buildTableRow(c)
				if row != nil && len(row.Children) > 0 {
					table.Children = append(table.Children, row)
				}
			}
		}
	}
	processRows(n)

	// Only add the table if it has content
	if len(table.Children) > 0 {
		parent.Children = append(parent.Children, table)
	}
}

func buildTableRow(tr *html.Node) *Node {
	row := &Node{Type: NodeTableRow}

	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.Data == "td" || c.Data == "th" {
			cell := &Node{
				Type:     NodeTableCell,
				IsHeader: c.Data == "th",
			}
			// Extract cell content - could be text, links, etc.
			extractCellContent(c, cell)
			row.Children = append(row.Children, cell)
		}
	}

	return row
}

func extractCellContent(n *html.Node, cell *Node) {
	// First try to get just the text
	text := strings.TrimSpace(textContent(n))

	// Check for links in the cell
	var links []*html.Node
	var findLinks func(*html.Node)
	findLinks = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if c.Data == "a" {
					links = append(links, c)
				} else {
					findLinks(c)
				}
			}
		}
	}
	findLinks(n)

	// If there's a single link that covers most of the content, make the cell a link
	if len(links) == 1 {
		linkText := strings.TrimSpace(textContent(links[0]))
		href := getAttr(links[0], "href")
		// If no link text, use href as display text
		if linkText == "" && href != "" {
			linkText = href
		}
		displayText := text
		if displayText == "" {
			displayText = linkText
		}
		if linkText != "" && href != "" && (len(linkText) >= len(text)/2 || text == "") {
			linkNode := &Node{
				Type: NodeLink,
				Href: href,
			}
			linkNode.Children = append(linkNode.Children, &Node{
				Type: NodeText,
				Text: displayText, // Use full cell text for display
			})
			cell.Children = append(cell.Children, linkNode)
			return
		}
	}

	// Otherwise just add the text
	if text != "" {
		cell.Children = append(cell.Children, &Node{
			Type: NodeText,
			Text: text,
		})
	}
}

// isNavigationTable checks if a table is a navigation element (not content).
func isNavigationTable(n *html.Node) bool {
	// Tables with role="navigation" are navigation, not content
	return getAttr(n, "role") == "navigation"
}

// isHNStyleTable checks if a table contains HN-style "athing" rows.
func isHNStyleTable(n *html.Node) bool {
	var check func(*html.Node) bool
	check = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "tr" {
			if class := getAttr(node, "class"); strings.Contains(class, "athing") {
				return true
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if check(c) {
				return true
			}
		}
		return false
	}
	return check(n)
}

// extractHNTable extracts content from a Hacker News style table.
// HN uses: athing row (title), subtext row (metadata), spacer row
func extractHNTable(n *html.Node, parent *Node) {
	// Collect all rows - need to recurse deeply because HN has nested tables
	var rows []*html.Node
	var collectRows func(*html.Node)
	collectRows = func(node *html.Node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				switch c.Data {
				case "table", "tbody", "thead", "tfoot", "td", "th":
					// Recurse into these to find nested tables
					collectRows(c)
				case "tr":
					// Check if this row has athing class or is related to stories
					class := getAttr(c, "class")
					if strings.Contains(class, "athing") || strings.Contains(class, "spacer") {
						rows = append(rows, c)
					} else {
						// Could be a metadata row - check if previous was athing
						if len(rows) > 0 {
							prevClass := getAttr(rows[len(rows)-1], "class")
							if strings.Contains(prevClass, "athing") {
								rows = append(rows, c)
							}
						}
					}
					// Also recurse in case there's a nested table
					collectRows(c)
				}
			}
		}
	}
	collectRows(n)

	// Process rows - athing rows contain the story, next row has metadata
	for i := 0; i < len(rows); i++ {
		row := rows[i]
		class := getAttr(row, "class")

		// Skip spacer rows
		if strings.Contains(class, "spacer") {
			continue
		}

		// Look for submission rows (class contains "athing" and usually "submission")
		if strings.Contains(class, "athing") {
			// Extract the main story link from titleline
			title, href := extractHNTitleLine(row)
			if title == "" {
				continue
			}

			// Look for metadata in the next row
			var meta string
			if i+1 < len(rows) {
				nextRow := rows[i+1]
				nextClass := getAttr(nextRow, "class")
				if !strings.Contains(nextClass, "athing") && !strings.Contains(nextClass, "spacer") {
					meta = extractHNMetadata(nextRow)
					i++ // Skip the metadata row
				}
			}

			// Create a paragraph with the story link
			node := &Node{Type: NodeParagraph}
			link := &Node{Type: NodeLink, Href: href}
			link.Children = append(link.Children, &Node{Type: NodeText, Text: title})
			node.Children = append(node.Children, link)

			// Add metadata on same line if present
			if meta != "" {
				node.Children = append(node.Children, &Node{Type: NodeText, Text: " (" + meta + ")"})
			}

			parent.Children = append(parent.Children, node)
		}
	}
}

// extractHNTitleLine extracts the title and href from an HN title row.
func extractHNTitleLine(row *html.Node) (title, href string) {
	// Look for span.titleline > a (the main story link)
	var find func(*html.Node)
	find = func(n *html.Node) {
		if title != "" {
			return // Already found
		}
		if n.Type == html.ElementNode {
			if n.Data == "span" && strings.Contains(getAttr(n, "class"), "titleline") {
				// Found titleline, get the first link
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "a" {
						title = strings.TrimSpace(textContent(c))
						href = getAttr(c, "href")
						return
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)
	return
}

// extractHNMetadata extracts points and comments from an HN subtext row.
func extractHNMetadata(row *html.Node) string {
	// Look for score and comments
	var score, comments string

	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "span" && strings.Contains(getAttr(n, "class"), "score") {
				score = strings.TrimSpace(textContent(n))
			}
			if n.Data == "a" {
				text := strings.TrimSpace(textContent(n))
				if strings.Contains(text, "comment") || text == "discuss" {
					comments = text
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(row)

	// Build metadata string
	var parts []string
	if score != "" {
		parts = append(parts, score)
	}
	if comments != "" {
		parts = append(parts, comments)
	}
	return strings.Join(parts, ", ")
}

// extractTableRow extracts a table row as a single paragraph or list item.
func extractTableRow(n *html.Node, parent *Node) {
	// Collect text and links from all cells in this row
	var parts []string
	var rowLinks []*Node

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cellText := strings.TrimSpace(textContent(c))
			if cellText != "" {
				parts = append(parts, cellText)
			}
			// Also extract any links in the cell
			extractCellLinks(c, &rowLinks)
		}
	}

	// Skip empty rows or rows that are just whitespace
	if len(parts) == 0 && len(rowLinks) == 0 {
		return
	}

	// Create a paragraph for each row
	node := &Node{Type: NodeParagraph}

	// If there are links, create a structured output
	if len(rowLinks) > 0 {
		// Add links with separators
		for i, link := range rowLinks {
			if i > 0 {
				node.Children = append(node.Children, &Node{Type: NodeText, Text: " "})
			}
			node.Children = append(node.Children, link)
		}
	} else if len(parts) > 0 {
		// Just text - join with separator
		node.Children = append(node.Children, &Node{Type: NodeText, Text: strings.Join(parts, " · ")})
	}

	// Only add non-empty rows
	if len(node.Children) > 0 {
		parent.Children = append(parent.Children, node)
	}
}

// extractCellLinks extracts links from a table cell.
func extractCellLinks(n *html.Node, links *[]*Node) {
	if n.Type == html.ElementNode && n.Data == "a" {
		href := getAttr(n, "href")
		text := strings.TrimSpace(textContent(n))
		// If no text, use href as display text
		if text == "" && href != "" {
			text = href
		}
		if href != "" && text != "" {
			link := &Node{Type: NodeLink, Href: href}
			link.Children = append(link.Children, &Node{Type: NodeText, Text: text})
			*links = append(*links, link)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractCellLinks(c, links)
	}
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

// extractDetails handles <details> disclosure widgets.
// Renders them as always-expanded: summary becomes a heading, content follows.
func extractDetails(n *html.Node, parent *Node) {
	// Find and extract summary first
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "summary" {
			// Summary becomes a heading
			node := &Node{Type: NodeHeading3, Text: textContentDeduped(c), ID: getAttr(c, "id")}
			extractHeadingLink(c, node)
			parent.Children = append(parent.Children, node)
			break
		}
	}

	// Extract remaining content (everything except summary)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if c.Data == "summary" {
			continue // Already handled
		}
		// Handle specific element types that extractContentOnly expects as children
		switch c.Data {
		case "ul", "ol":
			node := &Node{Type: NodeList, ID: getAttr(c, "id")}
			extractList(c, node)
			parent.Children = append(parent.Children, node)
		case "p":
			node := &Node{Type: NodeParagraph, ID: getAttr(c, "id")}
			extractInline(c, node)
			parent.Children = append(parent.Children, node)
		case "div", "section":
			extractContentOnly(c, parent)
		default:
			extractContentOnly(c, parent)
		}
	}
}

func extractList(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" {
			// Strategy 1: Check if this li contains an article element
			if article := findChildArticle(c); article != nil {
				data := extractArticleEntry(article)
				if data.Title != "" {
					item := articleDataToListItem(data)
					parent.Children = append(parent.Children, item)
					continue
				}
			}

			// Strategy 2: Check for news-card pattern (prominent link + description)
			// Used by NYTimes in sections without <article> elements
			if data := extractNewsCard(c); data.Title != "" {
				item := articleDataToListItem(data)
				parent.Children = append(parent.Children, item)
				continue
			}

			// Standard extraction for regular list items
			item := &Node{Type: NodeListItem}
			extractInline(c, item)
			parent.Children = append(parent.Children, item)
		}
	}
}

// extractNewsCard extracts structured data from a news card pattern.
// This handles <li> elements that contain a prominent link + description
// but no <article> element (e.g., NYTimes Personal Tech section).
func extractNewsCard(li *html.Node) articleData {
	var data articleData

	// Find first substantial link (>20 chars) - this is the title
	var findTitleLink func(*html.Node, int) bool
	findTitleLink = func(node *html.Node, depth int) bool {
		if depth > 5 {
			return false
		}
		if node.Type == html.ElementNode && node.Data == "a" {
			href := getAttr(node, "href")
			text := strings.TrimSpace(textContent(node))
			// Skip short links (navigation) and anchor links
			if len(text) > 20 && href != "" && !strings.HasPrefix(href, "#") {
				data.Title = text
				data.TitleHref = href
				data.Source = extractDomain(href)
				return true
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if findTitleLink(c, depth+1) {
				return true
			}
		}
		return false
	}

	if !findTitleLink(li, 0) {
		return data // No title found
	}

	// Find description: <p> element with >30 chars that's different from title
	var findDescription func(*html.Node, int)
	findDescription = func(node *html.Node, depth int) {
		if depth > 5 || data.Description != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "p" {
			text := strings.TrimSpace(textContent(node))
			// Must be substantial and different from title
			if len(text) > 30 && !strings.HasPrefix(text, "By ") && text != data.Title {
				// Avoid picking up the title paragraph
				if !strings.Contains(data.Title, text) && !strings.Contains(text, data.Title) {
					data.Description = truncateDescription(text, 100)
					return
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			findDescription(c, depth+1)
		}
	}
	findDescription(li, 0)

	// Find author/date metadata
	data.Author, data.Date = findNearbyMetadata(li)

	return data
}

// findChildArticle finds a child article element within a node (shallow search).
func findChildArticle(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.Data == "article" {
				return c
			}
			// Check one level deeper (for <li><div><article> patterns)
			for gc := c.FirstChild; gc != nil; gc = gc.NextSibling {
				if gc.Type == html.ElementNode && gc.Data == "article" {
					return gc
				}
			}
		}
	}
	return nil
}

func extractInline(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			// Normalize HTML whitespace: collapse sequences to single space
			text := whitespaceRe.ReplaceAllString(c.Data, " ")
			if text != "" {
				// Process any LaTeX in the text
				if latexEnabled() && latex.ContainsLaTeX(text) {
					text = latex.ProcessText(text)
				}
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

			case "mark":
				node := &Node{Type: NodeMark}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "ins":
				node := &Node{Type: NodeMarkInsert}
				extractInline(c, node)
				parent.Children = append(parent.Children, node)

			case "code":
				parent.Children = append(parent.Children, &Node{Type: NodeCode, Text: textContent(c)})

			case "img":
				// Inline image - render with distinctive styling
				src := getAttr(c, "src")
				if src != "" {
					alt := getAttr(c, "alt")
					if alt == "" {
						alt = extractFilename(src)
					}
					if alt == "" {
						alt = "image"
					}
					img := &Node{Type: NodeImage}
					link := &Node{Type: NodeLink, Href: src}
					link.Children = append(link.Children, &Node{Type: NodeText, Text: "[Image: " + alt + "]"})
					img.Children = append(img.Children, link)
					parent.Children = append(parent.Children, img)
				}

			case "style", "script", "noscript", "template":
				// Skip non-content elements
				continue

			case "ul", "ol":
				// Nested list - extract properly with article detection
				list := &Node{Type: NodeList}
				extractList(c, list)
				parent.Children = append(parent.Children, list)

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
			// Normalize whitespace: collapse sequences to single space
			text := whitespaceRe.ReplaceAllString(n.Data, " ")
			sb.WriteString(text)
		}
		// Skip non-content elements
		if n.Type == html.ElementNode {
			switch n.Data {
			case "style", "script", "noscript", "template":
				return // Don't extract text from these elements
			}
		}
		// Handle MathML <math> elements - extract annotation with LaTeX
		if latexEnabled() && n.Type == html.ElementNode && n.Data == "math" {
			// Look for annotation with LaTeX encoding
			latexContent := extractMathMLLatex(n)
			if latexContent != "" {
				sb.WriteString(latex.ToUnicode(latexContent))
				return // Don't recurse into math element
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)

	// Normalize the final result - collapse any remaining whitespace sequences
	text := whitespaceRe.ReplaceAllString(sb.String(), " ")
	text = strings.TrimSpace(text)
	if latexEnabled() && latex.ContainsLaTeX(text) {
		text = latex.ProcessText(text)
	}
	return text
}

// preformattedContent extracts text from a node preserving whitespace (for <pre> blocks).
func preformattedContent(n *html.Node) string {
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
	// Just trim leading/trailing whitespace, preserve internal newlines
	return strings.TrimSpace(sb.String())
}

// extractMathMLLatex extracts LaTeX from MathML annotation elements.
func extractMathMLLatex(mathNode *html.Node) string {
	var findAnnotation func(*html.Node) string
	findAnnotation = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "annotation" {
			encoding := getAttr(n, "encoding")
			if strings.Contains(encoding, "latex") || strings.Contains(encoding, "tex") {
				// Get text content of annotation
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						return c.Data
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := findAnnotation(c); result != "" {
				return result
			}
		}
		return ""
	}
	return findAnnotation(mathNode)
}

// textContentDeduped extracts text and removes duplicated fragments.
// This handles cases like BBC where <h1><div>News</div><div>News</div></h1>
// should render as "News", not "NewsNews".
func textContentDeduped(n *html.Node) string {
	// Collect text from each direct child separately
	var parts []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text := strings.TrimSpace(textContent(c))
		if text != "" {
			parts = append(parts, text)
		}
	}

	// If no children, get text from node itself
	if len(parts) == 0 {
		return strings.TrimSpace(textContent(n))
	}

	// Deduplicate consecutive identical parts
	var result []string
	for i, p := range parts {
		if i == 0 || p != parts[i-1] {
			result = append(result, p)
		}
	}
	return strings.Join(result, " ")
}

// extractFilename extracts the filename from a URL path.
func extractFilename(url string) string {
	// Remove query string and fragment
	if idx := strings.IndexAny(url, "?#"); idx != -1 {
		url = url[:idx]
	}
	// Get last path component
	if idx := strings.LastIndex(url, "/"); idx != -1 {
		url = url[idx+1:]
	}
	// Remove extension for cleaner display
	if idx := strings.LastIndex(url, "."); idx != -1 {
		url = url[:idx]
	}
	return url
}

// findChildImg looks for an img element as a direct or nested child.
func findChildImg(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "img" {
			return c
		}
		// Check nested elements (e.g., <a><span><img></span></a>)
		if found := findChildImg(c); found != nil {
			return found
		}
	}
	return nil
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
// Also captures IDs from anchor elements if the heading doesn't have one.
func extractHeadingLink(n *html.Node, parent *Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "a" {
			// Capture ID from anchor if heading doesn't have one
			if parent.ID == "" {
				if id := getAttr(c, "id"); id != "" {
					parent.ID = id
				} else if name := getAttr(c, "name"); name != "" {
					parent.ID = name
				}
			}
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

// PlainTextForTranslation returns the document text with paragraph structure preserved.
// This is better for translation APIs which work best with natural paragraph breaks.
func (d *Document) PlainTextForTranslation() string {
	if d.Content == nil {
		return ""
	}
	var sb strings.Builder
	d.Content.appendStructuredText(&sb, false)
	return strings.TrimSpace(sb.String())
}

// appendStructuredText appends text with paragraph breaks preserved.
func (n *Node) appendStructuredText(sb *strings.Builder, inBlock bool) {
	// Add paragraph breaks for block-level elements
	isBlock := n.Type == NodeParagraph ||
		n.Type == NodeHeading1 || n.Type == NodeHeading2 || n.Type == NodeHeading3 ||
		n.Type == NodeBlockquote || n.Type == NodeListItem ||
		n.Type == NodeCodeBlock

	if isBlock && sb.Len() > 0 {
		sb.WriteString("\n\n")
	}

	if n.Text != "" {
		sb.WriteString(n.Text)
	}

	for _, child := range n.Children {
		child.appendStructuredText(sb, isBlock)
	}
}

// looksLikeNewsIndex detects news index pages by their structure:
// - Multiple NodeList children at top level (story groupings)
// - Many NodeAnchor children (section markers)
// - Lists contain link-first items (headlines)
func looksLikeNewsIndex(content *Node) bool {
	if content == nil {
		return false
	}

	var listCount, anchorCount, paragraphCount int
	var listsHaveLinks int

	for _, child := range content.Children {
		switch child.Type {
		case NodeList:
			listCount++
			// Check if list items start with links (news headline pattern)
			if listHasLinkFirstItems(child) {
				listsHaveLinks++
			}
		case NodeAnchor:
			anchorCount++
		case NodeParagraph:
			paragraphCount++
		}
	}

	// News index pattern: multiple lists (>=3) with link-first items,
	// many anchors (>=5), and the lists dominate the structure
	return listCount >= 3 && listsHaveLinks >= 3 && anchorCount >= 5
}

// listHasLinkFirstItems checks if a list has items where the first child is a link
func listHasLinkFirstItems(list *Node) bool {
	if list == nil || list.Type != NodeList {
		return false
	}
	linkFirstCount := 0
	for _, item := range list.Children {
		if item.Type == NodeListItem && len(item.Children) > 0 {
			if item.Children[0].Type == NodeLink {
				linkFirstCount++
			}
		}
	}
	// At least half the items should be link-first
	return linkFirstCount > 0 && linkFirstCount >= len(list.Children)/2
}

// normalizeNewsIndex consolidates news index pages:
// - Merges all story lists into one
// - Removes empty anchors
// - Filters out image caption paragraphs
func normalizeNewsIndex(content *Node) {
	if content == nil {
		return
	}

	// Collect all list items from all lists
	var allItems []*Node
	seenLinks := make(map[string]bool) // Deduplicate by URL

	for _, child := range content.Children {
		if child.Type == NodeList && listHasLinkFirstItems(child) {
			for _, item := range child.Children {
				if item.Type == NodeListItem && len(item.Children) > 0 {
					if link := item.Children[0]; link.Type == NodeLink {
						// Deduplicate by URL
						if link.Href != "" && !seenLinks[link.Href] {
							seenLinks[link.Href] = true
							allItems = append(allItems, item)
						}
					}
				}
			}
		}
	}

	// Filter and rebuild children
	var newChildren []*Node

	// Keep headings (section titles)
	for _, child := range content.Children {
		switch child.Type {
		case NodeHeading1, NodeHeading2, NodeHeading3:
			newChildren = append(newChildren, child)
		case NodeParagraph:
			// Keep non-caption paragraphs (those not starting with "[")
			text := extractNodeText(child)
			if !strings.HasPrefix(strings.TrimSpace(text), "[") && len(text) > 20 {
				newChildren = append(newChildren, child)
			}
		}
	}

	// Add consolidated list if we have items
	if len(allItems) > 0 {
		consolidatedList := &Node{
			Type:     NodeList,
			Children: allItems,
		}
		newChildren = append(newChildren, consolidatedList)
	}

	content.Children = newChildren
}

// extractNodeText recursively extracts text from a node
func extractNodeText(n *Node) string {
	if n == nil {
		return ""
	}
	text := n.Text
	for _, child := range n.Children {
		text += extractNodeText(child)
	}
	return text
}
