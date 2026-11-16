# HTTP API 文档

Base URL：`http://<FNOS-IP>:<端口>/api/v1`（默认端口 8321）

## 通用约定

- 除 `GET /api/v1/health` 外，**所有接口都需要** `Authorization: Bearer <TOKEN>`。
- 成功响应统一为：`{ "success": true, "data": { ... } }`
- 失败响应统一为：`{ "success": false, "error": "<可读错误信息>" }`（部分场景带 `data` 附带当前状态）。
- HTTP 状态码语义：200 成功 / 400 参数非法 / 401 Token 无效 / 404 不存在 / 409 当前状态不允许 / 502 虚拟机控制失败 / 500 服务内部错误。
- API **不接受**任何虚拟机标识参数，不提供 destroy/reboot。

## 1. 健康检查（无需鉴权）

```http
GET /api/v1/health
```

```json
{ "success": true, "data": { "app": "fnos-vm-remote", "status": "ok", "version": "0.1.0" } }
```

## 2. 查询虚拟机状态

```http
GET /api/v1/vm/status
Authorization: Bearer <TOKEN>
```

```json
{
  "success": true,
  "data": {
    "name": "3f1jeea2",
    "uuid": "1ce21ce8-0bd1-4ab3-a9c0-064abf81381f",
    "state": "shut off",
    "raw_state": "shut off",
    "updated_at": "2026-11-16T10:00:00Z"
  }
}
```

`state` 取值：`running` | `shut off` | `starting`（启动中，推导） | `shutting_down`（关机中，推导） | `paused` | `unknown`。
状态以 libvirt `domstate` 实时查询结果为准；`raw_state` 为原始输出。

## 3. 开机

```http
POST /api/v1/vm/start
Authorization: Bearer <TOKEN>
```

```json
{
  "success": true,
  "data": {
    "status": { "name": "3f1jeea2", "uuid": "...", "state": "running", "raw_state": "running", "updated_at": "..." },
    "started": true,
    "already": false,
    "pending": false,
    "message": "开机指令已执行"
  }
}
```

幂等：已运行 → `already: true`；启动中 → `pending: true`；`paused`/无法识别 → HTTP 409。

## 4. 正常关机

```http
POST /api/v1/vm/shutdown[?timeout=120]
Authorization: Bearer <TOKEN>
```

- `timeout`（秒，可选，5—300，默认 120）：等待 Windows 关机的最长时间。
- 已关闭 → `already: true`；正在关机 → `pending: true`；已运行 → 发送关机指令并等待。
- 等到 `shut off` → `success: true`。
- 超时 → **`success: false` + `timed_out: true`**，明确提示未执行强制断电：

```json
{
  "success": false,
  "message": "已发送正常关机指令，但 Windows 未在 120 秒内关闭。未执行强制断电，请检查 Windows 是否正常响应后重试",
  "data": { "timed_out": true, "sent": true, "status": { "state": "running", "...": "..." } }
}
```

## 5. 查询服务配置（不含敏感数据）

```http
GET /api/v1/config
Authorization: Bearer <TOKEN>
```

返回 `listen_port`、`version`、`api_base`、VM 名称/UUID（常量）。

## 6. Token 管理

### 6.1 Token 元数据（不含哈希/明文）

```http
GET /api/v1/auth/token/meta
Authorization: Bearer <TOKEN>
```

```json
{ "success": true, "data": { "created_at": "...", "updated_at": "..." } }
```

### 6.2 重新生成 Token

```http
POST /api/v1/auth/token/rotate
Authorization: Bearer <TOKEN>
```

```json
{
  "success": true,
  "data": {
    "new_token": "仅此一次返回的完整 Token",
    "message": "Token 已重新生成，此明文仅显示这一次，请立即复制并妥善保存"
  }
}
```

旧 Token 立即失效；安装时生成的 `initial-token.txt` 会被自动删除。

## 7. 日志

```http
GET /api/v1/logs[?limit=200]      # 默认 200 条，新→旧
DELETE /api/v1/logs               # 清空日志
Authorization: Bearer <TOKEN>
```

单条日志：`{ "ts", "action", "result", "client_ip", ... }`，不记录 Token（含失败的）。

## 快捷指令示例

```
POST http://192.168.1.10:8321/api/v1/vm/start
Authorization: Bearer 粘贴你的Token
```

参见 [nfc-guide.md](nfc-guide.md)。
