# Changelog

## [0.2.1] - 2026-11-16

桌面入口改为 `url` 类型：

- 端口型应用在 FNOS 桌面的 iframe 无法正常嵌入（同机实测：Lucky、trim.photos 均为
  `type: url`；iframe 仅用于同源统一网关应用）
- 点击应用图标改为在浏览器新标签页打开应用（NFC 快捷指令与 HTTP API 不受影响）

## [0.2.0] - 2026-11-16

首次初始化改为应用内流程；修复 iframe 空白页：

- 桌面经 iframe 嵌入本应用，移除 `X-Frame-Options: DENY` 响应头（真机实测 DENY 导致空白页）
- 首次安装不再写 `initial-token.txt`；改为首次打开应用时「初始化」页由服务端生成 Token
  （明文仅返回一次，`/api/v1/setup`，未初始化时唯一可用端点；全部接口 403 直到完成初始化）
- 前端：新增初始化页（生成 → 展示 → 复制 → 进入）；未初始化自动进入初始化页
- `app.pid` 记录真正的 server pid（runuser 场景），`stop` 直接 TERM 服务进程
- 端口变更/升级恢复/回滚均同步恢复桌面入口端口

## [0.1.2] - 2026-11-16

真机入口修复：

- 桌面入口端口由生命周期脚本直接渲染（`render_entry`），不再依赖 FNOS 替换 `${wizard_port}`
  （真机实测 FNOS 不做该替换，导致入口指向无效端口、页面提示"拒绝连接"）
- `cmd/main` 记录真正的 server pid（runuser 场景 pgrep 子进程），`stop` 可直接 TERM 服务
- 配置端口变更/升级后均重新渲染入口；回滚路径同步恢复旧入口端口

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
