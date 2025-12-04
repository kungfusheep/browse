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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"browse/config"
	"browse/document"
	"browse/favourites"
	"browse/fetcher"
	"browse/html"
	"browse/inspector"
	"browse/llm"
	"browse/render"
	"browse/rules"
	"browse/search"
)

func main() {
	url := ""
	printMode := false
	initConfig := false

	for _, arg := range os.Args[1:] {
		switch arg {
		case "-p", "--print":
			printMode = true
		case "--init-config":
			initConfig = true
		case "-h", "--help":
			printUsage()
			return
		default:
			if url == "" {
				url = arg
			}
		}
	}

	// Generate default config and exit
	if initConfig {
		fmt.Print(config.DefaultPkl())
		return
	}

	if printMode {
		if err := runPrint(url); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(url); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Browse - Terminal Web Browser

Usage: browse [options] [url]

Options:
  -p, --print       Print page to stdout (one-shot mode)
  --init-config     Output default config (redirect to ~/.config/browse/config.pkl)
  -h, --help        Show this help

Examples:
  browse                          Open landing page
  browse https://example.com      Open URL
  browse -p https://example.com   Print page to stdout
  browse --init-config > ~/.config/browse/config.pkl

Configuration:
  Config file: ~/.config/browse/config.pkl
  Generate with: browse --init-config > ~/.config/browse/config.pkl`)
}

func runPrint(url string) error {
	var doc *html.Document
	var err error

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Configure fetcher with user settings
	fetcher.Configure(fetcher.Options{
		UserAgent:      cfg.Fetcher.UserAgent,
		TimeoutSeconds: cfg.Fetcher.TimeoutSeconds,
		ChromePath:     cfg.Fetcher.ChromePath,
	})

	// Configure HTML parsing options
	html.Configure(html.Options{
		LatexEnabled:  cfg.Rendering.LatexEnabled,
		TablesEnabled: cfg.Rendering.TablesEnabled,
	})

	// Configure document rendering options
	document.Configure(document.Options{
		MaxContentWidth: cfg.Rendering.DefaultWidth,
	})

	// Load favourites for landing page
	favStore, _ := favourites.Load()

	if url == "" {
		doc, err = landingPage(favStore)
	} else {
		doc, err = fetchAndParseQuiet(url)
	}
	if err != nil {
		return err
	}

	// Use terminal width if available, otherwise config default
	width := cfg.Rendering.DefaultWidth
	if w, _, werr := render.TerminalSize(); werr == nil {
		width = w
	}

	// Render to a tall canvas to capture full content
	height := 10000
	canvas := render.NewCanvas(width, height)
	renderer := document.NewRenderer(canvas)
	renderer.Render(doc, 0)

	fmt.Print(canvas.PlainText())
	return nil
}

func run(url string) error {
	var doc *html.Document
	var err error

	// Load configuration (defaults + user overrides)
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w\n\n%s", err, config.FormatError(err))
	}

	// Configure the fetcher with user settings
	fetcher.Configure(fetcher.Options{
		UserAgent:      cfg.Fetcher.UserAgent,
		TimeoutSeconds: cfg.Fetcher.TimeoutSeconds,
		ChromePath:     cfg.Fetcher.ChromePath,
	})

	// Configure HTML parsing options
	html.Configure(html.Options{
		LatexEnabled:  cfg.Rendering.LatexEnabled,
		TablesEnabled: cfg.Rendering.TablesEnabled,
	})

	// Configure document rendering options
	document.Configure(document.Options{
		MaxContentWidth: cfg.Rendering.DefaultWidth,
	})

	// Load favourites early so landing page can show them
	favStore, _ := favourites.Load()

	if url == "" {
		// Show landing page
		doc, err = landingPage(favStore)
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
	wideMode := cfg.Display.WideMode // from config, toggle with 'w'
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

	// Page state for history
	type pageState struct {
		url     string
		doc     *html.Document
		scrollY int
		html    string // raw HTML for rule generation
	}

	// Buffers - vim-style tabs, each with its own history
	type buffer struct {
		history []pageState // back history stack
		current pageState   // current page
		forward []pageState // forward history stack (after going back)
	}

	// Initialize with single buffer containing the starting page
	buffers := []buffer{{
		current: pageState{url: url, doc: doc, scrollY: 0, html: currentHTML},
	}}
	currentBufferIdx := 0

	// Helper to get current buffer
	getCurrentBuffer := func() *buffer {
		return &buffers[currentBufferIdx]
	}

	// Helper to navigate within current buffer (updates history)
	var navigateTo func(newURL string, newDoc *html.Document, newHTML string)

	// Helper to open a new buffer with a page
	var openNewBuffer func(newURL string, newDoc *html.Document, newHTML string)

	// State
	contentHeight := renderer.ContentHeight(doc)
	maxScroll := contentHeight - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollY := 0
	jumpMode := false
	inputMode := false       // selecting an input field
	textEntry := false       // entering text into a field
	tocMode := false         // showing table of contents
	navMode := false         // showing navigation overlay
	navScrollOffset := 0     // scroll position within nav overlay
	linkIndexMode := false   // showing link index overlay
	linkScrollOffset := 0    // scroll position within link index
	bufferMode := false      // showing buffer list overlay
	bufferScrollOffset := 0  // scroll position within buffer list
	urlMode := false         // entering a URL
	searchMode := false      // entering a search query
	loading := false         // currently loading a page
	structureMode := false   // showing DOM structure inspector
	jumpInput := ""
	gPending := false // waiting for second key after 'g'

	// Key matching helpers for configurable bindings
	key := func(input byte, binding string) bool {
		return config.MatchSingle(input, binding)
	}
	// keyG checks if gPending + input matches a 2-char binding
	keyG := func(input byte, binding string) bool {
		return gPending && config.MatchWithPrefix("g", input, binding)
	}
	kb := cfg.Keybindings // shorthand for keybindings

	var labels []string
	var navLinks []document.NavLink             // current navigation links in overlay
	var activeInput *document.Input             // currently selected input
	var enteredText string                      // text being entered
	var urlInput string                         // URL being entered
	var searchInput string                      // search query being entered
	var structureViewer *inspector.Viewer       // DOM structure viewer
	searchProvider := search.ProviderByName(cfg.Search.DefaultProvider) // web search provider

	// Favourites mode state (favStore loaded at start of run())
	favouritesMode := false      // showing favourites overlay
	favouritesScrollOffset := 0  // scroll position within favourites
	favouritesDeleteMode := false // waiting for label to delete

	// Define navigateTo helper - navigates within current buffer (updates history)
	navigateTo = func(newURL string, newDoc *html.Document, newHTML string) {
		buf := getCurrentBuffer()
		// Save current state to history
		buf.current.scrollY = scrollY
		buf.history = append(buf.history, buf.current)
		// Clear forward history (new navigation breaks forward chain)
		buf.forward = nil
		// Update current
		buf.current = pageState{url: newURL, doc: newDoc, scrollY: 0, html: newHTML}
		// Update local state
		doc = newDoc
		url = newURL
		currentHTML = newHTML
		contentHeight = renderer.ContentHeight(doc)
		maxScroll = contentHeight - height
		if maxScroll < 0 {
			maxScroll = 0
		}
		scrollY = 0
	}

	// Define openNewBuffer helper - opens a new buffer (like a new tab)
	openNewBuffer = func(newURL string, newDoc *html.Document, newHTML string) {
		// Save scroll position in current buffer
		getCurrentBuffer().current.scrollY = scrollY
		// Create new buffer with the page
		buffers = append(buffers, buffer{
			current: pageState{url: newURL, doc: newDoc, scrollY: 0, html: newHTML},
		})
		currentBufferIdx = len(buffers) - 1
		// Update local state
		doc = newDoc
		url = newURL
		currentHTML = newHTML
		contentHeight = renderer.ContentHeight(doc)
		maxScroll = contentHeight - height
		if maxScroll < 0 {
			maxScroll = 0
		}
		scrollY = 0
	}

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
		if cfg.Display.ShowScrollPercentage && contentHeight > height {
			pct := 0
			if maxScroll > 0 {
				pct = scrollY * 100 / maxScroll
			}
			pctStr = fmt.Sprintf("%d%%", pct)
		}

		// Draw as dim text - subtle but visible
		if cfg.Display.ShowUrl {
			canvas.WriteString(0, statusY, domainDisplay, render.Style{Dim: true})
		}

		// Show wide mode indicator
		if wideMode {
			wideIndicator := "[W]"
			wideX := 0
			if cfg.Display.ShowUrl {
				wideX = len(domainDisplay) + 1
			}
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
			canvas.DimAll()
			labels = document.GenerateLabels(len(renderer.Headings()))
			renderer.RenderTOC(labels)
		}
		if navMode && len(doc.Navigation) > 0 {
			canvas.DimAll()
			// Count total links across all nav sections
			totalLinks := 0
			for _, nav := range doc.Navigation {
				totalLinks += len(nav.Children)
			}
			labels = document.GenerateLabels(totalLinks)
			navLinks = renderer.RenderNavigation(doc.Navigation, labels, navScrollOffset)
		}
		if linkIndexMode {
			canvas.DimAll()
			labels = document.GenerateLabels(len(renderer.Links()))
			renderer.RenderLinkIndex(labels, linkScrollOffset)
		}
		if bufferMode {
			canvas.DimAll()
			labels = document.GenerateLabels(len(buffers))

			// Draw buffer list as centered overlay box
			boxWidth := 70
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			boxHeight := len(buffers) + 4
			if boxHeight > height-4 {
				boxHeight = height - 4
			}

			startX := (width - boxWidth) / 2
			startY := (height - boxHeight) / 2

			// Clear box area
			for by := startY; by < startY+boxHeight; by++ {
				for bx := startX; bx < startX+boxWidth; bx++ {
					canvas.Set(bx, by, ' ', render.Style{})
				}
			}

			// Draw border
			canvas.DrawBox(startX, startY, boxWidth, boxHeight, render.DoubleBox, render.Style{})

			// Title
			title := fmt.Sprintf(" Buffers (%d) ", len(buffers))
			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

			// Draw buffers with labels
			by := startY + 2
			visibleCount := boxHeight - 4
			for i := bufferScrollOffset; i < len(buffers) && i < bufferScrollOffset+visibleCount; i++ {
				if i >= len(labels) {
					break
				}

				buf := buffers[i]
				bx := startX + 2

				// Format: [label] [*] title - url
				label := labels[i]
				isCurrent := i == currentBufferIdx

				// Extract title from URL or use URL
				displayTitle := buf.current.url
				if len(displayTitle) > 40 {
					displayTitle = displayTitle[:37] + "..."
				}

				// Draw label (highlighted)
				for j, ch := range label {
					canvas.Set(bx+j, by, ch, render.Style{Reverse: true, Bold: true})
				}

				// Current buffer marker
				marker := "  "
				if isCurrent {
					marker = " *"
				}
				canvas.WriteString(bx+len(label), by, marker, render.Style{Bold: isCurrent})

				// URL
				canvas.WriteString(bx+len(label)+2, by, displayTitle, render.Style{Bold: isCurrent})

				by++
			}

			// Scroll indicators
			if bufferScrollOffset > 0 {
				canvas.WriteString(startX+boxWidth-4, startY+2, "↑", render.Style{Dim: true})
			}
			if bufferScrollOffset+visibleCount < len(buffers) {
				canvas.WriteString(startX+boxWidth-4, startY+boxHeight-3, "↓", render.Style{Dim: true})
			}

			// Footer hint
			hint := " label=switch  x=close  gt/gT=next/prev  ESC=cancel "
			hintX := startX + (boxWidth-len(hint))/2
			canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
		}
		if favouritesMode && favStore != nil {
			canvas.DimAll()
			labels = document.GenerateLabels(favStore.Len())

			// Draw favourites list as centered overlay box
			boxWidth := 70
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			boxHeight := favStore.Len() + 4
			if boxHeight < 6 {
				boxHeight = 6 // minimum size
			}
			if boxHeight > height-4 {
				boxHeight = height - 4
			}

			startX := (width - boxWidth) / 2
			startY := (height - boxHeight) / 2

			// Clear box area
			for by := startY; by < startY+boxHeight; by++ {
				for bx := startX; bx < startX+boxWidth; bx++ {
					canvas.Set(bx, by, ' ', render.Style{})
				}
			}

			// Draw border
			canvas.DrawBox(startX, startY, boxWidth, boxHeight, render.DoubleBox, render.Style{})

			// Title
			title := fmt.Sprintf(" Favourites (%d) ", favStore.Len())
			if favouritesDeleteMode {
				title = " DELETE: type label to remove "
			}
			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

			if favStore.Len() == 0 {
				// Empty state
				emptyMsg := "No favourites yet. Press M on any page to add."
				msgX := startX + (boxWidth-len(emptyMsg))/2
				canvas.WriteString(msgX, startY+2, emptyMsg, render.Style{Dim: true})
			} else {
				// Draw favourites with labels
				by := startY + 2
				visibleCount := boxHeight - 4
				for i := favouritesScrollOffset; i < favStore.Len() && i < favouritesScrollOffset+visibleCount; i++ {
					if i >= len(labels) {
						break
					}

					fav := favStore.Favourites[i]
					bx := startX + 2

					// Format: [label] title (truncated URL)
					label := labels[i]

					// Draw label (highlighted, red if delete mode)
					labelStyle := render.Style{Reverse: true, Bold: true}
					if favouritesDeleteMode {
						labelStyle.Reverse = false
						labelStyle.Bold = true
					}
					for j, ch := range label {
						canvas.Set(bx+j, by, ch, labelStyle)
					}

					// Title
					displayTitle := fav.Title
					if displayTitle == "" {
						displayTitle = fav.URL
					}
					maxLen := boxWidth - len(label) - 6
					if len(displayTitle) > maxLen {
						displayTitle = displayTitle[:maxLen-3] + "..."
					}
					canvas.WriteString(bx+len(label)+2, by, displayTitle, render.Style{})

					by++
				}

				// Scroll indicators
				if favouritesScrollOffset > 0 {
					canvas.WriteString(startX+boxWidth-4, startY+2, "↑", render.Style{Dim: true})
				}
				if favouritesScrollOffset+visibleCount < favStore.Len() {
					canvas.WriteString(startX+boxWidth-4, startY+boxHeight-3, "↓", render.Style{Dim: true})
				}
			}

			// Footer hint
			hint := " label=open  d=delete mode  ESC=cancel "
			if favouritesDeleteMode {
				hint = " label=DELETE  ESC=cancel "
			}
			hintX := startX + (boxWidth-len(hint))/2
			canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
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
			canvas.DimAll()
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
		if searchMode {
			canvas.DimAll()
			// Draw search input as centered overlay box
			boxWidth := 60
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
			title := fmt.Sprintf(" Search (%s) ", searchProvider.Name())
			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

			// Search input field
			inputY := startY + 2
			maxQueryWidth := boxWidth - 4
			displayQuery := searchInput
			if len(displayQuery) > maxQueryWidth-1 {
				displayQuery = displayQuery[len(displayQuery)-maxQueryWidth+1:]
			}
			canvas.WriteString(startX+2, inputY, displayQuery+"█", render.Style{})

			// Hint
			hint := " Enter to search, ESC to cancel "
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
						newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
						loading = false
						if err == nil {
							navigateTo(newURL, newDoc, htmlContent)
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

					// Navigate to the form result
					newDoc, htmlContent, err := fetchWithSpinner(canvas, formURL, ruleCache)
					if err == nil {
						navigateTo(formURL, newDoc, htmlContent)
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
						newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
						loading = false
						if err == nil {
							navigateTo(newURL, newDoc, htmlContent)
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

		// Link index mode input handling
		if linkIndexMode {
			switch {
			case buf[0] == 27: // Escape - cancel link index mode
				linkIndexMode = false
				jumpInput = ""
				linkScrollOffset = 0
				redraw()

			case buf[0] == 'j': // Scroll down
				linkScrollOffset++
				links := renderer.Links()
				if linkScrollOffset > len(links)-1 {
					linkScrollOffset = len(links) - 1
				}
				if linkScrollOffset < 0 {
					linkScrollOffset = 0
				}
				redraw()

			case buf[0] == 'k': // Scroll up
				linkScrollOffset--
				if linkScrollOffset < 0 {
					linkScrollOffset = 0
				}
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'j' && buf[0] != 'k':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				links := renderer.Links()
				for i, label := range labels {
					if label == jumpInput && i < len(links) {
						// Found a match - navigate to the link!
						matched = true
						linkIndexMode = false
						jumpInput = ""
						linkScrollOffset = 0

						newURL := resolveURL(url, links[i].Href)
						loading = true
						newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
						loading = false
						if err == nil {
							navigateTo(newURL, newDoc, htmlContent)
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
						linkIndexMode = false
						jumpInput = ""
						linkScrollOffset = 0
						redraw()
					}
				}
			}
			continue
		}

		// Buffer mode input handling
		if bufferMode {
			switch {
			case buf[0] == 27: // Escape - cancel buffer mode
				bufferMode = false
				jumpInput = ""
				bufferScrollOffset = 0
				redraw()

			case buf[0] == 'j': // Scroll down
				bufferScrollOffset++
				if bufferScrollOffset > len(buffers)-1 {
					bufferScrollOffset = len(buffers) - 1
				}
				if bufferScrollOffset < 0 {
					bufferScrollOffset = 0
				}
				redraw()

			case buf[0] == 'k': // Scroll up
				bufferScrollOffset--
				if bufferScrollOffset < 0 {
					bufferScrollOffset = 0
				}
				redraw()

			case buf[0] == 'x': // Close buffer under cursor
				targetIdx := bufferScrollOffset
				// Find which buffer the label would point to
				for i, label := range labels {
					if label == jumpInput && i < len(buffers) {
						targetIdx = i
						break
					}
				}
				if len(buffers) > 1 && targetIdx < len(buffers) {
					// Save current buffer's scroll position
					buffers[currentBufferIdx].current.scrollY = scrollY

					// Remove the buffer
					buffers = append(buffers[:targetIdx], buffers[targetIdx+1:]...)

					// Adjust current buffer index if needed
					if currentBufferIdx >= len(buffers) {
						currentBufferIdx = len(buffers) - 1
					}
					if targetIdx <= currentBufferIdx && currentBufferIdx > 0 {
						currentBufferIdx--
					}

					// Switch to current buffer
					buf := getCurrentBuffer()
					doc = buf.current.doc
					url = buf.current.url
					scrollY = buf.current.scrollY
					currentHTML = buf.current.html
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}

					// Adjust scroll offset if needed
					if bufferScrollOffset >= len(buffers) {
						bufferScrollOffset = len(buffers) - 1
					}
					redraw()
				}

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'j' && buf[0] != 'k' && buf[0] != 'x':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				for i, label := range labels {
					if label == jumpInput && i < len(buffers) {
						// Found a match - switch to buffer!
						matched = true
						bufferMode = false
						jumpInput = ""
						bufferScrollOffset = 0

						// Save current buffer's scroll position
						buffers[currentBufferIdx].current.scrollY = scrollY

						// Switch to selected buffer
						currentBufferIdx = i
						buf := getCurrentBuffer()
						doc = buf.current.doc
						url = buf.current.url
						scrollY = buf.current.scrollY
						currentHTML = buf.current.html
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
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
						bufferMode = false
						jumpInput = ""
						bufferScrollOffset = 0
						redraw()
					}
				}
			}
			continue
		}

		// Favourites mode input handling
		if favouritesMode {
			switch {
			case buf[0] == 27: // Escape - cancel favourites mode
				favouritesMode = false
				favouritesDeleteMode = false
				jumpInput = ""
				favouritesScrollOffset = 0
				redraw()

			case buf[0] == 'j': // Scroll down
				favouritesScrollOffset++
				if favouritesScrollOffset > favStore.Len()-1 {
					favouritesScrollOffset = favStore.Len() - 1
				}
				if favouritesScrollOffset < 0 {
					favouritesScrollOffset = 0
				}
				redraw()

			case buf[0] == 'k': // Scroll up
				favouritesScrollOffset--
				if favouritesScrollOffset < 0 {
					favouritesScrollOffset = 0
				}
				redraw()

			case buf[0] == 'd' && !favouritesDeleteMode: // Enter delete mode
				favouritesDeleteMode = true
				jumpInput = ""
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'j' && buf[0] != 'k' && buf[0] != 'd':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				for i, label := range labels {
					if label == jumpInput && i < favStore.Len() {
						matched = true
						if favouritesDeleteMode {
							// Delete this favourite
							favStore.Remove(i)
							favStore.Save()
							favouritesDeleteMode = false
							jumpInput = ""
							// Adjust scroll if needed
							if favouritesScrollOffset >= favStore.Len() && favouritesScrollOffset > 0 {
								favouritesScrollOffset = favStore.Len() - 1
							}
							redraw()
						} else {
							// Open this favourite
							fav := favStore.Favourites[i]
							favouritesMode = false
							jumpInput = ""
							favouritesScrollOffset = 0

							// Show loading
							loading = true
							redraw()

							// Fetch the page
							newDoc, rawHTML, err := fetchQuietWithHTML(fav.URL)
							loading = false
							if err == nil {
								navigateTo(fav.URL, newDoc, rawHTML)
							}
							redraw()
						}
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
						// Invalid input, reset
						jumpInput = ""
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
					newDoc, htmlContent, err := fetchWithSpinner(canvas, targetURL, ruleCache)
					loading = false
					if err == nil {
						navigateTo(targetURL, newDoc, htmlContent)
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

		// Search mode input handling
		if searchMode {
			switch {
			case buf[0] == 27: // Escape - cancel search mode
				searchMode = false
				searchInput = ""
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - execute search
				if searchInput != "" {
					searchMode = false
					query := searchInput
					searchInput = ""
					loading = true

					// Show loading status
					canvas.Clear()
					canvas.WriteString(width/2-10, height/2, "Searching...", render.Style{Bold: true})
					canvas.RenderTo(os.Stdout)

					results, err := searchProvider.Search(query)
					loading = false
					if err == nil && results != nil {
						// Convert results to HTML and parse as document
						htmlContent := results.ToHTML()
						newDoc, parseErr := html.ParseString(htmlContent)
						if parseErr == nil {
							navigateTo("search://"+query, newDoc, htmlContent)
						}
					}
					redraw()
				}

			case buf[0] == 127 || buf[0] == 8: // Backspace
				if len(searchInput) > 0 {
					searchInput = searchInput[:len(searchInput)-1]
					redraw()
				}

			case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
				searchInput += string(buf[0])
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
					// Update current buffer with modified document
					doc = newDoc
					buffers[currentBufferIdx].current.doc = newDoc
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
		case key(buf[0], kb.Quit):
			return nil

		case key(buf[0], kb.FollowLink): // follow link - enter jump mode
			jumpMode = true
			jumpInput = ""
			redraw()

		case keyG(buf[0], kb.NextBuffer): // gt - next buffer
			gPending = false
			if len(buffers) > 1 {
				buffers[currentBufferIdx].current.scrollY = scrollY
				currentBufferIdx++
				if currentBufferIdx >= len(buffers) {
					currentBufferIdx = 0 // wrap around
				}
				b := getCurrentBuffer()
				doc = b.current.doc
				url = b.current.url
				scrollY = b.current.scrollY
				currentHTML = b.current.html
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case key(buf[0], kb.TableOfContents): // t - table of contents
			if !gPending && len(renderer.Headings()) > 0 {
				tocMode = true
				jumpInput = ""
				redraw()
			}

		case key(buf[0], kb.InputField): // input - enter input mode
			if len(renderer.Inputs()) > 0 {
				inputMode = true
				jumpInput = ""
				redraw()
			}

		case key(buf[0], kb.SiteNavigation): // navigation - show nav links overlay
			if len(doc.Navigation) > 0 {
				navMode = true
				jumpInput = ""
				redraw()
			}

		case key(buf[0], kb.LinkIndex): // link index - show all page links
			if len(renderer.Links()) > 0 {
				linkIndexMode = true
				linkScrollOffset = 0
				jumpInput = ""
				redraw()
			}

		case key(buf[0], kb.Forward): // forward in history (within current buffer)
			buf := getCurrentBuffer()
			if len(buf.forward) > 0 {
				// Save current to history
				buf.current.scrollY = scrollY
				buf.history = append(buf.history, buf.current)
				// Pop from forward
				buf.current = buf.forward[len(buf.forward)-1]
				buf.forward = buf.forward[:len(buf.forward)-1]
				// Update local state
				doc = buf.current.doc
				url = buf.current.url
				scrollY = buf.current.scrollY
				currentHTML = buf.current.html
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case key(buf[0], kb.BufferList) || keyG(buf[0], kb.BufferList): // buffer list - show all open buffers
			gPending = false
			bufferMode = true
			bufferScrollOffset = 0
			jumpInput = ""
			redraw()

		case key(buf[0], kb.FavouritesList): // favourites list
			favouritesMode = true
			favouritesScrollOffset = 0
			favouritesDeleteMode = false
			jumpInput = ""
			redraw()

		case key(buf[0], kb.AddFavourite): // add current page to favourites
			if url != "" && url != "browse://home" && favStore != nil {
				// Get page title from first heading, or use URL
				title := url
				headings := renderer.Headings()
				if len(headings) > 0 && headings[0].Text != "" {
					title = headings[0].Text
				}
				if favStore.Add(url, title) {
					favStore.Save()
					// Show brief confirmation
					canvas.Clear()
					canvas.WriteString(width/2-10, height/2, "Added to favourites!", render.Style{Bold: true})
					canvas.RenderTo(os.Stdout)
					time.Sleep(300 * time.Millisecond)
				} else {
					// Already exists
					canvas.Clear()
					canvas.WriteString(width/2-12, height/2, "Already in favourites", render.Style{Dim: true})
					canvas.RenderTo(os.Stdout)
					time.Sleep(300 * time.Millisecond)
				}
				redraw()
			}

		case key(buf[0], kb.NewBuffer) && !gPending: // T - open new buffer (duplicate current page)
			openNewBuffer(url, doc, currentHTML)
			redraw()

		case key(buf[0], kb.ToggleWideMode): // toggle wide mode (80 chars vs full width)
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

		case key(buf[0], kb.StructureInspector): // structure inspector
			if currentHTML != "" {
				var err error
				structureViewer, err = inspector.NewViewer(currentHTML, canvas)
				if err == nil {
					structureMode = true
					redraw()
				}
			}

		case key(buf[0], kb.Back): // back in history (within current buffer)
			buf := getCurrentBuffer()
			if len(buf.history) > 0 {
				// Save current to forward
				buf.current.scrollY = scrollY
				buf.forward = append(buf.forward, buf.current)
				// Pop from history
				buf.current = buf.history[len(buf.history)-1]
				buf.history = buf.history[:len(buf.history)-1]
				// Update local state
				doc = buf.current.doc
				url = buf.current.url
				scrollY = buf.current.scrollY
				currentHTML = buf.current.html
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case key(buf[0], kb.Home): // home
			homeDoc, err := landingPage(favStore)
			if err == nil {
				navigateTo("browse://home", homeDoc, "")
				redraw()
			}

		case key(buf[0], kb.ReloadWithJs): // reload with browser (JS rendering)
			if url != "" && url != "browse://home" {
				newDoc, err := fetchBrowserWithSpinner(canvas, url)
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

		case key(buf[0], kb.GenerateRules): // Generate AI rules for current site
			if url != "" && url != "browse://home" && llmClient.Available() {
				domain := getDomain(url)
				providerName := "AI"
				if p := llmClient.Provider(); p != nil {
					providerName = p.Name()
				}

				// Fetch fresh HTML if we don't have it
				if currentHTML == "" {
					_, htmlContent, err := fetchWithSpinner(canvas, url, ruleCache)
					if err == nil {
						currentHTML = htmlContent
					}
				}

				if currentHTML != "" {
					var newDoc *html.Document
					var rule *rules.Rule
					var useV2 bool

					// Try v2 template system first with spinner
					rule, err := generateRulesWithSpinner(canvas, ruleGeneratorV2, domain, url, currentHTML, providerName)

					if err == nil && rule != nil {
						// Apply v2 rules
						if result, applyErr := rules.ApplyV2(rule, url, currentHTML); applyErr == nil && result != nil && result.Content != "" {
							newDoc = html.FromTemplateResult(result, domain)
							useV2 = true
						}
					}

					// Fall back to v1 if v2 didn't work
					if newDoc == nil {
						rule, err = generateRulesV1WithSpinner(canvas, ruleGeneratorV1, domain, currentHTML, providerName)

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

						// Update current buffer with the new parsed document
						doc = newDoc
						buffers[currentBufferIdx].current.doc = newDoc
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
						scrollY = 0
						buffers[currentBufferIdx].current.scrollY = 0
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

		case key(buf[0], kb.OpenUrl): // open URL
			urlMode = true
			urlInput = ""
			redraw()

		case key(buf[0], kb.Search): // search the web
			searchMode = true
			searchInput = ""
			redraw()

		case key(buf[0], kb.CopyUrl): // yank (copy) URL to clipboard
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

		case key(buf[0], kb.EditInEditor): // Edit in vim
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

		case key(buf[0], kb.EditConfig): // Edit config in $EDITOR and hot-reload
			configPath, err := config.ConfigPath()
			if err != nil {
				redraw()
				continue
			}

			// Create config file with defaults if it doesn't exist
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				os.MkdirAll(filepath.Dir(configPath), 0755)
				os.WriteFile(configPath, []byte(config.DefaultPkl()), 0644)
			}

			// Restore terminal for editor
			term.RestoreMode()
			render.ExitAltScreen(os.Stdout)

			// Launch editor
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vim"
			}
			cmd := exec.Command(editor, configPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()

			// Re-enter raw mode and alt screen
			render.EnterAltScreen(os.Stdout)
			term.EnterRawMode()

			// Reload config
			newCfg, err := config.Load()
			if err != nil {
				// Show error message
				canvas.Clear()
				canvas.WriteString(2, height/2-1, "Config Error:", render.Style{Bold: true})
				errMsg := err.Error()
				// Truncate if too long
				if len(errMsg) > width-4 {
					errMsg = errMsg[:width-7] + "..."
				}
				canvas.WriteString(2, height/2+1, errMsg, render.Style{})
				canvas.WriteString(2, height/2+3, "Press any key to continue with previous config", render.Style{Dim: true})
				canvas.RenderTo(os.Stdout)
				// Wait for keypress
				keyBuf := make([]byte, 1)
				os.Stdin.Read(keyBuf)
			} else {
				// Apply new config
				cfg = newCfg

				// Re-apply all package configurations
				fetcher.Configure(fetcher.Options{
					UserAgent:      cfg.Fetcher.UserAgent,
					TimeoutSeconds: cfg.Fetcher.TimeoutSeconds,
					ChromePath:     cfg.Fetcher.ChromePath,
				})
				html.Configure(html.Options{
					LatexEnabled:  cfg.Rendering.LatexEnabled,
					TablesEnabled: cfg.Rendering.TablesEnabled,
				})
				document.Configure(document.Options{
					MaxContentWidth: cfg.Rendering.DefaultWidth,
				})

				// Apply display settings and recreate renderer
				wideMode = cfg.Display.WideMode
				renderer = document.NewRendererWide(canvas, wideMode)
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}

				// Show success briefly
				canvas.Clear()
				canvas.WriteString(width/2-10, height/2, "Config reloaded!", render.Style{Bold: true})
				canvas.RenderTo(os.Stdout)
				time.Sleep(300 * time.Millisecond)
			}
			redraw()

		case key(buf[0], kb.ScrollDown), buf[0] == 14: // j or Ctrl+N
			scrollY++
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case key(buf[0], kb.ScrollUp), buf[0] == 16: // k or Ctrl+P
			scrollY--
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case key(buf[0], kb.HalfPageDown), buf[0] == 4: // d or Ctrl+D
			scrollY += height / 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case key(buf[0], kb.HalfPageUp), buf[0] == 21: // u or Ctrl+U
			scrollY -= height / 2
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case keyG(buf[0], kb.GoTop): // gg - go to top
			gPending = false
			scrollY = 0
			redraw()

		case buf[0] == 'g' && !gPending:
			// Start g-prefix mode
			gPending = true

		case keyG(buf[0], kb.PrevBuffer): // gT - previous buffer
			gPending = false
			if len(buffers) > 1 {
				buffers[currentBufferIdx].current.scrollY = scrollY
				currentBufferIdx--
				if currentBufferIdx < 0 {
					currentBufferIdx = len(buffers) - 1 // wrap around
				}
				b := getCurrentBuffer()
				doc = b.current.doc
				url = b.current.url
				scrollY = b.current.scrollY
				currentHTML = b.current.html
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case key(buf[0], kb.GoBottom):
			gPending = false
			scrollY = maxScroll
			redraw()

		case key(buf[0], kb.PrevParagraph): // Previous paragraph
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

		case key(buf[0], kb.NextParagraph): // Next paragraph
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

		case key(buf[0], kb.PrevSection): // Previous section (heading)
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

		case key(buf[0], kb.NextSection): // Next section (heading)
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
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
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
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
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
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
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
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
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

func landingPage(favStore *favourites.Store) (*html.Document, error) {
	// Build favourites section if we have any
	var favouritesSection string
	if favStore != nil && favStore.Len() > 0 {
		favouritesSection = "\n<h2>Favourites</h2>\n<ul>\n"
		for _, fav := range favStore.Favourites {
			// Escape HTML in title
			title := strings.ReplaceAll(fav.Title, "&", "&amp;")
			title = strings.ReplaceAll(title, "<", "&lt;")
			title = strings.ReplaceAll(title, ">", "&gt;")
			title = strings.ReplaceAll(title, "\"", "&quot;")
			favouritesSection += fmt.Sprintf("<li><a href=\"%s\">%s</a></li>\n", fav.URL, title)
		}
		favouritesSection += "</ul>\n"
	}

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
<strong>/</strong> - web search |
<strong>y</strong> - copy URL |
<strong>E</strong> - edit in $EDITOR |
<strong>f</strong> - follow link |
<strong>t</strong> - table of contents |
<strong>n</strong> - site navigation |
<strong>l</strong> - link index |
<strong>b/B</strong> - back/forward |
<strong>T</strong> - new buffer |
<strong>gt/gT</strong> - next/prev buffer |
<strong>&#96;</strong> - buffer list |
<strong>M</strong> - add favourite |
<strong>'</strong> - favourites list |
<strong>H</strong> - home |
<strong>s</strong> - structure inspector |
<strong>w</strong> - wide mode |
<strong>i</strong> - input field |
<strong>r</strong> - reload with JS |
<strong>R</strong> - generate AI rules |
<strong>C</strong> - edit config |
<strong>q</strong> - quit
</p>
` + favouritesSection + `
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

// withSpinner runs a function while showing an animated spinner.
// The work function runs in a goroutine while the spinner animates.
func withSpinner[T any](canvas *render.Canvas, message string, work func() T) T {
	resultCh := make(chan T, 1)

	go func() {
		resultCh <- work()
	}()

	loader := render.NewLoadingDisplay(render.SpinnerWave, message)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case result := <-resultCh:
			return result
		case <-ticker.C:
			loader.Tick()
			canvas.Clear()
			loader.DrawBox(canvas, "")
			canvas.RenderTo(os.Stdout)
		}
	}
}

// fetchResult holds the result of a fetch operation.
type fetchResult struct {
	doc     *html.Document
	content string
	err     error
}

// fetchWithSpinner fetches a URL while showing an animated spinner.
func fetchWithSpinner(canvas *render.Canvas, targetURL string, ruleCache *rules.Cache) (*html.Document, string, error) {
	result := withSpinner(canvas, targetURL, func() fetchResult {
		doc, content, err := fetchWithRules(targetURL, ruleCache)
		return fetchResult{doc, content, err}
	})
	return result.doc, result.content, result.err
}

// browserResult holds the result of a browser fetch operation.
type browserResult struct {
	doc *html.Document
	err error
}

// fetchBrowserWithSpinner fetches a URL using browser (JS) while showing spinner.
func fetchBrowserWithSpinner(canvas *render.Canvas, targetURL string) (*html.Document, error) {
	result := withSpinner(canvas, targetURL+" (JS)", func() browserResult {
		doc, err := fetchWithBrowserQuiet(targetURL)
		return browserResult{doc, err}
	})
	return result.doc, result.err
}

// ruleGenResult holds the result of AI rule generation.
type ruleGenResult struct {
	rule *rules.Rule
	err  error
}

// generateRulesWithSpinner generates AI rules (v2) while showing spinner.
func generateRulesWithSpinner(canvas *render.Canvas, generator *rules.GeneratorV2, domain, targetURL, htmlContent, providerName string) (*rules.Rule, error) {
	result := withSpinner(canvas, "Generating template with "+providerName+"...", func() ruleGenResult {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		rule, err := generator.GeneratePageType(ctx, domain, targetURL, htmlContent)
		return ruleGenResult{rule, err}
	})
	return result.rule, result.err
}

// generateRulesV1WithSpinner generates AI rules (v1 fallback) while showing spinner.
func generateRulesV1WithSpinner(canvas *render.Canvas, generator *rules.Generator, domain, htmlContent, providerName string) (*rules.Rule, error) {
	result := withSpinner(canvas, "Trying v1 fallback with "+providerName+"...", func() ruleGenResult {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		rule, err := generator.Generate(ctx, domain, htmlContent)
		return ruleGenResult{rule, err}
	})
	return result.rule, result.err
}
