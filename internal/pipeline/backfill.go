package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/feed"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func RunBackfill(ctx context.Context, cfg config.Config, db *store.Store, days int, opts RunOptions) error {
	if days < 1 {
		days = 1
	}
	cfg.DaysWindow = days
	processorNames := enabledProcessorNames(cfg, opts, nil)
	reportMode := reportModeForRun(opts, processorNames)

	run := model.Run{ID: model.BuildArticleID("", "", time.Now().UTC().Format(time.RFC3339Nano)), Mode: "run_backfill", StartedAt: time.Now().UTC(), Status: "running"}
	defer func() {
		if run.FinishedAt.IsZero() {
			run.FinishedAt = time.Now().UTC()
		}
		_ = db.AppendRun(run)
	}()

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
	now := time.Now().UTC()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	touchedArticles := make([]model.Article, 0)
	existingByID := make(map[string]*model.Article)
	newByID := make(map[string]bool)
	touchedDates := map[string]struct{}{}
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
				run.Status = "failed"
				run.ErrorMessage = err.Error()
				return err
			}
			if _, ok := existingByID[article.ID]; !ok {
				existingByID[article.ID] = existing
			}
			if isNew {
				newByID[article.ID] = true
			}
			touchedArticles = append(touchedArticles, article)
			if date := publishedDate(article); date != "" {
				touchedDates[date] = struct{}{}
			}
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

	dates := make([]string, 0, len(touchedDates))
	for date := range touchedDates {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		if err := savePublishedDateReport(cfg, db, date, reportMode, opts.CompilePDF); err != nil {
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

	run.Status = "ok"
	run.FinishedAt = time.Now().UTC()
	payload := map[string]any{
		"feeds":             len(feeds),
		"fetched":           fetchedCount,
		"new":               newCount,
		"content_tasks":     len(contentTasks),
		"content_changed":   contentChanged,
		"content_reasons":   contentReasonStats,
		"processor_tasks":   len(processorTasks),
		"processor_reasons": processorReasonStats,
		"processed":         run.ArticlesScored,
		"reports":           len(dates),
		"days":              days,
		"mode":              reportMode,
		"pdf":               opts.CompilePDF,
	}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}
