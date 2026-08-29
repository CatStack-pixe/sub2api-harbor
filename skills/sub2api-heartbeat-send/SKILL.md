---
name: sub2api-heartbeat-send
description: Send Sub2API Heartbeat reports from the key-checker to the production intake at heartbeat.catpithos.top. Use for building, validating, scheduling, or troubleshooting the outbound fingerprint-only POST.
---

# Sub2API Heartbeat Send

这个 skill 处理 Claw 上的完整上线顺序：先把本地真实 key 原子写入 Claw Vault，再向 Sub2API 发送只含 fingerprint 的 Heartbeat。真实 API key 不进入 Heartbeat 请求体。

## 固定接口

```http
POST https://heartbeat.catpithos.top/api/heartbeat
Content-Type: application/json
```

- 允许的来源 IP：`154.64.255.80`（唯一允许的 Heartbeat 入站来源）。
- 不使用 Sub2API 管理员 token、Authorization 或 Cookie。
- 建议每 30 至 60 秒发送一次；同一实例成功请求间隔至少 10 秒。
- 当前公网域名只开放精确路径 `/api/heartbeat`，其他路径返回 404，同路径非 allowlist 来源返回 403。

## Claw 本地 Vault 顺序

1. Claw 在本机生成或复用 32 位小写十六进制 `session_key`。
2. 脚本先原子更新 `~/.openclaw/heartbeat-vault/vault.json`，目录权限 0700、文件权限 0600；映射按 `session_key` 保存真实 key 和允许的 credentials 字段。
3. Vault 服务只读挂载这份文件并监听容器网络内的 `44444`；Caddy 对外提供 `https://vault.catpithos.top/api/hb/keys`，仅允许 Sub2API worker 的实际出口 IP `154.37.212.18`。
4. Vault 写入成功后才发送 `POST https://heartbeat.catpithos.top/api/heartbeat`。这样 Sub2API 收到 `202` 后立即取 Vault 时不会出现先入队、后写 Vault 的竞态。

不要使用 `--skip-vault-store` 进行真实上线；它只适合验证 Heartbeat 入站本身。

## 请求体

```json
{
  "session_key": "32_HEX_SESSION_KEY",
  "ts": 1786000000,
  "keys": [
    {
      "fp": "24_HEX_FINGERPRINT",
      "provider": "ds",
      "balance": 0,
      "checked_at": "2026-08-26T08:00:00Z",
      "group_id": 12
    }
  ]
}
```

约束：

- 请求体最大 1 MiB；`keys` 必须有 1 到 100 项。
- `session_key` 必须是 16 字节随机值的 32 位小写十六进制字符串。源端应持久化它并按密码保护。
- `session_key` 与 `ts` 是两个字段：不要把时间戳拼进 `session_key`；Sub2API 单独校验 `ts` 的时间窗口，并在队列中加密保存 `session_key`。
- `ts` 是 Unix 秒，和服务端当前时间相差不得超过 15 分钟。
- `fp` 是真实 key 去除首尾空白后 SHA-256 的前 24 位小写十六进制：`sha256(key.strip().encode("utf-8")).hexdigest()[:24]`。
- 新客户端使用带 `Z` 或明确 offset 的 RFC3339/RFC3339Nano `checked_at`；旧 `balance_checked_at` 仅在没有 `checked_at` 时兼容。
- `provider` 使用稳定平台 ID；当前路由支持 DeepSeek、Anthropic、OpenAI、Gemini、Antigravity、Grok、Agnes、NVIDIA、TokenRhythm、Kimi、Zhipu、ChatAnywhere、GLM、ModelScope、DashScope、MiniMax、Volcengine。
- `toapis` 和 `kling` 是已登记的 OpenAI-compatible checker ID：发送时在 Heartbeat 的 `keys[].provider` 使用原 ID，Vault 条目也必须返回相同 ID；两者的账户运行时映射到 OpenAI，但 wire ID 不改写。
- 来源没有专用 provider ID、且上游确实兼容 OpenAI 协议时，使用 `openai` 作为泛化回退，并把真实上游地址放在 Vault 条目的 `credentials.base_url`；未知 provider 保持校验失败，不进入 `ds` 路由。
- 可选 `group_id` 用于明确指定账户组；省略时由 worker 按 `targets`、平台兼容性和默认组选择。

OpenAI-compatible provider 的最小元数据示例（真实 key 仍只写入 Vault）：

```json
{
  "fp": "24_HEX_FINGERPRINT",
  "provider": "toapis",
  "balance": 0,
  "checked_at": "2026-08-26T08:00:00Z"
}
```

`kling` 使用相同格式；通用兼容上游使用 `provider: "openai"`。`credentials.base_url` 不放进 Heartbeat 请求体，而是随同一 `session_key` 保存在 Vault，供 worker 第二次握手读取。

## 发送示例

```bash
curl -fsS -X POST https://heartbeat.catpithos.top/api/heartbeat \
  -H "Content-Type: application/json" \
  -d @heartbeat.json
```

`heartbeat.json` 只保存 fingerprint 和元数据，不保存真实 key。发送端也可以从受控环境变量读取 key，在内存中计算 fp 后丢弃明文：

```python
import datetime
import hashlib
import json
import os
import secrets
import urllib.request

endpoint = "https://heartbeat.catpithos.top/api/heartbeat"
keys = [item.strip() for item in os.environ["DEEPSEEK_KEYS"].split(",") if item.strip()]
session_key = os.environ.get("HEARTBEAT_SESSION_KEY") or secrets.token_hex(16)
now = datetime.datetime.now(datetime.timezone.utc)
payload = {
    "session_key": session_key,
    "ts": int(now.timestamp()),
    "keys": [
        {
            "fp": hashlib.sha256(key.encode("utf-8")).hexdigest()[:24],
            "provider": "ds",
            "balance": 0.0,
            "checked_at": now.isoformat().replace("+00:00", "Z"),
        }
        for key in keys
    ],
}
body = json.dumps(payload).encode("utf-8")
req = urllib.request.Request(endpoint, data=body, headers={"Content-Type": "application/json", "User-Agent": "sub2api-heartbeat/1.0"}, method="POST")
with urllib.request.urlopen(req, timeout=20) as response:
    print(response.status, response.read(4096).decode("utf-8", "replace"))
```

生产日志只记录 HTTP 状态、accepted 数量和 fingerprint 数量；不要打印 payload、key、session_key 或完整响应。

## 响应和重试

成功：

```json
{"accepted": 1}
```

状态含义：

| 状态 | 处理 |
| --- | --- |
| 202 | 任务已入队；后续异步创建/恢复账户 |
| 400 | 修正 JSON、字段、数量、provider（包括 `toapis`/`kling` 是否拼写正确）、fp、时间或 session_key |
| 403 | 检查来源出口 IP 是否为 154.64.255.80，以及 Caddy/应用 allowlist |
| 404 | 检查 Heartbeat 开关和精确路径 |
| 429 | 降低发送频率，避免同一实例 10 秒内重复成功提交 |
| 503 | 检查 Sub2API 队列、PostgreSQL、Redis 和应用状态 |

只有 `2xx` 视为本次发送成功；失败批次保留待发送 fingerprint，按退避策略重试。服务端 worker 会在目标代理池中按地区轮换多个已探测代理，并排除上一轮失败代理；达到配置的多次尝试上限后才暂停该任务。`202` 不代表账号已创建或已可调度。

## 安全边界

- Heartbeat 线路只传 fingerprint，不传真实 API key。
- `session_key` 是后续 Vault 查询凭据，必须放在密钥存储或权限为 0600 的文件中。
- 不要把管理员密码、JWT、Cookie、真实 key 或完整 session URL 写入聊天和普通日志。

## Claw 调用脚本

脚本位于 `scripts/heartbeat_send.py`，Claw 端可直接运行：

```bash
export DEEPSEEK_KEYS='KEY_A
KEY_B'
python3 scripts/heartbeat_send.py \
  --session-file ~/.cache/sub2api/heartbeat-session.key \
  --vault-file ~/.openclaw/heartbeat-vault/vault.json
```

脚本会在 Claw 本地生成 32 位十六进制 `session_key`，先写 Vault，再把 `ts` 作为独立的 Unix 时间戳发送；Heartbeat 只提交 fingerprint 元数据。`--session-file` 和 `--vault-file` 的父目录不存在时会自动以 0700 创建，文件权限为 0600。需要在同一轮验证 Vault 时追加 `--fetch-vault`，脚本会复用刚生成的 `session_key`，且只输出状态码、accepted 数和匹配数。测试网络可传 `--proxy http://127.0.0.1:7890`；生产 Claw 若没有本机代理则省略该参数。
