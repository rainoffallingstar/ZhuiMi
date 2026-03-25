package content

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

func TestExtractTextPrefersMetaAndArticle(t *testing.T) {
	body := []byte(`<html><head><meta name="description" content="Meta summary"></head><body><article><p>Full body paragraph one.</p><p>Full body paragraph two.</p></article></body></html>`)
	text, sourceType := ExtractText(body)
	if sourceType != "html_meta_body" {
		t.Fatalf("unexpected source type: %q", sourceType)
	}
	for _, want := range []string{"Meta summary", "Full body paragraph one.", "Full body paragraph two."} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in extracted text, got %q", want, text)
		}
	}
}

func TestFetcherFallsBackToRSSDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF"))
	}))
	defer server.Close()

	fetcher := NewFetcher(config.Config{
		ContentEnabled:   true,
		ContentTimeout:   time.Second,
		ContentMaxBytes:  2048,
		ContentUserAgent: "test-agent",
	})
	article := model.Article{
		ID:       "article-1",
		Title:    "Demo",
		Link:     server.URL,
		Abstract: "RSS fallback summary",
	}

	got := fetcher.FetchArticle(context.Background(), article)
	if got.FetchStatus != model.ContentStatusRSSFallback {
		t.Fatalf("expected rss fallback status, got %+v", got)
	}
	if got.ContentText != "RSS fallback summary" || got.SourceType != "rss_description" {
		t.Fatalf("unexpected fallback content: %+v", got)
	}
}

func TestFetcherExtractsHTMLContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><meta property="og:description" content="Meta summary"></head><body><main><h1>Hello</h1><p>World body</p></main></body></html>`))
	}))
	defer server.Close()

	fetcher := NewFetcher(config.Config{
		ContentEnabled:   true,
		ContentTimeout:   time.Second,
		ContentMaxBytes:  4096,
		ContentUserAgent: "test-agent",
		ContentStoreHTML: true,
	})
	article := model.Article{
		ID:    "article-2",
		Title: "Demo",
		Link:  server.URL,
	}

	got := fetcher.FetchArticle(context.Background(), article)
	if got.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected fetched status, got %+v", got)
	}
	if got.ContentHash == "" || !strings.Contains(got.ContentText, "World body") || got.ContentHTML == "" {
		t.Fatalf("unexpected fetched content: %+v", got)
	}
}

func TestFetcherSkipsWhenContentDisabled(t *testing.T) {
	fetcher := NewFetcher(config.Config{
		ContentEnabled:   false,
		ContentTimeout:   time.Second,
		ContentMaxBytes:  2048,
		ContentUserAgent: "test-agent",
	})
	article := model.Article{
		ID:       "article-3",
		Title:    "Demo",
		Link:     "https://example.com/article",
		Abstract: "RSS fallback summary",
	}

	got := fetcher.FetchArticle(context.Background(), article)
	if got.FetchStatus != model.ContentStatusSkipped {
		t.Fatalf("expected skipped status when content disabled, got %+v", got)
	}
	if got.ContentText != "RSS fallback summary" || got.SourceType != "rss_description" {
		t.Fatalf("unexpected skipped fallback content: %+v", got)
	}
}
