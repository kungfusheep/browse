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

// VimFindType represents the type of character find motion.
type VimFindType int

const (
	FindNone VimFindType = iota
	FindForward        // f - find forward (inclusive)
	FindBackward       // F - find backward (inclusive)
	FindForwardBefore  // t - find forward, stop before (exclusive)
	FindBackwardAfter  // T - find backward, stop after (exclusive)
)

// VimChangeType represents the type of change for repeat (.).
type VimChangeType int

const (
	ChangeNone VimChangeType = iota
	ChangeSimple             // x, X, D, S, dd, cc
	ChangeMotion             // dw, de, cw, etc.
	ChangeTextObj            // diw, ciq, etc.
	ChangeFind               // df., ct", etc.
	ChangeReplace            // r
	ChangeInsert             // i, a, I, A (insert typed text)
)

// lastChange records the last change for repeat (.).
type lastChange struct {
	changeType VimChangeType
	count      int         // count used
	operator   VimOperator // d, c, y
	motion     byte        // w, e, $, 0, etc.
	findType   VimFindType // f, F, t, T
	findChar   byte        // target character for find
	textObj    byte        // w, ", ', q, s, etc.
	inner      bool        // inner vs around for text objects
	replaceChar byte       // replacement char for r
	simpleCmd  byte        // x, X, D, S
	insertText string      // text typed in insert mode (for change commands)
}

// VimScheme implements vim-style modal keybindings.
type VimScheme struct {
	mode           VimMode
	operator       VimOperator // Pending operator (d, c, y)
	count          int         // Numeric prefix (0 = no count)
	countBuf       string      // Buffer for building count
	textObjPending bool        // Waiting for text object type (w, ", ', etc.)
	textObjInner   bool        // true = inner (i), false = around (a)
	replacePending bool        // Waiting for replacement character (after 'r')
	findPending    VimFindType // Waiting for find target character

	// For recording changes (for . repeat)
	lastChange     lastChange // The last recorded change
	insertBuffer   string     // Buffer for text typed in insert mode
	recording      bool       // Whether we're recording insert text
}

// NewVimScheme creates a new vim keybinding scheme in normal mode.
func NewVimScheme() *VimScheme {
	return &VimScheme{mode: VimNormal}
}

// recordSimpleChange records a simple command (x, X, D, S) for repeat.
func (s *VimScheme) recordSimpleChange(cmd byte, count int) {
	s.lastChange = lastChange{
		changeType: ChangeSimple,
		count:      count,
		simpleCmd:  cmd,
	}
}

// recordMotionChange records an operator+motion change for repeat.
func (s *VimScheme) recordMotionChange(op VimOperator, motion byte, count int) {
	s.lastChange = lastChange{
		changeType: ChangeMotion,
		count:      count,
		operator:   op,
		motion:     motion,
	}
}

// recordTextObjChange records an operator+text object change for repeat.
func (s *VimScheme) recordTextObjChange(op VimOperator, obj byte, inner bool, count int) {
	s.lastChange = lastChange{
		changeType: ChangeTextObj,
		count:      count,
		operator:   op,
		textObj:    obj,
		inner:      inner,
	}
}

// recordFindChange records an operator+find change for repeat.
func (s *VimScheme) recordFindChange(op VimOperator, findType VimFindType, findChar byte, count int) {
	s.lastChange = lastChange{
		changeType: ChangeFind,
		count:      count,
		operator:   op,
		findType:   findType,
		findChar:   findChar,
	}
}

// recordReplaceChange records a replace command for repeat.
func (s *VimScheme) recordReplaceChange(replaceChar byte, count int) {
	s.lastChange = lastChange{
		changeType:  ChangeReplace,
		count:       count,
		replaceChar: replaceChar,
	}
}

// startInsertRecording starts recording text for an insert-type change.
func (s *VimScheme) startInsertRecording(cmd byte, count int) {
	s.lastChange = lastChange{
		changeType: ChangeInsert,
		count:      count,
		simpleCmd:  cmd,
	}
	s.insertBuffer = ""
	s.recording = true
}

// startChangeRecording starts recording for a change operator.
func (s *VimScheme) startChangeRecording() {
	s.insertBuffer = ""
	s.recording = true
}

// repeatLastChange replays the last recorded change.
func (s *VimScheme) repeatLastChange(e *Editor) Event {
	lc := s.lastChange
	if lc.changeType == ChangeNone {
		return Event{Consumed: true}
	}

	// Use . count if provided, otherwise use original count
	cnt := s.getCount()
	if cnt == 1 && lc.count > 1 {
		cnt = lc.count
	}
	s.resetState()

	switch lc.changeType {
	case ChangeSimple:
		e.SaveState()
		switch lc.simpleCmd {
		case 'x':
			for i := 0; i < cnt; i++ {
				e.DeleteForward()
			}
		case 'X':
			for i := 0; i < cnt; i++ {
				e.DeleteBackward()
			}
		case 'D':
			e.KillToEnd()
		case 'S':
			e.Clear()
			e.InsertString(lc.insertText)
		case 's':
			for i := 0; i < cnt; i++ {
				e.DeleteForward()
			}
			e.InsertString(lc.insertText)
		case 'C':
			e.KillToEnd()
			e.InsertString(lc.insertText)
		}
		return Event{Consumed: true, TextChanged: true}

	case ChangeMotion:
		e.SaveState()
		return s.replayMotionChange(e, lc, cnt)

	case ChangeTextObj:
		e.SaveState()
		return s.replayTextObjChange(e, lc)

	case ChangeFind:
		e.SaveState()
		return s.replayFindChange(e, lc, cnt)

	case ChangeReplace:
		e.SaveState()
		for i := 0; i < cnt && e.Cursor() < e.Len(); i++ {
			e.DeleteForward()
			e.Insert(lc.replaceChar)
		}
		e.Left()
		return Event{Consumed: true, TextChanged: true}

	case ChangeInsert:
		e.SaveState()
		// Move cursor based on original insert command
		switch lc.simpleCmd {
		case 'a':
			e.Right()
		case 'I':
			e.Home()
		case 'A':
			e.End()
		// 'i' - no movement needed
		}
		e.InsertString(lc.insertText)
		return Event{Consumed: true, TextChanged: true}
	}

	return Event{Consumed: true}
}

// replayMotionChange replays an operator+motion change.
func (s *VimScheme) replayMotionChange(e *Editor, lc lastChange, cnt int) Event {
	// Define motion function
	var motion func()
	var motionType string

	switch lc.motion {
	case 'h':
		motion = func() { e.Left() }
		motionType = "exclusive"
	case 'l':
		motion = func() { e.Right() }
		motionType = "exclusive"
	case 'w':
		motion = func() { e.WordRight() }
		motionType = "exclusive"
	case 'b':
		motion = func() { e.WordLeft() }
		motionType = "exclusive"
	case 'e':
		motion = func() { e.WordEnd() }
		motionType = "inclusive"
	case 'W':
		motion = func() { e.BigWordRight() }
		motionType = "exclusive"
	case 'B':
		motion = func() { e.BigWordLeft() }
		motionType = "exclusive"
	case 'E':
		motion = func() { e.BigWordEnd() }
		motionType = "inclusive"
	case '0':
		motion = func() { e.Home() }
		motionType = "exclusive"
		cnt = 1
	case '$':
		motion = func() { e.End() }
		motionType = "inclusive"
		cnt = 1
	case '^':
		motion = func() { e.Home() }
		motionType = "exclusive"
		cnt = 1
	default:
		return Event{Consumed: true}
	}

	startPos := e.Cursor()
	for i := 0; i < cnt; i++ {
		motion()
	}
	endPos := e.Cursor()

	if motionType == "inclusive" && endPos < len(e.Text()) {
		endPos++
	}
	if startPos > endPos {
		startPos, endPos = endPos, startPos
	}

	text := e.Text()
	switch lc.operator {
	case OpDelete:
		e.Set(text[:startPos] + text[endPos:])
		e.SetCursor(startPos)
	case OpChange:
		e.Set(text[:startPos] + text[endPos:])
		e.SetCursor(startPos)
		e.InsertString(lc.insertText)
	}

	return Event{Consumed: true, TextChanged: true}
}

// replayTextObjChange replays an operator+text object change.
func (s *VimScheme) replayTextObjChange(e *Editor, lc lastChange) Event {
	text := e.Text()
	cursor := e.Cursor()
	var start, end int
	found := false

	switch lc.textObj {
	case 'w', 'W':
		start, end, found = findWordObject(text, cursor, lc.inner)
	case '"':
		start, end, found = findQuoteObject(text, cursor, '"', lc.inner)
	case '\'':
		start, end, found = findQuoteObject(text, cursor, '\'', lc.inner)
	case 'q':
		start, end, found = findAnyQuoteObject(text, cursor, lc.inner)
	case 's':
		start, end, found = findSentenceObject(text, cursor, lc.inner)
	}

	if !found {
		return Event{Consumed: true}
	}

	switch lc.operator {
	case OpDelete:
		e.Set(text[:start] + text[end:])
		e.SetCursor(start)
	case OpChange:
		e.Set(text[:start] + text[end:])
		e.SetCursor(start)
		e.InsertString(lc.insertText)
	}

	return Event{Consumed: true, TextChanged: true}
}

// replayFindChange replays an operator+find change.
func (s *VimScheme) replayFindChange(e *Editor, lc lastChange, cnt int) Event {
	text := e.Text()
	cursor := e.Cursor()
	targetPos := -1

	switch lc.findType {
	case FindForward, FindForwardBefore:
		pos := cursor + 1
		found := 0
		for pos < len(text) {
			if text[pos] == lc.findChar {
				found++
				if found == cnt {
					targetPos = pos
					break
				}
			}
			pos++
		}
	case FindBackward, FindBackwardAfter:
		pos := cursor - 1
		found := 0
		for pos >= 0 {
			if text[pos] == lc.findChar {
				found++
				if found == cnt {
					targetPos = pos
					break
				}
			}
			pos--
		}
	}

	if targetPos == -1 {
		return Event{Consumed: true}
	}

	startPos := cursor
	endPos := targetPos

	switch lc.findType {
	case FindForward:
		endPos = targetPos + 1
	case FindForwardBefore:
		endPos = targetPos
	case FindBackward:
		startPos = targetPos
		endPos = cursor
	case FindBackwardAfter:
		startPos = targetPos + 1
		endPos = cursor
	}

	if startPos > endPos {
		startPos, endPos = endPos, startPos
	}

	switch lc.operator {
	case OpDelete:
		e.Set(text[:startPos] + text[endPos:])
		e.SetCursor(startPos)
	case OpChange:
		e.Set(text[:startPos] + text[endPos:])
		e.SetCursor(startPos)
		e.InsertString(lc.insertText)
	case OpNone:
		// Just a motion, not repeated with .
		return Event{Consumed: true}
	}

	return Event{Consumed: true, TextChanged: true}
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
		// Finalize recording if we were recording insert text
		if s.recording {
			s.lastChange.insertText = s.insertBuffer
			s.insertBuffer = ""
			s.recording = false
		}
		// Move cursor back one (vim behavior on escape)
		e.Left()
		return Event{Consumed: true}

	case buf[0] == 13: // Enter
		return Event{Consumed: true, Submit: true}

	case buf[0] == 127 || buf[0] == 8: // Backspace
		if e.DeleteBackward() {
			// Record backspace in insert buffer
			if s.recording && len(s.insertBuffer) > 0 {
				s.insertBuffer = s.insertBuffer[:len(s.insertBuffer)-1]
			}
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}

	case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
		e.Insert(buf[0])
		// Record in insert buffer
		if s.recording {
			s.insertBuffer += string(buf[0])
		}
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
	s.replacePending = false
	s.findPending = FindNone
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

	// Handle find target character when we're waiting for it
	if s.findPending != FindNone {
		if ch >= 32 && ch < 127 { // Printable ASCII
			return s.executeFindMotion(e, ch)
		}
		// Non-printable - cancel find
		s.resetState()
		return Event{Consumed: true}
	}

	// Handle replacement character when we're waiting for it
	if s.replacePending {
		if ch >= 32 && ch < 127 { // Printable ASCII
			cnt := s.getCount()
			s.recordReplaceChange(ch, cnt)
			s.resetState()
			e.SaveState()
			// Replace cnt characters with the pressed character
			for i := 0; i < cnt && e.Cursor() < e.Len(); i++ {
				e.DeleteForward()
				e.Insert(ch)
			}
			// Move cursor back one (vim behavior: stay on last replaced char)
			e.Left()
			return Event{Consumed: true, TextChanged: true}
		}
		// Non-printable - cancel replacement
		s.resetState()
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
			cnt := s.getCount()
			s.startInsertRecording('i', cnt)
			s.resetState()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'a': // Append after cursor
			cnt := s.getCount()
			s.startInsertRecording('a', cnt)
			s.resetState()
			e.Right()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'I': // Insert at beginning
			cnt := s.getCount()
			s.startInsertRecording('I', cnt)
			s.resetState()
			e.Home()
			s.mode = VimInsert
			return Event{Consumed: true}

		case 'A': // Append at end
			cnt := s.getCount()
			s.startInsertRecording('A', cnt)
			s.resetState()
			e.End()
			s.mode = VimInsert
			return Event{Consumed: true}

		// Simple deletions (not motions)
		case 'x': // Delete char at cursor
			cnt := s.getCount()
			s.recordSimpleChange('x', cnt)
			s.resetState()
			e.SaveState()
			changed := false
			for i := 0; i < cnt; i++ {
				if e.DeleteForward() {
					changed = true
				}
			}
			return Event{Consumed: true, TextChanged: changed}

		case 'X': // Delete char before cursor
			cnt := s.getCount()
			s.recordSimpleChange('X', cnt)
			s.resetState()
			e.SaveState()
			changed := false
			for i := 0; i < cnt; i++ {
				if e.DeleteBackward() {
					changed = true
				}
			}
			return Event{Consumed: true, TextChanged: changed}

		case 'D': // Delete to end of line
			s.recordSimpleChange('D', 1)
			s.resetState()
			e.SaveState()
			e.KillToEnd()
			return Event{Consumed: true, TextChanged: true}

		case 'C': // Change to end of line (delete + insert)
			cnt := s.getCount()
			s.lastChange = lastChange{
				changeType: ChangeSimple,
				count:      cnt,
				simpleCmd:  'C',
			}
			s.startChangeRecording()
			s.resetState()
			e.SaveState()
			e.KillToEnd()
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}

		case 'S': // Substitute line (clear + insert)
			cnt := s.getCount()
			s.lastChange = lastChange{
				changeType: ChangeSimple,
				count:      cnt,
				simpleCmd:  'S',
			}
			s.startChangeRecording()
			s.resetState()
			e.SaveState()
			e.Clear()
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}

		case 's': // Substitute char (delete + insert)
			cnt := s.getCount()
			s.lastChange = lastChange{
				changeType: ChangeSimple,
				count:      cnt,
				simpleCmd:  's',
			}
			s.startChangeRecording()
			s.resetState()
			e.SaveState()
			for i := 0; i < cnt; i++ {
				e.DeleteForward()
			}
			s.mode = VimInsert
			return Event{Consumed: true, TextChanged: true}

		case 'u': // Undo
			s.resetState()
			if e.Undo() {
				return Event{Consumed: true, TextChanged: true}
			}
			return Event{Consumed: true}

		case '.': // Repeat last change
			return s.repeatLastChange(e)

		case 'r': // Replace character - wait for replacement
			s.replacePending = true
			return Event{Consumed: true}

		}
	}

	// Find character motions - work in both normal and operator-pending modes
	switch ch {
	case 'f': // Find forward (inclusive)
		s.findPending = FindForward
		return Event{Consumed: true}

	case 'F': // Find backward (inclusive)
		s.findPending = FindBackward
		return Event{Consumed: true}

	case 't': // Find forward, stop before (exclusive)
		s.findPending = FindForwardBefore
		return Event{Consumed: true}

	case 'T': // Find backward, stop after (exclusive)
		s.findPending = FindBackwardAfter
		return Event{Consumed: true}
	}

	// Ctrl+R for redo (works in normal mode, outside the operator-pending check)
	if ch == 18 { // Ctrl+R
		s.resetState()
		if e.Redo() {
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}
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
	case 'W': // WORD forward (whitespace-separated)
		motion = func() { e.BigWordRight() }
		motionType = "exclusive"
	case 'B': // WORD backward (whitespace-separated)
		motion = func() { e.BigWordLeft() }
		motionType = "exclusive"
	case 'E': // WORD end (whitespace-separated)
		motion = func() { e.BigWordEnd() }
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
	// Record the change for repeat (.)
	op := s.operator
	s.recordMotionChange(op, ch, cnt)
	if op == OpChange {
		s.startChangeRecording()
	}

	// Save state BEFORE moving cursor (so undo restores to original position)
	e.SaveState()

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
// Note: SaveState should be called BEFORE this function (by caller).
func (s *VimScheme) executeOperator(e *Editor, start, end int) (Event, bool) {
	text := e.Text()

	switch s.operator {
	case OpDelete:
		e.Set(text[:start] + text[end:])
		e.SetCursor(start)
		return Event{Consumed: true, TextChanged: true}, false

	case OpChange:
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
		e.SaveState()
		e.Clear()
		return Event{Consumed: true, TextChanged: true}

	case OpChange:
		e.SaveState()
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

	// Record the change for repeat (.)
	op := s.operator
	s.recordTextObjChange(op, ch, s.textObjInner, s.getCount())
	if op == OpChange {
		s.startChangeRecording()
	}

	// Save state before executing operator
	e.SaveState()

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

// charClassAt returns the character class for a byte in a string.
func charClassAt(text string, i int) int {
	return charClass(text[i])
}

// findWordObject finds the boundaries of a word text object.
// inner=true: just the word, inner=false: word + surrounding space
// Uses character classes: whitespace (0), word chars (1), punctuation (2)
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

	// Get the character class at cursor position
	cursorClass := charClassAt(text, cursor)

	if cursorClass == 0 {
		// Cursor on whitespace - select the whitespace region
		start = cursor
		end = cursor

		// Expand left to find start of whitespace
		for start > 0 && charClassAt(text, start-1) == 0 {
			start--
		}
		// Expand right to find end of whitespace
		for end < len(text) && charClassAt(text, end) == 0 {
			end++
		}

		if !inner {
			// "a whitespace" - include adjacent word/punctuation
			// Prefer the text after, fall back to text before
			if end < len(text) {
				// Include next word/punctuation group
				nextClass := charClassAt(text, end)
				for end < len(text) && charClassAt(text, end) == nextClass {
					end++
				}
			} else if start > 0 {
				// Include previous word/punctuation group
				prevClass := charClassAt(text, start-1)
				for start > 0 && charClassAt(text, start-1) == prevClass {
					start--
				}
			}
		}
	} else {
		// Cursor on word char or punctuation - find boundaries of same class
		start = cursor
		end = cursor

		// Expand left to find start of this class
		for start > 0 && charClassAt(text, start-1) == cursorClass {
			start--
		}
		// Expand right to find end of this class
		for end < len(text) && charClassAt(text, end) == cursorClass {
			end++
		}

		if !inner {
			// "a word" - include trailing whitespace (or leading if at end)
			if end < len(text) && charClassAt(text, end) == 0 {
				// Include trailing whitespace
				for end < len(text) && charClassAt(text, end) == 0 {
					end++
				}
			} else if start > 0 && charClassAt(text, start-1) == 0 {
				// No trailing whitespace - include leading whitespace
				for start > 0 && charClassAt(text, start-1) == 0 {
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

// executeFindMotion executes a find character motion (f/F/t/T).
// Works as a motion on its own or with a pending operator.
func (s *VimScheme) executeFindMotion(e *Editor, target byte) Event {
	findType := s.findPending
	cnt := s.getCount()
	hasOperator := s.mode == VimOperatorPending
	text := e.Text()
	cursor := e.Cursor()

	// Find the target character
	targetPos := -1

	switch findType {
	case FindForward, FindForwardBefore: // f, t - search forward
		pos := cursor + 1
		found := 0
		for pos < len(text) {
			if text[pos] == target {
				found++
				if found == cnt {
					targetPos = pos
					break
				}
			}
			pos++
		}

	case FindBackward, FindBackwardAfter: // F, T - search backward
		pos := cursor - 1
		found := 0
		for pos >= 0 {
			if text[pos] == target {
				found++
				if found == cnt {
					targetPos = pos
					break
				}
			}
			pos--
		}
	}

	// Target not found - reset and do nothing
	if targetPos == -1 {
		s.resetState()
		s.mode = VimNormal
		return Event{Consumed: true}
	}

	// Adjust for t/T (stop before/after the target)
	finalPos := targetPos
	if findType == FindForwardBefore && targetPos > cursor {
		finalPos = targetPos - 1
	} else if findType == FindBackwardAfter && targetPos < cursor {
		finalPos = targetPos + 1
	}

	// If no operator pending, just move the cursor
	if !hasOperator {
		s.resetState()
		e.SetCursor(finalPos)
		return Event{Consumed: true}
	}

	// Record the change for repeat (.)
	op := s.operator
	s.recordFindChange(op, findType, target, cnt)
	if op == OpChange {
		s.startChangeRecording()
	}

	// Operator pending - execute operator over the range
	e.SaveState()

	startPos := cursor
	endPos := targetPos

	// For forward motions, f is inclusive (include target), t is exclusive
	// For backward motions, F is inclusive, T is exclusive
	switch findType {
	case FindForward: // f - inclusive forward
		endPos = targetPos + 1
	case FindForwardBefore: // t - exclusive forward (stop before target)
		endPos = targetPos
	case FindBackward: // F - inclusive backward
		// endPos is targetPos, startPos is cursor
		startPos = targetPos
		endPos = cursor
	case FindBackwardAfter: // T - exclusive backward (stop after target)
		startPos = targetPos + 1
		endPos = cursor
	}

	// Ensure start < end
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

	return ev
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
