#!/usr/bin/env python3
"""Report per-folder file counts and reorg candidates."""

from __future__ import annotations

import argparse
import pathlib
from collections import defaultdict


DEFAULT_SKIP_DIRS = {".git", ".cursor", "node_modules", "bin", "dist", "build"}


def normalize(path: pathlib.Path) -> str:
    rel = str(path)
    return "." if rel == "." else rel.replace("\\", "/")


def walk_counts(root: pathlib.Path, skip_dirs: set[str]) -> dict[str, int]:
    counts: defaultdict[str, int] = defaultdict(int)
    for path in root.rglob("*"):
        if not path.is_file():
            continue
        if any(part in skip_dirs for part in path.parts):
            continue
        rel_parent = path.parent.relative_to(root)
        counts[normalize(rel_parent)] += 1
    return dict(counts)


def render_markdown(
    counts: dict[str, int],
    high_threshold: int,
    low_threshold: int,
) -> str:
    high = sorted(
        ((folder, count) for folder, count in counts.items() if count >= high_threshold),
        key=lambda pair: (-pair[1], pair[0]),
    )
    low = sorted(
        (
            (folder, count)
            for folder, count in counts.items()
            if low_threshold <= count < high_threshold
        ),
        key=lambda pair: (-pair[1], pair[0]),
    )

    lines = [
        "# Folder Size Audit",
        "",
        f"Thresholds: high >= {high_threshold}, low >= {low_threshold}",
        "",
        f"## Folders With {high_threshold}+ Files",
        "",
    ]
    if not high:
        lines.append("- None")
    else:
        for folder, count in high:
            lines.append(f"- {count}: `{folder}`")

    lines.extend(["", f"## Folders With {low_threshold}-{high_threshold - 1} Files", ""])
    if not low:
        lines.append("- None")
    else:
        for folder, count in low:
            lines.append(f"- {count}: `{folder}`")
    lines.append("")
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Audit file counts per folder and surface reorg candidates."
    )
    parser.add_argument(
        "--root",
        default=".",
        help="Project root to scan (default: current directory).",
    )
    parser.add_argument(
        "--high-threshold",
        type=int,
        default=10,
        help="High threshold (default: 10).",
    )
    parser.add_argument(
        "--low-threshold",
        type=int,
        default=5,
        help="Low threshold (default: 5).",
    )
    parser.add_argument(
        "--out",
        default="docs/folder-size-audit.md",
        help="Output markdown path (default: docs/folder-size-audit.md).",
    )
    args = parser.parse_args()

    root = pathlib.Path(args.root).resolve()
    if args.low_threshold > args.high_threshold:
        raise ValueError("--low-threshold must be <= --high-threshold")

    counts = walk_counts(root, DEFAULT_SKIP_DIRS)
    report = render_markdown(counts, args.high_threshold, args.low_threshold)

    out_path = root / args.out
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(report, encoding="utf-8")
    print(f"Wrote {out_path.relative_to(root)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
