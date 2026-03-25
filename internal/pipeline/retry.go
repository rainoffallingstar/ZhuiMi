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

	articles, err := failedAMLRetryArticles(db, limit)
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

	states := buildStoredArticleStates(db, articles)
	contentMap := buildContentMap(states)
	tasks := make([]ProcessorTask, 0, len(states))
	for _, state := range states {
		content := contentMap[state.Article.ID]
		tasks = append(tasks, ProcessorTask{
			Article:   state.Article,
			Content:   content,
			Processor: "aml_score",
			InputHash: processorInputHash("aml_score", state.Article, content),
			Reason:    taskReasonProcessorFailed,
		})
	}
	processedCount, err := processArticles(ctx, cfg, db, tasks)
	if err != nil {
		run.Status = "failed"
		run.ErrorMessage = err.Error()
		return err
	}
	run.ArticlesScored = processedCount

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

func failedAMLRetryArticles(db *store.Store, limit int) ([]model.Article, error) {
	if limit <= 0 {
		limit = 50
	}

	articles, err := db.ListArticlesByLatestProcessorStatus("aml_score", model.ProcessorStatusFailed, limit)
	if err != nil {
		return nil, err
	}
	if len(articles) >= limit {
		return articles[:limit], nil
	}

	legacyFailed, err := db.ListArticles(store.ListArticlesOptions{Limit: limit, Status: "failed"})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(articles))
	for _, article := range articles {
		seen[article.ID] = struct{}{}
	}
	for _, article := range legacyFailed {
		if _, ok := seen[article.ID]; ok {
			continue
		}
		articles = append(articles, article)
		seen[article.ID] = struct{}{}
		if len(articles) >= limit {
			break
		}
	}
	return articles, nil
}
