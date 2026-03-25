package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zhuimi/internal/config"
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

	result := fetchFeed(context.Background(), server.Client(), model.Feed{URL: server.URL}, config.Config{FilterTitleOnly: true})
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

func TestFetchFeedParsesAtomEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Feed</title>
  <entry>
    <title>Skip Me</title>
    <id>tag:example.com,2026:skip</id>
    <updated>2026-03-09T00:00:00Z</updated>
    <summary>Skip Me</summary>
    <link href="https://example.com/skip" rel="alternate"></link>
  </entry>
  <entry>
    <title>Keep Atom</title>
    <id>tag:example.com,2026:keep</id>
    <published>2026-03-10T12:34:56Z</published>
    <summary type="html"><![CDATA[<p>This is <strong>Atom</strong> content.</p>]]></summary>
    <link href="https://example.com/self" rel="self"></link>
    <link href="https://example.com/keep" rel="alternate"></link>
  </entry>
  <entry>
    <title>Content Fallback</title>
    <id>doi:10.1000/example.atom</id>
    <updated>2026-03-11T08:00:00Z</updated>
    <content type="html"><![CDATA[<div>Content body</div>]]></content>
    <link href="https://doi.org/10.1000/example.atom"></link>
  </entry>
</feed>`))
	}))
	defer server.Close()

	result := fetchFeed(context.Background(), server.Client(), model.Feed{URL: server.URL}, config.Config{FilterTitleOnly: true})
	if result.Err != nil {
		t.Fatalf("unexpected fetch error: %v", result.Err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 kept items, got %d", len(result.Items))
	}

	first := result.Items[0]
	if first.Title != "Keep Atom" {
		t.Fatalf("unexpected first item title: %s", first.Title)
	}
	if first.Link != "https://example.com/keep" {
		t.Fatalf("expected alternate link, got %q", first.Link)
	}
	if first.PubDate == nil || first.PubDate.Format(time.RFC3339) != "2026-03-10T12:34:56Z" {
		t.Fatalf("unexpected first item date: %v", first.PubDate)
	}

	second := result.Items[1]
	if second.Description != "<![CDATA[<div>Content body</div>]]>" {
		t.Fatalf("unexpected content fallback description: %q", second.Description)
	}
	if second.DOI != "10.1000/example.atom" {
		t.Fatalf("expected DOI extracted from atom entry, got %q", second.DOI)
	}
}

func TestFetchFeedParsesRDFItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rdf+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel rdf:about="https://example.com/feed">
    <title>Example RDF Feed</title>
    <link>https://example.com/</link>
    <description>Example RDF channel</description>
  </channel>
  <item rdf:about="https://example.com/skip">
    <title>Skip RDF</title>
    <link>https://example.com/skip</link>
    <description>Skip RDF</description>
    <dc:date>2026-03-12T00:00:00Z</dc:date>
  </item>
  <item rdf:about="https://example.com/keep">
    <title>Keep RDF</title>
    <link>https://example.com/keep</link>
    <description><![CDATA[<p>RDF description</p>]]></description>
    <dc:date>2026-03-13T08:09:10Z</dc:date>
  </item>
  <item rdf:about="https://doi.org/10.2000/rdf.entry">
    <title>RDF Content Fallback</title>
    <link>https://doi.org/10.2000/rdf.entry</link>
    <content:encoded><![CDATA[<div>RDF body</div>]]></content:encoded>
    <dc:date>2026-03-14T11:12:13Z</dc:date>
  </item>
</rdf:RDF>`))
	}))
	defer server.Close()

	result := fetchFeed(context.Background(), server.Client(), model.Feed{URL: server.URL}, config.Config{FilterTitleOnly: true})
	if result.Err != nil {
		t.Fatalf("unexpected fetch error: %v", result.Err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 kept rdf items, got %d", len(result.Items))
	}

	first := result.Items[0]
	if first.Title != "Keep RDF" {
		t.Fatalf("unexpected first rdf title: %s", first.Title)
	}
	if first.PubDate == nil || first.PubDate.Format(time.RFC3339) != "2026-03-13T08:09:10Z" {
		t.Fatalf("unexpected first rdf date: %v", first.PubDate)
	}

	second := result.Items[1]
	if second.Description != "<div>RDF body</div>" {
		t.Fatalf("unexpected rdf content fallback description: %q", second.Description)
	}
	if second.DOI != "10.2000/rdf.entry" {
		t.Fatalf("expected DOI extracted from rdf item, got %q", second.DOI)
	}
}

func TestFetchFeedCanKeepTitleOnlyItemsWhenConfigured(t *testing.T) {
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
  </channel>
</rss>`))
	}))
	defer server.Close()

	result := fetchFeed(context.Background(), server.Client(), model.Feed{URL: server.URL}, config.Config{FilterTitleOnly: false})
	if result.Err != nil {
		t.Fatalf("unexpected fetch error: %v", result.Err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 title-only items kept, got %d", len(result.Items))
	}
}

func TestShouldFilterTitleOnlyForFeed(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.Config
		feed     model.Feed
		expected bool
	}{
		{
			name:     "global filter disabled",
			cfg:      config.Config{FilterTitleOnly: false},
			feed:     model.Feed{URL: "https://example.com/feed.xml"},
			expected: false,
		},
		{
			name:     "global filter enabled without allow list",
			cfg:      config.Config{FilterTitleOnly: true},
			feed:     model.Feed{URL: "https://example.com/feed.xml"},
			expected: true,
		},
		{
			name:     "domain allow list matches host",
			cfg:      config.Config{FilterTitleOnly: true, AllowTitleOnlyFor: []string{"example.com"}},
			feed:     model.Feed{URL: "https://news.example.com/feed.xml"},
			expected: false,
		},
		{
			name:     "exact feed url matches",
			cfg:      config.Config{FilterTitleOnly: true, AllowTitleOnlyFor: []string{"https://example.org/feed.xml"}},
			feed:     model.Feed{URL: "https://example.org/feed.xml"},
			expected: false,
		},
		{
			name:     "url prefix wildcard matches",
			cfg:      config.Config{FilterTitleOnly: true, AllowTitleOnlyFor: []string{"https://feeds.example.net/path/*"}},
			feed:     model.Feed{URL: "https://feeds.example.net/path/child/feed.xml"},
			expected: false,
		},
		{
			name:     "non matching pattern keeps filter on",
			cfg:      config.Config{FilterTitleOnly: true, AllowTitleOnlyFor: []string{"other.example"}},
			feed:     model.Feed{URL: "https://example.com/feed.xml"},
			expected: true,
		},
		{
			name:     "persisted allow_title_only overrides config",
			cfg:      config.Config{FilterTitleOnly: true},
			feed:     model.Feed{URL: "https://example.com/feed.xml", AllowTitleOnly: boolPtr(true)},
			expected: false,
		},
		{
			name:     "persisted disallow_title_only overrides global off",
			cfg:      config.Config{FilterTitleOnly: false},
			feed:     model.Feed{URL: "https://example.com/feed.xml", AllowTitleOnly: boolPtr(false)},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFilterTitleOnlyForFeed(tt.feed, tt.cfg); got != tt.expected {
				t.Fatalf("expected shouldFilterTitleOnlyForFeed=%v, got %v", tt.expected, got)
			}
		})
	}
}

func boolPtr(value bool) *bool {
	return &value
}
