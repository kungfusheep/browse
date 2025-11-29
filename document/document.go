// Package document renders parsed HTML to the terminal canvas.
package document

import (
	"browse/html"
	"browse/render"
	"strings"
)

const maxContentWidth = 80

// Renderer converts HTML nodes to canvas output.
type Renderer struct {
	canvas       *render.Canvas
	contentWidth int
	leftMargin   int
	y            int
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

	for _, child := range doc.Children {
		r.renderNode(child)
	}
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
	text := n.PlainText()
	lines := render.WrapAndJustify(text, r.contentWidth)

	for _, line := range lines {
		r.writeLine(r.leftMargin, r.y, line, render.Style{})
		r.y++
	}
	r.y++
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
