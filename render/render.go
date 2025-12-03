// Package render provides terminal rendering primitives with a 70s-80s
// technical documentation aesthetic.
package render

import (
	"strings"
	"unicode"
)

// Alignment specifies text alignment within a given width.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
	AlignCenter
	AlignJustify
)

// Cell represents a single character cell in the terminal.
type Cell struct {
	Rune  rune
	Style Style
}

// Style represents text styling for a cell.
type Style struct {
	Bold      bool
	Dim       bool
	Underline bool
	Reverse   bool
	FgColor   int // ANSI foreground color code (0 = default, 32 = green, 33 = yellow, etc.)
}

// BoxStyle defines the characters used for drawing boxes.
type BoxStyle struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horizontal  rune
	Vertical    rune
	TopTee      rune
	BottomTee   rune
	LeftTee     rune
	RightTee    rune
	Cross       rune
}

var (
	SingleBox = BoxStyle{
		TopLeft: '┌', TopRight: '┐', BottomLeft: '└', BottomRight: '┘',
		Horizontal: '─', Vertical: '│',
		TopTee: '┬', BottomTee: '┴', LeftTee: '├', RightTee: '┤', Cross: '┼',
	}

	DoubleBox = BoxStyle{
		TopLeft: '╔', TopRight: '╗', BottomLeft: '╚', BottomRight: '╝',
		Horizontal: '═', Vertical: '║',
		TopTee: '╦', BottomTee: '╩', LeftTee: '╠', RightTee: '╣', Cross: '╬',
	}

	RoundedBox = BoxStyle{
		TopLeft: '╭', TopRight: '╮', BottomLeft: '╰', BottomRight: '╯',
		Horizontal: '─', Vertical: '│',
		TopTee: '┬', BottomTee: '┴', LeftTee: '├', RightTee: '┤', Cross: '┼',
	}

	HeavyBox = BoxStyle{
		TopLeft: '┏', TopRight: '┓', BottomLeft: '┗', BottomRight: '┛',
		Horizontal: '━', Vertical: '┃',
		TopTee: '┳', BottomTee: '┻', LeftTee: '┣', RightTee: '┫', Cross: '╋',
	}

	ASCIIBox = BoxStyle{
		TopLeft: '+', TopRight: '+', BottomLeft: '+', BottomRight: '+',
		Horizontal: '-', Vertical: '|',
		TopTee: '+', BottomTee: '+', LeftTee: '+', RightTee: '+', Cross: '+',
	}
)

// UnicodeWidth returns the display width of a rune in terminal cells.
func UnicodeWidth(r rune) int {
	if r < 0x80 {
		if r < 0x20 || r == 0x7F {
			return 0
		}
		return 1
	}
	if isZeroWidth(r) {
		return 0
	}
	if isWideChar(r) {
		return 2
	}
	return 1
}

// StringWidth returns the display width of a string in terminal cells.
func StringWidth(s string) int {
	width := 0
	for _, r := range s {
		width += UnicodeWidth(r)
	}
	return width
}

func isZeroWidth(r rune) bool {
	return (r >= 0x0300 && r <= 0x036F) ||
		(r >= 0x1AB0 && r <= 0x1AFF) ||
		(r >= 0x1DC0 && r <= 0x1DFF) ||
		(r >= 0x20D0 && r <= 0x20FF) ||
		(r >= 0xFE00 && r <= 0xFE0F) ||
		(r >= 0xFE20 && r <= 0xFE2F) ||
		(r >= 0xE0100 && r <= 0xE01EF) ||
		r == 0x200B || r == 0x200C || r == 0x200D || r == 0x2060 || r == 0xFEFF
}

func isWideChar(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) ||
		(r >= 0x2E80 && r <= 0x2EF3) ||
		(r >= 0x2F00 && r <= 0x2FD5) ||
		(r >= 0x3000 && r <= 0x303E) ||
		(r >= 0x3041 && r <= 0x3096) ||
		(r >= 0x3099 && r <= 0x30FF) ||
		(r >= 0x3105 && r <= 0x312F) ||
		(r >= 0x3131 && r <= 0x318E) ||
		(r >= 0x31F0 && r <= 0x321E) ||
		(r >= 0x3250 && r <= 0x4DBF) ||
		(r >= 0x4E00 && r <= 0xA48C) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE6B) ||
		(r >= 0xFF01 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x3FFFD)
}

// WrapText wraps text to fit within a given width in terminal cells.
func WrapText(text string, width int) []string {
	if width <= 0 {
		return nil
	}

	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}

		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		var currentLine strings.Builder
		currentWidth := 0

		for _, word := range words {
			wordWidth := StringWidth(word)

			if currentWidth == 0 {
				if wordWidth > width {
					lines = append(lines, breakWordUnicode(word, width)...)
					currentLine.Reset()
					currentWidth = 0
				} else {
					currentLine.WriteString(word)
					currentWidth = wordWidth
				}
			} else if currentWidth+1+wordWidth <= width {
				currentLine.WriteByte(' ')
				currentLine.WriteString(word)
				currentWidth += 1 + wordWidth
			} else {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
				if wordWidth > width {
					lines = append(lines, breakWordUnicode(word, width)...)
					currentWidth = 0
				} else {
					currentLine.WriteString(word)
					currentWidth = wordWidth
				}
			}
		}

		if currentWidth > 0 {
			lines = append(lines, currentLine.String())
		}
	}

	return lines
}

func breakWordUnicode(word string, maxWidth int) []string {
	var result []string
	runes := []rune(word)

	for len(runes) > 0 {
		var line strings.Builder
		lineWidth := 0

		for len(runes) > 0 {
			r := runes[0]
			w := UnicodeWidth(r)
			if lineWidth+w > maxWidth {
				break
			}
			line.WriteRune(r)
			lineWidth += w
			runes = runes[1:]
		}

		if line.Len() > 0 {
			result = append(result, line.String())
		} else if len(runes) > 0 {
			line.WriteRune(runes[0])
			result = append(result, line.String())
			runes = runes[1:]
		}
	}

	return result
}

// JustifyLine justifies a line of text to fit exactly within a width.
func JustifyLine(line string, width int) string {
	words := strings.Fields(line)
	if len(words) <= 1 {
		return padRight(line, width)
	}

	totalWordWidth := 0
	for _, w := range words {
		totalWordWidth += StringWidth(w)
	}

	totalSpaces := width - totalWordWidth
	gaps := len(words) - 1

	if gaps == 0 || totalSpaces < gaps {
		return padRight(line, width)
	}

	baseSpaces := totalSpaces / gaps

	// Don't justify if gaps would be too wide - looks ugly
	if baseSpaces > 3 {
		return padRight(line, width)
	}

	extraSpaces := totalSpaces % gaps

	var sb strings.Builder
	for i, word := range words {
		sb.WriteString(word)
		if i < gaps {
			spaces := baseSpaces
			if i < extraSpaces {
				spaces++
			}
			sb.WriteString(strings.Repeat(" ", spaces))
		}
	}

	return sb.String()
}

// AlignText aligns text according to the specified alignment.
func AlignText(text string, width int, align Alignment) string {
	textWidth := StringWidth(text)
	if textWidth >= width {
		return TruncateToWidth(text, width)
	}

	switch align {
	case AlignRight:
		return strings.Repeat(" ", width-textWidth) + text
	case AlignCenter:
		left := (width - textWidth) / 2
		right := width - textWidth - left
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", right)
	case AlignJustify:
		return JustifyLine(text, width)
	default:
		return text + strings.Repeat(" ", width-textWidth)
	}
}

// TruncateToWidth truncates a string to fit within the specified width.
func TruncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	width := 0
	for i, r := range s {
		charWidth := UnicodeWidth(r)
		if width+charWidth > maxWidth {
			return s[:i]
		}
		width += charWidth
	}

	return s
}

// WrapAndJustify wraps text and justifies all lines except the last.
func WrapAndJustify(text string, width int) []string {
	lines := WrapText(text, width)
	for i := 0; i < len(lines)-1; i++ {
		if len(strings.TrimSpace(lines[i])) > 0 && StringWidth(lines[i]) < width {
			lines[i] = JustifyLine(lines[i], width)
		}
	}
	return lines
}

func padRight(s string, width int) string {
	sWidth := StringWidth(s)
	if sWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-sWidth)
}

// Truncate truncates a string adding ellipsis if needed.
func Truncate(s string, width int) string {
	sWidth := StringWidth(s)
	if sWidth <= width {
		return s
	}
	if width <= 3 {
		return TruncateToWidth(s, width)
	}
	return TruncateToWidth(s, width-3) + "..."
}

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	var sb strings.Builder
	inEscape := false

	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		sb.WriteRune(r)
	}

	return sb.String()
}

// IsBlank returns true if the string contains only whitespace.
func IsBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
