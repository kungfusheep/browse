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
	"browse/dict"
	"browse/document"
	"browse/favourites"
	"browse/fetcher"
	"browse/html"
	"browse/inspector"
	"browse/lineedit"
	"browse/llm"
	"browse/omnibox"
	"browse/render"
	"browse/rss"
	"browse/rules"
	"browse/search"
	"browse/session"
	"browse/sites"
	"browse/theme"
	"browse/translate"

	// Site-specific handlers register themselves via init()
	_ "browse/hn"
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

	switch {
	case url == "" || strings.HasPrefix(url, "browse://"):
		doc, _, err = handleBrowseURL(url, favStore)
	default:
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

	// Load RSS store for feed management
	rssStore, _ := rss.Load()

	// Start RSS background poller (30 minute default interval)
	rssPoller := rss.NewPoller(rssStore, 30*time.Minute)
	rssPoller.Start()
	defer rssPoller.Stop()

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
		llm.NewClaudeCode(),  // Prefer Claude Code CLI (free for CC users)
		llm.NewClaudeAPI(""), // Fall back to API if available
	)
	ruleCache, _ := rules.NewCache("")                 // Uses ~/.config/browse/rules/
	ruleGeneratorV1 := rules.NewGenerator(llmClient)   // Legacy list-based rules
	ruleGeneratorV2 := rules.NewGeneratorV2(llmClient) // New template-based rules

	// Store raw HTML for rule generation
	var currentHTML string

	// AI chat session - holds conversation context for ai:// pages
	type chatMessage struct {
		Role    string // "user" or "assistant"
		Content string
	}
	type chatSession struct {
		SourceURL     string        // Original page URL
		SourceContent string        // Original page content (plain text)
		Messages      []chatMessage // Conversation history (summary is first assistant message)
		SessionID     string        // Claude Code session ID for conversation continuity
	}

	// Page state for history
	type pageState struct {
		url     string
		doc     *html.Document
		scrollY int
		html    string       // raw HTML for rule generation
		chat    *chatSession // AI chat session (nil for regular pages)
	}

	// Buffers - vim-style tabs, each with its own history
	type buffer struct {
		history []pageState // back history stack
		current pageState   // current page
		forward []pageState // forward history stack (after going back)
	}

	var buffers []buffer
	currentBufferIdx := 0

	// Try to restore session if enabled and no URL provided
	sessionRestored := false
	if cfg.Session.RestoreSession && url == "" {
		if savedSession, err := session.Load(); err == nil && len(savedSession.Buffers) > 0 {
			// Restore buffers from session
			for _, sb := range savedSession.Buffers {
				var pageDoc *html.Document
				var pageHTML string
				pageURL := sb.Current.URL
				if pageURL == "" || strings.HasPrefix(pageURL, "browse://") {
					pageDoc, pageHTML, _ = handleBrowseURL(pageURL, favStore)
				} else if strings.HasPrefix(pageURL, "rss://") {
					pageDoc, _ = handleRSSURL(rssStore, pageURL)
				} else {
					pageDoc, _ = fetchAndParse(pageURL)
				}
				if pageDoc != nil {
					b := buffer{
						current: pageState{url: pageURL, doc: pageDoc, scrollY: sb.Current.ScrollY, html: pageHTML},
					}
					// Restore back history (docs will be fetched on demand)
					for _, h := range sb.History {
						b.history = append(b.history, pageState{
							url:     h.URL,
							scrollY: h.ScrollY,
							doc:     nil, // fetched when navigating back
						})
					}
					// Restore forward history
					for _, f := range sb.Forward {
						b.forward = append(b.forward, pageState{
							url:     f.URL,
							scrollY: f.ScrollY,
							doc:     nil, // fetched when navigating forward
						})
					}
					buffers = append(buffers, b)
				}
			}
			if len(buffers) > 0 {
				currentBufferIdx = savedSession.CurrentBufferIdx
				if currentBufferIdx >= len(buffers) {
					currentBufferIdx = 0
				}
				sessionRestored = true
			}
		}
	}

	// If session wasn't restored, start fresh
	if !sessionRestored {
		var pageHTML string
		switch {
		case url == "":
			doc, pageHTML, err = handleBrowseURL("browse://home", favStore)
			url = "browse://home"
		case strings.HasPrefix(url, "browse://"):
			doc, pageHTML, err = handleBrowseURL(url, favStore)
		default:
			doc, err = fetchAndParse(url)
		}
		if err != nil {
			return err
		}
		currentHTML = pageHTML
		buffers = []buffer{{
			current: pageState{url: url, doc: doc, scrollY: 0, html: currentHTML},
		}}
	}

	// Set initial state from current buffer
	url = buffers[currentBufferIdx].current.url
	doc = buffers[currentBufferIdx].current.doc
	scrollY := buffers[currentBufferIdx].current.scrollY
	currentHTML = buffers[currentBufferIdx].current.html

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

	// Handle initial URL hash/anchor if present
	if hash := extractHash(url); hash != "" {
		if anchorY, found := document.FindAnchorY(doc, hash, renderer.ContentWidth()); found {
			scrollY = anchorY
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			getCurrentBuffer().current.scrollY = scrollY
		}
	}

	jumpMode := false
	inputMode := false                // selecting an input field
	textEntry := false                // entering text into a field
	tocMode := false                  // showing table of contents
	navMode := false                  // showing navigation overlay
	navScrollOffset := 0              // scroll position within nav overlay
	linkIndexMode := false            // showing link index overlay
	linkScrollOffset := 0             // scroll position within link index
	bufferMode := false               // showing buffer list overlay
	bufferScrollOffset := 0           // scroll position within buffer list
	omniMode := false                 // omnibox mode (unified URL + search)
	findMode := false                 // find in page mode
	findInput := ""                   // current find query
	findMatches := []document.Match{} // match positions (from simple doc walker)
	findCurrentIdx := 0               // current match index
	loading := false                  // currently loading a page
	structureMode := false            // showing DOM structure inspector
	jumpInput := ""
	defineMode := false                               // word definition lookup mode
	defineFilter := ""                                // word prefix filter (what user is typing)
	defineAllWords := []render.Word{}                 // all word occurrences (with positions)
	defineUniqueWords := []string{}                   // unique matching word texts (for labels)
	defineWordPositions := map[string][]render.Word{} // word text -> all its positions
	gPending := false                                 // waiting for second key after 'g'

	// Focus mode state - dims non-focused paragraphs for easier reading
	focusModeActive := false // is focus mode currently active?
	focusParagraphStart := 0 // Y start of focused paragraph
	focusParagraphEnd := 0   // Y end of focused paragraph (start of next, or content end)

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
	var navLinks []document.NavLink       // current navigation links in overlay
	var activeInput *document.Input       // currently selected input
	var enteredText string                // text being entered
	var omniInput string                  // omnibox input being entered
	var structureViewer *inspector.Viewer // DOM structure viewer
	chatEditor := lineedit.New()          // chat input editor (for ai:// pages)
	var chatScheme lineedit.KeyScheme     // keybinding scheme (from config)
	if cfg.Editor.Scheme == "vim" {
		chatScheme = lineedit.NewVimScheme()
	} else {
		chatScheme = lineedit.NewEmacsScheme()
	}
	omniParser := omnibox.NewParser() // omnibox input parser

	// Favourites mode state (favStore loaded at start of run())
	favouritesMode := false       // showing favourites overlay
	favouritesScrollOffset := 0   // scroll position within favourites
	favouritesDeleteMode := false // waiting for label to delete

	// Theme picker state
	themePickerMode := false

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
		focusModeActive = false // Reset focus mode on navigation
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
		focusModeActive = false // Reset focus mode on buffer change
	}

	// generateDictHTML renders dictionary definitions as HTML
	generateDictHTML := func(word string, entries []dict.Entry) string {
		var content strings.Builder

		if len(entries) == 0 {
			content.WriteString(fmt.Sprintf(`<p>No definitions found for "<strong>%s</strong>".</p>`, word))
			content.WriteString(`<p>Try a different spelling or check if the word exists.</p>`)
		} else {
			for _, entry := range entries {
				// Show phonetic if available
				if entry.Phonetic != "" {
					content.WriteString(fmt.Sprintf(`<p><em>%s</em></p>`, entry.Phonetic))
				}

				// Show each meaning (part of speech + definitions)
				for _, meaning := range entry.Meanings {
					content.WriteString(fmt.Sprintf(`<h2>%s</h2>`, meaning.PartOfSpeech))

					content.WriteString("<ol>")
					for _, def := range meaning.Definitions {
						content.WriteString("<li>")
						content.WriteString(fmt.Sprintf(`<p>%s</p>`, def.Definition))
						if def.Example != "" {
							content.WriteString(fmt.Sprintf(`<p><em>"%s"</em></p>`, def.Example))
						}
						content.WriteString("</li>")
					}
					content.WriteString("</ol>")

					// Show synonyms if available
					if len(meaning.Synonyms) > 0 {
						content.WriteString(`<p><strong>Synonyms:</strong> `)
						content.WriteString(strings.Join(meaning.Synonyms, ", "))
						content.WriteString(`</p>`)
					}
				}
				content.WriteString("<hr>")
			}
		}

		return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Definition: %s</title></head>
<body>
<article>
<h1>%s</h1>
%s
</article>
</body>
</html>`, word, word, content.String())
	}

	// chatToHTML renders an AI chat session as a full HTML page
	chatToHTML := func(chat *chatSession, editor *lineedit.Editor) string {
		var msgHTML strings.Builder
		for _, msg := range chat.Messages {
			if msg.Role == "user" {
				// User messages in a distinct style
				escaped := strings.ReplaceAll(msg.Content, "<", "&lt;")
				escaped = strings.ReplaceAll(escaped, ">", "&gt;")
				msgHTML.WriteString(fmt.Sprintf(`<blockquote><strong>You:</strong> %s</blockquote>`, escaped))
			} else {
				// Assistant messages render as HTML
				msgHTML.WriteString(msg.Content)
			}
			msgHTML.WriteString("\n<hr>\n")
		}

		// Check if we're in vim normal mode for block cursor rendering
		vimNormalMode := false
		if vim, ok := chatScheme.(*lineedit.VimScheme); ok && !vim.InInsertMode() {
			vimNormalMode = true
		}

		// Use shared cursor rendering
		var inputLine string
		if vimNormalMode {
			inputLine = editor.RenderWithCursor("mark", true)
		} else {
			inputLine = editor.RenderWithCursor("ins", false)
		}

		// Include padding lines below input to ensure it's visible when scrolled to bottom
		inputPrompt := fmt.Sprintf(`<p><strong>&gt;</strong> %s</p>
<p>&nbsp;</p>
<p>&nbsp;</p>
<p>&nbsp;</p>`, inputLine)

		return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>AI Chat</title></head>
<body>
<article>
<h1>AI Chat</h1>
<p><em>Source: <a href="%s">%s</a></em></p>
<hr>
%s
%s
</article>
</body>
</html>`, chat.SourceURL, chat.SourceURL, msgHTML.String(), inputPrompt)
	}

	// generateChatResponseInline generates a response without modal spinner (for inline updates)
	generateChatResponseInline := func(chat *chatSession, userMessage string) string {
		if llmClient == nil || !llmClient.Available() {
			return "<p>AI not available. Please configure an LLM provider (ANTHROPIC_API_KEY).</p>"
		}

		ctx := context.Background()

		// If we have a session ID, use ContinueSession for seamless conversation
		if chat.SessionID != "" {
			wrappedMessage := userMessage + "\n\n(Remember: output as clean HTML using only <h2>, <h3>, <p>, <ul>, <li>, <ol>, <strong>, <em>, <blockquote>. No markdown.)"
			response, err := llmClient.ContinueSession(ctx, chat.SessionID, "", wrappedMessage)
			if err != nil {
				return "<p>Error: " + err.Error() + "</p>"
			}
			return response
		}

		// Fallback: build context in prompt
		var conversationContext strings.Builder
		conversationContext.WriteString("Previous conversation:\n\n")
		for _, msg := range chat.Messages {
			if msg.Role == "user" {
				conversationContext.WriteString("User: " + msg.Content + "\n\n")
			} else {
				conversationContext.WriteString("Assistant: [previous response]\n\n")
			}
		}

		sourceContent := chat.SourceContent
		maxContent := 80000
		if len(sourceContent) > maxContent {
			sourceContent = sourceContent[:maxContent] + "\n\n[Content truncated...]"
		}

		system := `You are a helpful assistant discussing a web page with the user.
Answer thoughtfully and conversationally. Draw on specific details from the page when relevant.
Output as clean HTML using only: <h2>, <h3>, <p>, <ul>, <li>, <ol>, <strong>, <em>, <blockquote>. No markdown.`

		userPrompt := fmt.Sprintf(`Page content:
%s

%s
User's question: %s`, sourceContent, conversationContext.String(), userMessage)

		response, err := llmClient.CompleteWithSystem(ctx, system, userPrompt)
		if err != nil {
			return "<p>Error: " + err.Error() + "</p>"
		}
		return response
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
		// For web URLs, show the host. For internal/non-web schemes, show the full URL.
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			parsed, err := neturl.Parse(u)
			if err != nil {
				return u
			}
			return parsed.Host
		}
		if strings.Contains(u, "://") {
			return u
		}
		return u
	}

	// Render canvas to screen with theme colors
	renderToScreen := func() {
		canvas.RenderToWithBase(os.Stdout, theme.Current.BaseStyle())
	}

	// Render helper
	redraw := func() {
		// Structure inspector mode - separate rendering
		if structureMode && structureViewer != nil {
			structureViewer.Render()
			renderToScreen()
			return
		}

		// Find matches using simple document walker (single source of truth)
		findMatches = document.FindMatches(doc, findInput, renderer.ContentWidth())

		// Clamp findCurrentIdx if out of bounds
		if len(findMatches) == 0 {
			findCurrentIdx = 0
		} else if findCurrentIdx >= len(findMatches) {
			findCurrentIdx = len(findMatches) - 1
		}

		// Set find highlighting and render
		renderer.SetFindQuery(findInput, findCurrentIdx)
		renderer.SetFocusMode(focusModeActive, focusParagraphStart, focusParagraphEnd)
		renderer.Render(doc, scrollY)

		// Apply focus mode dimming if active
		renderer.ApplyFocusDimming()

		// Draw subtle status bar at bottom with current domain
		domain := getDomain(url)
		statusY := height - 1

		// Build status line: [color accent] domain on left, [W] indicator, scroll % on right
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

		// Track where to start drawing (after optional theme color accent)
		statusX := 0

		// Draw theme color accent if available
		if doc.ThemeColor != "" {
			if r, g, b, ok := html.ParseHexColor(doc.ThemeColor); ok {
				canvas.Set(statusX, statusY, '█', render.Style{
					FgRGB:    [3]uint8{r, g, b},
					UseFgRGB: true,
				})
				statusX += 2 // Leave a space after the accent
			}
		}

		// Draw as dim text - subtle but visible
		if cfg.Display.ShowUrl {
			canvas.WriteString(statusX, statusY, domainDisplay, render.Style{Dim: true})
			statusX += len(domainDisplay)
		}

		// Show wide mode indicator
		if wideMode {
			wideIndicator := "[W]"
			if statusX > 0 {
				statusX++ // Add space before [W]
			}
			canvas.WriteString(statusX, statusY, wideIndicator, render.Style{Dim: true})
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
			renderer.RenderLinkLabels(labels, jumpInput)
		}
		if inputMode {
			labels = document.GenerateLabels(len(renderer.Inputs()))
			renderer.RenderInputLabels(labels, jumpInput)
		}
		if defineMode {
			// Group words by text, filter by prefix
			defineWordPositions = make(map[string][]render.Word)
			for _, w := range defineAllWords {
				if strings.HasPrefix(w.Text, defineFilter) {
					defineWordPositions[w.Text] = append(defineWordPositions[w.Text], w)
				}
			}
			// Get unique words sorted for consistent labeling
			defineUniqueWords = make([]string, 0, len(defineWordPositions))
			for word := range defineWordPositions {
				defineUniqueWords = append(defineUniqueWords, word)
			}

			// Show labels on ALL occurrences of each unique word
			if len(defineFilter) >= 1 && len(defineUniqueWords) > 0 {
				labels = document.GenerateLabels(len(defineUniqueWords))
				for i, word := range defineUniqueWords {
					if i >= len(labels) {
						break
					}
					label := labels[i]
					// Put same label on ALL occurrences of this word
					for _, pos := range defineWordPositions[word] {
						for j, ch := range label {
							if pos.X+j < canvas.Width() {
								style := theme.Current.Label.Style()
								style.Bold = true
								style.Reverse = true
								canvas.Set(pos.X+j, pos.Y, ch, style)
							}
						}
					}
				}
			}

			// Show filter prompt at bottom
			promptY := height - 1
			promptStyle := render.Style{Reverse: true}
			var prompt string
			if len(defineFilter) == 0 {
				prompt = " Define: type word... "
			} else if len(defineUniqueWords) == 0 {
				prompt = fmt.Sprintf(" Define: %s (no matches) ", defineFilter)
			} else if len(defineUniqueWords) == 1 {
				prompt = fmt.Sprintf(" Define: %s → %s (Enter) ", defineFilter, defineUniqueWords[0])
			} else {
				prompt = fmt.Sprintf(" Define: %s (%d words) ", defineFilter, len(defineUniqueWords))
			}
			canvas.WriteString(0, promptY, prompt, promptStyle)
			for x := len(prompt); x < width; x++ {
				canvas.Set(x, promptY, ' ', promptStyle)
			}
		}
		if tocMode {
			canvas.DimAll()
			labels = document.GenerateLabels(len(renderer.Headings()))
			renderer.RenderTOC(labels, jumpInput)
		}
		if navMode && len(doc.Navigation) > 0 {
			canvas.DimAll()
			// Generate labels only for visible items (nav box height - 4 for borders/title/footer)
			visibleCount := height - 10
			if visibleCount < 10 {
				visibleCount = 10
			}
			labels = document.GenerateLabels(visibleCount)
			navLinks = renderer.RenderNavigation(doc.Navigation, labels, navScrollOffset, jumpInput)
		}
		if linkIndexMode {
			canvas.DimAll()
			// Generate labels only for visible items
			visibleCount := len(renderer.Links())
			if visibleCount > height-8 {
				visibleCount = height - 8
			}
			labels = document.GenerateLabels(visibleCount)
			renderer.RenderLinkIndex(labels, linkScrollOffset, jumpInput)
		}
		if bufferMode {
			canvas.DimAll()

			// Draw buffer list as centered overlay box
			boxWidth := 70
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			boxHeight := len(buffers) + 4
			if boxHeight > height-4 {
				boxHeight = height - 4
			}
			visibleCount := boxHeight - 4

			// Generate labels only for visible items
			labels = document.GenerateLabels(visibleCount)

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
			for i := bufferScrollOffset; i < len(buffers) && i < bufferScrollOffset+visibleCount; i++ {
				labelIdx := i - bufferScrollOffset
				if labelIdx >= len(labels) {
					break
				}

				buf := buffers[i]
				bx := startX + 2

				// Format: [label] [*] title - url
				label := labels[labelIdx]
				isCurrent := i == currentBufferIdx

				// Extract title from URL or use URL
				displayTitle := buf.current.url
				if len(displayTitle) > 40 {
					displayTitle = displayTitle[:37] + "..."
				}

				// Check if this label matches the current input prefix
				matches := strings.HasPrefix(label, jumpInput)

				// Draw label with typed portion highlighted
				for j, ch := range label {
					var style render.Style
					if !matches && jumpInput != "" {
						style = theme.Current.LabelDim.Style()
					} else if j < len(jumpInput) {
						style = theme.Current.LabelTyped.Style()
						style.Bold = true
					} else {
						style = theme.Current.Label.Style()
						style.Reverse = true
						style.Bold = true
					}
					canvas.Set(bx+j, by, ch, style)
				}

				// Current buffer marker
				marker := "  "
				if isCurrent {
					marker = " *"
				}
				textStyle := render.Style{Bold: isCurrent}
				if !matches && jumpInput != "" {
					textStyle.Dim = true
				}
				canvas.WriteString(bx+len(label), by, marker, textStyle)

				// URL
				canvas.WriteString(bx+len(label)+2, by, displayTitle, textStyle)

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
			visibleCount := boxHeight - 4

			// Generate labels only for visible items
			labels = document.GenerateLabels(visibleCount)

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
				for i := favouritesScrollOffset; i < favStore.Len() && i < favouritesScrollOffset+visibleCount; i++ {
					labelIdx := i - favouritesScrollOffset
					if labelIdx >= len(labels) {
						break
					}

					fav := favStore.Favourites[i]
					bx := startX + 2

					// Format: [label] title (truncated URL)
					label := labels[labelIdx]

					// Check if this label matches the current input prefix
					matches := strings.HasPrefix(label, jumpInput)

					// Draw label with typed portion highlighted
					for j, ch := range label {
						var style render.Style
						if favouritesDeleteMode {
							// Delete mode - show in error color
							if j < len(jumpInput) {
								style = theme.Current.Error.Style()
								style.Bold = true
							} else {
								style = theme.Current.Error.Style()
								style.Bold = true
								style.Dim = true
							}
						} else if !matches && jumpInput != "" {
							style = theme.Current.LabelDim.Style()
						} else if j < len(jumpInput) {
							style = theme.Current.LabelTyped.Style()
							style.Bold = true
						} else {
							style = theme.Current.Label.Style()
							style.Reverse = true
							style.Bold = true
						}
						canvas.Set(bx+j, by, ch, style)
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
					textStyle := render.Style{}
					if !matches && jumpInput != "" {
						textStyle.Dim = true
					}
					canvas.WriteString(bx+len(label)+2, by, displayTitle, textStyle)

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
			hint := " label=open  x=delete mode  ESC=cancel "
			if favouritesDeleteMode {
				hint = " label=DELETE  ESC=cancel "
			}
			hintX := startX + (boxWidth-len(hint))/2
			canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
		}
		if themePickerMode {
			canvas.DimAll()

			// Draw theme picker as centered overlay box
			boxWidth := 50
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			themeCount := len(theme.All)
			boxHeight := themeCount + 4
			if boxHeight > height-4 {
				boxHeight = height - 4
			}
			visibleCount := boxHeight - 4

			// Generate labels for visible themes
			labels = document.GenerateLabels(visibleCount)

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
			title := " Select Theme "
			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, render.Style{Bold: true})

			// Draw themes with labels
			by := startY + 2
			for i := 0; i < themeCount && i < visibleCount; i++ {
				if i >= len(labels) {
					break
				}
				t := theme.All[i]
				bx := startX + 2

				// Label
				label := labels[i]
				matches := strings.HasPrefix(label, jumpInput)

				// Draw label with typed portion highlighted
				for j, ch := range label {
					var style render.Style
					if !matches && jumpInput != "" {
						style = theme.Current.LabelDim.Style()
					} else if j < len(jumpInput) {
						style = theme.Current.LabelTyped.Style()
						style.Bold = true
					} else {
						style = theme.Current.Label.Style()
						style.Reverse = true
						style.Bold = true
					}
					canvas.Set(bx+j, by, ch, style)
				}

				// Theme name
				displayName := t.Name
				if t == theme.Current {
					displayName += " ●" // Indicate current theme
				}
				maxLen := boxWidth - len(label) - 6
				if len(displayName) > maxLen {
					displayName = displayName[:maxLen-3] + "..."
				}
				textStyle := render.Style{}
				if !matches && jumpInput != "" {
					textStyle.Dim = true
				}
				if t == theme.Current {
					textStyle.Bold = true
				}
				canvas.WriteString(bx+len(label)+2, by, displayName, textStyle)

				// Show light/dark indicator
				indicator := "◐"
				if t.Dark {
					indicator = "◑"
				}
				canvas.WriteString(startX+boxWidth-4, by, indicator, render.Style{Dim: true})

				by++
			}

			// Footer hint
			hint := " label=preview  z=toggle variant  ESC=close "
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
		if findMode {
			// Draw find bar at bottom of screen
			findBarY := height - 1
			canvas.DrawHLine(0, findBarY, width, ' ', render.Style{Reverse: true})
			prompt := "/" + findInput + "█"
			canvas.WriteString(0, findBarY, prompt, render.Style{Reverse: true})

			// Show match count
			if len(findMatches) > 0 {
				matchInfo := fmt.Sprintf(" [%d/%d] ", findCurrentIdx+1, len(findMatches))
				canvas.WriteString(len(prompt)+1, findBarY, matchInfo, render.Style{Reverse: true, Bold: true})
			} else if findInput != "" {
				canvas.WriteString(len(prompt)+1, findBarY, " [no matches] ", render.Style{Reverse: true, Dim: true})
			}
		}
		if omniMode {
			canvas.DimAll()
			// Draw omnibox as centered overlay
			boxWidth := 70
			if boxWidth > width-4 {
				boxWidth = width - 4
			}
			boxHeight := 7
			startX := (width - boxWidth) / 2
			startY := (height - boxHeight) / 2

			// Clear box area
			for y := startY; y < startY+boxHeight; y++ {
				for x := startX; x < startX+boxWidth; x++ {
					canvas.Set(x, y, ' ', render.Style{})
				}
			}

			// Check if input starts with a known prefix
			title := " Omnibox "
			var matchedPrefix string
			var matchedDisplay string
			inputLower := strings.ToLower(omniInput)
			for _, pfx := range omniParser.Prefixes() {
				for _, name := range pfx.Names {
					// Check for "prefix " or just "prefix" at start
					if strings.HasPrefix(inputLower, name+" ") || inputLower == name {
						matchedPrefix = name
						matchedDisplay = pfx.Display
						title = " " + pfx.Display + " "
						break
					}
				}
				if matchedPrefix != "" {
					break
				}
			}

			// Draw border - accent when prefix matched
			boxStyle := render.Style{}
			titleStyle := render.Style{Bold: true}
			if matchedPrefix != "" {
				boxStyle = theme.Current.Success.Style()
				titleStyle = theme.Current.Success.Style()
				titleStyle.Bold = true
			}
			canvas.DrawBox(startX, startY, boxWidth, boxHeight, render.DoubleBox, boxStyle)

			titleX := startX + (boxWidth-len(title))/2
			canvas.WriteString(titleX, startY, title, titleStyle)

			// Input field with prefix highlighting
			inputY := startY + 2
			maxInputWidth := boxWidth - 4
			displayInput := omniInput
			if len(displayInput) > maxInputWidth-1 {
				displayInput = displayInput[len(displayInput)-maxInputWidth+1:]
			}

			if matchedPrefix != "" && len(omniInput) >= len(matchedPrefix) {
				// Draw prefix in success color/bold, rest in normal style
				prefixLen := len(matchedPrefix)
				prefixStyle := theme.Current.Success.Style()
				prefixStyle.Bold = true
				canvas.WriteString(startX+2, inputY, omniInput[:prefixLen], prefixStyle)
				canvas.WriteString(startX+2+prefixLen, inputY, omniInput[prefixLen:]+"█", render.Style{})
			} else {
				canvas.WriteString(startX+2, inputY, displayInput+"█", render.Style{})
			}

			// Prefix hints - dim the matched one
			prefixHint := "help  rss  ai  gh  hn  go  arch  mdn  man  dict  wp"
			if matchedDisplay != "" {
				prefixHint = "Searching " + matchedDisplay
			}
			if len(prefixHint) > boxWidth-4 {
				prefixHint = prefixHint[:boxWidth-7] + "..."
			}
			canvas.WriteString(startX+2, inputY+2, prefixHint, render.Style{Dim: true})

			// Hint
			hint := " URL, prefix:query, or search term "
			if matchedDisplay != "" {
				hint = " Enter search query "
			}
			hintX := startX + (boxWidth-len(hint))/2
			canvas.WriteString(hintX, startY+boxHeight-1, hint, render.Style{Dim: true})
		}

		renderToScreen()
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

						// Check if this is an image link - preview with Quick Look gallery
						// Use IsImage flag (from <img> tags) OR URL pattern detection
						if links[i].IsImage || isImageURL(newURL) {
							// Find all image links on the page
							allImageURLs := []string{}
							startIndex := 0
							for _, link := range links {
								linkURL := resolveURL(url, link.Href)
								if link.IsImage || isImageURL(linkURL) {
									allImageURLs = append(allImageURLs, linkURL)
									if linkURL == newURL {
										startIndex = len(allImageURLs) - 1
									}
								}
							}
							go openImageGallery(allImageURLs, startIndex)
							redraw()
							break
						}

						// Check if this is an RSS URL
						if strings.HasPrefix(newURL, "rss://") {
							rssDoc, err := handleRSSURL(rssStore, newURL)
							if err == nil {
								navigateTo(newURL, rssDoc, "")
							}
							redraw()
							break
						}

						// Internal browse:// pages
						if strings.HasPrefix(newURL, "browse://") {
							browseDoc, browseHTML, err := handleBrowseURL(newURL, favStore)
							if err == nil && browseDoc != nil {
								navigateTo(newURL, browseDoc, browseHTML)
							}
							redraw()
							break
						}

						// Mark RSS item as read when navigating from RSS page to article
						if strings.HasPrefix(url, "rss://") && rssStore != nil {
							if rssStore.MarkReadByLink(newURL) {
								rssStore.Save()
							}
						}

						// Check if this is a same-page anchor link
						if isSamePageLink(url, newURL) {
							hash := extractHash(newURL)
							if hash != "" {
								if anchorY, found := document.FindAnchorY(doc, hash, renderer.ContentWidth()); found {
									scrollY = anchorY
									if scrollY > maxScroll {
										scrollY = maxScroll
									}
								}
							}
						} else {
							loading = true
							newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
							loading = false
							if err == nil {
								navigateTo(newURL, newDoc, htmlContent)
								// After navigating, check for hash and scroll to anchor
								if hash := extractHash(newURL); hash != "" {
									if anchorY, found := document.FindAnchorY(newDoc, hash, renderer.ContentWidth()); found {
										scrollY = anchorY
										if scrollY > maxScroll {
											scrollY = maxScroll
										}
										getCurrentBuffer().current.scrollY = scrollY
									}
								}
							}
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
					}
					redraw() // Always redraw to show input highlighting
				}
			}
			continue
		}

		// Define mode input handling
		if defineMode {
			switch {
			case buf[0] == 27: // Escape - cancel
				defineMode = false
				defineFilter = ""
				redraw()

			case buf[0] == 127 || buf[0] == 8: // Backspace - remove last char
				if len(defineFilter) > 0 {
					defineFilter = defineFilter[:len(defineFilter)-1]
				}
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - select first/only match
				if len(defineUniqueWords) > 0 {
					defineMode = false
					wordText := defineUniqueWords[0]
					defineFilter = ""
					loading = true
					redraw()
					dictClient := dict.NewClient()
					definitions, err := dictClient.Define(wordText)
					loading = false
					if err != nil {
						errorHTML := fmt.Sprintf(`<!DOCTYPE html><html><head><title>Dictionary Error</title></head><body><article><h1>Dictionary Lookup Failed</h1><p>Could not look up "%s": %s</p></article></body></html>`, wordText, err.Error())
						errorDoc, _ := html.ParseString(errorHTML)
						navigateTo("dict://"+wordText, errorDoc, errorHTML)
					} else {
						dictHTML := generateDictHTML(wordText, definitions)
						dictDoc, parseErr := html.ParseString(dictHTML)
						if parseErr == nil {
							navigateTo("dict://"+wordText, dictDoc, dictHTML)
						}
					}
					redraw()
				}

			case buf[0] >= 'a' && buf[0] <= 'z':
				ch := string(buf[0])
				newFilter := defineFilter + ch

				// Would extending filter still have matches?
				hasFilterMatches := false
				for _, w := range defineAllWords {
					if strings.HasPrefix(w.Text, newFilter) {
						hasFilterMatches = true
						break
					}
				}

				if hasFilterMatches {
					// Extend filter
					defineFilter = newFilter
					redraw()
				} else if len(defineFilter) >= 1 && len(defineUniqueWords) > 0 {
					// No filter matches - check if it's a label
					for i, label := range labels {
						if label == ch && i < len(defineUniqueWords) {
							defineMode = false
							wordText := defineUniqueWords[i]
							defineFilter = ""
							loading = true
							redraw()
							dictClient := dict.NewClient()
							definitions, err := dictClient.Define(wordText)
							loading = false
							if err != nil {
								errorHTML := fmt.Sprintf(`<!DOCTYPE html><html><head><title>Dictionary Error</title></head><body><article><h1>Dictionary Lookup Failed</h1><p>Could not look up "%s": %s</p></article></body></html>`, wordText, err.Error())
								errorDoc, _ := html.ParseString(errorHTML)
								navigateTo("dict://"+wordText, errorDoc, errorHTML)
							} else {
								dictHTML := generateDictHTML(wordText, definitions)
								dictDoc, parseErr := html.ParseString(dictHTML)
								if parseErr == nil {
									navigateTo("dict://"+wordText, dictDoc, dictHTML)
								}
							}
							redraw()
							break
						}
					}
				}
			}
			continue
		}

		// AI Chat input handling (when on ai:// page with active chat)
		if strings.HasPrefix(url, "ai://") && getCurrentBuffer().current.chat != nil {
			chat := getCurrentBuffer().current.chat

			// Helper to update the chat display
			updateChatDisplay := func() {
				chatHTML := chatToHTML(chat, chatEditor)
				if chatDoc, err := html.ParseString(chatHTML); err == nil {
					getCurrentBuffer().current.doc = chatDoc
					getCurrentBuffer().current.html = chatHTML
					doc = chatDoc
					currentHTML = chatHTML
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
					scrollY = maxScroll
				}
				redraw()
			}

			// Let the keybinding scheme handle the input
			event := chatScheme.HandleKey(chatEditor, buf[:n], n)

			// Update display for any consumed event (cursor moves, mode changes, text changes)
			if event.Consumed {
				updateChatDisplay()
			}

			if event.Cancel {
				chatEditor.Clear()
				histBuf := getCurrentBuffer()
				if len(histBuf.history) > 0 {
					histBuf.current.scrollY = scrollY
					histBuf.forward = append(histBuf.forward, histBuf.current)
					histBuf.current = histBuf.history[len(histBuf.history)-1]
					histBuf.history = histBuf.history[:len(histBuf.history)-1]
					if histBuf.current.doc == nil {
						histBuf.current.doc, _ = fetchAndParse(histBuf.current.url)
					}
					doc = histBuf.current.doc
					url = histBuf.current.url
					scrollY = histBuf.current.scrollY
					currentHTML = histBuf.current.html
					if doc != nil {
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
					}
				}
				redraw()
				continue
			}

			if event.Submit && chatEditor.Len() > 0 {
				userMessage := chatEditor.Text()
				chatEditor.Clear()

				// Add user message to chat
				chat.Messages = append(chat.Messages, chatMessage{Role: "user", Content: userMessage})

				// Add animated loading placeholder
				thinkingIdx := len(chat.Messages)
				waveFrames := []string{
					"▁▂▃▄▅▆▇█", "▂▃▄▅▆▇█▇", "▃▄▅▆▇█▇▆", "▄▅▆▇█▇▆▅",
					"▅▆▇█▇▆▅▄", "▆▇█▇▆▅▄▃", "▇█▇▆▅▄▃▂", "█▇▆▅▄▃▂▁",
					"▇▆▅▄▃▂▁▂", "▆▅▄▃▂▁▂▃", "▅▄▃▂▁▂▃▄", "▄▃▂▁▂▃▄▅",
					"▃▂▁▂▃▄▅▆", "▂▁▂▃▄▅▆▇", "▁▂▃▄▅▆▇█",
				}
				frameIdx := 0
				chat.Messages = append(chat.Messages, chatMessage{Role: "assistant", Content: fmt.Sprintf("<p>%s</p>", waveFrames[0])})

				updateLoading := func() {
					chatHTML := chatToHTML(chat, chatEditor)
					if chatDoc, err := html.ParseString(chatHTML); err == nil {
						getCurrentBuffer().current.doc = chatDoc
						getCurrentBuffer().current.html = chatHTML
						doc = chatDoc
						currentHTML = chatHTML
						contentHeight = renderer.ContentHeight(doc)
						maxScroll = contentHeight - height
						if maxScroll < 0 {
							maxScroll = 0
						}
						scrollY = maxScroll
					}
					redraw()
				}
				updateLoading()

				responseCh := make(chan string, 1)
				go func() {
					responseCh <- generateChatResponseInline(chat, userMessage)
				}()

				ticker := time.NewTicker(80 * time.Millisecond)
			animationLoop:
				for {
					select {
					case response := <-responseCh:
						ticker.Stop()
						chat.Messages[thinkingIdx] = chatMessage{Role: "assistant", Content: response}
						updateLoading()
						break animationLoop
					case <-ticker.C:
						frameIdx = (frameIdx + 1) % len(waveFrames)
						chat.Messages[thinkingIdx] = chatMessage{Role: "assistant", Content: fmt.Sprintf("<p>%s</p>", waveFrames[frameIdx])}
						updateLoading()
					}
				}
				continue
			}

			if event.Consumed {
				continue // Key was handled by scheme
			}
			// Key not consumed - fall through for navigation handling
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
					}
					redraw() // Always redraw to show input highlighting
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
					}
					redraw() // Always redraw to show input highlighting
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

				// Check for exact match - labels are for visible items only
				matched := false
				for i, label := range labels {
					actualIdx := navScrollOffset + i
					if label == jumpInput && actualIdx < len(navLinks) {
						// Found a match - navigate to the link!
						matched = true
						navMode = false
						jumpInput = ""
						navScrollOffset = 0

						newURL := resolveURL(url, navLinks[actualIdx].Href)
						// Check if this is an RSS URL
						if strings.HasPrefix(newURL, "rss://") {
							rssDoc, err := handleRSSURL(rssStore, newURL)
							if err == nil {
								navigateTo(newURL, rssDoc, "")
							}
							redraw()
							break
						}

						// Internal browse:// pages
						if strings.HasPrefix(newURL, "browse://") {
							browseDoc, browseHTML, err := handleBrowseURL(newURL, favStore)
							if err == nil && browseDoc != nil {
								navigateTo(newURL, browseDoc, browseHTML)
							}
							redraw()
							break
						}

						// Mark RSS item as read when navigating from RSS page to article
						if strings.HasPrefix(url, "rss://") && rssStore != nil {
							if rssStore.MarkReadByLink(newURL) {
								rssStore.Save()
							}
						}

						// Check if this is a same-page anchor link
						if isSamePageLink(url, newURL) {
							hash := extractHash(newURL)
							if hash != "" {
								if anchorY, found := document.FindAnchorY(doc, hash, renderer.ContentWidth()); found {
									scrollY = anchorY
									if scrollY > maxScroll {
										scrollY = maxScroll
									}
								}
							}
						} else {
							loading = true
							newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
							loading = false
							if err == nil {
								navigateTo(newURL, newDoc, htmlContent)
								// After navigating, check for hash and scroll to anchor
								if hash := extractHash(newURL); hash != "" {
									if anchorY, found := document.FindAnchorY(newDoc, hash, renderer.ContentWidth()); found {
										scrollY = anchorY
										if scrollY > maxScroll {
											scrollY = maxScroll
										}
										getCurrentBuffer().current.scrollY = scrollY
									}
								}
							}
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
					}
					redraw() // Always redraw to show input highlighting
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

				// Check for exact match - labels are for visible items only
				matched := false
				links := renderer.Links()
				for i, label := range labels {
					actualIdx := linkScrollOffset + i
					if label == jumpInput && actualIdx < len(links) {
						// Found a match - navigate to the link!
						matched = true
						linkIndexMode = false
						jumpInput = ""
						linkScrollOffset = 0

						newURL := resolveURL(url, links[actualIdx].Href)
						// Check if this is an RSS URL
						if strings.HasPrefix(newURL, "rss://") {
							rssDoc, err := handleRSSURL(rssStore, newURL)
							if err == nil {
								navigateTo(newURL, rssDoc, "")
							}
							redraw()
							break
						}

						// Internal browse:// pages
						if strings.HasPrefix(newURL, "browse://") {
							browseDoc, browseHTML, err := handleBrowseURL(newURL, favStore)
							if err == nil && browseDoc != nil {
								navigateTo(newURL, browseDoc, browseHTML)
							}
							redraw()
							break
						}

						// Mark RSS item as read when navigating from RSS page to article
						if strings.HasPrefix(url, "rss://") && rssStore != nil {
							if rssStore.MarkReadByLink(newURL) {
								rssStore.Save()
							}
						}

						// Check if this is a same-page anchor link
						if isSamePageLink(url, newURL) {
							hash := extractHash(newURL)
							if hash != "" {
								if anchorY, found := document.FindAnchorY(doc, hash, renderer.ContentWidth()); found {
									scrollY = anchorY
									if scrollY > maxScroll {
										scrollY = maxScroll
									}
								}
							}
						} else {
							loading = true
							newDoc, htmlContent, err := fetchWithSpinner(canvas, newURL, ruleCache)
							loading = false
							if err == nil {
								navigateTo(newURL, newDoc, htmlContent)
								// After navigating, check for hash and scroll to anchor
								if hash := extractHash(newURL); hash != "" {
									if anchorY, found := document.FindAnchorY(newDoc, hash, renderer.ContentWidth()); found {
										scrollY = anchorY
										if scrollY > maxScroll {
											scrollY = maxScroll
										}
										getCurrentBuffer().current.scrollY = scrollY
									}
								}
							}
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
					}
					redraw() // Always redraw to show input highlighting
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

				// Check for exact match - labels are for visible items only
				matched := false
				for i, label := range labels {
					actualIdx := bufferScrollOffset + i
					if label == jumpInput && actualIdx < len(buffers) {
						// Found a match - switch to buffer!
						matched = true
						bufferMode = false
						jumpInput = ""
						bufferScrollOffset = 0

						// Save current buffer's scroll position
						buffers[currentBufferIdx].current.scrollY = scrollY

						// Switch to selected buffer
						currentBufferIdx = actualIdx
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
					}
					redraw() // Always redraw to show input highlighting
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

			case buf[0] == 'x' && !favouritesDeleteMode: // Enter delete mode (x like buffer close)
				favouritesDeleteMode = true
				jumpInput = ""
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'j' && buf[0] != 'k' && buf[0] != 'x':
				jumpInput += string(buf[0])

				// Check for exact match - labels are for visible items only
				matched := false
				for i, label := range labels {
					actualIdx := favouritesScrollOffset + i
					if label == jumpInput && actualIdx < favStore.Len() {
						matched = true
						if favouritesDeleteMode {
							// Delete this favourite
							favStore.Remove(actualIdx)
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
							fav := favStore.Favourites[actualIdx]
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
								// Handle hash/anchor in URL
								if hash := extractHash(fav.URL); hash != "" {
									if anchorY, found := document.FindAnchorY(newDoc, hash, renderer.ContentWidth()); found {
										scrollY = anchorY
										if scrollY > maxScroll {
											scrollY = maxScroll
										}
										getCurrentBuffer().current.scrollY = scrollY
									}
								}
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
					}
					redraw() // Always redraw to show input highlighting
				}
			}
			continue
		}

		// Theme picker mode input handling
		if themePickerMode {
			switch {
			case buf[0] == 27: // Escape - cancel theme picker
				themePickerMode = false
				jumpInput = ""
				redraw()

			case buf[0] == 'z': // Toggle light/dark variant
				theme.Toggle()
				redraw()

			case buf[0] >= 'a' && buf[0] <= 'z' && buf[0] != 'z':
				jumpInput += string(buf[0])

				// Check for exact match
				matched := false
				for i, label := range labels {
					if label == jumpInput && i < len(theme.All) {
						matched = true
						theme.Current = theme.All[i]
						// Stay open for easy theme trialling - ESC to close
						jumpInput = ""
						redraw()
						break
					}
				}

				// If no exact match, check if input could still match something
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
					}
					redraw()
				}
			}
			continue
		}

		// Find in page mode input handling
		if findMode {
			switch {
			case buf[0] == 27: // Escape - cancel find mode
				findMode = false
				findInput = ""
				findMatches = nil
				findCurrentIdx = 0
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - close input, keep highlights
				findMode = false
				// Don't clear findInput - keep it for n/N navigation
				redraw()

			case buf[0] == 127 || buf[0] == 8: // Backspace
				if len(findInput) > 0 {
					findInput = findInput[:len(findInput)-1]
					findCurrentIdx = 0 // Reset to first match when query changes
					redraw()           // Matches updated via renderer
				}

			case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
				findInput += string(buf[0])
				findCurrentIdx = 0 // Reset to first match when query changes
				redraw()           // First render to get match positions

				// Auto-scroll to first match (centered in viewport)
				if len(findMatches) > 0 {
					newScrollY := findMatches[findCurrentIdx].Y - height/2
					if newScrollY < 0 {
						newScrollY = 0
					}
					if newScrollY > maxScroll {
						newScrollY = maxScroll
					}
					if newScrollY != scrollY {
						scrollY = newScrollY
						redraw() // Re-render at new scroll position
					}
				}
			}
			continue
		}

		// Omnibox mode input handling
		if omniMode {
			switch {
			case buf[0] == 27: // Escape - cancel omnibox mode
				omniMode = false
				omniInput = ""
				redraw()

			case buf[0] == 13 || buf[0] == 10: // Enter - process omnibox input
				if omniInput != "" {
					omniMode = false
					input := omniInput
					omniInput = ""

					// Parse the input: URL, prefix:query, or plain search
					result := omniParser.Parse(input)

					// AI Summary request
					if result.IsAISummary {
						// Get page content for summary
						fullHeight := contentHeight + 10
						if fullHeight < height {
							fullHeight = height
						}
						summaryCanvas := render.NewCanvas(width, fullHeight)
						summaryRenderer := document.NewRendererWide(summaryCanvas, wideMode)
						summaryRenderer.Render(doc, 0)
						pageContent := summaryCanvas.PlainText()
						sourceURL := url // capture before navigation

						// Generate summary with LLM
						prompt := result.AIPrompt
						if prompt == "" {
							prompt = "Summarize this page. What is it about, what are the key points, and what's the takeaway?"
						}
						summary, sessionID := generateAISummary(llmClient, canvas, pageContent, prompt)
						if summary != "" {
							// Create chat session with summary as first assistant message
							chat := &chatSession{
								SourceURL:     sourceURL,
								SourceContent: pageContent,
								Messages: []chatMessage{
									{Role: "assistant", Content: summary},
								},
								SessionID: sessionID,
							}
							// Create HTML page from conversation and navigate to it
							chatHTML := chatToHTML(chat, chatEditor)
							chatDoc, err := html.ParseString(chatHTML)
							if err == nil {
								navigateTo("ai://chat", chatDoc, chatHTML)
								getCurrentBuffer().current.chat = chat
								chatEditor.Clear()
							}
						}
						redraw()
						continue
					}

					// Dictionary lookup request
					if result.IsDictLookup {
						loading = true
						redraw()
						dictClient := dict.NewClient()
						definitions, err := dictClient.Define(result.DictWord)
						loading = false
						if err != nil {
							// Show error page
							errorHTML := fmt.Sprintf(`<!DOCTYPE html><html><head><title>Dictionary Error</title></head><body><article><h1>Dictionary Lookup Failed</h1><p>Could not look up "%s": %s</p></article></body></html>`, result.DictWord, err.Error())
							errorDoc, _ := html.ParseString(errorHTML)
							navigateTo("dict://"+result.DictWord, errorDoc, errorHTML)
						} else {
							// Generate dictionary page
							dictHTML := generateDictHTML(result.DictWord, definitions)
							dictDoc, parseErr := html.ParseString(dictHTML)
							if parseErr == nil {
								navigateTo("dict://"+result.DictWord, dictDoc, dictHTML)
							}
						}
						redraw()
						continue
					}

					if result.UseInternal {
						// Use internal search provider
						provider := search.ProviderByName(result.Provider)
						loading = true
						results, err := searchWithSpinner(canvas, provider, result.Query)
						loading = false
						if err == nil && results != nil {
							htmlContent := results.ToHTML()
							newDoc, parseErr := html.ParseString(htmlContent)
							if parseErr == nil {
								navigateTo(result.Provider+"://"+result.Query, newDoc, htmlContent)
							}
						}
					} else if result.URL != "" {
						// Direct URL or prefixed search URL
						switch {
						case strings.HasPrefix(result.URL, "browse://"):
							browseDoc, browseHTML, err := handleBrowseURL(result.URL, favStore)
							if err == nil && browseDoc != nil {
								navigateTo(result.URL, browseDoc, browseHTML)
							}
						case strings.HasPrefix(result.URL, "rss://"):
							rssDoc, err := handleRSSURL(rssStore, result.URL)
							if err == nil && rssDoc != nil {
								navigateTo(result.URL, rssDoc, "")
							}
						default:
							loading = true
							newDoc, htmlContent, err := fetchWithSpinner(canvas, result.URL, ruleCache)
							loading = false
							if err == nil {
								navigateTo(result.URL, newDoc, htmlContent)
								// Handle hash/anchor in URL
								if hash := extractHash(result.URL); hash != "" {
									if anchorY, found := document.FindAnchorY(newDoc, hash, renderer.ContentWidth()); found {
										scrollY = anchorY
										if scrollY > maxScroll {
											scrollY = maxScroll
										}
										getCurrentBuffer().current.scrollY = scrollY
									}
								}
							}
						}
					}
					redraw()
				}

			case buf[0] == 127 || buf[0] == 8: // Backspace
				if len(omniInput) > 0 {
					omniInput = omniInput[:len(omniInput)-1]
					redraw()
				}

			case buf[0] >= 32 && buf[0] < 127: // Printable ASCII
				omniInput += string(buf[0])
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
			// Save session before quitting
			if cfg.Session.RestoreSession {
				// Update current buffer's scroll position
				buffers[currentBufferIdx].current.scrollY = scrollY

				// Build session state with full history
				var sessionBuffers []session.Buffer
				for _, b := range buffers {
					sb := session.Buffer{
						Current: session.PageState{
							URL:     b.current.url,
							ScrollY: b.current.scrollY,
						},
					}
					// Save back history
					for _, h := range b.history {
						sb.History = append(sb.History, session.PageState{
							URL:     h.url,
							ScrollY: h.scrollY,
						})
					}
					// Save forward history
					for _, f := range b.forward {
						sb.Forward = append(sb.Forward, session.PageState{
							URL:     f.url,
							ScrollY: f.scrollY,
						})
					}
					sessionBuffers = append(sessionBuffers, sb)
				}
				sess := &session.Session{
					Buffers:          sessionBuffers,
					CurrentBufferIdx: currentBufferIdx,
				}
				session.Save(sess) // Ignore errors on save
			}
			return nil

		case key(buf[0], kb.FollowLink): // follow link - enter jump mode
			jumpMode = true
			jumpInput = ""
			redraw()

		case key(buf[0], kb.DefineWord): // define word - enter define mode
			defineMode = true
			defineFilter = ""
			defineAllWords = canvas.ExtractWords(3)
			defineUniqueWords = nil
			defineWordPositions = nil
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
				focusModeActive = false // Reset focus mode on buffer switch
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case keyG(buf[0], kb.Refresh): // gr - refresh current page
			gPending = false
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				go func(targetURL string) {
					loading = true
					redraw()
					newDoc, err := fetchAndParseQuiet(targetURL)
					loading = false
					if err != nil {
						redraw() // Just redraw, user can try again
						return
					}
					// Update current buffer with refreshed content
					doc = newDoc
					buffers[currentBufferIdx].current.doc = newDoc
					focusModeActive = false
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
					redraw()
				}(url)
			}

		case keyG(buf[0], kb.RSSRefresh): // gf - refresh all RSS feeds
			gPending = false
			if rssPoller != nil {
				canvas.Clear()
				canvas.WriteString(width/2-12, height/2, "Refreshing feeds...", render.Style{Bold: true})
				renderToScreen()
				rssPoller.RefreshNow()
				// Brief confirmation (actual refresh happens in background)
				time.Sleep(300 * time.Millisecond)
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

		case buf[0] == 'n' && findInput != "": // find next match (takes priority over SiteNavigation when find active)
			if len(findMatches) > 0 {
				findCurrentIdx = (findCurrentIdx + 1) % len(findMatches)
				// Center match in viewport
				scrollY = findMatches[findCurrentIdx].Y - height/2
				if scrollY < 0 {
					scrollY = 0
				}
				if scrollY > maxScroll {
					scrollY = maxScroll
				}
				redraw()
			}

		case buf[0] == 'N' && findInput != "": // find previous match
			if len(findMatches) > 0 {
				findCurrentIdx--
				if findCurrentIdx < 0 {
					findCurrentIdx = len(findMatches) - 1
				}
				// Center match in viewport
				scrollY = findMatches[findCurrentIdx].Y - height/2
				if scrollY < 0 {
					scrollY = 0
				}
				if scrollY > maxScroll {
					scrollY = maxScroll
				}
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
				// Fetch page if doc is nil (restored from session)
				if buf.current.doc == nil {
					switch {
					case buf.current.url == "" || strings.HasPrefix(buf.current.url, "browse://"):
						buf.current.doc, buf.current.html, _ = handleBrowseURL(buf.current.url, favStore)
					case strings.HasPrefix(buf.current.url, "rss://"):
						buf.current.doc, _ = handleRSSURL(rssStore, buf.current.url)
					default:
						buf.current.doc, _ = fetchAndParse(buf.current.url)
					}
				}
				// Update local state
				doc = buf.current.doc
				url = buf.current.url
				scrollY = buf.current.scrollY
				currentHTML = buf.current.html
				focusModeActive = false // Reset focus mode on navigation
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
					renderToScreen()
					time.Sleep(300 * time.Millisecond)
				} else {
					// Already exists
					canvas.Clear()
					canvas.WriteString(width/2-12, height/2, "Already in favourites", render.Style{Dim: true})
					renderToScreen()
					time.Sleep(300 * time.Millisecond)
				}
				redraw()
			}

		case key(buf[0], kb.RSSFeeds): // F - open RSS feed list
			rssDoc, err := rssPage(rssStore)
			if err == nil {
				navigateTo("rss://", rssDoc, "")
				redraw()
			}

		case key(buf[0], kb.RSSSubscribe): // A - subscribe to current page's feed
			if currentHTML != "" && rssStore != nil {
				feeds := html.DiscoverFeeds(currentHTML)
				if len(feeds) == 0 {
					canvas.Clear()
					canvas.WriteString(width/2-10, height/2, "No RSS feed found", render.Style{Dim: true})
					renderToScreen()
					time.Sleep(500 * time.Millisecond)
					redraw()
				} else {
					// Use first feed found (could add picker for multiple)
					feed := feeds[0]
					feedURL := resolveURL(url, feed.URL)

					// Check if already subscribed
					alreadySubscribed := false
					for _, f := range rssStore.Feeds {
						if f.URL == feedURL {
							alreadySubscribed = true
							break
						}
					}

					if alreadySubscribed {
						canvas.Clear()
						canvas.WriteString(width/2-12, height/2, "Already subscribed", render.Style{Dim: true})
						renderToScreen()
						time.Sleep(500 * time.Millisecond)
						redraw()
					} else {
						// Subscribe and fetch
						canvas.Clear()
						canvas.WriteString(width/2-10, height/2, "Subscribing...", render.Style{Bold: true})
						renderToScreen()

						go func(feedURL, feedTitle string) {
							showError := func(msg string) {
								canvas.Clear()
								canvas.WriteString((width-len(msg))/2, height/2, msg, render.Style{Dim: true})
								renderToScreen()
								time.Sleep(800 * time.Millisecond)
								redraw()
							}

							// Add subscription
							rssStore.Subscribe(feedURL)

							// Fetch feed content with timeout
							client := &http.Client{Timeout: 15 * time.Second}
							resp, err := client.Get(feedURL)
							if err != nil {
								rssStore.SetFeedError(feedURL, err.Error())
								rssStore.Save()
								showError("Error fetching feed")
								return
							}
							defer resp.Body.Close()

							data, err := io.ReadAll(resp.Body)
							if err != nil {
								rssStore.SetFeedError(feedURL, err.Error())
								rssStore.Save()
								showError("Error reading feed")
								return
							}

							parsed, err := rss.Parse(data)
							if err != nil {
								rssStore.SetFeedError(feedURL, err.Error())
								rssStore.Save()
								showError("Error parsing feed")
								return
							}

							// Update feed metadata and items
							for i := range rssStore.Feeds {
								if rssStore.Feeds[i].URL == feedURL {
									if parsed.Title != "" {
										rssStore.Feeds[i].Title = parsed.Title
									} else if feedTitle != "" {
										rssStore.Feeds[i].Title = feedTitle
									}
									if parsed.Description != "" {
										rssStore.Feeds[i].Description = parsed.Description
									}
									if parsed.Link != "" {
										rssStore.Feeds[i].SiteURL = parsed.Link
									}
									break
								}
							}

							rssStore.UpdateFeed(feedURL, parsed, 100)
							rssStore.Save()

							// Show confirmation
							canvas.Clear()
							msg := fmt.Sprintf("Subscribed to %s (%d items)", parsed.Title, len(parsed.Items))
							if len(msg) > width-4 {
								msg = msg[:width-7] + "..."
							}
							canvas.WriteString((width-len(msg))/2, height/2, msg, render.Style{Bold: true})
							renderToScreen()
							time.Sleep(800 * time.Millisecond)
							redraw()
						}(feedURL, feed.Title)
					}
				}
			}

		case key(buf[0], kb.RSSUnsubscribe): // x - unsubscribe from feed
			if rssStore != nil && strings.HasPrefix(url, "rss://feed/") {
				// On a single feed page - remove this feed
				encoded := strings.TrimPrefix(url, "rss://feed/")
				decoded, err := neturl.PathUnescape(encoded)
				if err == nil && decoded != "" {
					if rssStore.Unsubscribe(decoded) {
						rssStore.Save()
						// Show confirmation and go back to feed list
						canvas.Clear()
						canvas.WriteString(width/2-8, height/2, "Unsubscribed!", render.Style{Bold: true})
						renderToScreen()
						time.Sleep(500 * time.Millisecond)
						// Navigate back to RSS feed list
						rssDoc, _ := handleRSSURL(rssStore, "rss://")
						navigateTo("rss://", rssDoc, "")
						redraw()
					}
				}
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

		case key(buf[0], kb.ToggleTheme): // toggle light/dark theme
			theme.Toggle()
			redraw()

		case key(buf[0], kb.ThemePicker): // open theme picker
			themePickerMode = true
			jumpInput = ""
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

		case key(buf[0], kb.AISummary): // AI summary
			// Get page content for summary
			fullHeight := contentHeight + 10
			if fullHeight < height {
				fullHeight = height
			}
			summaryCanvas := render.NewCanvas(width, fullHeight)
			summaryRenderer := document.NewRendererWide(summaryCanvas, wideMode)
			summaryRenderer.Render(doc, 0)
			pageContent := summaryCanvas.PlainText()
			sourceURL := url // capture before navigation

			// Generate summary with LLM
			summary, sessionID := generateAISummary(llmClient, canvas, pageContent, "Summarize this page. What is it about, what are the key points, and what's the takeaway?")
			if summary != "" {
				// Create chat session with summary as first assistant message
				chat := &chatSession{
					SourceURL:     sourceURL,
					SourceContent: pageContent,
					Messages: []chatMessage{
						{Role: "assistant", Content: summary},
					},
					SessionID: sessionID,
				}
				// Create HTML page from conversation and navigate to it
				chatHTML := chatToHTML(chat, chatEditor)
				chatDoc, err := html.ParseString(chatHTML)
				if err == nil {
					navigateTo("ai://chat", chatDoc, chatHTML)
					getCurrentBuffer().current.chat = chat
					chatEditor.Clear()
				}
			}
			redraw()

		case key(buf[0], kb.EditorSandbox): // Editor sandbox for testing keybinding schemes
			// Build sandbox page showing current scheme with toggle instructions
			sandboxEditor := lineedit.New()
			sandboxEditor.Set("Type here to test the editor...")
			sandboxEditor.End()

			// Track which scheme is active in sandbox
			sandboxScheme := chatScheme // Start with current global scheme

			sandboxHTML := func() string {
				schemeName := sandboxScheme.Name()
				modeInfo := ""
				vimNormalMode := false
				if vim, ok := sandboxScheme.(*lineedit.VimScheme); ok {
					if vim.InInsertMode() {
						modeInfo = " (INSERT)"
					} else {
						modeInfo = " (NORMAL)"
						vimNormalMode = true
					}
				}

				// Use shared cursor rendering
				var inputLine string
				if vimNormalMode {
					inputLine = sandboxEditor.RenderWithCursor("mark", true)
				} else {
					inputLine = sandboxEditor.RenderWithCursor("ins", false)
				}

				return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Editor Sandbox</title></head>
<body>
<article>
<h1>Editor Sandbox</h1>
<p>Test your keybinding scheme here.</p>
<hr>
<h2>Current Scheme: %s%s</h2>
<p>Press <strong>Tab</strong> to toggle between emacs/vim</p>
<p>Press <strong>Escape</strong> to exit sandbox</p>
<hr>
<h3>Input</h3>
<p><strong>&gt;</strong> %s</p>
<p>&nbsp;</p>
<hr>
<h3>Keybindings</h3>
<p><strong>Emacs:</strong> Ctrl+A/E (home/end), Ctrl+F/B (char), Alt+F/B (word), Ctrl+K/U (kill), Ctrl+W (del word)</p>
<p><strong>Vim Normal:</strong> h/l (char), w/b (word), 0/$ (home/end), x/X/D (delete), i/a/I/A (insert)</p>
<p><strong>Vim Insert:</strong> Type normally, Escape to return to normal mode</p>
</article>
</body>
</html>`, schemeName, modeInfo, inputLine)
			}

			// Navigate to sandbox
			sandboxDoc, _ := html.ParseString(sandboxHTML())
			navigateTo("editor://sandbox", sandboxDoc, sandboxHTML())
			redraw()

			// Sandbox input loop
			sandboxBuf := make([]byte, 3)
			for {
				sn, _ := os.Stdin.Read(sandboxBuf)
				if sn == 0 {
					continue
				}

				// Tab to toggle scheme
				if sandboxBuf[0] == '\t' {
					if sandboxScheme.Name() == "emacs" {
						sandboxScheme = lineedit.NewVimScheme()
					} else {
						sandboxScheme = lineedit.NewEmacsScheme()
					}
					// Update global scheme too
					chatScheme = sandboxScheme
				} else {
					// Let scheme handle the key
					event := sandboxScheme.HandleKey(sandboxEditor, sandboxBuf[:sn], sn)

					if event.Cancel {
						break // Exit sandbox
					}
				}

				// Update display
				newHTML := sandboxHTML()
				sandboxDoc, _ = html.ParseString(newHTML)
				getCurrentBuffer().current.doc = sandboxDoc
				getCurrentBuffer().current.html = newHTML
				doc = sandboxDoc
				currentHTML = newHTML
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

			// Restore previous page
			histBuf := getCurrentBuffer()
			if len(histBuf.history) > 0 {
				histBuf.current = histBuf.history[len(histBuf.history)-1]
				histBuf.history = histBuf.history[:len(histBuf.history)-1]
				doc = histBuf.current.doc
				url = histBuf.current.url
				scrollY = histBuf.current.scrollY
				currentHTML = histBuf.current.html
				if doc != nil {
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
				}
			}
			redraw()

		case key(buf[0], kb.Back): // back in history (within current buffer)
			buf := getCurrentBuffer()
			if len(buf.history) > 0 {
				// Save current to forward
				buf.current.scrollY = scrollY
				buf.forward = append(buf.forward, buf.current)
				// Pop from history
				buf.current = buf.history[len(buf.history)-1]
				buf.history = buf.history[:len(buf.history)-1]
				// Fetch page if doc is nil (restored from session)
				if buf.current.doc == nil {
					switch {
					case buf.current.url == "" || strings.HasPrefix(buf.current.url, "browse://"):
						buf.current.doc, buf.current.html, _ = handleBrowseURL(buf.current.url, favStore)
					case strings.HasPrefix(buf.current.url, "rss://"):
						buf.current.doc, _ = handleRSSURL(rssStore, buf.current.url)
					default:
						buf.current.doc, _ = fetchAndParse(buf.current.url)
					}
				}
				// Update local state
				doc = buf.current.doc
				url = buf.current.url
				scrollY = buf.current.scrollY
				currentHTML = buf.current.html
				focusModeActive = false // Reset focus mode on navigation
				if doc != nil {
					contentHeight = renderer.ContentHeight(doc)
					maxScroll = contentHeight - height
					if maxScroll < 0 {
						maxScroll = 0
					}
				}
				redraw()
			}

		case key(buf[0], kb.Help): // help
			gPending = false
			helpDoc, helpHTML, err := helpPage()
			if err == nil && helpDoc != nil {
				navigateTo("browse://help", helpDoc, helpHTML)
				redraw()
			}

		case key(buf[0], kb.Home): // home
			homeDoc, err := landingPage(favStore)
			if err == nil {
				navigateTo("browse://home", homeDoc, "")
				redraw()
			}

		case key(buf[0], kb.ReloadWithJs): // reload with browser (JS rendering)
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
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

		case key(buf[0], kb.TranslatePage): // Translate page to English
			if doc != nil && (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
				// Show translating message
				canvas.DimAll()
				msg := " Translating... "
				msgX := (width - len(msg)) / 2
				msgY := height / 2
				canvas.WriteString(msgX, msgY, msg, render.Style{Reverse: true, Bold: true})
				canvas.RenderToWithBase(os.Stdout, theme.Current.BaseStyle())

				// Get source language from document or use auto-detect
				sourceLang := doc.Lang
				if sourceLang == "" || sourceLang == "en" {
					sourceLang = "auto"
				}

				// Extract text for translation
				text := doc.PlainTextForTranslation()
				if text != "" {
					client := translate.NewClient()
					translated, err := client.Translate(text, sourceLang, "en")
					if err != nil {
						// Show error
						errorHTML := fmt.Sprintf(`<!DOCTYPE html><html><head><title>Translation Error</title></head><body><article><h1>Translation Failed</h1><p>%s</p></article></body></html>`, err.Error())
						errorDoc, _ := html.ParseString(errorHTML)
						if errorDoc != nil {
							doc = errorDoc
							doc.URL = url
						}
					} else {
						// Create translated document
						title := doc.Title
						if title != "" {
							title = title + " (Translated)"
						} else {
							title = "Translated Page"
						}
						translatedHTML := fmt.Sprintf(`<!DOCTYPE html><html lang="en"><head><title>%s</title></head><body><article><h1>%s</h1>%s</article></body></html>`,
							title, title, textToHTML(translated))
						translatedDoc, _ := html.ParseString(translatedHTML)
						if translatedDoc != nil {
							doc = translatedDoc
							doc.URL = url + " (translated)"
						}
					}
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
			if (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) && llmClient.Available() {
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
						renderToScreen()
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
							renderToScreen()
							time.Sleep(1 * time.Second)
						} else {
							if len(errMsg) > width-4 {
								errMsg = errMsg[:width-7] + "..."
							}
							canvas.WriteString(2, height/2, "Error: "+errMsg, render.Style{Bold: true})
							renderToScreen()
							time.Sleep(2 * time.Second)
						}
					}
				}
				redraw()
			}

		case key(buf[0], kb.Find): // find in page
			findMode = true
			findInput = ""
			findMatches = nil
			findCurrentIdx = 0
			redraw()

		case key(buf[0], kb.Omnibox) || (key(buf[0], kb.OpenUrl) && !gPending): // omnibox (unified URL + search)
			gPending = false
			omniMode = true
			omniInput = ""
			redraw()

		case key(buf[0], kb.CopyUrl): // yank (copy) URL to clipboard
			if err := copyToClipboard(url); err == nil {
				// Brief visual feedback - show "Copied!" in status area
				canvas.Clear()
				renderer.Render(doc, scrollY)
				statusY := height - 1
				msg := "URL copied to clipboard"
				canvas.WriteString(0, statusY, msg, render.Style{Bold: true})
				renderToScreen()
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
				renderToScreen()
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

				// Update keybindings
				kb = cfg.Keybindings

				// Show success briefly
				canvas.Clear()
				canvas.WriteString(width/2-10, height/2, "Config reloaded!", render.Style{Bold: true})
				renderToScreen()
				time.Sleep(300 * time.Millisecond)
			}
			redraw()

		case key(buf[0], kb.ScrollDown), buf[0] == 14: // j or Ctrl+N
			focusModeActive = false
			scrollY++
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case key(buf[0], kb.ScrollUp), buf[0] == 16: // k or Ctrl+P
			focusModeActive = false
			scrollY--
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case key(buf[0], kb.HalfPageDown), buf[0] == 4: // d or Ctrl+D
			focusModeActive = false
			scrollY += height / 2
			if scrollY > maxScroll {
				scrollY = maxScroll
			}
			redraw()

		case key(buf[0], kb.HalfPageUp), buf[0] == 21: // u or Ctrl+U
			focusModeActive = false
			scrollY -= height / 2
			if scrollY < 0 {
				scrollY = 0
			}
			redraw()

		case keyG(buf[0], kb.GoTop): // gg - go to top
			focusModeActive = false
			gPending = false
			scrollY = 0
			redraw()

		case keyG(buf[0], kb.OpenInBrowser): // go - open in default browser
			gPending = false
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				if err := openInBrowser(url); err == nil {
					// Brief feedback
					canvas.Clear()
					renderer.Render(doc, scrollY)
					statusY := height - 1
					canvas.WriteString(0, statusY, "Opened in browser", render.Style{Bold: true})
					renderToScreen()
					time.Sleep(500 * time.Millisecond)
				}
				redraw()
			}

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
				focusModeActive = false // Reset focus mode on buffer switch
				contentHeight = renderer.ContentHeight(doc)
				maxScroll = contentHeight - height
				if maxScroll < 0 {
					maxScroll = 0
				}
				redraw()
			}

		case key(buf[0], kb.GoBottom):
			gPending = false
			focusModeActive = false
			scrollY = maxScroll
			redraw()

		case key(buf[0], kb.PrevParagraph): // Previous paragraph
			paragraphs := renderer.Paragraphs()
			// When focus mode is active, find paragraph before the focused one
			searchFrom := scrollY
			if focusModeActive {
				searchFrom = focusParagraphStart
			}
			for i := len(paragraphs) - 1; i >= 0; i-- {
				if paragraphs[i] < searchFrom {
					// Calculate paragraph range
					paraStart := paragraphs[i]
					paraEnd := contentHeight
					if i+1 < len(paragraphs) {
						paraEnd = paragraphs[i+1]
					}
					paraHeight := paraEnd - paraStart

					// Center the paragraph vertically if focus mode enabled
					if cfg.Display.FocusMode {
						focusModeActive = true
						focusParagraphStart = paraStart
						focusParagraphEnd = paraEnd
						// Center: scroll so paragraph middle is at screen middle
						scrollY = paraStart - (height-paraHeight)/2
					} else {
						scrollY = paraStart
					}

					if scrollY < 0 {
						scrollY = 0
					}
					if scrollY > maxScroll {
						scrollY = maxScroll
					}
					break
				}
			}
			redraw()

		case key(buf[0], kb.NextParagraph): // Next paragraph
			paragraphs := renderer.Paragraphs()
			// When focus mode is active, find paragraph after the focused one
			searchFrom := scrollY
			if focusModeActive {
				searchFrom = focusParagraphStart
			}
			for i, p := range paragraphs {
				if p > searchFrom {
					// Calculate paragraph range
					paraStart := p
					paraEnd := contentHeight
					if i+1 < len(paragraphs) {
						paraEnd = paragraphs[i+1]
					}
					paraHeight := paraEnd - paraStart

					// Center the paragraph vertically if focus mode enabled
					if cfg.Display.FocusMode {
						focusModeActive = true
						focusParagraphStart = paraStart
						focusParagraphEnd = paraEnd
						// Center: scroll so paragraph middle is at screen middle
						scrollY = paraStart - (height-paraHeight)/2
					} else {
						scrollY = paraStart
					}

					if scrollY < 0 {
						scrollY = 0
					}
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

	body, err := io.ReadAll(resp.Body)
	done <- true
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	htmlContent := string(body)

	// Check for site-specific handlers (HN, etc.)
	if doc, _ := sites.ParseForURL(url, htmlContent); doc != nil {
		// Extract and set theme color for site-specific handlers
		if doc.ThemeColor == "" {
			doc.ThemeColor = html.ExtractThemeColorFromHTML(htmlContent)
		}
		return doc, nil
	}

	// Default HTML parser
	doc, err := html.ParseString(htmlContent)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

func fetchWithBrowser(targetURL string) (*html.Document, error) {
	// Start spinner in background (different animation for browser mode)
	done := make(chan bool)
	go showBrowserSpinner(done)

	result, err := fetcher.WithBrowser(targetURL)
	done <- true
	if err != nil {
		return nil, err
	}

	// Check for site-specific handlers (HN, etc.)
	if doc, _ := sites.ParseForURL(targetURL, result.HTML); doc != nil {
		// Extract and set theme color for site-specific handlers
		if doc.ThemeColor == "" {
			doc.ThemeColor = html.ExtractThemeColorFromHTML(result.HTML)
		}
		return doc, nil
	}

	// Default HTML parser
	doc, err := html.ParseString(result.HTML)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

// fetchAndParseQuiet fetches without spinner (for use when already in alt screen)
func fetchAndParseQuiet(targetURL string) (*html.Document, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	htmlContent := string(body)

	// Check for site-specific handlers (HN, etc.)
	if doc, _ := sites.ParseForURL(targetURL, htmlContent); doc != nil {
		// Extract and set theme color for site-specific handlers
		if doc.ThemeColor == "" {
			doc.ThemeColor = html.ExtractThemeColorFromHTML(htmlContent)
		}
		return doc, nil
	}

	// Default HTML parser
	doc, err := html.ParseString(htmlContent)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

// fetchWithBrowserQuiet uses headless Chrome without spinner
func fetchWithBrowserQuiet(url string) (*html.Document, error) {
	return fetchWithBrowserCtx(context.Background(), url)
}

// fetchWithBrowserCtx uses headless Chrome with context for cancellation.
func fetchWithBrowserCtx(ctx context.Context, url string) (*html.Document, error) {
	// Check if already cancelled
	if ctx.Err() != nil {
		return nil, ErrCancelled
	}

	// Run browser fetch in goroutine so we can check context
	type result struct {
		doc *html.Document
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		res, err := fetcher.WithBrowser(url)
		if err != nil {
			resultCh <- result{nil, err}
			return
		}

		// Check for site-specific handlers (HN, etc.)
		if doc, _ := sites.ParseForURL(url, res.HTML); doc != nil {
			if doc.ThemeColor == "" {
				doc.ThemeColor = html.ExtractThemeColorFromHTML(res.HTML)
			}
			resultCh <- result{doc, nil}
			return
		}

		doc, err := html.ParseString(res.HTML)
		if err != nil {
			resultCh <- result{nil, fmt.Errorf("parsing HTML: %w", err)}
			return
		}
		resultCh <- result{doc, nil}
	}()

	select {
	case <-ctx.Done():
		return nil, ErrCancelled
	case r := <-resultCh:
		return r.doc, r.err
	}
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

	htmlContent := string(body)

	// Check for site-specific handlers (HN, etc.)
	if doc, _ := sites.ParseForURL(targetURL, htmlContent); doc != nil {
		if doc.ThemeColor == "" {
			doc.ThemeColor = html.ExtractThemeColorFromHTML(htmlContent)
		}
		return doc, htmlContent, nil
	}

	doc, err := html.ParseString(htmlContent)
	if err != nil {
		return nil, "", fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, htmlContent, nil
}

// fetchWithRules fetches and parses, applying cached rules if available.
func fetchWithRules(targetURL string, cache *rules.Cache) (*html.Document, string, error) {
	return fetchWithRulesCtx(context.Background(), targetURL, cache)
}

// fetchWithRulesCtx fetches and parses with context for cancellation.
func fetchWithRulesCtx(ctx context.Context, targetURL string, cache *rules.Cache) (*html.Document, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", fetcher.UserAgent())

	client := &http.Client{Timeout: fetcher.Timeout()}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ErrCancelled
		}
		return nil, "", fmt.Errorf("fetching %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ErrCancelled
		}
		return nil, "", fmt.Errorf("reading body: %w", err)
	}

	htmlContent := string(body)

	// Detect bot protection pages (DataDome, etc.) and retry with browser
	if isBotProtectionPage(htmlContent) {
		// Try browser fetch to bypass protection
		result, browserErr := fetcher.WithBrowser(targetURL)
		if browserErr == nil && result != nil && !isBotProtectionPage(result.HTML) {
			htmlContent = result.HTML
		} else {
			// Browser fetch also failed - return a helpful message
			return createBotProtectionDoc(targetURL), htmlContent, nil
		}
	}

	// Check for site-specific handlers (HN, etc.) - they register via init()
	if doc, _ := sites.ParseForURL(targetURL, htmlContent); doc != nil {
		if doc.ThemeColor == "" {
			doc.ThemeColor = html.ExtractThemeColorFromHTML(htmlContent)
		}
		return doc, htmlContent, nil
	}

	// Default HTML parser
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
						// Set theme color from original HTML
						if rulesDoc.ThemeColor == "" {
							rulesDoc.ThemeColor = html.ExtractThemeColorFromHTML(htmlContent)
						}
						return rulesDoc, htmlContent, nil
					}
				}
			}
		}
	}

	return defaultDoc, htmlContent, nil
}

// createBotProtectionDoc creates a document explaining the bot protection issue.
func createBotProtectionDoc(targetURL string) *html.Document {
	// Extract domain for RSS suggestion
	u, _ := neturl.Parse(targetURL)
	domain := ""
	if u != nil {
		domain = u.Host
	}

	htmlContent := fmt.Sprintf(`<html><body>
<h1>Bot Protection Detected</h1>
<p>This site (%s) has aggressive bot protection that prevents automated access.</p>

<h2>Options</h2>
<ul>
<li><strong>Press 'o'</strong> to open in your default browser</li>
<li><strong>Subscribe via RSS</strong> - Press 'F' then 'A' on a page with RSS feeds</li>
<li><strong>Try again later</strong> - Sometimes protection passes after cookies are set</li>
</ul>

<h2>Why This Happens</h2>
<p>Many news sites use DataDome or similar services to block automated access.
These services use JavaScript fingerprinting that's difficult to bypass even with browser emulation.</p>
</body></html>`, domain)

	doc, _ := html.ParseString(htmlContent)
	return doc
}

// isBotProtectionPage detects bot protection/captcha pages that need browser rendering.
// Checks for DataDome, Cloudflare, and other common bot protection signatures.
func isBotProtectionPage(htmlContent string) bool {
	// Page must be small (protection pages are typically minimal)
	if len(htmlContent) > 50000 {
		return false
	}

	// DataDome signature
	if strings.Contains(htmlContent, "captcha-delivery.com") ||
		strings.Contains(htmlContent, "Please enable JS and disable any ad blocker") {
		return true
	}

	// Cloudflare challenge
	if strings.Contains(htmlContent, "cf-browser-verification") ||
		strings.Contains(htmlContent, "Checking your browser") {
		return true
	}

	// Generic bot detection patterns
	if strings.Contains(htmlContent, "bot-check") ||
		strings.Contains(htmlContent, "human-verification") {
		return true
	}

	return false
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
	// Handle hash-only links (same page anchors)
	if strings.HasPrefix(href, "#") {
		// Strip any existing hash from base and append the new one
		if idx := strings.Index(base, "#"); idx != -1 {
			return base[:idx] + href
		}
		return base + href
	}

	// Handle absolute URLs (including internal schemes)
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") ||
		strings.HasPrefix(href, "rss://") || strings.HasPrefix(href, "browse://") ||
		strings.HasPrefix(href, "dict://") || strings.HasPrefix(href, "hn://") {
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
	// Find the path portion (after scheme://host)
	idx := strings.Index(base, "://")
	if idx == -1 {
		return base + "/" + href
	}
	rest := base[idx+3:] // Everything after ://
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		// No path - just host (e.g., https://example.com)
		return base + "/" + href
	}
	// Find last slash in the path portion
	pathStart := idx + 3 + slashIdx
	lastSlash := strings.LastIndex(base[pathStart:], "/")
	if lastSlash == -1 {
		return base + "/" + href
	}
	return base[:pathStart+lastSlash+1] + href
}

// urlWithoutHash returns the URL with any hash/fragment removed.
func urlWithoutHash(u string) string {
	if idx := strings.Index(u, "#"); idx != -1 {
		return u[:idx]
	}
	return u
}

// extractHash returns the hash/fragment from a URL (without the # prefix).
func extractHash(u string) string {
	if idx := strings.Index(u, "#"); idx != -1 {
		return u[idx+1:]
	}
	return ""
}

// isSamePageLink checks if targetURL is the same page as currentURL (just different anchor).
func isSamePageLink(currentURL, targetURL string) bool {
	return urlWithoutHash(currentURL) == urlWithoutHash(targetURL)
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

// textToHTML converts plain text with paragraph breaks to HTML paragraphs.
func textToHTML(text string) string {
	var sb strings.Builder
	paragraphs := strings.Split(text, "\n\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Escape HTML entities
		p = strings.ReplaceAll(p, "&", "&amp;")
		p = strings.ReplaceAll(p, "<", "&lt;")
		p = strings.ReplaceAll(p, ">", "&gt;")
		// Convert single newlines to <br>
		p = strings.ReplaceAll(p, "\n", "<br>")
		sb.WriteString("<p>")
		sb.WriteString(p)
		sb.WriteString("</p>\n")
	}
	return sb.String()
}

// openInBrowser opens the URL in the default system browser.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("browser opening not supported on %s", runtime.GOOS)
	}
	return cmd.Start() // Don't wait for browser to close
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
<p><strong>?</strong> Help &amp; quickstart &mdash; <a href="browse://help">browse://help</a></p>
<p><strong>Ctrl+L</strong> Omnibox (URL, search, AI)</p>

<h2>Keybindings</h2>
<table>
<tr><th>Key</th><th>Action</th></tr>
<tr><td><strong>j/k</strong></td><td>Scroll down/up</td></tr>
<tr><td><strong>d/u</strong></td><td>Half page down/up</td></tr>
<tr><td><strong>gg/G</strong></td><td>Go to top/bottom</td></tr>
<tr><td><strong>[/]</strong></td><td>Prev/next paragraph (focus mode)</td></tr>
<tr><td><strong>{/}</strong></td><td>Prev/next section</td></tr>
<tr><td><strong>Ctrl+L</strong></td><td>Omnibox (URL, search, AI)</td></tr>
<tr><td><strong>/</strong></td><td>Find in page</td></tr>
<tr><td><strong>y</strong></td><td>Copy URL to clipboard</td></tr>
<tr><td><strong>f</strong></td><td>Follow link</td></tr>
<tr><td><strong>go</strong></td><td>Open in default browser</td></tr>
<tr><td><strong>E</strong></td><td>Edit page in $EDITOR</td></tr>
<tr><td><strong>t</strong></td><td>Table of contents</td></tr>
<tr><td><strong>n</strong></td><td>Site navigation</td></tr>
<tr><td><strong>l</strong></td><td>Link index</td></tr>
<tr><td><strong>s</strong></td><td>Structure inspector</td></tr>
<tr><td><strong>Ctrl+O/Tab</strong></td><td>Back/forward in history</td></tr>
<tr><td><strong>T</strong></td><td>New buffer</td></tr>
<tr><td><strong>gt/gT</strong></td><td>Next/prev buffer</td></tr>
<tr><td><strong>&#96;</strong></td><td>Buffer list</td></tr>
<tr><td><strong>M</strong></td><td>Add to favourites</td></tr>
<tr><td><strong>'</strong></td><td>Favourites list</td></tr>
<tr><td><strong>F</strong></td><td>RSS feeds</td></tr>
<tr><td><strong>H</strong></td><td>Home</td></tr>
<tr><td><strong>?</strong></td><td>Help &amp; quickstart</td></tr>
<tr><td><strong>w</strong></td><td>Toggle wide mode</td></tr>
<tr><td><strong>i</strong></td><td>Focus input field</td></tr>
<tr><td><strong>r</strong></td><td>Reload with JavaScript</td></tr>
<tr><td><strong>R</strong></td><td>Generate AI rules</td></tr>
<tr><td><strong>C</strong></td><td>Edit config</td></tr>
<tr><td><strong>X</strong></td><td>Translate page to English</td></tr>
<tr><td><strong>q</strong></td><td>Quit</td></tr>
</table>
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

func rssPage(rssStore *rss.Store) (*html.Document, error) {
	var feedsSection string
	if rssStore != nil && len(rssStore.Feeds) > 0 {
		feedsSection = "\n<h2>Subscribed Feeds</h2>\n<ul>\n"
		for _, feed := range rssStore.Feeds {
			// Escape HTML in title
			title := feed.Title
			if title == "" {
				title = feed.URL
			}
			title = strings.ReplaceAll(title, "&", "&amp;")
			title = strings.ReplaceAll(title, "<", "&lt;")
			title = strings.ReplaceAll(title, ">", "&gt;")
			title = strings.ReplaceAll(title, "\"", "&quot;")

			// Build feed URL for navigation
			feedURL := "rss://feed/" + neturl.PathEscape(feed.URL)

			// Get unread count
			unread := rssStore.UnreadCount(feed.URL)
			unreadStr := ""
			if unread > 0 {
				unreadStr = fmt.Sprintf(" <strong>(%d unread)</strong>", unread)
			}

			// Last fetch info
			lastFetch := ""
			if !feed.LastFetch.IsZero() {
				ago := time.Since(feed.LastFetch)
				if ago < time.Minute {
					lastFetch = " - updated just now"
				} else if ago < time.Hour {
					lastFetch = fmt.Sprintf(" - updated %d minutes ago", int(ago.Minutes()))
				} else if ago < 24*time.Hour {
					lastFetch = fmt.Sprintf(" - updated %d hours ago", int(ago.Hours()))
				} else {
					lastFetch = fmt.Sprintf(" - updated %s", feed.LastFetch.Format("Jan 2"))
				}
			}

			// Error indicator
			if feed.FetchError != "" {
				lastFetch = " - <em>error fetching</em>"
			}

			feedsSection += fmt.Sprintf("<li><a href=\"%s\">%s</a>%s%s</li>\n",
				feedURL, title, unreadStr, lastFetch)
		}
		feedsSection += "</ul>\n"
	} else {
		feedsSection = "\n<p><em>No subscribed feeds yet. Use 'A' on any page to subscribe to its RSS feed.</em></p>\n"
	}

	// Total unread count
	totalUnread := 0
	if rssStore != nil {
		totalUnread = rssStore.AllUnreadCount()
	}
	unreadHeader := ""
	if totalUnread > 0 {
		unreadHeader = fmt.Sprintf("<p><strong>%d unread items</strong> - <a href=\"rss://unread\">View all unread</a></p>\n", totalUnread)
	}

	page := `<!DOCTYPE html>
<html>
<head><title>RSS Feeds</title></head>
<body>
<article>
<h1>RSS Feeds</h1>
` + unreadHeader + `
<p><a href="rss://all">View all items</a></p>
` + feedsSection + `
<h2>Keybindings</h2>
<table>
<tr><th>Key</th><th>Action</th></tr>
<tr><td><strong>F</strong></td><td>Open RSS feed list (this page)</td></tr>
<tr><td><strong>A</strong></td><td>Subscribe to current page's feed</td></tr>
<tr><td><strong>x</strong></td><td>Unsubscribe (when viewing a feed)</td></tr>
<tr><td><strong>gf</strong></td><td>Refresh all feeds</td></tr>
</table>

<h2>Navigation</h2>
<ul>
<li><a href="rss://all">All items</a> - All items from all feeds</li>
<li><a href="rss://unread">Unread items</a> - Only unread items</li>
</ul>
</article>
</body>
</html>`

	return html.ParseString(page)
}

func helpPage() (*html.Document, string, error) {
	page := `<!DOCTYPE html>
<html>
<head><title>Browse - Help</title></head>
<body>
<article>
<h1>Help &amp; Quickstart</h1>
<p><a href="browse://home">← Home</a></p>

<h2>Start Here</h2>
<ul>
<li><strong>Ctrl+L</strong> omnibox (URL, search, AI)</li>
<li><strong>j/k</strong> scroll</li>
<li><strong>d/u</strong> half page down/up</li>
<li><strong>gg/G</strong> top/bottom</li>
<li><strong>[/]</strong> prev/next paragraph (focus mode)</li>
<li><strong>{/}</strong> prev/next section</li>
<li><strong>Ctrl+O/Tab</strong> back/forward</li>
<li><strong>gr</strong> refresh current page</li>
<li><strong>/</strong> find in page (then <strong>n/N</strong> next/prev)</li>
<li><strong>f</strong> follow link (labels appear)</li>
<li><strong>t</strong> table of contents</li>
<li><strong>n</strong> site navigation</li>
<li><strong>l</strong> link index</li>
<li><strong>H</strong> home</li>
<li><strong>q</strong> quit</li>
</ul>

<h2>Buffers &amp; Lists</h2>
<ul>
<li><strong>T</strong> new buffer</li>
<li><strong>gt/gT</strong> next/prev buffer</li>
<li><strong>&#96;</strong> buffer list</li>
<li><strong>M</strong> add favourite, <strong>'</strong> favourites list</li>
<li><strong>F</strong> RSS feeds, <strong>A</strong> subscribe, <strong>x</strong> unsubscribe, <strong>gf</strong> refresh all feeds</li>
</ul>

<h2>Features You Might Miss</h2>
<ul>
<li><strong>F</strong> RSS reader (<a href="rss://">rss://</a>)</li>
<li><strong>S</strong> AI summary (starts a chat)</li>
<li><strong>X</strong> translate page to English</li>
<li><strong>D</strong> define a word (type to filter, Enter to look up)</li>
<li><strong>s</strong> DOM structure inspector</li>
<li><strong>r</strong> reload with JavaScript (headless Chrome)</li>
<li><strong>z</strong> toggle light/dark theme</li>
<li><strong>P</strong> theme picker</li>
</ul>

<h2>Omnibox</h2>
<p>Press <strong>Ctrl+L</strong> to open the omnibox. Try:</p>
<ul>
<li><strong>help</strong> &mdash; open help</li>
<li><strong>rss</strong> &mdash; open RSS reader</li>
<li><strong>gh query</strong> or <strong>gh:query</strong> &mdash; GitHub</li>
<li><strong>hn query</strong> or <strong>hn:query</strong> &mdash; Hacker News</li>
<li><strong>wp query</strong> or <strong>wp:query</strong> &mdash; Wikipedia</li>
<li><strong>dict word</strong> or <strong>dict:word</strong> &mdash; Dictionary</li>
<li><strong>ai</strong> &mdash; summarize this page</li>
<li><strong>ai:question</strong> &mdash; custom summary prompt</li>
</ul>

<h2>Config</h2>
<p>Press <strong>C</strong> to edit config and hot-reload.</p>
</article>
</body>
</html>`

	doc, err := html.ParseString(page)
	return doc, page, err
}

func handleBrowseURL(targetURL string, favStore *favourites.Store) (*html.Document, string, error) {
	switch targetURL {
	case "", "browse://", "browse://home":
		doc, err := landingPage(favStore)
		return doc, "", err
	case "browse://help":
		return helpPage()
	default:
		page := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Browse - Not Found</title></head>
<body>
<article>
<h1>Page Not Found</h1>
<p>No internal page exists at <strong>%s</strong>.</p>
<p><a href="browse://home">← Home</a></p>
</article>
</body>
</html>`, targetURL)
		doc, err := html.ParseString(page)
		return doc, page, err
	}
}

// rssItemsPage renders a list of RSS items (for rss://all, rss://unread, or rss://feed/<url>)
func rssItemsPage(rssStore *rss.Store, feedURL string, unreadOnly bool, title string) (*html.Document, error) {
	var items []rss.FeedItem
	if feedURL != "" {
		items = rssStore.GetItems(feedURL)
	} else if unreadOnly {
		items = rssStore.GetUnreadItems()
	} else {
		items = rssStore.GetAllItems()
	}

	var itemsSection string
	if len(items) > 0 {
		itemsSection = "<ul>\n"
		for _, item := range items {
			// Escape HTML
			itemTitle := item.Title
			if itemTitle == "" {
				itemTitle = item.Link
			}
			itemTitle = strings.ReplaceAll(itemTitle, "&", "&amp;")
			itemTitle = strings.ReplaceAll(itemTitle, "<", "&lt;")
			itemTitle = strings.ReplaceAll(itemTitle, ">", "&gt;")

			// Truncate description
			desc := item.Description
			if len(desc) > 150 {
				desc = desc[:147] + "..."
			}
			desc = strings.ReplaceAll(desc, "&", "&amp;")
			desc = strings.ReplaceAll(desc, "<", "&lt;")
			desc = strings.ReplaceAll(desc, ">", "&gt;")

			// Read marker
			readMarker := ""
			if rssStore.IsRead(item.GUID) {
				readMarker = " <em>(read)</em>"
			}

			// Date and author
			meta := ""
			if !item.Published.IsZero() {
				meta = item.Published.Format("Jan 2, 2006")
			}
			if item.Author != "" {
				if meta != "" {
					meta += " · "
				}
				meta += item.Author
			}
			if meta != "" {
				meta = "<br/><small>" + meta + "</small>"
			}

			itemsSection += fmt.Sprintf("<li><a href=\"%s\">%s</a>%s%s<br/><small>%s</small></li>\n",
				item.Link, itemTitle, readMarker, meta, desc)
		}
		itemsSection += "</ul>\n"
	} else {
		itemsSection = "<p><em>No items found.</em></p>\n"
	}

	page := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>%s</title></head>
<body>
<article>
<h1>%s</h1>
<p><a href="rss://">← Back to feed list</a></p>
%s
</article>
</body>
</html>`, title, title, itemsSection)

	return html.ParseString(page)
}

// handleRSSURL routes rss:// URLs to the appropriate page handler
func handleRSSURL(rssStore *rss.Store, rssURL string) (*html.Document, error) {
	// Strip the rss:// prefix
	path := strings.TrimPrefix(rssURL, "rss://")

	switch {
	case path == "" || path == "/":
		// Main RSS feed list
		return rssPage(rssStore)

	case path == "all":
		// All items from all feeds
		return rssItemsPage(rssStore, "", false, "All RSS Items")

	case path == "unread":
		// Unread items only
		return rssItemsPage(rssStore, "", true, "Unread RSS Items")

	case strings.HasPrefix(path, "feed/"):
		// Specific feed items: rss://feed/<encoded-url>
		encodedURL := strings.TrimPrefix(path, "feed/")
		feedURL, err := neturl.PathUnescape(encodedURL)
		if err != nil {
			feedURL = encodedURL
		}
		// Find feed title
		title := feedURL
		if rssStore != nil {
			for _, f := range rssStore.Feeds {
				if f.URL == feedURL {
					if f.Title != "" {
						title = f.Title
					}
					break
				}
			}
		}
		return rssItemsPage(rssStore, feedURL, false, title)

	default:
		// Unknown RSS path - show main page
		return rssPage(rssStore)
	}
}

// ErrCancelled is returned when an operation is cancelled by the user.
var ErrCancelled = fmt.Errorf("cancelled")

// withSpinner runs a function while showing an animated spinner.
// The work function runs in a goroutine while the spinner animates.
// Press Escape to cancel the operation.
func withSpinner[T any](canvas *render.Canvas, message string, work func(ctx context.Context) T) T {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan T, 1)

	go func() {
		resultCh <- work(ctx)
	}()

	loader := render.NewLoadingDisplay(render.SpinnerWave, message)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// Set up non-blocking keyboard read
	keyCh := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			select {
			case keyCh <- buf[0]:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case result := <-resultCh:
			return result
		case key := <-keyCh:
			// Escape (27) or Ctrl+C (3) cancels
			if key == 27 || key == 3 {
				cancel()
				// Show cancelled message briefly
				canvas.Clear()
				accentStyle := theme.Current.Accent.Style()
				accentStyle.Bold = true
				loader.DrawBoxStyled(canvas, "Cancelled", accentStyle)
				canvas.RenderToWithBase(os.Stdout, theme.Current.BaseStyle())
				time.Sleep(200 * time.Millisecond)
				// Wait for goroutine to finish and return its result
				return <-resultCh
			}
		case <-ticker.C:
			loader.Tick()
			canvas.Clear()
			accentStyle := theme.Current.Accent.Style()
			accentStyle.Bold = true
			loader.DrawBoxStyled(canvas, "", accentStyle)
			canvas.RenderToWithBase(os.Stdout, theme.Current.BaseStyle())
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
// Cancellable with Escape key.
func fetchWithSpinner(canvas *render.Canvas, targetURL string, ruleCache *rules.Cache) (*html.Document, string, error) {
	result := withSpinner(canvas, targetURL, func(ctx context.Context) fetchResult {
		doc, content, err := fetchWithRulesCtx(ctx, targetURL, ruleCache)
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
// Cancellable with Escape key.
func fetchBrowserWithSpinner(canvas *render.Canvas, targetURL string) (*html.Document, error) {
	result := withSpinner(canvas, targetURL+" (JS)", func(ctx context.Context) browserResult {
		doc, err := fetchWithBrowserCtx(ctx, targetURL)
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
// Cancellable with Escape key.
func generateRulesWithSpinner(canvas *render.Canvas, generator *rules.GeneratorV2, domain, targetURL, htmlContent, providerName string) (*rules.Rule, error) {
	result := withSpinner(canvas, "Generating template with "+providerName+"...", func(ctx context.Context) ruleGenResult {
		// Add timeout to the cancellable context
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		rule, err := generator.GeneratePageType(ctx, domain, targetURL, htmlContent)
		return ruleGenResult{rule, err}
	})
	return result.rule, result.err
}

// generateRulesV1WithSpinner generates AI rules (v1 fallback) while showing spinner.
// Cancellable with Escape key.
func generateRulesV1WithSpinner(canvas *render.Canvas, generator *rules.Generator, domain, htmlContent, providerName string) (*rules.Rule, error) {
	result := withSpinner(canvas, "Trying v1 fallback with "+providerName+"...", func(ctx context.Context) ruleGenResult {
		// Add timeout to the cancellable context
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		rule, err := generator.Generate(ctx, domain, htmlContent)
		return ruleGenResult{rule, err}
	})
	return result.rule, result.err
}

// searchResult holds the result of a web search operation.
type searchResult struct {
	results *search.Results
	err     error
}

// searchWithSpinner performs a web search while showing spinner.
// Cancellable with Escape key.
func searchWithSpinner(canvas *render.Canvas, provider search.Provider, query string) (*search.Results, error) {
	result := withSpinner(canvas, "Searching "+provider.Name()+"...", func(ctx context.Context) searchResult {
		// TODO: Pass context to search provider when supported
		results, err := provider.Search(query)
		return searchResult{results, err}
	})
	return result.results, result.err
}

// isImageURL checks if a URL points to an image file
func isImageURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".jpg") ||
		strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".gif") ||
		strings.HasSuffix(lower, ".webp") ||
		strings.Contains(lower, ".png?") ||
		strings.Contains(lower, ".jpg?") ||
		strings.Contains(lower, ".jpeg?") ||
		strings.Contains(lower, ".gif?") ||
		strings.Contains(lower, ".webp?")
}

// openImageGallery downloads multiple images and opens them in Quick Look gallery
func openImageGallery(imageURLs []string, startIndex int) error {
	if len(imageURLs) == 0 {
		return nil
	}

	// Download all images concurrently
	type result struct {
		path string
		err  error
		idx  int
	}

	results := make(chan result, len(imageURLs))
	for i, imageURL := range imageURLs {
		go func(url string, idx int) {
			path, err := downloadImageToTemp(url)
			results <- result{path, err, idx}
		}(imageURL, i)
	}

	// Collect results
	imagePaths := make([]string, len(imageURLs))
	for i := 0; i < len(imageURLs); i++ {
		res := <-results
		if res.err == nil {
			imagePaths[res.idx] = res.path
		}
	}

	// Filter out failed downloads
	var validPaths []string
	for _, path := range imagePaths {
		if path != "" {
			validPaths = append(validPaths, path)
		}
	}

	if len(validPaths) == 0 {
		return fmt.Errorf("no images downloaded successfully")
	}

	// Adjust start index if needed
	if startIndex >= len(validPaths) {
		startIndex = 0
	}

	// Reorder so the selected image is first (qlmanage opens the first one)
	if startIndex > 0 && startIndex < len(validPaths) {
		reordered := append([]string{validPaths[startIndex]}, validPaths[:startIndex]...)
		reordered = append(reordered, validPaths[startIndex+1:]...)
		validPaths = reordered
	}

	// Open all images with Quick Look
	cmd := exec.Command("qlmanage", append([]string{"-p"}, validPaths...)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()

	// Activate Quick Look
	time.Sleep(100 * time.Millisecond)
	activateScript := `tell application "System Events" to set frontmost of first process whose name is "qlmanage" to true`
	exec.Command("osascript", "-e", activateScript).Run()

	// Clean up temp files after delay
	go func() {
		time.Sleep(60 * time.Second)
		for _, path := range validPaths {
			os.Remove(path)
		}
	}()

	return nil
}

// downloadImageToTemp downloads an image and saves to a temp file
func downloadImageToTemp(imageURL string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Determine extension
	ext := ".png"
	lower := strings.ToLower(imageURL)
	if strings.Contains(lower, ".jpg") || strings.Contains(lower, ".jpeg") {
		ext = ".jpg"
	} else if strings.Contains(lower, ".gif") {
		ext = ".gif"
	} else if strings.Contains(lower, ".webp") {
		ext = ".webp"
	}

	tmpFile, err := os.CreateTemp("", "browse_preview_*"+ext)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// openImagePreview downloads an image and opens it with Quick Look
func openImagePreview(imageURL string) error {
	// Download the image
	resp, err := http.Get(imageURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Save to temp file with proper extension
	ext := ".png"
	if strings.Contains(strings.ToLower(imageURL), ".jpg") || strings.Contains(strings.ToLower(imageURL), ".jpeg") {
		ext = ".jpg"
	} else if strings.Contains(strings.ToLower(imageURL), ".gif") {
		ext = ".gif"
	} else if strings.Contains(strings.ToLower(imageURL), ".webp") {
		ext = ".webp"
	}

	tmpFile, err := os.CreateTemp("", "browse_preview_*"+ext)
	if err != nil {
		return err
	}
	defer tmpFile.Close()

	// Copy image data to temp file
	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()

	// Open with Quick Look and force it to front
	cmd := exec.Command("qlmanage", "-p", tmpPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()

	// Give qlmanage a moment to start, then activate it
	time.Sleep(100 * time.Millisecond)

	// Activate Quick Look window using AppleScript
	activateScript := `tell application "System Events" to set frontmost of first process whose name is "qlmanage" to true`
	exec.Command("osascript", "-e", activateScript).Run()

	// Clean up temp file after delay
	go func() {
		time.Sleep(60 * time.Second)
		os.Remove(tmpPath)
	}()

	return nil
}

// generateAISummary uses the LLM to generate a summary of page content.
// Returns the summary HTML and a session ID for conversation continuity.
func generateAISummary(llmClient *llm.Client, canvas *render.Canvas, pageContent, prompt string) (string, string) {
	if llmClient == nil || !llmClient.Available() {
		return "<p>AI summary not available. Please configure an LLM provider (ANTHROPIC_API_KEY).</p>", ""
	}

	// Truncate content if too long (to stay within token limits)
	// Claude can handle ~200k tokens, so 100k chars (~25k tokens) is safe
	maxContent := 100000
	if len(pageContent) > maxContent {
		pageContent = pageContent[:maxContent] + "\n\n[Content truncated...]"
	}

	var sessionID string

	// Show loading spinner
	result := withSpinner(canvas, "Generating summary...", func(ctx context.Context) string {
		system := `You summarize web pages with clarity first, insight second. The user may ask follow-up questions about this content.

Structure your summary as a gradient:

**First, the facts** (what they asked for):
- What this content is about
- The key points, arguments, or information
- What conclusions or claims it makes

**Then, the insight** (the bit that makes it worth reading):
- What's genuinely interesting or surprising here
- The angle most readers might miss
- How this connects to something bigger
- Your honest take on what makes this notable (or not)

The factual section should feel reliable and complete. The insight section should feel like a sharp friend pointing out what you might have overlooked.

Keep it concise - aim for the facts in 2-3 paragraphs, then the insight in 1-2 paragraphs. Use a heading like "The Angle" or "What's Actually Interesting" to mark the transition.

Output as clean HTML using only: <h2>, <h3>, <p>, <ul>, <li>, <ol>, <strong>, <em>, <blockquote>
Do NOT include <html>, <head>, <body>, or <article> tags.`

		userPrompt := prompt + "\n\nContent to summarize:\n\n" + pageContent

		// Use session-based API to enable follow-up questions
		summary, sid, err := llmClient.StartSession(ctx, system, userPrompt)
		if err != nil {
			if err == llm.ErrNoProvider {
				return "<p>No LLM provider available. Please set ANTHROPIC_API_KEY.</p>"
			}
			return "<p>Error generating summary: " + err.Error() + "</p>"
		}
		sessionID = sid
		return summary
	})

	return result, sessionID
}

// generateDictHTML generates HTML for dictionary lookup results.
func generateDictHTML(word string, entries []dict.Entry) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>Definition: `)
	b.WriteString(word)
	b.WriteString(`</title></head><body><article>`)

	if len(entries) == 0 {
		b.WriteString(`<h1>`)
		b.WriteString(word)
		b.WriteString(`</h1><p>No definition found for "`)
		b.WriteString(word)
		b.WriteString(`".</p>`)
	} else {
		for _, entry := range entries {
			b.WriteString(`<h1>`)
			b.WriteString(entry.Word)
			b.WriteString(`</h1>`)

			// Phonetic pronunciation
			if entry.Phonetic != "" {
				b.WriteString(`<p><em>`)
				b.WriteString(entry.Phonetic)
				b.WriteString(`</em></p>`)
			}

			// Meanings by part of speech
			for _, meaning := range entry.Meanings {
				b.WriteString(`<h2>`)
				b.WriteString(meaning.PartOfSpeech)
				b.WriteString(`</h2>`)

				b.WriteString(`<ol>`)
				for _, def := range meaning.Definitions {
					b.WriteString(`<li>`)
					b.WriteString(def.Definition)
					if def.Example != "" {
						b.WriteString(`<br><em>"`)
						b.WriteString(def.Example)
						b.WriteString(`"</em>`)
					}
					b.WriteString(`</li>`)
				}
				b.WriteString(`</ol>`)

				// Synonyms for this part of speech
				if len(meaning.Synonyms) > 0 {
					b.WriteString(`<p><strong>Synonyms:</strong> `)
					b.WriteString(strings.Join(meaning.Synonyms, ", "))
					b.WriteString(`</p>`)
				}

				// Antonyms for this part of speech
				if len(meaning.Antonyms) > 0 {
					b.WriteString(`<p><strong>Antonyms:</strong> `)
					b.WriteString(strings.Join(meaning.Antonyms, ", "))
					b.WriteString(`</p>`)
				}
			}

			// Source URLs
			if len(entry.SourceURLs) > 0 {
				b.WriteString(`<hr><p><small>Sources: `)
				for i, src := range entry.SourceURLs {
					if i > 0 {
						b.WriteString(`, `)
					}
					b.WriteString(`<a href="`)
					b.WriteString(src)
					b.WriteString(`">`)
					b.WriteString(src)
					b.WriteString(`</a>`)
				}
				b.WriteString(`</small></p>`)
			}
		}
	}

	b.WriteString(`</article></body></html>`)
	return b.String()
}

// type PaymentRequest struct {
// 	IK string
// 	Amount   float64
// 	Currency string
// 	Source   string // e.g., credit card info
// 	Target   string // e.g., merchant account
// }
//
// type PaymentResponse struct {
// 	TransactionID string
// 	Status        string // e.g., "success", "failed"
// 	Message       string // additional info
// }
//
// type IdempotencyStore interface {
// 	Get(key string) (*IdempotencyRecord, error)
// 	Set(key string, record *IdempotencyRecord) error
// }
//
// type IdempotencyRecord struct {
// 	Key       string
// 	Status    string // "pending", "completed", "failed"
// 	Request   []byte
// 	Response  []byte
// 	Error     string
// 	CreatedAt time.Time
// }
//
// var store IdempotencyStore // Assume this is initialized elsewhere
//
// // This is what we're protecting
// func processPayment(req PaymentRequest) (PaymentResponse, error) {
// 	_ = req
// 	// talks to banks, moves money, scary stuff
//
// 	// check for idem key in store
// 	// if no match for key, store a pending record atomically
// 	//
//
//
// 	return PaymentResponse{TransactionID: "1234567890", Status: "success", Message: "Payment processed successfully"}, nil
// }
//
// var _ = processPayment
