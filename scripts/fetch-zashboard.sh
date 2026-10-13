#!/usr/bin/env bash
# Fetch the panel this binary serves.
#
# The panel is zashboard (https://github.com/Zephyruso/zashboard, MIT), used as
# its published build rather than rebuilt from source: it is a large Vue app
# with its own toolchain, and pinning a release plus its checksum makes the
# panel a fixed input to this repo instead of a moving dependency.
#
# Output lands in backend/internal/web/dist, which is gitignored and embedded by
# go:embed. Re-run it to move to a new panel version: change the two constants
# below together, since the checksum is what makes the pin mean anything.
set -euo pipefail

VERSION="v3.29.1"
# dist-no-fonts: system fonts only. The font-bundled builds are 3-5x larger for
# a dashboard that renders fine either way.
SHA256="21371cd111b6b3d87774f3ea3ddeacdcb0f6aff8690a16d652daa3ea6e3b0145"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${REPO_ROOT}/backend/internal/web/dist"
URL="https://github.com/Zephyruso/zashboard/releases/download/${VERSION}/dist-no-fonts.zip"

for tool in curl unzip sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "fetch-zashboard: ${tool} is required" >&2
    exit 1
  }
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "fetch-zashboard: downloading ${VERSION}"
curl -fsSL -o "${work}/panel.zip" "$URL"

echo "fetch-zashboard: verifying checksum"
echo "${SHA256}  ${work}/panel.zip" | sha256sum -c - >/dev/null

unzip -q "${work}/panel.zip" -d "$work"
[ -f "${work}/dist/index.html" ] || {
  echo "fetch-zashboard: archive did not contain dist/index.html" >&2
  exit 1
}

# Replace the tree wholesale so a file dropped between panel versions does not
# linger and get embedded. .gitkeep is restored so a fresh checkout still has
# the directory the placeholder exists for.
mkdir -p "$DEST"
rm -rf "${DEST:?}"/*
cp -R "${work}/dist/." "$DEST/"
touch "${DEST}/.gitkeep"

echo "fetch-zashboard: panel ${VERSION} is in backend/internal/web/dist"
