#!/usr/bin/env python3
"""Validate that an image exists, has bytes, and meets optional dimensions."""

from __future__ import annotations

import argparse
import pathlib
import struct
import sys


def png_size(data: bytes) -> tuple[int, int] | None:
    if data[:8] != b"\x89PNG\r\n\x1a\n" or len(data) < 24:
        return None
    return struct.unpack(">II", data[16:24])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", required=True)
    parser.add_argument("--min-bytes", type=int, default=60_000)
    parser.add_argument("--min-width", type=int, default=0)
    parser.add_argument("--min-height", type=int, default=0)
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    fails: list[str] = []
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1

    data = path.read_bytes()
    if len(data) < args.min_bytes:
        fails.append(f"{path} is {len(data)} bytes (need >= {args.min_bytes})")
    dims = png_size(data)
    if dims is None:
        fails.append(f"{path} is not a valid PNG file")
    else:
        width, height = dims
        if args.min_width and width < args.min_width:
            fails.append(f"{path} width is {width}px (need >= {args.min_width})")
        if args.min_height and height < args.min_height:
            fails.append(f"{path} height is {height}px (need >= {args.min_height})")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    width, height = dims or (0, 0)
    print(f"PASS: {path} is {len(data)} bytes, {width}x{height}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
