#!/usr/bin/env bash
# Works out the version of a CI run and writes it to $GITHUB_OUTPUT.
#
#   BUMP=patch|minor|major  release the next version after the newest vX.Y.Z tag
#   BUMP=none               no release; a dev build (dev-<sha>)
#   a pushed vX.Y.Z tag     release exactly that version
#   FIRST                   the version of the very first release (default v0.1.0)
#
# Outputs: version (v1.2.3 or dev-abc1234), number (1.2.3, or 0.0.0 for dev
# builds), release (true/false).
set -euo pipefail

BUMP=${BUMP:-none}
FIRST=${FIRST:-v0.1.0}
SHA=${GITHUB_SHA:-$(git rev-parse HEAD)}
semver='^v([0-9]+)\.([0-9]+)\.([0-9]+)$'

if [[ "${GITHUB_REF_TYPE:-}" == tag && "${GITHUB_REF_NAME:-}" =~ $semver ]]; then
  version=$GITHUB_REF_NAME
  release=true
elif [[ "$BUMP" != none ]]; then
  if [[ "${GITHUB_REF:-refs/heads/main}" != refs/heads/main ]]; then
    echo "::error::Releases are made from main; this run is on ${GITHUB_REF}." >&2
    exit 1
  fi
  last=$(git tag --list 'v*' --sort=-v:refname | grep -E "$semver" | head -n1 || true)
  if [[ -z "$last" ]]; then
    version=$FIRST
  else
    [[ "$last" =~ $semver ]]
    major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}
    case "$BUMP" in
      major) version="v$((major + 1)).0.0" ;;
      minor) version="v$major.$((minor + 1)).0" ;;
      patch) version="v$major.$minor.$((patch + 1))" ;;
      *) echo "::error::unknown bump '$BUMP'" >&2; exit 1 ;;
    esac
  fi
  if git rev-parse -q --verify "refs/tags/$version" >/dev/null; then
    echo "::error::tag $version already exists" >&2
    exit 1
  fi
  release=true
else
  version="dev-${SHA:0:7}"
  release=false
fi

number=${version#v}
[[ "$version" =~ $semver ]] || number=0.0.0

echo "version=$version number=$number release=$release"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "version=$version"
    echo "number=$number"
    echo "release=$release"
  } >> "$GITHUB_OUTPUT"
fi
if [[ -n "${GITHUB_STEP_SUMMARY:-}" && "$release" == true ]]; then
  echo "### Releasing $version" >> "$GITHUB_STEP_SUMMARY"
fi
