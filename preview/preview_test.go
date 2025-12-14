package preview

import (
	"testing"
)

func TestExtractMetaTags(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		wantTitle   string
		wantDesc    string
		wantSite    string
		wantType    string
	}{
		{
			name: "OG tags",
			html: `<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="Test Title">
<meta property="og:description" content="Test description here">
<meta property="og:site_name" content="Test Site">
<meta property="og:type" content="article">
</head>
<body></body>
</html>`,
			wantTitle: "Test Title",
			wantDesc:  "Test description here",
			wantSite:  "Test Site",
			wantType:  "Article",
		},
		{
			name: "Twitter fallback",
			html: `<!DOCTYPE html>
<html>
<head>
<meta name="twitter:title" content="Twitter Title">
<meta name="twitter:description" content="Twitter description">
</head>
<body></body>
</html>`,
			wantTitle: "Twitter Title",
			wantDesc:  "Twitter description",
		},
		{
			name: "Standard title fallback",
			html: `<!DOCTYPE html>
<html>
<head>
<title>Page Title</title>
<meta name="description" content="Meta description">
</head>
<body></body>
</html>`,
			wantTitle: "Page Title",
			wantDesc:  "Meta description",
		},
		{
			name: "HTML entities",
			html: `<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="Title with &amp; ampersand">
<meta property="og:description" content="Description with &quot;quotes&quot;">
</head>
<body></body>
</html>`,
			wantTitle: "Title with & ampersand",
			wantDesc:  `Description with "quotes"`,
		},
		{
			name: "Long description truncation",
			html: `<!DOCTYPE html>
<html>
<head>
<meta property="og:description" content="This is a very long description that should be truncated because it exceeds the maximum length we want to display in the preview box. We want to keep the preview compact and readable so we limit the description to a reasonable length. This text is definitely longer than 200 characters.">
</head>
<body></body>
</html>`,
			wantDesc: "This is a very long description that should be truncated because it exceeds the maximum length we want to display in the preview box. We want to keep the preview compact and readable so we limit the d...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := ExtractMetaTags([]byte(tt.html))

			if meta.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", meta.Title, tt.wantTitle)
			}
			if meta.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", meta.Description, tt.wantDesc)
			}
			if meta.SiteName != tt.wantSite {
				t.Errorf("SiteName = %q, want %q", meta.SiteName, tt.wantSite)
			}
			if meta.ContentType != tt.wantType {
				t.Errorf("ContentType = %q, want %q", meta.ContentType, tt.wantType)
			}
		})
	}
}

func TestEstimateReadingTime(t *testing.T) {
	tests := []struct {
		words int
		want  string
	}{
		{50, "< 1 min read"},
		{200, "1 min read"},
		{400, "2 min read"},
		{1000, "5 min read"},
		{2000, "10 min read"},
	}

	for _, tt := range tests {
		got := EstimateReadingTime(tt.words)
		if got != tt.want {
			t.Errorf("EstimateReadingTime(%d) = %q, want %q", tt.words, got, tt.want)
		}
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{10000, "10.0k"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}

	for _, tt := range tests {
		got := formatCount(tt.n)
		if got != tt.want {
			t.Errorf("formatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
