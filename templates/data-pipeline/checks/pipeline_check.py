#!/usr/bin/env python3
"""Validate staged data-pipeline handoffs."""

from __future__ import annotations

import argparse
import csv
import json
import operator
import re
import sys
from pathlib import Path
from typing import Any


OPS = {
    "==": operator.eq,
    "!=": operator.ne,
    ">=": operator.ge,
    "<=": operator.le,
    ">": operator.gt,
    "<": operator.lt,
}


def fail(message: str) -> None:
    print(f"FAIL: {message}")


def split_list(value: str) -> list[str]:
    if not value.strip():
        return []
    return [piece.strip() for piece in re.split(r"[,|]", value) if piece.strip()]


def split_invariants(value: str) -> list[str]:
    if not value.strip():
        return []
    return [piece.strip() for piece in value.split(";") if piece.strip()]


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    with path.open(encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, 1):
            if not line.strip():
                continue
            try:
                value = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path}:{line_number} is not valid JSON: {exc}") from exc
            if not isinstance(value, dict):
                raise ValueError(f"{path}:{line_number} is {type(value).__name__}, expected object")
            rows.append(value)
    return rows


def read_records(path: Path) -> list[dict[str, Any]]:
    if not path.is_file():
        raise ValueError(f"missing data file: {path}")
    if path.stat().st_size == 0:
        raise ValueError(f"data file is empty: {path}")
    suffix = path.suffix.lower()
    if suffix == ".csv":
        with path.open(newline="", encoding="utf-8") as handle:
            return [dict(row) for row in csv.DictReader(handle)]
    if suffix == ".json":
        value = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(value, list) or not all(isinstance(item, dict) for item in value):
            raise ValueError(f"{path} must be a JSON array of objects")
        return list(value)
    return read_jsonl(path)


def record_fields(rows: list[dict[str, Any]]) -> set[str]:
    fields: set[str] = set()
    for row in rows:
        fields.update(str(key) for key in row.keys())
    return fields


def is_empty(value: Any) -> bool:
    return value is None or (isinstance(value, str) and value.strip() == "")


def to_number(value: Any) -> float:
    if isinstance(value, (int, float)):
        return float(value)
    text = str(value).replace(",", "").strip()
    return float(text)


def count_rejects(path: Path | None, failures: list[str], require_file: bool = False) -> int:
    if path is None:
        return 0
    if not path.exists():
        if require_file:
            failures.append(f"missing rejects file: {path}")
        return 0
    if path.stat().st_size == 0:
        return 0
    if path.suffix.lower() == ".csv":
        with path.open(newline="", encoding="utf-8") as handle:
            return sum(1 for _ in csv.DictReader(handle))
    count = 0
    with path.open(encoding="utf-8") as handle:
        for line in handle:
            if line.strip():
                count += 1
    return count


def check_invariant(invariant: str, rows: list[dict[str, Any]], failures: list[str]) -> None:
    if invariant.startswith("count"):
        match = re.fullmatch(r"count\s*(==|!=|>=|<=|>|<)\s*(\d+)", invariant)
        if not match:
            failures.append(f"unsupported count invariant: {invariant}")
            return
        op, expected = match.groups()
        if not OPS[op](len(rows), int(expected)):
            failures.append(f"invariant failed: row count {len(rows)} does not satisfy {invariant}")
        return

    if invariant.startswith("unique:"):
        field = invariant.split(":", 1)[1].strip()
        values = [row.get(field) for row in rows if not is_empty(row.get(field))]
        if len(values) != len(set(map(str, values))):
            failures.append(f"invariant failed: {field} is not unique")
        return

    if invariant.startswith("nonempty:"):
        field = invariant.split(":", 1)[1].strip()
        empty_count = sum(1 for row in rows if is_empty(row.get(field)))
        if empty_count:
            failures.append(f"invariant failed: {field} has {empty_count} empty value(s)")
        return

    scope = "all"
    body = invariant
    if invariant.startswith("any:"):
        scope = "any"
        body = invariant.split(":", 1)[1].strip()
    elif invariant.startswith("all:"):
        body = invariant.split(":", 1)[1].strip()

    match = re.fullmatch(r"([A-Za-z0-9_.-]+)\s*(==|!=|>=|<=|>|<)\s*(.+)", body)
    if not match:
        failures.append(f"unsupported invariant: {invariant}")
        return
    field, op, expected_raw = match.groups()
    expected = expected_raw.strip().strip("'\"")

    results: list[bool] = []
    for row in rows:
        actual = row.get(field)
        if op in {">=", "<=", ">", "<"}:
            try:
                results.append(OPS[op](to_number(actual), float(expected)))
            except (TypeError, ValueError):
                results.append(False)
        else:
            results.append(OPS[op](str(actual).strip(), expected))

    if scope == "any":
        if not any(results):
            failures.append(f"invariant failed: no row satisfies {invariant}")
    elif not all(results):
        failed = len([result for result in results if not result])
        failures.append(f"invariant failed: {failed} row(s) do not satisfy {invariant}")


def validate_records(
    *,
    stage: str,
    data_file: Path,
    log_file: Path | None,
    schema_fields: list[str],
    required_fields: list[str],
    min_rows: int,
    invariants: list[str],
    rejects: Path | None,
    require_rejects_file: bool,
) -> tuple[list[str], int, list[dict[str, Any]]]:
    failures: list[str] = []
    try:
        rows = read_records(data_file)
    except ValueError as exc:
        return [str(exc)], 0, []

    if len(rows) < min_rows:
        failures.append(f"{stage}: row count {len(rows)} is below minimum {min_rows}")

    fields = record_fields(rows)
    missing_schema = [field for field in schema_fields if field not in fields]
    if missing_schema:
        failures.append(f"{stage}: missing schema field(s): {', '.join(missing_schema)}")

    for field in required_fields:
        empty_count = sum(1 for row in rows if is_empty(row.get(field)))
        if empty_count:
            failures.append(f"{stage}: required field {field} has {empty_count} empty value(s)")

    for invariant in invariants:
        check_invariant(invariant, rows, failures)

    reject_count = count_rejects(rejects, failures, require_file=require_rejects_file)

    if log_file is not None:
        if not log_file.is_file() or log_file.stat().st_size == 0:
            failures.append(f"{stage}: missing or empty log file: {log_file}")
        else:
            log_text = log_file.read_text(encoding="utf-8", errors="replace").lower()
            if "row count" not in log_text and "rows" not in log_text:
                failures.append(f"{stage}: log file does not record row counts")

    return failures, reject_count, rows


def records_command(args: argparse.Namespace) -> int:
    failures, reject_count, rows = validate_records(
        stage=args.stage,
        data_file=Path(args.file),
        log_file=Path(args.log) if args.log else None,
        schema_fields=split_list(args.schema_fields),
        required_fields=split_list(args.required_fields),
        min_rows=args.min_rows,
        invariants=split_invariants(args.invariants),
        rejects=Path(args.rejects) if args.rejects else None,
        require_rejects_file=args.require_rejects_file,
    )
    if failures:
        for item in failures:
            fail(item)
        return 1
    print(
        f"PASS: {args.stage} data has {len(rows)} row(s), required schema, no empty required values, "
        f"and {reject_count} reject(s)"
    )
    return 0


def report_command(args: argparse.Namespace) -> int:
    report_path = Path(args.report)
    failures: list[str] = []
    if not report_path.is_file() or report_path.stat().st_size == 0:
        failures.append(f"missing or empty validation report: {report_path}")
        for item in failures:
            fail(item)
        return 1

    report_text = report_path.read_text(encoding="utf-8", errors="replace").lower()
    for section in (
        "## verdict",
        "## row counts",
        "## schema",
        "## empty value scan",
        "## spot invariants",
        "## rejects",
        "## assumptions",
    ):
        if section not in report_text:
            failures.append(f"validation report missing section: {section}")

    record_failures, reject_count, rows = validate_records(
        stage=args.stage,
        data_file=Path(args.data_file),
        log_file=None,
        schema_fields=split_list(args.schema_fields),
        required_fields=split_list(args.required_fields),
        min_rows=args.min_rows,
        invariants=split_invariants(args.invariants),
        rejects=Path(args.rejects) if args.rejects else None,
        require_rejects_file=False,
    )
    failures.extend(record_failures)

    if str(len(rows)) not in report_text:
        failures.append(f"validation report does not mention actual row count {len(rows)}")
    if str(reject_count) not in report_text:
        failures.append(f"validation report does not mention reject count {reject_count}")

    if failures:
        for item in failures:
            fail(item)
        return 1
    print(
        f"PASS: validation report and transformed data agree on {len(rows)} row(s) and {reject_count} reject(s)"
    )
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    records = subparsers.add_parser("records", help="validate a data handoff file")
    records.add_argument("--stage", required=True)
    records.add_argument("--file", required=True)
    records.add_argument("--log", default="")
    records.add_argument("--schema-fields", default="")
    records.add_argument("--required-fields", default="")
    records.add_argument("--min-rows", type=int, default=1)
    records.add_argument("--invariants", default="")
    records.add_argument("--rejects", default="")
    records.add_argument("--require-rejects-file", action="store_true")
    records.set_defaults(func=records_command)

    report = subparsers.add_parser("report", help="validate a validation report against data")
    report.add_argument("--stage", required=True)
    report.add_argument("--report", required=True)
    report.add_argument("--data-file", required=True)
    report.add_argument("--schema-fields", default="")
    report.add_argument("--required-fields", default="")
    report.add_argument("--min-rows", type=int, default=1)
    report.add_argument("--invariants", default="")
    report.add_argument("--rejects", default="")
    report.set_defaults(func=report_command)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
