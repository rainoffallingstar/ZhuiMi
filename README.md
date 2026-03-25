# ZhuiMi

ZhuiMi 是一个面向 RSS/Atom 订阅的内容流水线工具，当前主链路已经支持：

- RSS/RDF/Atom 解析与增量抓取
- HTML 正文获取与摘要回退
- 可插拔 AI 处理器
- 领域化 AML 评分与日报生成

当前以 Go 版本为唯一维护主线。

本次改造后的推荐理解方式是：

- 默认能力是一个通用流水线：`RSS/Atom -> HTML 内容获取 -> AI processors`
- 默认 AI 处理器是 `generic_digest`
- `aml_score` 作为领域化兼容处理器保留，并继续服务现有 AML 日报

如果你是第一次接手这个项目，建议先看：

1. [CHANGELOG.md](CHANGELOG.md)
2. [README.md](README.md)
3. [scripts/config/zhuimi_config.yaml](scripts/config/zhuimi_config.yaml)
4. [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md)

## 概览

ZhuiMi 主要完成以下工作：

- 从 OPML 导入并管理 RSS/Atom 订阅
- 增量抓取近几天新文献，并基于数据库去重与持久化
- 抓取文章 HTML 正文，失败时回退到 RSS 摘要
- 调用 OpenAI 兼容接口执行可插拔 AI 处理
- 生成 Typst 日报，并可继续编译为 PDF
- 通过 GitHub Actions 自动运行、更新数据库并回写仓库

## 项目结构

- `cmd/zhuimi/`：Go CLI 入口
- `internal/`：抓取、存储、评分、报告生成等核心逻辑
- `data/zhuimi/store.db`：SQLite 状态库
- `content/ZhuiMi/`：按日期输出的日报目录
- `scripts/config/zhuimi_config.yaml`：运行参数配置
- `scripts/feeds.json`：订阅源清单
- `.github/workflows/zhuimi.yml`：自动化工作流

默认会过滤掉 `description` 为空或仅重复标题的条目；如果某些轻量 RSS/Atom 源需要保留这类内容，可以把 `rss.filter_title_only` 设为 `false`，或通过环境变量 `ZHUIMI_FILTER_TITLE_ONLY=false` 关闭该过滤。

对于 RSS 只给标题和链接、正文需要靠 HTML 抓取的源，当前 Go 流水线已经支持继续入库和补抓；也就是说，`description` 为空不会再天然阻断后续的正文获取和 AI 处理。

如果只想对部分订阅放行，可以设置 `rss.allow_title_only_for` 或环境变量 `ZHUIMI_ALLOW_TITLE_ONLY_FOR`。它支持逗号分隔的域名、完整 Feed URL，或带 `*` 的 URL 前缀，例如：`example.com, https://example.org/feed.xml, https://feeds.example.net/path/*`。

如果希望某个订阅在导入后自动记住这个策略，可以在 OPML 的对应 `outline` 上加 `allowTitleOnly="true"` 或 `zhuimiAllowTitleOnly="true"`；导入后该设置会持久化到本地 `feeds` 状态库，并在后续抓取时优先于全局配置生效。

## 快速开始

最小可用配置建议：

- 保持 `content.enabled=true`
- `processors.enabled` 先只开 `generic_digest`
- 需要继续产出 AML 日报时，再把 `aml_score` 加入 `processors.enabled`

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

如果想把通用正文获取和 AI 处理拆开跑，也可以：

```bash
go run ./cmd/zhuimi run fetch-content --limit=50
go run ./cmd/zhuimi run fetch-content --content-status=rss_fallback --limit=50
go run ./cmd/zhuimi run ai --processor=generic_digest --limit=50
go run ./cmd/zhuimi run ai --processor=generic_digest --processor-status=missing --limit=50
go run ./cmd/zhuimi run ai --processor=aml_score --limit=50
```

执行完成后，默认会输出：

- Typst 日报：`content/ZhuiMi/YYYY-MM-DD/index.typ`
- SQLite 状态库：`data/zhuimi/store.db`

如果你想用“更稳的首发方式”验证这次改造，推荐顺序是：

1. `go run ./cmd/zhuimi feeds import`
2. `go run ./cmd/zhuimi run daily --skip-scoring`
3. `go run ./cmd/zhuimi run fetch-content --content-status=rss_fallback --limit=50`
4. `go run ./cmd/zhuimi run ai --processor=generic_digest --processor-status=missing --limit=50`
5. 确认无误后，再启用 `aml_score`

## 常用命令

### 订阅管理

```bash
go run ./cmd/zhuimi feeds import
go run ./cmd/zhuimi feeds list --enabled=true --allow-title-only=inherit
go run ./cmd/zhuimi feeds list --domain=example.com --table
go run ./cmd/zhuimi feeds list --sort=enabled --reverse --table
go run ./cmd/zhuimi feeds list --sort=title-only --table
go run ./cmd/zhuimi feeds list --sort=has-error --reverse --table
go run ./cmd/zhuimi feeds list --sort=checked-missing --reverse --table
go run ./cmd/zhuimi feeds list --sort=success-missing --reverse --table
go run ./cmd/zhuimi feeds list --sort=domain --table
go run ./cmd/zhuimi feeds list --source=pubmed --table
go run ./cmd/zhuimi feeds list --sort=last-error --table
go run ./cmd/zhuimi feeds list --error-q=timeout --table
go run ./cmd/zhuimi feeds list --q=Nature --table
go run ./cmd/zhuimi feeds list --sort=source --table
go run ./cmd/zhuimi feeds list --sort=success --reverse --table
go run ./cmd/zhuimi feeds list --checked-missing=true --table
go run ./cmd/zhuimi feeds list --checked-after=2026-03-10 --table
go run ./cmd/zhuimi feeds list --checked-before=2026-03-20 --table
go run ./cmd/zhuimi feeds list --error-missing=false --sort=last-error --table
go run ./cmd/zhuimi feeds list --success-missing=true --sort=success --table
go run ./cmd/zhuimi feeds list --success-after=2026-03-10 --sort=success --table
go run ./cmd/zhuimi feeds list --success-before=2026-03-20 --sort=success --table
go run ./cmd/zhuimi feeds list --sort=title --table
go run ./cmd/zhuimi feeds list --sort=status --reverse --table
go run ./cmd/zhuimi feeds list --sort=title --limit=20 --table
go run ./cmd/zhuimi feeds list --allow-title-only=true --table
go run ./cmd/zhuimi feeds set --url=https://example.com/feed.xml --title="Example Feed"
go run ./cmd/zhuimi feeds set --url=https://example.com/feed.xml --allow-title-only=true
go run ./cmd/zhuimi feeds set --domain=example.com --allow-title-only=false --yes
go run ./cmd/zhuimi feeds set --source=pubmed.opml --status=inactive --yes
go run ./cmd/zhuimi feeds set --source=unknown --status=inactive --yes
go run ./cmd/zhuimi feeds set --domain=example.com --status=inactive --yes
go run ./cmd/zhuimi feeds set --all --status=active --yes
go run ./cmd/zhuimi feeds set --domain=example.com --status=inactive --dry-run
go run ./cmd/zhuimi feeds list --status=inactive
go run ./cmd/zhuimi feeds list --status=unknown --table
go run ./cmd/zhuimi feeds list --source=unknown --table
go run ./cmd/zhuimi feeds status
go run ./cmd/zhuimi feeds status --q=Nature --table
go run ./cmd/zhuimi feeds status --error-q=parse --table
go run ./cmd/zhuimi feeds status --checked-missing=true --table
go run ./cmd/zhuimi feeds status --sort=checked --reverse --table
go run ./cmd/zhuimi feeds status --checked-after=2026-03-10 --sort=checked-missing --reverse --table
go run ./cmd/zhuimi feeds status --checked-before=2026-03-20 --sort=checked-missing --reverse --table
go run ./cmd/zhuimi feeds status --error-missing=false --table
go run ./cmd/zhuimi feeds status --sort=checked-missing --reverse --table
go run ./cmd/zhuimi feeds status --sort=error-present --reverse --table
go run ./cmd/zhuimi feeds status --sort=success-missing-rate --reverse --table
go run ./cmd/zhuimi feeds status --sort=success --reverse --table
go run ./cmd/zhuimi feeds status --sort=error-present-rate --reverse --table
go run ./cmd/zhuimi feeds status --success-missing=true --sort=total --reverse --table
go run ./cmd/zhuimi feeds status --success-after=2026-03-10 --sort=success-missing-rate --reverse --table
go run ./cmd/zhuimi feeds status --success-before=2026-03-20 --sort=success-missing-rate --reverse --table
go run ./cmd/zhuimi feeds status --status=error --sort=total --reverse --table
go run ./cmd/zhuimi feeds status --sort=error-rate --reverse --table
go run ./cmd/zhuimi feeds status --sort=status-rate:ok --reverse --table
go run ./cmd/zhuimi feeds status --sort=title-only-rate:true --reverse --table
go run ./cmd/zhuimi feeds status --sort=status:ok --reverse --table
go run ./cmd/zhuimi feeds status --sort=title-only:true --reverse --table
go run ./cmd/zhuimi feeds status --sort=error --reverse --table
go run ./cmd/zhuimi feeds status --status=inactive --table
go run ./cmd/zhuimi feeds status --sort=total --reverse --limit=10 --table
go run ./cmd/zhuimi feeds status --sort=total --reverse --table
go run ./cmd/zhuimi feeds status --enabled=true --allow-title-only=inherit --table
go run ./cmd/zhuimi feeds status --source=pubmed.opml --table
go run ./cmd/zhuimi feeds status --table
```

说明：

- `feeds import`：从 OPML/配置源导入订阅，并保留已有状态
- `feeds list --enabled=true --allow-title-only=inherit`：查看当前启用且未单独设置标题策略的订阅
- `feeds list --domain=example.com --table`：查看某个域名及其子域名下的订阅；JSON 输出也会直接附带派生的 `domain`、`enabled`、`checked_missing`、`success_missing`、`has_error`、`allow_title_mode`、`source_name` 和 `status_label` 字段；排序参数也支持这些字段名对应的 `snake_case` 别名
- `feeds list --sort=enabled --reverse --table`：按是否启用排序，并把当前启用的订阅排在前面
- `feeds list --sort=title-only --table`：按标题策略排序，便于把显式放行、显式禁用和继承默认的订阅分组查看
- `feeds list --sort=has-error --reverse --table`：按是否带错误信息排序，并把当前有错误的订阅排在前面
- `feeds list --sort=checked-missing --reverse --table`：按是否从未检查过排序，并把还没检查过的订阅排在前面
- `feeds list --sort=success-missing --reverse --table`：按是否从未成功抓取过排序，并把还没成功过的订阅排在前面
- `feeds list --sort=domain --table`：按订阅 URL 的域名排序，表格里也会直接显示 `Domain` 列，便于集中查看同站点或同域族群的源
- `feeds list --source=pubmed --table`：按导入源文件名筛选订阅，支持大小写不敏感的包含匹配；没有来源文件名的订阅会在表格和 JSON 里统一显示为 `unknown`
- `feeds list --sort=last-error --table`：按最近错误信息排序，并在表格中直接查看报错内容
- `feeds list --error-q=timeout --table`：按错误文本关键字筛选订阅，便于快速聚合同类问题
- `feeds list --q=Nature --table`：按标题或 URL 关键字快速筛选订阅
- `feeds list --sort=source --table`：按导入源文件名排序，并在表格中直接显示来源列；也支持别名 `--sort=source-name`
- `feeds list --sort=success --reverse --table`：按最近成功时间倒序查看订阅，便于排查很久没成功过的源；也支持别名 `--sort=latest-success`
- `feeds list --checked-missing=true --table`：筛出从未检查过的订阅，便于确认新导入或异常遗漏的源
- `feeds list --checked-after=2026-03-10 --table`：筛出在 2026-03-10 当天及之后检查过的订阅，便于聚焦最近活跃的源；排序时也支持别名 `--sort=latest-checked`
- `feeds list --checked-before=2026-03-20 --table`：筛出在 2026-03-20 之前就检查过、但之后未再检查的订阅
- `feeds list --error-missing=false --sort=last-error --table`：筛出当前带错误信息的订阅，并直接按报错内容查看
- `feeds list --success-missing=true --sort=success --table`：筛出从未成功抓取过的订阅，并按成功时间查看排查优先级
- `feeds list --success-after=2026-03-10 --sort=success --table`：筛出在 2026-03-10 当天及之后成功抓取过的订阅，便于查看最近恢复正常的源
- `feeds list --success-before=2026-03-20 --sort=success --table`：筛出在 2026-03-20 之前成功过、但之后未再成功的订阅
- `feeds list --sort=title --table`：按标题排序查看订阅
- `feeds list --sort=status --reverse --table`：按状态倒序查看订阅；也支持别名 `--sort=status-label`
- `feeds list --sort=title --limit=20 --table`：排序后只显示前 20 条订阅
- `feeds list --allow-title-only=true --table`：以终端表格查看已单独放行标题型条目的订阅
- `feeds set --url=... --title="..." --allow-title-only=true|false|inherit --status=active|inactive`：直接调整单个 feed 的显示标题、标题型条目策略或启停状态
- `feeds set --domain=example.com --allow-title-only=false --yes`：按域名批量调整 feed 的标题型条目策略
- `feeds set --source=pubmed.opml --status=inactive --yes`：按导入源文件名批量停用某个 OPML 下的订阅
- `feeds set --source=unknown --status=inactive --yes`：批量处理没有来源文件名的订阅
- `feeds set --domain=example.com --status=inactive --yes`：按域名批量停用订阅
- `feeds set --all --status=active --yes`：批量启用全部订阅
- `feeds set --domain=example.com --status=inactive --dry-run`：预览将被修改的订阅，但不写入数据库和 `feeds.json`

批量修改（`--domain` / `--source` / `--all`）默认需要显式传 `--yes` 才会真正写入；不确定时先用 `--dry-run` 看预览。
- `feeds list --status=inactive`：查看已停用订阅
- `feeds list --status=unknown --table`：查看还没有明确抓取状态的订阅
- `feeds list --source=unknown --table`：查看没有来源文件名的订阅
- `feeds status`：查看订阅总量、启用数、状态分布、标题策略分布，以及按导入源分组的统计；JSON 输出会直接包含来源总数、各来源 `share` 占比、按 `--sort/--reverse/--limit` 排序后的 `sources` 列表、最近检查/成功时间、失败率和错误信息/未检查/未成功占比，表格模式也会显示这些数量、占比和最近时间；排序参数也支持一组与 JSON 字段同名的 `snake_case` 别名
- `feeds status --q=Nature --table`：按标题或 URL 关键字筛选后再查看汇总统计
- `feeds status --error-q=parse --table`：按错误文本关键字筛选后，再查看问题来源分布
- `feeds status --checked-missing=true --table`：只统计从未检查过的订阅，并继续按来源看分布
- `feeds status --sort=checked --reverse --table`：按各来源最近检查时间倒序查看，便于快速发现最近有活动的来源；也支持别名 `--sort=latest-checked`
- `feeds status --checked-after=2026-03-10 --sort=checked-missing --reverse --table`：只统计在 2026-03-10 当天及之后检查过的订阅来源分布
- `feeds status --checked-before=2026-03-20 --sort=checked-missing --reverse --table`：只统计 2026-03-20 之前检查过、但之后未再检查的订阅来源分布
- `feeds status --error-missing=false --table`：只统计当前带错误信息的订阅，并查看问题来源分布
- `feeds status --sort=checked-missing --reverse --table`：按各来源“从未检查过”的订阅数量倒序排查
- `feeds status --sort=error-present --reverse --table`：按各来源当前带错误信息的订阅数量倒序排查；也支持别名 `--sort=error-info`
- `feeds status --sort=success-missing-rate --reverse --table`：按各来源“从未成功过”的订阅占比倒序排查
- `feeds status --sort=success --reverse --table`：按各来源最近成功时间倒序查看，便于定位最近恢复正常的来源；也支持别名 `--sort=latest-success`
- `feeds status --sort=error-present-rate --reverse --table`：按各来源当前带错误信息的订阅占比倒序排查；也支持别名 `--sort=error-info-rate`
- `feeds status --success-missing=true --sort=total --reverse --table`：只统计从未成功抓取过的订阅，快速看问题主要集中在哪些来源
- `feeds status --success-after=2026-03-10 --sort=success-missing-rate --reverse --table`：只统计在 2026-03-10 当天及之后成功过的订阅来源分布
- `feeds status --success-before=2026-03-20 --sort=success-missing-rate --reverse --table`：只统计 2026-03-20 之前成功过、但之后未再成功的订阅来源分布
- `feeds status --status=error --sort=total --reverse --table`：查看抓取失败订阅主要集中在哪些导入源
- `feeds status --sort=error-rate --reverse --table`：按失败比例给各导入源排序，便于发现相对更不稳定的来源
- `feeds status --sort=status-rate:ok --reverse --table`：按某个具体抓取状态的占比给各导入源排序
- `feeds status --sort=title-only-rate:true --reverse --table`：按显式放行标题型条目的占比给各导入源排序
- `feeds status --sort=status:ok --reverse --table`：按某个具体抓取状态在各导入源中的数量排序
- `feeds status --sort=title-only:true --reverse --table`：按显式放行标题型条目最多的导入源排序
- `feeds status --sort=error --reverse --table`：不先筛选，直接按失败订阅数给各导入源排序
- `feeds status --status=inactive --table`：查看已停用订阅的汇总和来源分布
- `feeds status --sort=total --reverse --limit=10 --table`：只看订阅总数最多的前 10 个导入源；也支持别名 `--sort=share`
- `feeds status --sort=total --reverse --table`：按订阅总数倒序查看各导入源统计
- `feeds status --enabled=true --allow-title-only=inherit --table`：只统计当前启用且未单独设置标题策略的订阅
- `feeds status --source=pubmed.opml --table`：查看某个导入源下的订阅汇总统计
- `feeds status --table`：以表格方式查看总体和各导入源的订阅统计
- `feeds status --sort=checked --reverse --limit=5`：在 JSON 输出里保留全部 `by_source` 明细的同时，再额外返回最近检查时间最新的前 5 个来源列表

### 日常运行

```bash
go run ./cmd/zhuimi run daily
go run ./cmd/zhuimi run daily --skip-scoring
go run ./cmd/zhuimi run daily --pdf
```

说明：

- `run daily`：执行完整三段式流水线，按 `RSS/Atom -> HTML 内容获取 -> AI processors -> 报告` 运行
- `run daily --skip-scoring`：只抓取、入库并输出原始日报，不调用 AI processors
- `run daily --pdf`：在生成 `index.typ` 后继续编译 `index.pdf`
- `run fetch-content --limit=50`：只补抓正文内容，适合对历史文章做回填
- `run fetch-content --content-status=missing|pending|failed|rss_fallback|skipped`：只挑选指定内容状态的文章补抓正文；筛选会先于 `--limit` 生效
- `run fetch-content --content-status=fetched --force`：对已成功抓取过正文的文章强制重抓，适合站点模板变化后的回刷
- `run fetch-content --force`：即使已有 `rss_fallback` 或历史内容，也强制重新抓取
- `run ai --processor=generic_digest --limit=50`：运行通用摘要/标签处理器
- `run ai --processor=aml_score --limit=50`：单独运行 AML 评分处理器
- `run ai --processor=generic_digest --processor-status=missing|failed|pending|skipped`：只补跑某个 processor 当前缺失或异常的文章；筛选同样会先于 `--limit` 生效
- `run ai --processor=generic_digest --processor-status=processed --force`：对已经处理成功的结果强制重跑，适合 prompt/model 调整后的回刷
- `run ai --force`：忽略相同输入哈希，强制重跑指定 processor

### 历史回填与重试

```bash
go run ./cmd/zhuimi run backfill --days=7
go run ./cmd/zhuimi run backfill --days=7 --skip-scoring --pdf
go run ./cmd/zhuimi retry failed-scores --limit=20
```

说明：

- `run backfill --days=7`：回填最近 7 天历史数据，并按当前 processors 配置补抓正文与执行 AI 处理
- `run backfill --days=7 --skip-scoring --pdf`：仅抓取、正文获取并输出历史 PDF 日报
- `retry failed-scores`：只重试 AML 评分失败的文章；当前会优先按 `aml_score` 的最新 AI 结果状态选择失败项，并对 legacy `score_status=failed` 保留兼容兜底

### 报告与文章查询

```bash
go run ./cmd/zhuimi report rebuild --date=2026-03-09
go run ./cmd/zhuimi report rebuild --date=2026-03-09 --pdf
go run ./cmd/zhuimi report prune --keep-days=30
go run ./cmd/zhuimi articles list --limit=10
go run ./cmd/zhuimi articles list --min-total=75 --min-aml=20 --level=推荐
go run ./cmd/zhuimi articles list --content-status=rss_fallback
go run ./cmd/zhuimi articles list --processor=generic_digest --processor-status=missing
go run ./cmd/zhuimi articles top --date=2026-03-09 --table
```

说明：

- `report rebuild`：基于数据库内已保存结果重建指定日期报告
- `report prune`：清理旧报告文件
- `articles list`：JSON 输出现在会附带 `content` 与 `processors` 字段，便于直接查看正文抓取状态和最新 AI 结果
- `articles list`：按条件筛选文章及评分摘要
- `articles list --content-status=...`：按正文抓取状态筛选文章，例如快速查看 `rss_fallback` 或 `failed` 的文章集合
- `articles list --processor=... --processor-status=...`：按指定 processor 的最新处理状态筛选文章，例如查看 `generic_digest` 缺失但 `aml_score` 已存在的文章
- `articles top --table`：以终端表格查看某日高分文章

### 数据库维护

```bash
go run ./cmd/zhuimi db stats
go run ./cmd/zhuimi db stats --json
go run ./cmd/zhuimi db vacuum
go run ./cmd/zhuimi migrate legacy
```

说明：

- `db stats`：查看数据库概况，默认输出紧凑单行摘要
- `db stats --json`：输出更完整的统计信息，包括 `content_by_status` 和 `processor_latest_status`，适合排障或脚本消费
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

## 命令输出说明

`run daily` / `run backfill` 的 JSON 输出除了原有的 `feeds`、`fetched`、`new`、`processed`、`report_date` / `reports` 之外，还会额外包含：

- `content_tasks`：本轮进入正文抓取阶段的文章数
- `content_changed`：本轮正文抓取后实际写入变更的文章数
- `content_reasons`：正文抓取原因分布，例如 `new_article`、`content_changed`、`content_retry`
- `processor_tasks`：本轮进入 AI 处理阶段的任务数
- `processor_reasons`：AI 处理原因分布，格式为 `<processor>:<reason>`，例如 `generic_digest:processor_missing`、`aml_score:processor_failed`
- `report_written`：本轮是否实际写出了日报文件；即使当天没有任何文章进入报告，`run daily` 也会稳定输出 JSON
- `report_articles`：本轮实际进入日报的文章数

`run fetch-content` / `run ai` 的 JSON 输出也会包含 `reasons` 字段，用来解释本次任务是因为什么被选中；适合批量回填或重跑时快速确认是否命中了预期集合。

正文状态里：

- `rss_fallback`：尝试抓取 HTML 但抽取失败，回退到 RSS 摘要；后续主流程会继续纳入重试候选
- `skipped`：当前明确不做正文抓取，例如 `content.enabled=false` 或文章缺少可抓取链接；默认不会被主流程反复重试。若文章是因为此前关闭了正文抓取而被标记为 `skipped`，在重新启用 `content.enabled=true` 后，主流程会自动把这批文章纳回补抓候选

## 配置说明

`scripts/config/zhuimi_config.yaml` 当前主要分为三层：

- `rss`：订阅抓取窗口、feed 数量上限、标题型条目过滤策略
- `content`：正文抓取开关、并发、超时、大小上限、User-Agent、是否落库 HTML
- `processors`：启用哪些 AI processor，以及每个 processor 的模型覆盖

处理器相关配置里：

- `processors.enabled`：当前唯一的运行时启用列表
- `processors.default`：兼容保留字段，当前不会单独影响调度行为；运行时以 `processors.enabled` 为准
- `processors.generic_digest_model` / `processors.aml_score_model`：分别覆盖对应 processor 的模型，不写时回退到 `ai.model`

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
6. 提交 `content/ZhuiMi/`、`scripts/feeds.json` 与 `data/zhuimi/store.db` 的变更
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
