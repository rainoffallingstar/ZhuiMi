# PR Description

## Summary

This PR upgrades ZhuiMi from a legacy RSS + AML-scoring flow into a generalized content pipeline:

- `RSS/RDF/Atom -> HTML content fetch -> AI processors -> report`

The existing AML report path is preserved for compatibility, while the runtime is now capable of handling general RSS sources, content enrichment, and processor-driven AI workflows.

## Why

Before this refactor, the project already had useful RSS ingestion and scoring pieces, but the overall behavior still had several structural limitations:

- content processing was still effectively tied to feed title/abstract fields
- AI execution logic was too coupled to legacy AML scoring state
- repeated runs could do unnecessary work
- operational commands did not expose enough status or reasoning detail
- some general RSS sources with empty feed descriptions were blocked before HTML fetching

This PR closes those gaps and makes the pipeline safer to rerun, easier to backfill, and easier to operate.

## What Changed

### Pipeline

- keeps `run daily` as the main entry point
- formalizes the internal pipeline as:
  - ingest
  - enrich
  - process
- preserves the existing AML reporting flow as the compatibility report path

### Content Fetching

- adds persistent article content storage via `article_contents`
- fetches HTML content from article links
- falls back to RSS summaries when HTML extraction fails
- distinguishes retryable fallback states from explicit skip states
- allows RSS items with empty `description` to continue through the pipeline if HTML content is available

### AI Processing

- adds pluggable processor support
- includes two built-in processors:
  - `generic_digest`
  - `aml_score`
- persists structured AI results in `ai_results`
- keeps `aml_score` dual-writing legacy `scores` for compatibility
- schedules AI by processor state + input hash rather than legacy article-level scoring state

### Scheduling / Incrementality

- avoids refetching unchanged content
- avoids reprocessing unchanged AI inputs
- supports content and processor backfill by explicit status filters
- retries AML failures based on latest `aml_score` processor state, with legacy fallback compatibility
- re-enables skipped content fetches when content fetching is turned back on after being disabled

### Operability

- adds:
  - `run fetch-content`
  - `run ai`
- supports:
  - `--content-status=...`
  - `--processor-status=...`
  - `--force`
- improves runtime JSON output with:
  - stage task counts
  - reason breakdowns
  - report write status
- adds `db stats --json` with richer content / processor state summaries

### Documentation / Release Prep

- refreshes README to match current runtime behavior
- adds `CHANGELOG.md`
- adds release-oriented config guidance in `scripts/config/zhuimi_config.yaml`

## Compatibility

- current AML report generation remains intact
- `aml_score` still writes legacy `scores`
- `processors.default` remains accepted as a compatibility field, but runtime behavior is driven by `processors.enabled`

## Testing

Ran:

```bash
go test ./...
```

Coverage added in this refactor includes:

- unchanged content / processor skipping
- metadata-change refetch / reprocess
- missing processor backfill
- retrying fallback content
- processor status filtering with limits and force mode
- content status filtering with limits and force mode
- AML retry behavior using latest processor state
- disabled / re-enabled content fetching behavior
- daily output when no report is written
- RSS items without feed descriptions but with usable HTML content

## Suggested Review Focus

- scheduling correctness in `internal/pipeline/`
- content status semantics in `internal/content/` and `internal/store/`
- compatibility guarantees for AML scoring + reports
- CLI / README consistency for new operational workflows

## Follow-ups

Not included in this PR:

- PDF/fulltext downloading
- JS-rendered page support
- generalized cross-domain reporting beyond the existing AML report path
- broader ops commands beyond the current status-driven backfill / retry behavior
