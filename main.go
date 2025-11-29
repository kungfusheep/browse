// Browse is a terminal-based web browser focused on beautiful text layouts.
package main

import (
	"fmt"
	"net/http"
	"os"

	"browse/document"
	"browse/html"
	"browse/render"
)

func main() {
	url := "https://kungfusheep.com/articles/service-principles"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	if err := run(url); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(url string) error {
	// Fetch the page
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Parse HTML
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("parsing HTML: %w", err)
	}

	// Set up terminal
	width, height, err := render.TerminalSize()
	if err != nil {
		return fmt.Errorf("detecting terminal: %w", err)
	}

	term, err := render.NewTerminal(os.Stdin)
	if err != nil {
		return fmt.Errorf("initializing terminal: %w", err)
	}

	render.EnterAltScreen(os.Stdout)
	if err := term.EnterRawMode(); err != nil {
		render.ExitAltScreen(os.Stdout)
		return fmt.Errorf("entering raw mode: %w", err)
	}

	defer func() {
		term.RestoreMode()
		render.ExitAltScreen(os.Stdout)
	}()

	// Create canvas and renderer
	canvas := render.NewCanvas(width, height)
	renderer := document.NewRenderer(canvas)

	// Calculate content height for scrolling
	contentHeight := renderer.ContentHeight(doc)
	maxScroll := contentHeight - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollY := 0

	// Initial render
	renderer.Render(doc, scrollY)
	canvas.RenderTo(os.Stdout)

	// Input loop
	buf := make([]byte, 3)
	for {
		n, _ := os.Stdin.Read(buf)
		if n == 0 {
			continue
		}

		switch {
		case buf[0] == 'q':
			return nil

		case buf[0] == 'j', buf[0] == 14: // j or Ctrl+N
			scrollY++
			if scrollY > maxScroll {
				scrollY = maxScroll
			}

		case buf[0] == 'k', buf[0] == 16: // k or Ctrl+P
			scrollY--
			if scrollY < 0 {
				scrollY = 0
			}

		case buf[0] == 'd', buf[0] == 4: // d or Ctrl+D
			scrollY += height / 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}

		case buf[0] == 'u', buf[0] == 21: // u or Ctrl+U
			scrollY -= height / 2
			if scrollY < 0 {
				scrollY = 0
			}

		case buf[0] == 'g': // top
			scrollY = 0

		case buf[0] == 'G': // bottom
			scrollY = maxScroll

		case buf[0] == ' ': // page down
			scrollY += height - 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}

		case buf[0] == 27 && n == 3: // escape sequences
			if buf[1] == '[' {
				switch buf[2] {
				case 'A': // up arrow
					scrollY--
					if scrollY < 0 {
						scrollY = 0
					}
				case 'B': // down arrow
					scrollY++
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
				}
			}
		}

		renderer.Render(doc, scrollY)
		canvas.RenderTo(os.Stdout)
	}
}
