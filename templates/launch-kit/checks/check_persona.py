#!/usr/bin/env python3
"""Validate an in-character persona reaction with a parseable verdict block."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def field(text: str, name: str, pattern: str) -> str | None:
    match = re.search(rf"(?im)^\W*{re.escape(name)}\s*[:\-\u2014]\s*({pattern})\s*$", text)
    return match.group(1).strip() if match else None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="report.md")
    parser.add_argument("--min-words", type=int, default=200)
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []
    words = len(text.split())
    if words < args.min_words:
        fails.append(f"only {words} words (need >= {args.min_words})")

    checks = {
        "VERDICT": r"(BUY|MAYBE|PASS)\b.*",
        "PITCH": r"[ABC]\b.*",
        "NAME": r".+",
        "WOULD PAY": r"\$?\d+.*",
        "WHERE": r".+",
        "TOP OBJECTION": r".+",
    }
    values = {name: field(text, name, pattern) for name, pattern in checks.items()}
    for name, value in values.items():
        if not value:
            fails.append(f"missing parseable {name}: line")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {words} words; VERDICT={values['VERDICT'].split()[0]} PITCH={values['PITCH'].split()[0]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
