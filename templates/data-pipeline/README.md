Blueprint — adapt with care

# Data Pipeline

## What it is

This kit is a blueprint for a three-stage data pipeline run: fetch, transform, validate. Each stage is a separate Ringer task with explicit file handoffs through `{{DATA_DIR}}`, which must live outside task directories so deliverables survive cleanup and are easy to inspect.

The pattern is not proven in a recorded run yet. Keep it small at first: one source, one schema, one validator contract.

## When to use

Use this when the stages need different instructions or different workers: fetching from a source, transforming into a stable shape, and validating the final data. It is a good fit for public datasets, scraped exports, vendor CSV cleanup, benchmark tables, and repeatable content inventories.

Do not use it for an exploratory notebook where the schema is still unknown. First inspect the source, then fill the schema placeholders.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{DATASET_KEY}}` | Stable slug for the dataset and output filenames. |
| `{{WORKDIR}}` | Scratch run directory for Ringer task folders. |
| `{{DATA_DIR}}` | Absolute path outside task dirs where raw, transformed, and validated outputs survive. |
| `{{SOURCE_URL}}` | Fetchable source URL. |
| `{{SOURCE_NAME}}` | Human-readable source name for logs. |
| `{{FETCH_TIMEOUT_SECONDS}}` | Curl timeout for source fetches. |
| `{{RAW_SCHEMA_FIELDS}}` | Comma-separated fields expected in raw JSONL records. |
| `{{RAW_REQUIRED_FIELDS}}` | Comma-separated raw fields that must be present and non-empty. |
| `{{FETCH_SPOT_INVARIANTS}}` | Semicolon-separated raw-data invariants, for example `count>=10;unique:id`. |
| `{{PIPELINE_CHECK}}` | Path to `templates/data-pipeline/checks/pipeline_check.py` or a copied local version. |
| `{{MIN_RAW_ROWS}}` | Minimum raw row count required for the fetch stage to pass. |
| `{{STAGE_WAIT_SECONDS}}` | How long downstream workers wait for an upstream handoff before reporting it missing. |
| `{{TRANSFORM_SCHEMA_FIELDS}}` | Comma-separated target output fields in the transformed CSV. |
| `{{TRANSFORM_REQUIRED_FIELDS}}` | Comma-separated transformed fields that must be non-empty. |
| `{{TRANSFORM_SPOT_INVARIANTS}}` | Semicolon-separated transformed-data invariants, for example `count>=10;unique:canonical_id;score>=0`. |
| `{{MIN_TRANSFORM_ROWS}}` | Minimum transformed row count required to pass transform and validation. |

## Checks

`checks/pipeline_check.py` validates actual data files, not just their existence. It reads JSONL, JSON arrays, or CSV; checks row counts; verifies schema fields; scans required fields for empty values; evaluates spot invariants; and counts rejects.

The transform check requires a rejects file. That prevents the common failure where a worker "cleans" bad source rows silently and the final CSV looks neat but is no longer auditable.

The validate-stage check reads the validation report and then re-runs validation against the transformed data. A report cannot pass by saying "PASS" unless the data file itself still satisfies the contract.

## Mix with

Mix with `research-with-proof.json` when the pipeline output supports a claim and you need an executable proof over the produced dataset. Mix with `probe` when the fetch step depends on a brittle API and you want a one-task smoke test before running the full pipeline.

Use `fix-swarm.json` after this kit only when the pipeline revealed separate code fixes. Keep data production and code repair as different runs.

## Gotchas

`{{DATA_DIR}}` should be absolute and outside the Ringer task directories. If it points inside a task directory, handoffs can disappear or become hard to inspect after the run.

Do not let the transform stage silently fix bad source data. Exclusions, malformed rows, dedupes, and type coercion failures go to the rejects file with reasons, and the check counts that file.

Ringer does not have dependency fields. This blueprint uses parallel tasks whose downstream specs wait for upstream files. For the first real run, keep `{{STAGE_WAIT_SECONDS}}` generous and inspect retries; repeated waiting failures mean the pipeline should be split into separate stage runs.
