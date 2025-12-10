package main

import (
	"fmt"
	"os"

	"browse/render"
	"browse/theme"
)

type linkStyle struct {
	name   string
	render func(c *render.Canvas, x, y int, text string) int
}

func main() {
	styles := []linkStyle{
		// Current
		{
			name: "1. Current (underline only)",
			render: func(c *render.Canvas, x, y int, text string) int {
				return c.WriteString(x, y, text, render.Style{Underline: true})
			},
		},
		// Color variations
		{
			name: "2. Accent color only",
			render: func(c *render.Canvas, x, y int, text string) int {
				return c.WriteString(x, y, text, theme.Current.Accent.Style())
			},
		},
		{
			name: "3. Accent + bold",
			render: func(c *render.Canvas, x, y int, text string) int {
				style := theme.Current.Accent.Style()
				style.Bold = true
				return c.WriteString(x, y, text, style)
			},
		},
		{
			name: "4. Accent + underline",
			render: func(c *render.Canvas, x, y int, text string) int {
				style := theme.Current.Accent.Style()
				style.Underline = true
				return c.WriteString(x, y, text, style)
			},
		},
		// Leading markers
		{
			name: "5. Leading block ▌",
			render: func(c *render.Canvas, x, y int, text string) int {
				c.WriteString(x, y, "▌", theme.Current.Accent.Style())
				return c.WriteString(x+1, y, text, render.Style{}) + 1
			},
		},
		{
			name: "6. Leading arrow →",
			render: func(c *render.Canvas, x, y int, text string) int {
				c.WriteString(x, y, "→", theme.Current.Accent.Style())
				return c.WriteString(x+2, y, text, render.Style{}) + 2
			},
		},
		{
			name: "7. Leading dot ◦",
			render: func(c *render.Canvas, x, y int, text string) int {
				c.WriteString(x, y, "◦", theme.Current.Accent.Style())
				return c.WriteString(x+2, y, text, render.Style{}) + 2
			},
		},
		// Surrounding markers
		{
			name: "8. Brackets [text]",
			render: func(c *render.Canvas, x, y int, text string) int {
				accent := theme.Current.Accent.Style()
				c.WriteString(x, y, "[", accent)
				w := c.WriteString(x+1, y, text, render.Style{})
				c.WriteString(x+1+w, y, "]", accent)
				return w + 2
			},
		},
		{
			name: "9. Angle «text»",
			render: func(c *render.Canvas, x, y int, text string) int {
				accent := theme.Current.Accent.Style()
				c.WriteString(x, y, "«", accent)
				w := c.WriteString(x+1, y, text, render.Style{})
				c.WriteString(x+1+w, y, "»", accent)
				return w + 2
			},
		},
		{
			name: "10. Parens (text)",
			render: func(c *render.Canvas, x, y int, text string) int {
				accent := theme.Current.Accent.Style()
				c.WriteString(x, y, "(", accent)
				w := c.WriteString(x+1, y, text, render.Style{})
				c.WriteString(x+1+w, y, ")", accent)
				return w + 2
			},
		},
		// Background variations
		{
			name: "11. Subtle background",
			render: func(c *render.Canvas, x, y int, text string) int {
				style := render.Style{UseBgRGB: true}
				if theme.Current.Dark {
					style.BgRGB = [3]uint8{40, 40, 50}
				} else {
					style.BgRGB = [3]uint8{230, 230, 240}
				}
				return c.WriteString(x, y, text, style)
			},
		},
		{
			name: "12. Accent background + contrast text",
			render: func(c *render.Canvas, x, y int, text string) int {
				accent := theme.Current.Accent
				style := render.Style{
					BgRGB:    [3]uint8{accent.R, accent.G, accent.B},
					UseBgRGB: true,
					UseFgRGB: true,
				}
				if theme.Current.Dark {
					style.FgRGB = [3]uint8{0, 0, 0}
				} else {
					style.FgRGB = [3]uint8{255, 255, 255}
				}
				return c.WriteString(x, y, " "+text+" ", style)
			},
		},
		{
			name: "13. Dim background + accent text",
			render: func(c *render.Canvas, x, y int, text string) int {
				style := theme.Current.Accent.Style()
				style.UseBgRGB = true
				if theme.Current.Dark {
					style.BgRGB = [3]uint8{30, 35, 40}
				} else {
					style.BgRGB = [3]uint8{240, 240, 245}
				}
				return c.WriteString(x, y, text, style)
			},
		},
		{
			name: "14. Reverse video",
			render: func(c *render.Canvas, x, y int, text string) int {
				return c.WriteString(x, y, text, render.Style{Reverse: true})
			},
		},
		// Combinations
		{
			name: "15. Leading block ▌+ subtle bg",
			render: func(c *render.Canvas, x, y int, text string) int {
				c.WriteString(x, y, "▌", theme.Current.Accent.Style())
				style := render.Style{UseBgRGB: true}
				if theme.Current.Dark {
					style.BgRGB = [3]uint8{40, 40, 50}
				} else {
					style.BgRGB = [3]uint8{230, 230, 240}
				}
				return c.WriteString(x+1, y, text, style) + 1
			},
		},
		{
			name: "16. Leading block ▌+ subtle bg + space",
			render: func(c *render.Canvas, x, y int, text string) int {
				c.WriteString(x, y, "▌", theme.Current.Accent.Style())
				style := render.Style{UseBgRGB: true}
				if theme.Current.Dark {
					style.BgRGB = [3]uint8{40, 40, 50}
				} else {
					style.BgRGB = [3]uint8{230, 230, 240}
				}
				return c.WriteString(x+1, y, " "+text+" ", style) + 1
			},
		},
		{
			name: "17. Block + accent bg",
			render: func(c *render.Canvas, x, y int, text string) int {
				accent := theme.Current.Accent
				// Darker version of accent for block
				blockStyle := render.Style{
					FgRGB:    [3]uint8{accent.R, accent.G, accent.B},
					UseFgRGB: true,
				}
				c.WriteString(x, y, "▌", blockStyle)
				// Subtle tinted background
				style := render.Style{UseBgRGB: true}
				if theme.Current.Dark {
					style.BgRGB = [3]uint8{30, 40, 45}
				} else {
					style.BgRGB = [3]uint8{230, 240, 245}
				}
				return c.WriteString(x+1, y, text, style) + 1
			},
		},
		// Minimal
		{
			name: "18. Bold only",
			render: func(c *render.Canvas, x, y int, text string) int {
				return c.WriteString(x, y, text, render.Style{Bold: true})
			},
		},
		{
			name: "19. Dim text",
			render: func(c *render.Canvas, x, y int, text string) int {
				return c.WriteString(x, y, text, render.Style{Dim: true})
			},
		},
	}

	width := 78
	canvas := render.NewCanvas(width, 20*len(styles)+10)
	canvas.ClearWithStyle(theme.Current.Background.Style())

	y := 1
	titleStyle := theme.Current.Foreground.Style()
	titleStyle.Bold = true

	dimStyle := theme.Current.Foreground.Style()
	dimStyle.Dim = true

	canvas.WriteString(2, y, "Link Styling Comparison", titleStyle)
	y += 2
	canvas.WriteString(2, y, "Each style shown as: list items, inline in text, and dense links", dimStyle)
	y += 3

	for _, s := range styles {
		// Section header
		canvas.WriteString(2, y, s.name, titleStyle)
		y += 2

		// List items
		canvas.WriteString(2, y, "• ", render.Style{})
		s.render(canvas, 4, y, "Why the A.I. Boom Is Unlike the Dot-Com Boom")
		y++
		canvas.WriteString(2, y, "• ", render.Style{})
		s.render(canvas, 4, y, "China's Access to Powerful Nvidia Chips")
		y++
		canvas.WriteString(2, y, "• ", render.Style{})
		s.render(canvas, 4, y, "How Strangers Chose My Wedding Dress")
		y += 2

		// Inline in text
		x := 2
		x += canvas.WriteString(x, y, "Read more about ", render.Style{})
		x += s.render(canvas, x, y, "artificial intelligence")
		x += canvas.WriteString(x, y, " and how ", render.Style{})
		x += s.render(canvas, x, y, "tech companies")
		canvas.WriteString(x, y, " are", render.Style{})
		y++
		x = 2
		x += canvas.WriteString(x, y, "reshaping the industry. See also: ", render.Style{})
		x += s.render(canvas, x, y, "related coverage")
		canvas.WriteString(x, y, ".", render.Style{})
		y += 2

		// Dense links (like a nav or footer)
		x = 2
		x += s.render(canvas, x, y, "Home")
		x += canvas.WriteString(x, y, " | ", dimStyle)
		x += s.render(canvas, x, y, "World")
		x += canvas.WriteString(x, y, " | ", dimStyle)
		x += s.render(canvas, x, y, "Politics")
		x += canvas.WriteString(x, y, " | ", dimStyle)
		x += s.render(canvas, x, y, "Business")
		x += canvas.WriteString(x, y, " | ", dimStyle)
		x += s.render(canvas, x, y, "Tech")
		x += canvas.WriteString(x, y, " | ", dimStyle)
		s.render(canvas, x, y, "Science")
		y += 3
	}

	canvas.RenderTo(os.Stdout)
	fmt.Println()
}
