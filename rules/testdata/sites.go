// Package testdata provides sample HTML from various sites for testing extraction.
package testdata

// TestSite represents a site to test extraction against.
type TestSite struct {
	Name     string
	Domain   string
	URL      string
	HTML     string // Sample HTML (truncated for testing)
	Expected ExpectedResult
}

// ExpectedResult describes what good extraction should produce.
type ExpectedResult struct {
	MinItems      int
	MaxItems      int
	HasLinks      bool  // Most items should have links
	MinTitleLen   int   // Minimum average title length
	SampleTitles  []string // Some expected title substrings
	ShouldSkip    bool  // LLM should recommend default parser
}

// Sites returns test sites for iteration.
var Sites = []TestSite{
	{
		Name:   "Hacker News",
		Domain: "news.ycombinator.com",
		URL:    "https://news.ycombinator.com",
		Expected: ExpectedResult{
			MinItems:    20,
			MaxItems:    40,
			HasLinks:    true,
			MinTitleLen: 20,
		},
	},
	{
		Name:   "BBC News",
		Domain: "www.bbc.com",
		URL:    "https://www.bbc.com/news",
		Expected: ExpectedResult{
			MinItems:    10,
			MaxItems:    100,
			HasLinks:    true,
			MinTitleLen: 20,
		},
	},
	{
		Name:   "Lobsters",
		Domain: "lobste.rs",
		URL:    "https://lobste.rs",
		Expected: ExpectedResult{
			MinItems:    15,
			MaxItems:    30,
			HasLinks:    true,
			MinTitleLen: 15,
		},
	},
	{
		Name:   "Wikipedia Article",
		Domain: "en.wikipedia.org",
		URL:    "https://en.wikipedia.org/wiki/Go_(programming_language)",
		Expected: ExpectedResult{
			ShouldSkip: true, // Default parser works better
		},
	},
	{
		Name:   "NPR",
		Domain: "text.npr.org",
		URL:    "https://text.npr.org",
		Expected: ExpectedResult{
			MinItems:    10,
			MaxItems:    50,
			HasLinks:    true,
			MinTitleLen: 15,
		},
	},
}
