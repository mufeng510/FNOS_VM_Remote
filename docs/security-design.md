# 安全设计说明

本文档描述 FNOS VM Remote 的权限模型与威胁缓解，对应任务书 §5。

## 1. 权限模型总览

```
浏览器 / iPhone 快捷指令
  │  HTTP + Bearer Token（局域网明文 HTTP，见 §6）
  ▼
fnos-vm-remote-server            ← 以应用包用户（非 root）运行
  │  sudo -n（NOPASSWD，sudoers 白名单仅 3 条命令）
  ▼
fnos-vm-remote-helper            ← 以 root 执行（sudo），固定程序
  │  exec 固定路径 /usr/bin/virsh -c qemu:///system <固定参数>
  ▼
libvirtd（qemu:///system） → 仅 VM 3f1jeea2
```

### 1.1 Web 服务不以 root 运行

`cmd/main start` 通过 `runuser -u fnos-vm-remote` 启动服务；FNOS 应用中心也无法以 root 拉起 Web 进程。服务即使被远程利用，能做的也仅限：

- 读写 `$TRIM_PKGVAR`（配置/日志）
- 以 `sudo -n` 调用 3 个固定 helper 子命令

### 1.2 受限 helper（最小权限的核心）

`fnos-vm-remote-helper` 是唯一获得提权的二进制，设计约束：

- 子命令白名单：`status` / `start` / `shutdown` / `version`；其他全部拒绝（含 `destroy`、`reboot`、`undefine`、任意带参数形式）
- **VM 名称与 UUID 硬编码**，不接受任何外部标识输入
- 执行 `start`/`shutdown` 前先 `virsh -c qemu:///system domuuid 3f1jeea2`，与硬编码 UUID 不一致即拒绝（防止同名 VM 被误控）
- exec 固定路径 `/usr/bin/virsh`，无 Shell、无环境变量拼接、无 `os/exec` 之外的系统调用面
- 单次 virsh 调用 15 秒超时

### 1.3 sudoers 精确授权

`/etc/sudoers.d/fnos-vm-remote`（安装时写入，0440，visudo 校验）：

```
fnos-vm-remote ALL=(root) NOPASSWD: /var/apps/fnos-vm-remote/target/server/fnos-vm-remote-helper status, /var/apps/fnos-vm-remote/target/server/fnos-vm-remote-helper start, /var/apps/fnos-vm-remote/target/server/fnos-vm-remote-helper shutdown
```

- 仅该用户、仅该绝对路径、仅三个固定参数组合
- 升级时（upgrade_callback）重新断言同一文件；卸载时删除
- 即使服务进程被攻破，攻击者也只能触发固定 VM 的 3 个操作，无法执行任意命令

### 1.4 生命周期脚本为何是 root

FNOS 官方文档允许"生命周期脚本确实需要执行特权准备任务时使用 Root 模式，例如准备系统集成"。本应用特权脚本仅：

- 检查 libvirt / virsh / sudo 是否存在（install_init）
- 写 `/etc/sudoers.d/fnos-vm-remote`（install_callback / upgrade_callback）
- chown 应用数据目录给包用户（install/upgrade callback）
- 停止服务（uninstall_init）

长期运行的 Web 服务**永不**以 root 运行。不采用 `join-groups: libvirt` 方案（组权限=任意 VM 完整控制），也不采用 `run-as=root` 全包模式。

## 2. Token 鉴权

- 安装时 `install_callback` 生成 48 字符 base64url 随机 Token（36 字节 CSPRNG）
- 仅 `sha256(token)` 持久化于 `auth.json`（0600）；校验使用 constant-time 比较
- 安装时明文另写 `initial-token.txt`（0600）一次性供首次登录；登录后"重新生成 Token"会删除该文件
- API 全部需 Bearer；含健康检查以外的所有端点
- Web UI 不在普通页面展示完整 Token；仅"重新生成"瞬间返回一次明文
- 日志仅记录动作/结果/IP，**不记录任何形式的 Token**（含被拒绝的）

## 3. API 安全

- API **不接受**虚拟机名称/UUID/命令等任何标识参数（篡改 query/body 不改变控制目标）
- 不提供 destroy/reboot/undefine 等破坏性接口
- 幂等：重复开机/关机返回当前状态，不重复执行
- 关机超时（默认 120s，最长 300s）后**不强制断电**，返回 `timed_out` 明确错误
- 安全响应头：`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`、严格 CSP（仅 `'self'`）
- 统一错误格式；内部错误不回显堆栈/路径给客户端

## 4. 存储安全

- `$TRIM_PKGVAR` 权限 0750，包用户所有；`auth.json`/`config.json`/`initial-token.txt` 0600
- 日志 `logs/fnos-vm-remote.log`：JSON Lines，单文件 512KB 轮转 + 1 个 `.1` 备份

## 5. 网络边界

- 默认仅监听局域网（0.0.0.0:8321，无对外端口映射、无公网暴露、无防火墙放行动作）
- 桌面入口 `allUsers=false`（仅管理员可见）
- 无跨域放开（CSP `default-src 'self'`，无 CORS 头）
- 仅家庭 LAN 设计；请勿在路由器做端口映射

## 6. 已知限制与说明

1. **明文 HTTP**：Token 在自己的局域网内以明文传输（NAS 无证书场景的常规取舍）。局域网内不受信任设备/访客可嗅探到 Token。建议家庭网络不开放访客访问，或改用 FNOS 自带 HTTPS 反代并接受证书配置成本。
2. **初始 Token 窗口期**：安装完成到首次登录期间，能访问应用数据目录的用户可读取 `initial-token.txt`。建议安装后立即登录并重新生成 Token。
3. **CSRF**：API 使用 Bearer Header（非 Cookie），浏览器不会自动附带，故无 CSRF 风险；UI Token 存 `sessionStorage`。
4. **本地暴力破解**：无速率限制（个人局域网场景）。失败尝试会写入日志（含来源 IP），不影响可用性，但请定期检查异常日志。
5. **helper 提权面**：如果 FNOS 安装流程允许用户以非 root 安装，本应用会直接失败（明确报错），而不是降级为不安全模式。

## 7. 威胁-缓解对照

| 威胁 | 缓解 |
|---|---|
| 未授权控制 VM | Bearer Token；helper 白名单 + UUID 校验 |
| 任意命令执行 | helper 无 Shell、无任意参数；API 无 VM 标识输入 |
| Token 泄露 | 仅存 SHA-256；日志不记；UI 掩码；0600 |
| 中间人（LAN 嗅探） | 见 §6.1 明文 HTTP 说明 |
| 旧 Token 复用 | rotate 即时失效旧 Token |
| 日志敏感信息 | 不记录 Token；仅动作/结果/IP |
| 提权后攻破面 | 服务非 root；helper 固定功能；卸载删除 sudoers |
| VM 被意外销毁 | 全链路无 destroy；关机超时不断电 |
| 配置被篡改破坏入口 | config_init/config_callback 两阶段校验 + 原子写 + 回滚 |
