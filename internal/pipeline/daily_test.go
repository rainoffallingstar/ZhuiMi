package pipeline

import (
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
