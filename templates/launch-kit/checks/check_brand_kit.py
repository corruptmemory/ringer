#!/usr/bin/env python3
"""Validate a locked brand kit document."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="brand-kit.md")
    parser.add_argument("--brand-name", required=True)
    parser.add_argument("--tagline", default="")
    parser.add_argument("--palette", default="")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    if args.brand_name.lower() not in text.lower():
        fails.append(f"brand name missing: {args.brand_name}")
    if args.tagline and args.tagline.lower() not in text.lower():
        fails.append(f"tagline missing: {args.tagline}")
    present_hexes = {value.upper() for value in re.findall(r"#[0-9a-fA-F]{6}\b", text)}
    for hexc in [item.strip().upper() for item in args.palette.split(",") if item.strip()]:
        if hexc not in present_hexes:
            fails.append(f"palette color missing: {hexc}")
    if len(present_hexes) < 4:
        fails.append(f"only {len(present_hexes)} hex colors (need >= 4)")
    if not re.search(r"(?im)^#+\s*(?:\d+[\).]?\s*)?(type|typography)\b", text):
        fails.append("missing Type/Typography section")
    svg = re.search(r"<svg[\s\S]*?</svg>", text, flags=re.I)
    if not svg:
        fails.append("missing inline SVG wordmark")
    elif args.brand_name.lower() not in svg.group(0).lower():
        fails.append("SVG wordmark does not contain the brand name")
    if not re.search(r"(?i)(price|pricing|offer)", text):
        fails.append("missing pricing or offer notes")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: brand kit contains locked name, tagline, palette, type, SVG, and pricing notes")
    return 0


if __name__ == "__main__":
    sys.exit(main())
