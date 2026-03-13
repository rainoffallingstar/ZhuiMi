package model

import (
	"crypto/md5"
	"encoding/hex"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var doiPattern = regexp.MustCompile(`10\.\d{4,9}/[^\s"<>]+`)
var nonWordPattern = regexp.MustCompile(`[^\pL\pN\s]+`)

const (
	ReportModeScored = "scored"
	ReportModeRaw    = "raw"
)

type Feed struct {
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	SourceFile string    `json:"source_file,omitempty"`
	ETag       string    `json:"etag,omitempty"`
	LastMod    string    `json:"last_modified,omitempty"`
	CheckedAt  time.Time `json:"checked_at,omitempty"`
	SuccessAt  time.Time `json:"success_at,omitempty"`
	Status     string    `json:"status,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

type Article struct {
	ID             string     `json:"id"`
	DOI            string     `json:"doi,omitempty"`
	CanonicalLink  string     `json:"canonical_link"`
	Title          string     `json:"title"`
	Abstract       string     `json:"abstract"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	FeedURL        string     `json:"feed_url"`
	ContentHash    string     `json:"content_hash"`
	FirstSeenAt    time.Time  `json:"first_seen_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	LastScoredAt   *time.Time `json:"last_scored_at,omitempty"`
	ScoreStatus    string     `json:"score_status,omitempty"`
	LatestScore    *Score     `json:"latest_score,omitempty"`
	RawScoreSource string     `json:"raw_score_source,omitempty"`
	ReportDates    []string   `json:"report_dates,omitempty"`
	Link           string     `json:"link"`
	Reason         string     `json:"reason,omitempty"`
}

type Score struct {
	Research       int       `json:"research"`
	Social         int       `json:"social"`
	Blood          int       `json:"blood"`
	Recommendation int       `json:"recommendation"`
	Reason         string    `json:"reason"`
	Model          string    `json:"model"`
	ScoredAt       time.Time `json:"scored_at"`
	RawResponse    string    `json:"raw_response,omitempty"`
}

type Report struct {
	Date       string    `json:"date"`
	ArticleIDs []string  `json:"article_ids"`
	UpdatedAt  time.Time `json:"updated_at"`
	Mode       string    `json:"mode,omitempty"`
}

type Run struct {
	ID              string    `json:"id"`
	Mode            string    `json:"mode"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	FeedsChecked    int       `json:"feeds_checked"`
	ArticlesFetched int       `json:"articles_fetched"`
	ArticlesNew     int       `json:"articles_new"`
	ArticlesScored  int       `json:"articles_scored"`
	ReportDate      string    `json:"report_date,omitempty"`
	Status          string    `json:"status"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

func ExtractDOI(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		candidates := []string{value}
		if decoded := html.UnescapeString(value); decoded != value {
			candidates = append(candidates, decoded)
		}
		if decoded, err := url.PathUnescape(value); err == nil && decoded != value {
			candidates = append(candidates, decoded)
		}
		// Common in HTML: DOI has "/" encoded, then URL-escaped.
		if decoded := html.UnescapeString(value); decoded != value {
			if decoded2, err := url.PathUnescape(decoded); err == nil && decoded2 != decoded {
				candidates = append(candidates, decoded2)
			}
		}

		for _, candidate := range candidates {
			match := doiPattern.FindString(candidate)
			if match != "" {
				return strings.TrimRight(match, ".,;)")
			}
		}
	}
	return ""
}

func CanonicalizeLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimRight(raw, "/")
}

func BuildArticleID(doi, link, title string) string {
	if doi = strings.ToLower(strings.TrimSpace(doi)); doi != "" {
		return "doi:" + doi
	}
	if link = CanonicalizeLink(link); link != "" {
		return "link:" + hashString(link)
	}
	title = strings.TrimSpace(strings.ToLower(nonWordPattern.ReplaceAllString(title, "")))
	if title != "" {
		return "title:" + hashString(title)
	}
	return "unknown:" + hashString(time.Now().UTC().Format(time.RFC3339Nano))
}

func HashContent(parts ...string) string {
	clone := append([]string(nil), parts...)
	sort.Strings(clone)
	return hashString(strings.Join(clone, "\n"))
}

func hashString(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}
