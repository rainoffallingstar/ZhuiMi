package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/feed"
	"zhuimi/internal/model"
	"zhuimi/internal/pipeline"
	"zhuimi/internal/store"
)

type feedStatusSummary struct {
	Total           int                         `json:"total"`
	Enabled         int                         `json:"enabled"`
	Inactive        int                         `json:"inactive"`
	SourceCount     int                         `json:"source_count"`
	CheckedMissing  int                         `json:"checked_missing"`
	SuccessMissing  int                         `json:"success_missing"`
	ErrorPresent    int                         `json:"error_present"`
	LatestCheckedAt time.Time                   `json:"latest_checked_at,omitempty"`
	LatestSuccessAt time.Time                   `json:"latest_success_at,omitempty"`
	ErrorRate       float64                     `json:"error_rate"`
	ErrorInfoRate   float64                     `json:"error_present_rate"`
	CheckedRate     float64                     `json:"checked_missing_rate"`
	SuccessRate     float64                     `json:"success_missing_rate"`
	ByStatus        map[string]int              `json:"by_status"`
	ByTitleOnly     map[string]int              `json:"by_title_only"`
	BySource        map[string]feedSourceStatus `json:"by_source,omitempty"`
	Sources         []namedFeedSourceStatus     `json:"sources,omitempty"`
}

type feedSourceStatus struct {
	Total           int            `json:"total"`
	Enabled         int            `json:"enabled"`
	Inactive        int            `json:"inactive"`
	Share           float64        `json:"share"`
	CheckedMissing  int            `json:"checked_missing"`
	SuccessMissing  int            `json:"success_missing"`
	ErrorPresent    int            `json:"error_present"`
	LatestCheckedAt time.Time      `json:"latest_checked_at,omitempty"`
	LatestSuccessAt time.Time      `json:"latest_success_at,omitempty"`
	ErrorRate       float64        `json:"error_rate"`
	ErrorInfoRate   float64        `json:"error_present_rate"`
	CheckedRate     float64        `json:"checked_missing_rate"`
	SuccessRate     float64        `json:"success_missing_rate"`
	ByStatus        map[string]int `json:"by_status"`
	ByTitleOnly     map[string]int `json:"by_title_only"`
}

type namedFeedSourceStatus struct {
	Source string `json:"source"`
	feedSourceStatus
}

type feedListItem struct {
	model.Feed
	Domain         string `json:"domain,omitempty"`
	Enabled        bool   `json:"enabled"`
	CheckedMissing bool   `json:"checked_missing"`
	SuccessMissing bool   `json:"success_missing"`
	HasError       bool   `json:"has_error"`
	AllowTitleMode string `json:"allow_title_mode"`
	SourceName     string `json:"source_name"`
	StatusLabel    string `json:"status_label"`
}

type articleListItem struct {
	model.Article
	Review     *articleReviewSummary              `json:"review,omitempty"`
	Content    *articleContentSummary             `json:"content,omitempty"`
	Processors map[string]articleProcessorSummary `json:"processors,omitempty"`
}

type articleContentSummary struct {
	FetchStatus string `json:"fetch_status,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	ResolvedURL string `json:"resolved_url,omitempty"`
	HasText     bool   `json:"has_text"`
}

type articleProcessorSummary struct {
	Status      string          `json:"status,omitempty"`
	Model       string          `json:"model,omitempty"`
	ProcessedAt time.Time       `json:"processed_at,omitempty"`
	Error       string          `json:"error,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
}

type articleReviewSummary struct {
	ResearchValue       int    `json:"research_value"`
	AMLValue            int    `json:"aml_value"`
	ReusabilityValue    int    `json:"reusability_value"`
	TotalScore          int    `json:"total_score"`
	RecommendationLevel string `json:"recommendation_level,omitempty"`
	Summary             string `json:"summary,omitempty"`
}

type articleListFilters struct {
	MinTotal        int
	MinAML          int
	Level           string
	ContentStatus   string
	Processor       string
	ProcessorStatus string
}

type articleTopItem struct {
	Rank                int      `json:"rank"`
	Title               string   `json:"title"`
	DOI                 string   `json:"doi,omitempty"`
	PublishedDate       string   `json:"published_date,omitempty"`
	TotalScore          int      `json:"total_score"`
	AMLValue            int      `json:"aml_value"`
	ResearchValue       int      `json:"research_value"`
	ReusabilityValue    int      `json:"reusability_value"`
	RecommendationLevel string   `json:"recommendation_level,omitempty"`
	Summary             string   `json:"summary,omitempty"`
	Link                string   `json:"link,omitempty"`
	ReportDates         []string `json:"report_dates,omitempty"`
	ID                  string   `json:"id"`
}

type feedAllowTitleOnlyFilter struct {
	set   bool
	value *bool
}

func main() {
	ctx := context.Background()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	args := os.Args[1:]
	var runErr error

	switch {
	case len(args) == 2 && args[0] == "feeds" && args[1] == "import":
		runErr = feed.ImportOPMLFeeds(cfg, db)
	case len(args) >= 2 && args[0] == "feeds" && args[1] == "list":
		runErr = runFeedsList(db, args[2:])
	case len(args) >= 2 && args[0] == "feeds" && args[1] == "set":
		runErr = runFeedsSet(cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "feeds" && args[1] == "status":
		runErr = runFeedsStatus(db, args[2:])
	case len(args) >= 2 && args[0] == "run" && args[1] == "daily":
		runErr = runDaily(ctx, cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "db" && args[1] == "stats":
		runErr = runDBStats(db, args[2:])
	case len(args) >= 2 && args[0] == "articles" && args[1] == "list":
		runErr = runArticlesList(db, args[2:])
	case len(args) >= 2 && args[0] == "articles" && args[1] == "top":
		runErr = runArticlesTop(db, args[2:])
	case len(args) >= 2 && args[0] == "report" && args[1] == "rebuild":
		runErr = runReportRebuild(cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "run" && args[1] == "backfill":
		runErr = runBackfill(ctx, cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "run" && args[1] == "fetch-content":
		runErr = runFetchContent(ctx, cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "run" && args[1] == "ai":
		runErr = runAI(ctx, cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "retry" && args[1] == "failed-scores":
		runErr = runRetryFailedScores(ctx, cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "report" && args[1] == "prune":
		runErr = runReportPrune(cfg, db, args[2:])
	case len(args) == 2 && args[0] == "db" && args[1] == "vacuum":
		runErr = db.Vacuum()
	case len(args) == 2 && args[0] == "migrate" && args[1] == "legacy":
		runErr = pipeline.MigrateLegacy(cfg, db)
	default:
		printUsage()
		os.Exit(1)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", runErr)
		os.Exit(1)
	}
}

func runDaily(ctx context.Context, cfg config.Config, db *store.Store, args []string) error {
	opts, rest, err := parseRunOptions(args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unknown run daily arg: %s", rest[0])
	}
	return pipeline.RunDaily(ctx, cfg, db, opts)
}

func runDBStats(db *store.Store, args []string) error {
	asJSON := false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		default:
			return fmt.Errorf("unknown db stats arg: %s", arg)
		}
	}

	stats := db.Stats()
	if asJSON {
		return printJSON(stats)
	}
	fmt.Printf("feeds=%d articles=%d content_fetched=%d content_fallbacks=%d content_missing=%d scored=%d ai_results=%d reports=%d runs=%d\n",
		stats.Feeds,
		stats.Articles,
		stats.ContentFetched,
		stats.ContentFallbacks,
		stats.ContentMissing,
		stats.ScoredArticles,
		stats.AIResults,
		stats.Reports,
		stats.Runs,
	)
	return nil
}

func runFeedsList(db *store.Store, args []string) error {
	var status string
	domain := ""
	source := ""
	query := ""
	errorQuery := ""
	sortBy := "url"
	reverse := false
	limit := 0
	var checkedMissingFilter *bool
	var checkedAfterFilter *time.Time
	var checkedBeforeFilter *time.Time
	var errorMissingFilter *bool
	var successMissingFilter *bool
	var successAfterFilter *time.Time
	var successBeforeFilter *time.Time
	var enabledFilter *bool
	var allowTitleOnlyFilter feedAllowTitleOnlyFilter
	asTable := false
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--status="):
			status = strings.TrimSpace(strings.TrimPrefix(arg, "--status="))
		case strings.HasPrefix(arg, "--domain="):
			domain = strings.TrimSpace(strings.TrimPrefix(arg, "--domain="))
		case strings.HasPrefix(arg, "--source="):
			source = strings.TrimSpace(strings.TrimPrefix(arg, "--source="))
		case strings.HasPrefix(arg, "--q="):
			query = strings.TrimSpace(strings.TrimPrefix(arg, "--q="))
		case strings.HasPrefix(arg, "--error-q="):
			errorQuery = strings.TrimSpace(strings.TrimPrefix(arg, "--error-q="))
		case strings.HasPrefix(arg, "--sort="):
			parsed, err := parseFeedSort(strings.TrimSpace(strings.TrimPrefix(arg, "--sort=")))
			if err != nil {
				return fmt.Errorf("invalid --sort: %w", err)
			}
			sortBy = parsed
		case arg == "--reverse":
			reverse = true
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--limit=")))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		case strings.HasPrefix(arg, "--checked-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-missing: %w", err)
			}
			checkedMissingFilter = &parsed
		case strings.HasPrefix(arg, "--checked-after="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-after=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-after: %w", err)
			}
			checkedAfterFilter = &parsed
		case strings.HasPrefix(arg, "--checked-before="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-before=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-before: %w", err)
			}
			checkedBeforeFilter = &parsed
		case strings.HasPrefix(arg, "--error-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--error-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --error-missing: %w", err)
			}
			errorMissingFilter = &parsed
		case strings.HasPrefix(arg, "--success-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--success-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --success-missing: %w", err)
			}
			successMissingFilter = &parsed
		case strings.HasPrefix(arg, "--success-after="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--success-after=")))
			if err != nil {
				return fmt.Errorf("invalid --success-after: %w", err)
			}
			successAfterFilter = &parsed
		case strings.HasPrefix(arg, "--success-before="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--success-before=")))
			if err != nil {
				return fmt.Errorf("invalid --success-before: %w", err)
			}
			successBeforeFilter = &parsed
		case strings.HasPrefix(arg, "--enabled="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--enabled=")))
			if err != nil {
				return fmt.Errorf("invalid --enabled: %w", err)
			}
			enabledFilter = &parsed
		case strings.HasPrefix(arg, "--allow-title-only="):
			parsed, err := parseOptionalBool(strings.TrimSpace(strings.TrimPrefix(arg, "--allow-title-only=")))
			if err != nil {
				return fmt.Errorf("invalid --allow-title-only: %w", err)
			}
			allowTitleOnlyFilter = feedAllowTitleOnlyFilter{set: true, value: parsed}
		case arg == "--table":
			asTable = true
		default:
			return fmt.Errorf("unknown feeds list arg: %s", arg)
		}
	}

	feeds := filterFeedsWithErrorQuery(db.ListFeeds(), status, domain, source, query, errorQuery, sortBy, reverse, enabledFilter, allowTitleOnlyFilter, feedTimeMissingFilter{
		checked:       checkedMissingFilter,
		checkedAfter:  checkedAfterFilter,
		checkedBefore: checkedBeforeFilter,
		lastError:     errorMissingFilter,
		success:       successMissingFilter,
		successAfter:  successAfterFilter,
		successBefore: successBeforeFilter,
	})
	feeds = limitFeeds(feeds, limit)
	if asTable {
		return printFeedsTable(os.Stdout, feeds)
	}
	return printJSON(buildFeedListOutput(feeds))
}

func runFeedsStatus(db *store.Store, args []string) error {
	asTable := false
	status := ""
	domain := ""
	source := ""
	query := ""
	errorQuery := ""
	sortBy := "source"
	reverse := false
	limit := 0
	var checkedMissingFilter *bool
	var checkedAfterFilter *time.Time
	var checkedBeforeFilter *time.Time
	var errorMissingFilter *bool
	var successMissingFilter *bool
	var successAfterFilter *time.Time
	var successBeforeFilter *time.Time
	var enabledFilter *bool
	var allowTitleOnlyFilter feedAllowTitleOnlyFilter
	for _, arg := range args {
		switch {
		case arg == "--table":
			asTable = true
		case arg == "--reverse":
			reverse = true
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(arg, "--limit=")))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		case strings.HasPrefix(arg, "--sort="):
			parsed, err := parseFeedStatusSort(strings.TrimSpace(strings.TrimPrefix(arg, "--sort=")))
			if err != nil {
				return fmt.Errorf("invalid --sort: %w", err)
			}
			sortBy = parsed
		case strings.HasPrefix(arg, "--status="):
			parsed, err := parseFeedStatusFilter(strings.TrimSpace(strings.TrimPrefix(arg, "--status=")))
			if err != nil {
				return fmt.Errorf("invalid --status: %w", err)
			}
			status = parsed
		case strings.HasPrefix(arg, "--domain="):
			domain = strings.TrimSpace(strings.TrimPrefix(arg, "--domain="))
		case strings.HasPrefix(arg, "--source="):
			source = strings.TrimSpace(strings.TrimPrefix(arg, "--source="))
		case strings.HasPrefix(arg, "--q="):
			query = strings.TrimSpace(strings.TrimPrefix(arg, "--q="))
		case strings.HasPrefix(arg, "--error-q="):
			errorQuery = strings.TrimSpace(strings.TrimPrefix(arg, "--error-q="))
		case strings.HasPrefix(arg, "--checked-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-missing: %w", err)
			}
			checkedMissingFilter = &parsed
		case strings.HasPrefix(arg, "--checked-after="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-after=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-after: %w", err)
			}
			checkedAfterFilter = &parsed
		case strings.HasPrefix(arg, "--checked-before="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--checked-before=")))
			if err != nil {
				return fmt.Errorf("invalid --checked-before: %w", err)
			}
			checkedBeforeFilter = &parsed
		case strings.HasPrefix(arg, "--error-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--error-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --error-missing: %w", err)
			}
			errorMissingFilter = &parsed
		case strings.HasPrefix(arg, "--success-missing="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--success-missing=")))
			if err != nil {
				return fmt.Errorf("invalid --success-missing: %w", err)
			}
			successMissingFilter = &parsed
		case strings.HasPrefix(arg, "--success-after="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--success-after=")))
			if err != nil {
				return fmt.Errorf("invalid --success-after: %w", err)
			}
			successAfterFilter = &parsed
		case strings.HasPrefix(arg, "--success-before="):
			parsed, err := parseFilterDate(strings.TrimSpace(strings.TrimPrefix(arg, "--success-before=")))
			if err != nil {
				return fmt.Errorf("invalid --success-before: %w", err)
			}
			successBeforeFilter = &parsed
		case strings.HasPrefix(arg, "--enabled="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--enabled=")))
			if err != nil {
				return fmt.Errorf("invalid --enabled: %w", err)
			}
			enabledFilter = &parsed
		case strings.HasPrefix(arg, "--allow-title-only="):
			parsed, err := parseOptionalBool(strings.TrimSpace(strings.TrimPrefix(arg, "--allow-title-only=")))
			if err != nil {
				return fmt.Errorf("invalid --allow-title-only: %w", err)
			}
			allowTitleOnlyFilter = feedAllowTitleOnlyFilter{set: true, value: parsed}
		default:
			return fmt.Errorf("unknown feeds status arg: %s", arg)
		}
	}
	feeds := filterFeedsWithErrorQuery(db.ListFeeds(), status, domain, source, query, errorQuery, "url", false, enabledFilter, allowTitleOnlyFilter, feedTimeMissingFilter{
		checked:       checkedMissingFilter,
		checkedAfter:  checkedAfterFilter,
		checkedBefore: checkedBeforeFilter,
		lastError:     errorMissingFilter,
		success:       successMissingFilter,
		successAfter:  successAfterFilter,
		successBefore: successBeforeFilter,
	})
	summary := buildFeedStatusSummary(feeds)
	summary.Sources = limitFeedSourceStatuses(sortedFeedSourceStatuses(summary.BySource, sortBy, reverse), limit)
	if asTable {
		return printFeedStatusTable(os.Stdout, summary, sortBy, reverse, limit)
	}
	return printJSON(summary)
}

func runFeedsSet(cfg config.Config, db *store.Store, args []string) error {
	url := ""
	domain := ""
	source := ""
	all := false
	dryRun := false
	confirmed := false
	title := ""
	titleSet := false
	var allowTitleOnly *bool
	status := ""
	changed := false
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--url="):
			url = strings.TrimSpace(strings.TrimPrefix(arg, "--url="))
		case strings.HasPrefix(arg, "--domain="):
			domain = strings.TrimSpace(strings.TrimPrefix(arg, "--domain="))
		case strings.HasPrefix(arg, "--source="):
			source = strings.TrimSpace(strings.TrimPrefix(arg, "--source="))
		case arg == "--all":
			all = true
		case arg == "--dry-run":
			dryRun = true
		case arg == "--yes":
			confirmed = true
		case strings.HasPrefix(arg, "--title="):
			title = strings.TrimSpace(strings.TrimPrefix(arg, "--title="))
			titleSet = true
			changed = true
		case strings.HasPrefix(arg, "--allow-title-only="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--allow-title-only="))
			parsed, err := parseOptionalBool(value)
			if err != nil {
				return fmt.Errorf("invalid --allow-title-only: %w", err)
			}
			allowTitleOnly = parsed
			changed = true
		case strings.HasPrefix(arg, "--status="):
			parsed, err := parseFeedStatus(strings.TrimSpace(strings.TrimPrefix(arg, "--status=")))
			if err != nil {
				return fmt.Errorf("invalid --status: %w", err)
			}
			status = parsed
			changed = true
		default:
			return fmt.Errorf("unknown feeds set arg: %s", arg)
		}
	}
	targets := 0
	if url != "" {
		targets++
	}
	if domain != "" {
		targets++
	}
	if source != "" {
		targets++
	}
	if all {
		targets++
	}
	if targets != 1 {
		return fmt.Errorf("specify exactly one of --url, --domain, --source, or --all")
	}
	if titleSet && url == "" {
		return fmt.Errorf("--title only supports --url updates")
	}
	if !changed {
		return fmt.Errorf("no feed properties specified")
	}
	if !dryRun && url == "" && (domain != "" || source != "" || all) && !confirmed {
		return fmt.Errorf("bulk updates require --yes; use --dry-run to preview changes first")
	}

	feeds := db.ListFeeds()
	updated := make([]model.Feed, 0)
	for i, item := range feeds {
		if url != "" && item.URL != url {
			continue
		}
		if domain != "" && !feedMatchesDomain(item.URL, domain) {
			continue
		}
		if source != "" && !feedSourceEquals(item.SourceFile, source) {
			continue
		}
		if !all && url == "" && domain == "" && source == "" {
			continue
		}
		if titleSet {
			feeds[i].Title = title
		}
		feeds[i].AllowTitleOnly = allowTitleOnly
		if status != "" {
			feeds[i].Status = status
		}
		updated = append(updated, feeds[i])
	}
	if len(updated) == 0 {
		if url != "" {
			return fmt.Errorf("feed not found: %s", url)
		}
		if all {
			return fmt.Errorf("no feeds found")
		}
		if source != "" {
			return fmt.Errorf("no feeds found for source: %s", source)
		}
		return fmt.Errorf("no feeds found for domain: %s", domain)
	}
	if dryRun {
		return printJSON(map[string]any{
			"dry_run": true,
			"count":   len(updated),
			"feeds":   updated,
		})
	}

	if err := db.UpsertFeeds(updated); err != nil {
		return err
	}
	if err := feed.WriteFeedsJSON(cfg, db.ListFeeds()); err != nil {
		return err
	}
	if len(updated) == 1 {
		return printJSON(updated[0])
	}
	return printJSON(updated)
}

func feedSourceEquals(sourceFile, target string) bool {
	sourceFile = strings.TrimSpace(sourceFile)
	target = strings.TrimSpace(target)
	if sourceFile == "" {
		return strings.EqualFold(target, "unknown")
	}
	return strings.EqualFold(sourceFile, target)
}

func normalizedFeedSource(sourceFile string) string {
	sourceFile = strings.TrimSpace(sourceFile)
	if sourceFile == "" {
		return "unknown"
	}
	return sourceFile
}

func feedMatchesDomain(feedURL, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	host := feedURLHostname(feedURL)
	if host == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func feedURLHostname(feedURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(feedURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

type feedTimeMissingFilter struct {
	checked       *bool
	checkedAfter  *time.Time
	checkedBefore *time.Time
	lastError     *bool
	success       *bool
	successAfter  *time.Time
	successBefore *time.Time
}

func filterFeeds(feeds []model.Feed, status, domain, source, query, sortBy string, reverse bool, enabledFilter *bool, allowTitleOnlyFilter feedAllowTitleOnlyFilter, timeMissingFilter feedTimeMissingFilter) []model.Feed {
	return filterFeedsWithErrorQuery(feeds, status, domain, source, query, "", sortBy, reverse, enabledFilter, allowTitleOnlyFilter, timeMissingFilter)
}

func filterFeedsWithErrorQuery(feeds []model.Feed, status, domain, source, query, errorQuery, sortBy string, reverse bool, enabledFilter *bool, allowTitleOnlyFilter feedAllowTitleOnlyFilter, timeMissingFilter feedTimeMissingFilter) []model.Feed {
	filtered := make([]model.Feed, 0, len(feeds))
	for _, item := range feeds {
		if status != "" && !feedMatchesStatus(item.Status, status) {
			continue
		}
		if domain != "" && !feedMatchesDomain(item.URL, domain) {
			continue
		}
		if source != "" && !feedMatchesSource(item.SourceFile, source) {
			continue
		}
		if query != "" && !feedMatchesQuery(item, query) {
			continue
		}
		if errorQuery != "" && !feedMatchesErrorQuery(item.LastError, errorQuery) {
			continue
		}
		if enabledFilter != nil && isFeedEnabled(item) != *enabledFilter {
			continue
		}
		if !matchesAllowTitleOnlyFilter(item, allowTitleOnlyFilter) {
			continue
		}
		if !matchesFeedTimeMissingFilter(item, timeMissingFilter) {
			continue
		}
		filtered = append(filtered, item)
	}
	sortFeeds(filtered, sortBy, reverse)
	return filtered
}

func feedMatchesStatus(feedStatus, filter string) bool {
	actual := normalizedFeedStatus(feedStatus)
	expected := normalizedFeedStatus(filter)
	return strings.EqualFold(actual, expected)
}

func normalizedFeedStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "unknown"
	}
	return status
}

func feedMatchesSource(sourceFile, query string) bool {
	sourceFile = strings.ToLower(strings.TrimSpace(sourceFile))
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if sourceFile == "" {
		return query == "unknown"
	}
	return strings.Contains(sourceFile, query)
}

func sortFeeds(feeds []model.Feed, sortBy string, reverse bool) {
	sort.Slice(feeds, func(i, j int) bool {
		left := feeds[i]
		right := feeds[j]
		less := func(a, b string) bool {
			if reverse {
				return a > b
			}
			return a < b
		}
		lessTime := func(a, b time.Time) bool {
			if reverse {
				return a.After(b)
			}
			return a.Before(b)
		}
		switch sortBy {
		case "checked":
			if !left.CheckedAt.Equal(right.CheckedAt) {
				return lessTime(left.CheckedAt, right.CheckedAt)
			}
		case "checked-missing":
			leftMissing := left.CheckedAt.IsZero()
			rightMissing := right.CheckedAt.IsZero()
			if leftMissing != rightMissing {
				if reverse {
					return leftMissing && !rightMissing
				}
				return !leftMissing && rightMissing
			}
		case "enabled":
			leftEnabled := isFeedEnabled(left)
			rightEnabled := isFeedEnabled(right)
			if leftEnabled != rightEnabled {
				if reverse {
					return leftEnabled && !rightEnabled
				}
				return !leftEnabled && rightEnabled
			}
		case "success-missing":
			leftMissing := left.SuccessAt.IsZero()
			rightMissing := right.SuccessAt.IsZero()
			if leftMissing != rightMissing {
				if reverse {
					return leftMissing && !rightMissing
				}
				return !leftMissing && rightMissing
			}
		case "title-only":
			leftMode := allowTitleOnlyLabel(left.AllowTitleOnly)
			rightMode := allowTitleOnlyLabel(right.AllowTitleOnly)
			if leftMode != rightMode {
				return less(leftMode, rightMode)
			}
		case "has-error":
			leftHasError := strings.TrimSpace(left.LastError) != ""
			rightHasError := strings.TrimSpace(right.LastError) != ""
			if leftHasError != rightHasError {
				if reverse {
					return leftHasError && !rightHasError
				}
				return !leftHasError && rightHasError
			}
		case "domain":
			leftDomain := feedDomain(left.URL)
			rightDomain := feedDomain(right.URL)
			if leftDomain != rightDomain {
				return less(leftDomain, rightDomain)
			}
		case "last-error":
			leftError := strings.ToLower(strings.TrimSpace(left.LastError))
			rightError := strings.ToLower(strings.TrimSpace(right.LastError))
			if leftError != rightError {
				return less(leftError, rightError)
			}
		case "success":
			if !left.SuccessAt.Equal(right.SuccessAt) {
				return lessTime(left.SuccessAt, right.SuccessAt)
			}
		case "source":
			leftSource := strings.ToLower(normalizedFeedSource(left.SourceFile))
			rightSource := strings.ToLower(normalizedFeedSource(right.SourceFile))
			if leftSource != rightSource {
				return less(leftSource, rightSource)
			}
		case "title":
			leftTitle := strings.ToLower(strings.TrimSpace(left.Title))
			rightTitle := strings.ToLower(strings.TrimSpace(right.Title))
			if leftTitle != rightTitle {
				return less(leftTitle, rightTitle)
			}
		case "status":
			leftStatus := strings.ToLower(normalizedFeedStatus(left.Status))
			rightStatus := strings.ToLower(normalizedFeedStatus(right.Status))
			if leftStatus != rightStatus {
				return less(leftStatus, rightStatus)
			}
		default:
			leftURL := strings.ToLower(strings.TrimSpace(left.URL))
			rightURL := strings.ToLower(strings.TrimSpace(right.URL))
			if leftURL != rightURL {
				return less(leftURL, rightURL)
			}
		}
		return less(strings.ToLower(strings.TrimSpace(left.URL)), strings.ToLower(strings.TrimSpace(right.URL)))
	})
}

func limitFeeds(feeds []model.Feed, limit int) []model.Feed {
	if limit <= 0 || len(feeds) <= limit {
		return feeds
	}
	return feeds[:limit]
}

func feedMatchesQuery(feed model.Feed, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(feed.Title))
	url := strings.ToLower(strings.TrimSpace(feed.URL))
	return strings.Contains(title, query) || strings.Contains(url, query)
}

func feedMatchesErrorQuery(lastError, query string) bool {
	lastError = strings.ToLower(strings.TrimSpace(lastError))
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if lastError == "" {
		return false
	}
	return strings.Contains(lastError, query)
}

func buildFeedStatusSummary(feeds []model.Feed) feedStatusSummary {
	summary := feedStatusSummary{
		ByStatus:    map[string]int{},
		ByTitleOnly: map[string]int{},
		BySource:    map[string]feedSourceStatus{},
	}
	for _, item := range feeds {
		summary.Total++
		enabled := isFeedEnabled(item)
		if enabled {
			summary.Enabled++
		} else {
			summary.Inactive++
		}
		if item.CheckedAt.IsZero() {
			summary.CheckedMissing++
		} else if item.CheckedAt.After(summary.LatestCheckedAt) {
			summary.LatestCheckedAt = item.CheckedAt
		}
		if item.SuccessAt.IsZero() {
			summary.SuccessMissing++
		} else if item.SuccessAt.After(summary.LatestSuccessAt) {
			summary.LatestSuccessAt = item.SuccessAt
		}
		if strings.TrimSpace(item.LastError) != "" {
			summary.ErrorPresent++
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "unknown"
		}
		summary.ByStatus[status]++
		titleOnly := allowTitleOnlyLabel(item.AllowTitleOnly)
		summary.ByTitleOnly[titleOnly]++

		source := strings.TrimSpace(item.SourceFile)
		if source == "" {
			source = "unknown"
		}
		sourceSummary := summary.BySource[source]
		if sourceSummary.ByStatus == nil {
			sourceSummary.ByStatus = map[string]int{}
		}
		if sourceSummary.ByTitleOnly == nil {
			sourceSummary.ByTitleOnly = map[string]int{}
		}
		sourceSummary.Total++
		if enabled {
			sourceSummary.Enabled++
		} else {
			sourceSummary.Inactive++
		}
		if item.CheckedAt.IsZero() {
			sourceSummary.CheckedMissing++
		} else if item.CheckedAt.After(sourceSummary.LatestCheckedAt) {
			sourceSummary.LatestCheckedAt = item.CheckedAt
		}
		if item.SuccessAt.IsZero() {
			sourceSummary.SuccessMissing++
		} else if item.SuccessAt.After(sourceSummary.LatestSuccessAt) {
			sourceSummary.LatestSuccessAt = item.SuccessAt
		}
		if strings.TrimSpace(item.LastError) != "" {
			sourceSummary.ErrorPresent++
		}
		sourceSummary.ByStatus[status]++
		sourceSummary.ByTitleOnly[titleOnly]++
		summary.BySource[source] = sourceSummary
	}
	summary.ErrorRate = sourceStatusRate(summary.ByStatus["error"], summary.Total)
	summary.ErrorInfoRate = sourceStatusRate(summary.ErrorPresent, summary.Total)
	summary.CheckedRate = sourceStatusRate(summary.CheckedMissing, summary.Total)
	summary.SuccessRate = sourceStatusRate(summary.SuccessMissing, summary.Total)
	for source, item := range summary.BySource {
		item.Share = sourceStatusRate(item.Total, summary.Total)
		item.ErrorRate = sourceStatusRate(item.ByStatus["error"], item.Total)
		item.ErrorInfoRate = sourceStatusRate(item.ErrorPresent, item.Total)
		item.CheckedRate = sourceStatusRate(item.CheckedMissing, item.Total)
		item.SuccessRate = sourceStatusRate(item.SuccessMissing, item.Total)
		summary.BySource[source] = item
	}
	summary.SourceCount = len(summary.BySource)
	return summary
}

func isFeedEnabled(feed model.Feed) bool {
	return !strings.EqualFold(strings.TrimSpace(feed.Status), "inactive")
}

func matchesAllowTitleOnlyFilter(feed model.Feed, filter feedAllowTitleOnlyFilter) bool {
	if !filter.set {
		return true
	}
	if filter.value == nil {
		return feed.AllowTitleOnly == nil
	}
	return feed.AllowTitleOnly != nil && *feed.AllowTitleOnly == *filter.value
}

func matchesFeedTimeMissingFilter(feed model.Feed, filter feedTimeMissingFilter) bool {
	if filter.checked != nil && feed.CheckedAt.IsZero() != *filter.checked {
		return false
	}
	if filter.checkedAfter != nil && (feed.CheckedAt.IsZero() || !feed.CheckedAt.After(*filter.checkedAfter)) {
		return false
	}
	if filter.checkedBefore != nil && (feed.CheckedAt.IsZero() || !feed.CheckedAt.Before(*filter.checkedBefore)) {
		return false
	}
	if filter.lastError != nil && (strings.TrimSpace(feed.LastError) == "") != *filter.lastError {
		return false
	}
	if filter.success != nil && feed.SuccessAt.IsZero() != *filter.success {
		return false
	}
	if filter.successAfter != nil && (feed.SuccessAt.IsZero() || !feed.SuccessAt.After(*filter.successAfter)) {
		return false
	}
	if filter.successBefore != nil && (feed.SuccessAt.IsZero() || !feed.SuccessAt.Before(*filter.successBefore)) {
		return false
	}
	return true
}

func parseOptionalBool(raw string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inherit", "default", "":
		return nil, nil
	case "true":
		return boolPtr(true), nil
	case "false":
		return boolPtr(false), nil
	default:
		return nil, fmt.Errorf("expected true, false, or inherit")
	}
}

func parseFeedStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active", nil
	case "inactive":
		return "inactive", nil
	default:
		return "", fmt.Errorf("expected active or inactive")
	}
}

func parseFeedStatusFilter(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("expected a non-empty status")
	}
	return raw, nil
}

func parseFilterDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD")
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD")
	}
	return parsed.UTC(), nil
}

func parseFeedSort(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "url":
		return "url", nil
	case "checked", "checked-at", "latest-checked", "latest_checked_at":
		return "checked", nil
	case "checked-missing", "checked_missing":
		return "checked-missing", nil
	case "enabled":
		return "enabled", nil
	case "title-only", "allow-title-only", "allow_title_mode":
		return "title-only", nil
	case "has-error", "has_error", "error-present":
		return "has-error", nil
	case "domain":
		return "domain", nil
	case "last-error", "last_error", "error":
		return "last-error", nil
	case "source", "source_name":
		return "source", nil
	case "success", "success-at", "latest-success", "latest_success_at":
		return "success", nil
	case "success-missing", "success_missing":
		return "success-missing", nil
	case "source-name":
		return "source", nil
	case "status-label":
		return "status", nil
	case "title":
		return "title", nil
	case "status":
		return "status", nil
	default:
		return "", fmt.Errorf("expected checked, checked-missing, domain, enabled, title-only, has-error, last-error, source, success, success-missing, title, url, or status")
	}
}

func feedDomain(rawURL string) string {
	return feedURLHostname(rawURL)
}

func buildFeedListOutput(feeds []model.Feed) []feedListItem {
	items := make([]feedListItem, 0, len(feeds))
	for _, item := range feeds {
		items = append(items, feedListItem{
			Feed:           item,
			Domain:         feedDomain(item.URL),
			Enabled:        isFeedEnabled(item),
			CheckedMissing: item.CheckedAt.IsZero(),
			SuccessMissing: item.SuccessAt.IsZero(),
			HasError:       strings.TrimSpace(item.LastError) != "",
			AllowTitleMode: allowTitleOnlyLabel(item.AllowTitleOnly),
			SourceName:     normalizedFeedSource(item.SourceFile),
			StatusLabel:    normalizedFeedStatus(item.Status),
		})
	}
	return items
}

func parseFeedStatusSort(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "source", "source-name", "source_name":
		return "source", nil
	case "checked", "checked-at", "latest-checked", "latest_checked_at":
		return "checked", nil
	case "checked-missing", "checked_missing":
		return "checked-missing", nil
	case "success", "success-at", "latest-success", "latest_success_at":
		return "success", nil
	case "checked-missing-rate", "checked_missing_rate":
		return "checked-missing-rate", nil
	case "success-missing", "success_missing":
		return "success-missing", nil
	case "success-missing-rate", "success_missing_rate":
		return "success-missing-rate", nil
	case "error-present", "error_present", "error-info":
		return "error-present", nil
	case "error-present-rate", "error_present_rate", "error-info-rate":
		return "error-present-rate", nil
	case "total", "share", "source-share":
		return "total", nil
	case "enabled":
		return "enabled", nil
	case "inactive":
		return "inactive", nil
	case "error-rate":
		return "status-rate:error", nil
	case "error":
		return "status:error", nil
	default:
		if strings.HasPrefix(normalized, "status:") {
			status := strings.TrimSpace(strings.TrimPrefix(normalized, "status:"))
			if status == "" {
				return "", fmt.Errorf("expected status:<name>")
			}
			return "status:" + status, nil
		}
		if strings.HasPrefix(normalized, "status-rate:") {
			status := strings.TrimSpace(strings.TrimPrefix(normalized, "status-rate:"))
			if status == "" {
				return "", fmt.Errorf("expected status-rate:<name>")
			}
			return "status-rate:" + status, nil
		}
		if strings.HasPrefix(normalized, "title-only:") {
			key := strings.TrimSpace(strings.TrimPrefix(normalized, "title-only:"))
			switch key {
			case "true", "false", "inherit":
				return "title-only:" + key, nil
			default:
				return "", fmt.Errorf("expected title-only:true, title-only:false, or title-only:inherit")
			}
		}
		if strings.HasPrefix(normalized, "title-only-rate:") {
			key := strings.TrimSpace(strings.TrimPrefix(normalized, "title-only-rate:"))
			switch key {
			case "true", "false", "inherit":
				return "title-only-rate:" + key, nil
			default:
				return "", fmt.Errorf("expected title-only-rate:true, title-only-rate:false, or title-only-rate:inherit")
			}
		}
		return "", fmt.Errorf("expected source, checked, checked-missing, checked-missing-rate, success, success-missing, success-missing-rate, error-present, error-present-rate, total, enabled, inactive, error-rate, error, status:<name>, status-rate:<name>, title-only:true|false|inherit, or title-only-rate:true|false|inherit")
	}
}

func parseRunOptions(args []string) (pipeline.RunOptions, []string, error) {
	opts := pipeline.RunOptions{}
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--skip-scoring":
			opts.SkipScoring = true
		case "--pdf":
			opts.CompilePDF = true
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func runArticlesList(db *store.Store, args []string) error {
	opts := store.ListArticlesOptions{Limit: 20}
	filters := articleListFilters{}
	requestedLimit := opts.Limit
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			limit, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			opts.Limit = limit
			requestedLimit = limit
		case strings.HasPrefix(arg, "--status="):
			opts.Status = strings.TrimSpace(strings.TrimPrefix(arg, "--status="))
		case strings.HasPrefix(arg, "--report-date="):
			opts.ReportDate = strings.TrimSpace(strings.TrimPrefix(arg, "--report-date="))
		case strings.HasPrefix(arg, "--min-total="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--min-total="))
			if err != nil {
				return fmt.Errorf("invalid --min-total: %w", err)
			}
			filters.MinTotal = value
		case strings.HasPrefix(arg, "--min-aml="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--min-aml="))
			if err != nil {
				return fmt.Errorf("invalid --min-aml: %w", err)
			}
			filters.MinAML = value
		case strings.HasPrefix(arg, "--level="):
			filters.Level = strings.TrimSpace(strings.TrimPrefix(arg, "--level="))
		case strings.HasPrefix(arg, "--content-status="):
			filters.ContentStatus = strings.TrimSpace(strings.TrimPrefix(arg, "--content-status="))
		case strings.HasPrefix(arg, "--processor="):
			filters.Processor = strings.TrimSpace(strings.TrimPrefix(arg, "--processor="))
		case strings.HasPrefix(arg, "--processor-status="):
			filters.ProcessorStatus = strings.TrimSpace(strings.TrimPrefix(arg, "--processor-status="))
		default:
			return fmt.Errorf("unknown articles list arg: %s", arg)
		}
	}
	if hasArticleListFilters(filters) && opts.Limit < 10000 {
		opts.Limit = 10000
	}

	articles, err := db.ListArticles(opts)
	if err != nil {
		return err
	}
	items := filterArticleListOutput(buildArticleListOutput(db, articles), filters)
	if requestedLimit > 0 && len(items) > requestedLimit {
		items = items[:requestedLimit]
	}
	return printJSON(items)
}

func hasArticleListFilters(filters articleListFilters) bool {
	return filters.MinTotal > 0 || filters.MinAML > 0 || strings.TrimSpace(filters.Level) != "" || strings.TrimSpace(filters.ContentStatus) != "" || strings.TrimSpace(filters.Processor) != "" || strings.TrimSpace(filters.ProcessorStatus) != ""
}

func filterArticleListOutput(items []articleListItem, filters articleListFilters) []articleListItem {
	if !hasArticleListFilters(filters) {
		return items
	}
	filtered := make([]articleListItem, 0, len(items))
	level := strings.TrimSpace(filters.Level)
	contentStatus := model.NormalizeContentStatus(filters.ContentStatus)
	processor := strings.TrimSpace(filters.Processor)
	processorStatus := model.NormalizeProcessorStatus(filters.ProcessorStatus)
	for _, item := range items {
		if item.Review == nil {
			if filters.MinTotal > 0 || filters.MinAML > 0 || level != "" {
				continue
			}
		} else {
			if filters.MinTotal > 0 && item.Review.TotalScore < filters.MinTotal {
				continue
			}
			if filters.MinAML > 0 && item.Review.AMLValue < filters.MinAML {
				continue
			}
			if level != "" && item.Review.RecommendationLevel != level {
				continue
			}
		}
		if processor != "" || processorStatus != "" {
			if !matchesProcessorFilter(item, processor, processorStatus) {
				continue
			}
		}
		if contentStatus != "" && !matchesContentFilter(item, contentStatus) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func matchesContentFilter(item articleListItem, status string) bool {
	if item.Content == nil {
		return status == "missing"
	}
	if status == "missing" {
		return false
	}
	return model.NormalizeContentStatus(item.Content.FetchStatus) == status
}

func matchesProcessorFilter(item articleListItem, processor, status string) bool {
	if processor == "" {
		if status == "" {
			return true
		}
		for _, summary := range item.Processors {
			if model.NormalizeProcessorStatus(summary.Status) == status {
				return true
			}
		}
		if status == "missing" {
			return len(item.Processors) == 0
		}
		return false
	}

	summary, ok := item.Processors[processor]
	if status == "missing" {
		return !ok
	}
	if !ok {
		return false
	}
	if status == "" {
		return true
	}
	return model.NormalizeProcessorStatus(summary.Status) == status
}

func buildArticleListOutput(db *store.Store, articles []model.Article) []articleListItem {
	items := make([]articleListItem, 0, len(articles))
	for _, article := range articles {
		item := articleListItem{Article: article, Review: buildArticleReviewSummary(article)}
		if db != nil {
			item.Content = buildArticleContentSummary(db.ArticleContent(article.ID))
			item.Processors = buildArticleProcessorSummaries(db.LatestAIResults(article.ID))
		}
		items = append(items, item)
	}
	return items
}

func buildArticleContentSummary(content *model.ArticleContent) *articleContentSummary {
	if content == nil {
		return nil
	}
	return &articleContentSummary{
		FetchStatus: strings.TrimSpace(content.FetchStatus),
		SourceType:  strings.TrimSpace(content.SourceType),
		ResolvedURL: strings.TrimSpace(content.ResolvedURL),
		HasText:     strings.TrimSpace(content.ContentText) != "",
	}
}

func buildArticleProcessorSummaries(results []model.ProcessorResult) map[string]articleProcessorSummary {
	if len(results) == 0 {
		return nil
	}
	items := make(map[string]articleProcessorSummary, len(results))
	for _, result := range results {
		summary := articleProcessorSummary{
			Status:      strings.TrimSpace(result.Status),
			Model:       strings.TrimSpace(result.Model),
			ProcessedAt: result.ProcessedAt,
			Error:       strings.TrimSpace(result.ErrorMessage),
		}
		if strings.TrimSpace(result.OutputJSON) != "" {
			summary.Output = json.RawMessage(result.OutputJSON)
		}
		items[result.Processor] = summary
	}
	return items
}

func buildArticleReviewSummary(article model.Article) *articleReviewSummary {
	if article.LatestScore == nil {
		return nil
	}
	reason := strings.TrimSpace(article.Reason)
	if reason == "" {
		reason = strings.TrimSpace(article.LatestScore.Reason)
	}
	return &articleReviewSummary{
		ResearchValue:       article.LatestScore.Research,
		AMLValue:            article.LatestScore.Social,
		ReusabilityValue:    article.LatestScore.Blood,
		TotalScore:          article.LatestScore.Recommendation,
		RecommendationLevel: extractRecommendationLevel(reason, article.LatestScore.Recommendation),
		Summary:             normalizeReviewSummary(reason),
	}
}

func extractRecommendationLevel(reason string, totalScore int) string {
	reason = strings.TrimSpace(reason)
	for _, part := range strings.Split(reason, "；") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "推荐等级：") {
			return strings.TrimSpace(strings.TrimPrefix(part, "推荐等级："))
		}
	}
	switch {
	case totalScore >= 85:
		return "强烈推荐"
	case totalScore >= 75:
		return "推荐"
	case totalScore >= 65:
		return "有条件推荐"
	case totalScore >= 50:
		return "一般"
	default:
		return "不推荐"
	}
}

func normalizeReviewSummary(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, part := range strings.Split(reason, "；") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "推荐等级：") || strings.HasPrefix(part, "置信度：") {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return reason
	}
	return strings.Join(parts, "；")
}

func runArticlesTop(db *store.Store, args []string) error {
	filters := articleListFilters{}
	opts := store.ListArticlesOptions{Limit: 10000, Status: "scored"}
	limit := 10
	asTable := false
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		case strings.HasPrefix(arg, "--date="):
			opts.ReportDate = strings.TrimSpace(strings.TrimPrefix(arg, "--date="))
		case strings.HasPrefix(arg, "--report-date="):
			opts.ReportDate = strings.TrimSpace(strings.TrimPrefix(arg, "--report-date="))
		case strings.HasPrefix(arg, "--min-total="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--min-total="))
			if err != nil {
				return fmt.Errorf("invalid --min-total: %w", err)
			}
			filters.MinTotal = value
		case strings.HasPrefix(arg, "--min-aml="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--min-aml="))
			if err != nil {
				return fmt.Errorf("invalid --min-aml: %w", err)
			}
			filters.MinAML = value
		case strings.HasPrefix(arg, "--level="):
			filters.Level = strings.TrimSpace(strings.TrimPrefix(arg, "--level="))
		case arg == "--table":
			asTable = true
		default:
			return fmt.Errorf("unknown articles top arg: %s", arg)
		}
	}
	articles, err := db.ListArticles(opts)
	if err != nil {
		return err
	}
	items := filterArticleListOutput(buildArticleListOutput(db, articles), filters)
	sortArticleTopItems(items)
	top := buildArticleTopOutput(items, limit)
	if asTable {
		return printArticleTopTable(os.Stdout, top)
	}
	return printJSON(top)
}

func sortArticleTopItems(items []articleListItem) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i].Review
		right := items[j].Review
		if left == nil || right == nil {
			return right != nil
		}
		if left.TotalScore != right.TotalScore {
			return left.TotalScore > right.TotalScore
		}
		if left.AMLValue != right.AMLValue {
			return left.AMLValue > right.AMLValue
		}
		if left.ResearchValue != right.ResearchValue {
			return left.ResearchValue > right.ResearchValue
		}
		leftDate := publishedDateString(items[i].PublishedAt)
		rightDate := publishedDateString(items[j].PublishedAt)
		if leftDate != rightDate {
			return leftDate > rightDate
		}
		return items[i].ID > items[j].ID
	})
}

func printArticleTopTable(w io.Writer, items []articleTopItem) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Rank	Total	AML	Level	Date	Title	Summary"); err != nil {
		return err
	}
	for _, item := range items {
		title := truncateDisplay(item.Title, 60)
		summary := truncateDisplay(item.Summary, 80)
		if _, err := fmt.Fprintf(tw, "%d\t%d\t%d\t%s\t%s\t%s\t%s\n", item.Rank, item.TotalScore, item.AMLValue, item.RecommendationLevel, defaultDisplay(item.PublishedDate, "-"), title, summary); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func truncateDisplay(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	return value[:limit-1] + "…"
}

func defaultDisplay(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func printFeedsTable(w io.Writer, feeds []model.Feed) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Enabled\tStatus\tChecked\tSuccess\tError\tTitleOnly\tDomain\tSource\tTitle\tURL"); err != nil {
		return err
	}
	for _, item := range feeds {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			boolLabel(isFeedEnabled(item)),
			defaultDisplay(strings.TrimSpace(item.Status), "unknown"),
			formatFeedTimestamp(item.CheckedAt),
			formatFeedTimestamp(item.SuccessAt),
			formatFeedError(item.LastError),
			allowTitleOnlyLabel(item.AllowTitleOnly),
			defaultDisplay(feedDomain(item.URL), "-"),
			normalizedFeedSource(item.SourceFile),
			truncateDisplay(item.Title, 40),
			item.URL,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatFeedTimestamp(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02 15:04")
}

func formatFeedError(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return truncateDisplay(value, 48)
}

func printFeedStatusTable(w io.Writer, summary feedStatusSummary, sortBy string, reverse bool, limit int) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Source\tTotal\tShare\tChecked\tSuccess\tErrors\tErrorRate\tErrInfo\tErrInfoRate\tChkMiss\tChkMissRate\tSucMiss\tSucMissRate\tEnabled\tInactive\tStatuses\tTitleOnly"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		tw,
		"%s\t%d\t%s\t%s\t%s\t%d\t%s\t%d\t%s\t%d\t%s\t%d\t%s\t%d\t%d\t%s\t%s\n",
		"TOTAL",
		summary.Total,
		formatShare(summary.Total, summary.Total),
		formatFeedTimestamp(summary.LatestCheckedAt),
		formatFeedTimestamp(summary.LatestSuccessAt),
		summary.ByStatus["error"],
		formatRate(summary.ByStatus["error"], summary.Total),
		summary.ErrorPresent,
		formatRate(summary.ErrorPresent, summary.Total),
		summary.CheckedMissing,
		formatRate(summary.CheckedMissing, summary.Total),
		summary.SuccessMissing,
		formatRate(summary.SuccessMissing, summary.Total),
		summary.Enabled,
		summary.Inactive,
		formatStatusCounts(summary.ByStatus),
		formatTitleOnlyCounts(summary.ByTitleOnly),
	); err != nil {
		return err
	}

	rows := summary.Sources
	if len(rows) == 0 {
		rows = limitFeedSourceStatuses(sortedFeedSourceStatuses(summary.BySource, sortBy, reverse), limit)
	}
	for _, item := range rows {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%d\t%s\t%s\t%s\t%d\t%s\t%d\t%s\t%d\t%s\t%d\t%s\t%d\t%d\t%s\t%s\n",
			item.Source,
			item.Total,
			formatShare(item.Total, summary.Total),
			formatFeedTimestamp(item.LatestCheckedAt),
			formatFeedTimestamp(item.LatestSuccessAt),
			item.ByStatus["error"],
			formatRate(item.ByStatus["error"], item.Total),
			item.ErrorPresent,
			formatRate(item.ErrorPresent, item.Total),
			item.CheckedMissing,
			formatRate(item.CheckedMissing, item.Total),
			item.SuccessMissing,
			formatRate(item.SuccessMissing, item.Total),
			item.Enabled,
			item.Inactive,
			formatStatusCounts(item.ByStatus),
			formatTitleOnlyCounts(item.ByTitleOnly),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatShare(part, total int) string {
	if total <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)*100/float64(total))
}

func formatRate(count, total int) string {
	return fmt.Sprintf("%.1f%%", sourceStatusRate(count, total)*100)
}

func limitFeedSourceStatuses(items []namedFeedSourceStatus, limit int) []namedFeedSourceStatus {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func sortedFeedSourceStatuses(items map[string]feedSourceStatus, sortBy string, reverse bool) []namedFeedSourceStatus {
	list := make([]namedFeedSourceStatus, 0, len(items))
	for source, item := range items {
		list = append(list, namedFeedSourceStatus{Source: source, feedSourceStatus: item})
	}
	sort.Slice(list, func(i, j int) bool {
		left := list[i]
		right := list[j]
		lessText := func(a, b string) bool {
			if reverse {
				return a > b
			}
			return a < b
		}
		lessInt := func(a, b int) bool {
			if reverse {
				return a > b
			}
			return a < b
		}
		lessFloat := func(a, b float64) bool {
			if reverse {
				return a > b
			}
			return a < b
		}
		lessTime := func(a, b time.Time) bool {
			if reverse {
				return a.After(b)
			}
			return a.Before(b)
		}
		switch sortBy {
		case "checked":
			if !left.LatestCheckedAt.Equal(right.LatestCheckedAt) {
				return lessTime(left.LatestCheckedAt, right.LatestCheckedAt)
			}
		case "checked-missing":
			if left.CheckedMissing != right.CheckedMissing {
				return lessInt(left.CheckedMissing, right.CheckedMissing)
			}
		case "checked-missing-rate":
			if leftRate, rightRate := sourceStatusRate(left.CheckedMissing, left.Total), sourceStatusRate(right.CheckedMissing, right.Total); leftRate != rightRate {
				return lessFloat(leftRate, rightRate)
			}
		case "success":
			if !left.LatestSuccessAt.Equal(right.LatestSuccessAt) {
				return lessTime(left.LatestSuccessAt, right.LatestSuccessAt)
			}
		case "success-missing":
			if left.SuccessMissing != right.SuccessMissing {
				return lessInt(left.SuccessMissing, right.SuccessMissing)
			}
		case "success-missing-rate":
			if leftRate, rightRate := sourceStatusRate(left.SuccessMissing, left.Total), sourceStatusRate(right.SuccessMissing, right.Total); leftRate != rightRate {
				return lessFloat(leftRate, rightRate)
			}
		case "error-present":
			if left.ErrorPresent != right.ErrorPresent {
				return lessInt(left.ErrorPresent, right.ErrorPresent)
			}
		case "error-present-rate":
			if leftRate, rightRate := sourceStatusRate(left.ErrorPresent, left.Total), sourceStatusRate(right.ErrorPresent, right.Total); leftRate != rightRate {
				return lessFloat(leftRate, rightRate)
			}
		case "total":
			if left.Total != right.Total {
				return lessInt(left.Total, right.Total)
			}
		case "enabled":
			if left.Enabled != right.Enabled {
				return lessInt(left.Enabled, right.Enabled)
			}
		case "inactive":
			if left.Inactive != right.Inactive {
				return lessInt(left.Inactive, right.Inactive)
			}
		default:
			if strings.HasPrefix(sortBy, "status:") {
				statusKey := strings.TrimSpace(strings.TrimPrefix(sortBy, "status:"))
				leftStatus := left.ByStatus[statusKey]
				rightStatus := right.ByStatus[statusKey]
				if leftStatus != rightStatus {
					return lessInt(leftStatus, rightStatus)
				}
			}
			if strings.HasPrefix(sortBy, "status-rate:") {
				statusKey := strings.TrimSpace(strings.TrimPrefix(sortBy, "status-rate:"))
				leftRate := sourceStatusRate(left.ByStatus[statusKey], left.Total)
				rightRate := sourceStatusRate(right.ByStatus[statusKey], right.Total)
				if leftRate != rightRate {
					return lessFloat(leftRate, rightRate)
				}
			}
			if strings.HasPrefix(sortBy, "title-only:") {
				titleOnlyKey := strings.TrimSpace(strings.TrimPrefix(sortBy, "title-only:"))
				leftCount := left.ByTitleOnly[titleOnlyKey]
				rightCount := right.ByTitleOnly[titleOnlyKey]
				if leftCount != rightCount {
					return lessInt(leftCount, rightCount)
				}
			}
			if strings.HasPrefix(sortBy, "title-only-rate:") {
				titleOnlyKey := strings.TrimSpace(strings.TrimPrefix(sortBy, "title-only-rate:"))
				leftRate := sourceStatusRate(left.ByTitleOnly[titleOnlyKey], left.Total)
				rightRate := sourceStatusRate(right.ByTitleOnly[titleOnlyKey], right.Total)
				if leftRate != rightRate {
					return lessFloat(leftRate, rightRate)
				}
			}
			if left.Source != right.Source {
				return lessText(strings.ToLower(left.Source), strings.ToLower(right.Source))
			}
		}
		return lessText(strings.ToLower(left.Source), strings.ToLower(right.Source))
	})
	return list
}

func sourceStatusRate(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func formatStatusCounts(counts map[string]int) string {
	return formatCountMap(counts)
}

func formatTitleOnlyCounts(counts map[string]int) string {
	return formatCountMap(counts)
}

func formatCountMap(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func allowTitleOnlyLabel(value *bool) string {
	if value == nil {
		return "inherit"
	}
	if *value {
		return "true"
	}
	return "false"
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func buildArticleTopOutput(items []articleListItem, limit int) []articleTopItem {
	if limit <= 0 {
		limit = 10
	}
	if len(items) > limit {
		items = items[:limit]
	}
	output := make([]articleTopItem, 0, len(items))
	for index, item := range items {
		if item.Review == nil {
			continue
		}
		output = append(output, articleTopItem{
			Rank:                index + 1,
			Title:               item.Title,
			DOI:                 item.DOI,
			PublishedDate:       publishedDateString(item.PublishedAt),
			TotalScore:          item.Review.TotalScore,
			AMLValue:            item.Review.AMLValue,
			ResearchValue:       item.Review.ResearchValue,
			ReusabilityValue:    item.Review.ReusabilityValue,
			RecommendationLevel: item.Review.RecommendationLevel,
			Summary:             item.Review.Summary,
			Link:                item.Link,
			ReportDates:         item.ReportDates,
			ID:                  item.ID,
		})
	}
	return output
}

func publishedDateString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02")
}

func runBackfill(ctx context.Context, cfg config.Config, db *store.Store, args []string) error {
	opts, rest, err := parseRunOptions(args)
	if err != nil {
		return err
	}
	days := cfg.DaysWindow
	for _, arg := range rest {
		switch {
		case strings.HasPrefix(arg, "--days="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--days="))
			if err != nil {
				return fmt.Errorf("invalid --days: %w", err)
			}
			days = value
		default:
			return fmt.Errorf("unknown run backfill arg: %s", arg)
		}
	}
	return pipeline.RunBackfill(ctx, cfg, db, days, opts)
}

func runFetchContent(ctx context.Context, cfg config.Config, db *store.Store, args []string) error {
	limit := 50
	force := false
	contentStatus := ""
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		case strings.HasPrefix(arg, "--content-status="):
			contentStatus = strings.TrimSpace(strings.TrimPrefix(arg, "--content-status="))
		case arg == "--force":
			force = true
		default:
			return fmt.Errorf("unknown run fetch-content arg: %s", arg)
		}
	}
	return pipeline.RunFetchContent(ctx, cfg, db, limit, contentStatus, force)
}

func runAI(ctx context.Context, cfg config.Config, db *store.Store, args []string) error {
	limit := 50
	force := false
	processorStatus := ""
	processorNames := make([]string, 0, 2)
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		case strings.HasPrefix(arg, "--processor="):
			processorNames = append(processorNames, strings.TrimSpace(strings.TrimPrefix(arg, "--processor=")))
		case strings.HasPrefix(arg, "--processor-status="):
			processorStatus = strings.TrimSpace(strings.TrimPrefix(arg, "--processor-status="))
		case arg == "--force":
			force = true
		default:
			return fmt.Errorf("unknown run ai arg: %s", arg)
		}
	}
	if len(processorNames) == 0 {
		processorNames = pipeline.CurrentProcessorNames(cfg, pipeline.RunOptions{}, nil)
	}
	return pipeline.RunProcessors(ctx, cfg, db, limit, processorNames, processorStatus, force)
}

func runRetryFailedScores(ctx context.Context, cfg config.Config, db *store.Store, args []string) error {
	limit := 50
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--limit="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return fmt.Errorf("invalid --limit: %w", err)
			}
			limit = value
		default:
			return fmt.Errorf("unknown retry arg: %s", arg)
		}
	}
	return pipeline.RetryFailedScores(ctx, cfg, db, limit)
}

func runReportPrune(cfg config.Config, db *store.Store, args []string) error {
	keepDays := 30
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--keep-days="):
			value, err := strconv.Atoi(strings.TrimPrefix(arg, "--keep-days="))
			if err != nil {
				return fmt.Errorf("invalid --keep-days: %w", err)
			}
			keepDays = value
		default:
			return fmt.Errorf("unknown report prune arg: %s", arg)
		}
	}
	return pipeline.PruneReports(cfg, db, keepDays)
}

func runReportRebuild(cfg config.Config, db *store.Store, args []string) error {
	date := ""
	compilePDF := false
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--date="):
			date = strings.TrimSpace(strings.TrimPrefix(arg, "--date="))
		case arg == "--pdf":
			compilePDF = true
		default:
			return fmt.Errorf("unknown report rebuild arg: %s", arg)
		}
	}
	return pipeline.RebuildReports(cfg, db, date, compilePDF)
}

func printJSON(value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func printUsage() {
	fmt.Println("ZhuiMi Go CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/zhuimi feeds import")
	fmt.Println("  go run ./cmd/zhuimi feeds list [--status=ok|error|inactive|active|not_modified|unknown] [--domain=DOMAIN] [--source=FILE] [--q=KEYWORD] [--error-q=KEYWORD] [--sort=checked|checked-missing|domain|enabled|title-only|has-error|last-error|source|success|success-missing|title|url|status] [--reverse] [--limit=N] [--checked-missing=true|false] [--checked-after=YYYY-MM-DD] [--checked-before=YYYY-MM-DD] [--error-missing=true|false] [--success-missing=true|false] [--success-after=YYYY-MM-DD] [--success-before=YYYY-MM-DD] [--enabled=true|false] [--allow-title-only=true|false|inherit] [--table]")
	fmt.Println("  go run ./cmd/zhuimi feeds set (--url=FEED_URL|--domain=DOMAIN|--source=FILE|--all) [--title=TITLE] [--allow-title-only=true|false|inherit] [--status=active|inactive] [--dry-run] [--yes]")
	fmt.Println("  go run ./cmd/zhuimi feeds status [--status=STATUS] [--domain=DOMAIN] [--source=FILE] [--q=KEYWORD] [--error-q=KEYWORD] [--checked-missing=true|false] [--checked-after=YYYY-MM-DD] [--checked-before=YYYY-MM-DD] [--error-missing=true|false] [--success-missing=true|false] [--success-after=YYYY-MM-DD] [--success-before=YYYY-MM-DD] [--enabled=true|false] [--allow-title-only=true|false|inherit] [--sort=source|checked|checked-missing|checked-missing-rate|success|success-missing|success-missing-rate|error-present|error-present-rate|total|enabled|inactive|error-rate|error|status:NAME|status-rate:NAME|title-only:true|false|inherit|title-only-rate:true|false|inherit] [--reverse] [--limit=N] [--table]")
	fmt.Println("  go run ./cmd/zhuimi run daily [--skip-scoring] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi db stats [--json]")
	fmt.Println("  go run ./cmd/zhuimi db vacuum")
	fmt.Println("  go run ./cmd/zhuimi articles list [--limit=20] [--status=scored|failed|pending] [--report-date=YYYY-MM-DD] [--min-total=75] [--min-aml=20] [--level=推荐] [--content-status=missing|pending|fetched|rss_fallback|failed|skipped] [--processor=NAME] [--processor-status=processed|failed|pending|skipped|missing]")
	fmt.Println("  go run ./cmd/zhuimi articles top [--date=YYYY-MM-DD] [--limit=10] [--min-total=75] [--min-aml=20] [--level=推荐] [--table]")
	fmt.Println("  go run ./cmd/zhuimi run backfill [--days=7] [--skip-scoring] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi run fetch-content [--limit=50] [--content-status=missing|pending|failed|rss_fallback|skipped|fetched] [--force]")
	fmt.Println("  go run ./cmd/zhuimi run ai [--limit=50] [--processor=generic_digest] [--processor=aml_score] [--processor-status=missing|failed|pending|skipped|processed] [--force]")
	fmt.Println("  go run ./cmd/zhuimi retry failed-scores [--limit=50]")
	fmt.Println("  go run ./cmd/zhuimi report prune [--keep-days=30]")
	fmt.Println("  go run ./cmd/zhuimi report rebuild [--date=YYYY-MM-DD] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi migrate legacy")
}
