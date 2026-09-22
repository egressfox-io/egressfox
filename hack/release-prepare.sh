#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?set VERSION to the planned vX.Y.Z release version}"

branch=$(git branch --show-current)
if [[ -z "$branch" || "$branch" == main ]]; then
  echo 'release preparation requires a dedicated task branch' >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo 'release preparation requires a clean working tree' >&2
  exit 1
fi
if git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
  echo 'release version tag already exists' >&2
  exit 1
fi

python3 hack/generate-changelog.py --version "$VERSION" --write
if git diff --quiet -- CHANGELOG.md; then
  echo 'changelog already prepared; no commit created'
  exit 0
fi
git add -- CHANGELOG.md
git diff --cached --check
git commit -m "📝 docs(changelog): prepare $VERSION release notes"
python3 hack/generate-changelog.py --version "$VERSION" --check
