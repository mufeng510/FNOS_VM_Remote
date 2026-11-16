#!/bin/bash
# 构建 fnos-vm-remote 的 FPK 安装包。
#
# 用法：
#   scripts/build-fpk.sh [输出目录]
#
# 环境变量：
#   FNPACK           fnpack 可执行文件路径（默认从 PATH 查找 fnpack）
#   SKIP_TESTS=1     跳过 go test（仅本地快速打包调试时使用，CI 不要设）
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(pwd)"

VERSION="$(grep -E '^version' fnos-app/manifest | head -n1 | awk -F= '{gsub(/[ \t]/,"",$2); print $2}')"
if [ -z "$VERSION" ]; then
  echo "无法从 fnos-app/manifest 读取 version" >&2
  exit 1
fi
echo "[build] 版本: ${VERSION}"

FNPACK_BIN="${FNPACK:-$(command -v fnpack || true)}"
if [ -z "$FNPACK_BIN" ]; then
  echo "未找到 fnpack。请安装 fnpack 1.2.3（见 docs/release-process.md）" >&2
  exit 1
fi

OUT_DIR="${1:-${ROOT}/dist}"
mkdir -p "$OUT_DIR"

echo "[build] 运行 Go 单元测试 ..."
if [ "${SKIP_TESTS:-0}" != "1" ]; then
  (cd backend && go test ./...)
fi

echo "[build] 编译静态二进制 (linux/amd64) ..."
SERVER_OUT="${ROOT}/fnos-app/app/server"
rm -rf "$SERVER_OUT"
mkdir -p "$SERVER_OUT"
(cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${SERVER_OUT}/fnos-vm-remote-server" ./cmd/server)
(cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w" \
  -o "${SERVER_OUT}/fnos-vm-remote-helper" ./cmd/vmhelper)

echo "[build] 校验脚本与 JSON ..."
for f in fnos-app/cmd/*; do bash -n "$f"; done
python3 - <<'EOF'
import glob, json, sys
files = glob.glob('fnos-app/wizard/*') + ['fnos-app/config/privilege', 'fnos-app/config/resource', 'fnos-app/app/ui/config']
for f in files:
    with open(f) as fh:
        json.load(fh)
print('[build] JSON 全部合法')
EOF

echo "[build] fnpack 打包 ..."
(cd fnos-app && "$FNPACK_BIN" build)

FPK_SRC="$(ls fnos-app/*.fpk 2>/dev/null | head -n 1)"
if [ -z "$FPK_SRC" ]; then
  echo "fnpack 未生成 .fpk" >&2
  exit 1
fi
DEST="${OUT_DIR}/fnos-vm-remote-v${VERSION}.fpk"
mv "$FPK_SRC" "$DEST"
echo "[build] 完成: $DEST"
ls -la "$DEST"
