package feed

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
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
				results <- fetchFeed(ctx, client, item)
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

func fetchFeed(ctx context.Context, client *http.Client, feed model.Feed) FetchResult {
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

	var doc rssDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return FetchResult{Feed: feed, Err: fmt.Errorf("parse rss %s: %w", feed.URL, err)}
	}

	items := make([]RSSItem, 0, len(doc.Channel.Items))
	for _, item := range doc.Channel.Items {
		title := strings.TrimSpace(item.Title)
		description := strings.TrimSpace(item.Description)
		if shouldSkipTitleOnlyItem(title, description) {
			continue
		}
		publishedAt := parseDate(item.PubDate)
		items = append(items, RSSItem{
			Title:       title,
			Link:        strings.TrimSpace(item.Link),
			Description: description,
			PubDate:     publishedAt,
			DOI:         model.ExtractDOI(item.Title, item.Link, item.Description, item.GUID),
			FeedURL:     feed.URL,
		})
	}
	feed.SuccessAt = time.Now().UTC()
	feed.Status = "ok"
	feed.LastError = ""
	return FetchResult{Feed: feed, Items: items}
}

func parseDate(raw string) *time.Time {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	formats := []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339}
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
