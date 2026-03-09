package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

func TestSQLiteStoreLifecycle(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db")}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	version, err := store.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("unexpected schema version: %d", version)
	}

	if err := store.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	article := model.Article{
		ID:            "doi:10.1000/demo",
		DOI:           "10.1000/demo",
		CanonicalLink: "https://example.com/article",
		Title:         "Demo",
		Abstract:      "Abstract",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/article",
	}
	created, err := store.UpsertArticle(article)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected article to be created")
	}

	score := model.Score{Research: 91, Social: 77, Blood: 88, Recommendation: 90, Reason: "值得看", Model: "demo", ScoredAt: now}
	if err := store.SaveScore(article.ID, score); err != nil {
		t.Fatal(err)
	}

	report := model.Report{Date: "2026-03-09", ArticleIDs: []string{article.ID}, UpdatedAt: now, Mode: model.ReportModeRaw}
	if err := store.SaveReport(report); err != nil {
		t.Fatal(err)
	}

	gotReport := store.Report(report.Date)
	if gotReport == nil || gotReport.Mode != model.ReportModeRaw {
		t.Fatalf("expected raw report mode roundtrip, got %+v", gotReport)
	}

	got := store.FindArticle(article.ID)
	if got == nil || got.LatestScore == nil {
		t.Fatal("expected scored article")
	}
	if got.LatestScore.Recommendation != 90 {
		t.Fatalf("unexpected recommendation: %d", got.LatestScore.Recommendation)
	}
	if !store.HasProcessedID(article.ID) {
		t.Fatal("expected processed id to be recorded")
	}

	stats := store.Stats()
	if stats.Feeds != 1 || stats.Articles != 1 || stats.ScoredArticles != 1 || stats.Reports != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestOpenMigratesVersionOneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.db")
	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}

	for _, stmt := range schemaMigrations()[0].stmts {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	store, err := Open(config.Config{StatePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	version, err := store.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("expected migrated schema version %d, got %d", currentSchemaVersion, version)
	}

	if err := store.MarkProcessedID("doi:10.1000/legacy", "test"); err != nil {
		t.Fatal(err)
	}
	if !store.HasProcessedID("doi:10.1000/legacy") {
		t.Fatal("expected processed_ids table to be available after migration")
	}

	now := time.Now().UTC()
	if err := store.SaveReport(model.Report{Date: "2026-03-09", ArticleIDs: nil, UpdatedAt: now, Mode: model.ReportModeRaw}); err != nil {
		t.Fatal(err)
	}
	if got := store.Report("2026-03-09"); got == nil || got.Mode != model.ReportModeRaw {
		t.Fatalf("expected migrated reports table to support mode, got %+v", got)
	}
}
