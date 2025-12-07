// Package lineedit provides a simple line editor with emacs-style keybindings.
package lineedit

// editorState represents a snapshot of editor state for undo.
type editorState struct {
	text   []byte
	cursor int
}

// Editor is a simple single-line text editor with cursor tracking.
type Editor struct {
	text    []byte
	cursor  int
	history []editorState // Undo history stack
	maxHist int           // Maximum history size (0 = unlimited)
}

// New creates a new empty Editor.
func New() *Editor {
	return &Editor{}
}

// Text returns the current text.
func (e *Editor) Text() string {
	return string(e.text)
}

// Cursor returns the current cursor position.
func (e *Editor) Cursor() int {
	return e.cursor
}

// SetCursor sets the cursor position, clamping to valid range.
func (e *Editor) SetCursor(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(e.text) {
		pos = len(e.text)
	}
	e.cursor = pos
}

// Len returns the length of the text.
func (e *Editor) Len() int {
	return len(e.text)
}

// Clear resets the editor to empty state.
func (e *Editor) Clear() {
	e.text = e.text[:0]
	e.cursor = 0
}

// Set replaces the text and moves cursor to end.
func (e *Editor) Set(text string) {
	e.text = []byte(text)
	e.cursor = len(e.text)
}

// SaveState saves the current state to the undo history.
// Call this before making changes that should be undoable.
func (e *Editor) SaveState() {
	// Don't save if state is identical to last saved state
	if len(e.history) > 0 {
		last := e.history[len(e.history)-1]
		if last.cursor == e.cursor && string(last.text) == string(e.text) {
			return
		}
	}

	// Make a copy of the text
	textCopy := make([]byte, len(e.text))
	copy(textCopy, e.text)

	e.history = append(e.history, editorState{
		text:   textCopy,
		cursor: e.cursor,
	})

	// Trim history if needed
	if e.maxHist > 0 && len(e.history) > e.maxHist {
		e.history = e.history[1:]
	}
}

// Undo restores the previous state from the undo history.
// Returns true if undo was performed, false if history is empty.
func (e *Editor) Undo() bool {
	if len(e.history) == 0 {
		return false
	}

	// Pop the last state
	last := e.history[len(e.history)-1]
	e.history = e.history[:len(e.history)-1]

	// Restore state
	e.text = last.text
	e.cursor = last.cursor

	return true
}

// ClearHistory clears the undo history.
func (e *Editor) ClearHistory() {
	e.history = e.history[:0]
}

// SetMaxHistory sets the maximum undo history size (0 = unlimited).
func (e *Editor) SetMaxHistory(max int) {
	e.maxHist = max
}

// BeforeCursor returns text before the cursor.
func (e *Editor) BeforeCursor() string {
	return string(e.text[:e.cursor])
}

// AfterCursor returns text from cursor to end.
func (e *Editor) AfterCursor() string {
	return string(e.text[e.cursor:])
}

// Insert adds a character at the cursor position.
func (e *Editor) Insert(ch byte) {
	e.text = append(e.text, 0)
	copy(e.text[e.cursor+1:], e.text[e.cursor:])
	e.text[e.cursor] = ch
	e.cursor++
}

// InsertString adds a string at the cursor position.
func (e *Editor) InsertString(s string) {
	for i := 0; i < len(s); i++ {
		e.Insert(s[i])
	}
}

// DeleteBackward removes the character before the cursor (backspace).
// Returns true if a character was deleted.
func (e *Editor) DeleteBackward() bool {
	if e.cursor == 0 {
		return false
	}
	e.text = append(e.text[:e.cursor-1], e.text[e.cursor:]...)
	e.cursor--
	return true
}

// DeleteForward removes the character at the cursor (delete).
// Returns true if a character was deleted.
func (e *Editor) DeleteForward() bool {
	if e.cursor >= len(e.text) {
		return false
	}
	e.text = append(e.text[:e.cursor], e.text[e.cursor+1:]...)
	return true
}

// Left moves cursor one character left.
// Returns true if cursor moved.
func (e *Editor) Left() bool {
	if e.cursor == 0 {
		return false
	}
	e.cursor--
	return true
}

// Right moves cursor one character right.
// Returns true if cursor moved.
func (e *Editor) Right() bool {
	if e.cursor >= len(e.text) {
		return false
	}
	e.cursor++
	return true
}

// Home moves cursor to beginning of line.
func (e *Editor) Home() {
	e.cursor = 0
}

// End moves cursor to end of line.
func (e *Editor) End() {
	e.cursor = len(e.text)
}

// wordBoundaryLeft finds the position of the previous word boundary.
func (e *Editor) wordBoundaryLeft() int {
	if e.cursor == 0 {
		return 0
	}
	i := e.cursor - 1
	// Skip spaces
	for i > 0 && e.text[i] == ' ' {
		i--
	}
	// Skip word chars
	for i > 0 && e.text[i-1] != ' ' {
		i--
	}
	return i
}

// wordBoundaryRight finds the position of the next word boundary.
func (e *Editor) wordBoundaryRight() int {
	if e.cursor >= len(e.text) {
		return len(e.text)
	}
	i := e.cursor
	// Skip current word
	for i < len(e.text) && e.text[i] != ' ' {
		i++
	}
	// Skip spaces
	for i < len(e.text) && e.text[i] == ' ' {
		i++
	}
	return i
}

// WordLeft moves cursor to the previous word boundary.
func (e *Editor) WordLeft() {
	e.cursor = e.wordBoundaryLeft()
}

// WordRight moves cursor to the next word boundary.
func (e *Editor) WordRight() {
	e.cursor = e.wordBoundaryRight()
}

// wordEndRight finds the position of the end of the current/next word (vim 'e' motion).
func (e *Editor) wordEndRight() int {
	if e.cursor >= len(e.text)-1 {
		return len(e.text) - 1
	}
	i := e.cursor
	// If on a word char, move at least one position
	if i < len(e.text) && e.text[i] != ' ' {
		i++
	}
	// Skip any spaces
	for i < len(e.text) && e.text[i] == ' ' {
		i++
	}
	// Move to end of word
	for i < len(e.text)-1 && e.text[i+1] != ' ' {
		i++
	}
	if i < 0 {
		return 0
	}
	return i
}

// WordEnd moves cursor to the end of the current/next word (vim 'e' motion).
func (e *Editor) WordEnd() {
	if len(e.text) == 0 {
		return
	}
	e.cursor = e.wordEndRight()
}

// DeleteWordBackward deletes from cursor to previous word boundary (Ctrl+W).
func (e *Editor) DeleteWordBackward() {
	newPos := e.wordBoundaryLeft()
	e.text = append(e.text[:newPos], e.text[e.cursor:]...)
	e.cursor = newPos
}

// DeleteWordForward deletes from cursor to next word boundary (Alt+D).
func (e *Editor) DeleteWordForward() {
	newPos := e.wordBoundaryRight()
	e.text = append(e.text[:e.cursor], e.text[newPos:]...)
}

// KillToEnd deletes from cursor to end of line (Ctrl+K).
func (e *Editor) KillToEnd() {
	e.text = e.text[:e.cursor]
}

// KillToStart deletes from beginning to cursor (Ctrl+U).
func (e *Editor) KillToStart() {
	e.text = e.text[e.cursor:]
	e.cursor = 0
}

// Transpose swaps the character before cursor with the one at cursor (Ctrl+T).
// If at end, swaps the last two characters.
func (e *Editor) Transpose() {
	if e.cursor == 0 || len(e.text) < 2 {
		return
	}
	pos := e.cursor
	if pos == len(e.text) {
		pos-- // At end, transpose last two chars
	}
	if pos > 0 {
		e.text[pos-1], e.text[pos] = e.text[pos], e.text[pos-1]
		if e.cursor < len(e.text) {
			e.cursor++
		}
	}
}
