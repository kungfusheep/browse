// Browse is a terminal-based web browser focused on beautiful text layouts.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"browse/document"
	"browse/fetcher"
	"browse/html"
	"browse/inspector"
	"browse/llm"
	"browse/render"
	"browse/rules"
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
	wideMode := false // toggle with 'w' for full-width view
	renderer := document.NewRendererWide(canvas, wideMode)

	// Set up AI rule generation
	llmClient := llm.NewClient(
		llm.NewClaudeCode(),      // Prefer Claude Code CLI (free for CC users)
		llm.NewClaudeAPI(""),     // Fall back to API if available
	)
	ruleCache, _ := rules.NewCache("") // Uses ~/.config/browse/rules/
	ruleGeneratorV1 := rules.NewGenerator(llmClient)   // Legacy list-based rules
	ruleGeneratorV2 := rules.NewGeneratorV2(llmClient) // New template-based rules

	// Store raw HTML for rule generation
	var currentHTML string

	// History for back/forward navigation
	type historyEntry struct {
		url     string
		doc     *html.Document
		scrollY int
	}
	var history []historyEntry
	var forwardHistory []historyEntry

	// State
	contentHeight := renderer.ContentHeight(doc)
	maxScroll := contentHeight - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollY := 0
	jumpMode := false
	inputMode := false      // selecting an input field
	textEntry := false      // entering text into a field
	tocMode := false        // showing table of contents
	navMode := false        // showing navigation overlay
	navScrollOffset := 0    // scroll position within nav overlay
	urlMode := false        // entering a URL
	loading := false        // currently loading a page
	structureMode := false  // showing DOM structure inspector
	jumpInput := ""
	var labels []string
	var navLinks []document.NavLink             // current navigation links in overlay
	var activeInput *document.Input             // currently selected input
	var enteredText string                      // text being entered
	var urlInput string                         // URL being entered
	var structureViewer *inspector.Viewer       // DOM structure viewer

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
			renderer = document.NewRendererWide(canvas, wideMode)
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

	// Extract domain from URL for status bar
	getDomain := func(u string) string {
		if u == "" || u == "browse://home" {
			return "browse://home"
		}
		// Parse the URL to get the host
		parsed, err := neturl.Parse(u)
		if err != nil {
			return u
		}
		return parsed.Host
	}

	// Render helper
	redraw := func() {
		// Structure inspector mode - separate rendering
		if structureMode && structureViewer != nil {
			structureViewer.Render()
			canvas.RenderTo(os.Stdout)
			return
		}

		renderer.Render(doc, scrollY)

		// Draw subtle status bar at bottom with current domain
		domain := getDomain(url)
		statusY := height - 1

		// Build status line: domain on left, [W] indicator, scroll % on right
		domainDisplay := domain
		if len(domainDisplay) > width-15 {
			domainDisplay = domainDisplay[:width-15] + "…"
		}

		var pctStr string
		if contentHeight > height {
			pct := 0
			if maxScroll > 0 {
				pct = scrollY * 100 / maxScroll
			}
			pctStr = fmt.Sprintf("%d%%", pct)
		}

		// Draw as dim text - subtle but visible
		canvas.WriteString(0, statusY, domainDisplay, render.Style{Dim: true})

		// Show wide mode indicator
		if wideMode {
			wideIndicator := "[W]"
			wideX := len(domainDisplay) + 1
			canvas.WriteString(wideX, statusY, wideIndicator, render.Style{Dim: true})
		}

		if pctStr != "" {
			canvas.WriteString(width-len(pctStr), statusY, pctStr, render.Style{Dim: true})
		}

		// Draw scroll indicator on right edge (above status bar)
		scrollHeight := height - 1 // Leave room for status bar
		if contentHeight > height && scrollHeight > 0 {
			// Calculate thumb position and size
			thumbHeight := scrollHeight * scrollHeight / contentHeight
			if thumbHeight < 1 {
				thumbHeight = 1
			}
			thumbPos := 0
			if maxScroll > 0 {
				thumbPos = scrollY * (scrollHeight - thumbHeight) / maxScroll
			}

			// Draw track and thumb (only up to scrollHeight, not into status bar)
			for y := 0; y < scrollHeight; y++ {
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

						newDoc, htmlContent, err := fetchWithRules(newURL, ruleCache)
						loading = false
						if err == nil {
							// Clear forward history on new navigation
							forwardHistory = nil
							// Push current page to history before navigating
							history = append(history, historyEntry{
								url:     url,
								doc:     doc,
								scrollY: scrollY,
							})
							doc = newDoc
							url = newURL
							currentHTML = htmlContent
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
					newDoc, htmlContent, err := fetchWithRules(formURL, ruleCache)
					if err == nil {
						forwardHistory = nil
						history = append(history, historyEntry{
							url:     url,
							doc:     doc,
							scrollY: scrollY,
						})
						doc = newDoc
						url = formURL
						currentHTML = htmlContent
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

						newDoc, htmlContent, err := fetchWithRules(newURL, ruleCache)
						loading = false
						if err == nil {
							forwardHistory = nil
							history = append(history, historyEntry{
								url:     url,
								doc:     doc,
								scrollY: scrollY,
							})
							doc = newDoc
							url = newURL
							currentHTML = htmlContent
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

					newDoc, htmlContent, err := fetchWithRules(targetURL, ruleCache)
					loading = false
					if err == nil {
						forwardHistory = nil
						history = append(history, historyEntry{
							url:     url,
							doc:     doc,
							scrollY: scrollY,
						})
						doc = newDoc
						url = targetURL
						currentHTML = htmlContent
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

		// Structure inspector mode input handling
		if structureMode && structureViewer != nil {
			switch {
			case buf[0] == 27: // Escape - exit structure mode
				structureMode = false
				structureViewer = nil
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - apply changes and exit
				// Get visible content from inspector and create new document
				blocks := structureViewer.GetVisibleContent()
				if newDoc := html.FromInspectorBlocks(blocks); newDoc != nil {
					// Push current doc to history so user can go back
					history = append(history, historyEntry{
						url:     url,
						doc:     doc,
						scrollY: scrollY,
					})
					doc = newDoc
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
					scrollY = 0
				}
				structureMode = false
				structureViewer = nil
				redraw()

			case buf[0] == ' ': // Space - toggle visibility
				structureViewer.ToggleSelected()
				redraw()

			case buf[0] == 'j' || buf[0] == 14: // j or Ctrl+N - move down
				structureViewer.MoveDown()
				redraw()

			case buf[0] == 'k' || buf[0] == 16: // k or Ctrl+P - move up
				structureViewer.MoveUp()
				redraw()

			case buf[0] == 'h': // h - collapse
				structureViewer.Collapse()
				redraw()

			case buf[0] == 'l': // l - expand
				structureViewer.Expand()
				redraw()

			case n >= 3 && buf[0] == 27 && buf[1] == '[': // Arrow keys
				switch buf[2] {
				case 'A': // Up arrow
					structureViewer.MoveUp()
					redraw()
				case 'B': // Down arrow
					structureViewer.MoveDown()
					redraw()
				case 'C': // Right arrow - expand
					structureViewer.Expand()
					redraw()
				case 'D': // Left arrow - collapse
					structureViewer.Collapse()
					redraw()
				}

			case buf[0] == 'T': // Shift+T - toggle recursively
				structureViewer.ToggleSelectedRecursive()
				redraw()

			case buf[0] >= '1' && buf[0] <= '9': // Quick select suggestion
				structureViewer.SelectSuggestion(int(buf[0] - '0'))
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

		case buf[0] == 'w': // toggle wide mode (80 chars vs full width)
			wideMode = !wideMode
			renderer = document.NewRendererWide(canvas, wideMode)
			contentHeight = renderer.ContentHeight(doc)
			maxScroll = contentHeight - height
			if maxScroll < 0 {
				maxScroll = 0
			}
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case buf[0] == 's': // structure inspector
			if currentHTML != "" {
				var err error
				structureViewer, err = inspector.NewViewer(currentHTML, canvas)
				if err == nil {
					structureMode = true
					redraw()
				}
			}

		case buf[0] == 'b': // back
			if len(history) > 0 {
				// Push current page to forward history
				forwardHistory = append(forwardHistory, historyEntry{
					url:     url,
					doc:     doc,
					scrollY: scrollY,
				})

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

		case buf[0] == 'B': // forward
			if len(forwardHistory) > 0 {
				// Push current page to history
				history = append(history, historyEntry{
					url:     url,
					doc:     doc,
					scrollY: scrollY,
				})

				// Pop from forward history
				next := forwardHistory[len(forwardHistory)-1]
				forwardHistory = forwardHistory[:len(forwardHistory)-1]

				url = next.url
				doc = next.doc
				scrollY = next.scrollY
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
				forwardHistory = nil
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

		case buf[0] == 'R': // Generate AI rules for current site
			if url != "" && url != "browse://home" && llmClient.Available() {
				domain := getDomain(url)

				// Show generating status
				canvas.Clear()
				providerName := "AI"
				if p := llmClient.Provider(); p != nil {
					providerName = p.Name()
				}
				canvas.WriteString(width/2-18, height/2-1, "Generating template with "+providerName+"...", render.Style{Bold: true})
				canvas.WriteString(width/2-12, height/2+1, "(v2 template system)", render.Style{Dim: true})
				canvas.RenderTo(os.Stdout)

				// Fetch fresh HTML if we don't have it
				if currentHTML == "" {
					_, htmlContent, err := fetchQuietWithHTML(url)
					if err == nil {
						currentHTML = htmlContent
					}
				}

				if currentHTML != "" {
					var newDoc *html.Document
					var rule *rules.Rule
					var useV2 bool

					// Try v2 template system first
					ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
					rule, err := ruleGeneratorV2.GeneratePageType(ctx, domain, url, currentHTML)
					cancel()

					if err == nil && rule != nil {
						// Apply v2 rules
						if result, applyErr := rules.ApplyV2(rule, url, currentHTML); applyErr == nil && result != nil && result.Content != "" {
							newDoc = html.FromTemplateResult(result, domain)
							useV2 = true
						}
					}

					// Fall back to v1 if v2 didn't work
					if newDoc == nil {
						canvas.Clear()
						canvas.WriteString(width/2-12, height/2, "Trying v1 fallback...", render.Style{Dim: true})
						canvas.RenderTo(os.Stdout)

						ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
						rule, err = ruleGeneratorV1.Generate(ctx, domain, currentHTML)
						cancel()

						if err == nil && rule != nil {
							if result := rules.Apply(rule, currentHTML); result != nil {
								newDoc = html.FromRules(result)
							}
						}
					}

					if newDoc != nil {
						// Save the rule
						_ = ruleCache.Put(rule, true)

						// Show success
						canvas.Clear()
						if useV2 {
							canvas.WriteString(width/2-12, height/2, "✓ Template generated!", render.Style{Bold: true})
						} else if rule != nil && rule.Verified {
							canvas.WriteString(width/2-10, height/2, "✓ Rules validated!", render.Style{Bold: true})
						} else {
							canvas.WriteString(width/2-12, height/2, "Rules generated (unverified)", render.Style{Bold: true})
						}
						canvas.RenderTo(os.Stdout)
						time.Sleep(500 * time.Millisecond)

						// Push current to history so user can go back
						forwardHistory = nil
						history = append(history, historyEntry{
							url:     url,
							doc:     doc,
							scrollY: scrollY,
						})
						doc = newDoc
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
						scrollY = 0
					} else if err != nil {
						// Show error
						canvas.Clear()
						errMsg := err.Error()
						if strings.Contains(errMsg, "default parser") {
							canvas.WriteString(width/2-18, height/2, "Default parser works best for this site", render.Style{Dim: true})
							canvas.RenderTo(os.Stdout)
							time.Sleep(1 * time.Second)
						} else {
							if len(errMsg) > width-4 {
								errMsg = errMsg[:width-7] + "..."
							}
							canvas.WriteString(2, height/2, "Error: "+errMsg, render.Style{Bold: true})
							canvas.RenderTo(os.Stdout)
							time.Sleep(2 * time.Second)
						}
					}
				}
				redraw()
			}

		case buf[0] == 'o': // open URL
			urlMode = true
			urlInput = ""
			redraw()

		case buf[0] == 'y': // yank (copy) URL to clipboard
			if err := copyToClipboard(url); err == nil {
				// Brief visual feedback - show "Copied!" in status area
				canvas.Clear()
				renderer.Render(doc, scrollY)
				statusY := height - 1
				msg := "URL copied to clipboard"
				canvas.WriteString(0, statusY, msg, render.Style{Bold: true})
				canvas.RenderTo(os.Stdout)
				time.Sleep(800 * time.Millisecond)
			}
			redraw()

		case buf[0] == 'E': // Edit in vim
			// Render full document to get formatted content
			fullHeight := contentHeight + 10
			if fullHeight < height {
				fullHeight = height
			}
			editCanvas := render.NewCanvas(width, fullHeight)
			editRenderer := document.NewRendererWide(editCanvas, wideMode)
			editRenderer.Render(doc, 0)
			content := editCanvas.PlainText()

			// Write to temp file
			tmpFile, err := os.CreateTemp("", "browse-*.txt")
			if err != nil {
				redraw()
				continue
			}
			tmpPath := tmpFile.Name()
			tmpFile.WriteString(content)
			tmpFile.Close()

			// Restore terminal for vim
			term.RestoreMode()
			render.ExitAltScreen(os.Stdout)

			// Launch vim (or $EDITOR)
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vim"
			}
			cmd := exec.Command(editor, tmpPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()

			// Read back edited content
			editedContent, err := os.ReadFile(tmpPath)
			os.Remove(tmpPath)

			// Re-enter raw mode and alt screen
			render.EnterAltScreen(os.Stdout)
			term.EnterRawMode()

			// If content was edited, create a simple document from it
			if err == nil && string(editedContent) != content {
				// Create a simple document with the edited text
				editedDoc := &html.Document{
					Content: &html.Node{Type: html.NodeDocument},
				}
				// Split into paragraphs
				paragraphs := strings.Split(string(editedContent), "\n\n")
				for _, p := range paragraphs {
					p = strings.TrimSpace(p)
					if p != "" {
						para := &html.Node{Type: html.NodeParagraph}
						para.Children = append(para.Children, &html.Node{
							Type: html.NodeText,
							Text: p,
						})
						editedDoc.Content.Children = append(editedDoc.Content.Children, para)
					}
				}
				doc = editedDoc
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height + 2
				if maxScroll < 0 {
					maxScroll = 0
				}
				scrollY = 0
			}
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

		case buf[0] == '[': // Previous paragraph
			paragraphs := renderer.Paragraphs()
			for i := len(paragraphs) - 1; i >= 0; i-- {
				if paragraphs[i] < scrollY {
					scrollY = paragraphs[i]
					if scrollY < 0 {
						scrollY = 0
					}
					break
				}
			}
			redraw()

		case buf[0] == ']': // Next paragraph
			paragraphs := renderer.Paragraphs()
			for _, p := range paragraphs {
				if p > scrollY {
					scrollY = p
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
					break
				}
			}
			redraw()

		case buf[0] == '{': // Previous section (heading)
			headings := renderer.Headings()
			for i := len(headings) - 1; i >= 0; i-- {
				if headings[i].Y < scrollY {
					scrollY = headings[i].Y
					if scrollY < 0 {
						scrollY = 0
					}
					break
				}
			}
			redraw()

		case buf[0] == '}': // Next section (heading)
			headings := renderer.Headings()
			for _, h := range headings {
				if h.Y > scrollY {
					scrollY = h.Y
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
					break
				}
			}
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

// fetchQuietWithHTML fetches and returns both parsed doc and raw HTML.
func fetchQuietWithHTML(targetURL string) (*html.Document, string, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	// Read body into buffer so we can use it twice
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading body: %w", err)
	}

	doc, err := html.ParseString(string(body))
	if err != nil {
		return nil, "", fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, string(body), nil
}

// fetchWithRules fetches and parses, applying cached rules if available.
func fetchWithRules(targetURL string, cache *rules.Cache) (*html.Document, string, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading body: %w", err)
	}

	htmlContent := string(body)

	// Always parse with default parser first
	defaultDoc, err := html.ParseString(htmlContent)
	if err != nil {
		return nil, "", fmt.Errorf("parsing HTML: %w", err)
	}

	// Try to apply cached rules and compare quality
	if cache != nil {
		if rule := cache.GetForURL(targetURL); rule != nil {
			if result := rules.Apply(rule, htmlContent); result != nil {
				if rulesDoc := html.FromRules(result); rulesDoc != nil {
					// Only use rules if they produce better quality output
					if isRulesDocBetter(rulesDoc, defaultDoc, result) {
						return rulesDoc, htmlContent, nil
					}
				}
			}
		}
	}

	return defaultDoc, htmlContent, nil
}

// isRulesDocBetter checks if the rules-based document is actually better than default.
// Rules win if they have meaningful structured content, not garbage.
func isRulesDocBetter(rulesDoc, defaultDoc *html.Document, result *rules.ApplyResult) bool {
	if result == nil || len(result.Items) < 3 {
		return false
	}

	// Check for quality indicators
	goodTitles := 0
	totalTitleLen := 0
	hasLinks := 0

	for _, item := range result.Items {
		titleLen := len(item.Title)
		totalTitleLen += titleLen

		// Good title: reasonable length, not just numbers/punctuation
		if titleLen >= 10 && titleLen <= 500 {
			// Check it's not just section numbers like "1.1.1"
			nonDigitCount := 0
			for _, r := range item.Title {
				if r < '0' || r > '9' {
					if r != '.' && r != ' ' {
						nonDigitCount++
					}
				}
			}
			if nonDigitCount > 5 {
				goodTitles++
			}
		}

		if item.Href != "" && item.Href != "#" {
			hasLinks++
		}
	}

	// Quality checks:
	// 1. Average title length should be reasonable (not too short like "EN" or "1.1")
	avgTitleLen := float64(totalTitleLen) / float64(len(result.Items))
	if avgTitleLen < 15 {
		return false
	}

	// 2. Most titles should be "good" (not section numbers)
	goodRatio := float64(goodTitles) / float64(len(result.Items))
	if goodRatio < 0.5 {
		return false
	}

	// 3. For list-type content, should have decent link coverage
	if result.LayoutType == "list" || result.LayoutType == "newspaper" {
		linkRatio := float64(hasLinks) / float64(len(result.Items))
		if linkRatio < 0.3 {
			return false
		}
	}

	return true
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

func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	default:
		return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
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
<strong>[/]</strong> - prev/next paragraph |
<strong>{/}</strong> - prev/next section |
<strong>o</strong> - open URL |
<strong>y</strong> - copy URL |
<strong>E</strong> - edit in $EDITOR |
<strong>f</strong> - follow link |
<strong>t</strong> - table of contents |
<strong>n</strong> - site navigation |
<strong>b/B</strong> - back/forward |
<strong>H</strong> - home |
<strong>s</strong> - structure inspector |
<strong>w</strong> - wide mode |
<strong>i</strong> - input field |
<strong>r</strong> - reload with JS |
<strong>R</strong> - generate AI rules |
<strong>q</strong> - quit
</p>

<h2>News</h2>
<ul>
<li><a href="https://www.bbc.com/news">BBC News</a> - British Broadcasting Corporation</li>
<li><a href="https://www.nytimes.com">New York Times</a> - All the news that's fit to print</li>
<li><a href="https://www.theguardian.com">The Guardian</a> - Independent journalism</li>
<li><a href="https://www.reuters.com">Reuters</a> - International news agency</li>
<li><a href="https://www.washingtonpost.com">Washington Post</a> - Democracy dies in darkness</li>
<li><a href="https://www.wsj.com">Wall Street Journal</a> - Business and financial news</li>
<li><a href="https://www.cnn.com">CNN</a> - Cable News Network</li>
<li><a href="https://apnews.com">Associated Press</a> - AP News</li>
<li><a href="https://www.npr.org">NPR</a> - National Public Radio</li>
<li><a href="https://news.ycombinator.com">Hacker News</a> - Y Combinator tech news</li>
</ul>

<h3>Lightweight versions</h3>
<ul>
<li><a href="https://text.npr.org">NPR Text</a> - NPR (text-only version)</li>
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

<h2>Search</h2>
<ul>
<li><a href="https://html.duckduckgo.com/html/">DuckDuckGo</a> - Privacy-focused search (HTML version)</li>
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
