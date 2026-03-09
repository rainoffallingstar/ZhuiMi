package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func RetryFailedScores(ctx context.Context, cfg config.Config, db *store.Store, limit int) error {
	if limit <= 0 {
		limit = 50
	}
	run := model.Run{ID: model.BuildArticleID("", "", time.Now().UTC().Format(time.RFC3339Nano)), Mode: "retry_failed_scores", StartedAt: time.Now().UTC(), Status: "running"}
	defer func() {
		if run.FinishedAt.IsZero() {
			run.FinishedAt = time.Now().UTC()
		}
		_ = db.AppendRun(run)
	}()

	articles, err := db.ListArticles(store.ListArticlesOptions{Limit: limit, Status: "failed"})
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	if len(articles) == 0 {
		run.Status = "ok"
		run.FinishedAt = time.Now().UTC()
		fmt.Println(`{"retried":0,"message":"no failed articles"}`)
		return nil
	}

	if err := scoreArticles(ctx, cfg, db, articles); err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	run.ArticlesScored = len(articles)

	datesSet := map[string]struct{}{}
	for _, article := range articles {
		if date := publishedDate(article); date != "" {
			datesSet[date] = struct{}{}
		}
		for _, date := range article.ReportDates {
			datesSet[date] = struct{}{}
		}
	}
	dates := make([]string, 0, len(datesSet))
	for date := range datesSet {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		if err := savePublishedDateReport(cfg, db, date, model.ReportModeScored, false); err != nil {
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
	payload := map[string]any{"retried": len(articles), "reports": len(dates)}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}
