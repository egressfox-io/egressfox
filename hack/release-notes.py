#!/usr/bin/env python3
"""Extract exactly one reviewed changelog section for a release."""

import argparse
import pathlib
import re
import sys


def notes(changelog, version):
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-(?:dev|alpha|beta)\.[1-9]\d*)?", version):
        raise ValueError("invalid release version")
    heading = f"## [{version}]"
    lines = changelog.splitlines()
    starts = [i for i, line in enumerate(lines) if line == heading]
    if len(starts) != 1:
        raise ValueError(f"expected exactly one {heading} section")
    start = starts[0] + 1
    end = next((i for i in range(start, len(lines)) if lines[i].startswith("## ")), len(lines))
    body = "\n".join(lines[start:end]).strip()
    if not re.search(r"(?m)^### (Added|Changed|Deprecated|Removed|Fixed|Security)$", body):
        raise ValueError("release section has no change category")
    if not re.search(r"(?m)^- .+", body):
        raise ValueError("release section has no entries")
    return body + "\n"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--changelog", default="CHANGELOG.md")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    result = notes(pathlib.Path(args.changelog).read_text(), args.version)
    pathlib.Path(args.output).write_text(result)


if __name__ == "__main__":
    try:
        main()
    except ValueError as error:
        print(f"release notes rejected: {error}", file=sys.stderr)
        sys.exit(1)
