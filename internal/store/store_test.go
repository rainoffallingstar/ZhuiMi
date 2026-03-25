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

	content := model.ArticleContent{
		ArticleID:    article.ID,
		ResolvedURL:  article.Link,
		FetchStatus:  model.ContentStatusFetched,
		SourceType:   "html_body",
		ContentText:  "Full text",
		ContentHash:  "content-hash",
		FetchedAt:    &now,
		ErrorMessage: "",
	}
	contentChanged, err := store.UpsertArticleContent(content)
	if err != nil {
		t.Fatal(err)
	}
	if !contentChanged {
		t.Fatal("expected article content to be created")
	}
	gotContent := store.ArticleContent(article.ID)
	if gotContent == nil || gotContent.ContentHash != "content-hash" || gotContent.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("unexpected article content: %+v", gotContent)
	}

	result := model.ProcessorResult{
		ArticleID:   article.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   "input-hash",
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"ok"}`,
		RawResponse: `{"summary":"ok"}`,
		ProcessedAt: now,
	}
	if err := store.SaveAIResult(result); err != nil {
		t.Fatal(err)
	}
	if !store.HasProcessedAIResult(article.ID, "generic_digest", "input-hash") {
		t.Fatal("expected processed ai result to be recorded")
	}
	gotResult := store.LatestAIResult(article.ID, "generic_digest")
	if gotResult == nil || gotResult.OutputJSON == "" {
		t.Fatalf("unexpected latest ai result: %+v", gotResult)
	}
	allResults := store.LatestAIResults(article.ID)
	if len(allResults) != 1 || allResults[0].Processor != "generic_digest" {
		t.Fatalf("unexpected latest ai results: %+v", allResults)
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
	if stats.Feeds != 1 || stats.Articles != 1 || stats.ScoredArticles != 1 || stats.ContentFetched != 1 || stats.ContentFallbacks != 0 || stats.ContentMissing != 0 || stats.AIResults != 1 || stats.Reports != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.ContentByStatus[model.ContentStatusFetched] != 1 {
		t.Fatalf("expected fetched content stats, got %+v", stats.ContentByStatus)
	}
	if stats.ProcessorLatestStatus["generic_digest"][model.ProcessorStatusProcessed] != 1 {
		t.Fatalf("expected generic_digest processed stats, got %+v", stats.ProcessorLatestStatus)
	}

	pending, err := store.ListArticlesForContentFetch(10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending content fetch after content saved, got %+v", pending)
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

	if err := store.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	now = time.Now().UTC()
	if _, err := store.UpsertArticle(model.Article{
		ID:            "doi:10.1000/migrate",
		DOI:           "10.1000/migrate",
		CanonicalLink: "https://example.com/migrate",
		Title:         "Migrate",
		Abstract:      "Abstract",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/migrate",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertArticleContent(model.ArticleContent{
		ArticleID:   "doi:10.1000/migrate",
		FetchStatus: model.ContentStatusRSSFallback,
		SourceType:  "rss_description",
		ContentText: "Abstract",
		ContentHash: "content-hash",
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIResult(model.ProcessorResult{
		ArticleID:   "doi:10.1000/migrate",
		Processor:   "generic_digest",
		InputHash:   "input-hash",
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"ok"}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestListArticlesByLatestProcessorStatus(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db")}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	articleA := model.Article{
		ID:            "link:a",
		CanonicalLink: "https://example.com/a",
		Title:         "Article A",
		Abstract:      "A",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-a",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/a",
	}
	articleB := model.Article{
		ID:            "link:b",
		CanonicalLink: "https://example.com/b",
		Title:         "Article B",
		Abstract:      "B",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-b",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/b",
	}
	if _, err := store.UpsertArticle(articleA); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertArticle(articleB); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveAIResult(model.ProcessorResult{
		ArticleID:   articleA.ID,
		Processor:   "aml_score",
		InputHash:   "old-failed",
		Status:      model.ProcessorStatusFailed,
		ProcessedAt: now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIResult(model.ProcessorResult{
		ArticleID:   articleA.ID,
		Processor:   "aml_score",
		InputHash:   "new-processed",
		Status:      model.ProcessorStatusProcessed,
		ProcessedAt: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIResult(model.ProcessorResult{
		ArticleID:   articleB.ID,
		Processor:   "aml_score",
		InputHash:   "failed",
		Status:      model.ProcessorStatusFailed,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	articles, err := store.ListArticlesByLatestProcessorStatus("aml_score", model.ProcessorStatusFailed, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 || articles[0].ID != articleB.ID {
		t.Fatalf("expected only articleB latest aml_score failed, got %+v", articles)
	}
}

func TestListArticlesByContentStatus(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db")}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	missingArticle := model.Article{
		ID:            "link:missing",
		CanonicalLink: "https://example.com/missing",
		Title:         "Missing",
		Abstract:      "Missing",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-missing",
		FirstSeenAt:   now.Add(-2 * time.Minute),
		LastSeenAt:    now.Add(-2 * time.Minute),
		Link:          "https://example.com/missing",
	}
	fetchedArticle := model.Article{
		ID:            "link:fetched",
		CanonicalLink: "https://example.com/fetched",
		Title:         "Fetched",
		Abstract:      "Fetched",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-fetched",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/fetched",
	}
	if _, err := store.UpsertArticle(missingArticle); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertArticle(fetchedArticle); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertArticleContent(model.ArticleContent{
		ArticleID:   fetchedArticle.ID,
		ResolvedURL: fetchedArticle.Link,
		FetchStatus: model.ContentStatusFetched,
		SourceType:  "html_body",
		ContentText: "Full text",
		ContentHash: "content-hash",
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}

	missing, err := store.ListArticlesByContentStatus("missing", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].ID != missingArticle.ID {
		t.Fatalf("expected only missing article, got %+v", missing)
	}

	fetched, err := store.ListArticlesByContentStatus(model.ContentStatusFetched, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || fetched[0].ID != fetchedArticle.ID {
		t.Fatalf("expected only fetched article, got %+v", fetched)
	}
}

func TestListArticlesMissingProcessorResult(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db")}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	articleA := model.Article{
		ID:            "link:a-missing",
		CanonicalLink: "https://example.com/a-missing",
		Title:         "Article A",
		Abstract:      "A",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-a",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/a-missing",
	}
	articleB := model.Article{
		ID:            "link:b-present",
		CanonicalLink: "https://example.com/b-present",
		Title:         "Article B",
		Abstract:      "B",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash-b",
		FirstSeenAt:   now.Add(-1 * time.Minute),
		LastSeenAt:    now.Add(-1 * time.Minute),
		Link:          "https://example.com/b-present",
	}
	if _, err := store.UpsertArticle(articleA); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertArticle(articleB); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIResult(model.ProcessorResult{
		ArticleID:   articleB.ID,
		Processor:   "generic_digest",
		InputHash:   "input-b",
		Status:      model.ProcessorStatusProcessed,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	articles, err := store.ListArticlesMissingProcessorResult("generic_digest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 || articles[0].ID != articleA.ID {
		t.Fatalf("expected only article missing generic_digest, got %+v", articles)
	}
}
