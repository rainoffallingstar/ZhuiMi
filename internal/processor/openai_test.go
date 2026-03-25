package processor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

func TestGenericDigestProcessorProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"summary":"摘要","key_points":["要点1"],"tags":["标签"],"entities":["AML"],"topics":["文献追踪"],"content_source":"html_body"}`,
				},
			}},
		})
	}))
	defer server.Close()

	cfg := config.Config{
		OpenAIBaseURL:     server.URL,
		OpenAIAPIKey:      "demo-key",
		OpenAIModel:       "demo-model",
		OpenAIMaxTokens:   1024,
		OpenAITemperature: 0.1,
		OpenAITimeout:     time.Second,
	}
	output, err := NewGenericDigestProcessor(cfg).Process(context.Background(), Input{
		Article: model.Article{ID: "a1", Title: "Title", Abstract: "Abstract"},
		Content: &model.ArticleContent{ContentText: "Body", SourceType: "html_body"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Result.Processor != "generic_digest" || output.Result.Status != model.ProcessorStatusProcessed {
		t.Fatalf("unexpected output: %+v", output)
	}
	if !json.Valid([]byte(output.Result.OutputJSON)) {
		t.Fatalf("expected valid json output, got %q", output.Result.OutputJSON)
	}
}

func TestAMLScoreProcessorProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "# 4. 分数汇总\n- 研究本身价值小计（60分制）：40\n- AML借鉴价值小计（30分制）：21\n- 表达与可复用性小计（10分制）：7\n- 最终总分：68\n\n# 5. 推荐度\n- 推荐等级：有条件推荐\n\n# 9. 最终结论\n有一定价值。",
				},
			}},
		})
	}))
	defer server.Close()

	cfg := config.Config{
		OpenAIBaseURL:     server.URL,
		OpenAIAPIKey:      "demo-key",
		OpenAIModel:       "demo-model",
		OpenAIMaxTokens:   1024,
		OpenAITemperature: 0.1,
		OpenAITimeout:     time.Second,
	}
	output, err := NewAMLScoreProcessor(cfg).Process(context.Background(), Input{
		Article: model.Article{ID: "a1", Title: "Title", Abstract: "Abstract"},
		Content: &model.ArticleContent{ContentText: "Body", SourceType: "html_body"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.LegacyScore == nil || output.LegacyScore.Recommendation != 68 {
		t.Fatalf("unexpected legacy score: %+v", output.LegacyScore)
	}
	if output.Result.Processor != "aml_score" || output.Result.OutputJSON == "" {
		t.Fatalf("unexpected processor output: %+v", output.Result)
	}
}
