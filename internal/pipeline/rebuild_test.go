package pipeline

import (
	"path/filepath"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func TestRebuildReportsCompilesPDFWhenRequested(t *testing.T) {
	oldLookPath := reportLookPath()
	oldRun := reportRunCommand()
	defer restoreReportExec(oldLookPath, oldRun)

	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db"), ContentDir: filepath.Join(t.TempDir(), "content")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	publishedAt := time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)
	feedURL := "https://pubmed.example/rss"
	if err := db.UpsertFeed(model.Feed{URL: feedURL, Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	_, err = db.UpsertArticle(model.Article{
		ID:            "doi:10.1000/demo",
		DOI:           "10.1000/demo",
		CanonicalLink: "https://example.com/demo",
		Title:         "Demo article",
		Abstract:      "Demo abstract",
		PublishedAt:   &publishedAt,
		FeedURL:       feedURL,
		ContentHash:   "hash-demo",
		FirstSeenAt:   publishedAt,
		LastSeenAt:    publishedAt,
		Link:          "https://example.com/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveReport(model.Report{Date: "2026-03-09", ArticleIDs: []string{"doi:10.1000/demo"}, UpdatedAt: time.Now().UTC(), Mode: model.ReportModeRaw}); err != nil {
		t.Fatal(err)
	}

	var compileCalls int
	setReportExecFuncs(
		func(file string) (string, error) { return "/usr/local/bin/typst", nil },
		func(name string, args ...string) ([]byte, error) {
			compileCalls++
			return nil, nil
		},
	)

	if err := RebuildReports(cfg, db, "2026-03-09", true); err != nil {
		t.Fatal(err)
	}
	if compileCalls != 1 {
		t.Fatalf("expected typst compile once, got %d", compileCalls)
	}
}

func reportLookPath() func(string) (string, error) {
	return report.TestingLookPathFunc()
}

func reportRunCommand() func(string, ...string) ([]byte, error) {
	return report.TestingRunCommandFunc()
}

func setReportExecFuncs(lookPath func(string) (string, error), runCommand func(string, ...string) ([]byte, error)) {
	report.SetTestingExecFuncs(lookPath, runCommand)
}

func restoreReportExec(lookPath func(string) (string, error), runCommand func(string, ...string) ([]byte, error)) {
	report.SetTestingExecFuncs(lookPath, runCommand)
}
