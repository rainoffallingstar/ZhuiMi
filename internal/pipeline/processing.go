package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/content"
	"zhuimi/internal/model"
	"zhuimi/internal/processor"
	"zhuimi/internal/store"
)

const (
	taskReasonNewArticle       = "new_article"
	taskReasonContentChanged   = "content_changed"
	taskReasonContentRetry     = "content_retry"
	taskReasonContentMissing   = "content_missing"
	taskReasonProcessorMissing = "processor_missing"
	taskReasonProcessorFailed  = "processor_failed"
	taskReasonProcessorChanged = "input_changed"
	taskReasonProcessorPending = "processor_incomplete"
)

type articleTaskState struct {
	Article         model.Article
	Existing        *model.Article
	ExistingContent *model.ArticleContent
	LatestResults   map[string]model.ProcessorResult
	IsNew           bool
	MetadataChanged bool
}

type ContentFetchTask struct {
	Article         model.Article
	ExistingContent *model.ArticleContent
	Reason          string
}

type ProcessorTask struct {
	Article   model.Article
	Content   *model.ArticleContent
	Processor string
	InputHash string
	Reason    string
}

func enabledProcessorNames(cfg config.Config, opts RunOptions, explicit []string) []string {
	if opts.SkipScoring {
		return nil
	}
	if len(explicit) > 0 {
		return explicit
	}
	return processor.Enabled(cfg)
}

func hasProcessor(names []string, target string) bool {
	for _, name := range names {
		if strings.TrimSpace(name) == target {
			return true
		}
	}
	return false
}

func buildTouchedArticleStates(db *store.Store, touched []model.Article, existingByID map[string]*model.Article, newByID map[string]bool) []articleTaskState {
	order := make([]string, 0, len(touched))
	states := make(map[string]*articleTaskState, len(touched))
	for _, article := range touched {
		state := states[article.ID]
		if state == nil {
			order = append(order, article.ID)
			state = &articleTaskState{
				Existing:        existingByID[article.ID],
				ExistingContent: db.ArticleContent(article.ID),
				LatestResults:   db.LatestAIResultsMap(article.ID),
				IsNew:           newByID[article.ID],
			}
			states[article.ID] = state
		}
		state.Article = article
		if state.Existing != nil && state.Existing.ContentHash != article.ContentHash {
			state.MetadataChanged = true
		}
		if state.Existing == nil {
			state.MetadataChanged = true
		}
		if newByID[article.ID] {
			state.IsNew = true
		}
	}

	items := make([]articleTaskState, 0, len(order))
	for _, id := range order {
		if state := states[id]; state != nil {
			items = append(items, *state)
		}
	}
	return items
}

func buildStoredArticleStates(db *store.Store, articles []model.Article) []articleTaskState {
	items := make([]articleTaskState, 0, len(articles))
	for _, article := range articles {
		items = append(items, articleTaskState{
			Article:         article,
			Existing:        &article,
			ExistingContent: db.ArticleContent(article.ID),
			LatestResults:   db.LatestAIResultsMap(article.ID),
		})
	}
	return items
}

func buildContentFetchTasks(states []articleTaskState, contentEnabled bool) []ContentFetchTask {
	tasks := make([]ContentFetchTask, 0, len(states))
	for _, state := range states {
		reason := ""
		switch {
		case state.IsNew:
			reason = taskReasonNewArticle
		case state.MetadataChanged:
			reason = taskReasonContentChanged
		case state.ExistingContent == nil:
			reason = taskReasonContentMissing
		case shouldRetryContentFetch(state.ExistingContent, contentEnabled):
			reason = taskReasonContentRetry
		}
		if reason == "" {
			continue
		}
		tasks = append(tasks, ContentFetchTask{
			Article:         state.Article,
			ExistingContent: state.ExistingContent,
			Reason:          reason,
		})
	}
	return tasks
}

func shouldRetryContentFetch(content *model.ArticleContent, contentEnabled bool) bool {
	if content == nil {
		return false
	}
	switch strings.TrimSpace(content.FetchStatus) {
	case model.ContentStatusPending, model.ContentStatusFailed, model.ContentStatusRSSFallback:
		return true
	case model.ContentStatusSkipped:
		return contentEnabled && strings.TrimSpace(content.ErrorMessage) == "content fetching disabled"
	default:
		return false
	}
}

func buildContentMap(states []articleTaskState) map[string]*model.ArticleContent {
	contents := make(map[string]*model.ArticleContent, len(states))
	for _, state := range states {
		if state.ExistingContent != nil {
			clone := *state.ExistingContent
			contents[state.Article.ID] = &clone
		}
	}
	return contents
}

func fetchArticleContents(ctx context.Context, cfg config.Config, db *store.Store, tasks []ContentFetchTask) (map[string]*model.ArticleContent, int, error) {
	contents := make(map[string]*model.ArticleContent, len(tasks))
	if len(tasks) == 0 {
		return contents, 0, nil
	}

	fetcher := content.NewFetcher(cfg)
	jobs := make(chan ContentFetchTask)
	type result struct {
		content model.ArticleContent
		changed bool
		err     error
	}
	results := make(chan result, len(tasks))
	workers := cfg.ContentConcurrency
	if workers > len(tasks) {
		workers = len(tasks)
	}
	if workers < 1 {
		workers = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				content := fetcher.FetchArticle(ctx, task.Article)
				changed, err := db.UpsertArticleContent(content)
				results <- result{content: content, changed: changed, err: err}
			}
		}()
	}

	go func() {
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	changedCount := 0
	for item := range results {
		if item.err != nil {
			return nil, changedCount, item.err
		}
		contentCopy := item.content
		contents[item.content.ArticleID] = &contentCopy
		if item.changed {
			changedCount++
		}
	}
	return contents, changedCount, nil
}

func buildProcessorTasks(states []articleTaskState, contents map[string]*model.ArticleContent, processorNames []string) []ProcessorTask {
	if len(states) == 0 || len(processorNames) == 0 {
		return nil
	}
	tasks := make([]ProcessorTask, 0, len(states)*len(processorNames))
	for _, state := range states {
		content := contents[state.Article.ID]
		if !hasUsableProcessorInput(state.Article, content) {
			continue
		}
		for _, processorName := range processorNames {
			inputHash := processorInputHash(processorName, state.Article, content)
			latestResult, ok := state.LatestResults[processorName]
			reason := ""
			switch {
			case !ok:
				reason = taskReasonProcessorMissing
			case latestResult.InputHash != inputHash:
				reason = taskReasonProcessorChanged
			case strings.TrimSpace(latestResult.Status) == model.ProcessorStatusFailed:
				reason = taskReasonProcessorFailed
			case strings.TrimSpace(latestResult.Status) != model.ProcessorStatusProcessed:
				reason = taskReasonProcessorPending
			}
			if reason == "" {
				continue
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

func hasUsableProcessorInput(article model.Article, content *model.ArticleContent) bool {
	if content != nil && strings.TrimSpace(content.ContentText) != "" {
		return true
	}
	return strings.TrimSpace(article.Abstract) != ""
}

func processArticles(ctx context.Context, cfg config.Config, db *store.Store, tasks []ProcessorTask) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}

	progress := newProcessorProgress(len(tasks))
	progress.Start()
	defer progress.Done()

	names := make([]string, 0, len(tasks))
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if _, ok := seen[task.Processor]; ok {
			continue
		}
		seen[task.Processor] = struct{}{}
		names = append(names, task.Processor)
	}

	processors, err := processor.Build(cfg, names)
	if err != nil {
		return 0, err
	}
	processorByName := make(map[string]processor.Processor, len(processors))
	for _, proc := range processors {
		processorByName[proc.Name()] = proc
	}

	waitToken, stopLimiter := newScoreRateLimiter(cfg.ScoreRateLimit)
	defer stopLimiter()

	processedCount := 0
	for _, task := range tasks {
		proc := processorByName[task.Processor]
		if proc == nil {
			return processedCount, fmt.Errorf("processor %s not initialized", task.Processor)
		}
		if err := waitToken(ctx); err != nil {
			return processedCount, err
		}

		output, err := proc.Process(ctx, processor.Input{Article: task.Article, Content: task.Content})
		if err != nil {
			if saveErr := persistProcessorFailure(db, task.Article.ID, task.Processor, task.InputHash, err.Error()); saveErr != nil {
				return processedCount, saveErr
			}
			if task.Processor == "aml_score" {
				if markErr := db.MarkScoreFailed(task.Article.ID, err.Error(), time.Now().UTC()); markErr != nil {
					return processedCount, markErr
				}
			}
			progress.Advance(task, err)
			continue
		}

		output.Result.InputHash = task.InputHash
		if output.Result.ProcessedAt.IsZero() {
			output.Result.ProcessedAt = time.Now().UTC()
		}
		if err := db.SaveAIResult(output.Result); err != nil {
			return processedCount, err
		}
		if output.LegacyScore != nil {
			if err := db.SaveScore(task.Article.ID, *output.LegacyScore); err != nil {
				return processedCount, err
			}
		}
		processedCount++
		progress.Advance(task, nil)
	}
	return processedCount, nil
}

func processorInputHash(name string, article model.Article, content *model.ArticleContent) string {
	contentHash := article.ContentHash
	contentStatus := ""
	if content != nil {
		contentHash = firstNonEmpty(content.ContentHash, contentHash)
		contentStatus = content.FetchStatus
	}
	return model.HashContent(name, article.ID, article.Title, article.Abstract, article.Link, contentHash, contentStatus)
}

func persistProcessorFailure(db *store.Store, articleID, processorName, inputHash, message string) error {
	return db.SaveAIResult(model.ProcessorResult{
		ArticleID:    articleID,
		Processor:    processorName,
		InputHash:    inputHash,
		Status:       model.ProcessorStatusFailed,
		ProcessedAt:  time.Now().UTC(),
		ErrorMessage: truncateString(message, 1000),
	})
}

func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func reportModeForRun(opts RunOptions, processorNames []string) string {
	if opts.SkipScoring || !hasProcessor(processorNames, "aml_score") {
		return model.ReportModeRaw
	}
	return model.ReportModeScored
}

func buildArticleFromFeedItem(item feedItem, now time.Time) model.Article {
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
	return article
}

type feedItem struct {
	Title       string
	Link        string
	Description string
	PubDate     *time.Time
	DOI         string
	FeedURL     string
}
