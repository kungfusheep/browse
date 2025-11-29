// Browse is a terminal-based web browser focused on beautiful text layouts.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

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
	doc, err := fetchAndParse(url)
	if err != nil {
		return err
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

	// State
	contentHeight := renderer.ContentHeight(doc)
	maxScroll := contentHeight - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollY := 0
	jumpMode := false
	jumpInput := ""
	var labels []string

	// Render helper
	redraw := func() {
		renderer.Render(doc, scrollY)
		if jumpMode {
			labels = document.GenerateLabels(len(renderer.Links()))
			renderer.RenderLinkLabels(labels)
		}
		canvas.RenderTo(os.Stdout)
	}

	redraw()

	// Input loop
	buf := make([]byte, 3)
	for {
		n, _ := os.Stdin.Read(buf)
		if n == 0 {
			continue
		}

		// Jump mode input handling
		if jumpMode {
			switch {
			case buf[0] == 27: // Escape - cancel jump mode
				jumpMode = false
				jumpInput = ""
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z':
				jumpInput += string(buf[0])

				// Check for match
				links := renderer.Links()
				for i, label := range labels {
					if label == jumpInput && i < len(links) {
						// Found a match - navigate!
						jumpMode = false
						jumpInput = ""

						newURL := resolveURL(url, links[i].Href)
						newDoc, err := fetchAndParse(newURL)
						if err == nil {
							doc = newDoc
							url = newURL
							contentHeight = renderer.ContentHeight(doc)
							maxScroll = contentHeight - height
							if maxScroll < 0 {
								maxScroll = 0
							}
							scrollY = 0
						}
						redraw()
						break
					}
				}

				// Check if input could still match something
				couldMatch := false
				for _, label := range labels {
					if strings.HasPrefix(label, jumpInput) {
						couldMatch = true
						break
					}
				}
				if !couldMatch {
					jumpMode = false
					jumpInput = ""
					redraw()
				}
			}
			continue
		}

		// Normal mode
		switch {
		case buf[0] == 'q':
			return nil

		case buf[0] == 'f': // follow link - enter jump mode
			jumpMode = true
			jumpInput = ""
			redraw()

		case buf[0] == 'j', buf[0] == 14:
			scrollY++
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case buf[0] == 'k', buf[0] == 16:
			scrollY--
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case buf[0] == 'd', buf[0] == 4:
			scrollY += height / 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case buf[0] == 'u', buf[0] == 21:
			scrollY -= height / 2
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case buf[0] == 'g':
			scrollY = 0
			redraw()

		case buf[0] == 'G':
			scrollY = maxScroll
			redraw()

		case buf[0] == ' ':
			scrollY += height - 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case buf[0] == 27 && n == 3:
			if buf[1] == '[' {
				switch buf[2] {
				case 'A':
					scrollY--
					if scrollY < 0 {
						scrollY = 0
					}
				case 'B':
					scrollY++
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
				}
				redraw()
			}
		}
	}
}

func fetchAndParse(url string) (*html.Node, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

func resolveURL(base, href string) string {
	// Handle absolute URLs
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}

	// Handle protocol-relative URLs
	if strings.HasPrefix(href, "//") {
		if strings.HasPrefix(base, "https://") {
			return "https:" + href
		}
		return "http:" + href
	}

	// Handle root-relative URLs
	if strings.HasPrefix(href, "/") {
		// Find the origin (scheme + host)
		idx := strings.Index(base, "://")
		if idx == -1 {
			return href
		}
		rest := base[idx+3:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx == -1 {
			return base + href
		}
		return base[:idx+3+slashIdx] + href
	}

	// Handle relative URLs
	lastSlash := strings.LastIndex(base, "/")
	if lastSlash == -1 {
		return base + "/" + href
	}
	return base[:lastSlash+1] + href
}
