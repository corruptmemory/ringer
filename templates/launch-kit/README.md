# Launch Kit

## What it is

A three-round go-to-market swarm for turning real product facts into a checked launch page. Round 1 researches, names, drafts copy, creates the first brand board, generates the hero, and builds the first site; round 2 runs isolated buyer personas and locks the brand kit; round 3 applies the panel verdicts to the final self-contained site.

The orchestrator's between-round job is load-bearing: read round-1 artifacts, choose the name/pitch/brand direction, then write those choices into `manifest-round2.json` and `manifest-round3.json` before running the next swarm.

## When to use

Use this when a product, service, creator offer, or small-business idea needs a fast but evidence-grounded launch package. It works best when the orchestrator has fixed product facts, real photos or source assets, and enough source URLs to keep the market read from inventing facts.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{BRAND_VIBE}}` | The tone and visual constraints the brand workers must respect. |
| `{{DESKTOP_WIDTH}}` | Desktop viewport width used by the builder and reviewer, such as `1440`. |
| `{{ENGINE_BUILD}}` | Engine name for build-heavy or final assembly tasks. |
| `{{ENGINE_CHEAP}}` | Engine name for mechanical copy, persona, or batch tasks. |
| `{{ENGINE_RESEARCH}}` | Engine name for the market-read task. |
| `{{FINAL_PRICE_TESTS}}` | Draft price or bundle decisions from the orchestrator. |
| `{{FINAL_SITE_MIN_BYTES}}` | Minimum byte size expected for the final self-contained site. |
| `{{HERO_IMAGE_MIN_BYTES}}` | Minimum hero PNG byte size. |
| `{{HERO_IMAGE_MIN_HEIGHT}}` | Minimum hero PNG pixel height. |
| `{{HERO_IMAGE_MIN_WIDTH}}` | Minimum hero PNG pixel width. |
| `{{HERO_PROMPT_FILE}}` | Path to the filled hero-image prompt file. |
| `{{HERO_REFERENCE_IMAGE}}` | Path to the real reference photo or image input. |
| `{{IMAGE_GENERATOR_COMMAND}}` | Exact image-generation CLI command before prompt/ref/name/out flags. |
| `{{KIT_DIR}}` | Absolute path to `templates/launch-kit` after copying or installing this kit. |
| `{{LOCKED_BRAND_DECISIONS}}` | Round-2 brand decisions written by the orchestrator from round-1 winners. |
| `{{LOCKED_BRAND_DEVICE}}` | Signature visual device, type note, or motif the final page must apply. |
| `{{LOCKED_BRAND_NAME}}` | Final chosen brand or product name. |
| `{{LOCKED_PALETTE_HEXES}}` | Comma-separated locked hex colors, such as `#111111,#eeeeee,#00aaff,#ffcc00`. |
| `{{LOCKED_TAGLINE}}` | Final tagline text. |
| `{{MARKET_REQUIRED_TERMS}}` | Comma-separated topics the market report must address. |
| `{{MOBILE_WIDTH}}` | Mobile viewport width used by the builder and reviewer, such as `390`. |
| `{{PANEL_OBJECTIONS}}` | Persona objections the final page must answer honestly. |
| `{{PANEL_TALLY_AND_VERDICTS}}` | Orchestrator summary of persona verdicts, winning names, pitches, pay points, and objections. |
| `{{PERSONA_KEY}}` | Unique task key for one persona, such as `persona-maya-desk`. |
| `{{PERSONA_PROFILE}}` | Persona identity, context, shopping behavior, skepticism, and voice. |
| `{{PRODUCT_FACTS}}` | The only product facts workers may state without marking assumptions. |
| `{{PRODUCT_NAME}}` | Working product or offer name. |
| `{{PRODUCT_PHOTO_PATHS_AND_DESCRIPTIONS}}` | Real image paths plus what each shows. |
| `{{ROUND1_HERO_PATH}}` | Path to round 1 `hero.png`. |
| `{{ROUND1_SITE_PATH}}` | Path to round 1 `site.html`. |
| `{{ROUND2_BRAND_KIT_PATH}}` | Path to round 2 `brand-kit.md`. |
| `{{ROUND2_PERSONA_SUMMARY_PATH}}` | Path to the orchestrator-written persona summary for round 3. |
| `{{RUN_SLUG}}` | Stable job slug reused by all three rounds. |
| `{{SECONDARY_PITCH_VERBATIM}}` | Secondary pitch copied exactly from the panel result. |
| `{{SHORTLISTED_NAMES_AND_TAGLINES}}` | Name and tagline finalists selected after round 1. |
| `{{SHORTLISTED_PITCHES}}` | Pitch A/B/C finalists selected after round 1. |
| `{{SITE_MIN_BYTES}}` | Minimum byte size expected for the first self-contained site. |
| `{{SITE_REQUIRED_TERMS}}` | Comma-separated terms or sections the first site must include. |
| `{{SOURCE_ALLOWLIST_PATH}}` | Path to the source URL allowlist for market research. |
| `{{WINNING_HERO_PITCH_VERBATIM}}` | Persona-winning hero pitch copied exactly. |
| `{{WORKDIR}}` | Scratch run directory outside the repo or product source tree. |
| `{{WORKING_BRAND_PLACEHOLDER}}` | Temporary brand text used in round 1 and removed in round 3. |

## Checks

The checks validate substance, not worker confidence. The market validator reads an allowlist and rejects hallucinated citation URLs while tolerating same-page prefix differences, the candidate and persona validators parse machine-readable blocks, the site validators reject external requests and missing honesty markers, and the image validator checks real PNG bytes and dimensions.

This is hard to game because each worker has to produce a file that parses and carries the required content. A vague report, missing verdict block, external CDN link, or invented source URL fails with a named reason that becomes useful retry context.

## Mix with

Use `asset-swarm` after round 3 when the launch needs b-roll, stills, diagrams, or screenshots from the final site. Use `adversarial-review` before publishing if the page or claims need independent review. Use `repo-feature` when the final site must be installed into a real application repo instead of shipped as a standalone HTML artifact.

## Gotchas

Round transitions are orchestration work, not worker work. Read the artifacts, pick winners, and write those choices into the next manifest specs before launching the next round.

Citation URLs must be copied verbatim from the source allowlist. The market check tolerates same-page prefix differences so harmless URL truncation does not fail a good report, but it still rejects new URLs.

Heading regexes need to tolerate numbering. Real workers write `## 1. Direction` or `## DIRECTION 1`; checks should look for the substance, not one exact heading shape.

`expect_files` is asserted before the check runs. Put worker-produced deliverables there, but keep check-produced files such as rendered videos or screenshots out of `expect_files`; let fallback harvest collect those in render-as-check kits.

Do not let personas share a context. One persona per task is the thing that keeps the panel from blending into one averaged opinion.
