package content

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

type Fetcher struct {
	httpClient *http.Client
	config     config.Config
}

func NewFetcher(cfg config.Config) *Fetcher {
	return &Fetcher{
		httpClient: &http.Client{Timeout: cfg.ContentTimeout},
		config:     cfg,
	}
}

func (f *Fetcher) FetchArticle(ctx context.Context, article model.Article) model.ArticleContent {
	content := model.ArticleContent{
		ArticleID:   article.ID,
		ResolvedURL: article.Link,
		FetchStatus: model.ContentStatusPending,
	}
	if !f.config.ContentEnabled {
		return withSkippedFallback(content, article, "content fetching disabled")
	}
	if strings.TrimSpace(article.Link) == "" {
		return withSkippedFallback(content, article, "article link is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, article.Link, nil)
	if err != nil {
		return withFailureFallback(content, article, fmt.Errorf("create request: %w", err))
	}
	if ua := strings.TrimSpace(f.config.ContentUserAgent); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return withFailureFallback(content, article, fmt.Errorf("request content: %w", err))
	}
	defer resp.Body.Close()

	content.ResolvedURL = resp.Request.URL.String()
	if resp.StatusCode != http.StatusOK {
		return withFailureFallback(content, article, fmt.Errorf("unexpected status %d", resp.StatusCode))
	}
	if contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))); contentType != "" && !strings.Contains(contentType, "text/html") {
		return withFailureFallback(content, article, fmt.Errorf("unsupported content-type %s", contentType))
	}

	body, err := readLimited(resp.Body, f.config.ContentMaxBytes)
	if err != nil {
		return withFailureFallback(content, article, fmt.Errorf("read body: %w", err))
	}

	text, sourceType := ExtractText(body)
	if text == "" {
		return withRSSFallback(content, article, "html extraction returned empty text")
	}

	content.FetchStatus = model.ContentStatusFetched
	content.SourceType = sourceType
	content.ContentText = text
	if f.config.ContentStoreHTML {
		content.ContentHTML = string(body)
	}
	content.ContentHash = model.HashContent(content.ResolvedURL, sourceType, text)
	now := nowUTC()
	content.FetchedAt = &now
	content.ErrorMessage = ""
	return content
}

var (
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reNoBody  = regexp.MustCompile(`(?is)<(?:noscript|svg|canvas|iframe|footer|nav|aside)\b[^>]*>.*?</(?:noscript|svg|canvas|iframe|footer|nav|aside)>`)
	reComment = regexp.MustCompile(`(?is)<!--.*?-->`)
	reMeta    = regexp.MustCompile(`(?is)<meta[^>]+(?:name|property)\s*=\s*['"]([^'"]+)['"][^>]+content\s*=\s*['"]([^'"]+)['"][^>]*>`)
	reTag     = regexp.MustCompile(`(?is)<[^>]+>`)
	reWS      = regexp.MustCompile(`\s+`)
)

func ExtractText(body []byte) (string, string) {
	raw := string(body)
	cleaned := reScript.ReplaceAllString(raw, " ")
	cleaned = reStyle.ReplaceAllString(cleaned, " ")
	cleaned = reNoBody.ReplaceAllString(cleaned, " ")
	cleaned = reComment.ReplaceAllString(cleaned, " ")

	metaText := extractMetaContent(cleaned)
	articleText := extractTagBlock(cleaned, "article")
	mainText := extractTagBlock(cleaned, "main")
	bodyText := extractTagBlock(cleaned, "body")

	switch {
	case metaText != "" && articleText != "":
		if metaText == articleText {
			return metaText, "html_meta"
		}
		return metaText + "\n\n" + articleText, "html_meta_body"
	case metaText != "" && mainText != "":
		if metaText == mainText {
			return metaText, "html_meta"
		}
		return metaText + "\n\n" + mainText, "html_meta_body"
	case metaText != "":
		return metaText, "html_meta"
	case articleText != "":
		return articleText, "html_body"
	case mainText != "":
		return mainText, "html_body"
	case bodyText != "":
		return bodyText, "html_body"
	default:
		return "", ""
	}
}

func extractMetaContent(raw string) string {
	matches := reMeta.FindAllStringSubmatch(raw, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(match[1]))
		switch key {
		case "description", "og:description", "twitter:description", "citation_abstract", "dc.description", "dc.description.abstract":
			if text := normalizeHTMLText(match[2]); text != "" {
				values = append(values, text)
			}
		}
	}
	return dedupeJoined(values)
}

func extractTagBlock(raw, tag string) string {
	pattern := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>(.*?)</` + tag + `>`)
	matches := pattern.FindAllStringSubmatch(raw, -1)
	best := ""
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		text := normalizeHTMLText(match[1])
		if len(text) > len(best) {
			best = text
		}
	}
	return best
}

func normalizeHTMLText(value string) string {
	value = strings.ReplaceAll(value, "<br>", "\n")
	value = strings.ReplaceAll(value, "<br/>", "\n")
	value = strings.ReplaceAll(value, "<br />", "\n")
	value = strings.ReplaceAll(value, "</p>", "\n\n")
	value = strings.ReplaceAll(value, "</div>", "\n")
	value = strings.ReplaceAll(value, "</li>", "\n")
	value = reTag.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	lines := strings.Split(value, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = reWS.ReplaceAllString(strings.TrimSpace(line), " ")
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func dedupeJoined(values []string) string {
	seen := make(map[string]struct{}, len(values))
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return strings.TrimSpace(strings.Join(items, "\n\n"))
}

func withFailureFallback(content model.ArticleContent, article model.Article, err error) model.ArticleContent {
	if strings.TrimSpace(article.Abstract) != "" {
		return withRSSFallback(content, article, err.Error())
	}
	content.FetchStatus = model.ContentStatusFailed
	content.SourceType = "html_error"
	content.ErrorMessage = err.Error()
	now := nowUTC()
	content.FetchedAt = &now
	content.ContentHash = model.HashContent(content.ResolvedURL, content.FetchStatus, content.ErrorMessage)
	return content
}

func withRSSFallback(content model.ArticleContent, article model.Article, reason string) model.ArticleContent {
	content.FetchStatus = model.ContentStatusRSSFallback
	content.SourceType = "rss_description"
	content.ContentText = strings.TrimSpace(article.Abstract)
	content.ErrorMessage = strings.TrimSpace(reason)
	now := nowUTC()
	content.FetchedAt = &now
	content.ContentHash = model.HashContent(content.ResolvedURL, content.SourceType, content.ContentText)
	return content
}

func withSkippedFallback(content model.ArticleContent, article model.Article, reason string) model.ArticleContent {
	content.FetchStatus = model.ContentStatusSkipped
	content.ErrorMessage = strings.TrimSpace(reason)
	content.ContentText = strings.TrimSpace(article.Abstract)
	if content.ContentText != "" {
		content.SourceType = "rss_description"
	} else {
		content.SourceType = "content_skipped"
	}
	now := nowUTC()
	content.FetchedAt = &now
	content.ContentHash = model.HashContent(content.ResolvedURL, content.FetchStatus, content.SourceType, content.ContentText, content.ErrorMessage)
	return content
}

func readLimited(reader io.Reader, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024
	}
	limited := io.LimitReader(reader, int64(maxBytes)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("response exceeds max bytes %d", maxBytes)
	}
	return body, nil
}

var nowUTC = func() time.Time {
	return time.Now().UTC()
}
