package report

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

var superscriptReplacer = strings.NewReplacer(
	"⁰", "^0", "¹", "^1", "²", "^2", "³", "^3", "⁴", "^4", "⁵", "^5", "⁶", "^6", "⁷", "^7", "⁸", "^8", "⁹", "^9",
	"⁺", "^+", "⁻", "^-", "⁼", "^=", "⁽", "^(", "⁾", "^)",
	"₀", "_0", "₁", "_1", "₂", "_2", "₃", "_3", "₄", "_4", "₅", "_5", "₆", "_6", "₇", "_7", "₈", "_8", "₉", "_9",
	"₊", "_+", "₋", "_-", "₌", "_=", "₍", "_(", "₎", "_)",
)

var latexBracePattern = regexp.MustCompile(`([_^])\{([^}]*)\}`)
var lookPathFunc = exec.LookPath
var runCommandFunc = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

type WriteOptions struct {
	SortBy string
	Mode   string
}

func TestingLookPathFunc() func(string) (string, error) {
	return lookPathFunc
}

func TestingRunCommandFunc() func(string, ...string) ([]byte, error) {
	return runCommandFunc
}

func SetTestingExecFuncs(lookPath func(string) (string, error), runCommand func(string, ...string) ([]byte, error)) {
	if lookPath != nil {
		lookPathFunc = lookPath
	}
	if runCommand != nil {
		runCommandFunc = runCommand
	}
}

func WriteDaily(cfg config.Config, date string, articles []model.Article, sortBy string) error {
	return WriteDailyWithOptions(cfg, date, articles, WriteOptions{SortBy: sortBy, Mode: model.ReportModeScored})
}

func WriteDailyWithOptions(cfg config.Config, date string, articles []model.Article, opts WriteOptions) error {
	if err := os.MkdirAll(filepath.Join(cfg.ContentDir, date), 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}

	mode := normalizeReportMode(opts.Mode)
	if mode == model.ReportModeScored && strings.TrimSpace(opts.SortBy) != "" {
		sort.Slice(articles, func(i, j int) bool {
			return scoreValue(articles[i], opts.SortBy) > scoreValue(articles[j], opts.SortBy)
		})
	}

	var builder strings.Builder
	builder.WriteString(renderHeader(date, len(articles), mode))
	for index, article := range articles {
		if mode == model.ReportModeScored {
			score := article.LatestScore
			if score == nil {
				continue
			}
			builder.WriteString(fmt.Sprintf(`== #%d. %s

- **研究本身价值**: #%d / 60
- **AML借鉴价值**: #%d / 30
- **表达与可复用性**: #%d / 10
- **最终总分**: #%d / 100
- **发布日期**: #%s
- **DOI**: #%s
%s

审稿结论: %s

摘要: %s

---

`, index+1, escape(article.Title), score.Research, score.Social, score.Blood, score.Recommendation, escape(defaultPublishedDate(article.PublishedAt)), escape(defaultValue(article.DOI, "N/A")), renderLinkLine(article.Link), escape(article.Reason), escape(truncate(article.Abstract, 1000))))
			continue
		}
		builder.WriteString(fmt.Sprintf(`== #%d. %s

- **发布日期**: #%s
- **DOI**: #%s
- **Feed**: #%s
%s

摘要: %s

---

`, index+1, escape(article.Title), escape(defaultPublishedDate(article.PublishedAt)), escape(defaultValue(article.DOI, "N/A")), escape(defaultValue(article.FeedURL, "N/A")), renderLinkLine(article.Link), escape(truncate(article.Abstract, 1000))))
	}

	return os.WriteFile(filepath.Join(cfg.ContentDir, date, "index.typ"), []byte(builder.String()), 0o644)
}

func CompileDailyPDF(cfg config.Config, date string) error {
	return CompilePDF(filepath.Join(cfg.ContentDir, date, "index.typ"), filepath.Join(cfg.ContentDir, date, "index.pdf"))
}

func CompilePDF(inputPath, outputPath string) error {
	typstBin, err := lookPathFunc("typst")
	if err != nil {
		return fmt.Errorf("typst not found in PATH: %w", err)
	}
	rootPath, err := detectTypstRoot(inputPath)
	if err != nil {
		return err
	}
	output, err := runCommandFunc(typstBin, "compile", "--root", rootPath, inputPath, outputPath)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("typst compile failed: %w: %s", err, message)
		}
		return fmt.Errorf("typst compile failed: %w", err)
	}
	return nil
}

func detectTypstRoot(inputPath string) (string, error) {
	absInputPath, err := filepath.Abs(inputPath)
	if err != nil {
		return "", fmt.Errorf("resolve absolute input path: %w", err)
	}

	inputDir := filepath.Dir(absInputPath)
	dir := inputDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "config.typ")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return inputDir, nil
}

func WriteIndex(cfg config.Config, dates []string) error {
	var builder strings.Builder
	builder.WriteString(`#import "../../config.typ": template, tufted
#show: template.with(title: "追觅")

= 追觅

每日文献追踪与AI评分报告。

== 历史报告

`)
	for _, date := range dates {
		builder.WriteString(fmt.Sprintf(`- #link("/ZhuiMi/%s/")[%s]`+"\n", date, date))
	}
	return os.WriteFile(filepath.Join(cfg.ContentDir, "index.typ"), []byte(builder.String()), 0o644)
}

func normalizeReportMode(mode string) string {
	if strings.TrimSpace(mode) == model.ReportModeRaw {
		return model.ReportModeRaw
	}
	return model.ReportModeScored
}

func renderHeader(date string, count int, mode string) string {
	title := "追觅 - " + date
	description := "分析文章数量"
	if mode == model.ReportModeRaw {
		title = "追觅抓取 - " + date
		description = "抓取文章数量"
	}
	return fmt.Sprintf(`#import "../../../config.typ": template, tufted
#show: template.with(title: "%s")

= %s

生成时间: #%s

%s: #%d

`, title, title, time.Now().Format("2006-01-02 15:04"), description, count)
}

func renderLinkLine(link string) string {
	if strings.TrimSpace(link) == "" {
		return "- **链接**: N/A"
	}
	return fmt.Sprintf(`- **链接**: #link("%s")[查看原文]`, link)
}

func defaultPublishedDate(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "N/A"
	}
	return value.UTC().Format("2006-01-02")
}

func scoreValue(article model.Article, key string) int {
	if article.LatestScore == nil {
		return 0
	}
	switch key {
	case "research":
		return article.LatestScore.Research
	case "social":
		return article.LatestScore.Social
	case "blood":
		return article.LatestScore.Blood
	default:
		return article.LatestScore.Recommendation
	}
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "$3^{\\prime}$", "3'")
	value = strings.ReplaceAll(value, "$5^{\\prime}$", "5'")
	value = strings.ReplaceAll(value, "$\\alpha$", "alpha")
	value = strings.ReplaceAll(value, "$\\beta$", "beta")
	value = strings.ReplaceAll(value, "$\\gamma$", "gamma")
	value = strings.ReplaceAll(value, "$", "\\$")
	value = latexBracePattern.ReplaceAllString(value, `$1$2`)
	value = superscriptReplacer.Replace(value)
	replacer := strings.NewReplacer("*", "\\*", "{", "\\{", "}", "\\}", "@", "\\@", "#", "\\#", "<", "\\<", ">", "\\>", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)")
	return replacer.Replace(value)
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
