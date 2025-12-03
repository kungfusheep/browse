package inspector

import (
	"strings"
	"testing"
)

func TestParseHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
	<header>
		<nav>
			<a href="/">Home</a>
		</nav>
	</header>
	<main>
		<article>
			<h1>Title</h1>
			<p>Content here</p>
		</article>
	</main>
	<footer>
		<p>Copyright</p>
	</footer>
</body>
</html>`

	tree, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML failed: %v", err)
	}

	if tree.Root == nil {
		t.Fatal("Root is nil")
	}

	// Should have parsed some nodes
	if len(tree.AllNodes) == 0 {
		t.Fatal("No nodes parsed")
	}

	// Check that header/footer are hidden by default
	var header, main, footer *Node
	for _, node := range tree.AllNodes {
		switch node.Tag {
		case "header":
			header = node
		case "main":
			main = node
		case "footer":
			footer = node
		}
	}

	if header == nil {
		t.Error("header not found")
	} else if header.Visible {
		t.Error("header should be hidden by default")
	}

	if main == nil {
		t.Error("main not found")
	} else if !main.Visible {
		t.Error("main should be visible by default")
	}

	if footer == nil {
		t.Error("footer not found")
	} else if footer.Visible {
		t.Error("footer should be hidden by default")
	}
}

func TestNodeDisplayName(t *testing.T) {
	node := &Node{
		Tag:     "div",
		ID:      "main",
		Classes: []string{"container", "wide"},
	}

	name := node.DisplayName()
	if !strings.Contains(name, "div") {
		t.Error("display name should contain tag")
	}
	if !strings.Contains(name, "#main") {
		t.Error("display name should contain ID")
	}
}

func TestToggle(t *testing.T) {
	node := &Node{
		Tag:     "div",
		Visible: true,
		Children: []*Node{
			{Tag: "p", Visible: true},
			{Tag: "span", Visible: true},
		},
	}

	// Toggle single
	node.Toggle(false)
	if node.Visible {
		t.Error("node should be hidden after toggle")
	}
	if !node.Children[0].Visible {
		t.Error("children should not change with non-recursive toggle")
	}

	// Toggle recursive - node is currently hidden (from previous toggle)
	// So this will make it visible again, along with children
	node.Toggle(true)
	if !node.Visible {
		t.Error("node should be visible after recursive toggle")
	}
	if !node.Children[0].Visible {
		t.Error("child should also be visible with recursive toggle")
	}

	// Toggle recursive again to hide all
	node.Toggle(true)
	if node.Visible {
		t.Error("node should be hidden after second recursive toggle")
	}
	if node.Children[0].Visible {
		t.Error("child should be hidden with recursive toggle")
	}
}

func TestVisibleNodes(t *testing.T) {
	tree := &Tree{
		Root: &Node{
			Tag: "body",
			Children: []*Node{
				{Tag: "header", Collapsed: true, Children: []*Node{{Tag: "nav"}}},
				{Tag: "main"},
			},
		},
	}
	tree.flatten(tree.Root)

	visible := tree.VisibleNodes()

	// Should include body, header (but not nav since header is collapsed), main
	// Note: collapsed nodes themselves are visible, just their children are hidden
	found := make(map[string]bool)
	for _, n := range visible {
		found[n.Tag] = true
	}

	if !found["body"] {
		t.Error("body should be visible")
	}
	if !found["header"] {
		t.Error("header should be visible (even if collapsed)")
	}
	if !found["main"] {
		t.Error("main should be visible")
	}
	if found["nav"] {
		t.Error("nav should be hidden (parent is collapsed)")
	}
}
