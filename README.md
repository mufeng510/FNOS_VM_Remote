# FNOS VM Remote

飞牛 FNOS 原生 FPK 应用：通过局域网远程控制指定的 Windows 虚拟机（`3f1jeea2`）——状态查询、开机、正常关机，配合 iPhone「快捷指令」绑定 NFC 标签使用。

> ⚠️ **状态说明**：本项目代码与 FPK 包结构已通过本地自动化测试与本地模拟生命周期验证，**尚未在真实 FNOS 设备上安装验证**。上真机前请先阅读 [docs/install-uninstall.md](docs/install-uninstall.md) 中的真机验证清单。

## 功能

- 虚拟机状态查询（running / shut off / 启动中 / 关机中 / paused / unknown）
- 开机、正常关机（**绝不强制断电**，超时仅提示）
- Web 管理界面（电脑/手机自适应，原生 HTML/JS，无前端框架）
- HTTP API（Bearer Token 鉴权，幂等设计）
- 默认端口 `8321`，安装向导可改，应用中心「设置」页可后续修改
- Token 安装时随机生成、仅存 SHA-256、可重新生成
- 操作日志（上限轮转，不记录 Token）
- GitHub Actions 自动构建、Tag 触发 Release 并附带 FPK

## 技术要点

| 层 | 选型 |
|---|---|
| 后端 | Go（静态二进制，CGO=0） |
| 前端 | 原生 HTML/CSS/JS，`go:embed` 内嵌 |
| 虚拟机控制 | `virsh -c qemu:///system`，经受限 helper（sudoers 授权，固定参数、校验 UUID） |
| 服务运行身份 | 应用包用户（非 root）；生命周期脚本为 root 用于系统集成 |
| 安装包 | fnpack 1.2.3 打包 `.fpk` |

**目标虚拟机**：名称 `3f1jeea2`，UUID `1ce21ce8-0bd1-4ab3-a9c0-064abf81381f`，Windows，12 GiB / 5 vCPU，**autostart 必须始终关闭**（本应用不会修改 autostart）。

## 快速开始

### 安装

1. 从 [Releases](https://github.com/mufeng510/FNOS_VM_Remote/releases) 下载 `fnos-vm-remote-vX.Y.Z.fpk`。
2. FNOS 应用中心 → 手动安装 → 选择该 `.fpk`。
3. 安装向导中确认/修改服务端口（默认 8321）。
4. 安装完成后，打开应用，按页面提示获取初始 Token（`initial-token.txt`）并登录。
5. 登录后立即在「Token 管理」中重新生成 Token。

详见 [docs/install-uninstall.md](docs/install-uninstall.md)。

### API

```bash
curl -H "Authorization: Bearer <TOKEN>" http://<FNOS-IP>:8321/api/v1/vm/status
curl -X POST -H "Authorization: Bearer <TOKEN>" http://<FNOS-IP>:8321/api/v1/vm/start
curl -X POST -H "Authorization: Bearer <TOKEN>" http://<FNOS-IP>:8321/api/v1/vm/shutdown
```

完整 API 文档：[docs/api.md](docs/api.md)
安全设计：[docs/security-design.md](docs/security-design.md)
NFC 快捷指令教程：[docs/nfc-guide.md](docs/nfc-guide.md)

## 开发

```bash
cd backend && go test ./...          # 单元测试
./scripts/build-fpk.sh               # 构建 FPK → dist/
./scripts/check-fpk.sh dist/*.fpk 0.1.0
```

要求：Go ≥ 1.24、fnpack 1.2.3（可选，仅打包时需要）、Python 3（校验 JSON 时用）。

## 版本与发布

- SemVer。推送 `v*` Tag → GitHub Actions 自动构建并发布 Release（含 FPK 附件）。
- 发版前确保 `fnos-app/manifest` 的 `version` 已同步。
- 详见 [docs/release-process.md](docs/release-process.md)。

## 许可证

MIT，详见 [LICENSE](LICENSE)。
