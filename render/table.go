package render

import "strings"

// Table represents a drawable table.
type Table struct {
	Headers     []string
	Rows        [][]string
	ColumnAlign []Alignment
	BoxStyle    BoxStyle
	HeaderStyle Style
	CellStyle   Style
}

// NewTable creates a new table with the given headers.
func NewTable(headers ...string) *Table {
	return &Table{
		Headers:     headers,
		BoxStyle:    SingleBox,
		HeaderStyle: Style{Bold: true},
		CellStyle:   Style{},
		ColumnAlign: make([]Alignment, len(headers)),
	}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cells ...string) {
	for len(cells) < len(t.Headers) {
		cells = append(cells, "")
	}
	t.Rows = append(t.Rows, cells)
}

// SetAlignment sets the alignment for a column.
func (t *Table) SetAlignment(col int, align Alignment) {
	if col >= 0 && col < len(t.ColumnAlign) {
		t.ColumnAlign[col] = align
	}
}

func (t *Table) calculateColumnWidths() []int {
	widths := make([]int, len(t.Headers))

	for i, h := range t.Headers {
		widths[i] = StringWidth(h)
	}

	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) {
				if w := StringWidth(cell); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	return widths
}

// TotalWidth returns the total width the table will occupy.
func (t *Table) TotalWidth() int {
	widths := t.calculateColumnWidths()
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	return total
}

// Draw renders the table onto the canvas at the given position.
func (t *Table) Draw(c *Canvas, x, y int) int {
	if len(t.Headers) == 0 {
		return 0
	}

	widths := t.calculateColumnWidths()
	box := t.BoxStyle
	currentY := y

	t.drawHorizontalBorder(c, x, currentY, widths, box.TopLeft, box.TopTee, box.TopRight)
	currentY++

	t.drawRow(c, x, currentY, t.Headers, widths, t.HeaderStyle)
	currentY++

	t.drawHorizontalBorder(c, x, currentY, widths, box.LeftTee, box.Cross, box.RightTee)
	currentY++

	for _, row := range t.Rows {
		t.drawRow(c, x, currentY, row, widths, t.CellStyle)
		currentY++
	}

	t.drawHorizontalBorder(c, x, currentY, widths, box.BottomLeft, box.BottomTee, box.BottomRight)
	currentY++

	return currentY - y
}

func (t *Table) drawHorizontalBorder(c *Canvas, x, y int, widths []int, left, mid, right rune) {
	currentX := x
	c.Set(currentX, y, left, Style{})
	currentX++

	for i, w := range widths {
		for j := 0; j < w+2; j++ {
			c.Set(currentX, y, t.BoxStyle.Horizontal, Style{})
			currentX++
		}
		if i < len(widths)-1 {
			c.Set(currentX, y, mid, Style{})
		} else {
			c.Set(currentX, y, right, Style{})
		}
		currentX++
	}
}

func (t *Table) drawRow(c *Canvas, x, y int, cells []string, widths []int, style Style) {
	currentX := x
	c.Set(currentX, y, t.BoxStyle.Vertical, Style{})
	currentX++

	for i, width := range widths {
		c.Set(currentX, y, ' ', Style{})
		currentX++

		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}

		align := AlignLeft
		if i < len(t.ColumnAlign) {
			align = t.ColumnAlign[i]
		}

		aligned := AlignText(cell, width, align)
		c.WriteString(currentX, y, aligned, style)
		currentX += width

		c.Set(currentX, y, ' ', Style{})
		currentX++

		c.Set(currentX, y, t.BoxStyle.Vertical, Style{})
		currentX++
	}
}

// RenderToString renders the table to a string.
func (t *Table) RenderToString() string {
	width := t.TotalWidth()
	height := len(t.Rows) + 4

	canvas := NewCanvas(width, height)
	t.Draw(canvas, 0, 0)

	var lines []string
	for y := 0; y < height; y++ {
		var line strings.Builder
		for x := 0; x < width; x++ {
			line.WriteRune(canvas.Get(x, y).Rune)
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}

	return strings.Join(lines, "\n")
}
