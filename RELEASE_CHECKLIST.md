# Release Checklist

## Scope

This checklist is for the generalized ZhuiMi pipeline release that introduces:

- general RSS/RDF/Atom ingestion
- HTML content fetching
- pluggable AI processors
- `generic_digest` as the default processor
- `aml_score` as a compatibility processor for the existing AML report flow

## Before Merge

- confirm `go test ./...` passes locally
- confirm `README.md`, `CHANGELOG.md`, and `PR_DESCRIPTION.md` reflect the current runtime behavior
- confirm `.github/workflows/zhuimi.yml` stages `scripts/feeds.json` instead of the old legacy feed file
- confirm `scripts/config/zhuimi_config.yaml` still defaults to:
  - `content.enabled=true`
  - `processors.enabled=generic_digest`

## Local Smoke Test

Run the following in order:

```bash
go run ./cmd/zhuimi feeds import
go run ./cmd/zhuimi run daily --skip-scoring
go run ./cmd/zhuimi run fetch-content --content-status=rss_fallback --limit=20
go run ./cmd/zhuimi run ai --processor=generic_digest --processor-status=missing --limit=20
go run ./cmd/zhuimi db stats --json
```

Validate:

- feeds are imported into `scripts/feeds.json`
- database is created at `data/zhuimi/store.db`
- `run daily --skip-scoring` emits JSON even if no report is written
- `run fetch-content` emits `reasons`
- `run ai` emits `reasons`
- `db stats --json` includes:
  - `content_by_status`
  - `processor_latest_status`

## Compatibility Smoke Test

If AML reporting remains enabled in your deployment, also run:

```bash
go run ./cmd/zhuimi run ai --processor=aml_score --processor-status=missing --limit=20
go run ./cmd/zhuimi run daily --pdf
```

Validate:

- `aml_score` results are written
- legacy report generation still succeeds
- `content/ZhuiMi/YYYY-MM-DD/index.typ` is generated
- when there are reportable scored articles, `content/ZhuiMi/YYYY-MM-DD/index.pdf` is generated

## GitHub Actions Readiness

Confirm repository secrets exist:

- `OPENAI_BASE_URL`
- `OPENAI_API_KEY`
- `OPENAI_MODEL`

Optional:

- `ZHUIMI_SCORE_RATE_LIMIT`

Confirm workflow behavior expectations:

- daily workflow may complete successfully even when no PDF is produced
- PDF artifact upload only happens when `report_written=true`
- release upload only happens when `report_written=true`

## Rollout Recommendation

Recommended first rollout:

1. merge with `processors.enabled=generic_digest`
2. let the scheduled workflow run for at least one day
3. inspect:
   - workflow logs
   - `db stats --json`
   - generated `scripts/feeds.json`
   - generated `content/ZhuiMi/`
4. only then enable `aml_score` in production config if AML reports are still required

## Post-Release Watch Items

Watch for:

- unexpected growth in `rss_fallback`
- large numbers of `skipped` content records caused by configuration mistakes
- processors stuck in `failed` or `pending`
- workflow runs that succeed but never write reports
- sources with empty RSS descriptions that now rely entirely on HTML extraction

## Rollback Notes

If the generalized processor path causes issues:

- keep the database
- reduce `processors.enabled` back to `generic_digest` or `aml_score` only
- temporarily use `run daily --skip-scoring` to verify ingest/content behavior independently
- use `run ai --processor=... --processor-status=missing|failed` to selectively recover state
