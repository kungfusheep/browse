package omnibox

import "testing"

func TestParserParse(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name string
		in   string
		want Result
	}{
		{
			name: "http url",
			in:   "https://example.com",
			want: Result{URL: "https://example.com", IsSearch: false},
		},
		{
			name: "internal scheme url",
			in:   "browse://help",
			want: Result{URL: "browse://help", IsSearch: false},
		},
		{
			name: "help command",
			in:   "help",
			want: Result{URL: "browse://help", IsSearch: false},
		},
		{
			name: "rss command",
			in:   "rss",
			want: Result{URL: "rss://", IsSearch: false},
		},
		{
			name: "ai summary no prompt",
			in:   "ai",
			want: Result{IsAISummary: true, Provider: "AI Summary"},
		},
		{
			name: "ai summary prompt space",
			in:   "ai summarize this",
			want: Result{IsAISummary: true, AIPrompt: "summarize this", Provider: "AI Summary"},
		},
		{
			name: "ai summary prompt colon",
			in:   "ai:summarize this",
			want: Result{IsAISummary: true, AIPrompt: "summarize this", Provider: "AI Summary"},
		},
		{
			name: "dict lookup space",
			in:   "dict recursion",
			want: Result{IsDictLookup: true, DictWord: "recursion", Provider: "Dictionary"},
		},
		{
			name: "dict lookup colon",
			in:   "dict:recursion",
			want: Result{IsDictLookup: true, DictWord: "recursion", Provider: "Dictionary"},
		},
		{
			name: "internal search prefix space",
			in:   "gh openai",
			want: Result{Query: "openai", IsSearch: true, UseInternal: true, Provider: "GitHub"},
		},
		{
			name: "internal search prefix colon",
			in:   "gh:openai",
			want: Result{Query: "openai", IsSearch: true, UseInternal: true, Provider: "GitHub"},
		},
		{
			name: "default search",
			in:   "cats",
			want: Result{Query: "cats", IsSearch: true, UseInternal: true, Provider: "Search"},
		},
		{
			name: "looks like url",
			in:   "example.com",
			want: Result{URL: "https://example.com", IsSearch: false},
		},
		{
			name: "host port url",
			in:   "localhost:8080",
			want: Result{URL: "https://localhost:8080", IsSearch: false},
		},
		{
			name: "ai scheme url",
			in:   "ai://chat",
			want: Result{URL: "ai://chat", IsSearch: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Parse(tt.in)
			if got != tt.want {
				t.Fatalf("Parse(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
