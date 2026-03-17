package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
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

func TestSavePublishedDateReportPDFSkipsLowScores(t *testing.T) {
	oldLookPath := report.TestingLookPathFunc()
	oldRun := report.TestingRunCommandFunc()
	defer report.SetTestingExecFuncs(oldLookPath, oldRun)

	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "config.typ"), []byte("#let template(title: \"t\", body) = body\n#let tufted = none\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	low := model.Article{
		ID:            "doi:10.1000/low",
		DOI:           "10.1000/low",
		CanonicalLink: "https://example.com/low",
		Title:         "Low score article",
		Abstract:      "Abstract low",
		PublishedAt:   &publishedAt,
		FeedURL:       feedURL,
		ContentHash:   "hash-low",
		FirstSeenAt:   publishedAt,
		LastSeenAt:    publishedAt,
		Link:          "https://example.com/low",
	}
	if _, err := db.UpsertArticle(low); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveScore(low.ID, model.Score{Research: 10, Social: 10, Blood: 10, Recommendation: 19, Reason: "low", Model: "demo", ScoredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	high := model.Article{
		ID:            "doi:10.1000/high",
		DOI:           "10.1000/high",
		CanonicalLink: "https://example.com/high",
		Title:         "High score article",
		Abstract:      "Abstract high",
		PublishedAt:   &publishedAt,
		FeedURL:       feedURL,
		ContentHash:   "hash-high",
		FirstSeenAt:   publishedAt,
		LastSeenAt:    publishedAt,
		Link:          "https://example.com/high",
	}
	if _, err := db.UpsertArticle(high); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveScore(high.ID, model.Score{Research: 10, Social: 10, Blood: 10, Recommendation: 20, Reason: "high", Model: "demo", ScoredAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	var compileCalls int
	report.SetTestingExecFuncs(
		func(string) (string, error) { return "/usr/local/bin/typst", nil },
		func(string, ...string) ([]byte, error) {
			compileCalls++
			return nil, nil
		},
	)

	if err := savePublishedDateReport(cfg, db, "2026-03-09", model.ReportModeScored, true); err != nil {
		t.Fatal(err)
	}
	if compileCalls != 1 {
		t.Fatalf("expected typst compile once, got %d", compileCalls)
	}

	content, err := os.ReadFile(filepath.Join(cfg.ContentDir, "2026-03-09", "index.typ"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)
	if strings.Contains(body, "Low score article") {
		t.Fatalf("did not expect low-score article in pdf report, got %s", body)
	}
	if !strings.Contains(body, "High score article") {
		t.Fatalf("expected high-score article in pdf report, got %s", body)
	}
}
