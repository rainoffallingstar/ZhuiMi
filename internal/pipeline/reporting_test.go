package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

func TestSavePublishedDateReportRawPersistsMode(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db"), ContentDir: filepath.Join(tempDir, "content")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	feedURL := "https://pubmed.example/rss"
	if err := db.UpsertFeed(model.Feed{URL: feedURL, Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	publishedAt := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	_, err = db.UpsertArticle(model.Article{
		ID:            "doi:10.1000/raw",
		DOI:           "10.1000/raw",
		CanonicalLink: "https://example.com/raw",
		Title:         "Raw article",
		Abstract:      "Raw abstract",
		PublishedAt:   &publishedAt,
		FeedURL:       feedURL,
		ContentHash:   "hash-raw",
		FirstSeenAt:   publishedAt,
		LastSeenAt:    publishedAt,
		Link:          "https://example.com/raw",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := savePublishedDateReport(cfg, db, "2026-03-09", model.ReportModeRaw, false); err != nil {
		t.Fatal(err)
	}

	report := db.Report("2026-03-09")
	if report == nil {
		t.Fatal("expected report to be saved")
	}
	if report.Mode != model.ReportModeRaw {
		t.Fatalf("expected raw report mode, got %q", report.Mode)
	}
	if len(report.ArticleIDs) != 1 {
		t.Fatalf("expected 1 raw article, got %d", len(report.ArticleIDs))
	}

	content, err := os.ReadFile(filepath.Join(cfg.ContentDir, "2026-03-09", "index.typ"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)
	if !strings.Contains(body, "Raw article") || strings.Contains(body, "研究分数") {
		t.Fatalf("unexpected raw report body: %s", body)
	}
}
