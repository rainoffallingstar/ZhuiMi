package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"zhuimi/internal/config"
	"zhuimi/internal/model"
)

var legacyScorePatterns = map[string]*regexp.Regexp{
	"research":       regexp.MustCompile(`(?i)(?:Research|研究)[\s:：]+(\d+)`),
	"social":         regexp.MustCompile(`(?i)(?:Social|社会)[\s:：]+(\d+)`),
	"blood":          regexp.MustCompile(`(?i)(?:Blood|血液)[\s:：]+(\d+)`),
	"recommendation": regexp.MustCompile(`(?i)(?:Recommendation|推荐)[\s:：]+(\d+)`),
}

var legacyReasonPattern = regexp.MustCompile(`(?is)(?:Reason|理由)[\s:：]+(.+)$`)
var researchSubtotalPattern = regexp.MustCompile(`(?m)^-\s*研究本身价值小计（60分制）\s*[:：]\s*(\d+)`)
var amlSubtotalPattern = regexp.MustCompile(`(?m)^-\s*AML借鉴价值小计（30分制）\s*[:：]\s*(\d+)`)
var expressionSubtotalPattern = regexp.MustCompile(`(?m)^-\s*表达与可复用性小计（10分制）\s*[:：]\s*(\d+)`)
var finalTotalPattern = regexp.MustCompile(`(?m)^-\s*最终总分\s*[:：]\s*(\d+)`)
var recommendationLevelPattern = regexp.MustCompile(`(?m)^-\s*推荐等级\s*[:：]\s*(.+?)\s*$`)
var overallEvaluationPattern = regexp.MustCompile(`(?m)^-\s*总体评价一句话概括\s*[:：]\s*(.+?)\s*$`)
var confidencePattern = regexp.MustCompile(`(?m)^-\s*本次评分置信度\s*[:：]\s*(.+?)\s*$`)
var finalConclusionPattern = regexp.MustCompile(`(?s)#\s*9\.\s*最终结论\s*(.+)$`)

type Client struct {
	httpClient *http.Client
	config     config.Config
}

func NewClient(cfg config.Config) *Client {
	return &Client{httpClient: &http.Client{Timeout: cfg.OpenAITimeout}, config: cfg}
}

func (c *Client) ScoreArticle(ctx context.Context, article model.Article) (model.Score, error) {
	if strings.TrimSpace(c.config.OpenAIAPIKey) == "" {
		return model.Score{}, fmt.Errorf("OPENAI_API_KEY is required for scoring")
	}

	prompt := fmt.Sprintf(`你现在是一名严格、专业、审慎的血液学与白血病基础研究审稿专家，尤其熟悉 AML（急性髓系白血病）相关的基础研究、生物信息学研究、单细胞研究、机制研究、造血干/祖细胞研究、肿瘤微环境、表观遗传、代谢、耐药与复发研究。

你的任务不是简单总结文章，而是对这篇文章进行“研究质量 + AML借鉴价值”的双重评估，并给出结构化评分、推荐等级和具体理由。

请你严格按照以下要求执行：

========================
一、总体任务
========================
请完整阅读我提供的文章内容，并对其进行以下评估：

1. 评价这篇文章“研究本身”的学术价值与可信度
2. 评价这篇文章“对AML基础研究”的借鉴价值
3. 给出结构化评分、优点、缺点、核心启发点
4. 给出明确推荐度：强烈推荐 / 推荐 / 有条件推荐 / 一般 / 不推荐
5. 如果文章证据不足、结论夸大、仅停留于相关性、生信验证薄弱，请明确指出，不要出于礼貌提高评分

请注意：
- 你必须基于文章内容本身评分，不能因为期刊名、作者单位、影响因子而直接加分
- 如果某部分信息在文中缺失，请明确写“文中未充分提供，故此项降权/降分”
- 不要把“结果有趣”误判为“证据充分”
- 不要把“生信相关性”误判为“机制成立”
- 不要把“非AML研究”硬说成“对AML很有价值”；只有在逻辑上确实可迁移时才给高分
- 如果文章是纯生信研究，请特别审查其外部验证、队列独立性、混杂因素处理、是否有实验验证
- 如果文章是机制研究，请特别审查其因果证据、gain/loss-of-function、rescue experiment、原代样本和体内支持

如果我提供的不是全文，而只是摘要、引言、结果、图注、补充材料中的一部分，请你先说明“本次评分基于有限文本，置信度受限”，但仍要按框架完成评分。

请使用中文输出。

========================
二、评分框架（总分100分）
========================

请按以下维度逐项评分，并给出每项：
- 分数
- 打分理由（1-3句话，必须具体）
- 是否存在明显短板

--------------------------------
A. 研究本身价值（60分）
--------------------------------

1. 科学问题的重要性与原创性（15分）
评估重点：
- 问题是否重要，而不是边缘问题
- 是否有明显创新，而不是重复已有结论
- 是否提出了新机制、新模型、新靶点、新分析框架
- 是否推进了领域认知

评分参考：
- 13-15：问题重要且创新明显
- 9-12：问题较重要，有一定新意
- 5-8：问题一般，创新有限
- 0-4：重复性较强，边际价值低

2. 研究设计与技术路线合理性（15分）
评估重点：
- 假说是否清晰
- 技术路线是否能够真正回答问题
- 发现、验证、机制、功能是否形成闭环
- 细胞系、原代样本、动物模型、患者队列是否选择合理
- 对照设置是否充分
- 生信研究中是否有独立验证集、多队列验证、混杂控制

评分参考：
- 13-15：设计严谨，逻辑闭环强
- 9-12：总体合理，有小瑕疵
- 5-8：存在明显短板
- 0-4：设计不足以支持核心结论

3. 数据质量与证据强度（15分）
评估重点：
- 样本量是否足够
- 重复是否充分
- 关键结论是否有多层证据支持
- 是否区分相关性证据与因果性证据
- 是否有原代样本、体内外一致性、功能验证、rescue等强证据
- 生信文章是否有外部验证、稳健统计、交叉验证或独立队列复现

评分参考：
- 13-15：证据扎实，多层验证
- 9-12：证据较强，但关键点仍有薄弱处
- 5-8：证据主要停留在相关层面
- 0-4：证据明显不足，结论跳跃

4. 机制深度与生物学解释力（10分）
评估重点：
- 是否从现象推进到机制
- 是否揭示上下游关系
- 是否能解释生物学过程，而非仅呈现关联
- 是否对AML/造血系统的特异性有解释力

评分参考：
- 9-10：机制链条完整，解释力强
- 6-8：有机制探索，但深度有限
- 3-5：以现象描述为主
- 0-2：几乎无机制深度

5. 结论可信度与局限性处理（5分）
评估重点：
- 结论是否与证据匹配
- 是否存在明显过度外推
- 是否诚实讨论局限性
- 是否把“支持”说成“证明”

评分参考：
- 5：结论稳健，边界清楚
- 3-4：总体可信，但有轻度夸大
- 1-2：外推明显
- 0：结论与证据不匹配

--------------------------------
B. 对AML基础研究的借鉴价值（30分）
--------------------------------

6. 与AML核心问题的相关性（10分）
评估重点：
- 是否直接研究AML
- 若非AML，是否与造血干细胞、MDS、ALL、正常造血、骨髓微环境等具有明确可迁移性
- 是否与以下AML核心问题之一高度相关：
  - 白血病干细胞（LSC）
  - 克隆演化
  - 分化阻滞
  - 耐药/复发
  - 骨髓微环境
  - 表观遗传异常
  - 代谢重编程
  - 炎症/免疫调控
  - 发病驱动事件与脆弱性

评分参考：
- 9-10：直接切中AML核心问题
- 6-8：与AML高度相关，可直接迁移
- 3-5：有一定相关性，但需较多转译
- 0-2：与AML关联较弱

7. 对AML研究的启发深度（10分）
评估重点：
- 是否能形成新的AML研究假说
- 是否能帮助解释AML中某些未解决问题
- 是否能衍生出具体可做的课题
- 是否对AML的机制研究、靶点研究、状态定义、分层分析有明确启发

评分参考：
- 9-10：可直接催生新课题
- 6-8：有明确启发，值得跟进
- 3-5：有一些启发，但较泛
- 0-2：启发有限

8. 方法学与资源可借鉴性（10分）
评估重点：
- 是否有可直接借鉴的方法、分析流程、marker、signature、数据资源
- 是否对AML实验或生信分析具有实际可复用性
- 是否提供了可迁移的分析框架或验证策略

评分参考：
- 9-10：方法和资源高度可复用
- 6-8：部分可迁移
- 3-5：思路可借鉴，但难直接应用
- 0-2：借鉴价值有限

--------------------------------
C. 表达与可复用性（10分）
--------------------------------

9. 文章表达清晰度（5分）
评估重点：
- 逻辑是否清晰
- 图表是否有效支持结论
- 方法描述是否可理解
- 生信流程是否足够透明

10. 数据/代码/材料开放程度（5分）
评估重点：
- 数据是否公开
- 代码是否可得
- 方法参数是否透明
- 是否利于复现

========================
三、扣分项（总计可扣0-15分）
========================

请检查是否存在以下问题，并进行扣分。每项都要写明理由：

- 纯相关分析，缺乏功能验证：扣 3-5 分
- 仅使用细胞系，无原代/体内支持：扣 2-4 分
- 样本量明显不足：扣 2-4 分
- 生信流程不透明，缺少独立验证：扣 3-5 分
- 结论明显夸大：扣 2-4 分
- 与AML映射牵强：扣 2-5 分
- 其他你认为影响可信度的问题：酌情扣分

请给出：
- 扣分项名称
- 扣分分值
- 扣分理由

========================
四、推荐度判定标准
========================

请根据“最终得分 + 研究本身价值 + AML借鉴价值”综合判断推荐度：

1. 强烈推荐
条件：
- 总分 ≥ 85
- 且研究本身价值 ≥ 50/60
- 且AML借鉴价值 ≥ 24/30

2. 推荐
条件：
- 总分 75-84
- 且无明显致命硬伤

3. 有条件推荐
条件：
- 总分 65-74
- 某一方面突出，但整体不均衡

4. 一般
条件：
- 总分 50-64

5. 不推荐
条件：
- 总分 < 50
或存在以下之一：
- 核心结论证据明显不足
- 过度依赖相关分析且缺乏验证
- AML借鉴价值极低
- 设计存在明显逻辑漏洞

========================
五、输出格式（必须严格按照下面格式）
========================

请严格按照以下结构输出：

# 1. 基本判断
- 文章类型：机制研究 / 生信研究 / 单细胞研究 / 资源型研究 / 其他
- 研究主题一句话概括：
- 总体评价一句话概括：
- 本次评分置信度：高 / 中 / 低
- 置信度说明：

# 2. 结构化评分表
请用表格输出，列为：
维度 | 满分 | 得分 | 评分理由 | 是否存在明显短板

需要覆盖以下10项：
1）科学问题的重要性与原创性
2）研究设计与技术路线合理性
3）数据质量与证据强度
4）机制深度与生物学解释力
5）结论可信度与局限性处理
6）与AML核心问题的相关性
7）对AML研究的启发深度
8）方法学与资源可借鉴性
9）文章表达清晰度
10）数据/代码/材料开放程度

# 3. 扣分项
请列出每一条扣分项，格式如下：
- 扣分项：
- 扣分：
- 理由：

如果没有明显扣分项，也请明确写“无明显额外扣分项”。

# 4. 分数汇总
- 研究本身价值小计（60分制）：
- AML借鉴价值小计（30分制）：
- 表达与可复用性小计（10分制）：
- 扣分项合计：
- 最终总分：

# 5. 推荐度
- 推荐等级：
- 推荐理由（2-4条）：
- 不推荐/保留意见的关键原因（如有）：

# 6. 核心优点
请提炼 3-5 条，必须具体，不要空泛。

# 7. 核心不足
请提炼 3-5 条，必须具体，不要只写“样本量小”“机制不够深”，而要说明它具体影响什么结论。

# 8. 对AML基础研究的具体借鉴点
请从AML研究者视角，输出以下内容：
- 最值得借鉴的1个核心思想
- 最值得借鉴的1-3个方法/分析策略
- 最可迁移到AML的生物学问题
- 可以衍生出的2-3个AML研究问题或课题方向

# 9. 最终结论
请用一段 150-250 字总结：
- 这篇文章值不值得精读
- 适合如何使用（精读/略读/做背景/借方法/仅作启发）
- 对AML基础研究者最大的价值是什么
- 最大的保留意见是什么

========================
六、特别要求
========================

1. 请保持“严格审稿人”标准，不要轻易给高分
2. 如果文章主要是相关性研究，请明确提醒“只可借鉴思路，不能直接相信机制结论”
3. 如果文章主要是非AML研究，但对AML可能有启发，请明确区分：
   - “对原研究领域成立”
   - “对AML是否已被证明”
4. 如果文章的方法很强但结论一般，请把“方法借鉴价值”与“机制可信度”分开评价
5. 如果文章证据链不完整，请指出最关键缺失实验或分析是什么
6. 不允许只给笼统评价，必须结合文章内容说明理由
7. 如果文章内容不足以支持某项判断，请直接说“不足以判断/证据不足”，并降低该项分数
8. 最后请额外补充一句：
   - “如果我是AML基础研究者，我会不会把这篇文献纳入重点跟踪清单：会 / 不会 / 有条件会”
   - 并说明一句理由

========================
七、待分析文章
========================

下面是文章内容：
【当前可用内容通常仅包含题目、摘要及少量元信息；如果不是全文，请先按有限文本处理并下调置信度】

标题：%s

摘要：%s

请基于以上内容完成评审。`, article.Title, truncate(article.Abstract, 4000))

	body := map[string]any{
		"model": c.config.OpenAIModel,
		"messages": []map[string]string{
			{"role": "system", "content": "你是一名严格的AML基础研究审稿专家。你必须保守评分、强调证据链、拒绝夸大结论，并严格遵守用户给定的输出结构。"},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  c.config.OpenAIMaxTokens,
		"temperature": c.config.OpenAITemperature,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return model.Score{}, fmt.Errorf("encode scoring request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeAPIURL(c.config.OpenAIBaseURL)+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return model.Score{}, fmt.Errorf("create scoring request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.config.OpenAIAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return model.Score{}, fmt.Errorf("request scoring: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.Score{}, fmt.Errorf("read scoring response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return model.Score{}, fmt.Errorf("scoring status %d: %s", resp.StatusCode, string(responseBody))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return model.Score{}, fmt.Errorf("decode scoring response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return model.Score{}, fmt.Errorf("empty scoring response")
	}

	content := parsed.Choices[0].Message.Content
	score := parseScoreContent(content)
	score.Model = c.config.OpenAIModel
	score.ScoredAt = time.Now().UTC()
	score.RawResponse = content
	if score.Reason == "" {
		score.Reason = "模型未返回可提取的总体结论，请手动复核原始评分文本"
	}
	return score, nil
}

func parseScoreContent(content string) model.Score {
	if score, ok := parseStructuredScoreContent(content); ok {
		return score
	}
	return parseLegacyScoreContent(content)
}

func parseStructuredScoreContent(content string) (model.Score, bool) {
	score := model.Score{}
	found := 0
	if value, ok := extractInt(researchSubtotalPattern, content); ok {
		score.Research = value
		found++
	}
	if value, ok := extractInt(amlSubtotalPattern, content); ok {
		score.Social = value
		found++
	}
	if value, ok := extractInt(expressionSubtotalPattern, content); ok {
		score.Blood = value
		found++
	}
	if value, ok := extractInt(finalTotalPattern, content); ok {
		score.Recommendation = value
		found++
	}

	reasonParts := make([]string, 0, 3)
	if level := extractText(recommendationLevelPattern, content); level != "" {
		reasonParts = append(reasonParts, "推荐等级："+level)
	}
	if summary := extractText(overallEvaluationPattern, content); summary != "" {
		reasonParts = append(reasonParts, summary)
	}
	if confidence := extractText(confidencePattern, content); confidence != "" {
		reasonParts = append(reasonParts, "置信度："+confidence)
	}
	if len(reasonParts) == 0 {
		if summary := extractText(finalConclusionPattern, content); summary != "" {
			reasonParts = append(reasonParts, singleLine(summary))
		}
	}
	if len(reasonParts) > 0 {
		score.Reason = truncate(strings.Join(reasonParts, "；"), 300)
	}
	return score, found >= 2
}

func parseLegacyScoreContent(content string) model.Score {
	score := model.Score{}
	for key, pattern := range legacyScorePatterns {
		match := pattern.FindStringSubmatch(content)
		if len(match) < 2 {
			continue
		}
		var value int
		fmt.Sscanf(match[1], "%d", &value)
		switch key {
		case "research":
			score.Research = value
		case "social":
			score.Social = value
		case "blood":
			score.Blood = value
		case "recommendation":
			score.Recommendation = value
		}
	}
	if match := legacyReasonPattern.FindStringSubmatch(content); len(match) > 1 {
		score.Reason = truncate(strings.TrimSpace(match[1]), 300)
	}
	return score
}

func extractInt(pattern *regexp.Regexp, content string) (int, bool) {
	match := pattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return 0, false
	}
	var value int
	if _, err := fmt.Sscanf(match[1], "%d", &value); err != nil {
		return 0, false
	}
	return value, true
}

func extractText(pattern *regexp.Regexp, content string) string {
	match := pattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func singleLine(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	return strings.Join(strings.Fields(value), " ")
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

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
