package config

import (
	"os"
	"path/filepath"
	"testing"
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
ai:
  model: "demo-model"
  max_tokens: 1234
  temperature: 0.7
  timeout_seconds: 42
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

	if cfg.DaysWindow != 5 || cfg.MaxFeeds != 12 || cfg.OpenAIModel != "demo-model" || cfg.SortBy != "blood" || cfg.MaxArticles != 88 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
