package lineedit

// VimMode represents the current vim mode.
type VimMode int

const (
	VimNormal VimMode = iota
	VimInsert
	VimOperatorPending // Waiting for motion after d, c, y, etc.
)

// VimOperator represents a pending operator.
type VimOperator int

const (
	OpNone VimOperator = iota
	OpDelete           // d
	OpChange           // c
	OpYank             // y (for future use)
)

// VimScheme implements vim-style modal keybindings.
type VimScheme struct {
	mode           VimMode
	operator       VimOperator // Pending operator (d, c, y)
	count          int         // Numeric prefix (0 = no count)
	countBuf       string      // Buffer for building count
	textObjPending bool        // Waiting for text object type (w, ", ', etc.)
	textObjInner   bool        // true = inner (i), false = around (a)
}

// NewVimScheme creates a new vim keybinding scheme in normal mode.
func NewVimScheme() *VimScheme {
	return &VimScheme{mode: VimNormal}
}

// Name returns the scheme name.
func (s *VimScheme) Name() string {
	return "vim"
}

// Mode returns the current vim mode.
func (s *VimScheme) Mode() VimMode {
	return s.mode
}

// InInsertMode returns true if in insert mode.
func (s *VimScheme) InInsertMode() bool {
	return s.mode == VimInsert
}

// HandleKey processes a key press using vim keybindings.
func (s *VimScheme) HandleKey(e *Editor, buf []byte, n int) Event {
	if n == 0 {
		return Event{}
	}

	if s.mode == VimInsert {
		return s.handleInsertMode(e, buf, n)
	}
	return s.handleNormalMode(e, buf, n)
}

func (s *VimScheme) handleInsertMode(e *Editor, buf []byte, n int) Event {
	// Check for escape sequences (arrow keys)
	if buf[0] == 27 && n >= 2 {
		if n >= 3 && buf[1] == '[' {
			switch buf[2] {
			case 'C': // Right
				e.Right()
				return Event{Consumed: true}
			case 'D': // Left
				e.Left()
				return Event{Consumed: true}
			}
		}
		return Event{Consumed: false}
	}

	switch {
	case n == 1 && buf[0] == 27: // Escape - return to normal mode
		s.mode = VimNormal
		// Move cursor back one (vim behavior on escape)
		e.Left()
		return Event{Consumed: true}

	case buf[0] == 13: // Enter
		return Event{Consumed: true, Submit: true}

	case buf[0] == 127 || buf[0] == 8: // Backspace
		if e.DeleteBackward() {
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}

	case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
		e.Insert(buf[0])
		return Event{Consumed: true, TextChanged: true}

	// Ctrl keys that work in insert mode
	case buf[0] == 23: // Ctrl+W - delete word
		e.DeleteWordBackward()
		return Event{Consumed: true, TextChanged: true}

	case buf[0] == 21: // Ctrl+U - delete to start
		e.KillToStart()
		return Event{Consumed: true, TextChanged: true}
	}

	return Event{Consumed: false}
}

// resetState clears count and operator state.
func (s *VimScheme) resetState() {
	s.operator = OpNone
	s.count = 0
	s.countBuf = ""
	s.textObjPending = false
	s.textObjInner = false
}

// getCount returns the effective count (1 if no count specified).
func (s *VimScheme) getCount() int {
	if s.count == 0 {
		return 1
	}
	return s.count
}

func (s *VimScheme) handleNormalMode(e *Editor, buf []byte, n int) Event {
	// Check for escape sequences (arrow keys for cursor movement)
	if buf[0] == 27 && n >= 3 && buf[1] == '[' {
		s.resetState()
		switch buf[2] {
		case 'C': // Right
			e.Right()
			return Event{Consumed: true}
		case 'D': // Left
			e.Left()
			return Event{Consumed: true}
		}
		return Event{Consumed: false}
	}

	ch := buf[0]

	// Escape always cancels pending operation or exits
	if n == 1 && ch == 27 {
		if s.mode == VimOperatorPending || s.operator != OpNone || s.count != 0 {
			s.resetState()
			s.mode = VimNormal
			return Event{Consumed: true}
		}
		return Event{Consumed: true, Cancel: true}
	}

	// Enter always submits
	if ch == 13 {
		s.resetState()
		return Event{Consumed: true, Submit: true}
	}

	// Handle count prefix (digits 1-9, or 0 if count already started)
	if ch >= '1' && ch <= '9' || (ch == '0' && s.countBuf != "") {
		s.countBuf += string(ch)
		// Parse count
		count := 0
		for _, d := range s.countBuf {
			count = count*10 + int(d-'0')
		}
		s.count = count
		return Event{Consumed: true}
	}

	// Handle operators (d, c, y)
	if ch == 'd' || ch == 'c' || ch == 'y' {
		// If we already have an operator pending, check for doubled operator (dd, cc, yy)
		if s.mode == VimOperatorPending {
			if (ch == 'd' && s.operator == OpDelete) ||
				(ch == 'c' && s.operator == OpChange) ||
				(ch == 'y' && s.operator == OpYank) {
				// Doubled operator - operate on whole line
				return s.executeLineOperation(e)
			}
			// Different operator - reset and start fresh
			s.resetState()
		}

		// Set operator and enter operator-pending mode
		switch ch {
		case 'd':
			s.operator = OpDelete
		case 'c':
			s.operator = OpChange
		case 'y':
			s.operator = OpYank
		}
		s.mode = VimOperatorPending
		return Event{Consumed: true}
	}

	// Handle text objects when we're waiting for the object type
	if s.textObjPending {
		if ev, handled := s.handleTextObject(e, ch); handled {
			return ev
		}
		// Invalid text object key - reset
		s.resetState()
		s.mode = VimNormal
		return Event{Consumed: true}
	}

	// Handle 'i' and 'a' as text object prefix when operator is pending
	if s.mode == VimOperatorPending && (ch == 'i' || ch == 'a') {
		s.textObjPending = true
		s.textObjInner = (ch == 'i')
		return Event{Consumed: true}
	}

	// Handle motions - these work differently based on whether we have a pending operator
	if ev, handled := s.handleMotion(e, ch); handled {
		return ev
	}

	// Insert mode entry commands (not valid with pending operator)
	if s.mode != VimOperatorPending {
		switch ch {
		case 'i': // Insert before cursor
			s.resetState()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'a': // Append after cursor
			s.resetState()
			e.Right()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'I': // Insert at beginning
			s.resetState()
			e.Home()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'A': // Append at end
			s.resetState()
			e.End()
			s.mode = VimInsert
			return Event{Consumed: true}

		// Simple deletions (not motions)
		case 'x': // Delete char at cursor
			cnt := s.getCount()
			s.resetState()
			changed := false
			for i := 0; i < cnt; i++ {
				if e.DeleteForward() {
					changed = true
				}
			}
			return Event{Consumed: true, TextChanged: changed}

		case 'X': // Delete char before cursor
			cnt := s.getCount()
			s.resetState()
			changed := false
			for i := 0; i < cnt; i++ {
				if e.DeleteBackward() {
					changed = true
				}
			}
			return Event{Consumed: true, TextChanged: changed}

		case 'D': // Delete to end of line
			s.resetState()
			e.KillToEnd()
			return Event{Consumed: true, TextChanged: true}

		case 'C': // Change to end of line (delete + insert)
			s.resetState()
			e.KillToEnd()
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}

		case 'S': // Substitute line (clear + insert)
			s.resetState()
			e.Clear()
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}

		case 's': // Substitute char (delete + insert)
			cnt := s.getCount()
			s.resetState()
			for i := 0; i < cnt; i++ {
				e.DeleteForward()
			}
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}
		}
	}

	// Unknown key - reset state if we had operator pending
	if s.mode == VimOperatorPending {
		s.resetState()
		s.mode = VimNormal
		return Event{Consumed: true}
	}

	// j/k/g/G etc. are NOT consumed - let browser handle scrolling
	s.resetState()
	return Event{Consumed: false}
}

// handleMotion processes motion commands, with or without a pending operator.
func (s *VimScheme) handleMotion(e *Editor, ch byte) (Event, bool) {
	cnt := s.getCount()

	// Define what each motion does
	var motion func()
	var motionType string // "exclusive" or "inclusive"

	switch ch {
	case 'h': // Left
		motion = func() { e.Left() }
		motionType = "exclusive"
	case 'l': // Right
		motion = func() { e.Right() }
		motionType = "exclusive"
	case 'w': // Word forward
		motion = func() { e.WordRight() }
		motionType = "exclusive"
	case 'b': // Word backward
		motion = func() { e.WordLeft() }
		motionType = "exclusive"
	case 'e': // Word end
		motion = func() { e.WordEnd() }
		motionType = "inclusive"
	case '0': // Beginning of line
		motion = func() { e.Home() }
		motionType = "exclusive"
		cnt = 1 // 0 ignores count
	case '$': // End of line
		motion = func() { e.End() }
		motionType = "inclusive"
		cnt = 1 // $ ignores count
	case '^': // First non-blank (treat same as 0 for now)
		motion = func() { e.Home() }
		motionType = "exclusive"
		cnt = 1
	default:
		return Event{}, false
	}

	// If no operator pending, just execute the motion
	if s.mode != VimOperatorPending {
		s.resetState()
		for i := 0; i < cnt; i++ {
			motion()
		}
		return Event{Consumed: true}, true
	}

	// Operator pending - execute operator over motion range
	startPos := e.Cursor()

	// Execute motion to find end position
	for i := 0; i < cnt; i++ {
		motion()
	}
	endPos := e.Cursor()

	// For inclusive motions, include the character at end position
	if motionType == "inclusive" && endPos < len(e.Text()) {
		endPos++
	}

	// Ensure start < end (handle backward motions)
	if startPos > endPos {
		startPos, endPos = endPos, startPos
	}

	// Execute the operator
	ev, enterInsert := s.executeOperator(e, startPos, endPos)

	// Reset state and set appropriate mode
	s.resetState()
	if enterInsert {
		s.mode = VimInsert
	} else {
		s.mode = VimNormal
	}

	return ev, true
}

// executeOperator applies the current operator to the range [start, end).
// Returns the event and whether to enter insert mode after.
func (s *VimScheme) executeOperator(e *Editor, start, end int) (Event, bool) {
	text := e.Text()

	switch s.operator {
	case OpDelete:
		// Delete the range
		e.Set(text[:start] + text[end:])
		e.SetCursor(start)
		return Event{Consumed: true, TextChanged: true}, false

	case OpChange:
		// Delete the range and enter insert mode
		e.Set(text[:start] + text[end:])
		e.SetCursor(start)
		return Event{Consumed: true, TextChanged: true}, true

	case OpYank:
		// Yank doesn't modify text (would need a register system for proper yank)
		// For now, just reset cursor
		e.SetCursor(start)
		return Event{Consumed: true}, false
	}

	return Event{Consumed: true}, false
}

// executeLineOperation handles dd, cc, yy (operate on entire line).
func (s *VimScheme) executeLineOperation(e *Editor) Event {
	op := s.operator
	s.resetState()
	s.mode = VimNormal

	switch op {
	case OpDelete:
		e.Clear()
		return Event{Consumed: true, TextChanged: true}

	case OpChange:
		e.Clear()
		s.mode = VimInsert
		return Event{Consumed: true, TextChanged: true}

	case OpYank:
		// Yank line (would need register system)
		return Event{Consumed: true}
	}

	return Event{Consumed: true}
}

// SetMode explicitly sets the vim mode (useful for programmatic control).
func (s *VimScheme) SetMode(mode VimMode) {
	s.mode = mode
}

// handleTextObject processes text object commands (iw, aw, i", a", etc.)
func (s *VimScheme) handleTextObject(e *Editor, ch byte) (Event, bool) {
	text := e.Text()
	cursor := e.Cursor()
	var start, end int
	found := false

	switch ch {
	case 'w', 'W': // word / WORD
		start, end, found = findWordObject(text, cursor, s.textObjInner)
	case '"': // double quotes
		start, end, found = findQuoteObject(text, cursor, '"', s.textObjInner)
	case '\'': // single quotes
		start, end, found = findQuoteObject(text, cursor, '\'', s.textObjInner)
	case 'q': // any quote (smart: picks " or ' based on context)
		start, end, found = findAnyQuoteObject(text, cursor, s.textObjInner)
	case 's': // sentence
		start, end, found = findSentenceObject(text, cursor, s.textObjInner)
	default:
		return Event{}, false
	}

	if !found {
		// No text object found - reset and do nothing
		s.resetState()
		s.mode = VimNormal
		return Event{Consumed: true}, true
	}

	// Execute the operator on the text object range
	ev, enterInsert := s.executeOperator(e, start, end)

	// Reset state and set appropriate mode
	s.resetState()
	if enterInsert {
		s.mode = VimInsert
	} else {
		s.mode = VimNormal
	}

	return ev, true
}

// findWordObject finds the boundaries of a word text object.
// inner=true: just the word, inner=false: word + surrounding space
func findWordObject(text string, cursor int, inner bool) (start, end int, found bool) {
	if len(text) == 0 {
		return 0, 0, false
	}

	// Clamp cursor to valid range
	if cursor >= len(text) {
		cursor = len(text) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Check if cursor is on whitespace or word char
	onSpace := text[cursor] == ' '

	if onSpace {
		// Cursor on whitespace - select the whitespace region
		start = cursor
		end = cursor

		// Expand left to find start of whitespace
		for start > 0 && text[start-1] == ' ' {
			start--
		}
		// Expand right to find end of whitespace
		for end < len(text) && text[end] == ' ' {
			end++
		}

		if !inner {
			// "a whitespace" - include adjacent word
			// Prefer the word after, fall back to word before
			if end < len(text) {
				// Include word after
				for end < len(text) && text[end] != ' ' {
					end++
				}
			} else if start > 0 {
				// Include word before
				for start > 0 && text[start-1] != ' ' {
					start--
				}
			}
		}
	} else {
		// Cursor on word char - find word boundaries
		start = cursor
		end = cursor

		// Expand left to find start of word
		for start > 0 && text[start-1] != ' ' {
			start--
		}
		// Expand right to find end of word
		for end < len(text) && text[end] != ' ' {
			end++
		}

		if !inner {
			// "a word" - include trailing space (or leading if at end)
			if end < len(text) && text[end] == ' ' {
				// Include trailing whitespace
				for end < len(text) && text[end] == ' ' {
					end++
				}
			} else if start > 0 && text[start-1] == ' ' {
				// No trailing space - include leading whitespace
				for start > 0 && text[start-1] == ' ' {
					start--
				}
			}
		}
	}

	return start, end, true
}

// isSentenceEnd checks if the character is a sentence-ending punctuation.
func isSentenceEnd(ch byte) bool {
	return ch == '.' || ch == '!' || ch == '?'
}

// findSentenceObject finds the boundaries of a sentence text object.
// Sentences end with . ! or ? followed by whitespace or end of line.
// inner=true: just the sentence content, inner=false: include trailing whitespace
func findSentenceObject(text string, cursor int, inner bool) (start, end int, found bool) {
	if len(text) == 0 {
		return 0, 0, false
	}

	// Clamp cursor
	if cursor >= len(text) {
		cursor = len(text) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Find sentence boundaries around cursor
	// A sentence starts at: beginning of text, or after sentence-end punctuation + whitespace
	// A sentence ends at: sentence-end punctuation, or end of text

	// Find start of current sentence (scan backward)
	start = 0
	for i := cursor - 1; i >= 0; i-- {
		if isSentenceEnd(text[i]) {
			// Found end of previous sentence - start is after it + whitespace
			start = i + 1
			// Skip whitespace after punctuation
			for start < len(text) && text[start] == ' ' {
				start++
			}
			break
		}
	}

	// Find end of current sentence (scan forward)
	end = len(text)
	for i := cursor; i < len(text); i++ {
		if isSentenceEnd(text[i]) {
			// Found end of this sentence - include the punctuation
			end = i + 1
			break
		}
	}

	// For "a sentence", include trailing whitespace
	if !inner {
		// Include trailing whitespace
		for end < len(text) && text[end] == ' ' {
			end++
		}
		// If no trailing whitespace but there's leading whitespace, include that instead
		if end == len(text) || text[end-1] != ' ' {
			// Check if we consumed any trailing space
			hasTrailing := end > 0 && end <= len(text) && (end == len(text) || text[end] != ' ')
			if !hasTrailing && start > 0 {
				// Include leading whitespace instead
				for start > 0 && text[start-1] == ' ' {
					start--
				}
			}
		}
	}

	return start, end, true
}

// findAnyQuoteObject finds the boundaries of either " or ' quoted string.
// Picks the tightest fitting quote pair around cursor, or nearest if not inside any.
func findAnyQuoteObject(text string, cursor int, inner bool) (start, end int, found bool) {
	// Try both quote types
	dStart, dEnd, dFound := findQuoteObject(text, cursor, '"', inner)
	sStart, sEnd, sFound := findQuoteObject(text, cursor, '\'', inner)

	if !dFound && !sFound {
		return 0, 0, false
	}
	if dFound && !sFound {
		return dStart, dEnd, true
	}
	if sFound && !dFound {
		return sStart, sEnd, true
	}

	// Both found - check which one actually contains the cursor
	// For inner, we need to check the original quote positions
	dContains := cursorInQuotePair(text, cursor, '"')
	sContains := cursorInQuotePair(text, cursor, '\'')

	if dContains && !sContains {
		return dStart, dEnd, true
	}
	if sContains && !dContains {
		return sStart, sEnd, true
	}

	// Both contain cursor (nested quotes) - pick the tighter one (smaller range)
	if dContains && sContains {
		dSize := dEnd - dStart
		sSize := sEnd - sStart
		if dSize <= sSize {
			return dStart, dEnd, true
		}
		return sStart, sEnd, true
	}

	// Neither contains cursor - pick the nearest one
	// Use the start position to determine which is closer
	dDist := abs(dStart - cursor)
	sDist := abs(sStart - cursor)
	if dDist <= sDist {
		return dStart, dEnd, true
	}
	return sStart, sEnd, true
}

// cursorInQuotePair checks if cursor is within a quote pair of the given type.
func cursorInQuotePair(text string, cursor int, quote byte) bool {
	var quotes []int
	for i := 0; i < len(text); i++ {
		if text[i] == quote {
			quotes = append(quotes, i)
		}
	}
	for i := 0; i+1 < len(quotes); i += 2 {
		if cursor >= quotes[i] && cursor <= quotes[i+1] {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// findQuoteObject finds the boundaries of a quoted string text object.
// inner=true: just the contents, inner=false: include the quotes
func findQuoteObject(text string, cursor int, quote byte, inner bool) (start, end int, found bool) {
	if len(text) == 0 {
		return 0, 0, false
	}

	// Find all quote positions
	var quotes []int
	for i := 0; i < len(text); i++ {
		if text[i] == quote {
			quotes = append(quotes, i)
		}
	}

	// Need at least 2 quotes
	if len(quotes) < 2 {
		return 0, 0, false
	}

	// Find the quote pair that contains or is nearest to cursor
	// Strategy: find pairs (0,1), (2,3), etc. and check if cursor is inside
	for i := 0; i+1 < len(quotes); i += 2 {
		qStart := quotes[i]
		qEnd := quotes[i+1]

		// Check if cursor is within this quote pair (inclusive of quotes)
		if cursor >= qStart && cursor <= qEnd {
			if inner {
				// Inner: just the content between quotes
				return qStart + 1, qEnd, true
			}
			// Around: include the quotes
			return qStart, qEnd + 1, true
		}
	}

	// Cursor not inside any quotes - check if we're between quote pairs
	// or to the left/right of all quotes. Try to find nearest pair.
	// For simplicity, if cursor is before first pair, use first pair.
	// If between pairs or after, find the next pair after cursor.
	for i := 0; i+1 < len(quotes); i += 2 {
		qStart := quotes[i]
		qEnd := quotes[i+1]

		if cursor < qStart {
			// Cursor is before this pair - use this pair
			if inner {
				return qStart + 1, qEnd, true
			}
			return qStart, qEnd + 1, true
		}
	}

	// Cursor is after all pairs - use last pair
	qStart := quotes[len(quotes)-2]
	qEnd := quotes[len(quotes)-1]
	if inner {
		return qStart + 1, qEnd, true
	}
	return qStart, qEnd + 1, true
}
