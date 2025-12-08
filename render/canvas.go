package render

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Canvas is a drawable buffer that can be rendered to the terminal.
type Canvas struct {
	width  int
	height int
	cells  [][]Cell
}

// NewCanvas creates a new canvas with the given dimensions.
func NewCanvas(width, height int) *Canvas {
	cells := make([][]Cell, height)
	for y := range cells {
		cells[y] = make([]Cell, width)
		for x := range cells[y] {
			cells[y][x] = Cell{Rune: ' '}
		}
	}
	return &Canvas{width: width, height: height, cells: cells}
}

// NewCanvasFromTerminal creates a canvas sized to the current terminal.
func NewCanvasFromTerminal() (*Canvas, error) {
	w, h, err := TerminalSize()
	if err != nil {
		return nil, err
	}
	return NewCanvas(w, h), nil
}

// TerminalSize returns the current terminal dimensions.
func TerminalSize() (width, height int, err error) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, fmt.Errorf("getting terminal size: %w", err)
	}
	return int(ws.Col), int(ws.Row), nil
}

func (c *Canvas) Width() int  { return c.width }
func (c *Canvas) Height() int { return c.height }

// Clear fills the entire canvas with spaces.
func (c *Canvas) Clear() {
	for y := range c.cells {
		for x := range c.cells[y] {
			c.cells[y][x] = Cell{Rune: ' '}
		}
	}
}

// DimAll applies the Dim style to all cells, greying out the canvas content.
func (c *Canvas) DimAll() {
	for y := range c.cells {
		for x := range c.cells[y] {
			c.cells[y][x].Style.Dim = true
			c.cells[y][x].Style.Bold = false
		}
	}
}

// Set places a rune at the given position with the given style.
func (c *Canvas) Set(x, y int, r rune, style Style) {
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return
	}
	c.cells[y][x] = Cell{Rune: r, Style: style}
}

// Get returns the cell at the given position.
func (c *Canvas) Get(x, y int) Cell {
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return Cell{Rune: ' '}
	}
	return c.cells[y][x]
}

// WriteString writes a string starting at the given position.
// Returns the number of terminal cells used (not runes).
func (c *Canvas) WriteString(x, y int, s string, style Style) int {
	pos := 0
	for _, r := range s {
		w := UnicodeWidth(r)
		if x+pos+w > c.width {
			break
		}
		c.Set(x+pos, y, r, style)
		pos += w // Advance by character width (1 for normal, 2 for wide)
	}
	return pos
}

// DrawBox draws a box on the canvas.
func (c *Canvas) DrawBox(x, y, width, height int, box BoxStyle, style Style) {
	if width < 2 || height < 2 {
		return
	}

	c.Set(x, y, box.TopLeft, style)
	c.Set(x+width-1, y, box.TopRight, style)
	c.Set(x, y+height-1, box.BottomLeft, style)
	c.Set(x+width-1, y+height-1, box.BottomRight, style)

	for i := 1; i < width-1; i++ {
		c.Set(x+i, y, box.Horizontal, style)
		c.Set(x+i, y+height-1, box.Horizontal, style)
	}

	for i := 1; i < height-1; i++ {
		c.Set(x, y+i, box.Vertical, style)
		c.Set(x+width-1, y+i, box.Vertical, style)
	}
}

// DrawHLine draws a horizontal line.
func (c *Canvas) DrawHLine(x, y, length int, r rune, style Style) {
	for i := 0; i < length; i++ {
		c.Set(x+i, y, r, style)
	}
}

// DrawBoxWithTitle draws a box with a title in the top border.
func (c *Canvas) DrawBoxWithTitle(x, y, width, height int, title string, box BoxStyle, style Style, titleStyle Style) {
	c.DrawBox(x, y, width, height, box, style)

	if len(title) > 0 && width > 4 {
		maxTitleWidth := width - 4
		titleWidth := StringWidth(title)

		// Truncate title if too wide (by runes, checking width)
		if titleWidth > maxTitleWidth {
			truncated := ""
			w := 0
			for _, r := range title {
				rw := UnicodeWidth(r)
				if w+rw > maxTitleWidth {
					break
				}
				truncated += string(r)
				w += rw
			}
			title = truncated
			titleWidth = w
		}

		titleX := x + 2
		c.Set(titleX-1, y, ' ', style)
		c.WriteString(titleX, y, title, titleStyle)
		c.Set(titleX+titleWidth, y, ' ', style)
	}
}

// Render outputs the canvas as a string with ANSI escape codes.
func (c *Canvas) Render() string {
	var sb strings.Builder
	sb.WriteString("\033[H")

	var currentStyle Style

	for y := 0; y < c.height; y++ {
		for x := 0; x < c.width; x++ {
			cell := c.cells[y][x]

			if cell.Style != currentStyle {
				sb.WriteString(styleSequence(cell.Style))
				currentStyle = cell.Style
			}

			sb.WriteRune(cell.Rune)
		}
		if y < c.height-1 {
			sb.WriteString("\r\n")
		}
	}

	sb.WriteString("\033[0m")
	return sb.String()
}

func styleSequence(s Style) string {
	codes := []string{"0"}
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Dim {
		codes = append(codes, "2")
	}
	if s.Underline {
		codes = append(codes, "4")
	}
	if s.Reverse {
		codes = append(codes, "7")
	}
	if s.FgColor > 0 {
		codes = append(codes, fmt.Sprintf("%d", s.FgColor))
	}
	if s.UseBgRGB {
		// True color: 48;2;R;G;B
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", s.BgRGB[0], s.BgRGB[1], s.BgRGB[2]))
	} else if s.BgColor > 0 {
		codes = append(codes, fmt.Sprintf("%d", s.BgColor))
	}
	return fmt.Sprintf("\033[%sm", strings.Join(codes, ";"))
}

// RenderTo writes the canvas to the given file.
func (c *Canvas) RenderTo(w *os.File) error {
	_, err := w.WriteString(c.Render())
	return err
}

// Word represents a word extracted from the canvas with its position.
type Word struct {
	Text string
	X, Y int
}

// ExtractWords extracts all words (3+ letters) from the canvas with positions.
// Returns ALL occurrences in reading order (top-to-bottom, left-to-right).
func (c *Canvas) ExtractWords(minLen int) []Word {
	var words []Word

	for y := 0; y < c.height; y++ {
		var currentWord strings.Builder
		wordStartX := 0

		for x := 0; x < c.width; x++ {
			r := c.cells[y][x].Rune
			isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')

			if isLetter {
				if currentWord.Len() == 0 {
					wordStartX = x
				}
				currentWord.WriteRune(r)
			} else {
				if currentWord.Len() >= minLen {
					word := strings.ToLower(currentWord.String())
					words = append(words, Word{
						Text: word,
						X:    wordStartX,
						Y:    y,
					})
				}
				currentWord.Reset()
			}
		}
		// Handle word at end of line
		if currentWord.Len() >= minLen {
			word := strings.ToLower(currentWord.String())
			words = append(words, Word{
				Text: word,
				X:    wordStartX,
				Y:    y,
			})
		}
	}

	return words
}

// PlainText returns the canvas content as plain text without ANSI codes.
// Uses the same render output but strips escape sequences.
func (c *Canvas) PlainText() string {
	rendered := c.Render()
	stripped := StripANSI(rendered)
	// Normalize line endings and trim trailing whitespace from each line
	stripped = strings.ReplaceAll(stripped, "\r\n", "\n")
	stripped = strings.ReplaceAll(stripped, "\r", "\n")
	lines := strings.Split(stripped, "\n")
	var cleaned []string
	for _, line := range lines {
		cleaned = append(cleaned, strings.TrimRight(line, " \t"))
	}
	// Remove trailing empty lines
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n") + "\n"
}
