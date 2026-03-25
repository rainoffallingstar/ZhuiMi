package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

func TestFilterFeeds(t *testing.T) {
	checked1 := mustDateTime("2026-03-10T09:00:00Z")
	checked2 := mustDateTime("2026-03-11T09:00:00Z")
	success1 := mustDateTime("2026-03-09T08:00:00Z")
	success2 := mustDateTime("2026-03-12T08:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", Title: "Nature Feed", SourceFile: "nature.opml", CheckedAt: checked2, SuccessAt: success1, Status: "ok"},
		{URL: "https://sub.example.com/2", SourceFile: "pubmed.opml", CheckedAt: checked1, Status: "inactive", LastError: "http 500", AllowTitleOnly: boolPtr(true)},
		{URL: "https://example.com/3", Title: "AML Updates", SourceFile: "pubmed.opml", SuccessAt: success2, Status: "error", LastError: "parse failed", AllowTitleOnly: boolPtr(false)},
		{URL: "https://other.com/4", Title: "Other Feed", SourceFile: "journals.opml", Status: "active", AllowTitleOnly: boolPtr(true)},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, boolPtr(true), feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 3 {
		t.Fatalf("expected 3 enabled feeds, got %d", len(filtered))
	}
	for _, item := range filtered {
		if item.Status == "inactive" {
			t.Fatalf("expected inactive feed filtered out, got %+v", item)
		}
	}

	filtered = filterFeeds(feeds, "error", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected error feed only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{set: true, value: boolPtr(true)}, feedTimeMissingFilter{})
	if len(filtered) != 2 || filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected allow_title_only=true feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{set: true, value: boolPtr(false)}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected allow_title_only=false feed only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{set: true, value: nil}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/1" {
		t.Fatalf("expected allow_title_only=inherit feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "example.com", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 3 || filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://sub.example.com/2" {
		t.Fatalf("expected example.com domain feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "pubmed", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected source filter matches only pubmed feeds, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "nature", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/1" {
		t.Fatalf("expected title query match only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "OTHER.COM", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://other.com/4" {
		t.Fatalf("expected url query match only, got %+v", filtered)
	}

	filtered = filterFeedsWithErrorQuery(feeds, "", "", "", "", "http", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://sub.example.com/2" {
		t.Fatalf("expected error query match only, got %+v", filtered)
	}

	filtered = filterFeedsWithErrorQuery(feeds, "", "", "", "example.com", "parse", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected combined query and error query match only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "title", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://sub.example.com/2" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://example.com/1" {
		t.Fatalf("expected title sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "status", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://sub.example.com/2" {
		t.Fatalf("expected status sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "enabled", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://sub.example.com/2" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://example.com/3" || filtered[3].URL != "https://other.com/4" {
		t.Fatalf("expected enabled sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "title-only", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://other.com/4" || filtered[3].URL != "https://sub.example.com/2" {
		t.Fatalf("expected title-only sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "has-error", true, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://sub.example.com/2" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://other.com/4" || filtered[3].URL != "https://example.com/1" {
		t.Fatalf("expected has-error sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "checked-missing", true, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://sub.example.com/2" || filtered[3].URL != "https://example.com/1" {
		t.Fatalf("expected checked-missing sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "success-missing", true, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://sub.example.com/2" || filtered[1].URL != "https://other.com/4" || filtered[2].URL != "https://example.com/3" || filtered[3].URL != "https://example.com/1" {
		t.Fatalf("expected success-missing sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "title", true, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://example.com/3" {
		t.Fatalf("expected reverse title sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "source", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://example.com/3" {
		t.Fatalf("expected source sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "domain", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://example.com/3" || filtered[2].URL != "https://other.com/4" || filtered[3].URL != "https://sub.example.com/2" {
		t.Fatalf("expected domain sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "checked", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://other.com/4" || filtered[2].URL != "https://sub.example.com/2" || filtered[3].URL != "https://example.com/1" {
		t.Fatalf("expected checked sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "last-error", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://other.com/4" || filtered[2].URL != "https://sub.example.com/2" || filtered[3].URL != "https://example.com/3" {
		t.Fatalf("expected last-error sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "success", true, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://sub.example.com/2" || filtered[3].URL != "https://other.com/4" {
		t.Fatalf("expected reverse success sort ordering, got %+v", filtered)
	}

	filtered = filterFeeds([]model.Feed{
		{URL: "https://example.com/1", SourceFile: "beta.opml", Status: "ok"},
		{URL: "https://example.com/2", Status: ""},
		{URL: "https://example.com/3", SourceFile: "", Status: "inactive"},
	}, "", "", "", "", "source", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://example.com/2" || filtered[2].URL != "https://example.com/3" {
		t.Fatalf("expected source sort to use normalized source names, got %+v", filtered)
	}

	filtered = filterFeeds([]model.Feed{
		{URL: "https://example.com/1", Status: "ok"},
		{URL: "https://example.com/2", Status: ""},
		{URL: "https://example.com/3", Status: "inactive"},
	}, "", "", "", "", "status", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://example.com/1" || filtered[2].URL != "https://example.com/2" {
		t.Fatalf("expected status sort to use normalized status labels, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checked: boolPtr(true)})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://other.com/4" {
		t.Fatalf("expected checked-missing=true feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checkedBefore: timePtr(mustDate("2026-03-11"))})
	if len(filtered) != 1 || filtered[0].URL != "https://sub.example.com/2" {
		t.Fatalf("expected checked-before filter to match only sub.example.com/2, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checkedAfter: timePtr(mustDate("2026-03-10"))})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected checked-after filter to match example.com/1 and sub.example.com/2, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{lastError: boolPtr(true)})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://other.com/4" {
		t.Fatalf("expected error-missing=true feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{lastError: boolPtr(false)})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/3" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected error-missing=false feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checked: boolPtr(false)})
	if len(filtered) != 2 || filtered[0].URL != "https://example.com/1" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected checked-missing=false feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{success: boolPtr(true)})
	if len(filtered) != 2 || filtered[0].URL != "https://other.com/4" || filtered[1].URL != "https://sub.example.com/2" {
		t.Fatalf("expected success-missing=true feeds only, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{successBefore: timePtr(mustDate("2026-03-10"))})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/1" {
		t.Fatalf("expected success-before filter to match only example.com/1, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{successAfter: timePtr(mustDate("2026-03-10"))})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected success-after filter to match only example.com/3, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checked: boolPtr(true), success: boolPtr(false)})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/3" {
		t.Fatalf("expected combined missing-time filters to match only example.com/3, got %+v", filtered)
	}

	filtered = filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{lastError: boolPtr(false), success: boolPtr(true)})
	if len(filtered) != 1 || filtered[0].URL != "https://sub.example.com/2" {
		t.Fatalf("expected combined error/success missing filters to match only sub.example.com/2, got %+v", filtered)
	}
}

func TestFilterFeedsSupportsUnknownStatus(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", Status: "ok"},
		{URL: "https://example.com/2", Status: ""},
	}

	filtered := filterFeeds(feeds, "unknown", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/2" {
		t.Fatalf("expected unknown status filter to match empty status feed, got %+v", filtered)
	}
}

func TestLimitFeeds(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1"},
		{URL: "https://example.com/2"},
		{URL: "https://example.com/3"},
	}

	limited := limitFeeds(feeds, 2)
	if len(limited) != 2 || limited[0].URL != "https://example.com/1" || limited[1].URL != "https://example.com/2" {
		t.Fatalf("expected first two feeds only, got %+v", limited)
	}

	if got := limitFeeds(feeds, 0); len(got) != 3 {
		t.Fatalf("expected zero limit to keep all feeds, got %+v", got)
	}

	if got := limitFeeds(feeds, -1); len(got) != 3 {
		t.Fatalf("expected negative limit to keep all feeds, got %+v", got)
	}
}

func TestBuildFeedStatusSummary(t *testing.T) {
	checkedPubmed1 := mustDateTime("2026-03-09T08:00:00Z")
	checkedPubmed2 := mustDateTime("2026-03-10T09:00:00Z")
	checkedNature := mustDateTime("2026-03-08T07:00:00Z")
	successPubmed := mustDateTime("2026-03-09T08:30:00Z")
	successNature := mustDateTime("2026-03-11T10:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok", AllowTitleOnly: boolPtr(true), CheckedAt: checkedPubmed1, SuccessAt: successPubmed},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive", CheckedAt: checkedPubmed2},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error", AllowTitleOnly: boolPtr(false), CheckedAt: checkedNature, SuccessAt: successNature},
		{URL: "https://example.com/4", Status: ""},
	}

	summary := buildFeedStatusSummary(feeds)
	if summary.Total != 4 {
		t.Fatalf("expected total=4, got %d", summary.Total)
	}
	if summary.SourceCount != 3 {
		t.Fatalf("expected source_count=3, got %d", summary.SourceCount)
	}
	if summary.Enabled != 3 {
		t.Fatalf("expected enabled=3, got %d", summary.Enabled)
	}
	if summary.Inactive != 1 {
		t.Fatalf("expected inactive=1, got %d", summary.Inactive)
	}
	if summary.CheckedMissing != 1 || summary.SuccessMissing != 2 || summary.ErrorPresent != 0 {
		t.Fatalf("unexpected summary issue counts: %+v", summary)
	}
	if !summary.LatestCheckedAt.Equal(checkedPubmed2) || !summary.LatestSuccessAt.Equal(successNature) {
		t.Fatalf("unexpected summary latest timestamps: %+v", summary)
	}
	if summary.ErrorRate != 0.25 || summary.ErrorInfoRate != 0 || summary.CheckedRate != 0.25 || summary.SuccessRate != 0.5 {
		t.Fatalf("unexpected summary issue rates: %+v", summary)
	}
	if summary.ByStatus["ok"] != 1 || summary.ByStatus["inactive"] != 1 || summary.ByStatus["error"] != 1 || summary.ByStatus["unknown"] != 1 {
		t.Fatalf("unexpected by_status: %+v", summary.ByStatus)
	}
	if summary.ByTitleOnly["true"] != 1 || summary.ByTitleOnly["false"] != 1 || summary.ByTitleOnly["inherit"] != 2 {
		t.Fatalf("unexpected by_title_only: %+v", summary.ByTitleOnly)
	}
	if summary.BySource["pubmed.opml"].Total != 2 || summary.BySource["pubmed.opml"].Enabled != 1 || summary.BySource["pubmed.opml"].Inactive != 1 {
		t.Fatalf("unexpected pubmed source summary: %+v", summary.BySource["pubmed.opml"])
	}
	if summary.BySource["pubmed.opml"].CheckedMissing != 0 || summary.BySource["pubmed.opml"].SuccessMissing != 1 || summary.BySource["pubmed.opml"].ErrorPresent != 0 {
		t.Fatalf("unexpected pubmed issue counts: %+v", summary.BySource["pubmed.opml"])
	}
	if summary.BySource["pubmed.opml"].Share != 0.5 {
		t.Fatalf("unexpected pubmed share: %+v", summary.BySource["pubmed.opml"])
	}
	if !summary.BySource["pubmed.opml"].LatestCheckedAt.Equal(checkedPubmed2) || !summary.BySource["pubmed.opml"].LatestSuccessAt.Equal(successPubmed) {
		t.Fatalf("unexpected pubmed latest timestamps: %+v", summary.BySource["pubmed.opml"])
	}
	if summary.BySource["pubmed.opml"].ErrorRate != 0 || summary.BySource["pubmed.opml"].ErrorInfoRate != 0 || summary.BySource["pubmed.opml"].CheckedRate != 0 || summary.BySource["pubmed.opml"].SuccessRate != 0.5 {
		t.Fatalf("unexpected pubmed issue rates: %+v", summary.BySource["pubmed.opml"])
	}
	if summary.BySource["pubmed.opml"].ByStatus["ok"] != 1 || summary.BySource["pubmed.opml"].ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected pubmed by_status: %+v", summary.BySource["pubmed.opml"].ByStatus)
	}
	if summary.BySource["pubmed.opml"].ByTitleOnly["true"] != 1 || summary.BySource["pubmed.opml"].ByTitleOnly["inherit"] != 1 {
		t.Fatalf("unexpected pubmed by_title_only: %+v", summary.BySource["pubmed.opml"].ByTitleOnly)
	}
	if summary.BySource["nature.opml"].Total != 1 || summary.BySource["nature.opml"].ByStatus["error"] != 1 {
		t.Fatalf("unexpected nature source summary: %+v", summary.BySource["nature.opml"])
	}
	if summary.BySource["nature.opml"].Share != 0.25 {
		t.Fatalf("unexpected nature share: %+v", summary.BySource["nature.opml"])
	}
	if !summary.BySource["nature.opml"].LatestCheckedAt.Equal(checkedNature) || !summary.BySource["nature.opml"].LatestSuccessAt.Equal(successNature) {
		t.Fatalf("unexpected nature latest timestamps: %+v", summary.BySource["nature.opml"])
	}
	if summary.BySource["nature.opml"].ByTitleOnly["false"] != 1 {
		t.Fatalf("unexpected nature by_title_only: %+v", summary.BySource["nature.opml"].ByTitleOnly)
	}
	if summary.BySource["unknown"].Total != 1 || summary.BySource["unknown"].ByStatus["unknown"] != 1 {
		t.Fatalf("unexpected unknown source summary: %+v", summary.BySource["unknown"])
	}
	if summary.BySource["unknown"].ByTitleOnly["inherit"] != 1 {
		t.Fatalf("unexpected unknown by_title_only: %+v", summary.BySource["unknown"].ByTitleOnly)
	}
	if summary.BySource["unknown"].CheckedMissing != 1 || summary.BySource["unknown"].SuccessMissing != 1 || summary.BySource["unknown"].ErrorPresent != 0 {
		t.Fatalf("unexpected unknown issue counts: %+v", summary.BySource["unknown"])
	}
	if summary.BySource["unknown"].Share != 0.25 {
		t.Fatalf("unexpected unknown share: %+v", summary.BySource["unknown"])
	}
	if !summary.BySource["unknown"].LatestCheckedAt.IsZero() || !summary.BySource["unknown"].LatestSuccessAt.IsZero() {
		t.Fatalf("expected unknown latest timestamps to be zero, got %+v", summary.BySource["unknown"])
	}
	if summary.BySource["unknown"].ErrorRate != 0 || summary.BySource["unknown"].ErrorInfoRate != 0 || summary.BySource["unknown"].CheckedRate != 1 || summary.BySource["unknown"].SuccessRate != 1 {
		t.Fatalf("unexpected unknown issue rates: %+v", summary.BySource["unknown"])
	}
}

func TestBuildFeedStatusSummaryWithSourceFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "pubmed", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 2 || summary.Enabled != 1 || summary.Inactive != 1 {
		t.Fatalf("unexpected filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 2 {
		t.Fatalf("expected only pubmed source summary, got %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithDomainFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://sub.example.com/2", SourceFile: "nature.opml", Status: "inactive"},
		{URL: "https://other.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "example.com", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 2 || summary.Enabled != 1 || summary.Inactive != 1 {
		t.Fatalf("unexpected domain-filtered summary: %+v", summary)
	}
	if summary.ByStatus["ok"] != 1 || summary.ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected domain-filtered by_status: %+v", summary.ByStatus)
	}
}

func TestBuildFeedStatusSummaryWithEnabledFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, boolPtr(true), feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 2 || summary.Enabled != 2 || summary.Inactive != 0 {
		t.Fatalf("unexpected enabled-filtered summary: %+v", summary)
	}
	if _, ok := summary.ByStatus["inactive"]; ok {
		t.Fatalf("did not expect inactive status in enabled-filtered summary: %+v", summary.ByStatus)
	}
}

func TestBuildFeedStatusSummaryWithAllowTitleOnlyFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok", AllowTitleOnly: boolPtr(true)},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error", AllowTitleOnly: boolPtr(false)},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{set: true, value: boolPtr(true)}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected allow-title-only filtered summary: %+v", summary)
	}
	if summary.ByStatus["ok"] != 1 {
		t.Fatalf("unexpected allow-title-only filtered statuses: %+v", summary.ByStatus)
	}
}

func TestBuildFeedStatusSummaryWithStatusFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "active"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "inactive", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.Enabled != 0 || summary.Inactive != 1 {
		t.Fatalf("unexpected status-filtered summary: %+v", summary)
	}
	if summary.ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected status-filtered by_status: %+v", summary.ByStatus)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected status-filtered by_source: %+v", summary.BySource)
	}

	filtered = filterFeeds(feeds, "error", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary = buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected error-status filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["nature.opml"].Total != 1 {
		t.Fatalf("unexpected error-status filtered by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithQueryFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/nature", Title: "Nature Feed", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://example.com/aml", Title: "AML Updates", SourceFile: "nature.opml", Status: "inactive"},
		{URL: "https://other.com/feed", Title: "Other Feed", SourceFile: "other.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "nature", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["ok"] != 1 {
		t.Fatalf("unexpected query-filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected query-filtered by_source: %+v", summary.BySource)
	}

	filtered = filterFeeds(feeds, "", "", "", "other.com", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary = buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected url-query filtered summary: %+v", summary)
	}
}

func TestBuildFeedStatusSummaryWithErrorQueryFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/nature", Title: "Nature Feed", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://example.com/aml", Title: "AML Updates", SourceFile: "nature.opml", Status: "inactive", LastError: "request timeout"},
		{URL: "https://other.com/feed", Title: "Other Feed", SourceFile: "other.opml", Status: "error", LastError: "parse failed"},
	}

	filtered := filterFeedsWithErrorQuery(feeds, "", "", "", "", "timeout", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected error-query filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["nature.opml"].Total != 1 {
		t.Fatalf("unexpected error-query filtered by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithCheckedMissingFilter(t *testing.T) {
	checked := mustDateTime("2026-03-10T09:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", CheckedAt: checked, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checked: boolPtr(true)})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 2 || summary.Inactive != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected checked-missing filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 2 || summary.BySource["pubmed.opml"].Total != 1 || summary.BySource["nature.opml"].Total != 1 {
		t.Fatalf("unexpected checked-missing by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithCheckedBeforeFilter(t *testing.T) {
	checked1 := mustDateTime("2026-03-10T09:00:00Z")
	checked2 := mustDateTime("2026-03-12T09:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", CheckedAt: checked1, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", CheckedAt: checked2, Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checkedBefore: timePtr(mustDate("2026-03-11"))})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["ok"] != 1 {
		t.Fatalf("unexpected checked-before filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected checked-before by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithCheckedAfterFilter(t *testing.T) {
	checked1 := mustDateTime("2026-03-10T09:00:00Z")
	checked2 := mustDateTime("2026-03-12T09:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", CheckedAt: checked1, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", CheckedAt: checked2, Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{checkedAfter: timePtr(mustDate("2026-03-11"))})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected checked-after filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected checked-after by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithSuccessMissingFilter(t *testing.T) {
	success := mustDateTime("2026-03-12T08:30:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", SuccessAt: success, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{success: boolPtr(true)})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 2 || summary.Inactive != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected success-missing filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 2 || summary.BySource["pubmed.opml"].Total != 1 || summary.BySource["nature.opml"].Total != 1 {
		t.Fatalf("unexpected success-missing by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithSuccessBeforeFilter(t *testing.T) {
	success1 := mustDateTime("2026-03-09T08:00:00Z")
	success2 := mustDateTime("2026-03-12T08:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", SuccessAt: success1, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", SuccessAt: success2, Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{successBefore: timePtr(mustDate("2026-03-10"))})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["ok"] != 1 {
		t.Fatalf("unexpected success-before filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected success-before by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithSuccessAfterFilter(t *testing.T) {
	success1 := mustDateTime("2026-03-09T08:00:00Z")
	success2 := mustDateTime("2026-03-12T08:00:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", SuccessAt: success1, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", SuccessAt: success2, Status: "inactive"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{successAfter: timePtr(mustDate("2026-03-10"))})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.ByStatus["inactive"] != 1 {
		t.Fatalf("unexpected success-after filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected success-after by_source: %+v", summary.BySource)
	}
}

func TestBuildFeedStatusSummaryWithErrorMissingFilter(t *testing.T) {
	feeds := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "pubmed.opml", Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "pubmed.opml", Status: "inactive", LastError: "request timeout"},
		{URL: "https://example.com/3", SourceFile: "nature.opml", Status: "error", LastError: "parse failed"},
	}

	filtered := filterFeeds(feeds, "", "", "", "", "url", false, nil, feedAllowTitleOnlyFilter{}, feedTimeMissingFilter{lastError: boolPtr(true)})
	summary := buildFeedStatusSummary(filtered)
	if summary.Total != 1 || summary.Enabled != 1 || summary.ByStatus["ok"] != 1 {
		t.Fatalf("unexpected error-missing filtered summary: %+v", summary)
	}
	if len(summary.BySource) != 1 || summary.BySource["pubmed.opml"].Total != 1 {
		t.Fatalf("unexpected error-missing by_source: %+v", summary.BySource)
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

func TestBuildArticleListOutputIncludesContentAndProcessors(t *testing.T) {
	cfg := config.Config{StatePath: filepath.Join(t.TempDir(), "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := mustDateTime("2026-03-09T10:00:00Z")
	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/feed.xml", Title: "Demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	article := model.Article{
		ID:            "doi:10.1000/demo",
		DOI:           "10.1000/demo",
		CanonicalLink: "https://example.com/article",
		Title:         "Demo article",
		Abstract:      "Short abstract",
		FeedURL:       "https://example.com/feed.xml",
		ContentHash:   "hash-1",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/article",
	}
	if _, err := db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusFetched,
		SourceType:  "html_body",
		ContentText: "Full text",
		ContentHash: "content-hash",
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIResult(model.ProcessorResult{
		ArticleID:   article.ID,
		Processor:   "generic_digest",
		Model:       "demo-model",
		InputHash:   "input-hash",
		Status:      model.ProcessorStatusProcessed,
		OutputJSON:  `{"summary":"摘要","tags":["AML"]}`,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	items := buildArticleListOutput(db, []model.Article{article})
	if len(items) != 1 {
		t.Fatalf("expected one item, got %+v", items)
	}
	if items[0].Content == nil || items[0].Content.FetchStatus != model.ContentStatusFetched || !items[0].Content.HasText {
		t.Fatalf("unexpected content summary: %+v", items[0].Content)
	}
	processor, ok := items[0].Processors["generic_digest"]
	if !ok || processor.Status != model.ProcessorStatusProcessed || !strings.Contains(string(processor.Output), "摘要") {
		t.Fatalf("unexpected processor summaries: %+v", items[0].Processors)
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
		{Review: &articleReviewSummary{TotalScore: 88, AMLValue: 25, RecommendationLevel: "强烈推荐"}, Content: &articleContentSummary{FetchStatus: model.ContentStatusFetched}},
		{Review: &articleReviewSummary{TotalScore: 76, AMLValue: 18, RecommendationLevel: "推荐"}, Content: &articleContentSummary{FetchStatus: model.ContentStatusRSSFallback}},
		{Review: &articleReviewSummary{TotalScore: 66, AMLValue: 22, RecommendationLevel: "有条件推荐"}, Content: &articleContentSummary{FetchStatus: model.ContentStatusFailed}},
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

	filtered = filterArticleListOutput(items, articleListFilters{ContentStatus: "fetched"})
	if len(filtered) != 1 || filtered[0].Content == nil || filtered[0].Content.FetchStatus != model.ContentStatusFetched {
		t.Fatalf("expected fetched content item only, got %+v", filtered)
	}

	filtered = filterArticleListOutput(items, articleListFilters{ContentStatus: "missing"})
	if len(filtered) != 1 || filtered[0].Content != nil {
		t.Fatalf("expected missing content item only, got %+v", filtered)
	}
}

func TestFilterArticleListOutputByProcessor(t *testing.T) {
	items := []articleListItem{
		{Processors: map[string]articleProcessorSummary{
			"generic_digest": {Status: model.ProcessorStatusProcessed},
			"aml_score":      {Status: model.ProcessorStatusFailed},
		}},
		{Processors: map[string]articleProcessorSummary{
			"generic_digest": {Status: model.ProcessorStatusFailed},
		}},
		{},
	}

	filtered := filterArticleListOutput(items, articleListFilters{Processor: "generic_digest"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items with generic_digest result, got %+v", filtered)
	}

	filtered = filterArticleListOutput(items, articleListFilters{Processor: "aml_score", ProcessorStatus: "failed"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 aml_score failed item, got %+v", filtered)
	}

	filtered = filterArticleListOutput(items, articleListFilters{Processor: "aml_score", ProcessorStatus: "missing"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items missing aml_score, got %+v", filtered)
	}

	filtered = filterArticleListOutput(items, articleListFilters{ProcessorStatus: "failed"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 items with any failed processor, got %+v", filtered)
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

func TestRunDBStatsJSON(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/rss", Title: "demo", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	article := model.Article{
		ID:            "doi:10.1000/dbstats",
		DOI:           "10.1000/dbstats",
		CanonicalLink: "https://example.com/article",
		Title:         "Demo",
		Abstract:      "Abstract",
		FeedURL:       "https://example.com/rss",
		ContentHash:   "hash",
		FirstSeenAt:   now,
		LastSeenAt:    now,
		Link:          "https://example.com/article",
	}
	if _, err := db.UpsertArticle(article); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertArticleContent(model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusFetched,
		SourceType:  "html_body",
		ContentText: "Full text",
		ContentHash: "content-hash",
		FetchedAt:   &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAIResult(model.ProcessorResult{
		ArticleID:   article.ID,
		Processor:   "generic_digest",
		InputHash:   "input-hash",
		Status:      model.ProcessorStatusProcessed,
		ProcessedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runDBStats(db, []string{"--json"})
	})
	var payload struct {
		Feeds                 int                       `json:"feeds"`
		ContentFetched        int                       `json:"content_fetched"`
		ContentMissing        int                       `json:"content_missing"`
		ContentByStatus       map[string]int            `json:"content_by_status"`
		ProcessorLatestStatus map[string]map[string]int `json:"processor_latest_status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if payload.Feeds != 1 || payload.ContentFetched != 1 || payload.ContentMissing != 0 {
		t.Fatalf("unexpected db stats payload: %+v", payload)
	}
	if payload.ContentByStatus[model.ContentStatusFetched] != 1 {
		t.Fatalf("expected fetched content status in payload, got %+v", payload.ContentByStatus)
	}
	if payload.ProcessorLatestStatus["generic_digest"][model.ProcessorStatusProcessed] != 1 {
		t.Fatalf("expected processor latest status in payload, got %+v", payload.ProcessorLatestStatus)
	}
}

func TestRunDBStatsRejectsUnknownArg(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = runDBStats(db, []string{"--table"})
	if err == nil || !strings.Contains(err.Error(), "unknown db stats arg") {
		t.Fatalf("expected unknown db stats arg error, got %v", err)
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

func TestPrintFeedsTable(t *testing.T) {
	checked := mustDateTime("2026-03-10T09:00:00Z")
	success := mustDateTime("2026-03-12T08:30:00Z")
	feeds := []model.Feed{
		{URL: "https://example.com/1", Title: "First Feed", SourceFile: "alpha.opml", CheckedAt: checked, SuccessAt: success, Status: "ok"},
		{URL: "https://example.com/2", Title: "Second Feed", SourceFile: "beta.opml", Status: "inactive", LastError: "request timeout", AllowTitleOnly: boolPtr(true)},
		{URL: "https://example.com/3", Title: "Third Feed", Status: "error", LastError: "parse feed failed: unsupported format", AllowTitleOnly: boolPtr(false)},
	}
	var buf bytes.Buffer
	if err := printFeedsTable(&buf, feeds); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{"Enabled", "Status", "Checked", "Success", "Error", "TitleOnly", "Domain", "example.com", "Source", "alpha.opml", "beta.opml", "unknown", "request timeout", "parse feed failed: unsupported format", "2026-03-10 09:00", "2026-03-12 08:30", "First Feed", "inherit", "true", "false", "https://example.com/3"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in feeds table output, got %s", want, output)
		}
	}
}

func TestPrintFeedStatusTable(t *testing.T) {
	latestChecked := mustDateTime("2026-03-12T07:45:00Z")
	latestSuccess := mustDateTime("2026-03-11T08:30:00Z")
	summary := feedStatusSummary{
		Total:           4,
		Enabled:         3,
		Inactive:        1,
		CheckedMissing:  2,
		SuccessMissing:  3,
		ErrorPresent:    2,
		LatestCheckedAt: latestChecked,
		LatestSuccessAt: latestSuccess,
		ByStatus: map[string]int{
			"error":    1,
			"inactive": 1,
			"ok":       1,
			"unknown":  1,
		},
		ByTitleOnly: map[string]int{
			"false":   1,
			"inherit": 2,
			"true":    1,
		},
		BySource: map[string]feedSourceStatus{
			"pubmed.opml": {
				Total:           2,
				Enabled:         1,
				Inactive:        1,
				CheckedMissing:  1,
				SuccessMissing:  2,
				ErrorPresent:    1,
				LatestCheckedAt: mustDateTime("2026-03-12T07:45:00Z"),
				LatestSuccessAt: mustDateTime("2026-03-10T06:15:00Z"),
				ByStatus:        map[string]int{"inactive": 1, "ok": 1},
				ByTitleOnly:     map[string]int{"inherit": 1, "true": 1},
			},
			"unknown": {
				Total:           1,
				Enabled:         1,
				Inactive:        0,
				CheckedMissing:  1,
				SuccessMissing:  1,
				ErrorPresent:    1,
				LatestSuccessAt: mustDateTime("2026-03-11T08:30:00Z"),
				ByStatus:        map[string]int{"unknown": 1},
				ByTitleOnly:     map[string]int{"inherit": 1},
			},
		},
	}

	var buf bytes.Buffer
	if err := printFeedStatusTable(&buf, summary, "source", false, 0); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	for _, want := range []string{"Source", "Share", "Checked", "Success", "Errors", "ErrorRate", "ErrInfo", "ErrInfoRate", "ChkMiss", "ChkMissRate", "SucMiss", "SucMissRate", "TitleOnly", "TOTAL", "100.0%", "50.0%", "25.0%", "0.0%", "75.0%", "2026-03-12 07:45", "2026-03-11 08:30", "2026-03-10 06:15", "pubmed.opml", "unknown", "error=1, inactive=1, ok=1, unknown=1", "inactive=1, ok=1", "false=1, inherit=2, true=1", "inherit=1, true=1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in feed status table output, got %s", want, output)
		}
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and total row, got %s", output)
	}
	totalFields := strings.Fields(lines[1])
	if len(totalFields) < 15 || totalFields[0] != "TOTAL" || totalFields[3] != "2026-03-12" || totalFields[4] != "07:45" || totalFields[5] != "2026-03-11" || totalFields[6] != "08:30" || totalFields[7] != "1" || totalFields[8] != "25.0%" || totalFields[9] != "2" || totalFields[10] != "50.0%" || totalFields[11] != "2" || totalFields[12] != "50.0%" || totalFields[13] != "3" || totalFields[14] != "75.0%" {
		t.Fatalf("expected total row to include explicit error count/rate, got %v", totalFields)
	}
}

func TestPrintFeedStatusTableSortsRows(t *testing.T) {
	summary := feedStatusSummary{
		BySource: map[string]feedSourceStatus{
			"alpha.opml": {Total: 1, Enabled: 1, Inactive: 0},
			"beta.opml":  {Total: 3, Enabled: 2, Inactive: 1},
			"gamma.opml": {Total: 2, Enabled: 2, Inactive: 0},
		},
	}

	var buf bytes.Buffer
	if err := printFeedStatusTable(&buf, summary, "total", true, 0); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	beta := strings.Index(output, "beta.opml")
	gamma := strings.Index(output, "gamma.opml")
	alpha := strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by total desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {LatestCheckedAt: mustDateTime("2026-03-10T08:00:00Z")},
		"beta.opml":  {LatestCheckedAt: mustDateTime("2026-03-12T08:00:00Z")},
		"gamma.opml": {LatestCheckedAt: mustDateTime("2026-03-11T08:00:00Z")},
	}
	if err := printFeedStatusTable(&buf, summary, "checked", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by latest checked desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {CheckedMissing: 1},
		"beta.opml":  {CheckedMissing: 3},
		"gamma.opml": {CheckedMissing: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "checked-missing", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by checked-missing desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, CheckedMissing: 1},
		"beta.opml":  {Total: 2, CheckedMissing: 1},
		"gamma.opml": {Total: 3, CheckedMissing: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "checked-missing-rate", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by checked-missing-rate desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {LatestSuccessAt: mustDateTime("2026-03-10T08:00:00Z")},
		"beta.opml":  {LatestSuccessAt: mustDateTime("2026-03-12T08:00:00Z")},
		"gamma.opml": {LatestSuccessAt: mustDateTime("2026-03-11T08:00:00Z")},
	}
	if err := printFeedStatusTable(&buf, summary, "success", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by latest success desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {SuccessMissing: 1},
		"beta.opml":  {SuccessMissing: 3},
		"gamma.opml": {SuccessMissing: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "success-missing", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by success-missing desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, SuccessMissing: 1},
		"beta.opml":  {Total: 2, SuccessMissing: 1},
		"gamma.opml": {Total: 3, SuccessMissing: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "success-missing-rate", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by success-missing-rate desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {ErrorPresent: 1},
		"beta.opml":  {ErrorPresent: 3},
		"gamma.opml": {ErrorPresent: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "error-present", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by error-present desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, ErrorPresent: 1},
		"beta.opml":  {Total: 2, ErrorPresent: 1},
		"gamma.opml": {Total: 3, ErrorPresent: 2},
	}
	if err := printFeedStatusTable(&buf, summary, "error-present-rate", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by error-present-rate desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {ByStatus: map[string]int{"error": 1}},
		"beta.opml":  {ByStatus: map[string]int{"error": 3}},
		"gamma.opml": {ByStatus: map[string]int{"error": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "status:error", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by error desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {ByStatus: map[string]int{"ok": 1}},
		"beta.opml":  {ByStatus: map[string]int{"ok": 3}},
		"gamma.opml": {ByStatus: map[string]int{"ok": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "status:ok", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by ok desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {ByTitleOnly: map[string]int{"true": 1}},
		"beta.opml":  {ByTitleOnly: map[string]int{"true": 3}},
		"gamma.opml": {ByTitleOnly: map[string]int{"true": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "title-only:true", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	beta = strings.Index(output, "beta.opml")
	gamma = strings.Index(output, "gamma.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(beta >= 0 && gamma >= 0 && alpha >= 0 && beta < gamma && gamma < alpha) {
		t.Fatalf("expected rows sorted by title-only true desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, ByStatus: map[string]int{"error": 1}},
		"beta.opml":  {Total: 2, ByStatus: map[string]int{"error": 1}},
		"gamma.opml": {Total: 3, ByStatus: map[string]int{"error": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "error-rate", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by error-rate desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, ByStatus: map[string]int{"ok": 1}},
		"beta.opml":  {Total: 2, ByStatus: map[string]int{"ok": 1}},
		"gamma.opml": {Total: 3, ByStatus: map[string]int{"ok": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "status-rate:ok", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by ok-rate desc, got %s", output)
	}

	buf.Reset()
	summary.BySource = map[string]feedSourceStatus{
		"alpha.opml": {Total: 4, ByTitleOnly: map[string]int{"true": 1}},
		"beta.opml":  {Total: 2, ByTitleOnly: map[string]int{"true": 1}},
		"gamma.opml": {Total: 3, ByTitleOnly: map[string]int{"true": 2}},
	}
	if err := printFeedStatusTable(&buf, summary, "title-only-rate:true", true, 0); err != nil {
		t.Fatal(err)
	}
	output = buf.String()
	gamma = strings.Index(output, "gamma.opml")
	beta = strings.Index(output, "beta.opml")
	alpha = strings.Index(output, "alpha.opml")
	if !(gamma >= 0 && beta >= 0 && alpha >= 0 && gamma < beta && beta < alpha) {
		t.Fatalf("expected rows sorted by title-only true rate desc, got %s", output)
	}
}

func TestPrintFeedStatusTableLimitsRows(t *testing.T) {
	summary := feedStatusSummary{
		BySource: map[string]feedSourceStatus{
			"alpha.opml": {Total: 1},
			"beta.opml":  {Total: 3},
			"gamma.opml": {Total: 2},
		},
	}

	var buf bytes.Buffer
	if err := printFeedStatusTable(&buf, summary, "total", true, 2); err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	if !strings.Contains(output, "TOTAL") || !strings.Contains(output, "beta.opml") || !strings.Contains(output, "gamma.opml") {
		t.Fatalf("expected total row and top two source rows, got %s", output)
	}
	if strings.Contains(output, "alpha.opml") {
		t.Fatalf("expected limited output to exclude alpha.opml, got %s", output)
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

func TestParseOptionalBool(t *testing.T) {
	tests := []struct {
		raw      string
		expected *bool
		wantErr  bool
	}{
		{raw: "true", expected: boolPtr(true)},
		{raw: "false", expected: boolPtr(false)},
		{raw: "inherit", expected: nil},
		{raw: "default", expected: nil},
		{raw: "", expected: nil},
		{raw: "maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseOptionalBool(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %#v, got %#v", tt.expected, got)
			}
		})
	}
}

func TestParseFeedStatus(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
		wantErr  bool
	}{
		{raw: "active", expected: "active"},
		{raw: "inactive", expected: "inactive"},
		{raw: "ACTIVE", expected: "active"},
		{raw: "paused", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFeedStatus(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.raw, err)
		}
		if got != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, got)
		}
	}
}

func TestParseFeedStatusFilter(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
		wantErr  bool
	}{
		{raw: "active", expected: "active"},
		{raw: "inactive", expected: "inactive"},
		{raw: "error", expected: "error"},
		{raw: "not_modified", expected: "not_modified"},
		{raw: "  ok  ", expected: "ok"},
		{raw: "", wantErr: true},
		{raw: "   ", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFeedStatusFilter(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.raw, err)
		}
		if got != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, got)
		}
	}
}

func TestParseFilterDate(t *testing.T) {
	got, err := parseFilterDate("2026-03-11")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(mustDate("2026-03-11")) {
		t.Fatalf("expected parsed date 2026-03-11, got %v", got)
	}

	if _, err := parseFilterDate(""); err == nil {
		t.Fatal("expected empty date to fail")
	}
	if _, err := parseFilterDate("2026/03/11"); err == nil {
		t.Fatal("expected invalid date format to fail")
	}
}

func TestParseFeedSort(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
		wantErr  bool
	}{
		{raw: "url", expected: "url"},
		{raw: "checked", expected: "checked"},
		{raw: "checked-at", expected: "checked"},
		{raw: "latest-checked", expected: "checked"},
		{raw: "latest_checked_at", expected: "checked"},
		{raw: "checked-missing", expected: "checked-missing"},
		{raw: "checked_missing", expected: "checked-missing"},
		{raw: "domain", expected: "domain"},
		{raw: "enabled", expected: "enabled"},
		{raw: "title-only", expected: "title-only"},
		{raw: "allow-title-only", expected: "title-only"},
		{raw: "allow_title_mode", expected: "title-only"},
		{raw: "has-error", expected: "has-error"},
		{raw: "has_error", expected: "has-error"},
		{raw: "error-present", expected: "has-error"},
		{raw: "last-error", expected: "last-error"},
		{raw: "last_error", expected: "last-error"},
		{raw: "error", expected: "last-error"},
		{raw: "source", expected: "source"},
		{raw: "source_name", expected: "source"},
		{raw: "source-name", expected: "source"},
		{raw: "success", expected: "success"},
		{raw: "latest_success_at", expected: "success"},
		{raw: "success-at", expected: "success"},
		{raw: "latest-success", expected: "success"},
		{raw: "success-missing", expected: "success-missing"},
		{raw: "success_missing", expected: "success-missing"},
		{raw: "title", expected: "title"},
		{raw: "status", expected: "status"},
		{raw: "status-label", expected: "status"},
		{raw: "", expected: "url"},
		{raw: "priority", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFeedSort(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.raw, err)
		}
		if got != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, got)
		}
	}
}

func TestFeedDomain(t *testing.T) {
	if got := feedDomain("https://Sub.Example.com:8443/path"); got != "sub.example.com" {
		t.Fatalf("expected hostname without port, got %q", got)
	}
	if got := feedDomain("not a url"); got != "" {
		t.Fatalf("expected invalid url to return empty domain, got %q", got)
	}
}

func TestFormatStatusCounts(t *testing.T) {
	got := formatStatusCounts(map[string]int{"ok": 2, "error": 1, "inactive": 3})
	if got != "error=1, inactive=3, ok=2" {
		t.Fatalf("unexpected formatted status counts: %q", got)
	}
	if got := formatStatusCounts(nil); got != "-" {
		t.Fatalf("expected empty status counts as -, got %q", got)
	}
}

func TestParseFeedStatusSort(t *testing.T) {
	tests := []struct {
		raw      string
		expected string
		wantErr  bool
	}{
		{raw: "source", expected: "source"},
		{raw: "source-name", expected: "source"},
		{raw: "source_name", expected: "source"},
		{raw: "checked", expected: "checked"},
		{raw: "checked-at", expected: "checked"},
		{raw: "latest-checked", expected: "checked"},
		{raw: "latest_checked_at", expected: "checked"},
		{raw: "checked-missing", expected: "checked-missing"},
		{raw: "checked_missing", expected: "checked-missing"},
		{raw: "checked-missing-rate", expected: "checked-missing-rate"},
		{raw: "checked_missing_rate", expected: "checked-missing-rate"},
		{raw: "success", expected: "success"},
		{raw: "success-at", expected: "success"},
		{raw: "latest-success", expected: "success"},
		{raw: "latest_success_at", expected: "success"},
		{raw: "success-missing", expected: "success-missing"},
		{raw: "success_missing", expected: "success-missing"},
		{raw: "success-missing-rate", expected: "success-missing-rate"},
		{raw: "success_missing_rate", expected: "success-missing-rate"},
		{raw: "error-present", expected: "error-present"},
		{raw: "error_present", expected: "error-present"},
		{raw: "error-info", expected: "error-present"},
		{raw: "error-present-rate", expected: "error-present-rate"},
		{raw: "error_present_rate", expected: "error-present-rate"},
		{raw: "error-info-rate", expected: "error-present-rate"},
		{raw: "total", expected: "total"},
		{raw: "share", expected: "total"},
		{raw: "source-share", expected: "total"},
		{raw: "enabled", expected: "enabled"},
		{raw: "inactive", expected: "inactive"},
		{raw: "error-rate", expected: "status-rate:error"},
		{raw: "error", expected: "status:error"},
		{raw: "status:error", expected: "status:error"},
		{raw: "status:ok", expected: "status:ok"},
		{raw: "status-rate:error", expected: "status-rate:error"},
		{raw: "status-rate:ok", expected: "status-rate:ok"},
		{raw: "title-only:true", expected: "title-only:true"},
		{raw: "title-only:false", expected: "title-only:false"},
		{raw: "title-only:inherit", expected: "title-only:inherit"},
		{raw: "title-only-rate:true", expected: "title-only-rate:true"},
		{raw: "title-only-rate:false", expected: "title-only-rate:false"},
		{raw: "title-only-rate:inherit", expected: "title-only-rate:inherit"},
		{raw: "", expected: "source"},
		{raw: "status", wantErr: true},
		{raw: "status:", wantErr: true},
		{raw: "status-rate:", wantErr: true},
		{raw: "title-only:", wantErr: true},
		{raw: "title-only-rate:", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseFeedStatusSort(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.raw, err)
		}
		if got != tt.expected {
			t.Fatalf("expected %q, got %q", tt.expected, got)
		}
	}
}

func TestFormatShare(t *testing.T) {
	if got := formatShare(2, 4); got != "50.0%" {
		t.Fatalf("expected 50.0%%, got %q", got)
	}
	if got := formatShare(0, 0); got != "0.0%" {
		t.Fatalf("expected 0.0%% for zero total, got %q", got)
	}
}

func TestFormatFeedTimestamp(t *testing.T) {
	value := mustDateTime("2026-03-10T09:15:00Z")
	if got := formatFeedTimestamp(value); got != "2026-03-10 09:15" {
		t.Fatalf("unexpected formatted timestamp: %q", got)
	}
	if got := formatFeedTimestamp(time.Time{}); got != "-" {
		t.Fatalf("expected zero time as -, got %q", got)
	}
}

func TestFormatFeedError(t *testing.T) {
	if got := formatFeedError(""); got != "-" {
		t.Fatalf("expected empty error as -, got %q", got)
	}
	value := "parse feed failed: unsupported format"
	if got := formatFeedError(value); got != value {
		t.Fatalf("expected full error message, got %q", got)
	}
	longValue := "this is a very long error message that should be truncated in table output for readability"
	if got := formatFeedError(longValue); !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncated error message, got %q", got)
	}
}

func TestFeedMatchesErrorQuery(t *testing.T) {
	if !feedMatchesErrorQuery("request timeout", "timeout") {
		t.Fatal("expected error query match")
	}
	if feedMatchesErrorQuery("", "timeout") {
		t.Fatal("did not expect empty error to match non-empty query")
	}
	if !feedMatchesErrorQuery("parse failed", "FAILED") {
		t.Fatal("expected case-insensitive error query match")
	}
}

func TestFormatRate(t *testing.T) {
	if got := formatRate(1, 4); got != "25.0%" {
		t.Fatalf("expected 25.0%%, got %q", got)
	}
	if got := formatRate(0, 0); got != "0.0%" {
		t.Fatalf("expected 0.0%% for zero total, got %q", got)
	}
}

func TestSourceStatusRate(t *testing.T) {
	if got := sourceStatusRate(2, 4); got != 0.5 {
		t.Fatalf("expected 0.5, got %v", got)
	}
	if got := sourceStatusRate(1, 0); got != 0 {
		t.Fatalf("expected 0 for zero total, got %v", got)
	}
}

func mustDateTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestFeedMatchesDomain(t *testing.T) {
	tests := []struct {
		feedURL  string
		domain   string
		expected bool
	}{
		{feedURL: "https://example.com/feed.xml", domain: "example.com", expected: true},
		{feedURL: "https://news.example.com/feed.xml", domain: "example.com", expected: true},
		{feedURL: "https://otherexample.com/feed.xml", domain: "example.com", expected: false},
		{feedURL: "not a url", domain: "example.com", expected: false},
	}
	for _, tt := range tests {
		if got := feedMatchesDomain(tt.feedURL, tt.domain); got != tt.expected {
			t.Fatalf("feedMatchesDomain(%q, %q) = %v, want %v", tt.feedURL, tt.domain, got, tt.expected)
		}
	}
}

func TestFeedMatchesStatus(t *testing.T) {
	tests := []struct {
		feedStatus string
		filter     string
		expected   bool
	}{
		{feedStatus: "ok", filter: "ok", expected: true},
		{feedStatus: "", filter: "unknown", expected: true},
		{feedStatus: "unknown", filter: "unknown", expected: true},
		{feedStatus: "inactive", filter: "unknown", expected: false},
	}
	for _, tt := range tests {
		if got := feedMatchesStatus(tt.feedStatus, tt.filter); got != tt.expected {
			t.Fatalf("feedMatchesStatus(%q, %q) = %v, want %v", tt.feedStatus, tt.filter, got, tt.expected)
		}
	}
}

func TestFeedMatchesSource(t *testing.T) {
	tests := []struct {
		sourceFile string
		query      string
		expected   bool
	}{
		{sourceFile: "pubmed.opml", query: "pubmed", expected: true},
		{sourceFile: "", query: "unknown", expected: true},
		{sourceFile: "", query: "pubmed", expected: false},
	}
	for _, tt := range tests {
		if got := feedMatchesSource(tt.sourceFile, tt.query); got != tt.expected {
			t.Fatalf("feedMatchesSource(%q, %q) = %v, want %v", tt.sourceFile, tt.query, got, tt.expected)
		}
	}
}

func TestRunFeedsSetPersistsAllowTitleOnly(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "Example", Status: "active"},
		{URL: "https://example.com/inactive.xml", Title: "Inactive", Status: "inactive"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--url=https://example.com/feed.xml", "--allow-title-only=true"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	if feeds[0].AllowTitleOnly == nil || !*feeds[0].AllowTitleOnly {
		t.Fatalf("expected allow_title_only persisted, got %+v", feeds[0])
	}

	content, err := os.ReadFile(cfg.FeedsJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported []struct {
		URL            string `json:"url"`
		AllowTitleOnly *bool  `json:"allow_title_only,omitempty"`
	}
	if err := json.Unmarshal(content, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 1 {
		t.Fatalf("expected only active feeds exported, got %#v", exported)
	}
	if exported[0].AllowTitleOnly == nil || !*exported[0].AllowTitleOnly {
		t.Fatalf("expected exported allow_title_only true, got %#v", exported)
	}

	if err := runFeedsSet(cfg, db, []string{"--url=https://example.com/feed.xml", "--allow-title-only=inherit"}); err != nil {
		t.Fatal(err)
	}
	updated := db.ListFeeds()[0]
	if updated.AllowTitleOnly != nil {
		t.Fatalf("expected allow_title_only cleared to inherit, got %+v", updated.AllowTitleOnly)
	}
}

func TestRunFeedsSetUpdatesStatus(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/feed.xml", Title: "Example", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--url=https://example.com/feed.xml", "--status=inactive"}); err != nil {
		t.Fatal(err)
	}

	got := db.ListFeeds()[0]
	if got.Status != "inactive" {
		t.Fatalf("expected status inactive, got %+v", got)
	}
}

func TestRunFeedsSetUpdatesTitle(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/feed.xml", Title: "Old Title", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--url=https://example.com/feed.xml", "--title=New Title"}); err != nil {
		t.Fatal(err)
	}

	got := db.ListFeeds()[0]
	if got.Title != "New Title" {
		t.Fatalf("expected title updated, got %+v", got)
	}

	content, err := os.ReadFile(cfg.FeedsJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var exported []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(content, &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported) != 1 || exported[0].Title != "New Title" {
		t.Fatalf("expected exported title updated, got %#v", exported)
	}
}

func TestRunFeedsSetSupportsDomainBatchUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "Root", Status: "active"},
		{URL: "https://sub.example.com/feed.xml", Title: "Sub", Status: "active"},
		{URL: "https://other.com/feed.xml", Title: "Other", Status: "active"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--domain=example.com", "--allow-title-only=false", "--yes"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	byURL := map[string]model.Feed{}
	for _, item := range feeds {
		byURL[item.URL] = item
	}
	if byURL["https://example.com/feed.xml"].AllowTitleOnly == nil || *byURL["https://example.com/feed.xml"].AllowTitleOnly {
		t.Fatalf("expected root domain feed allow_title_only=false, got %+v", byURL["https://example.com/feed.xml"])
	}
	if byURL["https://sub.example.com/feed.xml"].AllowTitleOnly == nil || *byURL["https://sub.example.com/feed.xml"].AllowTitleOnly {
		t.Fatalf("expected subdomain feed allow_title_only=false, got %+v", byURL["https://sub.example.com/feed.xml"])
	}
	if byURL["https://other.com/feed.xml"].AllowTitleOnly != nil {
		t.Fatalf("expected unrelated feed unchanged, got %+v", byURL["https://other.com/feed.xml"])
	}
}

func TestRunFeedsSetSupportsDomainBatchStatusUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "Root", Status: "active"},
		{URL: "https://sub.example.com/feed.xml", Title: "Sub", Status: "active"},
		{URL: "https://other.com/feed.xml", Title: "Other", Status: "active"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--domain=example.com", "--status=inactive", "--yes"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	byURL := map[string]model.Feed{}
	for _, item := range feeds {
		byURL[item.URL] = item
	}
	if byURL["https://example.com/feed.xml"].Status != "inactive" {
		t.Fatalf("expected root domain feed inactive, got %+v", byURL["https://example.com/feed.xml"])
	}
	if byURL["https://sub.example.com/feed.xml"].Status != "inactive" {
		t.Fatalf("expected subdomain feed inactive, got %+v", byURL["https://sub.example.com/feed.xml"])
	}
	if byURL["https://other.com/feed.xml"].Status != "active" {
		t.Fatalf("expected unrelated feed unchanged, got %+v", byURL["https://other.com/feed.xml"])
	}
}

func TestRunFeedsSetSupportsSourceBatchUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "PubMed Root", SourceFile: "pubmed.opml", Status: "active"},
		{URL: "https://sub.example.com/feed.xml", Title: "PubMed Sub", SourceFile: "PUBMED.OPML", Status: "active"},
		{URL: "https://other.com/feed.xml", Title: "Other", SourceFile: "nature.opml", Status: "active"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--source=pubmed.opml", "--status=inactive", "--allow-title-only=false", "--yes"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	byURL := map[string]model.Feed{}
	for _, item := range feeds {
		byURL[item.URL] = item
	}
	for _, url := range []string{"https://example.com/feed.xml", "https://sub.example.com/feed.xml"} {
		item := byURL[url]
		if item.Status != "inactive" {
			t.Fatalf("expected source-matched feed inactive, got %+v", item)
		}
		if item.AllowTitleOnly == nil || *item.AllowTitleOnly {
			t.Fatalf("expected source-matched feed allow_title_only=false, got %+v", item)
		}
	}
	if byURL["https://other.com/feed.xml"].Status != "active" || byURL["https://other.com/feed.xml"].AllowTitleOnly != nil {
		t.Fatalf("expected unmatched feed unchanged, got %+v", byURL["https://other.com/feed.xml"])
	}
}

func TestRunFeedsSetSupportsUnknownSourceBatchUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "Unknown Source", Status: "active"},
		{URL: "https://other.com/feed.xml", Title: "Known Source", SourceFile: "pubmed.opml", Status: "active"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--source=unknown", "--status=inactive", "--yes"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	byURL := map[string]model.Feed{}
	for _, item := range feeds {
		byURL[item.URL] = item
	}
	if byURL["https://example.com/feed.xml"].Status != "inactive" {
		t.Fatalf("expected unknown-source feed inactive, got %+v", byURL["https://example.com/feed.xml"])
	}
	if byURL["https://other.com/feed.xml"].Status != "active" {
		t.Fatalf("expected known-source feed unchanged, got %+v", byURL["https://other.com/feed.xml"])
	}
}

func TestRunFeedsStatusSupportsMissingTimeFilters(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	checked := mustDateTime("2026-03-10T09:00:00Z")
	success := mustDateTime("2026-03-12T08:30:00Z")
	fixtures := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "alpha.opml", CheckedAt: checked, SuccessAt: success, Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "beta.opml", CheckedAt: checked, Status: "inactive", LastError: "request timeout"},
		{URL: "https://example.com/3", SourceFile: "gamma.opml", Status: "error", LastError: "parse failed"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--checked-missing=true"})
	})

	var summary feedStatusSummary
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 1 || summary.ByStatus["error"] != 1 || summary.BySource["gamma.opml"].Total != 1 {
		t.Fatalf("unexpected checked-missing status summary: %+v", summary)
	}
	if summary.ErrorRate != 1 || summary.ErrorInfoRate != 1 || summary.CheckedRate != 1 || summary.SuccessRate != 1 {
		t.Fatalf("unexpected checked-missing status rates: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--success-missing=true"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 2 || summary.ByStatus["inactive"] != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected success-missing status summary: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--error-missing=true"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 1 || summary.ByStatus["ok"] != 1 || summary.BySource["alpha.opml"].Total != 1 {
		t.Fatalf("unexpected error-missing status summary: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--error-missing=false"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 2 || summary.ByStatus["inactive"] != 1 || summary.ByStatus["error"] != 1 {
		t.Fatalf("unexpected error-present status summary: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--checked-before=2026-03-11"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 2 || summary.ByStatus["ok"] != 1 || summary.ByStatus["inactive"] != 1 || summary.BySource["alpha.opml"].Total != 1 || summary.BySource["beta.opml"].Total != 1 {
		t.Fatalf("unexpected checked-before status summary: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--checked-after=2026-03-11"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 0 {
		t.Fatalf("unexpected checked-after status summary: %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--success-before=2026-03-10"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 0 {
		t.Fatalf("expected no feeds before success cutoff, got %+v", summary)
	}

	output = captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--success-after=2026-03-10"})
	})
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 1 || summary.ByStatus["ok"] != 1 || summary.BySource["alpha.opml"].Total != 1 {
		t.Fatalf("unexpected success-after status summary: %+v", summary)
	}
}

func TestRunFeedsStatusRejectsInvalidMissingTimeFilter(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = runFeedsStatus(db, []string{"--checked-missing=maybe"})
	if err == nil || !strings.Contains(err.Error(), "invalid --checked-missing") {
		t.Fatalf("expected invalid checked-missing error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--success-missing=maybe"})
	if err == nil || !strings.Contains(err.Error(), "invalid --success-missing") {
		t.Fatalf("expected invalid success-missing error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--error-missing=maybe"})
	if err == nil || !strings.Contains(err.Error(), "invalid --error-missing") {
		t.Fatalf("expected invalid error-missing error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--checked-before=2026/03/11"})
	if err == nil || !strings.Contains(err.Error(), "invalid --checked-before") {
		t.Fatalf("expected invalid checked-before error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--checked-after=2026/03/11"})
	if err == nil || !strings.Contains(err.Error(), "invalid --checked-after") {
		t.Fatalf("expected invalid checked-after error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--success-before=2026/03/10"})
	if err == nil || !strings.Contains(err.Error(), "invalid --success-before") {
		t.Fatalf("expected invalid success-before error, got %v", err)
	}

	err = runFeedsStatus(db, []string{"--success-after=2026/03/10"})
	if err == nil || !strings.Contains(err.Error(), "invalid --success-after") {
		t.Fatalf("expected invalid success-after error, got %v", err)
	}
}

func TestRunFeedsStatusSupportsErrorQuery(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "alpha.opml", Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "beta.opml", Status: "inactive", LastError: "request timeout"},
		{URL: "https://example.com/3", SourceFile: "gamma.opml", Status: "error", LastError: "parse failed"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--error-q=parse"})
	})

	var summary feedStatusSummary
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.Total != 1 || summary.ByStatus["error"] != 1 || summary.BySource["gamma.opml"].Total != 1 {
		t.Fatalf("unexpected error-q status summary: %+v", summary)
	}
}

func TestRunFeedsStatusJSONIncludesSortedSources(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/1", SourceFile: "alpha.opml", CheckedAt: mustDateTime("2026-03-10T09:00:00Z"), Status: "ok"},
		{URL: "https://example.com/2", SourceFile: "beta.opml", CheckedAt: mustDateTime("2026-03-12T09:00:00Z"), Status: "ok"},
		{URL: "https://example.com/3", SourceFile: "gamma.opml", CheckedAt: mustDateTime("2026-03-11T09:00:00Z"), Status: "error"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runFeedsStatus(db, []string{"--sort=checked", "--reverse", "--limit=2"})
	})

	var summary feedStatusSummary
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if summary.SourceCount != 3 {
		t.Fatalf("expected source_count=3, got %+v", summary)
	}
	if len(summary.Sources) != 2 {
		t.Fatalf("expected 2 sorted sources after limit, got %+v", summary.Sources)
	}
	if summary.Sources[0].Source != "beta.opml" || summary.Sources[1].Source != "gamma.opml" {
		t.Fatalf("expected sorted sources beta,gamma, got %+v", summary.Sources)
	}
	if summary.Sources[0].Share != 1.0/3.0 || summary.Sources[1].Share != 1.0/3.0 {
		t.Fatalf("unexpected source share values: %+v", summary.Sources)
	}
	if !summary.Sources[0].LatestCheckedAt.Equal(mustDateTime("2026-03-12T09:00:00Z")) || !summary.Sources[1].LatestCheckedAt.Equal(mustDateTime("2026-03-11T09:00:00Z")) {
		t.Fatalf("unexpected latest checked timestamps in sorted sources: %+v", summary.Sources)
	}
	if summary.BySource["alpha.opml"].Total != 1 || summary.BySource["beta.opml"].Total != 1 || summary.BySource["gamma.opml"].Total != 1 {
		t.Fatalf("expected by_source to retain all sources, got %+v", summary.BySource)
	}
}

func TestRunFeedsListJSONIncludesDomain(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{StatePath: filepath.Join(tempDir, "store.db")}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://sub.example.com/feed.xml", SourceFile: "alpha.opml", Status: "ok", SuccessAt: mustDateTime("2026-03-12T08:30:00Z")},
		{URL: "https://other.org/rss.xml", SourceFile: "beta.opml", Status: "inactive", AllowTitleOnly: boolPtr(true), LastError: "request timeout", CheckedAt: mustDateTime("2026-03-10T09:00:00Z")},
		{URL: "https://zzz.net/feed.xml"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return runFeedsList(db, []string{"--sort=domain"})
	})

	var items []feedListItem
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		t.Fatalf("expected valid json output, got %q: %v", output, err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	if items[0].URL != "https://other.org/rss.xml" || items[0].Domain != "other.org" || items[0].Enabled || items[0].CheckedMissing || items[0].SuccessMissing != true || !items[0].HasError || items[0].AllowTitleMode != "true" || items[0].SourceName != "beta.opml" || items[0].StatusLabel != "inactive" {
		t.Fatalf("expected first item domain to be other.org, got %+v", items[0])
	}
	if items[1].URL != "https://sub.example.com/feed.xml" || items[1].Domain != "sub.example.com" || !items[1].Enabled || items[1].CheckedMissing != true || items[1].SuccessMissing || items[1].HasError || items[1].AllowTitleMode != "inherit" || items[1].SourceName != "alpha.opml" || items[1].StatusLabel != "ok" {
		t.Fatalf("expected second item domain to be sub.example.com, got %+v", items[1])
	}
	if items[2].URL != "https://zzz.net/feed.xml" || items[2].SourceName != "unknown" || items[2].StatusLabel != "unknown" {
		t.Fatalf("expected third item to expose unknown source/status, got %+v", items[2])
	}
}

func TestRunFeedsSetSupportsAllBatchUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	fixtures := []model.Feed{
		{URL: "https://example.com/feed.xml", Title: "One", Status: "active"},
		{URL: "https://other.com/feed.xml", Title: "Two", Status: "inactive"},
	}
	if err := db.UpsertFeeds(fixtures); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--all", "--status=inactive", "--allow-title-only=true", "--yes"}); err != nil {
		t.Fatal(err)
	}

	feeds := db.ListFeeds()
	for _, item := range feeds {
		if item.Status != "inactive" {
			t.Fatalf("expected all feeds inactive, got %+v", item)
		}
		if item.AllowTitleOnly == nil || !*item.AllowTitleOnly {
			t.Fatalf("expected all feeds allow_title_only=true, got %+v", item)
		}
	}
}

func TestRunFeedsSetDryRunDoesNotPersist(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/feed.xml", Title: "Example", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	if err := runFeedsSet(cfg, db, []string{"--url=https://example.com/feed.xml", "--status=inactive", "--dry-run"}); err != nil {
		t.Fatal(err)
	}

	got := db.ListFeeds()[0]
	if got.Status != "active" {
		t.Fatalf("expected dry-run to keep original status, got %+v", got)
	}
	if _, err := os.Stat(cfg.FeedsJSONPath); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run to avoid writing feeds.json, got err=%v", err)
	}
}

func TestRunFeedsSetRejectsMultipleTargets(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = runFeedsSet(cfg, db, []string{"--all", "--domain=example.com", "--status=inactive"})
	if err == nil || !strings.Contains(err.Error(), "specify exactly one") {
		t.Fatalf("expected mutually exclusive target error, got %v", err)
	}
}

func TestRunFeedsSetRejectsTitleForBulkUpdate(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = runFeedsSet(cfg, db, []string{"--domain=example.com", "--title=Renamed"})
	if err == nil || !strings.Contains(err.Error(), "--title only supports --url") {
		t.Fatalf("expected title scope error, got %v", err)
	}
}

func TestRunFeedsSetRequiresYesForBulkUpdates(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		StatePath:     filepath.Join(tempDir, "store.db"),
		FeedsJSONPath: filepath.Join(tempDir, "feeds.json"),
	}
	db, err := store.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.UpsertFeed(model.Feed{URL: "https://example.com/feed.xml", Title: "Example", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	err = runFeedsSet(cfg, db, []string{"--domain=example.com", "--status=inactive"})
	if err == nil || !strings.Contains(err.Error(), "require --yes") {
		t.Fatalf("expected --yes guard error, got %v", err)
	}

	err = runFeedsSet(cfg, db, []string{"--source=pubmed.opml", "--status=inactive"})
	if err == nil || !strings.Contains(err.Error(), "require --yes") {
		t.Fatalf("expected source --yes guard error, got %v", err)
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

func timePtr(value time.Time) *time.Time {
	return &value
}

func captureStdout(t *testing.T, fn func() error) string {
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
