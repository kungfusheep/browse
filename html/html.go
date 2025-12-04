// Package html provides minimal HTML parsing for article content extraction.
package html

import (
	"io"
	"strings"

	"browse/latex"
	"golang.org/x/net/html"
)

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

	// Table cell properties
	IsHeader bool // true for th cells
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
	NodeTable      // A data table
	NodeTableRow   // A table row
	NodeTableCell  // A table cell (th or td)
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

	// Find the best content container using multiple strategies
	contentRoot := findContentRoot(doc, body)

	// Second pass: extract content (navigation will be skipped)
	extractContentOnly(contentRoot, result.Content)
	return result, nil
}

// findContentRoot finds the best element to extract content from.
func findContentRoot(doc, body *html.Node) *html.Node {
	// Strategy 1: Semantic elements (article, main)
	if article := findElement(doc, "article"); article != nil {
		return article
	}
	if main := findElement(doc, "main"); main != nil {
		return main
	}

	// Strategy 2: Common content div patterns (role="main", id="content", etc.)
	if content := findByAttribute(body, "role", "main"); content != nil {
		return content
	}
	if content := findByID(body, "content"); content != nil {
		return content
	}
	if content := findByID(body, "main-content"); content != nil {
		return content
	}
	if content := findByID(body, "main"); content != nil {
		return content
	}

	// Strategy 3: Find the div with the most paragraph content
	if best := findContentRichDiv(body); best != nil {
		return best
	}

	return body
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
				node := &Node{Type: NodeHeading1, Text: textContentDeduped(c)}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h2":
				node := &Node{Type: NodeHeading2, Text: textContentDeduped(c)}
				extractHeadingLink(c, node)
				parent.Children = append(parent.Children, node)

			case "h3", "h4", "h5", "h6":
				node := &Node{Type: NodeHeading3, Text: textContentDeduped(c)}
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
				extractContentOnly(c, parent)

			case "tr":
				// Table row - extract cells as a single line
				extractTableRow(c, parent)

			case "td", "th":
				// Table cell - just extract content
				extractContentOnly(c, parent)

			case "a":
				// Standalone link (not inside a paragraph) - treat as a paragraph with a link
				href := getAttr(c, "href")
				text := strings.TrimSpace(textContent(c))
				// If no text, use the href as display text
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

	// Process any remaining LaTeX in the text
	text := strings.TrimSpace(sb.String())
	if latexEnabled() && latex.ContainsLaTeX(text) {
		text = latex.ProcessText(text)
	}
	return text
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
