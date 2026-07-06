Blueprint — adapt with care

# Competitive Teardown

## What it is

This kit is a blueprint for a competitive research swarm: one scout per competitor or product, each limited to an orchestrator-verified source allowlist. It is not a proven recorded run yet, so keep the guardrails and adapt the target fields with care.

The scouts fetch allowlisted pages with curl, extract real numbers, and write "could not fetch" when a source is unavailable. Synthesis is a second phase: the orchestrator reads the scout reports, then fills the synthesis task skeleton in `prompts/synthesis-task.json.txt`.

## When to use

Use this when you need a fair comparison across competitors and the source set can be constrained before the run. It fits pricing teardowns, feature matrix research, landing page claim audits, and product-positioning scans where uncited browsing would make the output hard to trust.

Do not use it when discovery is the main job. Run a broader research pass first, build the allowlist, then run this kit.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{PROJECT}}` | Short project or market name used in the run name. |
| `{{WORKDIR}}` | Scratch run directory for Ringer task folders. |
| `{{TARGET_KEY}}` | Stable slug for one competitor or product, for example `acme-pro`. |
| `{{TARGET_NAME}}` | Human-readable competitor or product name. |
| `{{TARGET_DESCRIPTION}}` | One sentence describing this target and why it is in the comparison. |
| `{{ALLOWLIST_PATH}}` | Path to the orchestrator-verified allowlist file, usually a filled copy of `sources.md.example`. |
| `{{FETCH_TIMEOUT_SECONDS}}` | Curl timeout for one source fetch. |
| `{{REQUIRED_ANGLES}}` | Pipe-separated angles every scout must cover, for example `pricing|onboarding|limits|proof points`. |
| `{{MAX_REPORT_WORDS}}` | Length cap the worker should obey. |
| `{{TEARDOWN_CHECK}}` | Path to `templates/competitive-teardown/checks/teardown_check.py` or a copied local version. |
| `{{MIN_REPORT_WORDS}}` | Minimum report word count the check enforces. |
| `{{MIN_CITATIONS}}` | Minimum number of cited allowlisted URLs required to pass. |
| `{{MIN_EXTRACTED_NUMBERS}}` | Minimum count of numeric facts required to pass. |
| `{{SYNTHESIS_KEY}}` | Task key for the second-phase synthesis task. |
| `{{SYNTHESIS_SCOPE — comparison question the final report must answer}}` | The exact decision or comparison the synthesis should resolve. |
| `{{SCOUT_REPORT_FILES}}` | Pipe-separated paths to the scout `report.md` files or harvested artifacts. |
| `{{SYNTHESIS_CHECK}}` | Path to `templates/competitive-teardown/checks/synthesis_check.py` or a copied local version. |
| `{{MIN_COMPETITORS}}` | Minimum number of scout reports the synthesis must compare. |

## Checks

`checks/teardown_check.py` validates that every URL in `report.md` matches the allowlist with prefix-tolerant matching. That catches non-allowlisted browsing while allowing legitimate slug truncation.

The check also enforces required angles, minimum word count, minimum citations, and minimum numeric evidence. It cannot be gamed by adding a few empty headings because it reads the whole report, counts actual URLs and numbers, and prints named failures for missing angles, weak substance, or disallowed citations.

`checks/synthesis_check.py` is for the second phase. It verifies that synthesis cites only URLs already present in the scout reports and that the final report has comparison sections instead of a loose summary.

## Mix with

Mix with `research-with-proof.json` when one competitor claim needs an executable proof after the teardown. Mix with `bakeoff.json` when the competitors are models or configurations and each target must run through the same scenario.

Use `review-swarm.json` before this kit when you need scouts to inspect different source groups and produce the allowlist candidates. The orchestrator still owns the final allowlist decision.

## Gotchas

The allowlist is the boundary. If the worker finds a great source outside it, the correct output is "not in allowlist", not a new citation.

Run this in two phases. First run only the scout manifest. Then read the scout reports, fill the synthesis task with the actual report paths and confirmed comparison question, and run that as a second one-task manifest.

Slug-tolerant URL matching is a convenience, not permission to cite homepages. Keep allowlisted URLs specific enough that a truncated slug still points at the same source.
