#!/usr/bin/env python3
"""Validate parseable brand-board directions."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="brand-board.md")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    directions = re.split(r"(?im)^#+\s*(?:\d+[\).]?\s*)?direction\b", text)[1:]
    if len(directions) < 3:
        fails.append(f"only {len(directions)} DIRECTION sections (need 3)")

    for index, section in enumerate(directions, start=1):
        hexes = re.findall(r"#[0-9a-fA-F]{6}\b", section)
        if len(hexes) < 4:
            fails.append(f"direction {index}: only {len(hexes)} hex colors (need >= 4)")
        if not re.search(r"(?i)headline\s*(type|font)", section):
            fails.append(f"direction {index}: missing HEADLINE TYPE")
        if not re.search(r"(?i)body\s*(type|font)", section):
            fails.append(f"direction {index}: missing BODY TYPE")
        if not re.search(r"(?i)why\s*(it\s*fits|this)", section):
            fails.append(f"direction {index}: missing WHY IT FITS")

    if not re.search(r"(?im)^\W*my pick\s*[:\-\u2014]", text):
        fails.append("missing MY PICK recommendation")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {len(directions)} directions with palettes, type, rationales, and MY PICK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
