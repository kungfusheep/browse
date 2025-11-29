// Package document renders parsed HTML to the terminal canvas.
package document

import (
	"browse/html"
	"browse/render"
	"strings"
)

// Renderer converts HTML nodes to canvas output.
type Renderer struct {
	canvas *render.Canvas
	width  int
	margin int
	y      int
}

// NewRenderer creates a renderer for the given canvas.
func NewRenderer(c *render.Canvas) *Renderer {
	return &Renderer{
		canvas: c,
		width:  c.Width(),
		margin: 2,
		y:      0,
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
	textWidth := r.width - (r.margin * 2)

	for _, child := range doc.Children {
		height += r.nodeHeight(child, textWidth)
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
	textWidth := r.width - (r.margin * 2)

	switch n.Type {
	case html.NodeHeading1:
		r.renderHeading1(n.Text, textWidth)

	case html.NodeHeading2:
		r.renderHeading2(n.Text, textWidth)

	case html.NodeHeading3:
		r.renderHeading3(n.Text, textWidth)

	case html.NodeParagraph:
		r.renderParagraph(n, textWidth)

	case html.NodeBlockquote:
		r.renderBlockquote(n, textWidth)

	case html.NodeList:
		r.renderList(n, textWidth)

	case html.NodeCodeBlock:
		r.renderCodeBlock(n.Text, textWidth)
	}
}

func (r *Renderer) renderHeading1(text string, width int) {
	r.y++
	r.writeLine(r.margin, r.y, text, render.Style{Bold: true})
	r.y++
	r.canvas.DrawHLine(r.margin, r.y, len(text), render.DoubleBox.Horizontal, render.Style{})
	r.y += 2
}

func (r *Renderer) renderHeading2(text string, width int) {
	r.y++
	r.writeLine(r.margin, r.y, text, render.Style{Bold: true})
	r.y += 2
}

func (r *Renderer) renderHeading3(text string, width int) {
	r.writeLine(r.margin, r.y, text, render.Style{Bold: true, Underline: true})
	r.y += 2
}

func (r *Renderer) renderParagraph(n *html.Node, width int) {
	text := n.PlainText()
	lines := render.WrapAndJustify(text, width)

	for _, line := range lines {
		r.writeLine(r.margin, r.y, line, render.Style{})
		r.y++
	}
	r.y++
}

func (r *Renderer) renderBlockquote(n *html.Node, width int) {
	// Draw left border
	startY := r.y

	// Render children with indent
	for _, child := range n.Children {
		if child.Type == html.NodeParagraph {
			text := child.PlainText()
			lines := render.WrapText(text, width-4)
			for _, line := range lines {
				r.writeLine(r.margin+4, r.y, line, render.Style{Dim: true})
				r.y++
			}
		}
	}

	// Draw the border
	for y := startY; y < r.y; y++ {
		if y >= 0 && y < r.canvas.Height() {
			r.canvas.Set(r.margin, y, '│', render.Style{Dim: true})
		}
	}

	r.y++
}

func (r *Renderer) renderList(n *html.Node, width int) {
	for _, item := range n.Children {
		text := item.PlainText()
		lines := render.WrapText(text, width-4)

		for i, line := range lines {
			if i == 0 {
				r.writeLine(r.margin, r.y, "•", render.Style{})
				r.writeLine(r.margin+2, r.y, line, render.Style{})
			} else {
				r.writeLine(r.margin+2, r.y, line, render.Style{})
			}
			r.y++
		}
	}
	r.y++
}

func (r *Renderer) renderCodeBlock(text string, width int) {
	lines := strings.Split(text, "\n")

	r.canvas.DrawHLine(r.margin, r.y, width, render.SingleBox.Horizontal, render.Style{Dim: true})
	r.y++

	for _, line := range lines {
		if len(line) > width {
			line = line[:width]
		}
		r.writeLine(r.margin, r.y, line, render.Style{Dim: true})
		r.y++
	}

	r.canvas.DrawHLine(r.margin, r.y, width, render.SingleBox.Horizontal, render.Style{Dim: true})
	r.y++
}

func (r *Renderer) writeLine(x, y int, text string, style render.Style) {
	if y < 0 || y >= r.canvas.Height() {
		return
	}
	r.canvas.WriteString(x, y, text, style)
}
