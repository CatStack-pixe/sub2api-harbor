---
name: sub2api-heartbeat-key
description: Provide and validate the key-checker Vault response consumed by Sub2API workers. Use for the HTTPS Vault endpoint, session-key lookup, provider credentials, fingerprint matching, and key-serving troubleshooting.
---

# Sub2API Heartbeat Key Vault

这个 skill 只处理第二次握手：Sub2API worker 使用 session_key 向 Vault 取回与 Heartbeat fingerprint 匹配的真实 key。

## 固定接口

```http
GET https://vault.catpithos.top/api/hb/keys?session_key=SESSION_KEY
```

- 当前 Vault 域名：`vault.catpithos.top`。
- Vault 与 Claw 位于同一台机器，源站公网地址为 `154.64.255.80`；Caddy 在 HTTPS 443 接收 `vault.catpithos.top`，并反代到 `openclaw-vault:44444` sidecar。Vault sidecar 不发布宿主端口。
- Sub2API 配置值：`https://vault.catpithos.top/api/hb/keys`。
- 不在配置中预置 `session_key`；worker 会自动追加或覆盖查询参数并保留其他参数。
- Vault 的访问白名单是反向方向，必须只允许 Sub2API worker 的实际出口 IP；它与 Heartbeat 入站白名单（仅 `154.64.255.80`）分开配置。当前目标 worker 主机是 `154.37.212.18`，若经 NAT 则以实际出口 IP 为准；禁止通过重定向暴露到其他地址。
- Claw 的发送 skill 会先原子写入 `/home/node/.openclaw/heartbeat-vault/vault.json`（宿主路径 `/opt/openclaw/state/heartbeat-vault/vault.json`），Vault sidecar 以只读方式加载该文件，再发送 Heartbeat。

## 响应格式

成功必须返回 HTTP 200 和 JSON：

```json
{
  "ok": true,
  "keys": [
    {
      "key": "REAL_API_KEY",
      "provider": "ds",
      "credentials": {
        "base_url": "https://upstream.example/v1"
      }
    }
  ]
}
```

`keys` 可以包含多个候选值；Sub2API 只读取 `ok`、`keys[].key`、`keys[].provider` 和白名单 credentials 字段，不依赖 `total`、`balance` 或 response 中的 session_key。

Provider 与账户运行时的对应关系：

| Vault `provider` | 运行时平台 | 说明 |
| --- | --- | --- |
| `toapis` | OpenAI-compatible | 第二次握手继续使用 `toapis` 作为 wire ID；`openai` 响应不匹配 |
| `kling` | OpenAI-compatible | 第二次握手继续使用 `kling` 作为 wire ID；`openai` 响应不匹配 |
| `openai` | OpenAI-compatible | 没有专用 provider ID 的通用回退；必须提供实际 `credentials.base_url` |

Heartbeat 请求中的 provider、Vault 返回的 provider 和任务中的稳定 provider ID 必须一致。`toapis`、`kling` 虽然使用 OpenAI-compatible 账户运行时，仍按各自 ID 做 fingerprint 匹配；只有通用上游才使用 `openai`。

## 匹配规则

1. 取 Heartbeat 任务保存的 `provider` 和 `fp`，不要让请求方提交 fp 作为 Vault 查询参数。
2. Vault 只返回当前 `SESSION_KEY` 绑定的最小 key 集合。
3. 过滤稳定化后的 provider；候选 key 去除首尾空白后计算 `sha256(key.strip().encode("utf-8")).hexdigest()[:24]`。
4. 只有 fingerprint 与任务的 fp 完全匹配时，worker 才继续创建或恢复账户。

## credentials 白名单

worker 只合并以下字段：

```text
base_url
api_protocol
account_mode
tokenrhythm_cookie
tr_session
tr_csrf
user_agent
header_overrides
```

平台要求：

- TokenRhythm 需要 `tr_session` 与 `tr_csrf`，或完整 `tokenrhythm_cookie`。
- Antigravity API key 需要兼容网关 `base_url`，路径以 `/antigravity` 结尾。
- 自定义供应商应在 `credentials.base_url` 中提供真实上游端点。
- `toapis`、`kling` 和通用 `openai` 上游均应在 `credentials.base_url` 中提供真实的 OpenAI-compatible 端点；可选 `api_protocol: "openai"` 用于明确协议。
- 缺少平台专属字段时，当前条目按凭据校验失败并进入重试。

## 错误处理

以下结果不会创建或启用账户，并进入 worker 重试：

| 结果 | 含义 |
| --- | --- |
| 401/403 | 访问控制或 session_key 无效 |
| 404 | Vault 路径、会话或 key 集合不存在 |
| 5xx/超时 | 源站或上游故障 |
| 3xx | 不允许跟随重定向 |
| 200 但 `ok=false` | 当前会话无可用 key |
| 200 但 provider/fingerprint 不匹配 | 拒绝该候选 key；`toapis`/`kling` 要求 Vault 返回相同 wire ID |
| 响应超过 1 MiB 或 JSON 无效 | 拒绝响应 |

默认最多尝试 5 次，首次退避约 30 秒，最大退避 1800 秒；达到上限后任务为 `failed`。已有账户在失败时保持 disabled/不可调度。

## Vault 反代验收

```text
https://vault.catpithos.top/api/hb/keys
```

没有 session_key 的探测应返回受控的 4xx/业务错误；返回 Cloudflare `521` 表示 DNS 已到达 Claw 的 Cloudflare 入口，但 Claw 源站 443 尚未连通。Claw 上的 Caddy 应使用 `vault.catpithos.top` 的有效证书，并将 `/api/hb/keys` 反代至 `openclaw-vault:44444`；当前若仍为 `521`，先检查 Cloudflare 的 Vault A/AAAA 源站记录和 SSL 模式。

## 保密要求

- 不记录完整 Vault URL（特别是带 session_key 的 URL）、真实 key、Cookie、JWT 或管理员密码。
- 日志只保留状态码、provider、fingerprint、任务 ID 和脱敏错误。
- Vault 只为当前 session 返回最小 key 集合，不向公网暴露完整 key 库。
