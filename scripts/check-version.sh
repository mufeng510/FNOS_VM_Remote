#!/bin/bash
# 校验发版条件：
#   1. manifest 中的 version 与可选的 git tag（vX.Y.Z）一致
#   2. version 为合法 SemVer（MAJOR.MINOR.PATCH，允许 -rc.N 等预发布后缀）
#
# 用法：
#   scripts/check-version.sh           # 仅校验 manifest version 格式
#   scripts/check-version.sh v0.1.0    # 同时校验 tag 与 manifest 一致
set -euo pipefail
cd "$(dirname "$0")/.."

MANIFEST_VERSION="$(grep -E '^version' fnos-app/manifest | head -n1 | awk -F= '{gsub(/[ \t]/,"",$2); print $2}')"
SEMVER_RE='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'

if ! [[ "$MANIFEST_VERSION" =~ $SEMVER_RE ]]; then
  echo "manifest version 不是合法 SemVer: ${MANIFEST_VERSION}" >&2
  exit 1
fi
echo "manifest version: ${MANIFEST_VERSION}"

if [ "${1:-}" != "" ]; then
  TAG="${1#v}"
  TAG="${TAG%$'\n'}"
  if [ "$TAG" != "$MANIFEST_VERSION" ]; then
    echo "Tag (v${TAG}) 与 manifest version (${MANIFEST_VERSION}) 不一致" >&2
    exit 1
  fi
  echo "Tag 与 manifest version 一致 (v${MANIFEST_VERSION})"
fi
