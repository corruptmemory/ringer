#!/usr/bin/env python3
"""Validate an adversarial review report with structured findings."""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


REQUIRED_LABELS = ["Finding", "Evidence", "Impact", "Fix", "Priority", "Confidence"]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--file", default="report.md")
    args = parser.parse_args()

    path = pathlib.Path(args.file)
    if not path.exists():
        print(f"FAIL: {path} not found")
        return 1
    text = path.read_text(encoding="utf-8", errors="replace")
    fails: list[str] = []

    if not re.search(r"(?im)^#+\s*summary\b", text):
        fails.append("missing ## Summary section")

    finding_blocks = re.split(r"(?im)^Finding\s*:\s*", text)
    finding_count = len(finding_blocks) - 1
    no_findings = bool(re.search(r"(?i)\bNO FINDINGS\b", text))

    if finding_count == 0 and not no_findings:
        fails.append("report must contain NO FINDINGS or at least one Finding: block")

    for index, block in enumerate(finding_blocks[1:], start=1):
        block_text = "Finding: " + block
        for label in REQUIRED_LABELS:
            if not re.search(rf"(?im)^{label}\s*:", block_text):
                fails.append(f"finding {index}: missing {label}: label")
        priority = re.search(r"(?im)^Priority\s*:\s*(P[0-3])\b", block_text)
        if not priority:
            fails.append(f"finding {index}: Priority must be P0, P1, P2, or P3")
        confidence = re.search(r"(?im)^Confidence\s*:\s*(high|medium|low)\b", block_text)
        if not confidence:
            fails.append(f"finding {index}: Confidence must be high, medium, or low")
        evidence = re.search(r"(?ims)^Evidence\s*:\s*(.+?)(?:\n[A-Z][A-Za-z ]+\s*:|\Z)", block_text)
        if evidence and len(evidence.group(1).strip()) < 20:
            fails.append(f"finding {index}: Evidence is too thin; cite a file, route, log, or reproduction detail")

    if re.search(r"(?i)\b(i\s+(fixed|patched|committed|pushed|modified)|patched\s+the|committed\s+the|pushed\s+the)\b", text):
        fails.append("reviewer appears to claim it changed files; reviewers must not fix")

    if fails:
        print("FAIL:")
        for fail in fails:
            print(f" - {fail}")
        return 1
    if no_findings and finding_count == 0:
        print("PASS: explicit no-findings report with summary")
    else:
        print(f"PASS: {finding_count} structured finding block(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
