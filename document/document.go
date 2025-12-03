// Package document renders parsed HTML to the terminal canvas.
package document

import (
	"fmt"
	"strings"

	"browse/html"
	"browse/render"
)

const maxContentWidth = 80

// Link represents a clickable link in the document.
type Link struct {
	Href   string
	Text   string // link text for display
	X, Y   int    // position on canvas
	Length int    // display length for highlighting
}

// Input represents an interactive input field in the document.
type Input struct {
	Name       string // field name for form submission
	Value      string // current value
	X, Y       int    // position on canvas
	Width      int    // display width
	FormAction string // URL to submit to
	FormMethod string // GET or POST
}

// Heading represents a section heading for table of contents.
type Heading struct {
	Level  int    // 1, 2, or 3
	Number string // e.g., "1.2.3"
	Text   string // heading text
	Y      int    // line position in document
}

// Renderer converts HTML nodes to canvas output.
type Renderer struct {
	canvas       *render.Canvas
	contentWidth int
	leftMargin   int
	y            int
	scrollY      int // current scroll offset for computing absolute positions
	links        []Link
	inputs       []Input
	headings     []Heading
	paragraphs   []int // Y positions of paragraph-like elements for navigation

	// Current form context (for inputs)
	currentFormAction string
	currentFormMethod string

	// Section numbering
	h1Count int
	h2Count int
	h3Count int
}

// NewRenderer creates a renderer for the given canvas with default 80-char margins.
func NewRenderer(c *render.Canvas) *Renderer {
	return NewRendererWide(c, false)
}

// NewRendererWide creates a renderer with optional wide mode (no content width cap).
func NewRendererWide(c *render.Canvas, wideMode bool) *Renderer {
	canvasWidth := c.Width()

	// Content width is capped at maxContentWidth unless in wide mode
	contentWidth := canvasWidth - 4 // minimal margins
	if !wideMode && contentWidth > maxContentWidth {
		contentWidth = maxContentWidth
	}

	// Center the content
	leftMargin := (canvasWidth - contentWidth) / 2

	return &Renderer{
		canvas:       c,
		contentWidth: contentWidth,
		leftMargin:   leftMargin,
		y:            0,
	}
}

// Render draws the document to the canvas starting at the given y offset.
func (r *Renderer) Render(doc *html.Document, scrollY int) {
	r.canvas.Clear()
	r.y = -scrollY
	r.scrollY = scrollY // store for computing absolute positions
	r.links = nil       // reset links for this render
	r.inputs = nil      // reset inputs for this render
	r.headings = nil    // reset headings for this render
	r.paragraphs = nil  // reset paragraphs for this render

	// Reset form context
	r.currentFormAction = ""
	r.currentFormMethod = ""

	// Reset section counters
	r.h1Count = 0
	r.h2Count = 0
	r.h3Count = 0

	for _, child := range doc.Content.Children {
		r.renderNode(child)
	}
}

// Links returns the visible links from the last render.
func (r *Renderer) Links() []Link {
	return r.links
}

// Inputs returns the visible input fields from the last render.
func (r *Renderer) Inputs() []Input {
	return r.inputs
}

// Headings returns the section headings from the last render.
func (r *Renderer) Headings() []Heading {
	return r.headings
}

// Paragraphs returns the Y positions of paragraph-like elements for navigation.
func (r *Renderer) Paragraphs() []int {
	return r.paragraphs
}

// ContentHeight returns the total height needed for the document.
func (r *Renderer) ContentHeight(doc *html.Document) int {
	height := 0

	for _, child := range doc.Content.Children {
		height += r.nodeHeight(child, r.contentWidth)
	}

	return height
}

func (r *Renderer) nodeHeight(n *html.Node, textWidth int) int {
	switch n.Type {
	case html.NodeHeading1:
		return 4 // title + underline + blank
	case html.NodeHeading2:
		return 3 // title + blank
	case html.NodeHeading3:
		return 2 // title + blank
	case html.NodeParagraph:
		text := n.PlainText()
		lines := render.WrapText(text, textWidth)
		return len(lines) + 1
	case html.NodeBlockquote:
		h := 0
		for _, child := range n.Children {
			h += r.nodeHeight(child, textWidth-4)
		}
		return h + 1
	case html.NodeList:
		h := 0
		for _, item := range n.Children {
			text := item.PlainText()
			lines := render.WrapText(text, textWidth-4)
			h += len(lines)
		}
		return h + 1
	case html.NodeCodeBlock:
		lines := strings.Split(n.Text, "\n")
		return len(lines) + 2
	case html.NodeTable:
		// Table height = header separator + rows + top/bottom borders + spacing
		return len(n.Children) + 3
	default:
		return 1
	}
}

func (r *Renderer) renderNode(n *html.Node) {
	// Track paragraph position for navigation (absolute Y position)
	switch n.Type {
	case html.NodeHeading1, html.NodeHeading2, html.NodeHeading3,
		html.NodeParagraph, html.NodeBlockquote, html.NodeList,
		html.NodeCodeBlock, html.NodeTable:
		r.paragraphs = append(r.paragraphs, r.y+r.scrollY)
	}

	switch n.Type {
	case html.NodeHeading1:
		r.renderHeading1(n)
	case html.NodeHeading2:
		r.renderHeading2(n)
	case html.NodeHeading3:
		r.renderHeading3(n)
	case html.NodeParagraph:
		r.renderParagraph(n)
	case html.NodeBlockquote:
		r.renderBlockquote(n)
	case html.NodeList:
		r.renderList(n)
	case html.NodeCodeBlock:
		r.renderCodeBlock(n.Text)
	case html.NodeForm:
		r.renderForm(n)
	case html.NodeInput:
		r.renderInput(n)
	case html.NodeTable:
		r.renderTable(n)
	}
}

// sectionNumWidth is the fixed width for right-aligned section numbers
const sectionNumWidth = 8 // fits "1.1.1.  " with padding

func (r *Renderer) renderHeading1(n *html.Node) {
	r.h1Count++
	r.h2Count = 0
	r.h3Count = 0

	// Horizontal rule before major sections (except first)
	if r.h1Count > 1 {
		r.y++
		r.canvas.DrawHLine(r.leftMargin, r.y, r.contentWidth, render.DoubleBox.Horizontal, render.Style{Dim: true})
		r.y += 2
	}

	r.y++
	text := n.Text
	href := r.findHeadingLink(n)

	// Section number (right-aligned, dimmed) + SMALL CAPS title (bold)
	displayText := strings.ToUpper(text)
	sectionNum := fmt.Sprintf("%d.", r.h1Count)
	paddedNum := fmt.Sprintf("%*s  ", sectionNumWidth-2, sectionNum) // right-align with 2 space gap

	// Track heading for TOC (store absolute document position, not screen position)
	r.headings = append(r.headings, Heading{
		Level:  1,
		Number: sectionNum,
		Text:   text,
		Y:      r.y + r.scrollY,
	})

	// Draw section number in margin (dimmed), title at content start (bold)
	r.writeLine(r.leftMargin-sectionNumWidth, r.y, paddedNum, render.Style{Dim: true})
	r.writeLine(r.leftMargin, r.y, displayText, render.Style{Bold: true})

	if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
		r.links = append(r.links, Link{
			Href:   href,
			Text:   displayText,
			X:      r.leftMargin,
			Y:      r.y,
			Length: render.StringWidth(displayText),
		})
	}

	r.y++
	r.canvas.DrawHLine(r.leftMargin, r.y, render.StringWidth(displayText), render.DoubleBox.Horizontal, render.Style{})
	r.y += 2
}

func (r *Renderer) renderHeading2(n *html.Node) {
	r.h2Count++
	r.h3Count = 0

	r.y++
	text := n.Text
	href := r.findHeadingLink(n)

	// Section number (right-aligned, dimmed) + title (bold)
	var sectionNum string
	if r.h1Count > 0 {
		sectionNum = fmt.Sprintf("%d.%d", r.h1Count, r.h2Count)
	} else {
		sectionNum = fmt.Sprintf("%d.", r.h2Count)
	}
	paddedNum := fmt.Sprintf("%*s  ", sectionNumWidth-2, sectionNum)

	// Track heading for TOC (store absolute document position, not screen position)
	r.headings = append(r.headings, Heading{
		Level:  2,
		Number: sectionNum,
		Text:   text,
		Y:      r.y + r.scrollY,
	})

	// Draw section number in margin (dimmed), title at content start (bold)
	r.writeLine(r.leftMargin-sectionNumWidth, r.y, paddedNum, render.Style{Dim: true})
	r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true})

	if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
		r.links = append(r.links, Link{
			Href:   href,
			Text:   text,
			X:      r.leftMargin,
			Y:      r.y,
			Length: render.StringWidth(text),
		})
	}

	// Single line under h2 (under title only)
	r.y++
	r.canvas.DrawHLine(r.leftMargin, r.y, render.StringWidth(text), render.SingleBox.Horizontal, render.Style{Dim: true})
	r.y += 2
}

func (r *Renderer) renderHeading3(n *html.Node) {
	r.h3Count++

	text := n.Text
	href := r.findHeadingLink(n)

	// Section number (right-aligned, dimmed) + title (bold, underlined)
	var sectionNum string
	if r.h1Count > 0 && r.h2Count > 0 {
		sectionNum = fmt.Sprintf("%d.%d.%d", r.h1Count, r.h2Count, r.h3Count)
	} else if r.h2Count > 0 {
		sectionNum = fmt.Sprintf("%d.%d", r.h2Count, r.h3Count)
	} else {
		sectionNum = ""
	}

	// Track heading for TOC (only if it has a section number)
	// Store absolute document position, not screen position
	if sectionNum != "" {
		r.headings = append(r.headings, Heading{
			Level:  3,
			Number: sectionNum,
			Text:   text,
			Y:      r.y + r.scrollY,
		})
	}

	// Draw section number in margin (dimmed), title at content start (bold, underlined)
	if sectionNum != "" {
		paddedNum := fmt.Sprintf("%*s  ", sectionNumWidth-2, sectionNum)
		r.writeLine(r.leftMargin-sectionNumWidth, r.y, paddedNum, render.Style{Dim: true})
		r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true, Underline: true})

		if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
			r.links = append(r.links, Link{
				Href:   href,
				Text:   text,
				X:      r.leftMargin,
				Y:      r.y,
				Length: render.StringWidth(text),
			})
		}
	} else {
		// No section number - just render title
		r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true, Underline: true})

		if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
			r.links = append(r.links, Link{
				Href:   href,
				Text:   text,
				X:      r.leftMargin,
				Y:      r.y,
				Length: render.StringWidth(text),
			})
		}
	}

	r.y += 2
}

func (r *Renderer) findHeadingLink(n *html.Node) string {
	for _, child := range n.Children {
		if child.Type == html.NodeLink {
			return child.Href
		}
	}
	return ""
}

func (r *Renderer) renderParagraph(n *html.Node) {
	// Extract text spans with link info
	spans := r.extractSpans(n)

	// Wrap into lines
	lines := r.wrapSpans(spans, r.contentWidth)

	// Render each line
	for _, line := range lines {
		x := r.leftMargin
		for _, span := range line {
			if r.y >= 0 && r.y < r.canvas.Height() {
				r.canvas.WriteString(x, r.y, span.Text, span.Style)

				// Track links
				if span.Href != "" {
					r.links = append(r.links, Link{
						Href:   span.Href,
						Text:   span.Text,
						X:      x,
						Y:      r.y,
						Length: render.StringWidth(span.Text),
					})
				}
			}
			x += render.StringWidth(span.Text)
		}
		r.y++
	}
	r.y++
}

type textSpan struct {
	Text  string
	Style render.Style
	Href  string
}

func (r *Renderer) extractSpans(n *html.Node) []textSpan {
	var spans []textSpan
	r.extractSpansRecursive(n, render.Style{}, "", &spans)
	return spans
}

func (r *Renderer) extractSpansRecursive(n *html.Node, style render.Style, href string, spans *[]textSpan) {
	for _, child := range n.Children {
		switch child.Type {
		case html.NodeText:
			if child.Text != "" {
				*spans = append(*spans, textSpan{Text: child.Text, Style: style, Href: href})
			}
		case html.NodeStrong:
			boldStyle := style
			boldStyle.Bold = true
			r.extractSpansRecursive(child, boldStyle, href, spans)
		case html.NodeEmphasis:
			emStyle := style
			emStyle.Underline = true
			r.extractSpansRecursive(child, emStyle, href, spans)
		case html.NodeCode:
			codeStyle := style
			codeStyle.Dim = true
			*spans = append(*spans, textSpan{Text: child.Text, Style: codeStyle, Href: href})
		case html.NodeLink:
			linkStyle := style
			linkStyle.Underline = true
			r.extractSpansRecursive(child, linkStyle, child.Href, spans)
		default:
			r.extractSpansRecursive(child, style, href, spans)
		}
	}
}

func (r *Renderer) wrapSpans(spans []textSpan, width int) [][]textSpan {
	// Build a character-to-span index so we can map wrapped text back to original spans
	var fullText strings.Builder
	type charInfo struct {
		style render.Style
		href  string
	}
	var charMap []charInfo

	for _, span := range spans {
		for _, ch := range span.Text {
			fullText.WriteRune(ch)
			charMap = append(charMap, charInfo{style: span.Style, href: span.Href})
		}
	}

	lines := render.WrapAndJustify(fullText.String(), width)
	result := make([][]textSpan, len(lines))

	// Track position in original text (excluding justification spaces)
	origPos := 0
	origText := fullText.String()
	origRunes := []rune(origText)

	for i, line := range lines {
		var lineSpans []textSpan
		lineRunes := []rune(line)

		j := 0
		for j < len(lineRunes) {
			// Skip leading spaces added by justification at start of line
			// (original text shouldn't start with spaces after wrapping)

			// Find matching position in original text
			for origPos < len(origRunes) && j < len(lineRunes) {
				if lineRunes[j] == origRunes[origPos] {
					// Match - start a span
					info := charMap[origPos]
					spanStart := j

					// Collect consecutive chars with same style/href
					for j < len(lineRunes) && origPos < len(origRunes) &&
						lineRunes[j] == origRunes[origPos] &&
						charMap[origPos].href == info.href &&
						charMap[origPos].style == info.style {
						j++
						origPos++
					}

					lineSpans = append(lineSpans, textSpan{
						Text:  string(lineRunes[spanStart:j]),
						Style: info.style,
						Href:  info.href,
					})
				} else if lineRunes[j] == ' ' {
					// Justification space - add as unstyled
					spanStart := j
					for j < len(lineRunes) && lineRunes[j] == ' ' &&
						(origPos >= len(origRunes) || lineRunes[j] != origRunes[origPos]) {
						j++
					}
					lineSpans = append(lineSpans, textSpan{
						Text:  string(lineRunes[spanStart:j]),
						Style: render.Style{},
						Href:  "",
					})
				} else {
					// Skip non-matching chars in original (consumed whitespace during wrap)
					origPos++
				}
			}

			// Handle any remaining chars in line (extra justification spaces at end)
			if j < len(lineRunes) {
				lineSpans = append(lineSpans, textSpan{
					Text:  string(lineRunes[j:]),
					Style: render.Style{},
					Href:  "",
				})
				break
			}
		}

		result[i] = lineSpans
	}

	return result
}

func (r *Renderer) renderBlockquote(n *html.Node) {
	startY := r.y

	for _, child := range n.Children {
		if child.Type == html.NodeParagraph {
			text := child.PlainText()
			lines := render.WrapText(text, r.contentWidth-4)
			for _, line := range lines {
				r.writeLine(r.leftMargin+4, r.y, line, render.Style{Dim: true})
				r.y++
			}
		}
	}

	for y := startY; y < r.y; y++ {
		if y >= 0 && y < r.canvas.Height() {
			r.canvas.Set(r.leftMargin, y, '│', render.Style{Dim: true})
		}
	}

	r.y++
}

func (r *Renderer) renderList(n *html.Node) {
	for _, item := range n.Children {
		// Build spans from list item (preserving links)
		var spans []textSpan
		r.extractSpansRecursive(item, render.Style{}, "", &spans)

		// Wrap the spans (not justified for lists)
		lines := r.wrapSpansNoJustify(spans, r.contentWidth-4)

		for i, lineSpans := range lines {
			if i == 0 {
				r.writeLine(r.leftMargin, r.y, "•", render.Style{})
			}
			x := r.leftMargin + 2
			for _, span := range lineSpans {
				// Track links
				if span.Href != "" {
					r.links = append(r.links, Link{
						Href:   span.Href,
						Text:   span.Text,
						X:      x,
						Y:      r.y,
						Length: render.StringWidth(span.Text),
					})
				}
				r.writeLine(x, r.y, span.Text, span.Style)
				x += render.StringWidth(span.Text)
			}
			r.y++
		}
	}
	r.y++
}

// wrapSpansNoJustify wraps spans without justification (for lists, etc.)
func (r *Renderer) wrapSpansNoJustify(spans []textSpan, width int) [][]textSpan {
	// Build full text and character map
	var fullText strings.Builder
	type charInfo struct {
		style render.Style
		href  string
	}
	var charMap []charInfo

	for _, span := range spans {
		for _, ch := range span.Text {
			fullText.WriteRune(ch)
			charMap = append(charMap, charInfo{style: span.Style, href: span.Href})
		}
	}

	// Wrap without justification
	lines := render.WrapText(fullText.String(), width)
	result := make([][]textSpan, len(lines))

	origPos := 0
	origRunes := []rune(fullText.String())

	for i, line := range lines {
		var lineSpans []textSpan
		lineRunes := []rune(line)

		j := 0
		for j < len(lineRunes) && origPos < len(origRunes) {
			if lineRunes[j] == origRunes[origPos] {
				info := charMap[origPos]
				spanStart := j

				for j < len(lineRunes) && origPos < len(origRunes) &&
					lineRunes[j] == origRunes[origPos] &&
					charMap[origPos].href == info.href &&
					charMap[origPos].style == info.style {
					j++
					origPos++
				}

				lineSpans = append(lineSpans, textSpan{
					Text:  string(lineRunes[spanStart:j]),
					Style: info.style,
					Href:  info.href,
				})
			} else if lineRunes[j] == ' ' {
				spanStart := j
				for j < len(lineRunes) && lineRunes[j] == ' ' {
					j++
				}
				lineSpans = append(lineSpans, textSpan{
					Text:  string(lineRunes[spanStart:j]),
					Style: render.Style{},
				})
			} else {
				origPos++
			}
		}

		result[i] = lineSpans
	}

	return result
}

func (r *Renderer) renderCodeBlock(text string) {
	lines := strings.Split(text, "\n")

	r.canvas.DrawHLine(r.leftMargin, r.y, r.contentWidth, render.SingleBox.Horizontal, render.Style{Dim: true})
	r.y++

	for _, line := range lines {
		if len(line) > r.contentWidth {
			line = line[:r.contentWidth]
		}
		r.writeLine(r.leftMargin, r.y, line, render.Style{Dim: true})
		r.y++
	}

	r.canvas.DrawHLine(r.leftMargin, r.y, r.contentWidth, render.SingleBox.Horizontal, render.Style{Dim: true})
	r.y++
}

func (r *Renderer) renderTable(n *html.Node) {
	if len(n.Children) == 0 {
		return
	}

	// Calculate column widths based on content
	colWidths := r.calculateColumnWidths(n)
	if len(colWidths) == 0 {
		return
	}

	// Calculate total table width
	tableWidth := 1 // left border
	for _, w := range colWidths {
		tableWidth += w + 3 // content + padding + separator
	}

	// Ensure table fits in content width
	if tableWidth > r.contentWidth {
		r.shrinkColumnsToFit(colWidths, r.contentWidth)
		tableWidth = 1
		for _, w := range colWidths {
			tableWidth += w + 3
		}
	}

	// Center the table if it's narrower than content width
	tableX := r.leftMargin
	if tableWidth < r.contentWidth {
		tableX = r.leftMargin + (r.contentWidth-tableWidth)/2
	}

	// Find first header row (if any)
	headerRowIdx := -1
	for i, row := range n.Children {
		if row.Type == html.NodeTableRow && len(row.Children) > 0 {
			if row.Children[0].IsHeader {
				headerRowIdx = i
				break
			}
		}
	}

	// Draw top border
	r.drawTableBorder(tableX, colWidths, '┌', '┬', '┐')
	r.y++

	// Draw rows
	for i, row := range n.Children {
		if row.Type != html.NodeTableRow {
			continue
		}

		// Skip empty rows
		if isEmptyRow(row) {
			continue
		}

		r.drawTableRow(tableX, row, colWidths)
		r.y++

		// Draw separator after header row
		if i == headerRowIdx {
			r.drawTableBorder(tableX, colWidths, '├', '┼', '┤')
			r.y++
		}
	}

	// Draw bottom border
	r.drawTableBorder(tableX, colWidths, '└', '┴', '┘')
	r.y++
	r.y++ // Extra spacing after table
}

func (r *Renderer) calculateColumnWidths(table *html.Node) []int {
	var maxCols int
	for _, row := range table.Children {
		if row.Type == html.NodeTableRow && len(row.Children) > maxCols {
			maxCols = len(row.Children)
		}
	}

	if maxCols == 0 {
		return nil
	}

	widths := make([]int, maxCols)

	// Find max width for each column
	for _, row := range table.Children {
		if row.Type != html.NodeTableRow {
			continue
		}
		for i, cell := range row.Children {
			if i >= maxCols {
				break
			}
			text := cell.PlainText()
			textWidth := render.StringWidth(text)
			if textWidth > widths[i] {
				widths[i] = textWidth
			}
		}
	}

	// Set minimum widths and cap maximum
	for i := range widths {
		if widths[i] < 3 {
			widths[i] = 3
		}
		if widths[i] > 40 {
			widths[i] = 40
		}
	}

	return widths
}

func (r *Renderer) shrinkColumnsToFit(widths []int, maxWidth int) {
	for {
		total := 1
		for _, w := range widths {
			total += w + 3
		}
		if total <= maxWidth {
			break
		}

		// Find widest column and shrink it
		maxIdx := 0
		for i, w := range widths {
			if w > widths[maxIdx] {
				maxIdx = i
			}
		}
		if widths[maxIdx] <= 3 {
			break // Can't shrink further
		}
		widths[maxIdx]--
	}
}

func (r *Renderer) drawTableBorder(x int, colWidths []int, left, mid, right rune) {
	style := render.Style{Dim: true}
	pos := x

	r.canvas.Set(pos, r.y, left, style)
	pos++

	for i, w := range colWidths {
		for j := 0; j < w+2; j++ { // width + padding
			r.canvas.Set(pos, r.y, '─', style)
			pos++
		}
		if i < len(colWidths)-1 {
			r.canvas.Set(pos, r.y, mid, style)
		} else {
			r.canvas.Set(pos, r.y, right, style)
		}
		pos++
	}
}

// isEmptyRow checks if a table row has no visible content.
func isEmptyRow(row *html.Node) bool {
	for _, cell := range row.Children {
		if strings.TrimSpace(cell.PlainText()) != "" {
			return false
		}
	}
	return true
}

// truncateToWidth truncates a string to fit within maxWidth visual columns.
func truncateToWidth(s string, maxWidth int) string {
	width := 0
	for i, r := range s {
		rw := render.UnicodeWidth(r)
		if width+rw > maxWidth {
			return s[:i]
		}
		width += rw
	}
	return s
}

func (r *Renderer) drawTableRow(x int, row *html.Node, colWidths []int) {
	style := render.Style{Dim: true}
	pos := x

	// Left border
	r.canvas.Set(pos, r.y, '│', style)
	pos++

	for i, width := range colWidths {
		// Get cell content
		var cellText string
		var isHeader bool
		var cellHref string
		if i < len(row.Children) {
			cell := row.Children[i]
			cellText = cell.PlainText()
			isHeader = cell.IsHeader
			// Check if cell contains a link
			for _, child := range cell.Children {
				if child.Type == html.NodeLink {
					cellHref = child.Href
					break
				}
			}
		}

		// Truncate if needed (accounting for Unicode width)
		cellWidth := render.StringWidth(cellText)
		if cellWidth > width {
			cellText = truncateToWidth(cellText, width-1) + "…"
			cellWidth = render.StringWidth(cellText)
		}

		// Pad to width
		padding := width - cellWidth
		leftPad := padding / 2
		rightPad := padding - leftPad

		// Write padding + content
		r.canvas.Set(pos, r.y, ' ', style)
		pos++

		for j := 0; j < leftPad; j++ {
			r.canvas.Set(pos, r.y, ' ', style)
			pos++
		}

		// Choose style based on header/link status
		contentStyle := render.Style{}
		if isHeader {
			contentStyle.Bold = true
		}
		if cellHref != "" {
			contentStyle.Underline = true
			// Track link position
			r.links = append(r.links, Link{
				Href:   cellHref,
				Text:   cellText,
				X:      pos,
				Y:      r.y + r.scrollY,
				Length: len(cellText),
			})
		}

		// Write cell content
		for _, ch := range cellText {
			r.canvas.Set(pos, r.y, ch, contentStyle)
			pos++
		}

		for j := 0; j < rightPad; j++ {
			r.canvas.Set(pos, r.y, ' ', style)
			pos++
		}

		r.canvas.Set(pos, r.y, ' ', style)
		pos++

		// Column separator
		r.canvas.Set(pos, r.y, '│', style)
		pos++
	}
}

func (r *Renderer) renderForm(n *html.Node) {
	// Save form context for child inputs
	prevAction := r.currentFormAction
	prevMethod := r.currentFormMethod
	r.currentFormAction = n.FormAction
	r.currentFormMethod = n.FormMethod

	// Render children
	for _, child := range n.Children {
		r.renderNode(child)
	}

	// Restore context
	r.currentFormAction = prevAction
	r.currentFormMethod = prevMethod
}

func (r *Renderer) renderInput(n *html.Node) {
	if r.y < 0 || r.y >= r.canvas.Height() {
		r.y++
		return
	}

	if n.InputType == "submit" {
		// Render submit button as [ Button Label ]
		label := n.InputValue
		if label == "" {
			label = "Submit"
		}
		text := "[ " + label + " ]"
		r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true, Reverse: true})
		r.y++
	} else {
		// Render text input as: [placeholder/title............]
		inputWidth := 40
		if inputWidth > r.contentWidth-4 {
			inputWidth = r.contentWidth - 4
		}

		label := n.Text
		if label == "" {
			label = n.InputName
		}

		// Draw input box
		display := label
		if len(display) > inputWidth-2 {
			display = display[:inputWidth-2]
		}
		padding := inputWidth - 2 - len(display)
		if padding < 0 {
			padding = 0
		}

		text := "[" + display + strings.Repeat("_", padding) + "]"
		r.writeLine(r.leftMargin, r.y, text, render.Style{Underline: true})

		// Track input for interaction
		r.inputs = append(r.inputs, Input{
			Name:       n.InputName,
			Value:      n.InputValue,
			X:          r.leftMargin,
			Y:          r.y,
			Width:      inputWidth,
			FormAction: r.currentFormAction,
			FormMethod: r.currentFormMethod,
		})
		r.y++
	}
}

func (r *Renderer) writeLine(x, y int, text string, style render.Style) {
	if y < 0 || y >= r.canvas.Height() {
		return
	}
	r.canvas.WriteString(x, y, text, style)
}

// GenerateLabels creates short jump labels for the given number of links.
// Uses home row keys for speed: a, s, d, f, g, h, j, k, l
// Then combinations: aa, as, ad...
func GenerateLabels(count int) []string {
	keys := []byte("asdfghjkl")
	labels := make([]string, 0, count)

	// If we can fit in single chars, use them
	if count <= len(keys) {
		for _, k := range keys {
			if len(labels) >= count {
				return labels
			}
			labels = append(labels, string(k))
		}
		return labels
	}

	// Otherwise ALL labels are two characters (no mixing)
	for _, k1 := range keys {
		for _, k2 := range keys {
			if len(labels) >= count {
				return labels
			}
			labels = append(labels, string([]byte{k1, k2}))
		}
	}

	return labels
}

// RenderLinkLabels overlays jump labels on visible links.
func (r *Renderer) RenderLinkLabels(labels []string) {
	for i, link := range r.links {
		if i >= len(labels) {
			break
		}
		if link.Y < 0 || link.Y >= r.canvas.Height() {
			continue
		}

		label := labels[i]
		// Draw label with reverse video for visibility
		for j, ch := range label {
			if link.X+j < r.canvas.Width() {
				r.canvas.Set(link.X+j, link.Y, ch, render.Style{Reverse: true, Bold: true})
			}
		}
	}
}

// RenderInputLabels overlays jump labels on visible inputs.
func (r *Renderer) RenderInputLabels(labels []string) {
	for i, input := range r.inputs {
		if i >= len(labels) {
			break
		}
		if input.Y < 0 || input.Y >= r.canvas.Height() {
			continue
		}

		label := labels[i]
		// Draw label with reverse video for visibility
		for j, ch := range label {
			if input.X+j < r.canvas.Width() {
				r.canvas.Set(input.X+j, input.Y, ch, render.Style{Reverse: true, Bold: true})
			}
		}
	}
}

// RenderTOC draws a table of contents overlay.
// Returns jump labels for each heading so the caller can handle selection.
func (r *Renderer) RenderTOC(labels []string) {
	if len(r.headings) == 0 {
		return
	}

	height := r.canvas.Height()
	width := r.canvas.Width()

	// Calculate TOC box dimensions
	tocWidth := 60
	if tocWidth > width-4 {
		tocWidth = width - 4
	}
	tocHeight := len(r.headings) + 4 // headings + title + borders + padding
	if tocHeight > height-4 {
		tocHeight = height - 4
	}

	// Center the TOC box
	startX := (width - tocWidth) / 2
	startY := (height - tocHeight) / 2

	// Draw box background (clear area)
	for y := startY; y < startY+tocHeight; y++ {
		for x := startX; x < startX+tocWidth; x++ {
			r.canvas.Set(x, y, ' ', render.Style{})
		}
	}

	// Draw border
	r.canvas.DrawBox(startX, startY, tocWidth, tocHeight, render.DoubleBox, render.Style{})

	// Title
	title := " Table of Contents "
	titleX := startX + (tocWidth-len(title))/2
	r.canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

	// Draw headings with labels
	y := startY + 2
	maxHeadings := tocHeight - 4
	for i, heading := range r.headings {
		if i >= maxHeadings || i >= len(labels) {
			break
		}

		// Indent based on level
		indent := (heading.Level - 1) * 2
		x := startX + 2 + indent

		// Format: [label] number  text
		label := labels[i]
		text := heading.Text
		maxTextWidth := tocWidth - 8 - indent - len(label)
		if len(text) > maxTextWidth {
			text = text[:maxTextWidth-3] + "..."
		}

		// Draw label
		for j, ch := range label {
			r.canvas.Set(x+j, y, ch, render.Style{Reverse: true, Bold: true})
		}

		// Draw section number and text
		line := fmt.Sprintf(" %s  %s", heading.Number, text)
		r.canvas.WriteString(x+len(label), y, line, render.Style{})

		y++
	}

	// Footer hint
	hint := " Press label to jump, ESC to close "
	hintX := startX + (tocWidth-len(hint))/2
	r.canvas.WriteString(hintX, startY+tocHeight-1, hint, render.Style{Dim: true})
}

// RenderLinkIndex draws a link index overlay showing all page links.
func (r *Renderer) RenderLinkIndex(labels []string, scrollOffset int) {
	if len(r.links) == 0 {
		return
	}

	height := r.canvas.Height()
	width := r.canvas.Width()

	// Calculate box dimensions
	boxWidth := 70
	if boxWidth > width-4 {
		boxWidth = width - 4
	}
	maxVisible := height - 8
	boxHeight := len(r.links) + 4
	if boxHeight > height-4 {
		boxHeight = height - 4
	}

	// Center the box
	startX := (width - boxWidth) / 2
	startY := (height - boxHeight) / 2

	// Draw box background (clear area)
	for y := startY; y < startY+boxHeight; y++ {
		for x := startX; x < startX+boxWidth; x++ {
			r.canvas.Set(x, y, ' ', render.Style{})
		}
	}

	// Draw border
	r.canvas.DrawBox(startX, startY, boxWidth, boxHeight, render.DoubleBox, render.Style{})

	// Title
	title := fmt.Sprintf(" Page Links (%d) ", len(r.links))
	titleX := startX + (boxWidth-len(title))/2
	r.canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

	// Draw links with labels
	y := startY + 2
	visibleCount := boxHeight - 4
	for i := scrollOffset; i < len(r.links) && i < scrollOffset+visibleCount; i++ {
		if i >= len(labels) {
			break
		}

		link := r.links[i]
		x := startX + 2

		// Format: [label] text → href
		label := labels[i]
		text := link.Text
		if text == "" {
			text = "(no text)"
		}

		// Truncate text and href to fit
		maxTextWidth := (boxWidth - 10 - len(label)) / 2
		if len(text) > maxTextWidth {
			text = text[:maxTextWidth-3] + "..."
		}

		href := link.Href
		maxHrefWidth := boxWidth - 8 - len(label) - render.StringWidth(text)
		if len(href) > maxHrefWidth {
			href = href[:maxHrefWidth-3] + "..."
		}

		// Draw label (highlighted)
		for j, ch := range label {
			r.canvas.Set(x+j, y, ch, render.Style{Reverse: true, Bold: true})
		}

		// Draw text and href
		line := fmt.Sprintf(" %s ", text)
		r.canvas.WriteString(x+len(label), y, line, render.Style{})
		r.canvas.WriteString(x+len(label)+render.StringWidth(line), y, href, render.Style{Dim: true})

		y++
	}

	// Scroll indicators
	if scrollOffset > 0 {
		r.canvas.WriteString(startX+boxWidth-4, startY+2, "↑", render.Style{Dim: true})
	}
	if scrollOffset+visibleCount < len(r.links) {
		r.canvas.WriteString(startX+boxWidth-4, startY+boxHeight-3, "↓", render.Style{Dim: true})
	}

	// Footer hint
	hint := " Press label to go, j/k scroll, ESC close "
	if maxVisible >= len(r.links) {
		hint = " Press label to go, ESC to close "
	}
	hintX := startX + (boxWidth-len(hint))/2
	r.canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
}

// NavLink represents a navigation link for the overlay.
type NavLink struct {
	Section string // section name (e.g., "Header", "Navigation")
	Text    string // link text
	Href    string // URL
}

// RenderNavigation draws a navigation overlay with all nav links.
// scrollOffset controls which portion of the list is visible.
func (r *Renderer) RenderNavigation(navSections []*html.Node, labels []string, scrollOffset int) []NavLink {
	if len(navSections) == 0 {
		return nil
	}

	// Flatten all nav links
	var allLinks []NavLink
	for _, section := range navSections {
		sectionName := section.Text
		for _, child := range section.Children {
			if child.Type == html.NodeLink && child.Text != "" && child.Href != "" {
				allLinks = append(allLinks, NavLink{
					Section: sectionName,
					Text:    child.Text,
					Href:    child.Href,
				})
			}
		}
	}

	if len(allLinks) == 0 {
		return nil
	}

	height := r.canvas.Height()
	width := r.canvas.Width()

	// Calculate nav box dimensions - fixed height that fits screen
	navWidth := 60
	if navWidth > width-4 {
		navWidth = width - 4
	}
	navHeight := height - 6 // Leave some margin
	if navHeight < 10 {
		navHeight = 10
	}

	// Center the nav box
	startX := (width - navWidth) / 2
	startY := (height - navHeight) / 2

	// Draw box background (clear area)
	for y := startY; y < startY+navHeight; y++ {
		for x := startX; x < startX+navWidth; x++ {
			r.canvas.Set(x, y, ' ', render.Style{})
		}
	}

	// Draw border
	r.canvas.DrawBox(startX, startY, navWidth, navHeight, render.DoubleBox, render.Style{})

	// Title with scroll indicator
	title := " Navigation "
	if scrollOffset > 0 || len(allLinks) > navHeight-4 {
		title = fmt.Sprintf(" Navigation (%d-%d of %d) ", scrollOffset+1,
			min(scrollOffset+navHeight-4, len(allLinks)), len(allLinks))
	}
	titleX := startX + (navWidth-len(title))/2
	r.canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

	// Available lines for content (inside border, minus title line and footer)
	contentLines := navHeight - 4
	y := startY + 2

	// Skip links before scroll offset
	linkIndex := 0
	linesRendered := 0
	currentSection := ""

	// Find starting position accounting for section headers
	for linkIndex < len(allLinks) && linkIndex < scrollOffset {
		if allLinks[linkIndex].Section != currentSection {
			currentSection = allLinks[linkIndex].Section
		}
		linkIndex++
	}

	// Reset section tracking for visible portion
	if linkIndex > 0 && linkIndex < len(allLinks) {
		// Show section header for first visible item if it's different
		currentSection = ""
	} else {
		currentSection = ""
	}

	// Render visible links
	for linkIndex < len(allLinks) && linesRendered < contentLines {
		link := allLinks[linkIndex]
		x := startX + 2

		// Show section header when it changes
		if link.Section != currentSection {
			currentSection = link.Section
			if linesRendered < contentLines {
				sectionText := currentSection
				if len(sectionText) > navWidth-6 {
					sectionText = sectionText[:navWidth-9] + "..."
				}
				r.canvas.WriteString(x, y, sectionText, render.Style{Dim: true, Bold: true})
				y++
				linesRendered++
				if linesRendered >= contentLines {
					break
				}
			}
		}

		// Format: [label] text
		if linkIndex < len(labels) {
			label := labels[linkIndex]
			text := link.Text
			maxTextWidth := navWidth - 6 - len(label)
			if maxTextWidth > 0 && len(text) > maxTextWidth {
				text = text[:maxTextWidth-3] + "..."
			}

			// Draw label
			for j, ch := range label {
				r.canvas.Set(x+j, y, ch, render.Style{Reverse: true, Bold: true})
			}

			// Draw link text
			r.canvas.WriteString(x+len(label)+1, y, text, render.Style{Underline: true})
		}

		y++
		linesRendered++
		linkIndex++
	}

	// Footer hint
	hint := " j/k scroll, label to follow, ESC close "
	hintX := startX + (navWidth-len(hint))/2
	r.canvas.WriteString(hintX, startY+navHeight-1, hint, render.Style{Dim: true})

	// Draw scroll indicators
	if scrollOffset > 0 {
		r.canvas.WriteString(startX+navWidth-3, startY+1, "▲", render.Style{Bold: true})
	}
	if linkIndex < len(allLinks) {
		r.canvas.WriteString(startX+navWidth-3, startY+navHeight-2, "▼", render.Style{Bold: true})
	}

	return allLinks
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
