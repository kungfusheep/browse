package lineedit

import "testing"

func TestEmacsScheme(t *testing.T) {
	e := New()
	s := NewEmacsScheme()

	// Always in insert mode
	if !s.InInsertMode() {
		t.Error("emacs should always be in insert mode")
	}

	// Type "hello"
	for _, ch := range "hello" {
		ev := s.HandleKey(e, []byte{byte(ch)}, 1)
		if !ev.Consumed || !ev.TextChanged {
			t.Error("printable chars should be consumed and change text")
		}
	}
	if e.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", e.Text())
	}

	// Ctrl+A goes home
	ev := s.HandleKey(e, []byte{1}, 1)
	if !ev.Consumed || e.Cursor() != 0 {
		t.Error("Ctrl+A should move to start")
	}

	// Ctrl+E goes to end
	ev = s.HandleKey(e, []byte{5}, 1)
	if !ev.Consumed || e.Cursor() != 5 {
		t.Error("Ctrl+E should move to end")
	}

	// Ctrl+W deletes word
	ev = s.HandleKey(e, []byte{23}, 1)
	if !ev.TextChanged || e.Text() != "" {
		t.Errorf("Ctrl+W should delete word, got %q", e.Text())
	}

	// Enter submits
	e.Set("test")
	ev = s.HandleKey(e, []byte{13}, 1)
	if !ev.Submit {
		t.Error("Enter should submit")
	}

	// Escape cancels
	ev = s.HandleKey(e, []byte{27}, 1)
	if !ev.Cancel {
		t.Error("Escape should cancel")
	}
}

func TestVimSchemeNormalMode(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Starts in normal mode
	if s.InInsertMode() {
		t.Error("vim should start in normal mode")
	}

	// 'j' and 'k' should NOT be consumed (for scrolling)
	ev := s.HandleKey(e, []byte{'j'}, 1)
	if ev.Consumed {
		t.Error("'j' should not be consumed in normal mode (for scrolling)")
	}

	// 'i' enters insert mode
	ev = s.HandleKey(e, []byte{'i'}, 1)
	if !ev.Consumed || !s.InInsertMode() {
		t.Error("'i' should enter insert mode")
	}

	// Escape returns to normal
	ev = s.HandleKey(e, []byte{27}, 1)
	if s.InInsertMode() {
		t.Error("Escape should return to normal mode")
	}

	// Test 'A' - append at end
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'A'}, 1)
	if !s.InInsertMode() || e.Cursor() != 5 {
		t.Error("'A' should enter insert at end")
	}
	s.HandleKey(e, []byte{27}, 1) // back to normal

	// Test 'I' - insert at beginning
	s.HandleKey(e, []byte{'I'}, 1)
	if !s.InInsertMode() || e.Cursor() != 0 {
		t.Error("'I' should enter insert at beginning")
	}
}

func TestVimSchemeInsertMode(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Enter insert mode
	s.HandleKey(e, []byte{'i'}, 1)

	// Type "hello"
	for _, ch := range "hello" {
		ev := s.HandleKey(e, []byte{byte(ch)}, 1)
		if !ev.Consumed || !ev.TextChanged {
			t.Error("typing should change text in insert mode")
		}
	}
	if e.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", e.Text())
	}

	// Enter submits
	ev := s.HandleKey(e, []byte{13}, 1)
	if !ev.Submit {
		t.Error("Enter should submit")
	}
}

func TestVimSchemeEditing(t *testing.T) {
	e := New()
	s := NewVimScheme()

	e.Set("hello world")
	e.Home()

	// 'x' deletes char at cursor
	ev := s.HandleKey(e, []byte{'x'}, 1)
	if !ev.TextChanged || e.Text() != "ello world" {
		t.Errorf("'x' should delete char, got %q", e.Text())
	}

	// 'w' moves word forward
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Cursor() != 5 {
		t.Errorf("'w' should move to next word, cursor at %d", e.Cursor())
	}

	// 'D' deletes to end
	s.HandleKey(e, []byte{'D'}, 1)
	if e.Text() != "ello " {
		t.Errorf("'D' should delete to end, got %q", e.Text())
	}

	// 'S' substitutes line
	e.Set("hello")
	s.HandleKey(e, []byte{'S'}, 1)
	if e.Text() != "" || !s.InInsertMode() {
		t.Error("'S' should clear and enter insert mode")
	}
}

func TestVimNormalModeCancel(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// In normal mode, Escape should cancel
	ev := s.HandleKey(e, []byte{27}, 1)
	if !ev.Cancel {
		t.Error("Escape in normal mode should cancel")
	}
}

func TestVimMotionsWithCount(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test 3w - move 3 words forward
	e.Set("one two three four five")
	e.Home()
	s.HandleKey(e, []byte{'3'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Cursor() != 14 { // after "one two three "
		t.Errorf("3w should move 3 words forward, cursor at %d, expected 14", e.Cursor())
	}

	// Test 2l - move 2 chars right
	e.Home()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'l'}, 1)
	if e.Cursor() != 2 {
		t.Errorf("2l should move 2 chars right, cursor at %d", e.Cursor())
	}

	// Test 5x - delete 5 chars
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'5'}, 1)
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != " world" {
		t.Errorf("5x should delete 5 chars, got %q", e.Text())
	}
}

func TestVimOperatorPending(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test dw - delete word
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	if s.mode != VimOperatorPending {
		t.Error("'d' should enter operator-pending mode")
	}
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "world" {
		t.Errorf("dw should delete word, got %q", e.Text())
	}
	if s.mode != VimNormal {
		t.Error("after dw should be back in normal mode")
	}

	// Test d$ - delete to end
	e.Set("hello world")
	e.SetCursor(6) // at 'w'
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'$'}, 1)
	if e.Text() != "hello " {
		t.Errorf("d$ should delete to end, got %q", e.Text())
	}

	// Test d0 - delete to start
	e.Set("hello world")
	e.SetCursor(6)
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'0'}, 1)
	if e.Text() != "world" {
		t.Errorf("d0 should delete to start, got %q", e.Text())
	}
}

func TestVimChangeOperator(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test cw - change word
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "world" {
		t.Errorf("cw should delete word, got %q", e.Text())
	}
	if !s.InInsertMode() {
		t.Error("after cw should be in insert mode")
	}

	// Type replacement text
	s.HandleKey(e, []byte{'h'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{' '}, 1)
	if e.Text() != "hi world" {
		t.Errorf("after cw and typing 'hi ', got %q", e.Text())
	}
}

func TestVimDoubledOperators(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test dd - delete line
	e.Set("hello world")
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'d'}, 1)
	if e.Text() != "" {
		t.Errorf("dd should clear line, got %q", e.Text())
	}

	// Test cc - change line
	e.Set("hello world")
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'c'}, 1)
	if e.Text() != "" {
		t.Errorf("cc should clear line, got %q", e.Text())
	}
	if !s.InInsertMode() {
		t.Error("after cc should be in insert mode")
	}
}

func TestVimCountWithOperator(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test 2dw - delete 2 words
	e.Set("one two three four")
	e.Home()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "three four" {
		t.Errorf("2dw should delete 2 words, got %q", e.Text())
	}

	// Test d2w - also delete 2 words (vim supports both forms)
	e.Set("one two three four")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "three four" {
		t.Errorf("d2w should delete 2 words, got %q", e.Text())
	}
}

func TestVimEscapeCancelsPending(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Start operator, then cancel with Escape
	e.Set("hello")
	s.HandleKey(e, []byte{'d'}, 1)
	if s.mode != VimOperatorPending {
		t.Error("'d' should enter operator-pending mode")
	}

	ev := s.HandleKey(e, []byte{27}, 1)
	if s.mode != VimNormal {
		t.Error("Escape should cancel operator-pending")
	}
	if ev.Cancel {
		t.Error("Escape after operator should NOT trigger Cancel event (just reset state)")
	}
	if e.Text() != "hello" {
		t.Errorf("text should be unchanged, got %q", e.Text())
	}

	// Now Escape in clean normal mode should Cancel
	ev = s.HandleKey(e, []byte{27}, 1)
	if !ev.Cancel {
		t.Error("Escape in clean normal mode should Cancel")
	}
}

func TestVimWordEndMotion(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test 'e' motion
	e.Set("hello world test")
	e.Home()
	s.HandleKey(e, []byte{'e'}, 1)
	if e.Cursor() != 4 { // end of "hello" (index of 'o')
		t.Errorf("'e' should move to end of word, cursor at %d", e.Cursor())
	}

	s.HandleKey(e, []byte{'e'}, 1)
	if e.Cursor() != 10 { // end of "world" (index of 'd')
		t.Errorf("second 'e' should move to end of next word, cursor at %d", e.Cursor())
	}

	// Test de - delete to end of word
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'e'}, 1)
	if e.Text() != " world" {
		t.Errorf("de should delete to end of word (inclusive), got %q", e.Text())
	}
}

func TestVimTextObjectWord(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test diw - delete inner word (cursor in middle of word)
	e.Set("hello world test")
	e.SetCursor(7) // on 'o' of "world"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "hello  test" {
		t.Errorf("diw should delete just the word, got %q", e.Text())
	}

	// Test daw - delete a word (includes trailing space)
	e.Set("hello world test")
	e.SetCursor(7)
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'a'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "hello test" {
		t.Errorf("daw should delete word + trailing space, got %q", e.Text())
	}

	// Test ciw - change inner word
	e.Set("hello world test")
	e.SetCursor(7)
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "hello  test" {
		t.Errorf("ciw should delete word, got %q", e.Text())
	}
	if !s.InInsertMode() {
		t.Error("ciw should enter insert mode")
	}

	// Type replacement
	for _, ch := range "universe" {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	if e.Text() != "hello universe test" {
		t.Errorf("after ciw and typing, got %q", e.Text())
	}
}

func TestVimTextObjectQuotes(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test di" - delete inner double quotes
	e.Set(`say "hello world" please`)
	e.SetCursor(8) // on 'l' of "hello"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'"'}, 1)
	if e.Text() != `say "" please` {
		t.Errorf(`di" should delete contents, got %q`, e.Text())
	}

	// Test da" - delete around double quotes (includes quotes)
	e.Set(`say "hello world" please`)
	e.SetCursor(8)
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'a'}, 1)
	s.HandleKey(e, []byte{'"'}, 1)
	if e.Text() != `say  please` {
		t.Errorf(`da" should delete quotes too, got %q`, e.Text())
	}

	// Test ci" - change inner double quotes
	e.Set(`say "hello" now`)
	e.SetCursor(6) // inside quotes
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'"'}, 1)
	if e.Text() != `say "" now` {
		t.Errorf(`ci" should clear contents, got %q`, e.Text())
	}
	if !s.InInsertMode() {
		t.Error(`ci" should enter insert mode`)
	}

	// Type replacement
	for _, ch := range "hi" {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	if e.Text() != `say "hi" now` {
		t.Errorf(`after ci" and typing, got %q`, e.Text())
	}
}

func TestVimTextObjectSingleQuotes(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test di' - delete inner single quotes
	e.Set("it's a 'test string' here")
	e.SetCursor(10) // on 's' of "string"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'\''}, 1)
	if e.Text() != "it's a '' here" {
		t.Errorf("di' should delete contents, got %q", e.Text())
	}

	// Test da' - delete around single quotes
	e.Set("it's a 'test' here")
	e.SetCursor(9) // inside quotes
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'a'}, 1)
	s.HandleKey(e, []byte{'\''}, 1)
	if e.Text() != "it's a  here" {
		t.Errorf("da' should delete quotes too, got %q", e.Text())
	}
}

func TestVimTextObjectCursorOnQuote(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test with cursor ON the opening quote
	e.Set(`say "hello" end`)
	e.SetCursor(4) // on opening "
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'"'}, 1)
	if e.Text() != `say "" end` {
		t.Errorf(`di" with cursor on quote should work, got %q`, e.Text())
	}

	// Test with cursor ON the closing quote
	e.Set(`say "hello" end`)
	e.SetCursor(10) // on closing "
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'"'}, 1)
	if e.Text() != `say "" end` {
		t.Errorf(`di" with cursor on closing quote should work, got %q`, e.Text())
	}
}

func TestVimTextObjectAnyQuote(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test ciq with double quotes
	e.Set(`say "hello" end`)
	e.SetCursor(6) // inside "hello"
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'q'}, 1)
	if e.Text() != `say "" end` {
		t.Errorf(`ciq should work with double quotes, got %q`, e.Text())
	}
	if !s.InInsertMode() {
		t.Error("ciq should enter insert mode")
	}
	s.HandleKey(e, []byte{27}, 1) // back to normal

	// Test ciq with single quotes
	e.Set("say 'hello' end")
	e.SetCursor(6) // inside 'hello'
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'q'}, 1)
	if e.Text() != "say '' end" {
		t.Errorf("ciq should work with single quotes, got %q", e.Text())
	}
	s.HandleKey(e, []byte{27}, 1)

	// Test daq - delete around any quote
	e.Set(`value = "test"`)
	e.SetCursor(10) // inside "test"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'a'}, 1)
	s.HandleKey(e, []byte{'q'}, 1)
	if e.Text() != "value = " {
		t.Errorf("daq should delete quotes too, got %q", e.Text())
	}

	// Test with both quote types - should pick the one cursor is in
	e.Set(`outer "inner 'nested' here" end`)
	e.SetCursor(15) // inside 'nested'
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'q'}, 1)
	if e.Text() != `outer "inner '' here" end` {
		t.Errorf("ciq should pick inner quotes when nested, got %q", e.Text())
	}
}

func TestVimTextObjectSentence(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test dis - delete inner sentence (single sentence)
	e.Set("Hello world.")
	e.SetCursor(3) // on 'l' of "Hello"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != "" {
		t.Errorf("dis on single sentence should delete it, got %q", e.Text())
	}

	// Test dis - middle sentence
	e.Set("First sentence. Second one here. Third sentence.")
	e.SetCursor(20) // in "Second"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != "First sentence.  Third sentence." {
		t.Errorf("dis should delete middle sentence, got %q", e.Text())
	}

	// Test das - delete a sentence (includes trailing space)
	e.Set("First sentence. Second one here. Third sentence.")
	e.SetCursor(20) // in "Second"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'a'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != "First sentence. Third sentence." {
		t.Errorf("das should delete sentence + trailing space, got %q", e.Text())
	}

	// Test cis - change inner sentence
	e.Set("Old sentence. Keep this.")
	e.SetCursor(3) // in "Old"
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != " Keep this." {
		t.Errorf("cis should delete first sentence, got %q", e.Text())
	}
	if !s.InInsertMode() {
		t.Error("cis should enter insert mode")
	}

	// Type replacement
	for _, ch := range "New one." {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	if e.Text() != "New one. Keep this." {
		t.Errorf("after cis and typing, got %q", e.Text())
	}
}

func TestVimTextObjectSentenceWithDifferentPunctuation(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test with exclamation mark
	e.Set("Hello! How are you?")
	e.SetCursor(2) // in "Hello"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != " How are you?" {
		t.Errorf("dis should work with !, got %q", e.Text())
	}

	// Test with question mark
	e.Set("What is this? I wonder.")
	e.SetCursor(17) // in "wonder"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'s'}, 1)
	if e.Text() != "What is this? " {
		t.Errorf("dis should work with ?, got %q", e.Text())
	}
}

func TestVimUndo(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Type some text
	e.Set("hello world")
	e.Home()

	// Delete word with dw
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "world" {
		t.Errorf("dw should delete word, got %q", e.Text())
	}

	// Undo with u
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello world" {
		t.Errorf("u should undo, got %q", e.Text())
	}

	// Test undo after x
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "ello" {
		t.Errorf("x should delete char, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello" {
		t.Errorf("u should undo x, got %q", e.Text())
	}

	// Test multiple undos
	e.Set("abc")
	e.Home()
	s.HandleKey(e, []byte{'x'}, 1) // delete 'a'
	s.HandleKey(e, []byte{'x'}, 1) // delete 'b'
	if e.Text() != "c" {
		t.Errorf("two x should leave 'c', got %q", e.Text())
	}
	s.HandleKey(e, []byte{'u'}, 1) // undo second x
	if e.Text() != "bc" {
		t.Errorf("first undo should restore 'bc', got %q", e.Text())
	}
	s.HandleKey(e, []byte{'u'}, 1) // undo first x
	if e.Text() != "abc" {
		t.Errorf("second undo should restore 'abc', got %q", e.Text())
	}
}

func TestEmacsUndo(t *testing.T) {
	e := New()
	s := NewEmacsScheme()

	// Type some text and delete word
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{23}, 1) // Ctrl+W deletes word backward
	if e.Text() != "hello " {
		t.Errorf("Ctrl+W should delete word, got %q", e.Text())
	}

	// Undo with Ctrl+Z
	s.HandleKey(e, []byte{26}, 1)
	if e.Text() != "hello world" {
		t.Errorf("Ctrl+Z should undo, got %q", e.Text())
	}

	// Test undo with Ctrl+_
	e.Set("test")
	e.End()
	s.HandleKey(e, []byte{11}, 1) // Ctrl+K kills to end (but we're at end, so does nothing visible)
	e.Home()
	s.HandleKey(e, []byte{11}, 1) // Ctrl+K kills to end from start
	if e.Text() != "" {
		t.Errorf("Ctrl+K should kill line, got %q", e.Text())
	}
	s.HandleKey(e, []byte{31}, 1) // Ctrl+_ to undo
	if e.Text() != "test" {
		t.Errorf("Ctrl+_ should undo, got %q", e.Text())
	}
}

func TestVimRedo(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Setup: delete something then undo
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "world" {
		t.Errorf("dw should delete word, got %q", e.Text())
	}

	// Undo
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello world" {
		t.Errorf("u should undo, got %q", e.Text())
	}

	// Redo with Ctrl+R
	s.HandleKey(e, []byte{18}, 1)
	if e.Text() != "world" {
		t.Errorf("Ctrl+R should redo, got %q", e.Text())
	}

	// Undo again
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello world" {
		t.Errorf("u should undo again, got %q", e.Text())
	}

	// Make a new change - this should clear redo history
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "ello world" {
		t.Errorf("x should delete char, got %q", e.Text())
	}

	// Redo should do nothing now (new change cleared redo)
	s.HandleKey(e, []byte{18}, 1)
	if e.Text() != "ello world" {
		t.Errorf("Ctrl+R after new change should do nothing, got %q", e.Text())
	}
}

func TestVimReplaceChar(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test r - replace single char
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'r'}, 1)
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "xello" {
		t.Errorf("r should replace char, got %q", e.Text())
	}

	// Test 3rx - replace 3 chars with x
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'3'}, 1)
	s.HandleKey(e, []byte{'r'}, 1)
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "xxxlo" {
		t.Errorf("3r should replace 3 chars, got %q", e.Text())
	}

	// Test r with undo
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'r'}, 1)
	s.HandleKey(e, []byte{'x'}, 1)
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello" {
		t.Errorf("u should undo r, got %q", e.Text())
	}
}

func TestEmacsRedo(t *testing.T) {
	e := New()
	s := NewEmacsScheme()

	// Setup: delete something then undo
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{23}, 1) // Ctrl+W deletes word
	if e.Text() != "hello " {
		t.Errorf("Ctrl+W should delete word, got %q", e.Text())
	}

	// Undo
	s.HandleKey(e, []byte{26}, 1)
	if e.Text() != "hello world" {
		t.Errorf("Ctrl+Z should undo, got %q", e.Text())
	}

	// Redo with Ctrl+Y
	s.HandleKey(e, []byte{25}, 1)
	if e.Text() != "hello " {
		t.Errorf("Ctrl+Y should redo, got %q", e.Text())
	}
}

func TestVimFindForward(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test f - find forward (inclusive)
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 4 { // first 'o' in "hello"
		t.Errorf("fo should find first 'o', cursor at %d expected 4", e.Cursor())
	}

	// Find next 'o' with 2fo
	e.Home()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 7 { // 'o' in "world"
		t.Errorf("2fo should find second 'o', cursor at %d expected 7", e.Cursor())
	}

	// Target not found - cursor shouldn't move
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{'z'}, 1)
	if e.Cursor() != 0 {
		t.Errorf("fz on 'hello' should not move cursor, at %d", e.Cursor())
	}
}

func TestVimFindBackward(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test F - find backward (inclusive)
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{'F'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 7 { // 'o' in "world"
		t.Errorf("Fo should find 'o' backward, cursor at %d expected 7", e.Cursor())
	}

	// Find 2nd 'o' backward with 2Fo
	e.End()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'F'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 4 { // 'o' in "hello"
		t.Errorf("2Fo should find second 'o' backward, cursor at %d expected 4", e.Cursor())
	}
}

func TestVimFindForwardBefore(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test t - find forward, stop before (exclusive)
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'t'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 3 { // one before first 'o' in "hello"
		t.Errorf("to should stop before 'o', cursor at %d expected 3", e.Cursor())
	}
}

func TestVimFindBackwardAfter(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test T - find backward, stop after (exclusive)
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{'T'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Cursor() != 8 { // one after 'o' in "world"
		t.Errorf("To should stop after 'o', cursor at %d expected 8", e.Cursor())
	}
}

func TestVimDeleteWithFind(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test df - delete forward to and including char
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Text() != " world" {
		t.Errorf("dfo should delete up to and including 'o', got %q", e.Text())
	}

	// Test dt - delete forward up to but not including char
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'t'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Text() != "o world" {
		t.Errorf("dto should delete up to but not including 'o', got %q", e.Text())
	}

	// Test dF - delete backward to and including char
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'F'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Text() != "hello w" {
		t.Errorf("dFo should delete backward to 'o', got %q", e.Text())
	}

	// Test dT - delete backward up to but not including char
	e.Set("hello world")
	e.End()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'T'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Text() != "hello wo" {
		t.Errorf("dTo should delete backward after 'o', got %q", e.Text())
	}
}

func TestVimChangeWithFind(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test cf - change forward to and including char
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{' '}, 1) // change to space
	if e.Text() != "world" {
		t.Errorf("cf<space> should delete to space, got %q", e.Text())
	}
	if !s.InInsertMode() {
		t.Error("cf should enter insert mode")
	}

	// Type replacement
	for _, ch := range "hi " {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	if e.Text() != "hi world" {
		t.Errorf("after cf and typing, got %q", e.Text())
	}
}

func TestVimFindWithUndo(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test that df can be undone
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{'o'}, 1)
	if e.Text() != " world" {
		t.Errorf("dfo should delete, got %q", e.Text())
	}

	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "hello world" {
		t.Errorf("u should undo dfo, got %q", e.Text())
	}
	if e.Cursor() != 0 {
		t.Errorf("undo should restore cursor to 0, got %d", e.Cursor())
	}
}

func TestVimRepeatSimple(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats x
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "ello" {
		t.Errorf("x should delete char, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "llo" {
		t.Errorf(". should repeat x, got %q", e.Text())
	}

	// Test . repeats 2x
	e.Set("hello world")
	e.Home()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'x'}, 1)
	if e.Text() != "llo world" {
		t.Errorf("2x should delete 2 chars, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "o world" {
		t.Errorf(". should repeat 2x, got %q", e.Text())
	}
}

func TestVimRepeatDelete(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats dw
	e.Set("one two three four")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "two three four" {
		t.Errorf("dw should delete word, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "three four" {
		t.Errorf(". should repeat dw, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "four" {
		t.Errorf(". should repeat dw again, got %q", e.Text())
	}
}

func TestVimRepeatDeleteWithFind(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats df
	e.Set("a=1, b=2, c=3, d=4")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'f'}, 1)
	s.HandleKey(e, []byte{' '}, 1)
	if e.Text() != "b=2, c=3, d=4" {
		t.Errorf("df should delete to space, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "c=3, d=4" {
		t.Errorf(". should repeat df, got %q", e.Text())
	}
}

func TestVimRepeatChange(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats cw + typed text
	e.Set("foo bar baz")
	e.Home()
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	// Type "hello "
	for _, ch := range "hello " {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	s.HandleKey(e, []byte{27}, 1) // Escape
	if e.Text() != "hello bar baz" {
		t.Errorf("cw + 'hello ' should give 'hello bar baz', got %q", e.Text())
	}

	// Cursor is now on 'b' of "bar" (after vim left-move on Escape)
	e.SetCursor(6) // Move to 'b' of "bar"
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "hello hello baz" {
		t.Errorf(". should repeat cw + typed text, got %q", e.Text())
	}
}

func TestVimRepeatReplace(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats r
	e.Set("hello")
	e.Home()
	s.HandleKey(e, []byte{'r'}, 1)
	s.HandleKey(e, []byte{'X'}, 1)
	if e.Text() != "Xello" {
		t.Errorf("rX should replace first char, got %q", e.Text())
	}
	s.HandleKey(e, []byte{'l'}, 1) // move right
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "XXllo" {
		t.Errorf(". should repeat rX, got %q", e.Text())
	}
}

func TestVimRepeatInsert(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats i + typed text
	e.Set("world")
	e.Home()
	s.HandleKey(e, []byte{'i'}, 1)
	for _, ch := range "hello " {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	s.HandleKey(e, []byte{27}, 1) // Escape
	if e.Text() != "hello world" {
		t.Errorf("i + 'hello ' should give 'hello world', got %q", e.Text())
	}

	// Now . should insert "hello " again
	e.End()
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "hello worldhello " {
		t.Errorf(". should repeat insert, got %q", e.Text())
	}
}

func TestVimRepeatTextObject(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test . repeats diw
	e.Set("one two three")
	e.SetCursor(0) // on "one"
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != " two three" {
		t.Errorf("diw should delete 'one', got %q", e.Text())
	}

	// Cursor at start, which is now on space
	e.SetCursor(1) // on "two"
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "  three" {
		t.Errorf(". should repeat diw, got %q", e.Text())
	}
}

func TestVimRepeatWithUndo(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test that repeated changes can be undone
	e.Set("one two three")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	s.HandleKey(e, []byte{'.'}, 1)
	if e.Text() != "three" {
		t.Errorf("dw . should delete 2 words, got %q", e.Text())
	}

	// Undo the repeat
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "two three" {
		t.Errorf("u should undo the repeat, got %q", e.Text())
	}

	// Undo the original
	s.HandleKey(e, []byte{'u'}, 1)
	if e.Text() != "one two three" {
		t.Errorf("u should undo the original, got %q", e.Text())
	}
}

// TestVimBigWordMotions tests W/B/E motions (WORD vs word)
func TestVimBigWordMotions(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test W motion - should skip over punctuation
	e.Set("foo.bar baz")
	e.Home()
	s.HandleKey(e, []byte{'W'}, 1)
	if e.Cursor() != 8 { // start of "baz"
		t.Errorf("W should move to next WORD at 8, got %d", e.Cursor())
	}

	// Test w motion for comparison - stops at punctuation
	e.Home()
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Cursor() != 3 { // at "."
		t.Errorf("w should stop at punctuation at 3, got %d", e.Cursor())
	}

	// Test B motion - backward WORD
	e.End()
	s.HandleKey(e, []byte{'B'}, 1)
	if e.Cursor() != 8 { // start of "baz"
		t.Errorf("B should move back to WORD start at 8, got %d", e.Cursor())
	}
	s.HandleKey(e, []byte{'B'}, 1)
	if e.Cursor() != 0 { // start of "foo.bar"
		t.Errorf("B should move back to WORD start at 0, got %d", e.Cursor())
	}

	// Test E motion - end of WORD
	e.Home()
	s.HandleKey(e, []byte{'E'}, 1)
	if e.Cursor() != 6 { // end of "foo.bar" (the 'r')
		t.Errorf("E should move to end of WORD at 6, got %d", e.Cursor())
	}

	// Test e motion for comparison - stops at end of word
	e.Home()
	s.HandleKey(e, []byte{'e'}, 1)
	if e.Cursor() != 2 { // end of "foo" (the second 'o')
		t.Errorf("e should stop at end of word at 2, got %d", e.Cursor())
	}
}

// TestVimTextObjectWordWithPunctuation tests iw/aw text objects with punctuation
func TestVimTextObjectWordWithPunctuation(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// ciw on 's' in "editor...so" should only delete "so", not the dots
	e.Set("editor...so")
	e.SetCursor(9) // on 's'
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "editor..." {
		t.Errorf("ciw on 's' in 'editor...so': expected 'editor...', got %q", e.Text())
	}

	// diw on '.' should only delete the dots, not the words
	e = New()
	s = NewVimScheme()
	e.Set("editor...so")
	e.SetCursor(6) // on first '.'
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "editorso" {
		t.Errorf("diw on '.' in 'editor...so': expected 'editorso', got %q", e.Text())
	}

	// diw on 'e' should only delete "editor", not the dots
	e = New()
	s = NewVimScheme()
	e.Set("editor...so")
	e.SetCursor(0) // on 'e'
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'i'}, 1)
	s.HandleKey(e, []byte{'w'}, 1)
	if e.Text() != "...so" {
		t.Errorf("diw on 'e' in 'editor...so': expected '...so', got %q", e.Text())
	}
}

// TestVimBigWordWithOperators tests W/B/E with operators (dW, cE, etc.)
func TestVimBigWordWithOperators(t *testing.T) {
	e := New()
	s := NewVimScheme()

	// Test dW - delete WORD
	e.Set("foo.bar baz qux")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'W'}, 1)
	if e.Text() != "baz qux" {
		t.Errorf("dW should delete 'foo.bar ', got %q", e.Text())
	}

	// Test cW - change WORD
	e.Set("hello.world test")
	e.Home()
	s.HandleKey(e, []byte{'c'}, 1)
	s.HandleKey(e, []byte{'W'}, 1)
	// Should be in insert mode now
	if !s.InInsertMode() {
		t.Error("cW should enter insert mode")
	}
	if e.Text() != "test" {
		t.Errorf("cW should delete WORD, got %q", e.Text())
	}
	// Type replacement text
	for _, ch := range "new" {
		s.HandleKey(e, []byte{byte(ch)}, 1)
	}
	if e.Text() != "newtest" {
		t.Errorf("after cW + typing 'new', got %q", e.Text())
	}

	// Exit insert mode
	s.HandleKey(e, []byte{27}, 1)

	// Test dE - delete to end of WORD (inclusive)
	e.Set("foo.bar baz")
	e.Home()
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'E'}, 1)
	if e.Text() != " baz" {
		t.Errorf("dE should delete 'foo.bar', got %q", e.Text())
	}

	// Test 2dW - delete 2 WORDs
	e.Set("one.two three.four five")
	e.Home()
	s.HandleKey(e, []byte{'2'}, 1)
	s.HandleKey(e, []byte{'d'}, 1)
	s.HandleKey(e, []byte{'W'}, 1)
	if e.Text() != "five" {
		t.Errorf("2dW should delete 2 WORDs, got %q", e.Text())
	}
}
