package pipeline

import (
	"fmt"
	"io"
	"os"
	"strings"

	"zhuimi/internal/model"
)

var scoreProgressWriter io.Writer = os.Stderr

type scoreResult struct {
	Article  model.Article
	ScoreErr error
	FatalErr error
}

type scoreProgress struct {
	total     int
	workers   int
	completed int
	succeeded int
	failed    int
}

func newScoreProgress(total int, workers int) *scoreProgress {
	return &scoreProgress{total: total, workers: workers}
}

func (p *scoreProgress) Start() {
	if p == nil || p.total == 0 {
		return
	}
	fmt.Fprintf(scoreProgressWriter, "[scoring] start total=%d workers=%d\n", p.total, p.workers)
}

func (p *scoreProgress) Advance(result scoreResult) {
	if p == nil || p.total == 0 {
		return
	}
	p.completed++
	status := "ok"
	if result.ScoreErr != nil || result.FatalErr != nil {
		p.failed++
		status = "failed"
	} else {
		p.succeeded++
	}

	message := fmt.Sprintf("[scoring] %d/%d %s ok=%d failed=%d", p.completed, p.total, status, p.succeeded, p.failed)
	if result.ScoreErr != nil {
		message += fmt.Sprintf(" title=%q error=%q", summarizeTitle(result.Article.Title), summarizeError(result.ScoreErr.Error()))
	}
	if result.FatalErr != nil {
		message += fmt.Sprintf(" title=%q fatal=%q", summarizeTitle(result.Article.Title), summarizeError(result.FatalErr.Error()))
	}
	fmt.Fprintln(scoreProgressWriter, message)
}

func (p *scoreProgress) Done() {
	if p == nil || p.total == 0 {
		return
	}
	fmt.Fprintf(scoreProgressWriter, "[scoring] done total=%d ok=%d failed=%d\n", p.total, p.succeeded, p.failed)
}

func summarizeTitle(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 80 {
		return value
	}
	return value[:77] + "..."
}

func summarizeError(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 120 {
		return value
	}
	return value[:117] + "..."
}
