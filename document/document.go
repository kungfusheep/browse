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
	X, Y   int // position on canvas
	Length int // display length for highlighting
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
	links        []Link
	inputs       []Input
	headings     []Heading

	// Current form context (for inputs)
	currentFormAction string
	currentFormMethod string

	// Section numbering
	h1Count int
	h2Count int
	h3Count int
}

// NewRenderer creates a renderer for the given canvas.
func NewRenderer(c *render.Canvas) *Renderer {
	canvasWidth := c.Width()

	// Content width is capped at maxContentWidth
	contentWidth := canvasWidth - 4 // minimal margins
	if contentWidth > maxContentWidth {
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
func (r *Renderer) Render(doc *html.Node, scrollY int) {
	r.canvas.Clear()
	r.y = -scrollY
	r.links = nil    // reset links for this render
	r.inputs = nil   // reset inputs for this render
	r.headings = nil // reset headings for this render

	// Reset form context
	r.currentFormAction = ""
	r.currentFormMethod = ""

	// Reset section counters
	r.h1Count = 0
	r.h2Count = 0
	r.h3Count = 0

	for _, child := range doc.Children {
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

// ContentHeight returns the total height needed for the document.
func (r *Renderer) ContentHeight(doc *html.Node) int {
	height := 0

	for _, child := range doc.Children {
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
	default:
		return 1
	}
}

func (r *Renderer) renderNode(n *html.Node) {
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

	// Track heading for TOC
	r.headings = append(r.headings, Heading{
		Level:  1,
		Number: sectionNum,
		Text:   text,
		Y:      r.y,
	})

	// Draw section number (dimmed) then title (bold)
	r.writeLine(r.leftMargin, r.y, paddedNum, render.Style{Dim: true})
	r.writeLine(r.leftMargin+sectionNumWidth, r.y, displayText, render.Style{Bold: true})

	if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
		r.links = append(r.links, Link{
			Href:   href,
			X:      r.leftMargin + sectionNumWidth,
			Y:      r.y,
			Length: render.StringWidth(displayText),
		})
	}

	r.y++
	r.canvas.DrawHLine(r.leftMargin+sectionNumWidth, r.y, render.StringWidth(displayText), render.DoubleBox.Horizontal, render.Style{})
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

	// Track heading for TOC
	r.headings = append(r.headings, Heading{
		Level:  2,
		Number: sectionNum,
		Text:   text,
		Y:      r.y,
	})

	// Draw section number (dimmed) then title (bold)
	r.writeLine(r.leftMargin, r.y, paddedNum, render.Style{Dim: true})
	r.writeLine(r.leftMargin+sectionNumWidth, r.y, text, render.Style{Bold: true})

	if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
		r.links = append(r.links, Link{
			Href:   href,
			X:      r.leftMargin + sectionNumWidth,
			Y:      r.y,
			Length: render.StringWidth(text),
		})
	}

	// Single line under h2 (under title only)
	r.y++
	r.canvas.DrawHLine(r.leftMargin+sectionNumWidth, r.y, render.StringWidth(text), render.SingleBox.Horizontal, render.Style{Dim: true})
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
	if sectionNum != "" {
		r.headings = append(r.headings, Heading{
			Level:  3,
			Number: sectionNum,
			Text:   text,
			Y:      r.y,
		})
	}

	// Draw with right-aligned section number pattern
	if sectionNum != "" {
		paddedNum := fmt.Sprintf("%*s  ", sectionNumWidth-2, sectionNum)
		r.writeLine(r.leftMargin, r.y, paddedNum, render.Style{Dim: true})
		r.writeLine(r.leftMargin+sectionNumWidth, r.y, text, render.Style{Bold: true, Underline: true})

		if href != "" && r.y >= 0 && r.y < r.canvas.Height() {
			r.links = append(r.links, Link{
				Href:   href,
				X:      r.leftMargin + sectionNumWidth,
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
