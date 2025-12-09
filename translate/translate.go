// Package translate provides page translation using Lingva Translate API.
package translate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	timeout      = 10 * time.Second // Per-request timeout
	maxChunkSize = 500              // Safe for URL encoding
)

// Known working Lingva instances
var instances = []string{
	"https://translate.plausibility.cloud",
}

// Client is a translation API client.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new translation client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

type lingvaResponse struct {
	Translation string `json:"translation"`
}

// Translate translates text from source language to target language.
func (c *Client) Translate(text, source, target string) (string, error) {
	if text == "" {
		return "", nil
	}

	if source == "" {
		source = "auto"
	}

	chunks := splitIntoChunks(text, maxChunkSize)

	// Limit chunks to avoid too many requests
	maxChunks := 20
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}

	var results []string
	for _, chunk := range chunks {
		translated, err := c.translateChunk(chunk, source, target)
		if err != nil {
			return "", err
		}
		results = append(results, translated)
	}

	return strings.Join(results, "\n\n"), nil
}

func (c *Client) translateChunk(text, source, target string) (string, error) {
	encoded := url.PathEscape(text)

	var lastErr error
	for _, instance := range instances {
		reqURL := fmt.Sprintf("%s/api/v1/%s/%s/%s", instance, source, target, encoded)

		resp, err := c.httpClient.Get(reqURL)
		if err != nil {
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("%s returned %d", instance, resp.StatusCode)
			continue
		}

		var result lingvaResponse
		if err := json.Unmarshal(body, &result); err != nil {
			lastErr = err
			continue
		}

		return result.Translation, nil
	}

	return "", fmt.Errorf("all instances failed: %v", lastErr)
}

func splitIntoChunks(text string, maxSize int) []string {
	var chunks []string
	paragraphs := strings.Split(text, "\n\n")

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if len(p) <= maxSize {
			chunks = append(chunks, p)
			continue
		}

		// Split long paragraphs
		var current strings.Builder
		for _, word := range strings.Fields(p) {
			if current.Len()+len(word)+1 > maxSize && current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			if current.Len() > 0 {
				current.WriteString(" ")
			}
			current.WriteString(word)
		}
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
		}
	}

	return chunks
}
