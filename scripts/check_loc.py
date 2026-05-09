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


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--max-lines", type=int, default=300)
    args = parser.parse_args()

    root = pathlib.Path.cwd()
    violations: list[tuple[int, str]] = []

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
        if line_count > args.max_lines:
            violations.append((line_count, rel_path))

    if not violations:
        print(f"LOC check passed: no tracked files exceed {args.max_lines} lines.")
        return 0

    print(f"LOC check failed: {len(violations)} file(s) exceed {args.max_lines} lines:")
    for lines, rel_path in sorted(violations, reverse=True):
        print(f"  {lines:4d}  {rel_path}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
