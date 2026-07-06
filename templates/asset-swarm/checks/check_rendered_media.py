#!/usr/bin/env python3
"""Run a host-side render command and validate the produced media files."""

from __future__ import annotations

import argparse
import pathlib
import shlex
import subprocess
import sys


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True)
    parser.add_argument("--render-command", required=True)
    parser.add_argument("--outputs", required=True, help="Comma-separated output files to validate")
    parser.add_argument("--min-bytes", type=int, default=100_000)
    args = parser.parse_args()

    fails: list[str] = []
    source = pathlib.Path(args.source)
    if not source.exists():
        print(f"FAIL: source file missing before render: {source}")
        return 1
    if source.stat().st_size == 0:
        print(f"FAIL: source file is empty before render: {source}")
        return 1

    print(f"running render command: {args.render_command}")
    proc = subprocess.run(args.render_command, shell=True, capture_output=True, text=True, timeout=1800)
    if proc.returncode != 0:
        print("FAIL: render command failed")
        print(proc.stdout[-3000:])
        print(proc.stderr[-3000:])
        return 1
    if proc.stdout.strip():
        print(proc.stdout[-2000:])
    if proc.stderr.strip():
        print(proc.stderr[-2000:])

    for raw in [item.strip() for item in args.outputs.split(",") if item.strip()]:
        path = pathlib.Path(raw)
        if not path.exists():
            fails.append(f"missing rendered output: {path}")
            continue
        size = path.stat().st_size
        if size < args.min_bytes:
            fails.append(f"{path} is {size} bytes (need >= {args.min_bytes})")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        print("render command tokens:", shlex.split(args.render_command)[:6])
        return 1
    print(f"PASS: rendered outputs exist and meet byte floor: {args.outputs}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
