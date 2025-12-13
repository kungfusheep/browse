// Command seeder searches DuckDuckGo for various topics and feeds
// discovered domains into the crawler's queue for processing.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var topics = []string{
	// Programming & Tech Blogs
	"best programming blogs",
	"software engineering blogs",
	"indie hacker blogs",
	"developer personal websites",
	"coding tutorial sites",
	"computer science blogs",
	"tech writer blogs",
	"engineering team blogs",
	"open source project blogs",
	"devops blogs",
	"sysadmin blogs",
	"security researcher blogs",
	"infosec blogs",
	"machine learning blogs",
	"data science blogs",
	"frontend development blogs",
	"backend development blogs",
	"web development tutorials",
	"functional programming blogs",
	"systems programming blogs",

	// Languages & Frameworks
	"golang tutorials",
	"rust programming blogs",
	"python tutorials text",
	"javascript tutorials",
	"haskell programming",
	"lisp programming blogs",
	"erlang elixir blogs",
	"c programming tutorials",
	"linux kernel documentation",
	"embedded systems blogs",
	"assembly programming tutorials",
	"compiler design blogs",
	"programming language theory",

	// Unix & Linux
	"linux documentation",
	"unix tutorials",
	"arch linux guides",
	"gentoo documentation",
	"freebsd handbook",
	"openbsd guides",
	"terminal tools blogs",
	"command line tutorials",
	"shell scripting guides",
	"vim tutorials",
	"emacs guides",
	"dotfiles repositories",
	"linux system administration",
	"self hosted software",

	// Minimalist & Text Web
	"minimalist web design",
	"plaintext websites",
	"text only news sites",
	"lightweight websites",
	"web without javascript",
	"html only websites",
	"brutalist web design",
	"accessible web design",
	"low bandwidth websites",
	"gemini protocol sites",
	"gopher protocol",
	"small web movement",
	"indie web movement",
	"personal websites directory",
	"web 1.0 aesthetic",
	"retro websites",
	"digital gardens",
	"zettelkasten blogs",

	// Academia & Research
	"academic papers open access",
	"research blogs",
	"science blogs",
	"physics blogs",
	"mathematics blogs",
	"philosophy blogs",
	"computer science papers",
	"arxiv alternatives",
	"preprint servers",
	"open access journals",
	"academic writing blogs",
	"research methodology",
	"statistics tutorials",

	// Documentation & Reference
	"technical documentation sites",
	"api documentation examples",
	"man pages online",
	"software manuals",
	"protocol specifications",
	"rfc documents",
	"w3c specifications",
	"programming references",
	"cheat sheets programming",
	"quick reference guides",

	// News & Journalism
	"text based news sites",
	"independent journalism",
	"investigative journalism sites",
	"nonprofit news organizations",
	"public interest journalism",
	"media criticism blogs",
	"press freedom organizations",
	"fact checking websites",
	"news aggregators text",
	"rss news feeds",

	// Privacy & Security
	"privacy focused websites",
	"digital rights organizations",
	"encryption tutorials",
	"privacy tools",
	"anonymous browsing guides",
	"opsec blogs",
	"threat modeling guides",
	"whistleblower resources",

	// Open Source & Free Software
	"free software foundation",
	"open source alternatives",
	"foss project websites",
	"gnu project documentation",
	"creative commons resources",
	"copyleft blogs",
	"software freedom",
	"open source sustainability",

	// History & Archives
	"internet history",
	"computer history",
	"digital archives",
	"web archives",
	"retrocomputing",
	"vintage computer blogs",
	"bbs history",
	"usenet archives",
	"early web preservation",

	// Writing & Publishing
	"writing blogs",
	"technical writing guides",
	"blogging platforms simple",
	"static site generators",
	"markdown resources",
	"plain text productivity",
	"note taking systems",
	"knowledge management blogs",

	// Government & Public Data
	"government open data",
	"public records databases",
	"legislation websites",
	"court documents online",
	"census data",
	"environmental data portals",
	"weather data sources",
	"geospatial data portals",

	// Standards & Specifications
	"web standards",
	"internet standards bodies",
	"protocol documentation",
	"file format specifications",
	"character encoding resources",
	"accessibility standards",
	"internationalization guides",

	// Encyclopedias & Knowledge
	"online encyclopedias",
	"wiki websites",
	"encyclopedic resources",
	"dictionary websites",
	"thesaurus online",
	"etymology resources",
	"language learning resources",
	"translation tools",

	// Books & Literature
	"public domain books",
	"free ebooks legal",
	"classic literature online",
	"poetry archives",
	"short story collections",
	"literary magazines online",
	"book review blogs",
	"reading lists curated",

	// DIY & Maker
	"diy electronics projects",
	"maker blogs",
	"hardware hacking",
	"repair guides",
	"right to repair",
	"open hardware projects",
	"3d printing resources",
	"amateur radio blogs",

	// Niche Technical
	"database internals blogs",
	"distributed systems blogs",
	"networking tutorials",
	"operating system development",
	"file system internals",
	"memory management tutorials",
	"performance engineering blogs",
	"site reliability engineering",
	"observability blogs",
	"chaos engineering",

	// Math & Logic
	"mathematical proofs online",
	"logic tutorials",
	"type theory resources",
	"category theory blogs",
	"cryptography tutorials",
	"algorithm visualizations",
	"competitive programming",
	"project euler solutions",

	// Art & Design (text-friendly)
	"ascii art resources",
	"typography blogs",
	"font design blogs",
	"color theory resources",
	"design principles blogs",
	"ux writing blogs",
	"information design",

	// Miscellaneous Quality Content
	"curated link collections",
	"awesome lists github",
	"hacker news favorites",
	"lobsters popular",
	"tildes discussions",
	"long form articles",
	"evergreen content blogs",
	"timeless articles",
	"best of web archives",
	"interesting websites list",
	"cool personal websites",
	"unique websites directory",
	"hidden gems internet",
	"underrated websites",
	"websites like old internet",
}

func main() {
	var (
		dbPath    = flag.String("db", "crawler.db", "Crawler database path")
		delay     = flag.Duration("delay", 5*time.Second, "Delay between searches")
		maxPerTopic = flag.Int("max", 30, "Max URLs to extract per topic")
		shuffle   = flag.Bool("shuffle", true, "Randomize topic order")
		dryRun    = flag.Bool("dry-run", false, "Print URLs without adding to DB")
		listTopics = flag.Bool("list", false, "List all topics and exit")
	)
	flag.Parse()

	if *listTopics {
		for i, t := range topics {
			fmt.Printf("%3d. %s\n", i+1, t)
		}
		fmt.Printf("\nTotal: %d topics\n", len(topics))
		return
	}

	// Open database
	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Ensure queue table exists
	db.Exec(`CREATE TABLE IF NOT EXISTS queue (
		url TEXT PRIMARY KEY,
		priority INTEGER DEFAULT 0,
		depth INTEGER DEFAULT 0,
		found_from TEXT,
		added TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)

	// Track completed searches
	db.Exec(`CREATE TABLE IF NOT EXISTS seeder_done (
		topic TEXT PRIMARY KEY,
		completed TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)

	// Get already completed topics
	completed := make(map[string]bool)
	rows, _ := db.Query("SELECT topic FROM seeder_done")
	if rows != nil {
		for rows.Next() {
			var t string
			rows.Scan(&t)
			completed[t] = true
		}
		rows.Close()
	}

	// Filter to remaining topics
	var remaining []string
	for _, t := range topics {
		if !completed[t] {
			remaining = append(remaining, t)
		}
	}

	if len(remaining) == 0 {
		fmt.Println("All topics already searched!")
		fmt.Println("Delete from seeder_done table to re-run topics.")
		return
	}

	fmt.Printf("Topics: %d total, %d completed, %d remaining\n",
		len(topics), len(completed), len(remaining))

	if *shuffle {
		rand.Shuffle(len(remaining), func(i, j int) {
			remaining[i], remaining[j] = remaining[j], remaining[i]
		})
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Stats
	totalAdded := 0
	topicsSearched := 0

	for _, topic := range remaining {
		select {
		case <-sigCh:
			fmt.Printf("\nStopping. Searched %d topics, added %d URLs\n", topicsSearched, totalAdded)
			return
		default:
		}

		fmt.Printf("\n[%d/%d] Searching: %s\n", topicsSearched+1, len(remaining), topic)

		urls, err := searchDDG(client, topic, *maxPerTopic)
		if err != nil {
			log.Printf("  Error: %v", err)
			time.Sleep(*delay)
			continue
		}

		added := 0
		for _, u := range urls {
			if *dryRun {
				fmt.Printf("  %s\n", u)
				added++
				continue
			}

			result, err := db.Exec(
				"INSERT OR IGNORE INTO queue (url, depth, found_from, priority) VALUES (?, 0, ?, 1)",
				u, "seeder:"+topic)
			if err != nil {
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				added++
			}
		}

		if !*dryRun {
			db.Exec("INSERT OR REPLACE INTO seeder_done (topic) VALUES (?)", topic)
		}

		fmt.Printf("  Found %d URLs, added %d new\n", len(urls), added)
		totalAdded += added
		topicsSearched++

		time.Sleep(*delay)
	}

	fmt.Printf("\nDone! Searched %d topics, added %d URLs to queue\n", topicsSearched, totalAdded)
}

// searchDDG searches DuckDuckGo HTML version and extracts result URLs
func searchDDG(client *http.Client, query string, maxResults int) ([]string, error) {
	// Use DDG HTML-only version
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; browse-seeder/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return extractURLs(string(body), maxResults), nil
}

// extractURLs pulls result URLs from DDG HTML response
func extractURLs(html string, max int) []string {
	// DDG HTML results have uddg= parameter with the actual URL
	// Pattern: uddg=https%3A%2F%2Fexample.com%2F
	uddgRe := regexp.MustCompile(`uddg=([^&"]+)`)
	matches := uddgRe.FindAllStringSubmatch(html, -1)

	seen := make(map[string]bool)
	var urls []string

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		decoded, err := url.QueryUnescape(match[1])
		if err != nil {
			continue
		}

		// Parse and normalize
		parsed, err := url.Parse(decoded)
		if err != nil || parsed.Host == "" {
			continue
		}

		// Skip DDG's own domains and common junk
		host := strings.ToLower(parsed.Host)
		if strings.Contains(host, "duckduckgo") ||
			strings.Contains(host, "google") ||
			strings.Contains(host, "bing") ||
			strings.Contains(host, "yahoo") ||
			strings.Contains(host, "facebook") ||
			strings.Contains(host, "twitter") ||
			strings.Contains(host, "youtube") ||
			strings.Contains(host, "amazon") ||
			strings.Contains(host, "reddit") {
			continue
		}

		// Normalize to just scheme + host
		normalized := fmt.Sprintf("https://%s", parsed.Host)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true

		urls = append(urls, normalized)
		if len(urls) >= max {
			break
		}
	}

	return urls
}
