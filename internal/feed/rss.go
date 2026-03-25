package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

type RSSItem struct {
	Title       string
	Link        string
	Description string
	PubDate     *time.Time
	DOI         string
	FeedURL     string
}

type rssDocument struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

type rdfDocument struct {
	Items []struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Encoded     string `xml:"encoded"`
		PubDate     string `xml:"pubDate"`
		Date        string `xml:"date"`
		GUID        string `xml:"guid"`
		ID          string `xml:"id"`
	} `xml:"item"`
}

type atomDocument struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     atomText   `xml:"title"`
	Summary   atomText   `xml:"summary"`
	Content   atomText   `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	ID        string     `xml:"id"`
	Links     []atomLink `xml:"link"`
}

type atomText struct {
	Value string `xml:",innerxml"`
}

type atomLink struct {
	Rel   string `xml:"rel,attr"`
	Href  string `xml:"href,attr"`
	Value string `xml:",chardata"`
}

type FetchResult struct {
	Feed        model.Feed
	Items       []RSSItem
	Err         error
	NotModified bool
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func FetchFeeds(ctx context.Context, cfg config.Config, feeds []model.Feed) []FetchResult {
	jobs := make(chan model.Feed)
	results := make(chan FetchResult)
	workers := cfg.FetchConcurrency
	if workers > len(feeds) && len(feeds) > 0 {
		workers = len(feeds)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				results <- fetchFeed(ctx, client, item, cfg)
			}
		}()
	}

	go func() {
		for _, item := range feeds {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	all := make([]FetchResult, 0, len(feeds))
	for result := range results {
		all = append(all, result)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Feed.URL < all[j].Feed.URL })
	return all
}

func fetchFeed(ctx context.Context, client *http.Client, feed model.Feed, cfg config.Config) FetchResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return FetchResult{Feed: feed, Err: fmt.Errorf("create request %s: %w", feed.URL, err)}
	}
	if feed.ETag != "" {
		req.Header.Set("If-None-Match", feed.ETag)
	}
	if feed.LastMod != "" {
		req.Header.Set("If-Modified-Since", feed.LastMod)
	}

	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{Feed: feed, Err: fmt.Errorf("fetch %s: %w", feed.URL, err)}
	}
	defer resp.Body.Close()

	feed.CheckedAt = time.Now().UTC()
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		feed.ETag = etag
	}
	if lastMod := strings.TrimSpace(resp.Header.Get("Last-Modified")); lastMod != "" {
		feed.LastMod = lastMod
	}

	if resp.StatusCode == http.StatusNotModified {
		feed.Status = "not_modified"
		return FetchResult{Feed: feed, NotModified: true}
	}
	if resp.StatusCode != http.StatusOK {
		return FetchResult{Feed: feed, Err: fmt.Errorf("fetch %s: unexpected status %d", feed.URL, resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchResult{Feed: feed, Err: fmt.Errorf("read %s: %w", feed.URL, err)}
	}

	items, err := parseFeedItems(body, feed.URL, shouldFilterTitleOnlyForFeed(feed, cfg))
	if err != nil {
		return FetchResult{Feed: feed, Err: fmt.Errorf("parse feed %s: %w", feed.URL, err)}
	}
	feed.SuccessAt = time.Now().UTC()
	feed.Status = "ok"
	feed.LastError = ""
	return FetchResult{Feed: feed, Items: items}
}

func parseFeedItems(body []byte, feedURL string, filterTitleOnly bool) ([]RSSItem, error) {
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, err
	}

	switch root.XMLName.Local {
	case "rss":
		var doc rssDocument
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		return parseRSSItems(doc, feedURL, filterTitleOnly), nil
	case "RDF":
		var doc rdfDocument
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		return parseRDFItems(doc, feedURL, filterTitleOnly), nil
	case "feed":
		var doc atomDocument
		if err := xml.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		return parseAtomItems(doc, feedURL, filterTitleOnly), nil
	default:
		return nil, fmt.Errorf("unsupported feed format %q", root.XMLName.Local)
	}
}

func parseRSSItems(doc rssDocument, feedURL string, filterTitleOnly bool) []RSSItem {
	items := make([]RSSItem, 0, len(doc.Channel.Items))
	for _, item := range doc.Channel.Items {
		title := strings.TrimSpace(item.Title)
		description := strings.TrimSpace(item.Description)
		if filterTitleOnly && shouldSkipTitleOnlyItem(title, description) {
			continue
		}
		publishedAt := parseDate(item.PubDate)
		items = append(items, RSSItem{
			Title:       title,
			Link:        strings.TrimSpace(item.Link),
			Description: description,
			PubDate:     publishedAt,
			DOI:         model.ExtractDOI(item.Title, item.Link, item.Description, item.GUID),
			FeedURL:     feedURL,
		})
	}
	return items
}

func parseAtomItems(doc atomDocument, feedURL string, filterTitleOnly bool) []RSSItem {
	items := make([]RSSItem, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		title := strings.TrimSpace(entry.Title.Value)
		description := strings.TrimSpace(firstNonEmpty(entry.Content.Value, entry.Summary.Value))
		if filterTitleOnly && shouldSkipTitleOnlyItem(title, description) {
			continue
		}
		link := pickAtomLink(entry.Links)
		publishedAt := parseDate(firstNonEmpty(entry.Published, entry.Updated))
		items = append(items, RSSItem{
			Title:       title,
			Link:        link,
			Description: description,
			PubDate:     publishedAt,
			DOI:         model.ExtractDOI(title, link, description, entry.ID),
			FeedURL:     feedURL,
		})
	}
	return items
}

func parseRDFItems(doc rdfDocument, feedURL string, filterTitleOnly bool) []RSSItem {
	items := make([]RSSItem, 0, len(doc.Items))
	for _, item := range doc.Items {
		title := strings.TrimSpace(item.Title)
		description := strings.TrimSpace(firstNonEmpty(item.Description, item.Encoded))
		if filterTitleOnly && shouldSkipTitleOnlyItem(title, description) {
			continue
		}
		publishedAt := parseDate(firstNonEmpty(item.PubDate, item.Date))
		link := strings.TrimSpace(item.Link)
		identifier := firstNonEmpty(item.GUID, item.ID)
		items = append(items, RSSItem{
			Title:       title,
			Link:        link,
			Description: description,
			PubDate:     publishedAt,
			DOI:         model.ExtractDOI(title, link, description, identifier),
			FeedURL:     feedURL,
		})
	}
	return items
}

func parseDate(raw string) *time.Time {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	formats := []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339, time.RFC3339Nano}
	for _, format := range formats {
		if parsed, err := time.Parse(format, raw); err == nil {
			return &parsed
		}
	}
	return nil
}

func shouldSkipTitleOnlyItem(title, description string) bool {
	normalizedTitle := normalizeContentForFilter(title)
	normalizedDescription := normalizeContentForFilter(description)
	if normalizedDescription == "" {
		return true
	}
	return normalizedTitle != "" && normalizedDescription == normalizedTitle
}

func normalizeContentForFilter(value string) string {
	value = html.UnescapeString(value)
	value = htmlTagPattern.ReplaceAllString(value, "")
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func pickAtomLink(links []atomLink) string {
	var fallback string
	for _, link := range links {
		href := strings.TrimSpace(firstNonEmpty(link.Href, link.Value))
		if href == "" {
			continue
		}
		rel := strings.TrimSpace(strings.ToLower(link.Rel))
		if rel == "" || rel == "alternate" {
			return href
		}
		if fallback == "" {
			fallback = href
		}
	}
	return fallback
}

func shouldFilterTitleOnlyForFeed(feed model.Feed, cfg config.Config) bool {
	if feed.AllowTitleOnly != nil {
		return !*feed.AllowTitleOnly
	}
	if !cfg.FilterTitleOnly {
		return false
	}
	return !matchesTitleOnlyAllowPattern(feed.URL, cfg.AllowTitleOnlyFor)
}

func matchesTitleOnlyAllowPattern(feedURL string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	host := feedHostname(feedURL)
	for _, rawPattern := range patterns {
		pattern := strings.TrimSpace(rawPattern)
		if pattern == "" {
			continue
		}
		if strings.Contains(pattern, "://") {
			if strings.HasSuffix(pattern, "*") {
				prefix := strings.TrimSuffix(pattern, "*")
				if strings.HasPrefix(feedURL, prefix) {
					return true
				}
				continue
			}
			if feedURL == pattern {
				return true
			}
			continue
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(strings.ToLower(pattern), "*.")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		normalized := strings.ToLower(pattern)
		if host == normalized || strings.HasSuffix(host, "."+normalized) {
			return true
		}
	}
	return false
}

func feedHostname(feedURL string) string {
	parsed, err := url.Parse(feedURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
