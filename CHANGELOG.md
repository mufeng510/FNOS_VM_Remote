# Changelog

## [0.1.1] - 2026-11-16

真机安装修复：

- 修正安装后 helper/server 路径：FPK 中 `app/` 的内容解压到应用 `target` 根目录，
  `app/server/x` 对应 `$TRIM_APPDEST/server/x`（而非 `.../app/server/x`）——安装时不再报
  "helper 二进制缺失或不可执行"
- 生命周期脚本相互调用改用 `$(dirname $0)/main`（`cmd/` 不在 `TRIM_APPDEST` 下）

## [0.1.0] - 2026-11-16

首个迭代版本（未在真实 FNOS 真机验证前，不会发布为 1.0.0）。

新增：

- FNOS 原生 FPK 应用（fnpack 打包）：Web 管理界面 + HTTP API
- 虚拟机状态查询（running / shut off / 启动中 / 关机中 / paused / unknown，libvirt 实时查询）
- 开机 / 正常关机（幂等；关机超时不强制断电）
- Token 鉴权（安装时随机生成、仅存 SHA-256、可重新生成；日志不记录 Token）
- 服务端口自定义（安装向导 + FNOS「设置」页修改；校验/占用检测/自动重启/健康检查/失败回滚）
- 受限 helper（sudoers 三条命令白名单 + UUID 校验）；服务以包用户降权运行
- 操作日志（JSON Lines，轮转上限 512KB）
- GitHub Actions：PR 检查（gofmt/vet/单测/FPK 结构校验）、main 自动构建、Tag 触发 Release（附件自动校验）
- iPhone NFC 快捷指令接入说明（docs/nfc-guide.md）
