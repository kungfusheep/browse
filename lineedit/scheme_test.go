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
