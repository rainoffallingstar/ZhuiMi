package pipeline

import "zhuimi/internal/model"

const minPDFTotalScore = 20

func filterArticlesForPDF(articles []model.Article, mode string, compilePDF bool) ([]model.Article, []string) {
	if !compilePDF || normalizeReportMode(mode) != model.ReportModeScored {
		return articles, articleIDs(articles)
	}
	kept := make([]model.Article, 0, len(articles))
	for _, article := range articles {
		if article.LatestScore == nil {
			continue
		}
		if article.LatestScore.Recommendation < minPDFTotalScore {
			continue
		}
		kept = append(kept, article)
	}
	return kept, articleIDs(kept)
}

func articleIDs(articles []model.Article) []string {
	ids := make([]string, 0, len(articles))
	for _, article := range articles {
		ids = append(ids, article.ID)
	}
	return ids
}
