package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/feed"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func RunDaily(ctx context.Context, cfg config.Config, db *store.Store, opts RunOptions) error {
	run := model.Run{ID: model.BuildArticleID("", "", time.Now().UTC().Format(time.RFC3339Nano)), Mode: "run_daily", StartedAt: time.Now().UTC(), Status: "running"}
	defer func() {
		if run.FinishedAt.IsZero() {
			run.FinishedAt = time.Now().UTC()
		}
		_ = db.AppendRun(run)
	}()

	processorNames := enabledProcessorNames(cfg, opts, nil)
	reportMode := reportModeForRun(opts, processorNames)
	feeds, err := loadFeeds(cfg, db)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	if len(feeds) > cfg.MaxFeeds {
		feeds = feeds[:cfg.MaxFeeds]
	}
	run.FeedsChecked = len(feeds)

	results := feed.FetchFeeds(ctx, cfg, feeds)
	now := time.Now()
	cutoff := now.Add(-time.Duration(cfg.DaysWindow) * 24 * time.Hour)
	touchedArticles := make([]model.Article, 0)
	existingByID := make(map[string]*model.Article)
	newByID := make(map[string]bool)
	reportCandidateIDs := map[string]struct{}{}
	fetchedCount := 0
	newCount := 0

	for _, result := range results {
		feedState := result.Feed
		if result.Err != nil {
			feedState.Status = "error"
			feedState.LastError = result.Err.Error()
			_ = db.UpsertFeed(feedState)
			continue
		}
		_ = db.UpsertFeed(feedState)
		for _, item := range result.Items {
			if item.PubDate == nil || item.PubDate.Before(cutoff) {
				continue
			}
			fetchedCount++
			article := buildArticleFromFeedItem(feedItem(item), now)
			existing := db.FindArticle(article.ID)
			isNew, err := db.UpsertArticle(article)
			if err != nil {
				return err
			}
			if _, ok := existingByID[article.ID]; !ok {
				existingByID[article.ID] = existing
			}
			if isNew {
				newByID[article.ID] = true
			}
			touchedArticles = append(touchedArticles, article)
			reportCandidateIDs[article.ID] = struct{}{}
			if opts.SkipScoring {
				if isNew {
					newCount++
				}
				continue
			}
			if isNew {
				newCount++
				if db.HasProcessedID(article.ID) {
					_ = db.SetArticleStatus(article.ID, "processed", "legacy_processed")
				}
				continue
			}
		}
	}

	run.ArticlesFetched = fetchedCount
	run.ArticlesNew = newCount

	states := buildTouchedArticleStates(db, touchedArticles, existingByID, newByID)
	contentMap := buildContentMap(states)
	contentTasks := buildContentFetchTasks(states, cfg.ContentEnabled)
	contentReasonStats := contentTaskReasonCounts(contentTasks)
	fetchedContents, contentChanged, err := fetchArticleContents(ctx, cfg, db, contentTasks)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	for articleID, content := range fetchedContents {
		contentMap[articleID] = content
	}

	processorTasks := []ProcessorTask(nil)
	processorReasonStats := map[string]int{}
	if len(processorNames) > 0 {
		processorTasks = buildProcessorTasks(states, contentMap, processorNames)
		processorReasonStats = processorTaskReasonCounts(processorTasks)
		processedCount, err := processArticles(ctx, cfg, db, processorTasks)
		if err != nil {
			run.Status = "failed"
			run.ErrorMessage = err.Error()
			return err
		}
		run.ArticlesScored = processedCount
	}

	today := now.Format("2006-01-02")
	reportArticleIDs := map[string]struct{}{}
	if existingReport := db.Report(today); existingReport != nil && normalizeReportMode(existingReport.Mode) == reportMode {
		for _, id := range existingReport.ArticleIDs {
			reportArticleIDs[id] = struct{}{}
		}
	}
	if reportMode == model.ReportModeRaw {
		for id := range reportCandidateIDs {
			reportArticleIDs[id] = struct{}{}
		}
	} else {
		for _, state := range states {
			article := state.Article
			stored := db.FindArticle(article.ID)
			if shouldInclude(stored) {
				reportArticleIDs[article.ID] = struct{}{}
			}
		}
	}

	orderedIDs := make([]string, 0, len(reportArticleIDs))
	for id := range reportArticleIDs {
		orderedIDs = append(orderedIDs, id)
	}
	articles := db.ArticlesByIDs(orderedIDs)
	filtered, orderedIDs := prepareReportArticles(articles, reportMode, cfg.SortBy)
	filtered, orderedIDs = filterArticlesForPDF(filtered, reportMode, opts.CompilePDF)
	reportWritten := false
	if len(filtered) == 0 {
		run.Status = "ok"
		run.FinishedAt = time.Now().UTC()
		return printDailyStats(len(feeds), fetchedCount, newCount, len(contentTasks), contentChanged, contentReasonStats, len(processorTasks), processorReasonStats, run.ArticlesScored, today, reportMode, opts.CompilePDF, reportWritten, 0)
	}

	if err := db.SaveReport(model.Report{Date: today, ArticleIDs: orderedIDs, UpdatedAt: time.Now().UTC(), Mode: reportMode}); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	if err := report.WriteDailyWithOptions(cfg, today, filtered, report.WriteOptions{SortBy: cfg.SortBy, Mode: reportMode}); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	if opts.CompilePDF {
		if err := report.CompileDailyPDF(cfg, today); err != nil {
			run.Status = "failed"
			run.ErrorMessage = err.Error()
			return err
		}
	}
	if err := report.WriteIndex(cfg, db.ListReportDates()); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	reportWritten = true

	run.ReportDate = today
	run.Status = "ok"
	run.FinishedAt = time.Now().UTC()

	return printDailyStats(len(feeds), fetchedCount, newCount, len(contentTasks), contentChanged, contentReasonStats, len(processorTasks), processorReasonStats, run.ArticlesScored, today, reportMode, opts.CompilePDF, reportWritten, len(filtered))
}

func printDailyStats(feeds, fetched, newCount, contentTasks, contentChanged int, contentReasons map[string]int, processorTasks int, processorReasons map[string]int, processed int, reportDate, reportMode string, pdf, reportWritten bool, reportArticles int) error {
	stats := map[string]any{
		"feeds":             feeds,
		"fetched":           fetched,
		"new":               newCount,
		"content_tasks":     contentTasks,
		"content_changed":   contentChanged,
		"content_reasons":   contentReasons,
		"processor_tasks":   processorTasks,
		"processor_reasons": processorReasons,
		"processed":         processed,
		"report_date":       reportDate,
		"report_written":    reportWritten,
		"report_articles":   reportArticles,
		"mode":              reportMode,
		"pdf":               pdf,
	}
	encoded, _ := json.Marshal(stats)
	fmt.Println(string(encoded))
	return nil
}

func loadFeeds(cfg config.Config, db *store.Store) ([]model.Feed, error) {
	allFeeds := db.ListFeeds()
	if len(allFeeds) > 0 {
		return filterActiveFeeds(allFeeds), nil
	}
	content, err := readFeedsJSON(cfg)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		URL            string `json:"url"`
		Title          string `json:"title"`
		AllowTitleOnly *bool  `json:"allow_title_only,omitempty"`
	}
	if err := json.Unmarshal(content, &rows); err != nil {
		return nil, fmt.Errorf("decode feeds json: %w", err)
	}
	feeds := make([]model.Feed, 0, len(rows))
	for _, row := range rows {
		feeds = append(feeds, model.Feed{URL: row.URL, Title: row.Title, AllowTitleOnly: row.AllowTitleOnly, Status: "active"})
	}
	if err := db.UpsertFeeds(feeds); err != nil {
		return nil, err
	}
	return feeds, nil
}

func readFeedsJSON(cfg config.Config) ([]byte, error) {
	content, err := os.ReadFile(cfg.FeedsJSONPath)
	if err == nil {
		return content, nil
	}
	if !os.IsNotExist(err) || strings.TrimSpace(cfg.LegacyFeedsPath) == "" {
		return nil, fmt.Errorf("read feeds json: %w", err)
	}

	content, legacyErr := os.ReadFile(cfg.LegacyFeedsPath)
	if legacyErr != nil {
		return nil, fmt.Errorf("read feeds json: %w", err)
	}
	return content, nil
}

func filterActiveFeeds(feeds []model.Feed) []model.Feed {
	active := make([]model.Feed, 0, len(feeds))
	for _, item := range feeds {
		if strings.EqualFold(strings.TrimSpace(item.Status), "inactive") {
			continue
		}
		active = append(active, item)
	}
	return active
}

func newScoreRateLimiter(limit int) (func(context.Context) error, func()) {
	if limit < 1 {
		limit = 1
	}
	interval := time.Second / time.Duration(limit)
	if interval <= 0 {
		interval = time.Nanosecond
	}
	tokens := make(chan struct{}, 1)
	tokens <- struct{}{}
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case tokens <- struct{}{}:
				default:
				}
			}
		}
	}()

	wait := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tokens:
			return nil
		}
	}
	stop := func() {
		close(done)
		ticker.Stop()
	}
	return wait, stop
}

func shouldInclude(article *model.Article) bool {
	if article == nil || article.LatestScore == nil {
		return false
	}
	return article.LatestScore.Recommendation > 0 && article.LatestScore.Social > 0
}

func scoreForSort(article model.Article, key string) int {
	if article.LatestScore == nil {
		return 0
	}
	switch key {
	case "research":
		return article.LatestScore.Research
	case "social":
		return article.LatestScore.Social
	case "blood":
		return article.LatestScore.Blood
	default:
		return article.LatestScore.Recommendation
	}
}
