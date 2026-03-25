package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadLegacyYAML(t *testing.T) {
	repoRoot := t.TempDir()
	configDir := filepath.Join(repoRoot, "scripts", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte(`rss:
  days_window: 5
  max_feeds: 12
  filter_title_only: false
  allow_title_only_for: "example.com, https://example.org/feed.xml"
ai:
  model: "demo-model"
  max_tokens: 1234
  temperature: 0.7
  timeout_seconds: 42
  score_rate_limit: 7
content:
  enabled: true
  concurrency: 5
  timeout_seconds: 18
  max_bytes: 4096
  user_agent: "TestAgent/1.0"
  store_html: true
processors:
  enabled: "generic_digest, aml_score"
  default: "generic_digest"
  generic_digest_model: "digest-model"
  aml_score_model: "aml-model"
report:
  sort_by: "blood"
  max_articles: 88
output:
  content_dir: "content/ZhuiMi"
  analyzed_db: "scripts/.zhuimi_analyzed.json"
`)
	if err := os.WriteFile(filepath.Join(configDir, "zhuimi_config.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DaysWindow != 5 || cfg.MaxFeeds != 12 || cfg.FilterTitleOnly || cfg.OpenAIModel != "demo-model" || cfg.SortBy != "blood" || cfg.MaxArticles != 88 || cfg.ScoreRateLimit != 7 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.AllowTitleOnlyFor) != 2 || cfg.AllowTitleOnlyFor[0] != "example.com" || cfg.AllowTitleOnlyFor[1] != "https://example.org/feed.xml" {
		t.Fatalf("unexpected allow_title_only_for: %#v", cfg.AllowTitleOnlyFor)
	}
	if !cfg.ContentEnabled || cfg.ContentConcurrency != 5 || cfg.ContentTimeout != 18*time.Second || cfg.ContentMaxBytes != 4096 || cfg.ContentUserAgent != "TestAgent/1.0" || !cfg.ContentStoreHTML {
		t.Fatalf("unexpected content config: %+v", cfg)
	}
	if len(cfg.ProcessorsEnabled) != 2 || cfg.ProcessorsEnabled[0] != "generic_digest" || cfg.ProcessorsEnabled[1] != "aml_score" || cfg.DefaultProcessor != "generic_digest" || cfg.GenericDigestModel != "digest-model" || cfg.AMLScoreModel != "aml-model" {
		t.Fatalf("unexpected processors config: %+v", cfg)
	}
}

func TestLoadUsesEnvForScoreRateLimit(t *testing.T) {
	t.Setenv("ZHUIMI_SCORE_RATE_LIMIT", "9")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ScoreRateLimit != 9 {
		t.Fatalf("expected score rate limit 9, got %d", cfg.ScoreRateLimit)
	}
}

func TestLoadUsesEnvForFilterTitleOnly(t *testing.T) {
	t.Setenv("ZHUIMI_FILTER_TITLE_ONLY", "false")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FilterTitleOnly {
		t.Fatalf("expected title-only filter disabled, got %+v", cfg)
	}
}

func TestLoadUsesEnvForAllowTitleOnlyFor(t *testing.T) {
	t.Setenv("ZHUIMI_ALLOW_TITLE_ONLY_FOR", "example.com, https://example.org/feed.xml")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowTitleOnlyFor) != 2 {
		t.Fatalf("expected 2 allow patterns, got %#v", cfg.AllowTitleOnlyFor)
	}
}

func TestLoadUsesEnvForProcessorsEnabled(t *testing.T) {
	t.Setenv("ZHUIMI_PROCESSORS_ENABLED", "generic_digest,aml_score,generic_digest")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ProcessorsEnabled) != 2 || cfg.ProcessorsEnabled[0] != "generic_digest" || cfg.ProcessorsEnabled[1] != "aml_score" {
		t.Fatalf("unexpected processors enabled: %#v", cfg.ProcessorsEnabled)
	}
}
