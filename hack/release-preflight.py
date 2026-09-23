#!/usr/bin/env python3
"""Read-only, fail-closed remote checks before any release publication."""

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request


API = "https://api.github.com"


def require(name):
    value = os.environ.get(name, "")
    if not value:
        raise ValueError(f"{name} is required")
    return value


def api(operation, path, missing_ok=False):
    request = urllib.request.Request(
        API + path,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + require("GH_TOKEN"),
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "egressfox-release-preflight",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if missing_ok and error.code == 404:
            return None
        raise RuntimeError(f"GitHub API {operation} failed with HTTP {error.code}") from None
    except (json.JSONDecodeError, UnicodeDecodeError):
        raise ValueError(f"GitHub API {operation} returned malformed JSON") from None
    except urllib.error.URLError:
        raise RuntimeError(f"GitHub API {operation} request failed") from None


def pages(operation, path, missing_ok=False):
    page = 1
    while True:
        separator = "&" if "?" in path else "?"
        result = api(
            f"{operation} page {page}",
            f"{path}{separator}per_page=100&page={page}",
            missing_ok=missing_ok and page == 1,
        )
        if result is None:
            return
        if not isinstance(result, list):
            raise ValueError(f"GitHub API {operation} returned malformed response: expected an array")
        yield from result
        if len(result) < 100:
            return
        page += 1


def preflight():
    version = require("VERSION")
    repository = require("GITHUB_REPOSITORY")
    sha = require("GITHUB_SHA")
    if not re.fullmatch(r"v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:dev|alpha|beta)\.[1-9]\d*)?", version):
        raise ValueError("invalid release version")
    if repository != "egressfox-io/egressfox":
        raise ValueError("unexpected release repository")
    if os.environ.get("GITHUB_REF_TYPE") != "tag" or os.environ.get("GITHUB_REF_NAME") != version:
        raise ValueError("release must run from the exact version tag")
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    tag = subprocess.check_output(["git", "rev-parse", f"refs/tags/{version}^{{commit}}"], text=True).strip()
    if sha != head or tag != head:
        raise ValueError("tag, workflow source and checked-out commit differ")
    owner, _ = repository.split("/", 1)
    owner = urllib.parse.quote(owner, safe="")
    info = api("repository identity lookup", f"/repos/{repository}")
    if not isinstance(info, dict) or not isinstance(info.get("full_name"), str) or info["full_name"].lower() != repository.lower():
        raise ValueError("repository identity mismatch")
    quoted = urllib.parse.quote(version, safe="")
    if api("GitHub Release lookup", f"/repos/{repository}/releases/tags/{quoted}", missing_ok=True) is not None:
        raise ValueError("GitHub Release already exists; advance the version")
    package = urllib.parse.quote("egressfox", safe="")
    for item in pages(
        "GHCR package version lookup",
        f"/orgs/{owner}/packages/container/{package}/versions",
        missing_ok=True,
    ):
        if not isinstance(item, dict):
            raise ValueError("GHCR package version lookup returned malformed response: expected an object")
        metadata = item.get("metadata")
        container = metadata.get("container") if isinstance(metadata, dict) else None
        tags = container.get("tags") if isinstance(container, dict) else None
        if not isinstance(tags, list) or not all(isinstance(tag, str) for tag in tags):
            raise ValueError("GHCR package version lookup returned malformed response: expected tag array")
        if version.removeprefix("v") in tags:
            raise ValueError("GHCR version tag already exists; advance the version")
    print(f"remote preflight passed for {version} at {head}")


if __name__ == "__main__":
    try:
        preflight()
    except (ValueError, RuntimeError, urllib.error.URLError, subprocess.CalledProcessError) as error:
        print(f"release preflight rejected: {error}", file=sys.stderr)
        sys.exit(1)
