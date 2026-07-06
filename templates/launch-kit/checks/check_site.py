#!/usr/bin/env python3
"""Validate a self-contained first-pass launch site."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="site.html")
    parser.add_argument("--min-images", type=int, default=3)
    parser.add_argument("--min-bytes", type=int, default=250_000)
    parser.add_argument("--required-terms", default="")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    size = path.stat().st_size
    if size < args.min_bytes:
        fails.append(f"{path} is {size} bytes (need >= {args.min_bytes}; likely missing embedded media)")
    embedded = len(re.findall(r"data:image/(?:jpeg|jpg|png|webp);base64,", text, flags=re.I))
    if embedded < args.min_images:
        fails.append(f"only {embedded} embedded images (need >= {args.min_images})")
    external = re.findall(r"(?:src|href)\s*=\s*['\"](https?://[^'\"]+)", text, flags=re.I)
    if external:
        fails.append(f"external requests found; site must be self-contained: {external[:4]}")
    lowered = text.lower()
    for raw_term in [term.strip() for term in args.required_terms.split(",") if term.strip()]:
        if raw_term.lower() not in lowered:
            fails.append(f"missing required term or section: {raw_term}")
    if "<form" not in lowered and "interest" not in lowered:
        fails.append("missing form or order-interest placeholder")
    if not re.search(r"(?i)(draft|tbd|placeholder|assumption|coming soon|to be measured)", text):
        fails.append("missing honesty markers for draft prices, unknown specs, or policies")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {size} bytes, {embedded} embedded images, no external requests")
    return 0


if __name__ == "__main__":
    sys.exit(main())
