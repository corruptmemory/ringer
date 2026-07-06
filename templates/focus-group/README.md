# focus-group

## What it is

A focus group swarm runs separate persona workers against the real product or harness. Each persona has its own session directory, stays in character during the conversation, then writes both a first-person reaction and out-of-character evaluator notes.

Use it to learn how a product, prompt, onboarding flow, or generated deliverable lands with different users. The value comes from a fixed panel that can be re-run after changes.

## When to use

- Testing whether an AI product asks the right questions and uses the answers.
- Comparing prompt or UX iterations against the same personas.
- Capturing qualitative reactions plus structured criteria grades.
- Finding where a flow feels generic, confusing, too pushy, or not worth continuing.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{PRODUCT}}` | Product, prompt, or workflow name. |
| `{{WORKDIR}}` | Absolute scratch directory where Ringer creates persona session directories. |
| `{{PERSONA_KEY}}` | Stable key for one persona, such as `skeptic-founder` or `busy-parent`. |
| `{{ONE_LINE_DESCRIPTION}}` | One sentence describing what the product is supposed to do. |
| `{{PERSONA — name, age, job, tech comfort, awareness level, pains they reveal only when asked, typing style, skepticism, and buying context}}` | The persona card. Include enough behavioral detail that the worker can stay consistent. |
| `{{PRODUCT_SURFACE — URL, CLI, local app route, or harness target being evaluated}}` | The exact product surface the persona should use. |
| `{{HARNESS_COMMAND — exact command that sends one message and prints the reply, using the session dir above}}` | Command for one conversational turn against the real product. |
| `{{END_CONDITION — exact stop signal, turn count, or harness message}}` | The condition that ends the session. |
| `{{OUTPUT_CONTRACT — numbered, checkable criteria the product output must satisfy}}` | The grading rubric every persona applies after the session. |
| `{{CHECK_SCRIPT_PATH — absolute path to templates/focus-group/checks/focus-group.py}}` | Absolute path to this kit's validator after you copy or reference the kit. |
| `{{SESSION_VALIDATOR — exact command that validates the session transcript and product run}}` | Command that proves the session happened against the real product and prints why it failed. |

## Checks

The manifest invokes `checks/focus-group.py`. The validator checks both output files, required headings, PASS/FAIL/MIXED grading, length limits, and then runs the filled session validator command.

This is harder to game than asking for notes because the worker must leave structured evidence and the harness must prove the product conversation actually happened. The session validator should inspect transcripts, run metadata, or product logs, not just check that files exist.

## Mix with

- `bakeoff`: use focus-group to define the fixed personas and criteria, then bakeoff candidate models against the same scenario rows.
- `research-with-proof`: use it when persona reactions depend on facts that need source-backed checking.
- Re-run this same kit after each product or prompt iteration with the same persona panel. A fixed panel tells you whether the change addressed the original complaint.

## Gotchas

- Do not put multiple personas in one task. They bleed into each other.
- Keep the panel stable across iterations; changing personas makes before/after results hard to compare.
- Persona workers should not help the product. Realistic resistance is useful.
- The session validator is part of the product surface. If it only checks for a file, the run can be faked.
