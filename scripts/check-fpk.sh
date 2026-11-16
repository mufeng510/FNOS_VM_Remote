#!/bin/bash
# 校验 FPK 安装包结构完整性（CI 用）。
# 用法: scripts/check-fpk.sh <path/to/file.fpk> [expected-version]
set -euo pipefail

FPK="${1:?用法: check-fpk.sh <file.fpk> [expected-version]}"
EXPECT="${2:-}"

[ -f "$FPK" ] || { echo "[check] FPK 不存在: $FPK" >&2; exit 1; }
[ -s "$FPK" ] || { echo "[check] FPK 为空文件: $FPK" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "[check] 解包 $FPK ..."
tar xzf "$FPK" -C "$TMP"

for required in manifest config/privilege config/resource cmd/main wizard/install wizard/config wizard/uninstall app.tgz ICON.PNG ICON_256.PNG; do
  if [ ! -e "$TMP/$required" ]; then
    echo "[check] 缺少必需成员: $required" >&2
    exit 1
  fi
done

mkdir -p "$TMP/app"
tar xzf "$TMP/app.tgz" -C "$TMP/app"
for required in server/fnos-vm-remote-server server/fnos-vm-remote-helper ui/config ui/images/icon_64.png ui/images/icon_256.png; do
  if [ ! -e "$TMP/app/$required" ]; then
    echo "[check] app.tgz 缺少必需成员: $required" >&2
    exit 1
  fi
done

# manifest 必查字段
M="$TMP/manifest"
for field in appname version display_name platform source maintainer distributor desktop_uidir desktop_applaunchname; do
  grep -Eq "^${field}[[:space:]]*=" "$M" || { echo "[check] manifest 缺少字段: $field" >&2; exit 1; }
done

VERSION="$(grep -E '^version' "$M" | head -n1 | awk -F= '{gsub(/[ \t]/,"",$2); print $2}')"
echo "[check] FPK manifest version = ${VERSION}"
if [ -n "$EXPECT" ] && [ "$VERSION" != "$EXPECT" ]; then
  echo "[check] FPK 版本 (${VERSION}) 与期望版本 (${EXPECT}) 不一致" >&2
  exit 1
fi

# privilege/resource 必须是合法 JSON
python3 - "$TMP" <<'EOF'
import json, os, sys
tmp = sys.argv[1]
json_files = {
    "config/privilege": f"{tmp}/config/privilege",
    "config/resource": f"{tmp}/config/resource",
    "app/ui/config": f"{tmp}/app/ui/config",
}
for name, path in json_files.items():
    with open(path) as fh:
        json.load(fh)
    print(f"[check] JSON OK: {name}")
EOF

# 权限：cmd/* 必须可执行
for f in "$TMP"/cmd/*; do
  [ -x "$f" ] || { echo "[check] 脚本不可执行: $f" >&2; exit 1; }
done

echo "[check] FPK 结构校验通过"
