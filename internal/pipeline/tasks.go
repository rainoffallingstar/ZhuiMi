package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

func RunFetchContent(ctx context.Context, cfg config.Config, db *store.Store, limit int, statusFilter string, force bool) error {
	statusFilter = model.NormalizeContentStatus(statusFilter)
	articles, err := fetchContentTargetArticles(db, limit, statusFilter, force)
	if err != nil {
		return err
	}
	states := buildStoredArticleStates(db, articles)
	tasks := filterContentFetchTasksByStatus(buildContentFetchTasks(states, cfg.ContentEnabled), statusFilter)
	if force {
		tasks = make([]ContentFetchTask, 0, len(states))
		for _, state := range states {
			if !matchesContentLatestStatus(state.ExistingContent, statusFilter) {
				continue
			}
			tasks = append(tasks, ContentFetchTask{Article: state.Article, ExistingContent: state.ExistingContent, Reason: taskReasonContentChanged})
		}
	}
	contents, changed, err := fetchArticleContents(ctx, cfg, db, tasks)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"requested": len(articles),
		"tasks":     len(tasks),
		"changed":   changed,
		"fetched":   len(contents),
		"reasons":   contentTaskReasonCounts(tasks),
		"status":    statusFilter,
		"force":     force,
	}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}

func fetchContentTargetArticles(db *store.Store, limit int, statusFilter string, force bool) ([]model.Article, error) {
	if statusFilter != "" {
		return db.ListArticlesByContentStatus(statusFilter, limit)
	}
	return db.ListArticlesForContentFetch(limit, force)
}

func filterContentFetchTasksByStatus(tasks []ContentFetchTask, statusFilter string) []ContentFetchTask {
	if statusFilter == "" {
		return tasks
	}
	filtered := make([]ContentFetchTask, 0, len(tasks))
	for _, task := range tasks {
		if contentFetchTaskMatchesStatus(task, statusFilter) {
			filtered = append(filtered, task)
		}
	}
	return filtered
}

func contentFetchTaskMatchesStatus(task ContentFetchTask, statusFilter string) bool {
	switch statusFilter {
	case "missing":
		return task.Reason == taskReasonContentMissing || task.Reason == taskReasonNewArticle
	case "failed":
		return task.Reason == taskReasonContentRetry && task.ExistingContent != nil && model.NormalizeContentStatus(task.ExistingContent.FetchStatus) == model.ContentStatusFailed
	case "rss_fallback":
		return task.Reason == taskReasonContentRetry && task.ExistingContent != nil && model.NormalizeContentStatus(task.ExistingContent.FetchStatus) == model.ContentStatusRSSFallback
	case "pending", "skipped":
		return task.Reason == taskReasonContentRetry && task.ExistingContent != nil && model.NormalizeContentStatus(task.ExistingContent.FetchStatus) == statusFilter
	case "fetched":
		return false
	default:
		return true
	}
}

func matchesContentLatestStatus(content *model.ArticleContent, statusFilter string) bool {
	if statusFilter == "" {
		return true
	}
	if content == nil {
		return statusFilter == "missing"
	}
	if statusFilter == "missing" {
		return false
	}
	return model.NormalizeContentStatus(content.FetchStatus) == statusFilter
}

func RunProcessors(ctx context.Context, cfg config.Config, db *store.Store, limit int, processorNames []string, statusFilter string, force bool) error {
	if limit <= 0 {
		limit = 50
	}
	statusFilter = model.NormalizeProcessorStatus(statusFilter)
	articles, err := fetchProcessorTargetArticles(db, limit, processorNames, statusFilter)
	if err != nil {
		return err
	}
	states := buildStoredArticleStates(db, articles)
	contents := buildContentMap(states)
	tasks := buildProcessorTasks(states, contents, processorNames)
	if statusFilter != "" {
		tasks = buildStatusFilteredProcessorTasks(states, contents, processorNames, statusFilter, force)
	} else if force {
		tasks = buildForcedProcessorTasks(states, contents, processorNames, statusFilter)
	}
	processed, err := processArticles(ctx, cfg, db, tasks)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"requested":  len(articles),
		"tasks":      len(tasks),
		"processed":  processed,
		"processors": processorNames,
		"reasons":    processorTaskReasonCounts(tasks),
		"status":     statusFilter,
		"force":      force,
	}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}

func fetchProcessorTargetArticles(db *store.Store, limit int, processorNames []string, statusFilter string) ([]model.Article, error) {
	if statusFilter == "" {
		return db.ListArticles(store.ListArticlesOptions{Limit: limit})
	}

	articles := make([]model.Article, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendUnique := func(items []model.Article) {
		for _, article := range items {
			if _, ok := seen[article.ID]; ok {
				continue
			}
			seen[article.ID] = struct{}{}
			articles = append(articles, article)
		}
	}

	for _, processorName := range processorNames {
		var items []model.Article
		var err error
		if statusFilter == "missing" {
			items, err = db.ListArticlesMissingProcessorResult(processorName, limit)
		} else {
			items, err = db.ListArticlesByLatestProcessorStatus(processorName, statusFilter, limit)
		}
		if err != nil {
			return nil, err
		}
		appendUnique(items)
	}
	sort.Slice(articles, func(i, j int) bool {
		left := articles[i]
		right := articles[j]
		leftTime := left.LastSeenAt
		if leftTime.IsZero() {
			leftTime = left.FirstSeenAt
		}
		rightTime := right.LastSeenAt
		if rightTime.IsZero() {
			rightTime = right.FirstSeenAt
		}
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return left.ID > right.ID
	})
	if len(articles) > limit {
		articles = articles[:limit]
	}
	return articles, nil
}

func buildForcedProcessorTasks(states []articleTaskState, contents map[string]*model.ArticleContent, processorNames []string, statusFilter string) []ProcessorTask {
	tasks := make([]ProcessorTask, 0, len(states)*len(processorNames))
	for _, state := range states {
		content := contents[state.Article.ID]
		for _, processorName := range processorNames {
			if !hasUsableProcessorInput(state.Article, content) {
				continue
			}
			if !matchesProcessorLatestStatus(state.LatestResults, processorName, statusFilter) {
				continue
			}
			tasks = append(tasks, ProcessorTask{
				Article:   state.Article,
				Content:   content,
				Processor: processorName,
				InputHash: processorInputHash(processorName, state.Article, content),
				Reason:    taskReasonProcessorChanged,
			})
		}
	}
	return tasks
}

func buildStatusFilteredProcessorTasks(states []articleTaskState, contents map[string]*model.ArticleContent, processorNames []string, statusFilter string, force bool) []ProcessorTask {
	tasks := make([]ProcessorTask, 0, len(states)*len(processorNames))
	for _, state := range states {
		content := contents[state.Article.ID]
		for _, processorName := range processorNames {
			if !hasUsableProcessorInput(state.Article, content) {
				continue
			}
			if !matchesProcessorLatestStatus(state.LatestResults, processorName, statusFilter) {
				continue
			}

			inputHash := processorInputHash(processorName, state.Article, content)
			reason := taskReasonProcessorChanged
			if !force {
				reason = processorTaskReason(state.LatestResults, processorName, inputHash)
				if reason == "" {
					continue
				}
			}

			tasks = append(tasks, ProcessorTask{
				Article:   state.Article,
				Content:   content,
				Processor: processorName,
				InputHash: inputHash,
				Reason:    reason,
			})
		}
	}
	return tasks
}

func processorTaskReason(latest map[string]model.ProcessorResult, processorName, inputHash string) string {
	latestResult, ok := latest[processorName]
	switch {
	case !ok:
		return taskReasonProcessorMissing
	case latestResult.InputHash != inputHash:
		return taskReasonProcessorChanged
	case strings.TrimSpace(latestResult.Status) == model.ProcessorStatusFailed:
		return taskReasonProcessorFailed
	case strings.TrimSpace(latestResult.Status) != model.ProcessorStatusProcessed:
		return taskReasonProcessorPending
	default:
		return ""
	}
}

func matchesProcessorLatestStatus(latest map[string]model.ProcessorResult, processorName, statusFilter string) bool {
	if statusFilter == "" {
		return true
	}
	result, ok := latest[processorName]
	if statusFilter == "missing" {
		return !ok
	}
	if !ok {
		return false
	}
	return model.NormalizeProcessorStatus(result.Status) == statusFilter
}

func contentTaskReasonCounts(tasks []ContentFetchTask) map[string]int {
	counts := make(map[string]int)
	for _, task := range tasks {
		counts[task.Reason]++
	}
	return counts
}

func processorTaskReasonCounts(tasks []ProcessorTask) map[string]int {
	counts := make(map[string]int)
	for _, task := range tasks {
		key := task.Reason
		if task.Processor != "" {
			key = task.Processor + ":" + task.Reason
		}
		counts[key]++
	}
	return counts
}

func CurrentProcessorNames(cfg config.Config, opts RunOptions, explicit []string) []string {
	return enabledProcessorNames(cfg, opts, explicit)
}

func CurrentReportMode(opts RunOptions, processorNames []string) string {
	return reportModeForRun(opts, processorNames)
}

func ShouldWriteScoredReport(mode string) bool {
	return mode == model.ReportModeScored
}
