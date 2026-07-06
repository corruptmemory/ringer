#!/usr/bin/env python3
"""Validate parseable name, pitch, and tagline candidates."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="candidates.md")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    sep = r"[:\-\u2014]"
    names = re.findall(rf"(?im)^\W*name\s*\d*\s*{sep}\s*(.+)$", text)
    pitches = re.findall(rf"(?im)^\W*pitch\s*[abc123]\s*{sep}\s*(.+)$", text)
    taglines = re.findall(rf"(?im)^\W*tagline\b[^:\-\u2014]*{sep}\s*(.+)$", text)

    if len(names) < 8:
        fails.append(f"only {len(names)} NAME lines (need >= 8)")
    if len(pitches) < 3:
        fails.append(f"only {len(pitches)} PITCH lines (need PITCH A, B, and C)")
    for index, pitch in enumerate(pitches[:3], start=1):
        if len(pitch.split()) > 30:
            fails.append(f"pitch {index} is {len(pitch.split())} words (expected a tight one-sentence pitch)")
    if len(taglines) < 3:
        fails.append(f"only {len(taglines)} TAGLINE lines (need >= 3)")
    if not re.search(rf"(?im)^\W*my pick\s*{sep}", text):
        fails.append("missing MY PICK recommendation")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {len(names)} names, {len(pitches)} pitches, {len(taglines)} taglines")
    return 0


if __name__ == "__main__":
    sys.exit(main())
