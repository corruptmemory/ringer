# research-with-proof

## What it is

Research-with-proof pairs source-backed research with at least one executable proof task. One worker writes a cited report; another builds a runnable artifact that proves the core claim or shows the limit.

Use it when plausible prose is not enough. The proof does not replace judgment, but it forces the main claim through a command that can fail.

## When to use

- Checking current API behavior, standards, pricing rules, product limits, or repo behavior.
- Preparing a technical guide where the main claim should be demonstrated.
- Turning uncertain review findings into verified facts.
- Building small fixtures that prove or disprove a product assumption.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{TOPIC}}` | Short topic slug for the run name and research check. |
| `{{WORKDIR}}` | Absolute scratch directory where Ringer creates research and proof task directories. |
| `{{RESEARCH_KEY}}` | Stable task key for the research report. |
| `{{RESEARCH_QUESTION}}` | The exact question the research report must answer. |
| `{{DELIVERABLE — e.g. a technical guide, buying memo, implementation plan, or product decision}}` | The downstream artifact or decision this research supports. |
| `{{PRIMARY_SOURCES — official docs, standards, vendor pages, repos, or first-party changelogs}}` | Source hierarchy the researcher should prefer. |
| `{{CHECK_SCRIPT_PATH — absolute path to templates/research-with-proof/checks/research-with-proof.py}}` | Absolute path to this kit's validator after you copy or reference the kit. |
| `{{PROOF_KEY}}` | Stable task key for the executable proof task. |
| `{{PROOF_ARTIFACT_PATH — relative path to the runnable proof entrypoint, e.g. proof.py}}` | Relative path to the script or runnable entrypoint the proof worker creates. |
| `{{CLAIM_TO_PROVE}}` | The core claim the proof must demonstrate or fail against. |
| `{{PROOF_ARTIFACT — e.g. a Python script plus a fixture, a small test app, or a reproducible command sequence}}` | What kind of runnable artifact the proof worker should build. |
| `{{CONSTRAINTS — runtime, dependency, network, fixture, and credential limits}}` | Runtime and dependency limits, including any no-network or no-credential rules. |
| `{{PROOF_COMMAND — exact command that runs the proof end to end and prints PROOF OK}}` | Shell-safe command the validator executes from the proof task directory. |

## Checks

The research check validates the report structure, source URLs, access dates, and evidence fields. The proof check validates `proof.md`, verifies the runnable artifact exists, executes the filled proof command, and requires the success marker.

This is harder to game than a cited report alone because the central claim has to survive an executable check. The proof can still be scoped too narrowly, so the proof document must also state limits.

## Mix with

- `review-swarm`: send uncertain review claims here before treating them as findings.
- `fix-swarm`: feed the proof result into a fix task when the correct behavior is now known and executable.
- `bakeoff`: use research-with-proof to source model pricing, limits, or current API behavior before building a bakeoff rubric.

## Gotchas

- A source URL without an access date is not enough for current research.
- Do not let the proof use private credentials or live accounts unless that is explicitly allowed.
- The proof should print the failing step on error. Silent proof failures waste the retry.
- A proof demonstrates one claim under stated constraints; it does not prove every adjacent recommendation.
