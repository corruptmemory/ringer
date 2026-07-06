#!/usr/bin/env python3
"""Validate storefront listing copy with honesty markers."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="listing-copy.md")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    products = re.split(r"(?im)^#+\s*(?:\d+[\).]?\s*)?product\b", text)[1:]
    if len(products) < 3:
        fails.append(f"only {len(products)} PRODUCT sections (need 3)")
    for index, section in enumerate(products[:3], start=1):
        title = re.search(r"(?im)^\W*title\s*[:\-\u2014]\s*(.+)$", section)
        if not title:
            fails.append(f"product {index}: missing TITLE line")
        elif len(title.group(1)) > 140:
            fails.append(f"product {index}: title is {len(title.group(1))} chars (need <= 140)")
        bullets = re.findall(r"(?m)^\s*[-*]\s+\S", section)
        if len(bullets) < 3:
            fails.append(f"product {index}: only {len(bullets)} bullets (need >= 3)")

    words = len(text.split())
    if words < 250:
        fails.append(f"only {words} words (need >= 250)")
    if not re.search(r"(?i)(assumption|tbd|to be measured|draft|coming soon|placeholder)", text):
        fails.append("missing honesty markers for unknown specs, prices, or policies")
    if not re.search(r"(?im)^#+\s*about", text):
        fails.append("missing ABOUT section")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {len(products)} product sections, {words} words, honesty markers present")
    return 0


if __name__ == "__main__":
    sys.exit(main())
