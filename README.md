# ZhuiMi

ZhuiMi 是独立的 PubMed 文献追踪与 AI 评分系统。

## 功能
- 从 OPML 提取 PubMed RSS 源
- 抓取近几天新文献并去重
- 调用 OpenAI 兼容接口进行评分与推荐
- 生成 Typst 日报到 `content/ZhuiMi/YYYY-MM-DD/index.typ`

## 目录
- `scripts/zhuimi_update.py`：主流程
- `scripts/extract_pubmed_feeds.py`：从 OPML 生成 feeds 列表
- `scripts/config/zhuimi_config.yaml`：参数配置
- `scripts/pubmed_feeds.json`：RSS 源清单
- `content/ZhuiMi/`：日报输出与历史数据
- `.github/workflows/zhuimi.yml`：每日自动运行

## 本地运行
```bash
export OPENAI_BASE_URL=https://api.openai.com
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4o-mini

python scripts/extract_pubmed_feeds.py
python scripts/zhuimi_update.py
```

## GitHub Actions Secrets
- `OPENAI_BASE_URL`
- `OPENAI_API_KEY`
- `OPENAI_MODEL`

计划任务：每天 UTC 22:00（北京时间次日 06:00）。
