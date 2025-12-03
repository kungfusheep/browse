package rules

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// matchRegex checks if pattern matches text.
func matchRegex(pattern, text string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(text), nil
}

// containsString checks if text contains substr (case-insensitive).
func containsString(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}

// htmlContainsSelector checks if HTML contains elements matching the selector.
func htmlContainsSelector(html, selector string) bool {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return false
	}
	return doc.Find(selector).Length() > 0
}
