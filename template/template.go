// Package template provides a rich template engine for rendering extracted content.
// It wraps Go's text/template with custom functions designed for terminal output.
package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"
)

// Engine renders templates with terminal-friendly functions.
type Engine struct {
	funcs template.FuncMap
}

// New creates a new template engine with all built-in functions.
func New() *Engine {
	e := &Engine{}
	e.funcs = template.FuncMap{
		// Text formatting - all handle both strings and maps with "text" key
		"upper":    safeUpper,
		"lower":    safeLower,
		"title":    safeTitle,
		"trim":     safeTrim,
		"text":     getText, // Extract text from string or map
		"wrap":     wrap,
		"truncate": truncate,
		"indent":   indent,

		// Layout
		"hr":   hr,
		"pad":  pad,
		"box":  box,
		"cols": cols,

		// Content manipulation
		"limit":  limit,
		"skip":   skip,
		"first":  first,
		"last":   last,
		"join":   join,
		"split":  strings.Split,
		"concat": concat,

		// Conditionals
		"default":  defaultVal,
		"coalesce": coalesce,
		"empty":    empty,
		"notEmpty": notEmpty,

		// Terminal styling (markers that renderer interprets)
		"bold": bold,
		"dim":  dim,
		"link": link,

		// Utilities
		"repeat": strings.Repeat,
		"len":    length,
		"add":    add,
		"sub":    sub,
	}
	return e
}

// Render executes a template with the given data.
func (e *Engine) Render(tmpl string, data any) (string, error) {
	t, err := template.New("page").Funcs(e.funcs).Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Funcs returns the function map for external use.
func (e *Engine) Funcs() template.FuncMap {
	return e.funcs
}

// --- Text Extraction and Safe Functions ---

// getText extracts text from various types (string, map with "text" key, etc.)
func getText(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if title, ok := v["title"].(string); ok {
			return title
		}
		// Return first string value found
		for _, val := range v {
			if s, ok := val.(string); ok {
				return s
			}
		}
	case map[string]string:
		if text, ok := v["text"]; ok {
			return text
		}
	}
	return fmt.Sprintf("%v", val)
}

// safeUpper handles both strings and maps
func safeUpper(val any) string {
	return strings.ToUpper(getText(val))
}

// safeLower handles both strings and maps
func safeLower(val any) string {
	return strings.ToLower(getText(val))
}

// safeTitle handles both strings and maps
func safeTitle(val any) string {
	return strings.Title(getText(val))
}

// safeTrim handles both strings and maps
func safeTrim(val any) string {
	return strings.TrimSpace(getText(val))
}

// --- Text Formatting Functions ---

// wrap wraps text to the specified width.
func wrap(width int, val any) string {
	s := getText(val)
	if width <= 0 {
		width = 70
	}

	var result strings.Builder
	lines := strings.Split(s, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(wrapLine(line, width))
	}

	return result.String()
}

func wrapLine(line string, width int) string {
	if utf8.RuneCountInString(line) <= width {
		return line
	}

	var result strings.Builder
	words := strings.Fields(line)
	lineLen := 0

	for i, word := range words {
		wordLen := utf8.RuneCountInString(word)

		if lineLen+wordLen+1 > width && lineLen > 0 {
			result.WriteString("\n")
			lineLen = 0
		}

		if lineLen > 0 {
			result.WriteString(" ")
			lineLen++
		}

		result.WriteString(word)
		lineLen += wordLen

		_ = i // avoid unused warning
	}

	return result.String()
}

// truncate shortens text to n characters with ellipsis.
func truncate(n int, val any) string {
	s := getText(val)
	if n <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

// indent adds n spaces to the start of each line.
func indent(n int, val any) string {
	s := getText(val)
	if n <= 0 {
		return s
	}
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// --- Layout Functions ---

// hr creates a horizontal rule of the specified width using the given character.
func hr(width int, char string) string {
	if char == "" {
		char = "─"
	}
	if width <= 0 {
		width = 40
	}
	// Handle multi-byte characters
	r := []rune(char)
	if len(r) > 0 {
		return strings.Repeat(string(r[0]), width)
	}
	return strings.Repeat("─", width)
}

// pad creates n blank lines, or pads a string with spaces.
// pad(n) - n blank lines
// pad(n, s) - pad string s to n characters
func pad(args ...any) string {
	if len(args) == 0 {
		return "\n"
	}

	n := 1
	if num, ok := args[0].(int); ok {
		n = num
	}

	if len(args) == 1 {
		// Just blank lines
		if n <= 0 {
			return ""
		}
		return strings.Repeat("\n", n)
	}

	// Pad string to width
	s := getText(args[1])
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// box wraps content in a unicode box.
func box(width int, content string) string {
	if width <= 4 {
		width = 40
	}

	lines := strings.Split(content, "\n")
	innerWidth := width - 4 // Account for "│ " and " │"

	var result strings.Builder

	// Top border
	result.WriteString("┌")
	result.WriteString(strings.Repeat("─", width-2))
	result.WriteString("┐\n")

	// Content lines
	for _, line := range lines {
		// Wrap if needed
		wrapped := wrapLine(line, innerWidth)
		for _, wl := range strings.Split(wrapped, "\n") {
			result.WriteString("│ ")
			result.WriteString(wl)
			padding := innerWidth - utf8.RuneCountInString(wl)
			if padding > 0 {
				result.WriteString(strings.Repeat(" ", padding))
			}
			result.WriteString(" │\n")
		}
	}

	// Bottom border
	result.WriteString("└")
	result.WriteString(strings.Repeat("─", width-2))
	result.WriteString("┘")

	return result.String()
}

// cols arranges items in n columns.
func cols(n int, items []string) string {
	if n <= 0 || len(items) == 0 {
		return ""
	}

	// Calculate column width
	maxLen := 0
	for _, item := range items {
		if l := utf8.RuneCountInString(item); l > maxLen {
			maxLen = l
		}
	}
	colWidth := maxLen + 2

	var result strings.Builder
	for i, item := range items {
		if i > 0 && i%n == 0 {
			result.WriteString("\n")
		}
		result.WriteString(item)
		padding := colWidth - utf8.RuneCountInString(item)
		if padding > 0 && (i+1)%n != 0 {
			result.WriteString(strings.Repeat(" ", padding))
		}
	}

	return result.String()
}

// --- Content Manipulation ---

// limit returns the first n items from a slice.
func limit(n int, items any) any {
	switch v := items.(type) {
	case []any:
		if n >= len(v) {
			return v
		}
		return v[:n]
	case []string:
		if n >= len(v) {
			return v
		}
		return v[:n]
	case []map[string]any:
		if n >= len(v) {
			return v
		}
		return v[:n]
	default:
		return items
	}
}

// skip returns items after skipping the first n.
func skip(n int, items any) any {
	switch v := items.(type) {
	case []any:
		if n >= len(v) {
			return []any{}
		}
		return v[n:]
	case []string:
		if n >= len(v) {
			return []string{}
		}
		return v[n:]
	case []map[string]any:
		if n >= len(v) {
			return []map[string]any{}
		}
		return v[n:]
	default:
		return items
	}
}

// first returns the first item or nil.
func first(items any) any {
	switch v := items.(type) {
	case []any:
		if len(v) > 0 {
			return v[0]
		}
	case []string:
		if len(v) > 0 {
			return v[0]
		}
	case []map[string]any:
		if len(v) > 0 {
			return v[0]
		}
	}
	return nil
}

// last returns the last item or nil.
func last(items any) any {
	switch v := items.(type) {
	case []any:
		if len(v) > 0 {
			return v[len(v)-1]
		}
	case []string:
		if len(v) > 0 {
			return v[len(v)-1]
		}
	case []map[string]any:
		if len(v) > 0 {
			return v[len(v)-1]
		}
	}
	return nil
}

// join concatenates strings with a separator.
func join(sep string, items any) string {
	switch v := items.(type) {
	case []string:
		return strings.Join(v, sep)
	case []any:
		strs := make([]string, len(v))
		for i, item := range v {
			if s, ok := item.(string); ok {
				strs[i] = s
			}
		}
		return strings.Join(strs, sep)
	default:
		return ""
	}
}

// concat joins multiple strings.
func concat(parts ...string) string {
	return strings.Join(parts, "")
}

// --- Conditionals ---

// defaultVal returns the default if value is empty.
func defaultVal(def, val any) any {
	if empty(val) {
		return def
	}
	return val
}

// coalesce returns the first non-empty value.
func coalesce(vals ...any) any {
	for _, v := range vals {
		if !empty(v) {
			return v
		}
	}
	return nil
}

// empty checks if a value is empty.
func empty(val any) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case []string:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case bool:
		return !v
	case int:
		return v == 0
	}
	return false
}

// notEmpty is the inverse of empty.
func notEmpty(val any) bool {
	return !empty(val)
}

// --- Terminal Styling ---
// These return marker strings that the document renderer interprets.

// bold marks text as bold.
func bold(val any) string {
	s := getText(val)
	return "**" + s + "**"
}

// dim marks text as dimmed.
func dim(val any) string {
	s := getText(val)
	return "~~" + s + "~~"
}

// link outputs markdown-style linked text [text](href) that parseLine can convert to clickable links.
// Can take (text, href) or a single map with text and href keys.
func link(args ...any) string {
	var text, href string

	if len(args) == 1 {
		// Single argument - expect a map with text and href
		if m, ok := args[0].(map[string]any); ok {
			text = getText(m)
			if h, ok := m["href"].(string); ok {
				href = h
			}
		} else {
			text = getText(args[0])
		}
	} else if len(args) >= 2 {
		// Two arguments: text and href
		text = getText(args[0])
		href = getText(args[1])
	}

	// Output markdown format for parseLine to convert to clickable links
	if href != "" {
		return "[" + text + "](" + href + ")"
	}
	return text
}

// --- Utilities ---

// length returns the length of a string or slice.
func length(val any) int {
	switch v := val.(type) {
	case string:
		return utf8.RuneCountInString(v)
	case []any:
		return len(v)
	case []string:
		return len(v)
	case []map[string]any:
		return len(v)
	default:
		return 0
	}
}

// add returns a + b.
func add(a, b int) int {
	return a + b
}

// sub returns a - b.
func sub(a, b int) int {
	return a - b
}
