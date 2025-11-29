// Demo showcases the rendering capabilities in a 70s-80s technical documentation style.
package main

import (
	"fmt"
	"strings"

	"browse/render"
)

func main() {
	// Create a canvas
	canvas := render.NewCanvas(80, 40)
	canvas.Clear()

	// Title in a double-line box
	canvas.DrawBoxWithTitle(0, 0, 80, 3, " BROWSE - Terminal Web Browser ", render.DoubleBox, render.Style{}, render.Style{Bold: true})

	// Justified paragraph demonstrating the text engine
	paraText := `The Browse terminal web browser reimagines web content for the terminal. Rather than attempting to faithfully reproduce complex CSS layouts, Browse focuses on what terminals do best: beautifully formatted text with clear visual hierarchy.`

	lines := render.WrapAndJustify(paraText, 76)
	y := 4
	for _, line := range lines {
		canvas.WriteString(2, y, line, render.Style{})
		y++
	}

	// Section header
	y += 1
	canvas.DrawHLine(0, y, 80, render.SingleBox.Horizontal, render.Style{})
	y++
	canvas.WriteString(2, y, "DESIGN PRINCIPLES", render.Style{Bold: true, Underline: true})
	y += 2

	// Bullet points
	bullets := []string{
		"Text-first rendering optimized for monospace display",
		"Box-drawing characters for structure and visual appeal",
		"Justified text creating dense, readable paragraphs",
		"Tables that actually look like tables",
	}

	for _, bullet := range bullets {
		canvas.WriteString(2, y, "•", render.Style{})
		canvas.WriteString(4, y, bullet, render.Style{})
		y++
	}

	// Create a table
	y += 1
	table := render.NewTable("Feature", "Status", "Notes")
	table.AddRow("Canvas", "Complete", "Screen buffer abstraction")
	table.AddRow("Box Drawing", "Complete", "Single, double, rounded styles")
	table.AddRow("Text Justify", "Complete", "Unicode-aware")
	table.AddRow("Tables", "Complete", "With alignment support")
	table.AddRow("Document Layout", "Pending", "Next milestone")

	table.SetAlignment(1, render.AlignCenter)

	tableHeight := table.Draw(canvas, 2, y)
	y += tableHeight

	// Footer
	y += 1
	canvas.DrawHLine(0, y, 80, render.DoubleBox.Horizontal, render.Style{})
	y++
	footer := render.AlignText("Inspired by DEC manuals and IBM technical references", 76, render.AlignCenter)
	canvas.WriteString(2, y, footer, render.Style{Dim: true})

	// Render to string and print
	var sb strings.Builder
	for row := 0; row < 30; row++ {
		for col := 0; col < 80; col++ {
			cell := canvas.Get(col, row)
			sb.WriteRune(cell.Rune)
		}
		sb.WriteString("\n")
	}

	fmt.Print(sb.String())
}
