# Changelog

## Unreleased

### Added

- 新增通用正文获取子系统，支持基于文章链接抓取 HTML、抽取正文并在失败时回退到 RSS 摘要。
- 新增可插拔 AI processor 机制，内置 `generic_digest` 与 `aml_score` 两个处理器。
- 新增 `run fetch-content` 与 `run ai` 命令，支持按内容状态或 processor 状态批量回填。
- 新增 `article_contents` / `ai_results` 持久化能力，并保留 `scores` 兼容写入链路。
- 新增更完整的运维输出：
  - `run daily` / `run backfill` 增加 `content_tasks`、`content_reasons`、`processor_tasks`、`processor_reasons`
  - `run fetch-content` / `run ai` 增加 `reasons`
  - `db stats --json` 增加 `content_by_status`、`processor_latest_status`

### Changed

- 主流程从“RSS 抓取后直接评分”升级为三段式流水线：
  - `ingest`
  - `enrich`
  - `process`
- `run daily` / `run backfill` 的调度已改为基于正文状态和每个 processor 的最新结果、输入哈希做增量执行，不再盲目重复抓取或重复跑 AI。
- `retry failed-scores` 现在优先依据 `aml_score` 的最新 AI 结果状态重试，同时保留 legacy `score_status=failed` 的兼容兜底。
- `run daily` 即使当天没有任何文章进入日报，也会稳定输出 JSON 统计，并显式给出 `report_written=false`。
- RSS `description` 为空的条目不再天然被主流程丢弃；只要后续 HTML 获取能拿到正文，仍可继续进入 AI 处理。
- `skipped` 内容状态语义收敛为“当前明确不做抓取”；如果是因为此前关闭了正文抓取而跳过，在重新启用后会自动进入补抓队列。

### Compatibility

- 现有 AML 日报链路仍然保留，`aml_score` 继续双写 `ai_results` 与 legacy `scores`。
- `processors.default` 保留为兼容字段，但当前运行时只以 `processors.enabled` 为准。
