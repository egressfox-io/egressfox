#!/usr/bin/env python3
"""Generate the next release section from emoji Conventional Commits."""

import argparse
import json
import pathlib
import re
import subprocess
import sys


VERSION = re.compile(r"^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(dev|alpha|beta)\.([1-9]\d*))?$")
COMMIT = re.compile(r"^(?P<emoji>\S+) (?P<type>[a-z]+)(?:\((?P<scope>[^()]+)\))?(?P<breaking>!)?: (?P<description>.+)$")
HEADINGS = re.compile(r"(?m)^## \[([^\]]+)\](?: - .*?)?$", re.MULTILINE)
CATEGORIES = ("Added", "Changed", "Fixed", "Security")
INCLUDED = {"feat": "Added", "perf": "Changed", "fix": "Fixed", "revert": "Fixed"}
INTRO = "# Changelog\n\nGenerated from emoji-prefixed Conventional Commits during explicit release preparation.\nRelease entries are not edited by hand.\n\n"


def git(*arguments):
    return subprocess.check_output(["git", *arguments], stderr=subprocess.PIPE)


def version_key(value):
    match = VERSION.fullmatch(value)
    if not match:
        raise ValueError(f"invalid release version: {value}")
    major, minor, patch, stage, number = match.groups()
    return (int(major), int(minor), int(patch), {"dev": 0, "alpha": 1, "beta": 2, None: 3}[stage], int(number or 0))


def previous_tag(version):
    candidates = []
    for tag in git("tag", "--merged", "HEAD", "--list").decode().splitlines():
        if not VERSION.fullmatch(tag) or tag == version:
            continue
        distance = int(git("rev-list", "--count", f"{tag}..HEAD").decode().strip())
        candidates.append((distance, version_key(tag), tag))
    if not candidates:
        return None
    distance, key, tag = min(candidates, key=lambda item: (item[0], tuple(-n for n in item[1])))
    if version_key(version) <= key:
        raise ValueError(f"release version must advance past {tag}")
    return tag


def commits_since(tag):
    revision = f"{tag}..HEAD" if tag else "HEAD"
    raw = git("log", "--no-merges", "--reverse", "--topo-order", "-z", "--format=%H%x00%s%x00%b", revision)
    fields = raw.decode("utf-8").split("\x00")
    if fields[-1] == "":
        fields.pop()
    if len(fields) % 3:
        raise ValueError("unexpected Git log record format")
    return [(fields[index], fields[index + 1], fields[index + 2]) for index in range(0, len(fields), 3)]


def entry(subject, body):
    match = COMMIT.fullmatch(subject)
    if not match:
        if re.search(r"(?:^|\s)(?:feat|fix|perf|revert)(?:\(|!?:)", subject):
            raise ValueError("user-facing Conventional Commit lacks a leading emoji or valid subject")
        return None
    kind = match["type"]
    if kind not in INCLUDED:
        return None
    emoji = match["emoji"]
    description = re.sub(r"\s+", " ", match["description"].strip())
    if description.startswith(emoji + " "):
        description = description[len(emoji) + 1 :]
    if "[skip changelog]" in subject.lower() or re.search(r"(?im)^Changelog: skip\s*$", body):
        return None
    if kind == "perf" and re.match(r"(?i)^benchmark\b", description):
        return None
    if not description:
        raise ValueError("empty user-facing commit description")
    category = "Security" if emoji == "🔒" or match["scope"] == "security" else INCLUDED[kind]
    if match["breaking"] or re.search(r"(?m)^BREAKING CHANGE:", body):
        description = "Breaking: " + description
        if category != "Security":
            category = "Changed"
    description = description[0].upper() + description[1:]
    if description[-1] not in ".!?":
        description += "."
    return category, f"- {emoji} {description}"


def release_section(version, commits, allow_empty=False):
    grouped = {category: [] for category in CATEGORIES}
    seen = set()
    for _, subject, body in commits:
        item = entry(subject, body)
        if item is None:
            continue
        category, bullet = item
        key = re.sub(r"[^\w]+", "", bullet.split(" ", 2)[-1].casefold())
        if key in seen:
            continue
        seen.add(key)
        grouped[category].append(bullet)
    if not any(grouped.values()) and not allow_empty:
        raise ValueError("no user-facing commits since the previous release tag")
    output = [f"## [{version}]"]
    for category in CATEGORIES:
        if grouped[category]:
            output.extend(("", f"### {category}", "", *grouped[category]))
    return "\n".join(output) + "\n"


def render(current, version, section):
    old = current.replace("\r\n", "\n")
    headings = list(HEADINGS.finditer(old))
    historical = []
    names = set()
    for index, heading in enumerate(headings):
        name = heading.group(1)
        if name in names:
            raise ValueError(f"duplicate changelog section: {name}")
        names.add(name)
        if name in ("Unreleased", version):
            continue
        if not VERSION.fullmatch(name):
            raise ValueError(f"unexpected changelog section: {name}")
        end = headings[index + 1].start() if index + 1 < len(headings) else len(old)
        historical.append(old[heading.start() : end].strip())
    return INTRO + "## [Unreleased]\n\n" + section + ("\n" + "\n\n".join(historical) + "\n" if historical else "")


def planned_version():
    data = json.loads(pathlib.Path("release/manifest.json").read_text())
    return data["release"]["developmentVersion"]


def generate(version, current, allow_empty=False):
    version_key(version)
    if version != planned_version():
        raise ValueError("version differs from release/manifest.json")
    tag = previous_tag(version)
    section = release_section(version, commits_since(tag), allow_empty=allow_empty)
    return render(current, version, section), tag


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", default=None)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--write", action="store_true")
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--preview", action="store_true")
    args = parser.parse_args()
    version = args.version or planned_version()
    path = pathlib.Path("CHANGELOG.md")
    current = path.read_text() if path.exists() else ""
    generated, tag = generate(version, current, allow_empty=args.preview)
    if args.check:
        if current != generated:
            raise ValueError("CHANGELOG.md differs from Git history; run release preparation before tagging")
    elif args.write:
        path.write_text(generated)
    else:
        print(generated)
    baseline = tag or "repository root"
    print(f"changelog {version}: baseline {baseline}", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, subprocess.CalledProcessError, KeyError) as error:
        print(f"changelog generation rejected: {error}", file=sys.stderr)
        sys.exit(1)
