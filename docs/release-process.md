# 安装与发布流程

## 一、本地构建与安装

```bash
# 依赖：Go ≥ 1.24、fnpack 1.2.3（打包时）、Python 3（可选）
./scripts/build-fpk.sh                # 产出 dist/fnos-vm-remote-v<manifest版本>.fpk
./scripts/check-fpk.sh dist/*.fpk 0.1.0
```

fnpack 1.2.3 获取（与 CI 一致）：

```bash
curl -fsSL https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64 -o ~/.local/bin/fnpack
echo "54b97fa7b70968c4d05c79840f5daeff508957d0bb2062fdb0376d00d9615c93  $HOME/.local/bin/fnpack" | sha256sum -c -
chmod +x ~/.local/bin/fnpack
```

安装到 FNOS：应用中心 → 手动安装 → 上传 `.fpk`。见 [install-uninstall.md](install-uninstall.md)。

## 二、版本策略

- SemVer；Manifest 版本为唯一来源（`fnos-app/manifest` 的 `version`）。
- 当前：`0.1.0`（迭代中）。正式发布：升到 `1.0.0`（**先完成真机验证**）。
- 升级版本：编辑 `fnos-app/manifest`，提交、推送 main；打 Tag 触发 Release。

## 三、GitHub Actions 一览

| Workflow | 触发 | 作用 |
|---|---|---|
| `.github/workflows/ci.yml` | PR / push main | Go 静态检查+单测 → FPK 构建+结构/版本校验 →（main）上传 Artifact |
| `.github/workflows/release.yml` | push `v*` Tag | 校验 tag==manifest 版本 → 完整检查 → 构建 → 校验 FPK → 创建 Release + 附件 + 自动生成 Notes 前接安装模板 |

发布安全：默认最小权限（CI 仅 `contents: read`；Release 仅 `contents: write`）；不需要 FNOS 凭据；Tag 与 manifest 不一致直接失败；已存在同名 Release 拒绝覆盖。

## 四、发版步骤（维护者）

```bash
# 1. 确保真机验证已完成、docs 已同步
# 2. 更新版本号
${EDITOR:-vim} fnos-app/manifest        # version = 1.0.0
./scripts/check-version.sh              # 校验 SemVer 格式

# 3. 提交并推送
git add -A && git commit -m "chore: release v1.0.0"
git push origin main

# 4. 打 Tag 并推送（触发 Release workflow）
git tag v1.0.0
git push origin v1.0.0
```

随后 GitHub Actions 自动：版本一致性校验 → 测试 → fnpack 打包 → FPK 结构/版本/文件名校验 → 创建 GitHub Release（附件 `fnos-vm-remote-v1.0.0.fpk`）→ 校验附件存在。

**不要手动上传 `.fpk` 或手建 Release。**

## 五、Release Notes 模板

见 [release-templates/release-body.md](release-templates/release-body.md)。Release 时模板内容（安装说明、校验、升级注意）会作为 "additional release notes" 前置，后接 GitHub 自动生成的变更列表（`--generate-notes`）。

## 六、回滚与撤销发布

- 不要删除已发布 Tag。如需撤回：标记 pre-release 或 yank 对应 Release，并在 README 注明问题版本。
- 发布失败（workflow 红）：修复后提交新 commit；若 Tag 已存在但 Release 未创建，重新运行 `release.yml`（Re-run jobs），或删除 Tag 重打——仅在确认 Release 未创建时才可重打 Tag。
