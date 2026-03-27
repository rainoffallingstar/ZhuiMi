package pipeline

import (
	"fmt"
	"io"
	"os"
	"strings"
)

var processorProgressWriter io.Writer = os.Stderr

type processorProgress struct {
	total     int
	completed int
	succeeded int
	failed    int
}

func newProcessorProgress(total int) *processorProgress {
	return &processorProgress{total: total}
}

func (p *processorProgress) Start() {
	if p == nil || p.total == 0 {
		return
	}
	fmt.Fprintf(processorProgressWriter, "[processors] start total=%d\n", p.total)
}

func (p *processorProgress) Advance(task ProcessorTask, err error) {
	if p == nil || p.total == 0 {
		return
	}
	p.completed++
	status := "ok"
	if err != nil {
		p.failed++
		status = "failed"
	} else {
		p.succeeded++
	}

	message := fmt.Sprintf("[processors] %d/%d %s ok=%d failed=%d processor=%q title=%q", p.completed, p.total, status, p.succeeded, p.failed, task.Processor, summarizeProcessorTitle(task.Article.Title))
	if err != nil {
		message += fmt.Sprintf(" error=%q", summarizeProcessorError(err.Error()))
	}
	fmt.Fprintln(processorProgressWriter, message)
}

func (p *processorProgress) Done() {
	if p == nil || p.total == 0 {
		return
	}
	fmt.Fprintf(processorProgressWriter, "[processors] done total=%d ok=%d failed=%d\n", p.total, p.succeeded, p.failed)
}

func summarizeProcessorTitle(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 80 {
		return value
	}
	return value[:77] + "..."
}

func summarizeProcessorError(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 120 {
		return value
	}
	return value[:117] + "..."
}
