// Browse is a terminal-based web browser focused on beautiful text layouts.
package main

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"browse/document"
	"browse/fetcher"
	"browse/html"
	"browse/render"
)

func main() {
	url := ""
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	if err := run(url); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(url string) error {
	var doc *html.Document
	var err error

	if url == "" {
		// Show landing page
		doc, err = landingPage()
		url = "browse://home"
	} else {
		// Fetch the page
		doc, err = fetchAndParse(url)
	}
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

	// History for back navigation
	type historyEntry struct {
		url     string
		doc     *html.Document
		scrollY int
	}
	var history []historyEntry

	// State
	contentHeight := renderer.ContentHeight(doc)
	maxScroll := contentHeight - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollY := 0
	jumpMode := false
	inputMode := false   // selecting an input field
	textEntry := false   // entering text into a field
	tocMode := false     // showing table of contents
	navMode := false     // showing navigation overlay
	navScrollOffset := 0 // scroll position within nav overlay
	urlMode := false     // entering a URL
	loading := false     // currently loading a page
	jumpInput := ""
	var labels []string
	var navLinks []document.NavLink          // current navigation links in overlay
	var activeInput *document.Input          // currently selected input
	var enteredText string                   // text being entered
	var urlInput string                      // URL being entered

	// Handle terminal resize
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)

	handleResize := func() {
		if loading {
			return // Don't resize while loading
		}
		newWidth, newHeight, err := render.TerminalSize()
		if err != nil {
			return
		}
		if newWidth != width || newHeight != height {
			width = newWidth
			height = newHeight
			canvas = render.NewCanvas(width, height)
			renderer = document.NewRenderer(canvas)
			contentHeight = renderer.ContentHeight(doc)
			maxScroll = contentHeight - height
			if maxScroll < 0 {
				maxScroll = 0
			}
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
		}
	}

	// Render helper
	redraw := func() {
		renderer.Render(doc, scrollY)

		// Draw scroll indicator on right edge
		if contentHeight > height {
			// Calculate thumb position and size
			thumbHeight := height * height / contentHeight
			if thumbHeight < 1 {
				thumbHeight = 1
			}
			thumbPos := 0
			if maxScroll > 0 {
				thumbPos = scrollY * (height - thumbHeight) / maxScroll
			}

			// Draw track and thumb
			for y := 0; y < height; y++ {
				if y >= thumbPos && y < thumbPos+thumbHeight {
					canvas.Set(width-1, y, '█', render.Style{Dim: true})
				} else {
					canvas.Set(width-1, y, '│', render.Style{Dim: true})
				}
			}
		}

		if jumpMode {
			labels = document.GenerateLabels(len(renderer.Links()))
			renderer.RenderLinkLabels(labels)
		}
		if inputMode {
			labels = document.GenerateLabels(len(renderer.Inputs()))
			renderer.RenderInputLabels(labels)
		}
		if tocMode {
			labels = document.GenerateLabels(len(renderer.Headings()))
			renderer.RenderTOC(labels)
		}
		if navMode && len(doc.Navigation) > 0 {
			// Count total links across all nav sections
			totalLinks := 0
			for _, nav := range doc.Navigation {
				totalLinks += len(nav.Children)
			}
			labels = document.GenerateLabels(totalLinks)
			navLinks = renderer.RenderNavigation(doc.Navigation, labels, navScrollOffset)
		}
		if textEntry && activeInput != nil {
			// Draw text entry prompt at bottom of screen
			prompt := fmt.Sprintf(" %s: %s█ ", activeInput.Name, enteredText)
			for x := 0; x < width; x++ {
				canvas.Set(x, height-1, ' ', render.Style{Reverse: true})
			}
			canvas.WriteString(0, height-1, prompt, render.Style{Reverse: true, Bold: true})
		}
		if urlMode {
			// Draw URL input as centered overlay box (like TOC)
			boxWidth := 50
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			boxHeight := 5
			startX := (width - boxWidth) / 2
			startY := (height - boxHeight) / 2

			// Clear box area
			for y := startY; y < startY+boxHeight; y++ {
				for x := startX; x < startX+boxWidth; x++ {
					canvas.Set(x, y, ' ', render.Style{})
				}
			}

			// Draw border
			canvas.DrawBox(startX, startY, boxWidth, boxHeight, render.DoubleBox, render.Style{})

			// Title
			title := " Open URL "
			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

			// URL input field
			inputY := startY + 2
			maxURLWidth := boxWidth - 4
			displayURL := urlInput
			if len(displayURL) > maxURLWidth-1 {
				displayURL = displayURL[len(displayURL)-maxURLWidth+1:]
			}
			canvas.WriteString(startX+2, inputY, displayURL+"█", render.Style{})

			// Hint
			hint := " Enter to go, ESC to cancel "
			hintX := startX + (boxWidth-len(hint))/2
			canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
		}
		canvas.RenderTo(os.Stdout)
	}

	redraw()

	// Handle resize signals in background
	go func() {
		for range resizeCh {
			handleResize()
			redraw()
		}
	}()

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

				// Check for exact match
				matched := false
				links := renderer.Links()
				for i, label := range labels {
					if label == jumpInput && i < len(links) {
						// Found a match - navigate!
						matched = true
						jumpMode = false
						jumpInput = ""

						newURL := resolveURL(url, links[i].Href)
						loading = true

						// Show loading status
						canvas.Clear()
						canvas.WriteString(width/2-10, height/2, "Loading...", render.Style{Bold: true})
						canvas.RenderTo(os.Stdout)

						newDoc, err := fetchAndParseQuiet(newURL)
						loading = false
						if err == nil {
							// Push current page to history before navigating
							history = append(history, historyEntry{
								url:     url,
								doc:     doc,
								scrollY: scrollY,
							})
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

				// If no match yet, check if input could still match something
				if !matched {
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
			}
			continue
		}

		// Input selection mode handling
		if inputMode {
			switch {
			case buf[0] == 27: // Escape - cancel input mode
				inputMode = false
				jumpInput = ""
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				inputs := renderer.Inputs()
				for i, label := range labels {
					if label == jumpInput && i < len(inputs) {
						// Found a match - enter text entry mode
						matched = true
						inputMode = false
						jumpInput = ""
						textEntry = true
						activeInput = &inputs[i]
						enteredText = ""
						redraw()
						break
					}
				}

				// If no match yet, check if input could still match something
				if !matched {
					couldMatch := false
					for _, label := range labels {
						if strings.HasPrefix(label, jumpInput) {
							couldMatch = true
							break
						}
					}
					if !couldMatch {
						inputMode = false
						jumpInput = ""
						redraw()
					}
				}
			}
			continue
		}

		// Text entry mode handling
		if textEntry {
			switch {
			case buf[0] == 27: // Escape - cancel text entry
				textEntry = false
				activeInput = nil
				enteredText = ""
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - submit form
				if activeInput != nil && activeInput.FormAction != "" {
					// Build the URL with query parameter (URL-encoded)
					formURL := resolveURL(url, activeInput.FormAction)
					if strings.Contains(formURL, "?") {
						formURL += "&"
					} else {
						formURL += "?"
					}
					formURL += activeInput.Name + "=" + neturl.QueryEscape(enteredText)

					textEntry = false
					activeInput = nil
					enteredText = ""

					// Show loading status
					canvas.Clear()
					canvas.WriteString(width/2-10, height/2, "Loading...", render.Style{Bold: true})
					canvas.RenderTo(os.Stdout)

					// Navigate to the form result
					newDoc, err := fetchAndParseQuiet(formURL)
					if err == nil {
						history = append(history, historyEntry{
							url:     url,
							doc:     doc,
							scrollY: scrollY,
						})
						doc = newDoc
						url = formURL
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
						scrollY = 0
					}
				}
				redraw()

			case buf[0] == 127 || buf[0] == 8: // Backspace
				if len(enteredText) > 0 {
					enteredText = enteredText[:len(enteredText)-1]
					redraw()
				}

			case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
				enteredText += string(buf[0])
				redraw()
			}
			continue
		}

		// TOC mode input handling
		if tocMode {
			switch {
			case buf[0] == 27: // Escape - cancel TOC mode
				tocMode = false
				jumpInput = ""
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				headings := renderer.Headings()
				for i, label := range labels {
					if label == jumpInput && i < len(headings) {
						// Found a match - jump to heading!
						matched = true
						tocMode = false
						jumpInput = ""

						// Scroll to heading position
						scrollY = headings[i].Y
						if scrollY > maxScroll {
							scrollY = maxScroll
						}
						if scrollY < 0 {
							scrollY = 0
						}
						redraw()
						break
					}
				}

				// If no match yet, check if input could still match something
				if !matched {
					couldMatch := false
					for _, label := range labels {
						if strings.HasPrefix(label, jumpInput) {
							couldMatch = true
							break
						}
					}
					if !couldMatch {
						tocMode = false
						jumpInput = ""
						redraw()
					}
				}
			}
			continue
		}

		// Navigation mode input handling
		if navMode {
			switch {
			case buf[0] == 27: // Escape - cancel nav mode
				navMode = false
				jumpInput = ""
				navScrollOffset = 0
				redraw()

			case buf[0] == 'j': // Scroll down in nav
				navScrollOffset++
				// Clamp to max scroll
				totalNavLinks := 0
				for _, nav := range doc.Navigation {
					totalNavLinks += len(nav.Children)
				}
				if navScrollOffset > totalNavLinks-1 {
					navScrollOffset = totalNavLinks - 1
				}
				if navScrollOffset < 0 {
					navScrollOffset = 0
				}
				redraw()

			case buf[0] == 'k': // Scroll up in nav
				navScrollOffset--
				if navScrollOffset < 0 {
					navScrollOffset = 0
				}
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'j' && buf[0] != 'k':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				for i, label := range labels {
					if label == jumpInput && i < len(navLinks) {
						// Found a match - navigate to the link!
						matched = true
						navMode = false
						jumpInput = ""
						navScrollOffset = 0

						newURL := resolveURL(url, navLinks[i].Href)
						loading = true

						// Show loading status
						canvas.Clear()
						canvas.WriteString(width/2-10, height/2, "Loading...", render.Style{Bold: true})
						canvas.RenderTo(os.Stdout)

						newDoc, err := fetchAndParseQuiet(newURL)
						loading = false
						if err == nil {
							history = append(history, historyEntry{
								url:     url,
								doc:     doc,
								scrollY: scrollY,
							})
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

				// If no match yet, check if input could still match something
				if !matched {
					couldMatch := false
					for _, label := range labels {
						if strings.HasPrefix(label, jumpInput) {
							couldMatch = true
							break
						}
					}
					if !couldMatch {
						navMode = false
						jumpInput = ""
						navScrollOffset = 0
						redraw()
					}
				}
			}
			continue
		}

		// URL input mode handling
		if urlMode {
			switch {
			case buf[0] == 27: // Escape - cancel URL mode
				urlMode = false
				urlInput = ""
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - navigate to URL
				if urlInput != "" {
					// Add https:// if no scheme provided
					targetURL := urlInput
					if !strings.Contains(targetURL, "://") {
						targetURL = "https://" + targetURL
					}

					urlMode = false
					urlInput = ""
					loading = true

					// Show loading status on screen
					canvas.Clear()
					canvas.WriteString(width/2-10, height/2, "Loading...", render.Style{Bold: true})
					canvas.RenderTo(os.Stdout)

					newDoc, err := fetchAndParseQuiet(targetURL)
					loading = false
					if err == nil {
						history = append(history, historyEntry{
							url:     url,
							doc:     doc,
							scrollY: scrollY,
						})
						doc = newDoc
						url = targetURL
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
						scrollY = 0
					}
					redraw()
				}

			case buf[0] == 127 || buf[0] == 8: // Backspace
				if len(urlInput) > 0 {
					urlInput = urlInput[:len(urlInput)-1]
					redraw()
				}

			case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
				urlInput += string(buf[0])
				redraw()
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

		case buf[0] == 't': // table of contents
			if len(renderer.Headings()) > 0 {
				tocMode = true
				jumpInput = ""
				redraw()
			}

		case buf[0] == 'i': // input - enter input mode
			if len(renderer.Inputs()) > 0 {
				inputMode = true
				jumpInput = ""
				redraw()
			}

		case buf[0] == 'n': // navigation - show nav links overlay
			if len(doc.Navigation) > 0 {
				navMode = true
				jumpInput = ""
				redraw()
			}

		case buf[0] == 'b': // back
			if len(history) > 0 {
				// Pop from history
				prev := history[len(history)-1]
				history = history[:len(history)-1]

				url = prev.url
				doc = prev.doc
				scrollY = prev.scrollY
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case buf[0] == 'H': // home
			homeDoc, err := landingPage()
			if err == nil {
				history = append(history, historyEntry{
					url:     url,
					doc:     doc,
					scrollY: scrollY,
				})
				doc = homeDoc
				url = "browse://home"
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				scrollY = 0
				redraw()
			}

		case buf[0] == 'r': // reload with browser (JS rendering)
			if url != "" && url != "browse://home" {
				// Show loading status
				canvas.Clear()
				canvas.WriteString(width/2-12, height/2, "Loading (JS)...", render.Style{Bold: true})
				canvas.RenderTo(os.Stdout)

				newDoc, err := fetchWithBrowserQuiet(url)
				if err == nil {
					doc = newDoc
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
					scrollY = 0
				}
				redraw()
			}

		case buf[0] == 'o': // open URL
			urlMode = true
			urlInput = ""
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

func fetchAndParse(url string) (*html.Document, error) {
	// Start spinner in background
	done := make(chan bool)
	go showSpinner(done)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		done <- true
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		done <- true
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	done <- true
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

func fetchWithBrowser(url string) (*html.Document, error) {
	// Start spinner in background (different animation for browser mode)
	done := make(chan bool)
	go showBrowserSpinner(done)

	result, err := fetcher.WithBrowser(url)
	done <- true
	if err != nil {
		return nil, err
	}

	doc, err := html.ParseString(result.HTML)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

// fetchAndParseQuiet fetches without spinner (for use when already in alt screen)
func fetchAndParseQuiet(url string) (*html.Document, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
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

// fetchWithBrowserQuiet uses headless Chrome without spinner
func fetchWithBrowserQuiet(url string) (*html.Document, error) {
	result, err := fetcher.WithBrowser(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.ParseString(result.HTML)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

func showBrowserSpinner(done chan bool) {
	frames := []string{"◐", "◓", "◑", "◒"} // Different spinner for browser mode
	i := 0
	for {
		select {
		case <-done:
			fmt.Print("\r     \r")
			return
		default:
			fmt.Printf("\r%s JS", frames[i%len(frames)])
			i++
			time.Sleep(150 * time.Millisecond)
		}
	}
}

func showSpinner(done chan bool) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	for {
		select {
		case <-done:
			// Clear spinner
			fmt.Print("\r   \r")
			return
		default:
			fmt.Printf("\r%s", frames[i%len(frames)])
			i++
			time.Sleep(80 * time.Millisecond)
		}
	}
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

func landingPage() (*html.Document, error) {
	page := `<!DOCTYPE html>
<html>
<head><title>Browse - Terminal Web Browser</title></head>
<body>
<article>
<h1>Browse</h1>
<p>A terminal-based web browser for reading the web in beautiful monospace.</p>

<h2>Navigation</h2>
<p>
<strong>j/k</strong> - scroll down/up |
<strong>d/u</strong> - half page down/up |
<strong>g/G</strong> - top/bottom |
<strong>o</strong> - open URL |
<strong>f</strong> - follow link |
<strong>t</strong> - table of contents |
<strong>n</strong> - site navigation |
<strong>b</strong> - back |
<strong>H</strong> - home |
<strong>i</strong> - input field |
<strong>r</strong> - reload with JS |
<strong>q</strong> - quit
</p>

<h2>News</h2>
<ul>
<li><a href="https://text.npr.org">NPR Text</a> - National Public Radio (text version)</li>
<li><a href="https://lite.cnn.com">CNN Lite</a> - CNN (lightweight version)</li>
<li><a href="https://lobste.rs">Lobsters</a> - Computing-focused link aggregator</li>
</ul>

<h2>Reference</h2>
<ul>
<li><a href="https://en.wikipedia.org">Wikipedia</a> - The free encyclopedia</li>
<li><a href="https://go.dev/doc/effective_go">Effective Go</a> - Go programming guide</li>
<li><a href="https://man.archlinux.org">Arch Manual Pages</a> - Linux manual pages</li>
</ul>

<h2>Blogs</h2>
<ul>
<li><a href="https://kungfusheep.com/articles">kungfusheep</a> - Software engineering articles</li>
<li><a href="https://blog.golang.org">Go Blog</a> - Official Go blog</li>
<li><a href="https://jvns.ca">Julia Evans</a> - Programming zines and posts</li>
<li><a href="https://danluu.com">Dan Luu</a> - Systems and performance</li>
</ul>

<h2>Tools</h2>
<ul>
<li><a href="https://wttr.in">wttr.in</a> - Weather in your terminal</li>
<li><a href="https://example.com">example.com</a> - Test page</li>
</ul>

<p><em>Press 'f' to follow a link, or pass a URL as an argument.</em></p>
</article>
</body>
</html>`

	return html.ParseString(page)
}
