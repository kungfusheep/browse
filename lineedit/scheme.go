package lineedit

// Event represents the result of handling a key press.
type Event struct {
	Consumed    bool // true if the scheme handled the key
	TextChanged bool // true if editor content was modified
	Submit      bool // true if user wants to submit (Enter)
	Cancel      bool // true if user wants to cancel/exit
}

// KeyScheme interprets key presses and translates them to editor actions.
// Different schemes (emacs, vim, etc.) provide different key mappings.
type KeyScheme interface {
	// Name returns the scheme name for display/config.
	Name() string

	// HandleKey processes a key press and performs editor actions.
	// buf contains the raw bytes read, n is the number of bytes.
	// Returns an Event describing what happened.
	HandleKey(e *Editor, buf []byte, n int) Event

	// InInsertMode returns true if the scheme is currently accepting text input.
	// For emacs, this is always true. For vim, only in insert mode.
	InInsertMode() bool
}
