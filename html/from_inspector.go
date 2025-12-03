package html

import (
	"browse/inspector"
)

// FromInspectorBlocks creates a Document from inspector visible blocks.
// This allows users to manually select which parts of a page to display.
func FromInspectorBlocks(blocks []inspector.VisibleBlock) *Document {
	if len(blocks) == 0 {
		return nil
	}

	doc := &Document{
		Content: &Node{Type: NodeDocument},
	}

	for _, block := range blocks {
		node := blockToNode(block)
		if node != nil {
			doc.Content.Children = append(doc.Content.Children, node)
		}
	}

	return doc
}

func blockToNode(block inspector.VisibleBlock) *Node {
	if block.Text == "" {
		return nil
	}

	// Map HTML tags to our node types
	switch block.Tag {
	case "h1":
		return &Node{
			Type: NodeHeading1,
			Children: []*Node{{
				Type: NodeText,
				Text: block.Text,
			}},
		}
	case "h2":
		return &Node{
			Type: NodeHeading2,
			Children: []*Node{{
				Type: NodeText,
				Text: block.Text,
			}},
		}
	case "h3", "h4", "h5", "h6":
		return &Node{
			Type: NodeHeading3,
			Children: []*Node{{
				Type: NodeText,
				Text: block.Text,
			}},
		}

	case "li":
		// Create a list with single item
		list := &Node{Type: NodeList}
		item := &Node{Type: NodeListItem}
		item.Children = append(item.Children, &Node{
			Type: NodeText,
			Text: block.Text,
		})
		list.Children = append(list.Children, item)
		return list

	case "pre", "code":
		return &Node{
			Type: NodeCodeBlock,
			Text: block.Text,
		}

	case "blockquote":
		return &Node{
			Type: NodeBlockquote,
			Children: []*Node{{
				Type: NodeText,
				Text: block.Text,
			}},
		}

	default:
		// Default to paragraph for most content
		return &Node{
			Type: NodeParagraph,
			Children: []*Node{{
				Type: NodeText,
				Text: block.Text,
			}},
		}
	}
}
