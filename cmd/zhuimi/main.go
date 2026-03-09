package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Total    int            `json:"total"`
	Enabled  int            `json:"enabled"`
	Inactive int            `json:"inactive"`
	ByStatus map[string]int `json:"by_status"`
}

type articleListItem struct {
	model.Article
	Review *articleReviewSummary `json:"review,omitempty"`
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
	MinTotal int
	MinAML   int
	Level    string
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
	case len(args) == 2 && args[0] == "feeds" && args[1] == "status":
		runErr = runFeedsStatus(db)
	case len(args) >= 2 && args[0] == "run" && args[1] == "daily":
		runErr = runDaily(ctx, cfg, db, args[2:])
	case len(args) == 2 && args[0] == "db" && args[1] == "stats":
		stats := db.Stats()
		fmt.Printf("feeds=%d articles=%d scored=%d reports=%d runs=%d\n", stats.Feeds, stats.Articles, stats.ScoredArticles, stats.Reports, stats.Runs)
	case len(args) >= 2 && args[0] == "articles" && args[1] == "list":
		runErr = runArticlesList(db, args[2:])
	case len(args) >= 2 && args[0] == "articles" && args[1] == "top":
		runErr = runArticlesTop(db, args[2:])
	case len(args) >= 2 && args[0] == "report" && args[1] == "rebuild":
		runErr = runReportRebuild(cfg, db, args[2:])
	case len(args) >= 2 && args[0] == "run" && args[1] == "backfill":
		runErr = runBackfill(ctx, cfg, db, args[2:])
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

func runFeedsList(db *store.Store, args []string) error {
	var status string
	var enabledFilter *bool
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--status="):
			status = strings.TrimSpace(strings.TrimPrefix(arg, "--status="))
		case strings.HasPrefix(arg, "--enabled="):
			parsed, err := strconv.ParseBool(strings.TrimSpace(strings.TrimPrefix(arg, "--enabled=")))
			if err != nil {
				return fmt.Errorf("invalid --enabled: %w", err)
			}
			enabledFilter = &parsed
		default:
			return fmt.Errorf("unknown feeds list arg: %s", arg)
		}
	}

	feeds := filterFeeds(db.ListFeeds(), status, enabledFilter)
	return printJSON(feeds)
}

func runFeedsStatus(db *store.Store) error {
	return printJSON(buildFeedStatusSummary(db.ListFeeds()))
}

func filterFeeds(feeds []model.Feed, status string, enabledFilter *bool) []model.Feed {
	filtered := make([]model.Feed, 0, len(feeds))
	for _, item := range feeds {
		if status != "" && !strings.EqualFold(strings.TrimSpace(item.Status), status) {
			continue
		}
		if enabledFilter != nil && isFeedEnabled(item) != *enabledFilter {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].URL < filtered[j].URL })
	return filtered
}

func buildFeedStatusSummary(feeds []model.Feed) feedStatusSummary {
	summary := feedStatusSummary{ByStatus: map[string]int{}}
	for _, item := range feeds {
		summary.Total++
		if isFeedEnabled(item) {
			summary.Enabled++
		} else {
			summary.Inactive++
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "unknown"
		}
		summary.ByStatus[status]++
	}
	return summary
}

func isFeedEnabled(feed model.Feed) bool {
	return !strings.EqualFold(strings.TrimSpace(feed.Status), "inactive")
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
	items := filterArticleListOutput(buildArticleListOutput(articles), filters)
	if requestedLimit > 0 && len(items) > requestedLimit {
		items = items[:requestedLimit]
	}
	return printJSON(items)
}

func hasArticleListFilters(filters articleListFilters) bool {
	return filters.MinTotal > 0 || filters.MinAML > 0 || strings.TrimSpace(filters.Level) != ""
}

func filterArticleListOutput(items []articleListItem, filters articleListFilters) []articleListItem {
	if !hasArticleListFilters(filters) {
		return items
	}
	filtered := make([]articleListItem, 0, len(items))
	level := strings.TrimSpace(filters.Level)
	for _, item := range items {
		if item.Review == nil {
			continue
		}
		if filters.MinTotal > 0 && item.Review.TotalScore < filters.MinTotal {
			continue
		}
		if filters.MinAML > 0 && item.Review.AMLValue < filters.MinAML {
			continue
		}
		if level != "" && item.Review.RecommendationLevel != level {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func buildArticleListOutput(articles []model.Article) []articleListItem {
	items := make([]articleListItem, 0, len(articles))
	for _, article := range articles {
		items = append(items, articleListItem{Article: article, Review: buildArticleReviewSummary(article)})
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
	items := filterArticleListOutput(buildArticleListOutput(articles), filters)
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
	fmt.Println("  go run ./cmd/zhuimi feeds list [--status=ok|error|inactive|active|not_modified] [--enabled=true|false]")
	fmt.Println("  go run ./cmd/zhuimi feeds status")
	fmt.Println("  go run ./cmd/zhuimi run daily [--skip-scoring] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi db stats")
	fmt.Println("  go run ./cmd/zhuimi db vacuum")
	fmt.Println("  go run ./cmd/zhuimi articles list [--limit=20] [--status=scored|failed|pending] [--report-date=YYYY-MM-DD] [--min-total=75] [--min-aml=20] [--level=推荐]")
	fmt.Println("  go run ./cmd/zhuimi articles top [--date=YYYY-MM-DD] [--limit=10] [--min-total=75] [--min-aml=20] [--level=推荐] [--table]")
	fmt.Println("  go run ./cmd/zhuimi run backfill [--days=7] [--skip-scoring] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi retry failed-scores [--limit=50]")
	fmt.Println("  go run ./cmd/zhuimi report prune [--keep-days=30]")
	fmt.Println("  go run ./cmd/zhuimi report rebuild [--date=YYYY-MM-DD] [--pdf]")
	fmt.Println("  go run ./cmd/zhuimi migrate legacy")
}
