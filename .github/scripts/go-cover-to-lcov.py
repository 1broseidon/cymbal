#!/usr/bin/env python3
"""Convert a Go coverprofile to LCOV line coverage.

Go coverprofiles are block-oriented. For Codecov's project badge we want a
stable line-oriented view of the same profile: a line is covered when any
instrumented Go block touching that line ran.
"""

from __future__ import annotations

import argparse
from collections import defaultdict
from pathlib import Path


MODULE_PREFIX = "github.com/1broseidon/cymbal/"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("coverprofile", type=Path)
    parser.add_argument("lcov", type=Path)
    return parser.parse_args()


def repo_path(path: str) -> str:
    if path.startswith(MODULE_PREFIX):
        return path[len(MODULE_PREFIX) :]
    return path


def main() -> None:
    args = parse_args()
    files: dict[str, dict[int, int]] = defaultdict(dict)

    for raw in args.coverprofile.read_text().splitlines():
        if not raw or raw.startswith("mode:"):
            continue
        location, count_raw = raw.rsplit(" ", 1)
        range_part, _statement_count = location.rsplit(" ", 1)
        file_part, coords = range_part.split(":", 1)
        start, end = coords.split(",", 1)
        start_line = int(start.split(".", 1)[0])
        end_line = int(end.split(".", 1)[0])
        count = int(count_raw)

        line_hits = files[repo_path(file_part)]
        for line_no in range(start_line, end_line + 1):
            line_hits[line_no] = max(line_hits.get(line_no, 0), count)

    with args.lcov.open("w") as out:
        for path in sorted(files):
            line_hits = files[path]
            out.write("TN:\n")
            out.write(f"SF:{path}\n")
            for line_no in sorted(line_hits):
                out.write(f"DA:{line_no},{line_hits[line_no]}\n")
            out.write(f"LF:{len(line_hits)}\n")
            out.write(f"LH:{sum(1 for hits in line_hits.values() if hits > 0)}\n")
            out.write("end_of_record\n")


if __name__ == "__main__":
    main()
