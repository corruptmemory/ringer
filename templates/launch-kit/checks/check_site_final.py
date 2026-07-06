#!/usr/bin/env python3
"""Validate a final launch site assembled from brand and persona verdicts."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="site.html")
    parser.add_argument("--brand-name", required=True)
    parser.add_argument("--tagline", default="")
    parser.add_argument("--palette", default="")
    parser.add_argument("--required-pitch", default="")
    parser.add_argument("--min-images", type=int, default=4)
    parser.add_argument("--min-bytes", type=int, default=300_000)
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []
    lowered = text.lower()

    if args.brand_name.lower() not in lowered:
        fails.append(f"brand name missing: {args.brand_name}")
    if args.tagline and args.tagline.lower() not in lowered:
        fails.append(f"tagline missing: {args.tagline}")
    for hexc in [item.strip().lower() for item in args.palette.split(",") if item.strip()]:
        if hexc not in lowered:
            fails.append(f"palette color missing: {hexc}")
    if args.required_pitch and args.required_pitch.lower() not in lowered:
        fails.append("required winning pitch is not present verbatim")
    if re.search(r"working title|placeholder brand", lowered):
        fails.append("round-1 placeholder brand text is still present")

    size = path.stat().st_size
    if size < args.min_bytes:
        fails.append(f"{path} is {size} bytes (need >= {args.min_bytes})")
    embedded = len(re.findall(r"data:image/(?:jpeg|jpg|png|webp);base64,", text, flags=re.I))
    if embedded < args.min_images:
        fails.append(f"only {embedded} embedded images (need >= {args.min_images})")
    external = re.findall(r"(?:src|href)\s*=\s*['\"](https?://[^'\"]+)", text, flags=re.I)
    if external:
        fails.append(f"external requests found: {external[:4]}")
    if "<svg" not in lowered:
        fails.append("missing inline SVG wordmark")
    if not re.search(r"(?i)(draft|tbd|placeholder|coming soon|to be measured)", text):
        fails.append("missing honesty markers for draft prices, unknown specs, or policies")
    if not re.search(r"(?i)(objection|small print|make.it.right|refund|replace|trust|guarantee)", text):
        fails.append("missing trust or objection-answering copy from the persona panel")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {path} applies brand, pitch, embedded images, self-contained HTML, and panel objections")
    return 0


if __name__ == "__main__":
    sys.exit(main())
