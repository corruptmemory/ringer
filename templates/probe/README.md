Blueprint — adapt with care

# Probe

## What it is

This kit is a blueprint for the one-task manifest pattern: a visible, verified probe for work that is too easy to lose in a chat thread. It covers model-calling probes, API smoke tests, and failed-worker post-mortem scouts.

The rule is simple: if it calls a model or takes three steps, it runs under Ringer where it is visible, verified, and logged. This is the anti-"tiny-edit death spiral" tool.

## When to use

Use it before scaling a model or API workflow, when checking whether a harness still works, or when diagnosing a failed worker from logs. It is also the right shape for a single model response transcript that needs to be kept as evidence.

Do not use it for a true one-line local command whose output is already the answer. Use it when the transcript matters.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{PROBE_KEY}}` | Stable slug for this probe. |
| `{{WORKDIR}}` | Scratch run directory for Ringer task folders. |
| `{{TRANSCRIPT_FILE}}` | Transcript file path inside the task directory, usually `transcript.md`. |
| `{{OUTPUT_FILE}}` | Raw output file path inside the task directory, usually `probe-output.txt` or `probe-output.json`. |
| `{{PROBE_MODE}}` | One of `model`, `api`, `postmortem`, or `generic`. |
| `{{PROBE_GOAL}}` | One sentence naming the behavior being tested or diagnosed. |
| `{{PROBE_COMMAND — exact command or short command sequence to run, with paths}}` | The exact command or compact sequence the worker must run. |
| `{{MUST_CONTAIN}}` | Text that must appear in the transcript or output to prove the probed behavior. Use `NONE` only when mode-specific validation is enough. |
| `{{PROBE_CHECK}}` | Path to `templates/probe/checks/probe_check.py` or a copied local version. |
| `{{MIN_RESPONSE_CHARS}}` | Minimum model response length for `model` mode. |
| `{{MODEL_NAME}}` | Model name or route used by the model probe prompt skeleton. |
| `{{MODEL_PROMPT}}` | Exact prompt sent in a model probe. |
| `{{EXPECTED_BEHAVIOR}}` | Behavior the probe should confirm. |
| `{{API_URL}}` | API endpoint used by the API smoke prompt skeleton. |
| `{{HTTP_METHOD}}` | HTTP method for the API smoke test. |
| `{{EXPECTED_STATUS}}` | Expected HTTP status code or status family. |
| `{{FAILED_RUN_PATH}}` | Path to a failed Ringer run, worker log, or task directory for post-mortem mode. |

## Checks

`checks/probe_check.py` validates the transcript and raw output together. In `model` mode it requires a real model-response section with enough response text; in `api` mode it requires status and observed behavior; in `postmortem` mode it requires failure evidence, likely cause, and next check.

The check never uses unconditional `exit 0`. It fails with named messages when the transcript is missing the behavior marker, when the model response is too thin, or when a post-mortem has no evidence.

## Mix with

Mix with `bakeoff.json` after a probe confirms the harness can pin and call one model. Mix with `focus-group.json` when the probe confirms the product conversation harness works.

Mix with `data-pipeline` before a full pipeline run when the fetch API is brittle. Mix with `review-swarm.json` when a failed-worker post-mortem reveals a larger surface that needs multiple scouts.

## Gotchas

A transcript is not useful if it only says "ran command." Copy the prompt, raw response, status code, relevant stderr, and the concrete behavior observed.

Keep the probe narrow. If the worker starts making fixes, stop and turn the confirmed finding into a fix-swarm or normal implementation task.

For model probes, the response itself is the artifact. Do not summarize it away before the check can inspect it.
