// Package sites provides a registry for site-specific HTML parsers.
// This allows modular handling of sites that need custom parsing
// without embedding site-specific code in the core engine.
package sites

import (
	"browse/html"
	"sync"
)

// Handler defines the interface for site-specific parsers.
type Handler interface {
	// Name returns a human-readable name for this handler.
	Name() string

	// Match returns true if this handler should process the given URL.
	Match(url string) bool

	// Parse processes the raw HTML and returns a document.
	// Returns nil if parsing fails or produces no useful content.
	Parse(rawHTML string) (*html.Document, error)
}

var (
	handlers []Handler
	mu       sync.RWMutex
)

// Register adds a site handler to the registry.
// Handlers are checked in registration order.
func Register(h Handler) {
	mu.Lock()
	defer mu.Unlock()
	handlers = append(handlers, h)
}

// ParseForURL finds a matching handler and parses the HTML.
// Returns the document and the handler name if successful.
// Returns nil, "" if no handler matches or parsing fails.
func ParseForURL(url, rawHTML string) (*html.Document, string) {
	mu.RLock()
	defer mu.RUnlock()

	for _, h := range handlers {
		if h.Match(url) {
			doc, err := h.Parse(rawHTML)
			if err == nil && doc != nil && hasContent(doc) {
				return doc, h.Name()
			}
			// Handler matched but parsing failed - continue to next handler
			// or fall through to default parser
		}
	}
	return nil, ""
}

// HasHandler returns true if any registered handler matches the URL.
func HasHandler(url string) bool {
	mu.RLock()
	defer mu.RUnlock()

	for _, h := range handlers {
		if h.Match(url) {
			return true
		}
	}
	return false
}

// Handlers returns a copy of all registered handler names.
func Handlers() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, len(handlers))
	for i, h := range handlers {
		names[i] = h.Name()
	}
	return names
}

// hasContent checks if a document has meaningful content.
func hasContent(doc *html.Document) bool {
	if doc == nil || doc.Content == nil {
		return false
	}
	return len(doc.Content.Children) > 0
}
