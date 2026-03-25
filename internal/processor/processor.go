package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
	"zhuimi/internal/scoring"
)

type Input struct {
	Article model.Article
	Content *model.ArticleContent
}

func (i Input) Text() string {
	if i.Content != nil && strings.TrimSpace(i.Content.ContentText) != "" {
		return strings.TrimSpace(i.Content.ContentText)
	}
	return strings.TrimSpace(i.Article.Abstract)
}

func (i Input) SourceType() string {
	if i.Content == nil {
		return "rss_description"
	}
	return strings.TrimSpace(i.Content.SourceType)
}

type Output struct {
	Result      model.ProcessorResult
	LegacyScore *model.Score
}

type Processor interface {
	Name() string
	Process(ctx context.Context, input Input) (Output, error)
}

func Enabled(cfg config.Config) []string {
	return append([]string(nil), cfg.ProcessorsEnabled...)
}

func Build(cfg config.Config, names []string) ([]Processor, error) {
	if len(names) == 0 {
		names = Enabled(cfg)
	}
	processors := make([]Processor, 0, len(names))
	for _, name := range names {
		switch strings.TrimSpace(name) {
		case "", "generic_digest":
			processors = append(processors, NewGenericDigestProcessor(cfg))
		case "aml_score":
			processors = append(processors, NewAMLScoreProcessor(cfg))
		default:
			return nil, fmt.Errorf("unknown processor: %s", name)
		}
	}
	return processors, nil
}

type AMLScoreProcessor struct {
	client *scoring.Client
	model  string
}

func NewAMLScoreProcessor(cfg config.Config) *AMLScoreProcessor {
	clone := cfg
	if strings.TrimSpace(cfg.AMLScoreModel) != "" {
		clone.OpenAIModel = strings.TrimSpace(cfg.AMLScoreModel)
	}
	return &AMLScoreProcessor{client: scoring.NewClient(clone), model: clone.OpenAIModel}
}

func (p *AMLScoreProcessor) Name() string { return "aml_score" }

func (p *AMLScoreProcessor) Process(ctx context.Context, input Input) (Output, error) {
	article := input.Article
	article.Abstract = input.Text()
	score, err := p.client.ScoreArticle(ctx, article)
	if err != nil {
		return Output{}, err
	}
	encoded, err := json.Marshal(score)
	if err != nil {
		return Output{}, fmt.Errorf("marshal aml score: %w", err)
	}
	return Output{
		Result: model.ProcessorResult{
			ArticleID:   input.Article.ID,
			Processor:   p.Name(),
			Model:       p.model,
			Status:      model.ProcessorStatusProcessed,
			OutputJSON:  string(encoded),
			RawResponse: score.RawResponse,
			ProcessedAt: score.ScoredAt,
		},
		LegacyScore: &score,
	}, nil
}
