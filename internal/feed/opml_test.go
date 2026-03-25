package feed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

func TestImportOPMLFeedsPreservesStateAndInactivatesMissingFeeds(t *testing.T) {
	tempDir := t.TempDir()
	subscribeDir := filepath.Join(tempDir, "subscribe")
	if err := os.MkdirAll(subscribeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <body>
    <outline text="Mixed Feeds">
      <outline text="Imported title" xmlUrl="https://pubmed.ncbi.nlm.nih.gov/rss/search/1" />
      <outline text="New feed" xmlUrl="https://pubmed.ncbi.nlm.nih.gov/rss/search/2" />
      <outline text="Reactivated feed" xmlUrl="https://pubmed.ncbi.nlm.nih.gov/rss/search/4" />
      <outline text="Generic RSS" xmlUrl="https://example.com/feed.xml" allowTitleOnly="true" />
    </outline>
  </body>
</opml>`
	if err := os.WriteFile(filepath.Join(subscribeDir, "demo.opml"), []byte(opml), 0o644); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(tempDir, "store.db")
	db, err := store.Open(config.Config{StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	checkedAt := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	successAt := time.Date(2026, 3, 8, 10, 1, 0, 0, time.UTC)
	fixtures := []model.Feed{
		{
			URL:        "https://pubmed.ncbi.nlm.nih.gov/rss/search/1",
			Title:      "Old title",
			SourceFile: "old.opml",
			ETag:       "etag-1",
			LastMod:    "Mon, 08 Mar 2026 10:00:00 GMT",
			CheckedAt:  checkedAt,
			SuccessAt:  successAt,
			Status:     "ok",
		},
		{
			URL:        "https://pubmed.ncbi.nlm.nih.gov/rss/search/3",
			Title:      "To be disabled",
			SourceFile: "old.opml",
			ETag:       "etag-3",
			LastMod:    "Mon, 08 Mar 2026 09:00:00 GMT",
			CheckedAt:  checkedAt,
			SuccessAt:  successAt,
			Status:     "ok",
		},
		{
			URL:        "https://pubmed.ncbi.nlm.nih.gov/rss/search/4",
			Title:      "Old inactive title",
			SourceFile: "old.opml",
			ETag:       "etag-4",
			LastMod:    "Mon, 08 Mar 2026 08:00:00 GMT",
			CheckedAt:  checkedAt,
			SuccessAt:  successAt,
			Status:     "inactive",
		},
	}
	for _, item := range fixtures {
		if err := db.UpsertFeed(item); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Config{
		SubscribeDir:  subscribeDir,
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
		StatePath:     statePath,
	}
	if err := ImportOPMLFeeds(cfg, db); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	if len(feeds) != 5 {
		t.Fatalf("expected 5 feeds, got %d", len(feeds))
	}

	byURL := make(map[string]model.Feed, len(feeds))
	for _, item := range feeds {
		byURL[item.URL] = item
	}

	preserved := byURL["https://pubmed.ncbi.nlm.nih.gov/rss/search/1"]
	if preserved.Title != "Imported title" {
		t.Fatalf("expected imported title, got %q", preserved.Title)
	}
	if preserved.SourceFile != "demo.opml" {
		t.Fatalf("expected source file demo.opml, got %q", preserved.SourceFile)
	}
	if preserved.ETag != "etag-1" || preserved.LastMod != "Mon, 08 Mar 2026 10:00:00 GMT" {
		t.Fatalf("expected fetch cache preserved, got etag=%q lastmod=%q", preserved.ETag, preserved.LastMod)
	}
	if !preserved.CheckedAt.Equal(checkedAt) || !preserved.SuccessAt.Equal(successAt) {
		t.Fatalf("expected timestamps preserved, got checked=%v success=%v", preserved.CheckedAt, preserved.SuccessAt)
	}
	if preserved.Status != "ok" {
		t.Fatalf("expected status preserved, got %q", preserved.Status)
	}

	if got := byURL["https://pubmed.ncbi.nlm.nih.gov/rss/search/2"]; got.Status != "active" {
		t.Fatalf("expected new feed active, got %q", got.Status)
	}
	if got := byURL["https://example.com/feed.xml"]; got.Status != "active" || got.Title != "Generic RSS" {
		t.Fatalf("expected generic rss feed imported, got status=%q title=%q", got.Status, got.Title)
	}
	if got := byURL["https://example.com/feed.xml"]; got.AllowTitleOnly == nil || !*got.AllowTitleOnly {
		t.Fatalf("expected generic rss feed allow_title_only persisted, got %#v", got.AllowTitleOnly)
	}

	disabled := byURL["https://pubmed.ncbi.nlm.nih.gov/rss/search/3"]
	if disabled.Status != "inactive" {
		t.Fatalf("expected missing feed inactive, got %q", disabled.Status)
	}
	if disabled.ETag != "etag-3" {
		t.Fatalf("expected inactive feed state preserved, got etag=%q", disabled.ETag)
	}

	reactivated := byURL["https://pubmed.ncbi.nlm.nih.gov/rss/search/4"]
	if reactivated.Status != "active" {
		t.Fatalf("expected reactivated feed active, got %q", reactivated.Status)
	}
	if reactivated.ETag != "etag-4" {
		t.Fatalf("expected reactivated fetch state preserved, got etag=%q", reactivated.ETag)
	}
	if reactivated.Title != "Reactivated feed" {
		t.Fatalf("expected reactivated title from opml, got %q", reactivated.Title)
	}

	content, err := os.ReadFile(cfg.FeedsJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported []struct {
		URL            string `json:"url"`
		Title          string `json:"title"`
		AllowTitleOnly *bool  `json:"allow_title_only,omitempty"`
	}
	if err := json.Unmarshal(content, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 4 {
		t.Fatalf("expected 4 exported feeds, got %d", len(exported))
	}
	foundAllow := false
	for _, item := range exported {
		if item.URL == "https://example.com/feed.xml" && item.AllowTitleOnly != nil && *item.AllowTitleOnly {
			foundAllow = true
		}
	}
	if !foundAllow {
		t.Fatalf("expected exported feeds json to persist allow_title_only, got %#v", exported)
	}
}
