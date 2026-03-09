package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RepoRoot          string
	LegacyConfigPath  string
	SubscribeDir      string
	FeedsJSONPath     string
	ContentDir        string
	StatePath         string
	DaysWindow        int
	MaxFeeds          int
	MaxArticles       int
	SortBy            string
	OpenAIBaseURL     string
	OpenAIAPIKey      string
	OpenAIModel       string
	OpenAIMaxTokens   int
	OpenAITimeout     time.Duration
	OpenAITemperature float64
	FetchConcurrency  int
	ScoreConcurrency  int
}

func Load(repoRoot string) (Config, error) {
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		RepoRoot:          repoRoot,
		LegacyConfigPath:  filepath.Join(repoRoot, "scripts", "config", "zhuimi_config.yaml"),
		SubscribeDir:      filepath.Join(repoRoot, "scripts", "subscribe"),
		FeedsJSONPath:     filepath.Join(repoRoot, "scripts", "pubmed_feeds.json"),
		ContentDir:        filepath.Join(repoRoot, "content", "ZhuiMi"),
		StatePath:         filepath.Join(repoRoot, "data", "zhuimi", "store.db"),
		DaysWindow:        3,
		MaxFeeds:          100,
		MaxArticles:       200,
		SortBy:            "recommendation",
		OpenAIBaseURL:     "https://api.openai.com/v1",
		OpenAIModel:       "gpt-4o-mini",
		OpenAIMaxTokens:   1800,
		OpenAITimeout:     120 * time.Second,
		OpenAITemperature: 0.3,
		FetchConcurrency:  6,
		ScoreConcurrency:  3,
	}

	if err := applyLegacyYAML(&cfg); err != nil {
		return Config{}, err
	}
	applyEnv(&cfg)

	if cfg.DaysWindow < 1 {
		cfg.DaysWindow = 1
	}
	if cfg.MaxFeeds < 1 {
		cfg.MaxFeeds = 1
	}
	if cfg.MaxArticles < 1 {
		cfg.MaxArticles = 1
	}
	if cfg.FetchConcurrency < 1 {
		cfg.FetchConcurrency = 1
	}
	if cfg.ScoreConcurrency < 1 {
		cfg.ScoreConcurrency = 1
	}

	return cfg, nil
}

func applyEnv(cfg *Config) {
	if value := os.Getenv("OPENAI_BASE_URL"); value != "" {
		cfg.OpenAIBaseURL = value
	}
	if value := os.Getenv("OPENAI_API_KEY"); value != "" {
		cfg.OpenAIAPIKey = value
	}
	if value := os.Getenv("OPENAI_MODEL"); value != "" {
		cfg.OpenAIModel = value
	}
	if value := os.Getenv("ZHUIMI_FETCH_CONCURRENCY"); value != "" {
		cfg.FetchConcurrency = parseInt(value, cfg.FetchConcurrency)
	}
	if value := os.Getenv("ZHUIMI_SCORE_CONCURRENCY"); value != "" {
		cfg.ScoreConcurrency = parseInt(value, cfg.ScoreConcurrency)
	}
	if value := os.Getenv("ZHUIMI_STATE_PATH"); value != "" {
		cfg.StatePath = resolvePath(cfg.RepoRoot, value)
	}
}

func applyLegacyYAML(cfg *Config) error {
	file, err := os.Open(cfg.LegacyConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open legacy config: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	section := ""
	for scanner.Scan() {
		line := stripComments(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}

		indent := countIndent(line)
		trimmed := strings.TrimSpace(line)

		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if indent != 2 || strings.HasPrefix(trimmed, "- ") {
			continue
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		switch section + "." + key {
		case "rss.days_window":
			cfg.DaysWindow = parseInt(value, cfg.DaysWindow)
		case "rss.max_feeds":
			cfg.MaxFeeds = parseInt(value, cfg.MaxFeeds)
		case "ai.model":
			cfg.OpenAIModel = value
		case "ai.max_tokens":
			cfg.OpenAIMaxTokens = parseInt(value, cfg.OpenAIMaxTokens)
		case "ai.temperature":
			cfg.OpenAITemperature = parseFloat(value, cfg.OpenAITemperature)
		case "ai.timeout_seconds":
			cfg.OpenAITimeout = time.Duration(parseInt(value, int(cfg.OpenAITimeout/time.Second))) * time.Second
		case "report.sort_by":
			cfg.SortBy = value
		case "report.max_articles":
			cfg.MaxArticles = parseInt(value, cfg.MaxArticles)
		case "output.content_dir":
			cfg.ContentDir = resolvePath(cfg.RepoRoot, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan legacy config: %w", err)
	}

	return nil
}

func stripComments(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimRight(line[:i], " ")
			}
		}
	}
	return line
}

func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

func parseInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func parseFloat(raw string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return fallback
	}
	return value
}

func resolvePath(repoRoot, value string) string {
	if value == "" {
		return value
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(repoRoot, value)
}
