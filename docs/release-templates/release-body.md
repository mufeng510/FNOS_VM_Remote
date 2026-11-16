## 安装与升级

1. 下载本 Release 附件 `fnos-vm-remote-v__VERSION__.fpk`
2. FNOS 应用中心 → 手动安装 → 上传该文件
3. 安装向导中确认服务端口（默认 8321）
4. 安装完成后：桌面/应用中心打开「FNOS VM 遥控」（新标签页打开），按页面提示生成初始 Token 并登录

从 `0.x` 升级：覆盖安装即可，端口/Token/日志会保留。

## 系统要求

- 飞牛 FNOS（裸机，Debian 12 基础），x86_64
- libvirt / virsh 可用（系统虚拟化场景默认满足）
- 目标虚拟机：`3f1jeea2`（UUID `1ce21ce8-0bd1-4ab3-a9c0-064abf81381f`）

## 注意事项

- 本应用不修改虚拟机 autostart（始终保持关闭）
- 正常关机超时**不会**强制断电
- 卸载不会删除虚拟机

## 首次使用

1. 碰开机 NFC 标签 → Windows 开机；碰关机标签 → 正常关机
2. NFC 配置教程见仓库 `docs/nfc-guide.md`
3. API 文档见仓库 `docs/api.md`

## 验证包完整性

```bash
echo "<sha256>  fnos-vm-remote-v__VERSION__.fpk" | sha256sum -c -
```
