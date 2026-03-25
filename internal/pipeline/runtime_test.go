package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

type pipelineHarness struct {
	cfg         config.Config
	db          *store.Store
	server      *httptest.Server
	articleHits atomic.Int32
	aiHits      atomic.Int32

	mu              sync.RWMutex
	feedDescription string
	articleHTML     string
}

func newPipelineHarness(t *testing.T, processors []string) *pipelineHarness {
	t.Helper()

	h := &pipelineHarness{
		feedDescription: "Initial abstract",
		articleHTML:     `<html><head><meta name="description" content="Meta summary"></head><body><article><p>Body paragraph</p></article></body></html>`,
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			h.mu.RLock()
			description := h.feedDescription
			h.mu.RUnlock()
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Demo Feed</title>
    <item>
      <title>Demo Article</title>
      <link>%s/article</link>
      <description>%s</description>
      <pubDate>%s</pubDate>
      <guid>%s/article</guid>
    </item>
  </channel>
</rss>`, server.URL, description, time.Now().UTC().Format(time.RFC1123Z), server.URL)
		case "/article", "/article-fallback", "/article-fetched":
			h.articleHits.Add(1)
			h.mu.RLock()
			body := h.articleHTML
			h.mu.RUnlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, body)
		case "/v1/chat/completions":
			h.aiHits.Add(1)
			body, _ := io.ReadAll(r.Body)
			content := amlScoreResponse()
			if strings.Contains(string(body), "严格合法的 JSON 对象") {
				content = `{"summary":"摘要","key_points":["要点1"],"tags":["标签"],"entities":["AML"],"topics":["文献追踪"],"content_source":"html_body"}`
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{"content": content},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	h.server = server

	tempDir := t.TempDir()
	h.cfg = config.Config{
		StatePath:          filepath.Join(tempDir, "store.db"),
		FeedsJSONPath:      filepath.Join(tempDir, "feeds.json"),
		ContentDir:         filepath.Join(tempDir, "content"),
		DaysWindow:         3,
		MaxFeeds:           10,
		SortBy:             "recommendation",
		OpenAIBaseURL:      server.URL,
		OpenAIAPIKey:       "demo-key",
		OpenAIModel:        "demo-model",
		OpenAIMaxTokens:    1024,
		OpenAITimeout:      time.Second,
		OpenAITemperature:  0.1,
		FetchConcurrency:   1,
		ScoreConcurrency:   1,
		ScoreRateLimit:     1000,
		ContentEnabled:     true,
		ContentConcurrency: 1,
		ContentTimeout:     time.Second,
		ContentMaxBytes:    4096,
		ContentUserAgent:   "test-agent",
		ContentStoreHTML:   false,
		ProcessorsEnabled:  processors,
	}

	if err := writeFeedsJSON(h.cfg.FeedsJSONPath, server.URL+"/feed.xml"); err != nil {
		server.Close()
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.cfg.ContentDir, 0o755); err != nil {
		server.Close()
		t.Fatal(err)
	}

	db, err := store.Open(h.cfg)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	h.db = db

	t.Cleanup(func() {
		_ = h.db.Close()
		h.server.Close()
	})
	return h
}

func writeFeedsJSON(path, feedURL string) error {
	content := fmt.Sprintf(`[{"url":%q,"title":"Demo Feed"}]`, feedURL)
	return os.WriteFile(path, []byte(content), 0o644)
}

func amlScoreResponse() string {
	return "# 4. 分数汇总\n- 研究本身价值小计（60分制）：40\n- AML借鉴价值小计（30分制）：21\n- 表达与可复用性小计（10分制）：7\n- 最终总分：68\n\n# 5. 推荐度\n- 推荐等级：有条件推荐\n\n# 9. 最终结论\n有一定价值。"
}

func (h *pipelineHarness) setFeedDescription(value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.feedDescription = value
}

func (h *pipelineHarness) setArticleHTML(value string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.articleHTML = value
}

func (h *pipelineHarness) seedFeed(t *testing.T) {
	t.Helper()
	if err := h.db.UpsertFeed(model.Feed{URL: h.server.URL + "/feed.xml", Title: "Demo Feed", Status: "active"}); err != nil {
		t.Fatal(err)
	}
}

func capturePipelineOutput(t *testing.T, fn func() error) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = writer
	runErr := fn()
	_ = writer.Close()
	os.Stdout = original

	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("read captured stdout: %v", readErr)
	}
	if runErr != nil {
		t.Fatalf("unexpected error while capturing stdout: %v", runErr)
	}
	return strings.TrimSpace(string(output))
}

func (h *pipelineHarness) currentArticle(now time.Time) model.Article {
	article := model.Article{
		CanonicalLink: model.CanonicalizeLink(h.server.URL + "/article"),
		Title:         "Demo Article",
		Abstract:      h.feedDescriptionValue(),
		FeedURL:       h.server.URL + "/feed.xml",
		Link:          h.server.URL + "/article",
		FirstSeenAt:   now.UTC(),
		LastSeenAt:    now.UTC(),
		ContentHash:   model.HashContent("Demo Article", h.feedDescriptionValue(), h.server.URL+"/article"),
	}
	article.ID = model.BuildArticleID(article.DOI, article.CanonicalLink, article.Title)
	return article
}

func (h *pipelineHarness) feedDescriptionValue() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.feedDescription
}

func (h *pipelineHarness) seedFetchedContent(t *testing.T, article model.Article, now time.Time) {
	t.Helper()
	if _, err := h.db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusFetched,
		SourceType:  "html_body",
		ContentText: "Body paragraph",
		ContentHash: model.HashContent(article.Link, "html_body", "Body paragraph"),
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunDailySkipsUnchangedContentAndProcessors(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected 1 content fetch after first run, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected 1 ai call after first run, got %d", got)
	}

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected unchanged run to skip refetch, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected unchanged run to skip reprocess, got %d", got)
	}
}

func TestRunDailyOutputIncludesStageReasonCounts(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})

	output := capturePipelineOutput(t, func() error {
		return RunDaily(context.Background(), h.cfg, h.db, RunOptions{})
	})

	var payload struct {
		ContentTasks     int            `json:"content_tasks"`
		ContentChanged   int            `json:"content_changed"`
		ContentReasons   map[string]int `json:"content_reasons"`
		ProcessorTasks   int            `json:"processor_tasks"`
		ProcessorReasons map[string]int `json:"processor_reasons"`
		ReportWritten    bool           `json:"report_written"`
		ReportArticles   int            `json:"report_articles"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.ContentTasks != 1 || payload.ContentChanged != 1 || payload.ContentReasons[taskReasonNewArticle] != 1 {
		t.Fatalf("expected daily output to include new_article content reason, got %+v", payload)
	}
	if payload.ProcessorTasks != 1 || payload.ProcessorReasons["generic_digest:"+taskReasonProcessorMissing] != 1 {
		t.Fatalf("expected daily output to include processor missing reason, got %+v", payload)
	}
	if !payload.ReportWritten || payload.ReportArticles != 1 {
		t.Fatalf("expected daily output to include written report info, got %+v", payload)
	}
}

func TestRunDailyDoesNotRetrySkippedContentWhenDisabled(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.cfg.ContentEnabled = false

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected first disabled-content run to process once, got %d", got)
	}
	article := h.db.ArticlesByIDs([]string{h.currentArticle(time.Now().UTC()).ID})
	_ = article

	output := capturePipelineOutput(t, func() error {
		return RunDaily(context.Background(), h.cfg, h.db, RunOptions{})
	})
	var payload struct {
		ContentTasks   int            `json:"content_tasks"`
		ContentReasons map[string]int `json:"content_reasons"`
		Processed      int            `json:"processed"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.ContentTasks != 0 || len(payload.ContentReasons) != 0 || payload.Processed != 0 {
		t.Fatalf("expected second disabled-content run to skip content retry and ai rerun, got %+v", payload)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected disabled skipped content not to trigger second ai rerun, got %d", got)
	}
	content := h.db.ArticleContent(h.currentArticle(time.Now().UTC()).ID)
	if content == nil || content.FetchStatus != model.ContentStatusSkipped {
		t.Fatalf("expected stored content to remain skipped, got %+v", content)
	}
}

func TestRunDailyRetriesSkippedContentAfterReenable(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.cfg.ContentEnabled = false

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected first disabled-content run to process once, got %d", got)
	}

	h.cfg.ContentEnabled = true
	output := capturePipelineOutput(t, func() error {
		return RunDaily(context.Background(), h.cfg, h.db, RunOptions{})
	})

	var payload struct {
		ContentTasks   int            `json:"content_tasks"`
		ContentReasons map[string]int `json:"content_reasons"`
		Processed      int            `json:"processed"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.ContentTasks != 1 || payload.ContentReasons[taskReasonContentRetry] != 1 || payload.Processed != 1 {
		t.Fatalf("expected reenabled run to retry skipped content and rerun processor, got %+v", payload)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected reenabled content fetch to hit article once, got %d", got)
	}
	if got := h.aiHits.Load(); got != 2 {
		t.Fatalf("expected reenabled content fetch to trigger second ai run, got %d", got)
	}
	content := h.db.ArticleContent(h.currentArticle(time.Now().UTC()).ID)
	if content == nil || content.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected skipped content to become fetched after reenable, got %+v", content)
	}
}

func TestRunDailyOutputsStatsWhenNoReportArticles(t *testing.T) {
	h := newPipelineHarness(t, []string{"aml_score"})
	h.setFeedDescription("")
	h.setArticleHTML(`<html><head><title>Empty</title></head><body></body></html>`)

	output := capturePipelineOutput(t, func() error {
		return RunDaily(context.Background(), h.cfg, h.db, RunOptions{})
	})

	var payload struct {
		Fetched        int    `json:"fetched"`
		New            int    `json:"new"`
		ReportDate     string `json:"report_date"`
		ReportWritten  bool   `json:"report_written"`
		ReportArticles int    `json:"report_articles"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.Fetched != 1 || payload.New != 1 || payload.ReportDate == "" || payload.ReportWritten || payload.ReportArticles != 0 {
		t.Fatalf("expected no-report daily stats output, got %+v", payload)
	}
}

func TestRunDailyRefetchesAndReprocessesWhenMetadataChanges(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	h.setFeedDescription("Updated abstract")

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 2 {
		t.Fatalf("expected changed metadata to refetch content, got %d", got)
	}
	if got := h.aiHits.Load(); got != 2 {
		t.Fatalf("expected changed metadata to reprocess ai, got %d", got)
	}

	stats := h.db.Stats()
	if stats.AIResults != 2 {
		t.Fatalf("expected 2 ai result rows after input change, got %+v", stats)
	}
}

func TestRunDailyProcessesFeedItemsWithoutRSSDescriptionWhenHTMLSucceeds(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.setFeedDescription("")

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected empty-description feed item to still fetch html content, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected empty-description feed item to still run ai after html fetch, got %d", got)
	}
	articles, err := h.db.ListArticles(store.ListArticlesOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected article to be ingested even without rss description, got %+v", articles)
	}
}

func TestRunDailySkipsAIWhenNoRSSDescriptionAndNoExtractedHTML(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.setFeedDescription("")
	h.setArticleHTML(`<html><head><title>Empty</title></head><body></body></html>`)

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected html fetch attempt for empty-description feed item, got %d", got)
	}
	if got := h.aiHits.Load(); got != 0 {
		t.Fatalf("expected no ai run when neither rss nor html provides usable text, got %d", got)
	}
	content := h.db.ArticleContent(h.currentArticle(time.Now().UTC()).ID)
	if content == nil || strings.TrimSpace(content.ContentText) != "" {
		t.Fatalf("expected stored content without usable text, got %+v", content)
	}
}

func TestRunDailyBackfillsMissingGenericDigestWithoutRefetch(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)
	if err := h.db.SaveScore(article.ID, model.Score{
		Research: 40, Social: 21, Blood: 7, Recommendation: 68,
		Reason: "existing aml score", Model: "demo-model", ScoredAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 0 {
		t.Fatalf("expected missing processor补跑时不重新抓正文, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected exactly one generic digest run, got %d", got)
	}
	if result := h.db.LatestAIResult(article.ID, "generic_digest"); result == nil {
		t.Fatal("expected generic_digest result to be created")
	}
}

func TestRunDailyBackfillsMissingAMLScoreWithoutRefetch(t *testing.T) {
	h := newPipelineHarness(t, []string{"aml_score"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:   article.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   processorInputHash("generic_digest", article, h.db.ArticleContent(article.ID)),
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"摘要"}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 0 {
		t.Fatalf("expected missing aml_score补跑时不重新抓正文, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected exactly one aml score run, got %d", got)
	}
	stored := h.db.FindArticle(article.ID)
	if stored == nil || stored.LatestScore == nil {
		t.Fatal("expected aml score to be backfilled")
	}
}

func TestRunDailyRetriesFallbackContent(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusRSSFallback,
		SourceType:  "rss_description",
		ContentText: article.Abstract,
		ContentHash: model.HashContent(article.Link, "rss_description", article.Abstract),
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunDaily(context.Background(), h.cfg, h.db, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected rss_fallback content to be retried, got %d", got)
	}
	content := h.db.ArticleContent(article.ID)
	if content == nil || content.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected retried content to become fetched, got %+v", content)
	}
}

func TestRunBackfillSkipsUnchangedContentAndProcessors(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})

	if err := RunBackfill(context.Background(), h.cfg, h.db, 3, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := RunBackfill(context.Background(), h.cfg, h.db, 3, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected unchanged backfill rerun to skip refetch, got %d", got)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected unchanged backfill rerun to skip reprocess, got %d", got)
	}
}

func TestRunBackfillOutputIncludesStageReasonCounts(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})

	output := capturePipelineOutput(t, func() error {
		return RunBackfill(context.Background(), h.cfg, h.db, 3, RunOptions{})
	})

	var payload struct {
		ContentTasks     int            `json:"content_tasks"`
		ContentChanged   int            `json:"content_changed"`
		ContentReasons   map[string]int `json:"content_reasons"`
		ProcessorTasks   int            `json:"processor_tasks"`
		ProcessorReasons map[string]int `json:"processor_reasons"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.ContentTasks != 1 || payload.ContentChanged != 1 || payload.ContentReasons[taskReasonNewArticle] != 1 {
		t.Fatalf("expected backfill output to include new_article content reason, got %+v", payload)
	}
	if payload.ProcessorTasks != 1 || payload.ProcessorReasons["generic_digest:"+taskReasonProcessorMissing] != 1 {
		t.Fatalf("expected backfill output to include processor missing reason, got %+v", payload)
	}
}

func TestRunProcessorsFiltersMissingStatus(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)

	if err := RunProcessors(context.Background(), h.cfg, h.db, 50, []string{"generic_digest"}, "missing", false); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected missing filter to process one article, got %d", got)
	}

	if err := RunProcessors(context.Background(), h.cfg, h.db, 50, []string{"generic_digest"}, "missing", false); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected second missing run to skip processed article, got %d", got)
	}
}

func TestRunProcessorsForceFiltersProcessedStatus(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:   article.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   processorInputHash("generic_digest", article, h.db.ArticleContent(article.ID)),
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"摘要"}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunProcessors(context.Background(), h.cfg, h.db, 50, []string{"generic_digest"}, "processed", true); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected force+processed filter to rerun one processed result, got %d", got)
	}
}

func TestRunProcessorsStatusFilterAppliesBeforeLimit(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()

	missingArticle := h.currentArticle(now)
	missingArticle.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fallback")
	missingArticle.Link = h.server.URL + "/article-fallback"
	missingArticle.ContentHash = model.HashContent(missingArticle.Title, missingArticle.Abstract, missingArticle.Link)
	missingArticle.FirstSeenAt = now.Add(-2 * time.Hour)
	missingArticle.LastSeenAt = now.Add(-2 * time.Hour)
	missingArticle.ID = model.BuildArticleID(missingArticle.DOI, missingArticle.CanonicalLink, missingArticle.Title)
	if _, err := h.db.UpsertArticle(missingArticle); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, missingArticle, now.Add(-2*time.Hour))

	processedArticle := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(processedArticle); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, processedArticle, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:   processedArticle.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   processorInputHash("generic_digest", processedArticle, h.db.ArticleContent(processedArticle.ID)),
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"摘要"}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunProcessors(context.Background(), h.cfg, h.db, 1, []string{"generic_digest"}, "missing", false); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected missing processor filter with limit=1 to process matching article, got %d", got)
	}
}

func TestRunProcessorsStatusFilterAcrossProcessorsUsesUnifiedLimitOrdering(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest", "aml_score"})
	h.seedFeed(t)
	now := time.Now().UTC()

	olderGenericMissing := h.currentArticle(now)
	olderGenericMissing.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fallback")
	olderGenericMissing.Link = h.server.URL + "/article-fallback"
	olderGenericMissing.ContentHash = model.HashContent(olderGenericMissing.Title, olderGenericMissing.Abstract, olderGenericMissing.Link)
	olderGenericMissing.FirstSeenAt = now.Add(-2 * time.Hour)
	olderGenericMissing.LastSeenAt = now.Add(-2 * time.Hour)
	olderGenericMissing.ID = model.BuildArticleID(olderGenericMissing.DOI, olderGenericMissing.CanonicalLink, olderGenericMissing.Title)
	if _, err := h.db.UpsertArticle(olderGenericMissing); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, olderGenericMissing, now.Add(-2*time.Hour))

	newerAMLMissing := h.currentArticle(now)
	newerAMLMissing.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fetched")
	newerAMLMissing.Link = h.server.URL + "/article-fetched"
	newerAMLMissing.ContentHash = model.HashContent(newerAMLMissing.Title, newerAMLMissing.Abstract, newerAMLMissing.Link)
	newerAMLMissing.ID = model.BuildArticleID(newerAMLMissing.DOI, newerAMLMissing.CanonicalLink, newerAMLMissing.Title)
	if _, err := h.db.UpsertArticle(newerAMLMissing); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, newerAMLMissing, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:   newerAMLMissing.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   processorInputHash("generic_digest", newerAMLMissing, h.db.ArticleContent(newerAMLMissing.ID)),
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"摘要"}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := RunProcessors(context.Background(), h.cfg, h.db, 1, []string{"generic_digest", "aml_score"}, "missing", false); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected unified processor status limit to process one newest missing task, got %d", got)
	}
	if result := h.db.LatestAIResult(newerAMLMissing.ID, "aml_score"); result == nil || result.Status != model.ProcessorStatusProcessed {
		t.Fatalf("expected newest aml_score-missing article to be selected, got %+v", result)
	}
	if result := h.db.LatestAIResult(olderGenericMissing.ID, "generic_digest"); result != nil {
		t.Fatalf("expected older generic_digest-missing article not selected under limit=1, got %+v", result)
	}
}

func TestRunProcessorsFailedFilterIncludesChangedInput(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:    article.ID,
		Processor:    "generic_digest",
		InputHash:    "old-input-hash",
		Status:       model.ProcessorStatusFailed,
		ProcessedAt:  now.Add(-1 * time.Minute),
		ErrorMessage: "temporary upstream error",
	}); err != nil {
		t.Fatal(err)
	}

	article.Abstract = "Updated abstract after failure"
	article.ContentHash = model.HashContent(article.Title, article.Abstract, article.Link)
	article.LastSeenAt = now.Add(1 * time.Minute)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}

	if err := RunProcessors(context.Background(), h.cfg, h.db, 50, []string{"generic_digest"}, "failed", false); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected failed status filter to rerun changed-input failure, got %d", got)
	}
}

func TestRunProcessorsOutputIncludesReasonCounts(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)

	output := capturePipelineOutput(t, func() error {
		return RunProcessors(context.Background(), h.cfg, h.db, 50, []string{"generic_digest"}, "missing", false)
	})

	var payload struct {
		Tasks   int            `json:"tasks"`
		Reasons map[string]int `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.Tasks != 1 || payload.Reasons["generic_digest:processor_missing"] != 1 {
		t.Fatalf("expected generic_digest missing reason in output, got %+v", payload)
	}
}

func TestRunFetchContentFiltersRSSFallbackStatus(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()

	fallbackArticle := h.currentArticle(now)
	fallbackArticle.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fallback")
	fallbackArticle.Link = h.server.URL + "/article-fallback"
	fallbackArticle.ContentHash = model.HashContent(fallbackArticle.Title, fallbackArticle.Abstract, fallbackArticle.Link)
	fallbackArticle.ID = model.BuildArticleID(fallbackArticle.DOI, fallbackArticle.CanonicalLink, fallbackArticle.Title)
	if _, err := h.db.UpsertArticle(fallbackArticle); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   fallbackArticle.ID,
		ResolvedURL: fallbackArticle.Link,
		FetchStatus: model.ContentStatusRSSFallback,
		SourceType:  "rss_description",
		ContentText: fallbackArticle.Abstract,
		ContentHash: model.HashContent(fallbackArticle.Link, "rss_description", fallbackArticle.Abstract),
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}

	fetchedArticle := h.currentArticle(now)
	fetchedArticle.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fetched")
	fetchedArticle.Link = h.server.URL + "/article-fetched"
	fetchedArticle.ContentHash = model.HashContent(fetchedArticle.Title, fetchedArticle.Abstract, fetchedArticle.Link)
	fetchedArticle.ID = model.BuildArticleID(fetchedArticle.DOI, fetchedArticle.CanonicalLink, fetchedArticle.Title)
	if _, err := h.db.UpsertArticle(fetchedArticle); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, fetchedArticle, now)

	if err := RunFetchContent(context.Background(), h.cfg, h.db, 50, model.ContentStatusRSSFallback, false); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected rss_fallback filter to fetch one article, got %d", got)
	}
	content := h.db.ArticleContent(fallbackArticle.ID)
	if content == nil || content.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected fallback content to be refreshed, got %+v", content)
	}
}

func TestRunFetchContentForceFiltersFetchedStatus(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()

	fetchedArticle := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(fetchedArticle); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, fetchedArticle, now)

	if err := RunFetchContent(context.Background(), h.cfg, h.db, 50, model.ContentStatusFetched, true); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected force+fetched filter to refetch one article, got %d", got)
	}
}

func TestRunFetchContentStatusFilterAppliesBeforeLimit(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()

	fallbackArticle := h.currentArticle(now)
	fallbackArticle.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fallback")
	fallbackArticle.Link = h.server.URL + "/article-fallback"
	fallbackArticle.ContentHash = model.HashContent(fallbackArticle.Title, fallbackArticle.Abstract, fallbackArticle.Link)
	fallbackArticle.FirstSeenAt = now.Add(-2 * time.Hour)
	fallbackArticle.LastSeenAt = now.Add(-2 * time.Hour)
	fallbackArticle.ID = model.BuildArticleID(fallbackArticle.DOI, fallbackArticle.CanonicalLink, fallbackArticle.Title)
	if _, err := h.db.UpsertArticle(fallbackArticle); err != nil {
		t.Fatal(err)
	}
	fallbackFetchedAt := now.Add(-90 * time.Minute)
	if _, err := h.db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   fallbackArticle.ID,
		ResolvedURL: fallbackArticle.Link,
		FetchStatus: model.ContentStatusRSSFallback,
		SourceType:  "rss_description",
		ContentText: fallbackArticle.Abstract,
		ContentHash: model.HashContent(fallbackArticle.Link, "rss_description", fallbackArticle.Abstract),
		FetchedAt:   &fallbackFetchedAt,
	}); err != nil {
		t.Fatal(err)
	}

	fetchedArticle := h.currentArticle(now)
	fetchedArticle.CanonicalLink = model.CanonicalizeLink(h.server.URL + "/article-fetched")
	fetchedArticle.Link = h.server.URL + "/article-fetched"
	fetchedArticle.ContentHash = model.HashContent(fetchedArticle.Title, fetchedArticle.Abstract, fetchedArticle.Link)
	fetchedArticle.ID = model.BuildArticleID(fetchedArticle.DOI, fetchedArticle.CanonicalLink, fetchedArticle.Title)
	if _, err := h.db.UpsertArticle(fetchedArticle); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, fetchedArticle, now)

	if err := RunFetchContent(context.Background(), h.cfg, h.db, 1, model.ContentStatusRSSFallback, false); err != nil {
		t.Fatal(err)
	}
	if got := h.articleHits.Load(); got != 1 {
		t.Fatalf("expected rss_fallback status filter with limit=1 to still fetch one matching article, got %d", got)
	}
	content := h.db.ArticleContent(fallbackArticle.ID)
	if content == nil || content.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected fallback article to be refreshed despite newer fetched article, got %+v", content)
	}
}

func TestRunFetchContentOutputIncludesReasonCounts(t *testing.T) {
	h := newPipelineHarness(t, []string{"generic_digest"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusRSSFallback,
		SourceType:  "rss_description",
		ContentText: article.Abstract,
		ContentHash: model.HashContent(article.Link, "rss_description", article.Abstract),
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}

	output := capturePipelineOutput(t, func() error {
		return RunFetchContent(context.Background(), h.cfg, h.db, 50, model.ContentStatusRSSFallback, false)
	})

	var payload struct {
		Tasks   int            `json:"tasks"`
		Reasons map[string]int `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.Tasks != 1 || payload.Reasons[taskReasonContentRetry] != 1 {
		t.Fatalf("expected content retry reason in output, got %+v", payload)
	}
}

func TestRetryFailedScoresUsesAMLProcessorFailures(t *testing.T) {
	h := newPipelineHarness(t, []string{"aml_score"})
	h.seedFeed(t)
	now := time.Now().UTC()
	article := h.currentArticle(now)
	if _, err := h.db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	h.seedFetchedContent(t, article, now)
	if err := h.db.SaveAIResult(model.ProcessorResult{
		ArticleID:    article.ID,
		Processor:    "aml_score",
		InputHash:    processorInputHash("aml_score", article, h.db.ArticleContent(article.ID)),
		Status:       model.ProcessorStatusFailed,
		ProcessedAt:  now,
		ErrorMessage: "temporary upstream error",
	}); err != nil {
		t.Fatal(err)
	}

	if err := RetryFailedScores(context.Background(), h.cfg, h.db, 50); err != nil {
		t.Fatal(err)
	}
	if got := h.aiHits.Load(); got != 1 {
		t.Fatalf("expected retry_failed_scores to rerun aml_score from ai_results failure, got %d", got)
	}
	stored := h.db.FindArticle(article.ID)
	if stored == nil || stored.LatestScore == nil {
		t.Fatal("expected aml score to be persisted after retry")
	}
}
