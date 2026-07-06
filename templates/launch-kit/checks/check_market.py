#!/usr/bin/env python3
"""Validate a market-read report against an allowlisted source file."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def urls_from(text: str) -> set[str]:
    return {url.rstrip(".,);]") for url in re.findall(r"https?://\S+", text)}


def allowed_match(url: str, allowed: set[str]) -> bool:
    candidate = url.rstrip("/")
    for source in allowed:
        source = source.rstrip("/")
        if candidate == source:
            return True
        if (candidate.startswith(source) or source.startswith(candidate)) and min(len(candidate), len(source)) >= 34:
            return True
    return False


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--report", default="report.md")
    parser.add_argument("--sources", required=True)
    parser.add_argument("--required-terms", default="")
    parser.add_argument("--min-words", type=int, default=350)
    parser.add_argument("--min-citations", type=int, default=4)
    args = parser.parse_args()

    report = pathlib.Path(args.report)
    sources = pathlib.Path(args.sources)
    fails: list[str] = []

    if not report.exists():
        print(f"FAIL: {report} not found in task dir")
        return 1
    if not sources.exists():
        print(f"FAIL: source allowlist file not found: {sources}")
        return 1

    text = report.read_text(encoding="utf-8", errors="replace")
    allowed = urls_from(sources.read_text(encoding="utf-8", errors="replace"))
    words = len(text.split())
    if words < args.min_words:
        fails.append(f"too short: {words} words (need >= {args.min_words})")

    cited = urls_from(text)
    good = {url for url in cited if allowed_match(url, allowed)}
    bad = sorted(cited - good)
    if len(good) < args.min_citations:
        fails.append(f"only {len(good)} allowlisted citations (need >= {args.min_citations})")
    if bad:
        fails.append(f"cites URLs not in the allowlist: {bad[:5]}")

    lowered = text.lower()
    for raw_term in [term.strip() for term in args.required_terms.split(",") if term.strip()]:
        if raw_term.lower() not in lowered:
            fails.append(f"missing required term or topic: {raw_term}")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {words} words, {len(good)} allowlisted citations, required topics present")
    return 0


if __name__ == "__main__":
    sys.exit(main())
