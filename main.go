// Browse is a terminal-based web browser focused on beautiful text layouts.
//
// Rather than recreating web pages visually, Browse reimagines them for the
// terminal with justified text, box-drawing characters, and a 70s-80s
// technical documentation aesthetic.
package main

import (
	"fmt"
	"os"

	"browse/render"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Get terminal dimensions
	width, height, err := render.TerminalSize()
	if err != nil {
		return fmt.Errorf("detecting terminal: %w", err)
	}

	// Set up terminal for raw mode
	term, err := render.NewTerminal(os.Stdin)
	if err != nil {
		return fmt.Errorf("initializing terminal: %w", err)
	}

	// Enter alternate screen and raw mode
	render.EnterAltScreen(os.Stdout)
	if err := term.EnterRawMode(); err != nil {
		render.ExitAltScreen(os.Stdout)
		return fmt.Errorf("entering raw mode: %w", err)
	}

	// Ensure cleanup on exit
	defer func() {
		term.RestoreMode()
		render.ExitAltScreen(os.Stdout)
	}()

	// Create canvas
	canvas := render.NewCanvas(width, height)

	// For now: render a welcome screen
	renderWelcome(canvas)
	canvas.RenderTo(os.Stdout)

	// Wait for 'q' to quit
	buf := make([]byte, 1)
	for {
		n, _ := os.Stdin.Read(buf)
		if n > 0 && buf[0] == 'q' {
			break
		}
	}

	return nil
}

func renderWelcome(c *render.Canvas) {
	c.Clear()

	// Title
	title := " BROWSE "
	c.DrawBoxWithTitle(0, 0, c.Width(), 3, title, render.DoubleBox, render.Style{}, render.Style{Bold: true})

	// Welcome text
	text := `Welcome to Browse, a terminal web browser that reimagines web content for the terminal.

Press 'q' to quit.`

	lines := render.WrapAndJustify(text, c.Width()-4)
	y := 5
	for _, line := range lines {
		c.WriteString(2, y, line, render.Style{})
		y++
	}
}
