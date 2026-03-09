package scoring

import "testing"

func TestParseScoreContentStructuredReviewerOutput(t *testing.T) {
	content := `# 1. 基本判断
- 文章类型：机制研究
- 研究主题一句话概括：研究某信号轴在白血病中的功能
- 总体评价一句话概括：研究问题较重要，但当前证据链仍不够闭环。
- 本次评分置信度：低
- 置信度说明：仅基于摘要。

# 4. 分数汇总
- 研究本身价值小计（60分制）：41
- AML借鉴价值小计（30分制）：22
- 表达与可复用性小计（10分制）：7
- 扣分项合计：4
- 最终总分：66

# 5. 推荐度
- 推荐等级：有条件推荐
- 推荐理由（2-4条）：
- 不推荐/保留意见的关键原因（如有）：证据深度不足

# 9. 最终结论
这篇文章有一定启发，但更适合作为思路参考。`

	score := parseScoreContent(content)
	if score.Research != 41 || score.Social != 22 || score.Blood != 7 || score.Recommendation != 66 {
		t.Fatalf("unexpected parsed score: %+v", score)
	}
	if score.Reason == "" || score.Reason != "推荐等级：有条件推荐；研究问题较重要，但当前证据链仍不够闭环。；置信度：低" {
		t.Fatalf("unexpected parsed reason: %q", score.Reason)
	}
}

func TestParseScoreContentLegacyOutputFallback(t *testing.T) {
	content := "Research: 80\nSocial: 70\nBlood: 65\nRecommendation: 75\n\nReason: 值得关注"
	score := parseScoreContent(content)
	if score.Research != 80 || score.Social != 70 || score.Blood != 65 || score.Recommendation != 75 {
		t.Fatalf("unexpected legacy parsed score: %+v", score)
	}
	if score.Reason != "值得关注" {
		t.Fatalf("unexpected legacy reason: %q", score.Reason)
	}
}

func TestNormalizeAPIURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "auto append v1", base: "https://api.openai.com", want: "https://api.openai.com/v1"},
		{name: "keep explicit v1", base: "https://api-inference.modelscope.cn/v1", want: "https://api-inference.modelscope.cn/v1"},
		{name: "keep explicit endpoint", base: "https://api-inference.modelscope.cn/v1/chat/completions", want: "https://api-inference.modelscope.cn/v1"},
		{name: "trailing slash disables auto v1", base: "https://api-inference.modelscope.cn/", want: "https://api-inference.modelscope.cn"},
		{name: "trailing slash on v1 stays v1", base: "https://api-inference.modelscope.cn/v1/", want: "https://api-inference.modelscope.cn/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAPIURL(tt.base); got != tt.want {
				t.Fatalf("normalizeAPIURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}
