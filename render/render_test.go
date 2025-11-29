package render

import (
	"strings"
	"testing"
)

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		expected []string
	}{
		{"no wrap needed", "hello world", 20, []string{"hello world"}},
		{"simple wrap", "hello world foo bar", 11, []string{"hello world", "foo bar"}},
		{"multiple lines", "one two three four five six", 10, []string{"one two", "three four", "five six"}},
		{"preserves newlines", "first\n\nsecond", 20, []string{"first", "", "second"}},
		{"long word breaks", "supercalifragilisticexpialidocious", 10, []string{"supercalif", "ragilistic", "expialidoc", "ious"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WrapText(tt.text, tt.width)
			if len(result) != len(tt.expected) {
				t.Errorf("got %d lines, expected %d lines\ngot: %v\nexpected: %v",
					len(result), len(tt.expected), result, tt.expected)
				return
			}
			for i, line := range result {
				if line != tt.expected[i] {
					t.Errorf("line %d: got %q, expected %q", i, line, tt.expected[i])
				}
			}
		})
	}
}

func TestJustifyLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		width    int
		expected string
	}{
		{"two words", "hello world", 15, "hello     world"},
		{"three words even", "one two three", 16, "one   two  three"},
		{"single word stays left", "hello", 10, "hello     "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JustifyLine(tt.line, tt.width)
			if result != tt.expected {
				t.Errorf("got %q (len=%d), expected %q (len=%d)",
					result, len(result), tt.expected, len(tt.expected))
			}
		})
	}
}

func TestAlignText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		align    Alignment
		expected string
	}{
		{"left", "hello", 10, AlignLeft, "hello     "},
		{"right", "hello", 10, AlignRight, "     hello"},
		{"center", "hello", 10, AlignCenter, "  hello   "},
		{"center odd", "hi", 7, AlignCenter, "  hi   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AlignText(tt.text, tt.width, tt.align)
			if result != tt.expected {
				t.Errorf("got %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		width    int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"hello", 3, "hel"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := Truncate(tt.input, tt.width)
			if result != tt.expected {
				t.Errorf("Truncate(%q, %d) = %q, expected %q",
					tt.input, tt.width, result, tt.expected)
			}
		})
	}
}

func TestWrapAndJustify(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog"
	lines := WrapAndJustify(text, 20)

	for i := 0; i < len(lines)-1; i++ {
		if StringWidth(lines[i]) != 20 {
			t.Errorf("line %d has width %d, expected 20: %q", i, StringWidth(lines[i]), lines[i])
		}
	}

	rejoined := strings.Join(lines, " ")
	words := strings.Fields(rejoined)
	originalWords := strings.Fields(text)
	if len(words) != len(originalWords) {
		t.Errorf("word count mismatch: got %d, expected %d", len(words), len(originalWords))
	}
}

func TestCanvas(t *testing.T) {
	c := NewCanvas(10, 5)

	if c.Width() != 10 || c.Height() != 5 {
		t.Errorf("wrong dimensions: got %dx%d, expected 10x5", c.Width(), c.Height())
	}

	c.Set(0, 0, 'X', Style{})
	if c.Get(0, 0).Rune != 'X' {
		t.Error("Set/Get failed")
	}

	c.Set(-1, 0, 'Y', Style{})
	c.Set(100, 0, 'Y', Style{})
	if c.Get(-1, 0).Rune != ' ' {
		t.Error("out of bounds Set should be ignored")
	}
}

func TestTable(t *testing.T) {
	tbl := NewTable("Name", "Value")
	tbl.AddRow("foo", "bar")
	tbl.AddRow("longer", "x")

	s := tbl.RenderToString()
	if !strings.Contains(s, "Name") || !strings.Contains(s, "foo") {
		t.Error("table should contain headers and data")
	}
	if !strings.Contains(s, "┌") || !strings.Contains(s, "┘") {
		t.Error("table should have box drawing characters")
	}
}
