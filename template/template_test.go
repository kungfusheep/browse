package template

import (
	"strings"
	"testing"
)

func TestEngine_Render(t *testing.T) {
	e := New()

	tests := []struct {
		name     string
		template string
		data     any
		want     string
		wantErr  bool
	}{
		{
			name:     "simple text",
			template: "Hello, World!",
			data:     nil,
			want:     "Hello, World!",
		},
		{
			name:     "variable",
			template: "Hello, {{.Name}}!",
			data:     map[string]any{"Name": "Claude"},
			want:     "Hello, Claude!",
		},
		{
			name:     "upper",
			template: "{{.Title | upper}}",
			data:     map[string]any{"Title": "hello"},
			want:     "HELLO",
		},
		{
			name:     "hr",
			template: "{{hr 10 \"═\"}}",
			data:     nil,
			want:     "══════════",
		},
		{
			name:     "hr default char",
			template: "{{hr 5 \"\"}}",
			data:     nil,
			want:     "─────",
		},
		{
			name:     "wrap",
			template: "{{wrap 20 .Text}}",
			data:     map[string]any{"Text": "This is a long sentence that should wrap"},
			want:     "This is a long\nsentence that should\nwrap",
		},
		{
			name:     "truncate",
			template: "{{truncate 10 .Text}}",
			data:     map[string]any{"Text": "This is a very long text"},
			want:     "This is...",
		},
		{
			name:     "indent",
			template: "{{indent 2 .Text}}",
			data:     map[string]any{"Text": "line1\nline2"},
			want:     "  line1\n  line2",
		},
		{
			name:     "bold",
			template: "{{bold .Title}}",
			data:     map[string]any{"Title": "Important"},
			want:     "**Important**",
		},
		{
			name:     "dim",
			template: "{{dim .Meta}}",
			data:     map[string]any{"Meta": "2 hours ago"},
			want:     "~~2 hours ago~~",
		},
		{
			name:     "link",
			template: "{{link .Title .Href}}",
			data:     map[string]any{"Title": "Click me", "Href": "/page"},
			want:     "[Click me](/page)",
		},
		{
			name:     "range with limit",
			template: "{{range limit 2 .Items}}{{.}}\n{{end}}",
			data:     map[string]any{"Items": []string{"a", "b", "c", "d"}},
			want:     "a\nb\n",
		},
		{
			name:     "box",
			template: "{{box 20 .Content}}",
			data:     map[string]any{"Content": "Hello"},
			want:     "┌──────────────────┐\n│ Hello            │\n└──────────────────┘",
		},
		{
			name:     "default value",
			template: "{{default \"N/A\" .Missing}}",
			data:     map[string]any{},
			want:     "N/A",
		},
		{
			name:     "conditional with notEmpty",
			template: "{{if notEmpty .Title}}{{.Title}}{{end}}",
			data:     map[string]any{"Title": "Hello"},
			want:     "Hello",
		},
		{
			name:     "pad",
			template: "A{{pad 2}}B",
			data:     nil,
			want:     "A\n\nB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.Render(tt.template, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Render() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEngine_ComplexTemplate(t *testing.T) {
	e := New()

	tmpl := `{{.Site | upper}}
{{hr 20 "═"}}

{{range .Items}}• {{.Title}}{{if .Time}} — {{.Time | dim}}{{end}}
{{end}}`

	data := map[string]any{
		"Site": "BBC News",
		"Items": []map[string]any{
			{"Title": "Breaking story", "Time": "2h ago"},
			{"Title": "Another headline", "Time": ""},
			{"Title": "Third item", "Time": "5h ago"},
		},
	}

	got, err := e.Render(tmpl, data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// Check key parts
	if !strings.Contains(got, "BBC NEWS") {
		t.Error("Expected uppercase site name")
	}
	if !strings.Contains(got, "════════════════════") {
		t.Error("Expected horizontal rule")
	}
	if !strings.Contains(got, "• Breaking story") {
		t.Error("Expected bullet point")
	}
	if !strings.Contains(got, "~~2h ago~~") {
		t.Error("Expected dimmed time")
	}
	if strings.Contains(got, "Another headline —") {
		t.Error("Should not have dash when time is empty")
	}
}

func TestCols(t *testing.T) {
	items := []string{"Apple", "Banana", "Cherry", "Date"}
	result := cols(2, items)

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}
}

func TestWrapPreservesNewlines(t *testing.T) {
	input := "First paragraph.\n\nSecond paragraph."
	result := wrap(80, input)

	if !strings.Contains(result, "\n\n") {
		t.Error("Should preserve paragraph breaks")
	}
}

// TestHierarchicalNewsLayout proves the template system can render
// rich, structured content matching real news site hierarchy
func TestHierarchicalNewsLayout(t *testing.T) {
	e := New()

	// This represents what we SHOULD extract from BBC:
	// - A hero/featured story with summary
	// - Sectioned content (Top Stories, World, UK, etc.)
	// - Timestamps on items
	data := map[string]any{
		"Site": "BBC NEWS",
		"Hero": map[string]any{
			"text":    "Putin meets Trump envoys in Moscow",
			"href":    "/news/live/12345",
			"summary": "Latest updates on Ukraine peace talks as diplomatic efforts intensify",
		},
		"Sections": []map[string]any{
			{
				"name": "TOP STORIES",
				"items": []map[string]any{
					{"text": "Pavarotti statue frozen in ice rink", "href": "/news/1", "time": "2h"},
					{"text": "Honduras president released from prison", "href": "/news/2", "time": "3h"},
					{"text": "US Navy admiral ordered strike", "href": "/news/3", "time": "4h"},
				},
			},
			{
				"name": "WORLD",
				"items": []map[string]any{
					{"text": "Gaza aid delays criticised by UK", "href": "/world/1", "time": "1h"},
					{"text": "Australia defends social media ban", "href": "/world/2", "time": "5h"},
				},
			},
		},
	}

	// This is the STYLE we want - clear hierarchy, scannable, informative
	tmpl := `{{.Site}}
{{hr 50 "═"}}

{{bold .Hero.text}}
  {{.Hero.summary | dim}}
  {{link "↳ Live updates" .Hero.href}}

{{range .Sections}}
{{.name}}
{{hr 40 "─"}}
{{range .items}}  • {{link .text .href}}{{if .time}} {{.time | dim}}{{end}}
{{end}}{{end}}`

	got, err := e.Render(tmpl, data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	t.Logf("Rendered hierarchical layout:\n%s", got)

	// Verify structure
	if !strings.Contains(got, "BBC NEWS") {
		t.Error("Missing site name")
	}
	if !strings.Contains(got, "══════") {
		t.Error("Missing main header rule")
	}
	if !strings.Contains(got, "**Putin meets Trump envoys in Moscow**") {
		t.Error("Missing bold hero headline")
	}
	if !strings.Contains(got, "~~Latest updates on Ukraine peace talks") {
		t.Error("Missing dimmed summary")
	}
	if !strings.Contains(got, "TOP STORIES") {
		t.Error("Missing TOP STORIES section")
	}
	if !strings.Contains(got, "WORLD") {
		t.Error("Missing WORLD section")
	}
	if !strings.Contains(got, "──────") {
		t.Error("Missing section dividers")
	}
	if !strings.Contains(got, "Pavarotti statue frozen in ice rink") {
		t.Error("Missing story text")
	}
	if !strings.Contains(got, "~~2h~~") {
		t.Error("Missing dimmed timestamp")
	}
}

// TestNestedRangeAccess proves we can access nested arrays in sections
func TestNestedRangeAccess(t *testing.T) {
	e := New()

	data := map[string]any{
		"Sections": []map[string]any{
			{
				"title": "Section A",
				"stories": []map[string]any{
					{"headline": "Story A1"},
					{"headline": "Story A2"},
				},
			},
			{
				"title": "Section B",
				"stories": []map[string]any{
					{"headline": "Story B1"},
				},
			},
		},
	}

	tmpl := `{{range .Sections}}[{{.title}}]
{{range .stories}}- {{.headline}}
{{end}}{{end}}`

	got, err := e.Render(tmpl, data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	expected := `[Section A]
- Story A1
- Story A2
[Section B]
- Story B1
`
	if got != expected {
		t.Errorf("Got:\n%s\nExpected:\n%s", got, expected)
	}
}

// TestMapFieldAccess proves we can access nested map fields
func TestMapFieldAccess(t *testing.T) {
	e := New()

	data := map[string]any{
		"featured": map[string]any{
			"title":   "Big Story",
			"details": map[string]any{
				"author": "John Smith",
				"time":   "2 hours ago",
			},
		},
	}

	tmpl := `{{.featured.title}} by {{.featured.details.author}} ({{.featured.details.time}})`

	got, err := e.Render(tmpl, data)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	expected := "Big Story by John Smith (2 hours ago)"
	if got != expected {
		t.Errorf("Got: %s, Expected: %s", got, expected)
	}
}
