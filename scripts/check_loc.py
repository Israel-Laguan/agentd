#!/usr/bin/env python3
"""Fail when tracked text files exceed the configured LOC limit."""

from __future__ import annotations

import argparse
import fnmatch
import pathlib
import subprocess
import sys


DEFAULT_EXCLUDES = (
    "vendor/**",
    "dist/**",
    "build/**",
    "coverage/**",
    "**/generated/**",
    "*.min.js",
    "*.min.css",
    "go.sum",
    "web/package-lock.json",
)

CATEGORY_LIMITS = (
    ("*_test.go", 500),
    ("docs/**", 400),
)


def is_excluded(path: str, patterns: tuple[str, ...]) -> bool:
    return any(fnmatch.fnmatch(path, pattern) for pattern in patterns)


def count_lines(raw: bytes) -> int:
    if not raw:
        return 0
    text = raw.decode("utf-8", errors="ignore")
    return text.count("\n") + (0 if text.endswith("\n") else 1)


def tracked_files() -> list[str]:
    output = subprocess.check_output(["git", "ls-files", "-z"])
    return [entry for entry in output.decode("utf-8", errors="replace").split("\0") if entry]


def max_lines_for(path: str, default: int) -> int:
    for pattern, limit in CATEGORY_LIMITS:
        if fnmatch.fnmatch(path, pattern):
            return limit
    return default


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--max-lines", type=int, default=300)
    args = parser.parse_args()

    root = pathlib.Path.cwd()
    violations: list[tuple[int, str, int]] = []

    for rel_path in tracked_files():
        if is_excluded(rel_path, DEFAULT_EXCLUDES):
            continue
        path = root / rel_path
        if not path.is_file():
            continue
        raw = path.read_bytes()
        if b"\x00" in raw:
            continue
        line_count = count_lines(raw)
        limit = max_lines_for(rel_path, args.max_lines)
        if line_count > limit:
            violations.append((line_count, rel_path, limit))

    if not violations:
        print("LOC check passed: no tracked files exceed their limits.")
        return 0

    print(f"LOC check failed: {len(violations)} file(s) exceed their limits:")
    for lines, rel_path, limit in sorted(violations, reverse=True):
        print(f"  {lines:4d}/{limit:4d}  {rel_path}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
