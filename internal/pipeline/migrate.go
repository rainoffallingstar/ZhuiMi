package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/store"
)

var (
	legacyArticleSplitPattern   = regexp.MustCompile(`\n== #\d+\. `)
	legacyResearchPattern       = regexp.MustCompile(`\*\*研究分数\*\*: #(\d+)`)
	legacySocialPattern         = regexp.MustCompile(`\*\*社会影响\*\*: #(\d+)`)
	legacyBloodPattern          = regexp.MustCompile(`\*\*血液相关性\*\*: #(\d+)`)
	legacyRecommendationPattern = regexp.MustCompile(`\*\*推荐度\*\*: #(\d+)`)
	legacyDOIPattern            = regexp.MustCompile(`(?m)\*\*DOI\*\*: #(.+?)$`)
	legacyLinkPattern           = regexp.MustCompile(`#link\("([^"]+)"\)`)
	legacyReasonPattern         = regexp.MustCompile(`(?s)推荐理由: (.+?)(?:\n\n|\n摘要:)`)
	legacyAbstractPattern       = regexp.MustCompile(`(?s)摘要: (.+?)(?:\n\n---|\n---)`)
)

type legacyParsedArticle struct {
	article model.Article
	score   model.Score
}

func MigrateLegacy(cfg config.Config, db *store.Store) error {
	legacyFeed := model.Feed{URL: "legacy://report-import", Title: "Legacy report import", Status: "active"}
	if err := db.UpsertFeed(legacyFeed); err != nil {
		return err
	}
	if _, err := loadFeeds(cfg, db); err != nil {
		return err
	}

	processedCount, err := importLegacyProcessedIDs(cfg, db)
	if err != nil {
		return err
	}
	reportCount, articleCount, err := importLegacyReports(cfg, db, legacyFeed.URL)
	if err != nil {
		return err
	}
	if err := db.Vacuum(); err != nil {
		return err
	}

	payload := map[string]any{
		"processed_ids": processedCount,
		"reports":       reportCount,
		"articles":      articleCount,
	}
	encoded, _ := json.Marshal(payload)
	fmt.Println(string(encoded))
	return nil
}

func importLegacyProcessedIDs(cfg config.Config, db *store.Store) (int, error) {
	path := filepath.Join(cfg.RepoRoot, "scripts", ".zhuimi_analyzed.json")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read legacy analyzed db: %w", err)
	}
	var ids []string
	if err := json.Unmarshal(content, &ids); err != nil {
		return 0, fmt.Errorf("decode legacy analyzed db: %w", err)
	}
	count := 0
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := db.MarkProcessedID(strings.TrimSpace(id), "legacy_analyzed_db"); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func importLegacyReports(cfg config.Config, db *store.Store, feedURL string) (int, int, error) {
	entries, err := os.ReadDir(cfg.ContentDir)
	if err != nil {
		return 0, 0, fmt.Errorf("read content dir: %w", err)
	}

	reportDates := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !dateDirPattern(entry.Name()) {
			continue
		}
		reportDates = append(reportDates, entry.Name())
	}
	sort.Strings(reportDates)

	reportCount := 0
	articleCount := 0
	for _, reportDate := range reportDates {
		path := filepath.Join(cfg.ContentDir, reportDate, "index.typ")
		parsed, err := parseLegacyReport(path, reportDate, feedURL)
		if err != nil {
			return reportCount, articleCount, err
		}
		if len(parsed) == 0 {
			continue
		}
		ids := make([]string, 0, len(parsed))
		seen := map[string]struct{}{}
		for _, item := range parsed {
			if _, err := db.UpsertArticle(item.article); err != nil {
				return reportCount, articleCount, err
			}
			if err := db.SaveScore(item.article.ID, item.score); err != nil {
				return reportCount, articleCount, err
			}
			if err := db.MarkProcessedID(item.article.ID, "legacy_report"); err != nil {
				return reportCount, articleCount, err
			}
			if _, ok := seen[item.article.ID]; ok {
				continue
			}
			seen[item.article.ID] = struct{}{}
			ids = append(ids, item.article.ID)
			articleCount++
		}
		updatedAt := parsed[0].score.ScoredAt
		if err := db.SaveReport(model.Report{Date: reportDate, ArticleIDs: ids, UpdatedAt: updatedAt}); err != nil {
			return reportCount, articleCount, err
		}
		reportCount++
	}
	return reportCount, articleCount, nil
}

func parseLegacyReport(path, reportDate, feedURL string) ([]legacyParsedArticle, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read legacy report %s: %w", path, err)
	}
	blocks := legacyArticleSplitPattern.Split(string(content), -1)
	if len(blocks) <= 1 {
		return nil, nil
	}
	parsedDate, _ := time.Parse("2006-01-02", reportDate)
	if parsedDate.IsZero() {
		parsedDate = time.Now().UTC()
	}
	parsedDate = parsedDate.UTC().Add(12 * time.Hour)

	articles := make([]legacyParsedArticle, 0, len(blocks)-1)
	for _, block := range blocks[1:] {
		item, ok := parseLegacyArticleBlock(block, reportDate, parsedDate, feedURL)
		if ok {
			articles = append(articles, item)
		}
	}
	return articles, nil
}

func parseLegacyArticleBlock(block, reportDate string, reportTime time.Time, feedURL string) (legacyParsedArticle, bool) {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	if len(lines) == 0 {
		return legacyParsedArticle{}, false
	}
	title := legacyUnescape(strings.TrimSpace(lines[0]))
	linkMatch := legacyLinkPattern.FindStringSubmatch(block)
	if title == "" || len(linkMatch) < 2 {
		return legacyParsedArticle{}, false
	}
	link := strings.TrimSpace(linkMatch[1])
	doi := extractLegacyField(legacyDOIPattern, block)
	if doi == "N/A" {
		doi = ""
	}
	reason := legacyUnescape(extractLegacyField(legacyReasonPattern, block))
	abstract := legacyUnescape(extractLegacyField(legacyAbstractPattern, block))
	articleID := model.BuildArticleID(doi, link, title)
	publishedAt := reportTime
	article := model.Article{
		ID:             articleID,
		DOI:            doi,
		CanonicalLink:  model.CanonicalizeLink(link),
		Title:          title,
		Abstract:       defaultString(abstract, "No abstract"),
		PublishedAt:    &publishedAt,
		FeedURL:        feedURL,
		ContentHash:    model.HashContent(title, abstract, link, reportDate),
		FirstSeenAt:    reportTime,
		LastSeenAt:     reportTime,
		Link:           link,
		Reason:         reason,
		ScoreStatus:    "scored",
		RawScoreSource: "legacy_report_import",
	}
	score := model.Score{
		Research:       extractLegacyInt(legacyResearchPattern, block),
		Social:         extractLegacyInt(legacySocialPattern, block),
		Blood:          extractLegacyInt(legacyBloodPattern, block),
		Recommendation: extractLegacyInt(legacyRecommendationPattern, block),
		Reason:         defaultString(reason, "Legacy import"),
		Model:          "legacy-import",
		ScoredAt:       reportTime,
		RawResponse:    "legacy report import",
	}
	return legacyParsedArticle{article: article, score: score}, true
}

func extractLegacyField(pattern *regexp.Regexp, block string) string {
	match := pattern.FindStringSubmatch(block)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func extractLegacyInt(pattern *regexp.Regexp, block string) int {
	match := pattern.FindStringSubmatch(block)
	if len(match) < 2 {
		return 0
	}
	var value int
	fmt.Sscanf(match[1], "%d", &value)
	return value
}

func legacyUnescape(value string) string {
	replacer := strings.NewReplacer(`\@`, `@`, `\#`, `#`, `\<`, `<`, `\>`, `>`, `\[`, `[`, `\]`, `]`, `\(`, `(`, `\)`, `)`, `\{`, `{`, `\}`, `}`, `\*`, `*`, `\\$`, `$`)
	return strings.TrimSpace(replacer.Replace(value))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func dateDirPattern(name string) bool {
	_, err := time.Parse("2006-01-02", name)
	return err == nil
}
