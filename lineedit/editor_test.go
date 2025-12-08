package lineedit

import "testing"

func TestInsert(t *testing.T) {
	e := New()
	e.Insert('h')
	e.Insert('i')
	if e.Text() != "hi" {
		t.Errorf("expected 'hi', got %q", e.Text())
	}
	if e.Cursor() != 2 {
		t.Errorf("expected cursor at 2, got %d", e.Cursor())
	}
}

func TestInsertMiddle(t *testing.T) {
	e := New()
	e.Set("hllo")
	e.cursor = 1 // After 'h'
	e.Insert('e')
	if e.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", e.Text())
	}
}

func TestDeleteBackward(t *testing.T) {
	e := New()
	e.Set("hello")
	e.DeleteBackward()
	if e.Text() != "hell" {
		t.Errorf("expected 'hell', got %q", e.Text())
	}

	// At start, should return false
	e.Home()
	if e.DeleteBackward() {
		t.Error("DeleteBackward at start should return false")
	}
}

func TestDeleteForward(t *testing.T) {
	e := New()
	e.Set("hello")
	e.Home()
	e.DeleteForward()
	if e.Text() != "ello" {
		t.Errorf("expected 'ello', got %q", e.Text())
	}

	// At end, should return false
	e.End()
	if e.DeleteForward() {
		t.Error("DeleteForward at end should return false")
	}
}

func TestMovement(t *testing.T) {
	e := New()
	e.Set("hello")

	e.Home()
	if e.Cursor() != 0 {
		t.Errorf("Home: expected cursor at 0, got %d", e.Cursor())
	}

	e.End()
	if e.Cursor() != 5 {
		t.Errorf("End: expected cursor at 5, got %d", e.Cursor())
	}

	e.Left()
	if e.Cursor() != 4 {
		t.Errorf("Left: expected cursor at 4, got %d", e.Cursor())
	}

	e.Right()
	if e.Cursor() != 5 {
		t.Errorf("Right: expected cursor at 5, got %d", e.Cursor())
	}

	// Bounds checking
	e.End()
	if e.Right() {
		t.Error("Right at end should return false")
	}
	e.Home()
	if e.Left() {
		t.Error("Left at start should return false")
	}
}

func TestWordMovement(t *testing.T) {
	e := New()
	e.Set("hello world test")

	e.Home()
	e.WordRight()
	if e.Cursor() != 6 { // After "hello "
		t.Errorf("WordRight: expected cursor at 6, got %d", e.Cursor())
	}

	e.WordRight()
	if e.Cursor() != 12 { // After "world "
		t.Errorf("WordRight: expected cursor at 12, got %d", e.Cursor())
	}

	e.WordLeft()
	if e.Cursor() != 6 { // Back to "world"
		t.Errorf("WordLeft: expected cursor at 6, got %d", e.Cursor())
	}
}

func TestDeleteWordBackward(t *testing.T) {
	e := New()
	e.Set("hello world")
	e.DeleteWordBackward()
	if e.Text() != "hello " {
		t.Errorf("expected 'hello ', got %q", e.Text())
	}
}

func TestDeleteWordForward(t *testing.T) {
	e := New()
	e.Set("hello world")
	e.Home()
	e.DeleteWordForward()
	if e.Text() != "world" {
		t.Errorf("expected 'world', got %q", e.Text())
	}
}

func TestKillToEnd(t *testing.T) {
	e := New()
	e.Set("hello world")
	e.cursor = 5
	e.KillToEnd()
	if e.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", e.Text())
	}
}

func TestKillToStart(t *testing.T) {
	e := New()
	e.Set("hello world")
	e.cursor = 6
	e.KillToStart()
	if e.Text() != "world" {
		t.Errorf("expected 'world', got %q", e.Text())
	}
	if e.Cursor() != 0 {
		t.Errorf("expected cursor at 0, got %d", e.Cursor())
	}
}

func TestTranspose(t *testing.T) {
	e := New()
	e.Set("ab")
	e.Transpose() // At end, should swap last two
	if e.Text() != "ba" {
		t.Errorf("expected 'ba', got %q", e.Text())
	}

	e.Set("abc")
	e.cursor = 2 // Between 'b' and 'c'
	e.Transpose()
	if e.Text() != "acb" {
		t.Errorf("expected 'acb', got %q", e.Text())
	}
}

func TestClear(t *testing.T) {
	e := New()
	e.Set("hello")
	e.Clear()
	if e.Text() != "" {
		t.Errorf("expected empty, got %q", e.Text())
	}
	if e.Cursor() != 0 {
		t.Errorf("expected cursor at 0, got %d", e.Cursor())
	}
}

func TestBeforeAfterCursor(t *testing.T) {
	e := New()
	e.Set("hello")
	e.cursor = 2
	if e.BeforeCursor() != "he" {
		t.Errorf("expected 'he', got %q", e.BeforeCursor())
	}
	if e.AfterCursor() != "llo" {
		t.Errorf("expected 'llo', got %q", e.AfterCursor())
	}
}

// TestWordVsBigWordMovement tests the difference between word (w/b/e) and WORD (W/B/E) motions.
// word motions stop at punctuation boundaries, WORD motions only stop at whitespace.
func TestWordVsBigWordMovement(t *testing.T) {
	e := New()
	// With punctuation: "hello.world" should be one WORD but multiple words
	e.Set("hello.world test")

	// Small 'w' - word motion stops at punctuation
	e.Home()
	e.WordRight() // Should stop at '.'
	if e.Cursor() != 5 {
		t.Errorf("WordRight (w): expected cursor at 5 (before '.'), got %d", e.Cursor())
	}

	// Big 'W' - WORD motion crosses punctuation, stops at space
	e.Home()
	e.BigWordRight() // Should stop at 't' (after "hello.world ")
	if e.Cursor() != 12 {
		t.Errorf("BigWordRight (W): expected cursor at 12 (start of 'test'), got %d", e.Cursor())
	}

	// Test 'b' vs 'B' backward motions
	e.End() // cursor at 16
	e.BigWordLeft() // Should stop at 't' of "test"
	if e.Cursor() != 12 {
		t.Errorf("BigWordLeft (B): expected cursor at 12, got %d", e.Cursor())
	}

	e.BigWordLeft() // Should stop at 'h' of "hello.world"
	if e.Cursor() != 0 {
		t.Errorf("BigWordLeft (B): expected cursor at 0, got %d", e.Cursor())
	}

	e.End()
	e.WordLeft() // Should stop at 't' of "test"
	if e.Cursor() != 12 {
		t.Errorf("WordLeft (b): expected cursor at 12, got %d", e.Cursor())
	}

	e.WordLeft() // Should stop at 'w' of "world" (after punctuation)
	if e.Cursor() != 6 {
		t.Errorf("WordLeft (b): expected cursor at 6, got %d", e.Cursor())
	}

	// Test 'e' vs 'E' end motions
	e.Set("foo.bar baz")
	e.Home()
	e.WordEnd() // Should stop at 'o' (end of "foo")
	if e.Cursor() != 2 {
		t.Errorf("WordEnd (e): expected cursor at 2 (end of 'foo'), got %d", e.Cursor())
	}

	e.Home()
	e.BigWordEnd() // Should stop at 'r' (end of "foo.bar")
	if e.Cursor() != 6 {
		t.Errorf("BigWordEnd (E): expected cursor at 6 (end of 'foo.bar'), got %d", e.Cursor())
	}
}
