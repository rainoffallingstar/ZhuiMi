package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/feed"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/scoring"
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

	reportMode := opts.ReportMode()
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
	articlesToScore := make([]model.Article, 0)
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
			if item.Description == "" {
				continue
			}
			fetchedCount++
			article := model.Article{
				DOI:           item.DOI,
				CanonicalLink: model.CanonicalizeLink(item.Link),
				Title:         item.Title,
				Abstract:      item.Description,
				PublishedAt:   item.PubDate,
				FeedURL:       item.FeedURL,
				Link:          item.Link,
				FirstSeenAt:   now.UTC(),
				LastSeenAt:    now.UTC(),
				ContentHash:   model.HashContent(item.Title, item.Description, item.Link),
			}
			article.ID = model.BuildArticleID(article.DOI, article.CanonicalLink, article.Title)
			existing := db.FindArticle(article.ID)
			isNew, err := db.UpsertArticle(article)
			if err != nil {
				return err
			}
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
					continue
				}
				articlesToScore = append(articlesToScore, article)
				continue
			}
			if existing != nil && existing.LatestScore == nil && strings.TrimSpace(existing.ScoreStatus) == "" {
				merged := *existing
				merged.Title = article.Title
				merged.Abstract = article.Abstract
				merged.Link = article.Link
				merged.CanonicalLink = article.CanonicalLink
				articlesToScore = append(articlesToScore, merged)
			}
		}
	}

	run.ArticlesFetched = fetchedCount
	run.ArticlesNew = newCount

	if !opts.SkipScoring {
		if err := scoreArticles(ctx, cfg, db, articlesToScore); err != nil {
			run.Status = "failed"
			run.ErrorMessage = err.Error()
			return err
		}
		run.ArticlesScored = len(articlesToScore)
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
		for _, article := range articlesToScore {
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
	if len(filtered) == 0 {
		run.Status = "ok"
		run.FinishedAt = time.Now().UTC()
		return nil
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

	run.ReportDate = today
	run.Status = "ok"
	run.FinishedAt = time.Now().UTC()

	stats := map[string]any{"feeds": len(feeds), "fetched": fetchedCount, "new": newCount, "scored": len(articlesToScore), "report_date": today, "mode": reportMode, "pdf": opts.CompilePDF}
	encoded, _ := json.Marshal(stats)
	fmt.Println(string(encoded))
	return nil
}

func loadFeeds(cfg config.Config, db *store.Store) ([]model.Feed, error) {
	allFeeds := db.ListFeeds()
	if len(allFeeds) > 0 {
		return filterActiveFeeds(allFeeds), nil
	}
	content, err := os.ReadFile(cfg.FeedsJSONPath)
	if err != nil {
		return nil, fmt.Errorf("read feeds json: %w", err)
	}
	var rows []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(content, &rows); err != nil {
		return nil, fmt.Errorf("decode feeds json: %w", err)
	}
	feeds := make([]model.Feed, 0, len(rows))
	for _, row := range rows {
		feeds = append(feeds, model.Feed{URL: row.URL, Title: row.Title, Status: "active"})
	}
	if err := db.UpsertFeeds(feeds); err != nil {
		return nil, err
	}
	return feeds, nil
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

func scoreArticles(ctx context.Context, cfg config.Config, db *store.Store, articles []model.Article) error {
	if len(articles) == 0 {
		return nil
	}
	client := scoring.NewClient(cfg)
	jobs := make(chan model.Article)
	results := make(chan scoreResult, len(articles))
	workers := cfg.ScoreConcurrency
	if workers > len(articles) {
		workers = len(articles)
	}
	progress := newScoreProgress(len(articles), workers)
	progress.Start()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range jobs {
				score, err := client.ScoreArticle(ctx, article)
				if err != nil {
					if markErr := db.MarkScoreFailed(article.ID, err.Error(), time.Now().UTC()); markErr != nil {
						results <- scoreResult{Article: article, FatalErr: markErr}
						continue
					}
					results <- scoreResult{Article: article, ScoreErr: err}
					continue
				}
				if err := db.SaveScore(article.ID, score); err != nil {
					results <- scoreResult{Article: article, FatalErr: err}
					continue
				}
				results <- scoreResult{Article: article}
			}
		}()
	}

	go func() {
		for _, article := range articles {
			jobs <- article
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var fatalErr error
	for result := range results {
		progress.Advance(result)
		if result.FatalErr != nil && fatalErr == nil {
			fatalErr = result.FatalErr
		}
	}
	progress.Done()
	return fatalErr
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
