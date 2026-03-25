package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

func TestLoadFeedsSkipsInactiveFeeds(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db"), FeedsJSONPath: filepath.Join(t.TempDir(), "missing.json")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://pubmed.ncbi.nlm.nih.gov/rss/search/1", Title: "OK", Status: "ok"},
		{URL: "https://pubmed.ncbi.nlm.nih.gov/rss/search/2", Title: "Active", Status: "active"},
		{URL: "https://pubmed.ncbi.nlm.nih.gov/rss/search/3", Title: "Inactive", Status: "inactive"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	feeds, err := loadFeeds(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Fatalf("expected 2 active feeds, got %d", len(feeds))
	}
	for _, item := range feeds {
		if item.Status == "inactive" {
			t.Fatalf("expected inactive feeds filtered out, got %+v", item)
		}
	}
}

func TestLoadFeedsFallsBackToLegacyFeedsJSON(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:       filepath.Join(tempDir, "store.db"),
		FeedsJSONPath:   filepath.Join(tempDir, "feeds.json"),
		LegacyFeedsPath: filepath.Join(tempDir, "pubmed_feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	content := []byte(`[
  {"url":"https://example.com/rss.xml","title":"Example Feed","allow_title_only":true}
]`)
	if err := os.WriteFile(cfg.LegacyFeedsPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	feeds, err := loadFeeds(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 feed from legacy json, got %d", len(feeds))
	}
	if feeds[0].URL != "https://example.com/rss.xml" {
		t.Fatalf("unexpected legacy feed url: %q", feeds[0].URL)
	}
	if feeds[0].AllowTitleOnly == nil || !*feeds[0].AllowTitleOnly {
		t.Fatalf("expected allow_title_only loaded from legacy json, got %#v", feeds[0].AllowTitleOnly)
	}
}
