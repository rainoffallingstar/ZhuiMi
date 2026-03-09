package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

const currentSchemaVersion = 3

type migration struct {
	version int
	stmts   []string
}

type Store struct {
	db   *sql.DB
	path string
}

type Stats struct {
	Feeds          int
	Articles       int
	ScoredArticles int
	Reports        int
	Runs           int
}

type ListArticlesOptions struct {
	Limit         int
	Status        string
	ReportDate    string
	PublishedDate string
}

func Open(cfg config.Config) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	db, err := sql.Open("sqlite3", sqliteDSN(cfg.StatePath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	store := &Store{db: db, path: cfg.StatePath}
	if err := store.migrateSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func sqliteDSN(path string) string {
	return fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on", path)
}

func schemaMigrations() []migration {
	return []migration{
		{
			version: 1,
			stmts: []string{
				`CREATE TABLE IF NOT EXISTS feeds (
					url TEXT PRIMARY KEY,
					title TEXT NOT NULL,
					source_file TEXT,
					etag TEXT,
					last_modified TEXT,
					checked_at TEXT,
					success_at TEXT,
					status TEXT,
					last_error TEXT
				);`,
				`CREATE TABLE IF NOT EXISTS articles (
					id TEXT PRIMARY KEY,
					doi TEXT,
					canonical_link TEXT NOT NULL,
					title TEXT NOT NULL,
					abstract TEXT NOT NULL,
					published_at TEXT,
					feed_url TEXT NOT NULL,
					content_hash TEXT NOT NULL,
					first_seen_at TEXT NOT NULL,
					last_seen_at TEXT NOT NULL,
					last_scored_at TEXT,
					score_status TEXT,
					raw_score_source TEXT,
					link TEXT NOT NULL,
					reason TEXT,
					FOREIGN KEY(feed_url) REFERENCES feeds(url) ON DELETE CASCADE
				);`,
				`CREATE TABLE IF NOT EXISTS scores (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					article_id TEXT NOT NULL,
					research_score INTEGER NOT NULL,
					social_score INTEGER NOT NULL,
					blood_score INTEGER NOT NULL,
					recommendation_score INTEGER NOT NULL,
					reason TEXT,
					model TEXT,
					scored_at TEXT NOT NULL,
					raw_response TEXT,
					FOREIGN KEY(article_id) REFERENCES articles(id) ON DELETE CASCADE
				);`,
				`CREATE INDEX IF NOT EXISTS idx_scores_article_scored_at ON scores(article_id, scored_at DESC);`,
				`CREATE TABLE IF NOT EXISTS reports (
					report_date TEXT PRIMARY KEY,
					updated_at TEXT NOT NULL
				);`,
				`CREATE TABLE IF NOT EXISTS report_articles (
					report_date TEXT NOT NULL,
					article_id TEXT NOT NULL,
					position INTEGER NOT NULL,
					PRIMARY KEY(report_date, article_id),
					FOREIGN KEY(report_date) REFERENCES reports(report_date) ON DELETE CASCADE,
					FOREIGN KEY(article_id) REFERENCES articles(id) ON DELETE CASCADE
				);`,
				`CREATE INDEX IF NOT EXISTS idx_report_articles_position ON report_articles(report_date, position);`,
				`CREATE TABLE IF NOT EXISTS runs (
					id TEXT PRIMARY KEY,
					mode TEXT NOT NULL,
					started_at TEXT NOT NULL,
					finished_at TEXT NOT NULL,
					feeds_checked INTEGER NOT NULL,
					articles_fetched INTEGER NOT NULL,
					articles_new INTEGER NOT NULL,
					articles_scored INTEGER NOT NULL,
					report_date TEXT,
					status TEXT NOT NULL,
					error_message TEXT
				);`,
			},
		},
		{
			version: 2,
			stmts: []string{
				`CREATE TABLE IF NOT EXISTS processed_ids (
					article_id TEXT PRIMARY KEY,
					source TEXT,
					imported_at TEXT NOT NULL
				);`,
			},
		},
		{
			version: 3,
			stmts: []string{
				`ALTER TABLE reports ADD COLUMN mode TEXT NOT NULL DEFAULT 'scored';`,
			},
		},
	}
}

func (s *Store) migrateSchema() error {
	currentVersion, err := s.schemaVersion()
	if err != nil {
		return err
	}
	if currentVersion > currentSchemaVersion {
		return fmt.Errorf("sqlite schema version %d is newer than supported version %d", currentVersion, currentSchemaVersion)
	}

	for _, migration := range schemaMigrations() {
		if migration.version <= currentVersion {
			continue
		}
		if err := s.applyMigration(migration); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration v%d: %w", m.version, err)
	}
	defer tx.Rollback()

	for _, stmt := range m.stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("apply migration v%d: %w", m.version, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
		return fmt.Errorf("set migration version %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration v%d: %w", m.version, err)
	}
	return nil
}

func (s *Store) schemaVersion() (int, error) {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("query schema version: %w", err)
	}
	return version, nil
}

func (s *Store) SchemaVersion() (int, error) {
	return s.schemaVersion()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Vacuum() error {
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum sqlite: %w", err)
	}
	return nil
}

func (s *Store) Save() error {
	return nil
}

func (s *Store) MarkProcessedID(articleID, source string) error {
	_, err := s.db.Exec(`
		INSERT INTO processed_ids (article_id, source, imported_at)
		VALUES (?, ?, ?)
		ON CONFLICT(article_id) DO UPDATE SET
			source = COALESCE(excluded.source, processed_ids.source),
			imported_at = excluded.imported_at
	`, articleID, nullIfEmpty(source), timeString(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("mark processed id %s: %w", articleID, err)
	}
	return nil
}

func (s *Store) HasProcessedID(articleID string) bool {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM processed_ids WHERE article_id = ?`, articleID).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (s *Store) SetArticleStatus(articleID, status, rawSource string) error {
	_, err := s.db.Exec(`
		UPDATE articles
		SET score_status = ?, raw_score_source = ?, last_scored_at = COALESCE(last_scored_at, ?)
		WHERE id = ?
	`, status, nullIfEmpty(rawSource), timeString(time.Now().UTC()), articleID)
	if err != nil {
		return fmt.Errorf("set article status %s: %w", articleID, err)
	}
	return nil
}

func (s *Store) UpsertFeed(feed model.Feed) error {
	_, err := s.db.Exec(`
		INSERT INTO feeds (url, title, source_file, etag, last_modified, checked_at, success_at, status, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			source_file = excluded.source_file,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			checked_at = excluded.checked_at,
			success_at = excluded.success_at,
			status = excluded.status,
			last_error = excluded.last_error
	`,
		feed.URL,
		feed.Title,
		nullIfEmpty(feed.SourceFile),
		nullIfEmpty(feed.ETag),
		nullIfEmpty(feed.LastMod),
		nullTime(feed.CheckedAt),
		nullTime(feed.SuccessAt),
		nullIfEmpty(feed.Status),
		nullIfEmpty(feed.LastError),
	)
	if err != nil {
		return fmt.Errorf("upsert feed %s: %w", feed.URL, err)
	}
	return nil
}

func (s *Store) UpsertFeeds(feeds []model.Feed) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin feed upsert: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO feeds (url, title, source_file, etag, last_modified, checked_at, success_at, status, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title = excluded.title,
			source_file = excluded.source_file,
			etag = excluded.etag,
			last_modified = excluded.last_modified,
			checked_at = excluded.checked_at,
			success_at = excluded.success_at,
			status = excluded.status,
			last_error = excluded.last_error
	`)
	if err != nil {
		return fmt.Errorf("prepare feed upsert: %w", err)
	}
	defer stmt.Close()

	for _, feed := range feeds {
		if _, err := stmt.Exec(
			feed.URL,
			feed.Title,
			nullIfEmpty(feed.SourceFile),
			nullIfEmpty(feed.ETag),
			nullIfEmpty(feed.LastMod),
			nullTime(feed.CheckedAt),
			nullTime(feed.SuccessAt),
			nullIfEmpty(feed.Status),
			nullIfEmpty(feed.LastError),
		); err != nil {
			return fmt.Errorf("upsert feed %s: %w", feed.URL, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit feed upsert: %w", err)
	}
	return nil
}

func (s *Store) ListFeeds() []model.Feed {
	rows, err := s.db.Query(`SELECT url, title, source_file, etag, last_modified, checked_at, success_at, status, last_error FROM feeds ORDER BY url`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	feeds := make([]model.Feed, 0)
	for rows.Next() {
		var feed model.Feed
		var sourceFile, etag, lastMod, checkedAt, successAt, status, lastError sql.NullString
		if err := rows.Scan(&feed.URL, &feed.Title, &sourceFile, &etag, &lastMod, &checkedAt, &successAt, &status, &lastError); err != nil {
			continue
		}
		feed.SourceFile = sourceFile.String
		feed.ETag = etag.String
		feed.LastMod = lastMod.String
		feed.CheckedAt = parseTime(checkedAt)
		feed.SuccessAt = parseTime(successAt)
		feed.Status = status.String
		feed.LastError = lastError.String
		feeds = append(feeds, feed)
	}
	return feeds
}

func (s *Store) FindArticle(id string) *model.Article {
	article, err := s.findArticle(id)
	if err != nil {
		return nil
	}
	return article
}

func (s *Store) UpsertArticle(article model.Article) (bool, error) {
	var existing int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM articles WHERE id = ?`, article.ID).Scan(&existing); err != nil {
		return false, fmt.Errorf("check article %s: %w", article.ID, err)
	}

	_, err := s.db.Exec(`
		INSERT INTO articles (
			id, doi, canonical_link, title, abstract, published_at, feed_url, content_hash,
			first_seen_at, last_seen_at, last_scored_at, score_status, raw_score_source, link, reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			doi = excluded.doi,
			canonical_link = excluded.canonical_link,
			title = excluded.title,
			abstract = excluded.abstract,
			published_at = COALESCE(excluded.published_at, articles.published_at),
			feed_url = excluded.feed_url,
			content_hash = excluded.content_hash,
			last_seen_at = excluded.last_seen_at,
			link = excluded.link
	`,
		article.ID,
		nullIfEmpty(article.DOI),
		article.CanonicalLink,
		article.Title,
		article.Abstract,
		nullTimePtr(article.PublishedAt),
		article.FeedURL,
		article.ContentHash,
		timeString(article.FirstSeenAt),
		timeString(article.LastSeenAt),
		nullTimePtr(article.LastScoredAt),
		nullIfEmpty(article.ScoreStatus),
		nullIfEmpty(article.RawScoreSource),
		article.Link,
		nullIfEmpty(article.Reason),
	)
	if err != nil {
		return false, fmt.Errorf("upsert article %s: %w", article.ID, err)
	}
	return existing == 0, nil
}

func (s *Store) SaveScore(articleID string, score model.Score) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin save score: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO scores (article_id, research_score, social_score, blood_score, recommendation_score, reason, model, scored_at, raw_response)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, articleID, score.Research, score.Social, score.Blood, score.Recommendation, score.Reason, score.Model, timeString(score.ScoredAt), nullIfEmpty(score.RawResponse)); err != nil {
		return fmt.Errorf("insert score %s: %w", articleID, err)
	}

	if _, err := tx.Exec(`
		UPDATE articles
		SET last_scored_at = ?, score_status = 'scored', raw_score_source = NULL, reason = ?
		WHERE id = ?
	`, timeString(score.ScoredAt), score.Reason, articleID); err != nil {
		return fmt.Errorf("update article score %s: %w", articleID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save score: %w", err)
	}
	if err := s.MarkProcessedID(articleID, "scored"); err != nil {
		return err
	}
	return nil
}

func (s *Store) MarkScoreFailed(articleID, reason string, when time.Time) error {
	_, err := s.db.Exec(`
		UPDATE articles
		SET score_status = 'failed', last_scored_at = ?, raw_score_source = ?
		WHERE id = ?
	`, timeString(when), reason, articleID)
	if err != nil {
		return fmt.Errorf("mark score failed %s: %w", articleID, err)
	}
	if err := s.MarkProcessedID(articleID, "failed_score"); err != nil {
		return err
	}
	return nil
}

func (s *Store) ArticlesByIDs(ids []string) []model.Article {
	articles := make([]model.Article, 0, len(ids))
	for _, id := range ids {
		article, err := s.findArticle(id)
		if err == nil && article != nil {
			articles = append(articles, *article)
		}
	}
	return articles
}

func (s *Store) SaveReport(report model.Report) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin save report: %w", err)
	}
	defer tx.Rollback()

	mode := strings.TrimSpace(report.Mode)
	if mode == "" {
		mode = model.ReportModeScored
	}

	if _, err := tx.Exec(`
		INSERT INTO reports (report_date, updated_at, mode)
		VALUES (?, ?, ?)
		ON CONFLICT(report_date) DO UPDATE SET updated_at = excluded.updated_at, mode = excluded.mode
	`, report.Date, timeString(report.UpdatedAt), mode); err != nil {
		return fmt.Errorf("upsert report %s: %w", report.Date, err)
	}

	if _, err := tx.Exec(`DELETE FROM report_articles WHERE report_date = ?`, report.Date); err != nil {
		return fmt.Errorf("clear report articles %s: %w", report.Date, err)
	}

	stmt, err := tx.Prepare(`INSERT INTO report_articles (report_date, article_id, position) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare report articles %s: %w", report.Date, err)
	}
	defer stmt.Close()

	for index, articleID := range report.ArticleIDs {
		if _, err := stmt.Exec(report.Date, articleID, index); err != nil {
			return fmt.Errorf("insert report article %s/%s: %w", report.Date, articleID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit report %s: %w", report.Date, err)
	}
	return nil
}

func (s *Store) Report(date string) *model.Report {
	var updatedAt, mode string
	if err := s.db.QueryRow(`SELECT updated_at, COALESCE(mode, 'scored') FROM reports WHERE report_date = ?`, date).Scan(&updatedAt, &mode); err != nil {
		return nil
	}

	rows, err := s.db.Query(`SELECT article_id FROM report_articles WHERE report_date = ? ORDER BY position ASC`, date)
	if err != nil {
		return nil
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var articleID string
		if err := rows.Scan(&articleID); err != nil {
			continue
		}
		ids = append(ids, articleID)
	}

	return &model.Report{Date: date, ArticleIDs: ids, UpdatedAt: parseTime(sql.NullString{String: updatedAt, Valid: updatedAt != ""}), Mode: mode}
}

func (s *Store) ListReportDates() []string {
	rows, err := s.db.Query(`SELECT report_date FROM reports ORDER BY report_date DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	dates := make([]string, 0)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			continue
		}
		dates = append(dates, date)
	}
	return dates
}

func (s *Store) ListArticles(opts ListArticlesOptions) ([]model.Article, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	status := strings.TrimSpace(opts.Status)
	reportDate := strings.TrimSpace(opts.ReportDate)
	publishedDate := strings.TrimSpace(opts.PublishedDate)

	query := `
		SELECT a.id
		FROM articles a
		WHERE
			(? = '' OR ((? = 'pending' AND COALESCE(a.score_status, '') = '') OR a.score_status = ?))
			AND (? = '' OR EXISTS (SELECT 1 FROM report_articles ra WHERE ra.article_id = a.id AND ra.report_date = ?))
			AND (? = '' OR substr(COALESCE(a.published_at, ''), 1, 10) = ?)
		ORDER BY COALESCE(a.last_scored_at, a.last_seen_at) DESC, a.id DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, status, status, status, reportDate, reportDate, publishedDate, publishedDate, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	defer rows.Close()

	articles := make([]model.Article, 0, opts.Limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan article id: %w", err)
		}
		article, err := s.findArticle(id)
		if err != nil {
			return nil, err
		}
		if article != nil {
			articles = append(articles, *article)
		}
	}
	return articles, nil
}

func (s *Store) DeleteReport(date string) error {
	_, err := s.db.Exec(`DELETE FROM reports WHERE report_date = ?`, date)
	if err != nil {
		return fmt.Errorf("delete report %s: %w", date, err)
	}
	return nil
}

func (s *Store) AppendRun(run model.Run) error {
	_, err := s.db.Exec(`
		INSERT INTO runs (id, mode, started_at, finished_at, feeds_checked, articles_fetched, articles_new, articles_scored, report_date, status, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.Mode, timeString(run.StartedAt), timeString(run.FinishedAt), run.FeedsChecked, run.ArticlesFetched, run.ArticlesNew, run.ArticlesScored, nullIfEmpty(run.ReportDate), run.Status, nullIfEmpty(run.ErrorMessage))
	if err != nil {
		return fmt.Errorf("append run %s: %w", run.ID, err)
	}
	_, _ = s.db.Exec(`DELETE FROM runs WHERE id NOT IN (SELECT id FROM runs ORDER BY started_at DESC LIMIT 200)`)
	return nil
}

func (s *Store) Stats() Stats {
	stats := Stats{}
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM feeds`).Scan(&stats.Feeds)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM articles`).Scan(&stats.Articles)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM articles WHERE score_status = 'scored'`).Scan(&stats.ScoredArticles)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM reports`).Scan(&stats.Reports)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM runs`).Scan(&stats.Runs)
	return stats
}

func (s *Store) findArticle(id string) (*model.Article, error) {
	row := s.db.QueryRow(`
		SELECT id, doi, canonical_link, title, abstract, published_at, feed_url, content_hash,
		       first_seen_at, last_seen_at, last_scored_at, score_status, raw_score_source, link, reason
		FROM articles WHERE id = ?
	`, id)

	var article model.Article
	var doi, publishedAt, lastScoredAt, scoreStatus, rawScoreSource, reason sql.NullString
	var firstSeenAt, lastSeenAt string
	if err := row.Scan(
		&article.ID,
		&doi,
		&article.CanonicalLink,
		&article.Title,
		&article.Abstract,
		&publishedAt,
		&article.FeedURL,
		&article.ContentHash,
		&firstSeenAt,
		&lastSeenAt,
		&lastScoredAt,
		&scoreStatus,
		&rawScoreSource,
		&article.Link,
		&reason,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan article %s: %w", id, err)
	}

	article.DOI = doi.String
	article.PublishedAt = parseTimePtr(publishedAt)
	article.FirstSeenAt = parseTime(sql.NullString{String: firstSeenAt, Valid: firstSeenAt != ""})
	article.LastSeenAt = parseTime(sql.NullString{String: lastSeenAt, Valid: lastSeenAt != ""})
	article.LastScoredAt = parseTimePtr(lastScoredAt)
	article.ScoreStatus = scoreStatus.String
	article.RawScoreSource = rawScoreSource.String
	article.Reason = reason.String

	score, err := s.latestScore(article.ID)
	if err != nil {
		return nil, err
	}
	article.LatestScore = score
	article.ReportDates = s.reportDatesForArticle(article.ID)
	return &article, nil
}

func (s *Store) latestScore(articleID string) (*model.Score, error) {
	row := s.db.QueryRow(`
		SELECT research_score, social_score, blood_score, recommendation_score, reason, model, scored_at, raw_response
		FROM scores WHERE article_id = ? ORDER BY scored_at DESC, id DESC LIMIT 1
	`, articleID)

	var score model.Score
	var reason, modelName, scoredAt string
	var rawResponse sql.NullString
	if err := row.Scan(&score.Research, &score.Social, &score.Blood, &score.Recommendation, &reason, &modelName, &scoredAt, &rawResponse); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load latest score %s: %w", articleID, err)
	}
	score.Reason = reason
	score.Model = modelName
	parsed, err := time.Parse(time.RFC3339Nano, scoredAt)
	if err == nil {
		score.ScoredAt = parsed
	}
	score.RawResponse = rawResponse.String
	return &score, nil
}

func (s *Store) reportDatesForArticle(articleID string) []string {
	rows, err := s.db.Query(`SELECT report_date FROM report_articles WHERE article_id = ? ORDER BY report_date ASC`, articleID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	dates := make([]string, 0)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			continue
		}
		dates = append(dates, date)
	}
	return dates
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return timeString(value)
}

func nullTimePtr(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return timeString(*value)
}

func timeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value sql.NullString) time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseTimePtr(value sql.NullString) *time.Time {
	parsed := parseTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}
