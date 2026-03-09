package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
	reportMode := opts.ReportMode()

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
	articlesToScore := make([]model.Article, 0)
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
				FirstSeenAt:   now,
				LastSeenAt:    now,
				ContentHash:   model.HashContent(item.Title, item.Description, item.Link),
			}
			article.ID = model.BuildArticleID(article.DOI, article.CanonicalLink, article.Title)
			existing := db.FindArticle(article.ID)
			isNew, err := db.UpsertArticle(article)
			if err != nil {
				run.Status = "failed"
				run.ErrorMessage = err.Error()
				return err
			}
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
					continue
				}
				articlesToScore = append(articlesToScore, article)
				continue
			}
			if existing != nil && ((existing.LatestScore == nil && strings.TrimSpace(existing.ScoreStatus) == "") || existing.ScoreStatus == "failed") {
				merged := *existing
				merged.Title = article.Title
				merged.Abstract = article.Abstract
				merged.Link = article.Link
				merged.CanonicalLink = article.CanonicalLink
				merged.PublishedAt = article.PublishedAt
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
	payload := map[string]any{"feeds": len(feeds), "fetched": fetchedCount, "new": newCount, "scored": len(articlesToScore), "reports": len(dates), "days": days, "mode": reportMode, "pdf": opts.CompilePDF}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}
