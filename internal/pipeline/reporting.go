package pipeline

import (
	"sort"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/report"
	"zhuimi/internal/store"
)

func savePublishedDateReport(cfg config.Config, db *store.Store, date, mode string, compilePDF bool) error {
	articles, err := db.ListArticles(store.ListArticlesOptions{
		Limit:         10000,
		PublishedDate: date,
		Status:        reportStatusFilter(mode),
	})
	if err != nil {
		return err
	}

	filtered, orderedIDs := prepareReportArticles(articles, mode, cfg.SortBy)
	filtered, orderedIDs = filterArticlesForPDF(filtered, mode, compilePDF)
	if len(filtered) == 0 {
		return nil
	}

	if err := db.SaveReport(model.Report{Date: date, ArticleIDs: orderedIDs, UpdatedAt: time.Now().UTC(), Mode: normalizeReportMode(mode)}); err != nil {
		return err
	}
	if err := report.WriteDailyWithOptions(cfg, date, filtered, report.WriteOptions{SortBy: cfg.SortBy, Mode: mode}); err != nil {
		return err
	}
	if compilePDF {
		if err := report.CompileDailyPDF(cfg, date); err != nil {
			return err
		}
	}
	return nil
}

func reportStatusFilter(mode string) string {
	if normalizeReportMode(mode) == model.ReportModeScored {
		return "scored"
	}
	return ""
}

func prepareReportArticles(articles []model.Article, mode, sortBy string) ([]model.Article, []string) {
	normalized := normalizeReportMode(mode)
	filtered := make([]model.Article, 0, len(articles))
	for _, article := range articles {
		if normalized == model.ReportModeScored && !shouldInclude(&article) {
			continue
		}
		filtered = append(filtered, article)
	}
	sortArticlesForReport(filtered, normalized, sortBy)
	orderedIDs := make([]string, 0, len(filtered))
	for _, article := range filtered {
		orderedIDs = append(orderedIDs, article.ID)
	}
	return filtered, orderedIDs
}

func sortArticlesForReport(articles []model.Article, mode, sortBy string) {
	if len(articles) < 2 {
		return
	}
	if normalizeReportMode(mode) == model.ReportModeScored {
		sort.Slice(articles, func(i, j int) bool {
			left := scoreForSort(articles[i], sortBy)
			right := scoreForSort(articles[j], sortBy)
			if left != right {
				return left > right
			}
			return rawArticleLess(articles[i], articles[j])
		})
		return
	}
	sort.Slice(articles, func(i, j int) bool {
		return rawArticleLess(articles[i], articles[j])
	})
}

func rawArticleLess(left, right model.Article) bool {
	leftPublished := publishedAtForSort(left)
	rightPublished := publishedAtForSort(right)
	if !leftPublished.Equal(rightPublished) {
		return leftPublished.After(rightPublished)
	}
	if !left.LastSeenAt.Equal(right.LastSeenAt) {
		return left.LastSeenAt.After(right.LastSeenAt)
	}
	return strings.Compare(left.ID, right.ID) > 0
}

func publishedAtForSort(article model.Article) time.Time {
	if article.PublishedAt == nil || article.PublishedAt.IsZero() {
		return time.Time{}
	}
	return article.PublishedAt.UTC()
}

func normalizeReportMode(mode string) string {
	if strings.TrimSpace(mode) == model.ReportModeRaw {
		return model.ReportModeRaw
	}
	return model.ReportModeScored
}

func publishedDate(article model.Article) string {
	if article.PublishedAt == nil || article.PublishedAt.IsZero() {
		return ""
	}
	return article.PublishedAt.UTC().Format("2006-01-02")
}
