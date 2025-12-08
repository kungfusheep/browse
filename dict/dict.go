// Package dict provides a dictionary lookup client using Free Dictionary API.
package dict

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	apiURL  = "https://api.dictionaryapi.dev/api/v2/entries/en/"
	timeout = 10 * time.Second
)

// Definition represents a single dictionary definition.
type Definition struct {
	PartOfSpeech string
	Definition   string
	Example      string
	Synonyms     []string
	Antonyms     []string
}

// Entry represents a dictionary entry for a word.
type Entry struct {
	Word       string
	Phonetic   string
	Phonetics  []Phonetic
	Meanings   []Meaning
	SourceURLs []string
}

// Phonetic represents pronunciation info.
type Phonetic struct {
	Text  string `json:"text"`
	Audio string `json:"audio"`
}

// Meaning represents a part of speech and its definitions.
type Meaning struct {
	PartOfSpeech string       `json:"partOfSpeech"`
	Definitions  []APIDef     `json:"definitions"`
	Synonyms     []string     `json:"synonyms"`
	Antonyms     []string     `json:"antonyms"`
}

// APIDef represents a single definition from the API.
type APIDef struct {
	Definition string   `json:"definition"`
	Example    string   `json:"example"`
	Synonyms   []string `json:"synonyms"`
	Antonyms   []string `json:"antonyms"`
}

// apiResponse represents the raw API response.
type apiResponse struct {
	Word       string     `json:"word"`
	Phonetic   string     `json:"phonetic"`
	Phonetics  []Phonetic `json:"phonetics"`
	Meanings   []Meaning  `json:"meanings"`
	SourceURLs []string   `json:"sourceUrls"`
}

// Client is a dictionary API client.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new dictionary client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Define looks up a word and returns entries.
func (c *Client) Define(word string) ([]Entry, error) {
	reqURL := apiURL + url.PathEscape(word)

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch definition: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// Word not found
		return nil, nil
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var responses []apiResponse
	if err := json.Unmarshal(body, &responses); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to our Entry type
	entries := make([]Entry, len(responses))
	for i, r := range responses {
		entries[i] = Entry{
			Word:       r.Word,
			Phonetic:   r.Phonetic,
			Phonetics:  r.Phonetics,
			Meanings:   r.Meanings,
			SourceURLs: r.SourceURLs,
		}
	}

	return entries, nil
}
