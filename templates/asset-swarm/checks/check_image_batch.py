#!/usr/bin/env python3
"""Validate an idempotent generated-image batch."""

from __future__ import annotations

import argparse
import pathlib
import sys


PNG_MAGIC = b"\x89PNG\r\n\x1a\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--files", required=True, help="Comma-separated PNG filenames")
    parser.add_argument("--min-bytes", type=int, default=60_000)
    args = parser.parse_args()

    fails: list[str] = []
    files = [pathlib.Path(item.strip()) for item in args.files.split(",") if item.strip()]
    if not files:
        print("FAIL: no expected PNG files were supplied to the validator")
        return 1

    for path in files:
        if not path.exists():
            fails.append(f"missing expected PNG: {path}")
            continue
        data = path.read_bytes()
        if len(data) < args.min_bytes:
            fails.append(f"{path} is {len(data)} bytes (need >= {args.min_bytes})")
        if data[:8] != PNG_MAGIC:
            fails.append(f"{path} is not a PNG file")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    print(f"PASS: {len(files)} PNG files exist and meet the byte floor")
    return 0


if __name__ == "__main__":
    sys.exit(main())
