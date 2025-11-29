// Package document renders parsed HTML to the terminal canvas.
package document

import (
	"browse/html"
	"browse/render"
	"strings"
)

const maxContentWidth = 80

// Link represents a clickable link in the document.
type Link struct {
	Href   string
	X, Y   int // position on canvas
	Length int // display length for highlighting
}

// Renderer converts HTML nodes to canvas output.
type Renderer struct {
	canvas       *render.Canvas
	contentWidth int
	leftMargin   int
	y            int
	links        []Link
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
	r.links = nil // reset links for this render

	for _, child := range doc.Children {
		r.renderNode(child)
	}
}

// Links returns the visible links from the last render.
func (r *Renderer) Links() []Link {
	return r.links
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
		r.renderHeading1(n.Text)
	case html.NodeHeading2:
		r.renderHeading2(n.Text)
	case html.NodeHeading3:
		r.renderHeading3(n.Text)
	case html.NodeParagraph:
		r.renderParagraph(n)
	case html.NodeBlockquote:
		r.renderBlockquote(n)
	case html.NodeList:
		r.renderList(n)
	case html.NodeCodeBlock:
		r.renderCodeBlock(n.Text)
	}
}

func (r *Renderer) renderHeading1(text string) {
	r.y++
	r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true})
	r.y++
	r.canvas.DrawHLine(r.leftMargin, r.y, len(text), render.DoubleBox.Horizontal, render.Style{})
	r.y += 2
}

func (r *Renderer) renderHeading2(text string) {
	r.y++
	r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true})
	r.y += 2
}

func (r *Renderer) renderHeading3(text string) {
	r.writeLine(r.leftMargin, r.y, text, render.Style{Bold: true, Underline: true})
	r.y += 2
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
	// For now, flatten to text, wrap, then re-apply styles
	// This is simpler and handles justification correctly
	var fullText strings.Builder
	for _, span := range spans {
		fullText.WriteString(span.Text)
	}

	lines := render.WrapAndJustify(fullText.String(), width)
	result := make([][]textSpan, len(lines))

	for i, line := range lines {
		result[i] = []textSpan{{Text: line, Style: render.Style{}}}
	}

	// TODO: preserve inline styles across wrapped lines
	// For now, links won't be tracked perfectly on wrapped paragraphs
	// but this gets us started

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
		text := item.PlainText()
		lines := render.WrapText(text, r.contentWidth-4)

		for i, line := range lines {
			if i == 0 {
				r.writeLine(r.leftMargin, r.y, "•", render.Style{})
				r.writeLine(r.leftMargin+2, r.y, line, render.Style{})
			} else {
				r.writeLine(r.leftMargin+2, r.y, line, render.Style{})
			}
			r.y++
		}
	}
	r.y++
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

	// Single character labels first
	for _, k := range keys {
		if len(labels) >= count {
			return labels
		}
		labels = append(labels, string(k))
	}

	// Two character labels
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
