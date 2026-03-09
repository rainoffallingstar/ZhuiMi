package pipeline

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"zhuimi/internal/model"
)

func TestScoreProgressOutput(t *testing.T) {
	oldWriter := scoreProgressWriter
	defer func() { scoreProgressWriter = oldWriter }()

	var buf bytes.Buffer
	scoreProgressWriter = &buf

	progress := newScoreProgress(3, 2)
	progress.Start()
	progress.Advance(scoreResult{Article: model.Article{Title: "Paper A"}})
	progress.Advance(scoreResult{Article: model.Article{Title: "Paper B"}, ScoreErr: errors.New("rate limit")})
	progress.Advance(scoreResult{Article: model.Article{Title: "Paper C"}, FatalErr: errors.New("save score: disk full")})
	progress.Done()

	output := buf.String()
	checks := []string{
		"[scoring] start total=3 workers=2",
		"[scoring] 1/3 ok ok=1 failed=0",
		"[scoring] 2/3 failed ok=1 failed=1 title=\"Paper B\" error=\"rate limit\"",
		"[scoring] 3/3 failed ok=1 failed=2 title=\"Paper C\" fatal=\"save score: disk full\"",
		"[scoring] done total=3 ok=1 failed=2",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Fatalf("expected output to contain %q, got %q", check, output)
		}
	}
}
