#!/bin/sh
# Publish a new release on GitHub.
#
#   1. Change   const version = "..."   in main.go
#   2. Add a matching  ## <version>  section to CHANGELOG.md
#   3. ./release.sh
#
# This checks everything is committed, then pushes a tag such as v1.8.
# GitHub sees the tag, runs the tests, builds Windows, Mac and Linux
# versions and publishes the release (.github/workflows/release.yml).
set -e
cd "$(dirname "$0")"

v=$(sed -n 's/^const version = "\(.*\)"/\1/p' main.go)
if [ -z "$v" ]; then
  echo "Could not find  const version = \"...\"  in main.go."; exit 1
fi
tag="v$v"

if ! grep -Eq "^## $v([^0-9.]|\$)" CHANGELOG.md; then
  echo "CHANGELOG.md has no '## $v' section. Add one; it becomes the release notes."; exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
  echo "You have changes that are not committed. Run ./push.sh first."; exit 1
fi
git fetch --tags --quiet origin
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "$tag has already been released. Raise the version in main.go for a new one."; exit 1
fi

git push origin HEAD
git tag -a "$tag" -m "Sporeline $v"
git push origin "$tag"

url=$(git remote get-url origin | sed -e 's#^git@github.com:#https://github.com/#' -e 's#\.git$##')
echo
echo "  Tagged $tag. GitHub is building it now:"
echo "  $url/actions"
echo "  The release appears here in a few minutes:"
echo "  $url/releases"
