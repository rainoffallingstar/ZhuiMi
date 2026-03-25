package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

type GenericDigestProcessor struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	temp       float64
}

func NewGenericDigestProcessor(cfg config.Config) *GenericDigestProcessor {
	modelName := cfg.OpenAIModel
	if strings.TrimSpace(cfg.GenericDigestModel) != "" {
		modelName = strings.TrimSpace(cfg.GenericDigestModel)
	}
	return &GenericDigestProcessor{
		httpClient: &http.Client{Timeout: cfg.OpenAITimeout},
		baseURL:    normalizeAPIURL(cfg.OpenAIBaseURL),
		apiKey:     cfg.OpenAIAPIKey,
		model:      modelName,
		maxTokens:  cfg.OpenAIMaxTokens,
		temp:       cfg.OpenAITemperature,
	}
}

func (p *GenericDigestProcessor) Name() string { return "generic_digest" }

func (p *GenericDigestProcessor) Process(ctx context.Context, input Input) (Output, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return Output{}, fmt.Errorf("OPENAI_API_KEY is required for processor %s", p.Name())
	}
	prompt := fmt.Sprintf(`请基于下面的文章信息，输出一个严格合法的 JSON 对象，字段固定为：
{
  "summary": "100-180字中文摘要",
  "key_points": ["3-5条要点"],
  "tags": ["3-8个短标签"],
  "entities": ["文中关键实体、基因、疾病、方法或组织"],
  "topics": ["1-3个主题分类"],
  "content_source": "%s"
}

要求：
1. 只输出 JSON，不要输出 Markdown 代码块
2. 若信息不足，保持字段存在但内容从简
3. 全部用中文输出

标题：%s

摘要：%s

正文：
%s
`, input.SourceType(), input.Article.Title, truncate(input.Article.Abstract, 2000), truncate(input.Text(), 12000))

	content, err := chatCompletion(ctx, p.httpClient, p.baseURL, p.apiKey, p.model, p.maxTokens, p.temp, prompt)
	if err != nil {
		return Output{}, err
	}
	normalized := normalizeJSONContent(content)
	if !json.Valid([]byte(normalized)) {
		fallback, marshalErr := json.Marshal(map[string]any{
			"summary":        strings.TrimSpace(content),
			"key_points":     []string{},
			"tags":           []string{},
			"entities":       []string{},
			"topics":         []string{},
			"content_source": input.SourceType(),
		})
		if marshalErr != nil {
			return Output{}, fmt.Errorf("marshal generic digest fallback: %w", marshalErr)
		}
		normalized = string(fallback)
	}
	return Output{
		Result: model.ProcessorResult{
			ArticleID:   input.Article.ID,
			Processor:   p.Name(),
			Model:       p.model,
			Status:      model.ProcessorStatusProcessed,
			OutputJSON:  normalized,
			RawResponse: content,
			ProcessedAt: time.Now().UTC(),
		},
	}, nil
}

func chatCompletion(ctx context.Context, httpClient *http.Client, baseURL, apiKey, modelName string, maxTokens int, temperature float64, prompt string) (string, error) {
	body := map[string]any{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "system", "content": "你是一个严谨的中文科研信息抽取助手。"},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode processor request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("create processor request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request processor: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read processor response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("processor status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", fmt.Errorf("decode processor response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty processor response")
	}
	return parsed.Choices[0].Message.Content, nil
}

func normalizeAPIURL(base string) string {
	raw := strings.TrimSpace(base)
	hadTrailingSlash := strings.HasSuffix(raw, "/")
	base = strings.TrimRight(raw, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return strings.TrimSuffix(base, "/chat/completions")
	}
	if hadTrailingSlash {
		return base
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}

func normalizeJSONContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
