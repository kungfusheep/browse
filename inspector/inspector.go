// Package inspector provides a DOM structure viewer with toggle controls.
package inspector

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Node represents an element in the DOM tree.
type Node struct {
	Tag       string   // e.g., "div", "article", "nav"
	ID        string   // id attribute
	Classes   []string // class names
	Text      string   // text preview (first 100 chars)
	Children  []*Node
	Parent    *Node
	Depth     int
	Visible   bool // whether this node is included in output
	Collapsed bool // whether children are hidden in tree view
	Selector  string // CSS selector path to this element
}

// Tree represents the full DOM structure.
type Tree struct {
	Root     *Node
	AllNodes []*Node // flattened for easy navigation
}

// ParseHTML creates a DOM tree from HTML content.
func ParseHTML(htmlContent string) (*Tree, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	tree := &Tree{}

	// Find body or use root
	body := doc.Find("body")
	if body.Length() == 0 {
		body = doc.Selection
	}

	tree.Root = parseNode(body, nil, 0, "body")
	tree.flatten(tree.Root)

	return tree, nil
}

func parseNode(sel *goquery.Selection, parent *Node, depth int, parentSelector string) *Node {
	if sel.Length() == 0 {
		return nil
	}

	// Get the first element
	elem := sel.First()
	if elem.Length() == 0 {
		return nil
	}

	// Get node info
	tagName := goquery.NodeName(elem)
	if tagName == "" || tagName == "#document" {
		tagName = "body"
	}

	id, _ := elem.Attr("id")
	classAttr, _ := elem.Attr("class")
	classes := strings.Fields(classAttr)

	// Build selector
	selector := buildSelector(tagName, id, classes, parentSelector)

	// Get full text content (collapse whitespace but don't truncate)
	text := strings.TrimSpace(elem.Text())
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ") // collapse whitespace

	node := &Node{
		Tag:      tagName,
		ID:       id,
		Classes:  classes,
		Text:     text,
		Parent:   parent,
		Depth:    depth,
		Visible:  isDefaultVisible(tagName, classes),
		Selector: selector,
	}

	// Parse children (direct element children only)
	elem.Children().Each(func(i int, child *goquery.Selection) {
		childTag := goquery.NodeName(child)
		// Skip script, style, and other non-content elements
		if shouldSkipTag(childTag) {
			return
		}

		childNode := parseNode(child, node, depth+1, selector)
		if childNode != nil {
			node.Children = append(node.Children, childNode)
		}
	})

	return node
}

func buildSelector(tag, id string, classes []string, parentSelector string) string {
	sel := tag
	if id != "" {
		sel += "#" + id
	} else if len(classes) > 0 {
		// Just use first class for brevity
		sel += "." + classes[0]
	}

	if parentSelector != "" {
		return parentSelector + " > " + sel
	}
	return sel
}

func shouldSkipTag(tag string) bool {
	skip := map[string]bool{
		"script": true, "style": true, "link": true, "meta": true,
		"noscript": true, "svg": true, "path": true, "iframe": true,
		"#text": true, "#comment": true,
	}
	return skip[tag]
}

func isDefaultVisible(tag string, classes []string) bool {
	// By default, hide nav, header, footer, aside, ads
	hiddenTags := map[string]bool{
		"nav": true, "header": true, "footer": true, "aside": true,
	}
	if hiddenTags[tag] {
		return false
	}

	// Hide common ad/clutter classes
	for _, class := range classes {
		lower := strings.ToLower(class)
		if strings.Contains(lower, "ad") ||
			strings.Contains(lower, "sidebar") ||
			strings.Contains(lower, "nav") ||
			strings.Contains(lower, "menu") ||
			strings.Contains(lower, "footer") ||
			strings.Contains(lower, "header") ||
			strings.Contains(lower, "social") ||
			strings.Contains(lower, "share") ||
			strings.Contains(lower, "comment") {
			return false
		}
	}

	return true
}

// flatten builds the AllNodes slice for easy navigation.
func (t *Tree) flatten(node *Node) {
	if node == nil {
		return
	}
	t.AllNodes = append(t.AllNodes, node)
	for _, child := range node.Children {
		t.flatten(child)
	}
}

// VisibleNodes returns only the nodes that should be shown in the tree view
// (respecting collapsed state).
func (t *Tree) VisibleNodes() []*Node {
	var result []*Node
	t.collectVisible(t.Root, &result, false)
	return result
}

func (t *Tree) collectVisible(node *Node, result *[]*Node, parentCollapsed bool) {
	if node == nil {
		return
	}
	if !parentCollapsed {
		*result = append(*result, node)
	}

	collapsed := parentCollapsed || node.Collapsed
	for _, child := range node.Children {
		t.collectVisible(child, result, collapsed)
	}
}

// DisplayName returns a formatted name for the node.
func (n *Node) DisplayName() string {
	name := n.Tag
	if n.ID != "" {
		name += "#" + n.ID
	}
	for _, class := range n.Classes {
		if len(name) < 30 { // limit length
			name += "." + class
		}
	}
	if len(name) > 35 {
		name = name[:32] + "..."
	}
	return name
}

// Toggle flips the visibility of a node and optionally its children.
func (n *Node) Toggle(recursive bool) {
	n.Visible = !n.Visible
	if recursive {
		n.setChildrenVisible(n.Visible)
	}
}

// setChildrenVisible sets visibility for all descendants.
func (n *Node) setChildrenVisible(visible bool) {
	for _, child := range n.Children {
		child.Visible = visible
		child.setChildrenVisible(visible)
	}
}

// ToggleCollapse expands/collapses the node's children in tree view.
func (n *Node) ToggleCollapse() {
	n.Collapsed = !n.Collapsed
}

// HasChildren returns true if the node has child elements.
func (n *Node) HasChildren() bool {
	return len(n.Children) > 0
}

// PreviewText returns the text content for preview.
func (n *Node) PreviewText() string {
	if n.Text != "" {
		return n.Text
	}
	// Collect text from children
	var texts []string
	for _, child := range n.Children {
		if t := child.PreviewText(); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, " ")
}

// TextLength returns the approximate text length of this node.
func (n *Node) TextLength() int {
	return len(n.Text)
}

// ContentSuggestions returns hidden nodes that contain significant text content.
// These are likely candidates for the main content that got filtered out.
func (t *Tree) ContentSuggestions(minTextLength int) []*Node {
	var suggestions []*Node

	for _, node := range t.AllNodes {
		// Only suggest hidden nodes
		if node.Visible {
			continue
		}

		// Skip structural elements that are always hidden
		if node.Tag == "nav" || node.Tag == "header" || node.Tag == "footer" {
			continue
		}

		// Check if this node has significant text content
		textLen := node.TextLength()
		if textLen >= minTextLength {
			suggestions = append(suggestions, node)
		}
	}

	// Sort by text length (most content first)
	for i := 0; i < len(suggestions)-1; i++ {
		for j := i + 1; j < len(suggestions); j++ {
			if suggestions[j].TextLength() > suggestions[i].TextLength() {
				suggestions[i], suggestions[j] = suggestions[j], suggestions[i]
			}
		}
	}

	// Limit to top 10 suggestions
	if len(suggestions) > 10 {
		suggestions = suggestions[:10]
	}

	return suggestions
}
