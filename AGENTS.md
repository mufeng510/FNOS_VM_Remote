# AGENTS.md — FNOS VM Remote 项目知识

供后续开发者/Agent 使用。记录架构、约束、命令与关键决策。

## 项目身份

- 名称：FNOS VM Remote（`fnos-vm-remote`）
- 目标：FNOS（飞牛 NAS）原生 FPK 应用，局域网控制指定 Windows VM `3f1jeea2`（UUID `1ce21ce8-0bd1-4ab3-a9c0-064abf81381f`）
- 目标用户：个人家庭局域网自用
- 许可证：MIT

## 硬性约束（不得违反）

1. **虚拟机 autostart 必须始终关闭**。任何脚本/代码不得修改 autostart（`virsh autostart`），不得 `define`/`undefine`/`destroy`。
2. **正常关机 = `virsh -c qemu:///system shutdown`**。超时后绝不强制断电。
3. **禁止** 出现"端口配置与监听不一致"：改端口必须校验→写配置→重启→健康检查（HTTP `GET /api/v1/health` 匹配 `"app":"fnos-vm-remote"`）→失败回滚。
4. **Token 明文不落盘**（服务端仅存 SHA-256）；日志不记 Token；首次打开应用时在初始化页生成，明文仅返回一次。
5. VM 控制**只能**经 `fnos-vm-remote-helper`（sudoers NOPASSWD 授权包用户，子命令白名单 `status|start|shutdown`，VM 名/UUID 硬编码，执行前 `domuuid` 校验）。
6. API **不接受**任何 VM 标识参数；只管理 `3f1jeea2`。
7. FNOS 官方规范优先于社区文档；镜像站 fnapps-doc.tnnevol.cn 与本仓库 docs 中的摘录仅作参考。

## 架构

```
浏览器/快捷指令 ──HTTP Bearer──> fnos-vm-remote-server（包用户，:8321）
                                   │ sudo -n helper（固定参数）
                                   ▼
                       fnos-vm-remote-helper（root 经 sudoers）
                                   │ exec 固定路径 /usr/bin/virsh -c qemu:///system
                                   ▼
                              libvirtd (qemu:///system) → VM
```

- Web 界面：Go `net/http` + `go:embed`（`backend/internal/httpx/web/`），无框架。
- 数据：`$TRIM_PKGVAR`（= `/var/apps/fnos-vm-remote/var`）：`config.json`（端口）、`auth.json`（Token SHA-256）、`logs/`、`info.log`、`app.pid`（服务进程 pid）。
- 升级时 FNOS 保留 `TRIM_PKGVAR`；卸载由 `wizard/uninstall` 选择保留/删除。

## 关键决策（ADR 摘要）

| 决策 | 理由 |
|---|---|
| Go 静态二进制 + 原生前端 | FNOS=Debian 12 x86_64，无需额外运行时；零前端依赖 |
| 生命周期脚本 `run-as=root` + 服务降权（runuser→包用户） | 官方允许特权脚本"准备系统集成"；写 `/etc/sudoers.d` 需要 root；服务本体永不 root |
| 不采用 libvirt 组授权 | 组权限=任意 VM 完整控制，违反最小权限 |
| 端口默认 8321，`checkport=false` | 避开 fnOS 5666 系端口；占用检查由 `config_init`/`config_callback` 完成 |
| 端口修改走 FNOS「设置」页（`wizard/config`，字段 `wizard_port`）；入口端口由生命周期脚本写入（`render_entry`） | 真机实测 FNOS 不替换 `${wizard_port}` 占位符，入口地址必须自行渲染；`config_init` 校验 + `config_callback` 重启验证回滚 |
| 入口 `type=iframe, allUsers=false, accessPerm=readonly` | 控制面板仅管理员可见 |
| X-Frame-Options **不得**设 DENY | FNOS 桌面经 iframe 嵌入，DENY 导致空白页（真机验证）；防护由 Token 鉴权把守 |
| 首次 Token：应用内"初始化"页生成（明文仅返回一次） | 用户明确反馈：不应到数据目录/SSH 找 `initial-token.txt`；服务端不落盘明文 |
| `status`=`domstate` 实查 | 不信任缓存状态；starting/shutting_down 为"pending 操作 + domstate"推导，超时 2 分钟失效 |
| 关机等待默认 120s，最长 300s（`?timeout=`） | iPhone 快捷指令友好；超时返回 `success:false` 且**不断电** |
| 日志 JSON Lines，512KB+1 备份 | 上限可控、不记 Token |

## 开发命令

```bash
cd backend
go build ./... && go vet ./... && gofmt -l . && go test ./...

# 本地模拟 FNOS 生命周期（不 root 则 sudoers 步骤预期失败）
./scripts/build-fpk.sh           # 构建 dist/fnos-vm-remote-v<版本>.fpk
./scripts/check-fpk.sh dist/*.fpk <版本>
bash -n fnos-app/cmd/*           # 脚本语法

# 图标重新生成
python3 scripts/gen-icons.py
```

## fnpack / FPK 结构

- fnpack 1.2.3（`~/.local/bin/fnpack`；CI 从 `https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64` 下载，SHA-256 见 `.github/workflows/*.yml`）。
- `fnos-app/` 是打包目录（`fnpack build` 在其中执行）；`fnos-app/app/server/` 为构建产物，不入库。
- `.fpk` = gzip 的 tar：`app.tgz`（server/ui/www）+ `cmd/` + `config/` + `wizard/` + `manifest` + 图标。

## 测试分层

1. Go 单元/集成测试：`internal/vmmgmt`、`internal/config`、`internal/httpx`、`cmd/vmhelper`（假 virsh）。覆盖：鉴权、幂等、关机超时不断电、Token 轮换删初始文件、日志不含 Token、回滚逻辑由 shell 层验证。
2. 本地模拟生命周期：stub `TRIM_*` 环境变量 + 假 virsh，验证 install/main/config_callback（含 9494→9610 占用回滚）/uninstall。
3. **必须真机验证**（见 docs/install-uninstall.md）：FNOS 版本兼容、root 生命周期实际执行用户、sudoers 生效、入口端口渲染（已在真机修复）、Windows 180s 正常关机、NFC 快捷指令。

## 版本策略

- SemVer；当前先 `0.1.0` 迭代；正式发布手动升到 `1.0.0` 并同步 `fnos-app/manifest`。
- 推送 `v*` Tag → `.github/workflows/release.yml` 自动校验（tag==manifest、结构、版本）→ GitHub Release + FPK 附件。
- **不得**在未真机验证前发布 `1.0.0`。

## 未验证项（截至 2026-11-16）

- FNOS 真实设备：安装/升级/卸载、sudoers 生效——安装已真机验证（0.1.2 修复入口渲染）；sudoers/升级/卸载仍未验证
- 已实测：FNOS 不替换 `${wizard_port}`，iframe 嵌入下 `X-Frame-Options: DENY` 导致空白页，生命周期脚本 root 执行
- libvirt 9.0.0 实际 `domstate` 输出值（running/shut off/in shutdown 假设基于 libvirt 文档）
- fnOS 生命周期脚本实际执行用户（是否 root）——真机表现为 root
- Windows 正常关机耗时分布（默认超时 120s 为估计值）

## 知识库同步

- 本文档、README、docs/ 需随改动同步；版本号唯一来源为 `fnos-app/manifest`。
