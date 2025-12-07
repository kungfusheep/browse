package lineedit

// EmacsScheme implements emacs-style keybindings.
// Always in "insert mode" - all printable characters go to the editor.
type EmacsScheme struct{}

// NewEmacsScheme creates a new emacs keybinding scheme.
func NewEmacsScheme() *EmacsScheme {
	return &EmacsScheme{}
}

// Name returns the scheme name.
func (s *EmacsScheme) Name() string {
	return "emacs"
}

// InInsertMode returns true - emacs is always ready for text input.
func (s *EmacsScheme) InInsertMode() bool {
	return true
}

// HandleKey processes a key press using emacs keybindings.
func (s *EmacsScheme) HandleKey(e *Editor, buf []byte, n int) Event {
	if n == 0 {
		return Event{}
	}

	// Check for escape sequences (Alt+key, arrow keys)
	if buf[0] == 27 && n >= 2 {
		switch {
		case buf[1] == 127: // Alt+Backspace
			e.SaveState()
			e.DeleteWordBackward()
			return Event{Consumed: true, TextChanged: true}
		case buf[1] == 'b' || buf[1] == 'B': // Alt+B
			e.WordLeft()
			return Event{Consumed: true}
		case buf[1] == 'f' || buf[1] == 'F': // Alt+F
			e.WordRight()
			return Event{Consumed: true}
		case buf[1] == 'd' || buf[1] == 'D': // Alt+D
			e.SaveState()
			e.DeleteWordForward()
			return Event{Consumed: true, TextChanged: true}
		case n >= 3 && buf[1] == '[': // Arrow keys
			switch buf[2] {
			case 'C': // Right
				e.Right()
				return Event{Consumed: true}
			case 'D': // Left
				e.Left()
				return Event{Consumed: true}
			case 'A': // Up - could be history, but not handled here
				return Event{Consumed: false}
			case 'B': // Down - could be history, but not handled here
				return Event{Consumed: false}
			}
		}
		return Event{Consumed: false}
	}

	// Single byte inputs
	switch {
	case n == 1 && buf[0] == 27: // Escape
		return Event{Consumed: true, Cancel: true}

	case buf[0] == 13: // Enter
		return Event{Consumed: true, Submit: true}

	case buf[0] == 1: // Ctrl+A
		e.Home()
		return Event{Consumed: true}

	case buf[0] == 5: // Ctrl+E
		e.End()
		return Event{Consumed: true}

	case buf[0] == 6: // Ctrl+F
		e.Right()
		return Event{Consumed: true}

	case buf[0] == 2: // Ctrl+B
		e.Left()
		return Event{Consumed: true}

	case buf[0] == 4: // Ctrl+D
		e.SaveState()
		if e.DeleteForward() {
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}

	case buf[0] == 11: // Ctrl+K
		e.SaveState()
		e.KillToEnd()
		return Event{Consumed: true, TextChanged: true}

	case buf[0] == 21: // Ctrl+U
		e.SaveState()
		e.KillToStart()
		return Event{Consumed: true, TextChanged: true}

	case buf[0] == 23: // Ctrl+W
		e.SaveState()
		e.DeleteWordBackward()
		return Event{Consumed: true, TextChanged: true}

	case buf[0] == 20: // Ctrl+T
		e.SaveState()
		e.Transpose()
		return Event{Consumed: true, TextChanged: true}

	case buf[0] == 26 || buf[0] == 31: // Ctrl+Z or Ctrl+_ (undo)
		if e.Undo() {
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}

	case buf[0] == 127 || buf[0] == 8: // Backspace
		e.SaveState()
		if e.DeleteBackward() {
			return Event{Consumed: true, TextChanged: true}
		}
		return Event{Consumed: true}

	case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
		e.Insert(buf[0])
		return Event{Consumed: true, TextChanged: true}
	}

	return Event{Consumed: false}
}
