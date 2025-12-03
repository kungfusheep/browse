package html

import (
	"regexp"
	"strings"

	"browse/rules"
)

// Debug flag - set to true to see link matching details
var DebugLinkMatching = false

// FromTemplateResult creates a Document from v2 template-rendered content.
// Parses the rendered text with styling markers into a document structure.
func FromTemplateResult(result *rules.ApplyV2Result, domain string) *Document {
	if result == nil || result.Content == "" {
		return nil
	}

	doc := &Document{
		Content: &Node{Type: NodeDocument},
	}

	// Build a map of text -> href for matching
	linkMap := make(map[string]string)
	for _, link := range result.Links {
		if link.Text != "" && link.Href != "" {
			linkMap[link.Text] = link.Href
		}
	}

	if DebugLinkMatching {
		println("=== FromTemplateResult Debug ===")
		println("Links in linkMap:", len(linkMap))
		for text := range linkMap {
			if len(text) > 50 {
				println("  Link text:", text[:50]+"...")
			} else {
				println("  Link text:", text)
			}
		}
	}

	// Parse the rendered content line by line
	lines := strings.Split(result.Content, "\n")

	// Current paragraph being built
	var currentPara *Node

	for _, line := range lines {
		// Skip empty lines - they separate paragraphs
		if strings.TrimSpace(line) == "" {
			currentPara = nil
			continue
		}

		// Check for box characters (header box)
		if isBoxLine(line) {
			// Skip box borders, content inside will be handled
			continue
		}

		// Check for horizontal rule - render as a dim paragraph
		if isHorizontalRule(line) {
			hrNode := &Node{Type: NodeParagraph}
			hrNode.Children = append(hrNode.Children, &Node{
				Type: NodeEmphasis, // Renders dimmed
				Children: []*Node{{
					Type: NodeText,
					Text: line,
				}},
			})
			doc.Content.Children = append(doc.Content.Children, hrNode)
			currentPara = nil
			continue
		}

		// Parse line content with inline styling
		lineNode := parseLine(line)

		// Check if this looks like a list item
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "•") ||
			strings.HasPrefix(trimmed, "-") ||
			strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "→") ||
			isNumberedItem(trimmed) {
			// It's a list item
			listItem := &Node{Type: NodeListItem}

			// Try to find a matching link for this item's text
			itemText := extractPlainText(lineNode)
			var matchedHref string
			if href, ok := linkMap[itemText]; ok {
				matchedHref = href
			} else {
				// Try partial match - the item text might contain the link text
				for linkText, href := range linkMap {
					if strings.Contains(itemText, linkText) {
						matchedHref = href
						break
					}
				}
			}

			if DebugLinkMatching {
				truncItem := itemText
				if len(truncItem) > 60 {
					truncItem = truncItem[:60] + "..."
				}
				if matchedHref != "" {
					println("  MATCHED:", truncItem)
					println("       →", matchedHref)
				} else {
					println("  NO MATCH:", truncItem)
				}
			}

			// If we found a matching href, wrap children in a NodeLink
			if matchedHref != "" {
				linkNode := &Node{
					Type:     NodeLink,
					Href:     matchedHref,
					Children: lineNode.Children,
				}
				listItem.Children = []*Node{linkNode}
			} else {
				listItem.Children = lineNode.Children
			}

			// Find or create a list
			var list *Node
			if len(doc.Content.Children) > 0 {
				lastChild := doc.Content.Children[len(doc.Content.Children)-1]
				if lastChild.Type == NodeList {
					list = lastChild
				}
			}
			if list == nil {
				list = &Node{Type: NodeList}
				doc.Content.Children = append(doc.Content.Children, list)
			}
			list.Children = append(list.Children, listItem)
			currentPara = nil
		} else {
			// Regular paragraph text
			if currentPara == nil {
				currentPara = &Node{Type: NodeParagraph}
				doc.Content.Children = append(doc.Content.Children, currentPara)
			} else {
				// Add line break within paragraph
				currentPara.Children = append(currentPara.Children, &Node{
					Type: NodeText,
					Text: " ",
				})
			}
			currentPara.Children = append(currentPara.Children, lineNode.Children...)
		}
	}

	return doc
}

// isBoxLine checks if a line is part of a Unicode box border
func isBoxLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	// Check for box drawing characters
	boxChars := "╔╗╚╝║═┌┐└┘│─"
	firstRune := []rune(trimmed)[0]
	return strings.ContainsRune(boxChars, firstRune)
}

// isHorizontalRule checks if a line is a horizontal rule
func isHorizontalRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	// Check for repeated rule characters
	ruleChars := "═─━─-="
	firstRune := []rune(trimmed)[0]
	if !strings.ContainsRune(ruleChars, firstRune) {
		return false
	}
	// Must be mostly the same character
	count := 0
	for _, r := range trimmed {
		if r == firstRune {
			count++
		}
	}
	return float64(count)/float64(len([]rune(trimmed))) > 0.8
}

// isNumberedItem checks if line starts with a number
func isNumberedItem(line string) bool {
	// Match patterns like "1.", "12.", "1)", etc.
	matched, _ := regexp.MatchString(`^\d+[\.\)]\s`, line)
	return matched
}

// extractPlainText recursively extracts plain text from a node
func extractPlainText(node *Node) string {
	if node == nil {
		return ""
	}
	var sb strings.Builder
	extractPlainTextRecursive(node, &sb)
	return strings.TrimSpace(sb.String())
}

func extractPlainTextRecursive(node *Node, sb *strings.Builder) {
	if node.Text != "" {
		sb.WriteString(node.Text)
	}
	for _, child := range node.Children {
		extractPlainTextRecursive(child, sb)
	}
}

// parseLine parses inline styling markers in a line
func parseLine(line string) *Node {
	node := &Node{Type: NodeParagraph}

	// Process the line for styled content
	// **bold**, ~~dim~~, [text](href)

	remaining := line
	for len(remaining) > 0 {
		// Look for the next marker
		boldIdx := strings.Index(remaining, "**")
		dimIdx := strings.Index(remaining, "~~")
		linkIdx := strings.Index(remaining, "[")

		// Find the earliest marker
		minIdx := len(remaining)
		markerType := ""

		if boldIdx >= 0 && boldIdx < minIdx {
			minIdx = boldIdx
			markerType = "bold"
		}
		if dimIdx >= 0 && dimIdx < minIdx {
			minIdx = dimIdx
			markerType = "dim"
		}
		if linkIdx >= 0 && linkIdx < minIdx {
			minIdx = linkIdx
			markerType = "link"
		}

		// Add any text before the marker
		if minIdx > 0 {
			node.Children = append(node.Children, &Node{
				Type: NodeText,
				Text: remaining[:minIdx],
			})
			remaining = remaining[minIdx:]
		}

		if markerType == "" {
			break
		}

		switch markerType {
		case "bold":
			// Find closing **
			remaining = remaining[2:] // Skip opening **
			endIdx := strings.Index(remaining, "**")
			if endIdx >= 0 {
				node.Children = append(node.Children, &Node{
					Type: NodeStrong,
					Children: []*Node{{
						Type: NodeText,
						Text: remaining[:endIdx],
					}},
				})
				remaining = remaining[endIdx+2:]
			}

		case "dim":
			// Find closing ~~
			remaining = remaining[2:] // Skip opening ~~
			endIdx := strings.Index(remaining, "~~")
			if endIdx >= 0 {
				node.Children = append(node.Children, &Node{
					Type: NodeEmphasis, // Render as dim/italic
					Children: []*Node{{
						Type: NodeText,
						Text: remaining[:endIdx],
					}},
				})
				remaining = remaining[endIdx+2:]
			}

		case "link":
			// Parse [text](href)
			remaining = remaining[1:] // Skip [
			closeIdx := strings.Index(remaining, "]")
			if closeIdx >= 0 && len(remaining) > closeIdx+1 && remaining[closeIdx+1] == '(' {
				linkText := remaining[:closeIdx]
				remaining = remaining[closeIdx+2:] // Skip ](
				hrefEnd := strings.Index(remaining, ")")
				if hrefEnd >= 0 {
					href := remaining[:hrefEnd]
					node.Children = append(node.Children, &Node{
						Type: NodeLink,
						Href: href,
						Children: []*Node{{
							Type: NodeText,
							Text: linkText,
						}},
					})
					remaining = remaining[hrefEnd+1:]
				}
			} else {
				// Not a valid link, treat [ as text
				node.Children = append(node.Children, &Node{
					Type: NodeText,
					Text: "[",
				})
			}
		}
	}

	return node
}
