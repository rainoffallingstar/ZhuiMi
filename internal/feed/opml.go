package feed

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

type outline struct {
	XMLName  xml.Name  `xml:"outline"`
	Title    string    `xml:"title,attr"`
	Text     string    `xml:"text,attr"`
	XMLURL   string    `xml:"xmlUrl,attr"`
	Children []outline `xml:"outline"`
}

type opmlDocument struct {
	Body struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

func ImportOPMLFeeds(cfg config.Config, db *store.Store) error {
	entries, err := os.ReadDir(cfg.SubscribeDir)
	if err != nil {
		return fmt.Errorf("read subscribe dir: %w", err)
	}

	existingFeeds := db.ListFeeds()
	existingByURL := make(map[string]model.Feed, len(existingFeeds))
	for _, item := range existingFeeds {
		existingByURL[item.URL] = item
	}

	feedsMap := map[string]model.Feed{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".opml") {
			continue
		}
		path := filepath.Join(cfg.SubscribeDir, entry.Name())
		items, err := parseOPML(path)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.URL == "" {
				continue
			}
			feedsMap[item.URL] = mergeFeed(item, existingByURL[item.URL])
		}
	}

	feeds := make([]model.Feed, 0, len(existingFeeds)+len(feedsMap))
	legacyFeeds := make([]map[string]string, 0, len(feedsMap))
	newCount := 0
	updatedCount := 0
	for _, item := range feedsMap {
		feeds = append(feeds, item)
		legacyFeeds = append(legacyFeeds, map[string]string{"url": item.URL, "title": item.Title})
		if _, ok := existingByURL[item.URL]; ok {
			updatedCount++
		} else {
			newCount++
		}
	}
	for _, item := range existingFeeds {
		if _, ok := feedsMap[item.URL]; ok {
			continue
		}
		item.Status = "inactive"
		feeds = append(feeds, item)
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].URL < feeds[j].URL })
	sort.Slice(legacyFeeds, func(i, j int) bool { return legacyFeeds[i]["url"] < legacyFeeds[j]["url"] })

	if err := db.UpsertFeeds(feeds); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.FeedsJSONPath), 0o755); err != nil {
		return fmt.Errorf("create feeds json dir: %w", err)
	}
	content, err := json.MarshalIndent(legacyFeeds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode feeds json: %w", err)
	}
	if err := os.WriteFile(cfg.FeedsJSONPath, content, 0o644); err != nil {
		return fmt.Errorf("write feeds json: %w", err)
	}

	fmt.Printf("imported %d feeds (%d new, %d updated)\n", len(legacyFeeds), newCount, updatedCount)
	return nil
}

func mergeFeed(imported, existing model.Feed) model.Feed {
	merged := imported
	if strings.TrimSpace(merged.Title) == "" {
		merged.Title = existing.Title
	}
	merged.ETag = existing.ETag
	merged.LastMod = existing.LastMod
	merged.CheckedAt = existing.CheckedAt
	merged.SuccessAt = existing.SuccessAt
	merged.LastError = existing.LastError

	switch strings.TrimSpace(existing.Status) {
	case "", "inactive":
		merged.Status = "active"
	default:
		merged.Status = existing.Status
	}
	return merged
}

func parseOPML(path string) ([]model.Feed, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read opml %s: %w", path, err)
	}
	var doc opmlDocument
	if err := xml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse opml %s: %w", path, err)
	}
	var feeds []model.Feed
	for _, item := range doc.Body.Outlines {
		feeds = append(feeds, collectOutlines(item, filepath.Base(path))...)
	}
	return feeds, nil
}

func collectOutlines(item outline, source string) []model.Feed {
	var feeds []model.Feed
	if item.XMLURL != "" && strings.Contains(strings.ToLower(item.XMLURL), "pubmed") {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = strings.TrimSpace(item.Text)
		}
		feeds = append(feeds, model.Feed{URL: strings.TrimSpace(item.XMLURL), Title: title, SourceFile: source, Status: "active"})
	}
	for _, child := range item.Children {
		feeds = append(feeds, collectOutlines(child, source)...)
	}
	return feeds
}
