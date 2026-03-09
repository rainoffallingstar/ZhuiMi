package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"zhuimi/internal/model"
)

func TestFetchFeedFiltersTitleOnlyItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Only Title A</title>
      <description></description>
      <link>https://example.com/a</link>
      <pubDate>Mon, 09 Mar 2026 00:00:00 GMT</pubDate>
      <guid>a</guid>
    </item>
    <item>
      <title>Only Title B</title>
      <description>Only Title B</description>
      <link>https://example.com/b</link>
      <pubDate>Mon, 09 Mar 2026 00:00:00 GMT</pubDate>
      <guid>b</guid>
    </item>
    <item>
      <title>Only Title C</title>
      <description><![CDATA[<p>Only Title C</p>]]></description>
      <link>https://example.com/c</link>
      <pubDate>Mon, 09 Mar 2026 00:00:00 GMT</pubDate>
      <guid>c</guid>
    </item>
    <item>
      <title>Keep Me</title>
      <description><![CDATA[<p>This is real content.</p>]]></description>
      <link>https://example.com/keep</link>
      <pubDate>Mon, 09 Mar 2026 00:00:00 GMT</pubDate>
      <guid>keep</guid>
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	result := fetchFeed(context.Background(), server.Client(), model.Feed{URL: server.URL})
	if result.Err != nil {
		t.Fatalf("unexpected fetch error: %v", result.Err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 kept item, got %d", len(result.Items))
	}
	if result.Items[0].Title != "Keep Me" {
		t.Fatalf("unexpected item title: %s", result.Items[0].Title)
	}
}
