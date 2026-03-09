package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

func TestWriteDailyWithOptionsRaw(t *testing.T) {
	cfg := config.Config{ContentDir: t.TempDir()}
	publishedAt := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	articles := []model.Article{{
		ID:          "doi:10.1000/demo",
		Title:       "Demo raw article",
		Abstract:    "Abstract body",
		DOI:         "10.1000/demo",
		FeedURL:     "https://pubmed.example/rss",
		Link:        "https://example.com/article",
		PublishedAt: &publishedAt,
	}}
	if err := WriteDailyWithOptions(cfg, "2026-03-09", articles, WriteOptions{Mode: model.ReportModeRaw}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(cfg.ContentDir, "2026-03-09", "index.typ"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(content)
	if !strings.Contains(body, "抓取文章数量") {
		t.Fatalf("expected raw header, got %s", body)
	}
	if !strings.Contains(body, "Demo raw article") {
		t.Fatalf("expected article title in report, got %s", body)
	}
	if strings.Contains(body, "研究分数") {
		t.Fatalf("did not expect score labels in raw report, got %s", body)
	}
	if !strings.Contains(body, "Feed") {
		t.Fatalf("expected feed line in raw report, got %s", body)
	}
}

func TestCompileDailyPDFUsesTypstCommand(t *testing.T) {
	oldLookPath := lookPathFunc
	oldRun := runCommandFunc
	defer func() {
		lookPathFunc = oldLookPath
		runCommandFunc = oldRun
	}()

	cfg := config.Config{ContentDir: t.TempDir()}
	var gotBin string
	var gotArgs []string
	lookPathFunc = func(file string) (string, error) {
		if file != "typst" {
			t.Fatalf("unexpected binary lookup: %s", file)
		}
		return "/usr/local/bin/typst", nil
	}
	runCommandFunc = func(name string, args ...string) ([]byte, error) {
		gotBin = name
		gotArgs = append([]string(nil), args...)
		return nil, nil
	}

	if err := CompileDailyPDF(cfg, "2026-03-09"); err != nil {
		t.Fatal(err)
	}
	if gotBin != "/usr/local/bin/typst" {
		t.Fatalf("unexpected typst binary: %s", gotBin)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "compile" || !strings.HasSuffix(gotArgs[1], filepath.Join("2026-03-09", "index.typ")) || !strings.HasSuffix(gotArgs[2], filepath.Join("2026-03-09", "index.pdf")) {
		t.Fatalf("unexpected typst args: %+v", gotArgs)
	}
}
