package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	if strings.Contains(body, "**") {
		t.Fatalf("did not expect markdown-style bold, got %s", body)
	}
	if strings.Contains(body, "#N/A") {
		t.Fatalf("did not expect typst code interpolation for N/A, got %s", body)
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
	if err := os.WriteFile(filepath.Join(cfg.ContentDir, "config.typ"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ContentDir, "2026-03-09"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	if len(gotArgs) != 5 || gotArgs[0] != "compile" || gotArgs[1] != "--root" || gotArgs[2] != cfg.ContentDir || !strings.HasSuffix(gotArgs[3], filepath.Join("2026-03-09", "index.typ")) || !strings.HasSuffix(gotArgs[4], filepath.Join("2026-03-09", "index.pdf")) {
		t.Fatalf("unexpected typst args: %+v", gotArgs)
	}
}

func TestDetectTypstRootFallsBackToRepoRoot(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inputDir := filepath.Join(repoRoot, "content", "ZhuiMi", "2026-03-09")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(inputDir, "index.typ")
	if err := os.WriteFile(inputPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	root, err := detectTypstRoot(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if root != repoRoot {
		t.Fatalf("expected repo root %s, got %s", repoRoot, root)
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	// 3-byte rune; truncating by bytes can easily cut it in half.
	value := "你好你好你好"
	out := truncate(value, 5)
	if !utf8.ValidString(out) {
		t.Fatalf("expected valid utf-8, got %q", out)
	}
}
