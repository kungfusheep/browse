package inspector

import (
	"fmt"
	"strings"

	"browse/render"
)

// Viewer handles the split-pane inspector UI.
type Viewer struct {
	tree            *Tree
	canvas          *render.Canvas
	selectedIndex   int
	scrollOffset    int    // for tree scrolling
	treeWidth       int    // left pane width
	inSuggestions   bool   // true if navigating suggestions section
	suggestionIndex int    // current suggestion selection
	suggestions     []*Node // cached content suggestions
}

// NewViewer creates an inspector viewer for the given HTML.
func NewViewer(htmlContent string, canvas *render.Canvas) (*Viewer, error) {
	tree, err := ParseHTML(htmlContent)
	if err != nil {
		return nil, err
	}

	v := &Viewer{
		tree:      tree,
		canvas:    canvas,
		treeWidth: canvas.Width() * 2 / 5, // 40% for tree
	}

	// Get content suggestions (hidden nodes with >100 chars of text)
	v.suggestions = tree.ContentSuggestions(100)

	// Start in suggestions if we have any
	if len(v.suggestions) > 0 {
		v.inSuggestions = true
	}

	return v, nil
}

// Render draws the split-pane inspector view.
func (v *Viewer) Render() {
	v.canvas.Clear()
	width := v.canvas.Width()
	height := v.canvas.Height()

	// Draw divider
	dividerX := v.treeWidth
	for y := 0; y < height-1; y++ {
		v.canvas.Set(dividerX, y, '│', render.Style{Dim: true})
	}

	// Calculate how much space suggestions take
	suggestionsHeight := 0
	if len(v.suggestions) > 0 {
		suggestionsHeight = len(v.suggestions) + 3 // title + divider + items + gap
		if suggestionsHeight > 8 {
			suggestionsHeight = 8 // cap at 5 suggestions shown + header
		}
	}

	// Draw suggestions at top of left pane
	if len(v.suggestions) > 0 {
		v.renderSuggestions(0, 0, v.treeWidth-1, suggestionsHeight-1)
	}

	// Draw tree below suggestions
	treeY := suggestionsHeight
	treeHeight := height - 2 - suggestionsHeight
	if treeHeight > 0 {
		v.renderTree(0, treeY, v.treeWidth-1, treeHeight)
	}

	// Draw preview on right
	v.renderPreview(dividerX+2, 0, width-dividerX-3, height-2)

	// Draw help bar at bottom
	help := " [↑↓] navigate  [←→] collapse  [Space] toggle  [1-9] quick select  [Enter] apply  [Esc] cancel "
	if len(help) > width {
		help = " [↑↓]nav [Space]toggle [Enter]apply [Esc]cancel "
	}
	v.canvas.WriteString(0, height-1, help, render.Style{Reverse: true})
}

func (v *Viewer) renderSuggestions(x, y, width, height int) {
	// Title
	title := "💡 Hidden Content (likely main content)"
	v.canvas.WriteString(x+1, y, title, render.Style{Bold: true})
	v.canvas.DrawHLine(x, y+1, width, '─', render.Style{Dim: true})

	startY := y + 2
	maxItems := height - 2
	if maxItems > len(v.suggestions) {
		maxItems = len(v.suggestions)
	}

	for i := 0; i < maxItems; i++ {
		node := v.suggestions[i]
		lineY := startY + i

		// Number prefix for quick selection
		numLabel := fmt.Sprintf("%d.", i+1)

		// Node info
		name := node.DisplayName()
		textLen := node.TextLength()
		info := fmt.Sprintf("%s %s (%d chars)", numLabel, name, textLen)

		// Truncate if needed
		maxWidth := width - 2
		if len(info) > maxWidth && maxWidth > 3 {
			info = info[:maxWidth-3] + "..."
		}

		// Style based on selection
		style := render.Style{}
		if v.inSuggestions && i == v.suggestionIndex {
			style.Reverse = true
		}
		style.Dim = true // Dim because they're hidden

		v.canvas.WriteString(x+1, lineY, info, style)
	}
}

func (v *Viewer) renderTree(x, y, width, height int) {
	// Title
	title := "Page Structure"
	v.canvas.WriteString(x+1, y, title, render.Style{Bold: true})
	v.canvas.DrawHLine(x, y+1, width, '─', render.Style{Dim: true})

	visibleNodes := v.tree.VisibleNodes()
	startY := y + 2

	// Handle empty tree
	if len(visibleNodes) == 0 {
		v.canvas.WriteString(x+1, startY, "(no elements)", render.Style{Dim: true})
		return
	}

	// Ensure selected is in view
	if v.selectedIndex >= len(visibleNodes) {
		v.selectedIndex = len(visibleNodes) - 1
	}
	if v.selectedIndex < 0 {
		v.selectedIndex = 0
	}

	// Adjust scroll
	viewHeight := height - 2
	if viewHeight < 1 {
		viewHeight = 1
	}
	if v.selectedIndex < v.scrollOffset {
		v.scrollOffset = v.selectedIndex
	}
	if v.selectedIndex >= v.scrollOffset+viewHeight {
		v.scrollOffset = v.selectedIndex - viewHeight + 1
	}

	// Render visible nodes
	for i := v.scrollOffset; i < len(visibleNodes) && i-v.scrollOffset < viewHeight; i++ {
		node := visibleNodes[i]
		lineY := startY + (i - v.scrollOffset)

		// Build the line
		indent := strings.Repeat("  ", node.Depth)

		// Expand/collapse indicator
		expandChar := " "
		if node.HasChildren() {
			if node.Collapsed {
				expandChar = "▶"
			} else {
				expandChar = "▼"
			}
		}

		// Visibility checkbox
		checkBox := "[ ]"
		if node.Visible {
			checkBox = "[✓]"
		}

		// Node name
		name := node.DisplayName()

		// Truncate if needed
		fullLine := indent + expandChar + " " + name
		maxNameWidth := width - 6 // space for checkbox
		if maxNameWidth < 10 {
			maxNameWidth = 10
		}
		if len(fullLine) > maxNameWidth {
			if maxNameWidth > 3 {
				fullLine = fullLine[:maxNameWidth-3] + "..."
			} else if maxNameWidth > 0 {
				fullLine = fullLine[:maxNameWidth]
			}
		}

		// Style based on selection and content
		style := render.Style{}
		if !v.inSuggestions && i == v.selectedIndex {
			style.Reverse = true
		}
		if !node.Visible {
			style.Dim = true
		}
		// Highlight content-rich nodes in green (same threshold as suggestions)
		if node.TextLength() >= 100 {
			style.FgColor = 32 // ANSI green
		}

		// Draw the line
		v.canvas.WriteString(x+1, lineY, fullLine, style)

		// Draw checkbox at end of line
		checkStyle := render.Style{}
		if node.Visible {
			checkStyle.Bold = true
		} else {
			checkStyle.Dim = true
		}
		if !v.inSuggestions && i == v.selectedIndex {
			checkStyle.Reverse = true
		}
		// Match the green highlight for content-rich nodes
		if node.TextLength() >= 100 {
			checkStyle.FgColor = 32 // ANSI green
		}
		checkX := x + width - 4
		if checkX > x+len(fullLine)+1 {
			v.canvas.WriteString(checkX, lineY, checkBox, checkStyle)
		}
	}

	// Scroll indicators
	if v.scrollOffset > 0 && width > 2 {
		v.canvas.WriteString(x+width-2, startY, "↑", render.Style{Dim: true})
	}
	if v.scrollOffset+viewHeight < len(visibleNodes) && viewHeight > 0 && width > 2 {
		v.canvas.WriteString(x+width-2, startY+viewHeight-1, "↓", render.Style{Dim: true})
	}
}

func (v *Viewer) renderPreview(x, y, width, height int) {
	// Title
	title := "Content Preview"
	v.canvas.WriteString(x, y, title, render.Style{Bold: true})
	v.canvas.DrawHLine(x, y+1, width, '─', render.Style{Dim: true})

	// Get selected node
	var node *Node
	if v.inSuggestions {
		if v.suggestionIndex >= 0 && v.suggestionIndex < len(v.suggestions) {
			node = v.suggestions[v.suggestionIndex]
		}
	} else {
		visibleNodes := v.tree.VisibleNodes()
		if v.selectedIndex >= 0 && v.selectedIndex < len(visibleNodes) {
			node = visibleNodes[v.selectedIndex]
		}
	}

	if node == nil {
		return
	}

	startY := y + 2

	// Show node info
	info := "Tag: <" + node.Tag + ">"
	v.canvas.WriteString(x, startY, info, render.Style{Bold: true})

	if node.ID != "" {
		v.canvas.WriteString(x, startY+1, "ID: "+node.ID, render.Style{Dim: true})
		startY++
	}

	if len(node.Classes) > 0 {
		classes := "Classes: " + strings.Join(node.Classes, ", ")
		if len(classes) > width {
			classes = classes[:width-3] + "..."
		}
		v.canvas.WriteString(x, startY+1, classes, render.Style{Dim: true})
		startY++
	}

	sel := node.Selector
	if len(sel) > width-10 {
		sel = "..." + sel[len(sel)-(width-13):]
	}
	v.canvas.WriteString(x, startY+1, "Selector: "+sel, render.Style{Dim: true})
	startY += 2

	// Visibility status
	status := "Status: HIDDEN"
	statusStyle := render.Style{Dim: true}
	if node.Visible {
		status = "Status: VISIBLE"
		statusStyle = render.Style{Bold: true}
	}
	v.canvas.WriteString(x, startY+1, status, statusStyle)
	textLen := node.TextLength()
	startY += 2

	// Show child count
	if len(node.Children) > 0 {
		childInfo := strings.Repeat("─", 20)
		v.canvas.WriteString(x, startY+1, childInfo, render.Style{Dim: true})
		startY++

		visibleChildren := 0
		for _, child := range node.Children {
			if child.Visible {
				visibleChildren++
			}
		}
		childStatus := fmt.Sprintf("Children: %d/%d visible", visibleChildren, len(node.Children))
		v.canvas.WriteString(x, startY+1, childStatus, render.Style{})
		startY += 2
	}

	// Content preview - show full text
	textLabel := fmt.Sprintf("Full Text (%d chars):", textLen)
	v.canvas.WriteString(x, startY+1, textLabel, render.Style{Bold: true})
	startY += 2

	preview := node.PreviewText()
	if preview == "" {
		preview = "(no text content)"
	}

	// Word wrap the full text
	lines := wrapText(preview, width-1)
	maxLines := height - (startY - y) - 1
	if maxLines < 1 {
		maxLines = 1
	}

	for i, line := range lines {
		if i >= maxLines {
			remaining := len(lines) - maxLines
			moreText := fmt.Sprintf("... (%d more lines)", remaining)
			v.canvas.WriteString(x, startY+i, moreText, render.Style{Dim: true})
			break
		}
		v.canvas.WriteString(x, startY+i, line, render.Style{})
	}
}

// Navigation methods

func (v *Viewer) MoveUp() {
	if v.inSuggestions {
		if v.suggestionIndex > 0 {
			v.suggestionIndex--
		}
	} else {
		if v.selectedIndex > 0 {
			v.selectedIndex--
		} else if len(v.suggestions) > 0 {
			// Move to suggestions
			v.inSuggestions = true
			v.suggestionIndex = len(v.suggestions) - 1
		}
	}
}

func (v *Viewer) MoveDown() {
	if v.inSuggestions {
		if v.suggestionIndex < len(v.suggestions)-1 {
			v.suggestionIndex++
		} else {
			// Move to tree
			v.inSuggestions = false
			v.selectedIndex = 0
		}
	} else {
		nodes := v.tree.VisibleNodes()
		if v.selectedIndex < len(nodes)-1 {
			v.selectedIndex++
		}
	}
}

func (v *Viewer) Collapse() {
	if v.inSuggestions {
		return // No collapse in suggestions
	}
	nodes := v.tree.VisibleNodes()
	if v.selectedIndex >= 0 && v.selectedIndex < len(nodes) {
		node := nodes[v.selectedIndex]
		if node.HasChildren() && !node.Collapsed {
			node.ToggleCollapse()
		} else if node.Parent != nil {
			// Move to parent
			for i, n := range nodes {
				if n == node.Parent {
					v.selectedIndex = i
					break
				}
			}
		}
	}
}

func (v *Viewer) Expand() {
	if v.inSuggestions {
		return // No expand in suggestions
	}
	nodes := v.tree.VisibleNodes()
	if v.selectedIndex >= 0 && v.selectedIndex < len(nodes) {
		node := nodes[v.selectedIndex]
		if node.HasChildren() && node.Collapsed {
			node.ToggleCollapse()
		}
	}
}

func (v *Viewer) ToggleSelected() {
	if v.inSuggestions {
		if v.suggestionIndex >= 0 && v.suggestionIndex < len(v.suggestions) {
			v.suggestions[v.suggestionIndex].Toggle(false)
			// Refresh suggestions list
			v.suggestions = v.tree.ContentSuggestions(100)
			if v.suggestionIndex >= len(v.suggestions) {
				v.suggestionIndex = len(v.suggestions) - 1
			}
			if len(v.suggestions) == 0 {
				v.inSuggestions = false
			}
		}
	} else {
		nodes := v.tree.VisibleNodes()
		if v.selectedIndex >= 0 && v.selectedIndex < len(nodes) {
			nodes[v.selectedIndex].Toggle(false)
		}
	}
}

func (v *Viewer) ToggleSelectedRecursive() {
	if v.inSuggestions {
		if v.suggestionIndex >= 0 && v.suggestionIndex < len(v.suggestions) {
			v.suggestions[v.suggestionIndex].Toggle(true)
			// Refresh suggestions list
			v.suggestions = v.tree.ContentSuggestions(100)
			if v.suggestionIndex >= len(v.suggestions) {
				v.suggestionIndex = len(v.suggestions) - 1
			}
			if len(v.suggestions) == 0 {
				v.inSuggestions = false
			}
		}
	} else {
		nodes := v.tree.VisibleNodes()
		if v.selectedIndex >= 0 && v.selectedIndex < len(nodes) {
			nodes[v.selectedIndex].Toggle(true)
		}
	}
}

// SelectSuggestion selects a suggestion by number (1-9)
func (v *Viewer) SelectSuggestion(num int) {
	idx := num - 1
	if idx >= 0 && idx < len(v.suggestions) {
		v.inSuggestions = true
		v.suggestionIndex = idx
	}
}

// GetVisibleSelectors returns CSS selectors for all visible nodes.
func (v *Viewer) GetVisibleSelectors() []string {
	var selectors []string
	for _, node := range v.tree.AllNodes {
		if node.Visible {
			selectors = append(selectors, node.Selector)
		}
	}
	return selectors
}

// GetVisibleContent returns the text content of all visible leaf nodes.
// This collects content from visible nodes that don't have visible children.
func (v *Viewer) GetVisibleContent() []VisibleBlock {
	var blocks []VisibleBlock
	v.collectVisibleContent(v.tree.Root, &blocks)
	return blocks
}

// VisibleBlock represents a block of visible content with its tag context.
type VisibleBlock struct {
	Tag     string
	Text    string
	Href    string // for links
	IsBlock bool   // true for block elements like p, div, h1-h6
}

func (v *Viewer) collectVisibleContent(node *Node, blocks *[]VisibleBlock) {
	if node == nil || !node.Visible {
		return
	}

	// Check if this node has any visible children
	hasVisibleChildren := false
	for _, child := range node.Children {
		if child.Visible {
			hasVisibleChildren = true
			break
		}
	}

	// If no visible children, this is a leaf - collect its content
	if !hasVisibleChildren && node.Text != "" {
		isBlock := isBlockElement(node.Tag)
		*blocks = append(*blocks, VisibleBlock{
			Tag:     node.Tag,
			Text:    node.Text,
			IsBlock: isBlock,
		})
		return
	}

	// Recurse into visible children
	for _, child := range node.Children {
		v.collectVisibleContent(child, blocks)
	}
}

func isBlockElement(tag string) bool {
	blockTags := map[string]bool{
		"p": true, "div": true, "article": true, "section": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
		"ul": true, "ol": true, "li": true, "blockquote": true,
		"pre": true, "header": true, "footer": true, "main": true,
		"aside": true, "nav": true, "figure": true, "figcaption": true,
	}
	return blockTags[tag]
}

// Helper functions

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return nil
	}

	var lines []string
	words := strings.Fields(text)
	var currentLine strings.Builder

	for _, word := range words {
		if currentLine.Len() == 0 {
			currentLine.WriteString(word)
		} else if currentLine.Len()+1+len(word) <= width {
			currentLine.WriteString(" ")
			currentLine.WriteString(word)
		} else {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentLine.WriteString(word)
		}
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return lines
}
