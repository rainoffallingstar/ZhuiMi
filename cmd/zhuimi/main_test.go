package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/model"
)

func TestFilterFeeds(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", Status: "ok"},
		{URL: "https://example.com/2", Status: "inactive"},
		{URL: "https://example.com/3", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", boolPtr(true))
	if len(filtered) != 2 {
		t.Fatalf("expected 2 enabled feeds, got %d", len(filtered))
	}
	for _, item := range filtered {
		if item.Status == "inactive" {
			t.Fatalf("expected inactive feed filtered out, got %+v", item)
		}
	}

	filtered = filterFeeds(feeds, "error", nil)
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected error feed only, got %+v", filtered)
	}
}

func TestBuildFeedStatusSummary(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", Status: "ok"},
		{URL: "https://example.com/2", Status: "inactive"},
		{URL: "https://example.com/3", Status: "error"},
		{URL: "https://example.com/4", Status: ""},
	}

	summary := buildFeedStatusSummary(feeds)
	if summary.Total != 4 {
		t.Fatalf("expected total=4, got %d", summary.Total)
	}
	if summary.Enabled != 3 {
		t.Fatalf("expected enabled=3, got %d", summary.Enabled)
	}
	if summary.Inactive != 1 {
		t.Fatalf("expected inactive=1, got %d", summary.Inactive)
	}
	if summary.ByStatus["ok"] != 1 || summary.ByStatus["inactive"] != 1 || summary.ByStatus["error"] != 1 || summary.ByStatus["unknown"] != 1 {
		t.Fatalf("unexpected by_status: %+v", summary.ByStatus)
	}
}

func TestParseRunOptions(t *testing.T) {
	opts, rest, err := parseRunOptions([]string{"--skip-scoring", "--pdf", "--days=7"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.SkipScoring || !opts.CompilePDF {
		t.Fatalf("unexpected run options: %+v", opts)
	}
	if !reflect.DeepEqual(rest, []string{"--days=7"}) {
		t.Fatalf("unexpected remaining args: %+v", rest)
	}
}

func TestBuildArticleReviewSummary(t *testing.T) {
	article := model.Article{
		Reason: "推荐等级：有条件推荐；研究问题较重要，但证据链仍不完整。；置信度：低",
		LatestScore: &model.Score{
			Research:       41,
			Social:         22,
			Blood:          7,
			Recommendation: 66,
		},
	}

	review := buildArticleReviewSummary(article)
	if review == nil {
		t.Fatal("expected review summary")
	}
	if review.TotalScore != 66 || review.AMLValue != 22 || review.ReusabilityValue != 7 {
		t.Fatalf("unexpected review values: %+v", review)
	}
	if review.RecommendationLevel != "有条件推荐" {
		t.Fatalf("unexpected recommendation level: %q", review.RecommendationLevel)
	}
	if review.Summary != "研究问题较重要，但证据链仍不完整。" {
		t.Fatalf("unexpected review summary: %q", review.Summary)
	}
}

func TestExtractRecommendationLevelFallback(t *testing.T) {
	if got := extractRecommendationLevel("", 88); got != "强烈推荐" {
		t.Fatalf("unexpected fallback level: %q", got)
	}
	if got := extractRecommendationLevel("", 52); got != "一般" {
		t.Fatalf("unexpected fallback level: %q", got)
	}
}

func TestFilterArticleListOutput(t *testing.T) {
	items := []articleListItem{
		{Review: &articleReviewSummary{TotalScore: 88, AMLValue: 25, RecommendationLevel: "强烈推荐"}},
		{Review: &articleReviewSummary{TotalScore: 76, AMLValue: 18, RecommendationLevel: "推荐"}},
		{Review: &articleReviewSummary{TotalScore: 66, AMLValue: 22, RecommendationLevel: "有条件推荐"}},
		{},
	}

	filtered := filterArticleListOutput(items, articleListFilters{MinTotal: 75})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items after min-total filter, got %d", len(filtered))
	}

	filtered = filterArticleListOutput(items, articleListFilters{MinAML: 20, Level: "有条件推荐"})
	if len(filtered) != 1 || filtered[0].Review.RecommendationLevel != "有条件推荐" {
		t.Fatalf("unexpected filtered items: %+v", filtered)
	}
}

func TestHasArticleListFilters(t *testing.T) {
	if hasArticleListFilters(articleListFilters{}) {
		t.Fatal("expected empty filters to be false")
	}
	if !hasArticleListFilters(articleListFilters{MinTotal: 1}) {
		t.Fatal("expected min-total filter to be detected")
	}
}

func TestPrintArticleTopTable(t *testing.T) {
	items := []articleTopItem{{
		Rank:                1,
		Title:               "A very important AML paper",
		PublishedDate:       "2026-03-09",
		TotalScore:          88,
		AMLValue:            24,
		RecommendationLevel: "强烈推荐",
		Summary:             "研究设计扎实，值得重点跟踪。",
	}}
	var buf bytes.Buffer
	if err := printArticleTopTable(&buf, items); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{"Rank", "Total", "AML", "强烈推荐", "A very important AML paper", "研究设计扎实"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in table output, got %s", want, output)
		}
	}
}

func TestBuildArticleTopOutput(t *testing.T) {
	published1 := mustDate("2026-03-09")
	published2 := mustDate("2026-03-08")
	items := []articleListItem{
		{Article: model.Article{ID: "a", Title: "A", PublishedAt: &published2}, Review: &articleReviewSummary{TotalScore: 76, AMLValue: 20, ResearchValue: 40, ReusabilityValue: 7, RecommendationLevel: "推荐", Summary: "A-summary"}},
		{Article: model.Article{ID: "b", Title: "B", PublishedAt: &published1}, Review: &articleReviewSummary{TotalScore: 88, AMLValue: 24, ResearchValue: 50, ReusabilityValue: 8, RecommendationLevel: "强烈推荐", Summary: "B-summary"}},
		{Article: model.Article{ID: "c", Title: "C", PublishedAt: &published1}, Review: &articleReviewSummary{TotalScore: 88, AMLValue: 22, ResearchValue: 51, ReusabilityValue: 9, RecommendationLevel: "强烈推荐", Summary: "C-summary"}},
	}

	sortArticleTopItems(items)
	top := buildArticleTopOutput(items, 2)
	if len(top) != 2 {
		t.Fatalf("expected top 2 items, got %d", len(top))
	}
	if top[0].Title != "B" || top[0].Rank != 1 {
		t.Fatalf("unexpected first top item: %+v", top[0])
	}
	if top[1].Title != "C" || top[1].Rank != 2 {
		t.Fatalf("unexpected second top item: %+v", top[1])
	}
}

func mustDate(value string) time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func boolPtr(value bool) *bool {
	return &value
}
