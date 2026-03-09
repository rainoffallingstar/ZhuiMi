# ZhuiMi

ZhuiMi 是一个面向 PubMed 的文献追踪、结构化评分与日报生成工具，当前以 Go 版本为唯一维护主线。

## 概览

ZhuiMi 主要完成以下工作：

- 从 OPML 导入并管理 PubMed RSS 订阅
- 增量抓取近几天新文献，并基于数据库去重与持久化
- 调用 OpenAI 兼容接口进行结构化 AI 评分
- 生成 Typst 日报，并可继续编译为 PDF
- 通过 GitHub Actions 自动运行、更新数据库并回写仓库

## 项目结构

- `cmd/zhuimi/`：Go CLI 入口
- `internal/`：抓取、存储、评分、报告生成等核心逻辑
- `data/zhuimi/store.db`：SQLite 状态库
- `content/ZhuiMi/`：按日期输出的日报目录
- `scripts/config/zhuimi_config.yaml`：运行参数配置
- `scripts/pubmed_feeds.json`：订阅源清单
- `.github/workflows/zhuimi.yml`：自动化工作流

## 快速开始

先准备 OpenAI 兼容接口相关环境变量：

```bash
export OPENAI_BASE_URL=https://api.openai.com
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4o-mini
export ZHUIMI_SCORE_RATE_LIMIT=1
```

然后执行最基本流程：

```bash
go run ./cmd/zhuimi feeds import
go run ./cmd/zhuimi run daily
```

执行完成后，默认会输出：

- Typst 日报：`content/ZhuiMi/YYYY-MM-DD/index.typ`
- SQLite 状态库：`data/zhuimi/store.db`

## 常用命令

### 订阅管理

```bash
go run ./cmd/zhuimi feeds import
go run ./cmd/zhuimi feeds list --enabled=true
go run ./cmd/zhuimi feeds list --status=inactive
go run ./cmd/zhuimi feeds status
```

说明：

- `feeds import`：从 OPML/配置源导入订阅，并保留已有状态
- `feeds list --enabled=true`：查看当前启用订阅
- `feeds list --status=inactive`：查看已停用订阅
- `feeds status`：查看订阅总量、启用数与状态分布

### 日常运行

```bash
go run ./cmd/zhuimi run daily
go run ./cmd/zhuimi run daily --skip-scoring
go run ./cmd/zhuimi run daily --pdf
```

说明：

- `run daily`：抓取、评分并生成日报
- `run daily --skip-scoring`：只抓取、入库并输出原始日报，不调用 AI 评分
- `run daily --pdf`：在生成 `index.typ` 后继续编译 `index.pdf`

### 历史回填与重试

```bash
go run ./cmd/zhuimi run backfill --days=7
go run ./cmd/zhuimi run backfill --days=7 --skip-scoring --pdf
go run ./cmd/zhuimi retry failed-scores --limit=20
```

说明：

- `run backfill --days=7`：回填最近 7 天历史数据
- `run backfill --days=7 --skip-scoring --pdf`：仅抓取并输出历史 PDF 日报
- `retry failed-scores`：重试此前评分失败的文章

### 报告与文章查询

```bash
go run ./cmd/zhuimi report rebuild --date=2026-03-09
go run ./cmd/zhuimi report rebuild --date=2026-03-09 --pdf
go run ./cmd/zhuimi report prune --keep-days=30
go run ./cmd/zhuimi articles list --limit=10
go run ./cmd/zhuimi articles list --min-total=75 --min-aml=20 --level=推荐
go run ./cmd/zhuimi articles top --date=2026-03-09 --table
```

说明：

- `report rebuild`：基于数据库内已保存结果重建指定日期报告
- `report prune`：清理旧报告文件
- `articles list`：按条件筛选文章及评分摘要
- `articles top --table`：以终端表格查看某日高分文章

### 数据库维护

```bash
go run ./cmd/zhuimi db stats
go run ./cmd/zhuimi db vacuum
go run ./cmd/zhuimi migrate legacy
```

说明：

- `db stats`：查看数据库概况
- `db vacuum`：压缩并整理 SQLite 数据库
- `migrate legacy`：导入历史报告和旧版分析状态

## 运行模式说明

### 1. 标准日报模式

`go run ./cmd/zhuimi run daily`

流程包括：

1. 读取订阅并增量抓取 RSS
2. 去重后写入 SQLite
3. 调用 AI 进行结构化评分
4. 生成 Typst 日报

### 2. 仅抓取不评分

`go run ./cmd/zhuimi run daily --skip-scoring`

适合以下场景：

- 先验证抓取结果是否正常
- 暂时不希望消耗模型调用额度
- 只想输出未评分原始日报

### 3. 直接生成 PDF

`go run ./cmd/zhuimi run daily --pdf`

或：

`go run ./cmd/zhuimi report rebuild --date=2026-03-09 --pdf`

会在 Typst 文件生成后继续调用 `typst compile`，输出 PDF 文件。

## 数据与状态管理

当前主流程采用 SQLite 持久化，默认数据库路径为 `data/zhuimi/store.db`。

数据库中会保存：

- feed 元信息与启用状态
- 文章抓取记录与去重结果
- AI 评分结果与推荐等级
- 每日报告索引
- 抓取过程所需的 `ETag` / `Last-Modified` 等增量状态

程序启动时会自动执行 schema migration，无需手工初始化数据库。

## GitHub Actions 自动更新

工作流文件：`/.github/workflows/zhuimi.yml`

当前自动化流程会：

1. 安装 Go 与 Typst
2. 运行测试
3. 导入订阅
4. 执行 `go run ./cmd/zhuimi run daily --pdf`
5. 执行数据库整理
6. 提交 `content/ZhuiMi/`、`scripts/pubmed_feeds.json` 与 `data/zhuimi/store.db` 的变更
7. 上传生成的 PDF artifact

这样可以保留：

- 历史文章状态
- 抓取增量缓存
- 评分结果
- 报告索引

仅当上述文件确实发生变化时，工作流才会执行 commit / push。

## GitHub Actions Secrets

需要在仓库中配置：

- `OPENAI_BASE_URL`
- `OPENAI_API_KEY`
- `OPENAI_MODEL`
- `ZHUIMI_SCORE_RATE_LIMIT`（可选，默认 `1`，表示每秒评分请求数）

当前计划任务时间：每天 UTC 22:00，即北京时间次日 06:00。

## 说明

- 当前仓库以 Go 实现为准，不再维护 Python 兼容流程
- 若仓库内仍有旧时代产物，可通过 `go run ./cmd/zhuimi migrate legacy` 迁移到 SQLite
