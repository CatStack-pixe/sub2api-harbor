# 远程账号安全接入手册

本文面向远程机器的客户端开发者。服务端部署、Cloudflare Access、Caddy、
防火墙和加密密钥环配置见 [REMOTE_INGEST.md](REMOTE_INGEST.md)。可运行的
curl、Go、Python 和 JavaScript 客户端位于
[`examples/remote-ingest/`](examples/remote-ingest/)。

## 1. 接入前准备

管理员需要通过安全渠道分别提供：

- 接入地址：`https://ingest.catpithos.top`
- 该机器独占的 Cloudflare Access Service Token ID 和 Secret
- 一个有效期默认 10 分钟、只能使用一次的注册令牌
- 已存在且状态为 active 的目标分组名称
- 上游平台、HTTPS Base URL 和测试模型

支持的平台为 `openai`、`anthropic`、`gemini`、`grok`、`agnes`、
`deepseek` 和 `nvidia`。首版只接受单个 API Key 账号，不接受 OAuth、代理、
批量账号、任意 credentials 或 extra 字段。

Service Token、注册令牌、Ed25519 私钥、delivery 查询令牌和上游 API Key
都属于敏感凭据。只能放入进程环境变量或机器密钥存储，不能写入 Git、命令行
参数、日志、截图或交接文档。

## 2. 首次注册机器

每台机器只生成一次 Ed25519 密钥对。私钥留在本机密钥存储中，注册时只提交
标准 Base64 编码的 32 字节公钥。

```http
POST /api/v1/remote-ingest/enroll
CF-Access-Client-Id: <service-token-id>
CF-Access-Client-Secret: <service-token-secret>
Content-Type: application/json

{
  "registration_token": "<one-time-registration-token>",
  "machine_name": "worker-shanghai-01",
  "public_key": "<base64-ed25519-public-key>"
}
```

成功响应的 `data` 包含 `client_id`、`public_key_fingerprint` 和
`enrolled_at`。持久保存 `client_id`，并核对公钥指纹。注册令牌一旦成功消费
就不能再次使用；不要在失败后自动生成新身份，应先让管理员核对客户端列表。

Cloudflare 会在通过 Access 策略后向源站签发 JWT。服务端把其中的 Service
Token 身份绑定到本地 `client_id`，因此不能把一个 `client_id` 搬到使用另一
枚 Service Token 的机器。

## 3. 每次提交前获取握手

每次提交账号都必须获取新的 challenge。nonce 默认 60 秒过期，而且只能成功
消费一次。

```http
POST /api/v1/remote-ingest/handshakes
CF-Access-Client-Id: <service-token-id>
CF-Access-Client-Secret: <service-token-secret>
Content-Type: application/json

{"client_id":"<client-id>"}
```

保存响应 `data.challenge_id`、`data.nonce` 和 `data.expires_at`。本机时钟必须
通过 NTP 保持准确；提交使用 Unix 秒级时间戳，超出允许偏差会被拒绝。

## 4. 构造并签名账号请求

请求体只允许以下字段：

```json
{
  "external_id": "remote-account-001",
  "name": "upstream-account-001",
  "platform": "openai",
  "base_url": "https://api.openai.com/v1",
  "api_key": "<upstream-api-key>",
  "group_name": "openai-default",
  "test_model": "gpt-4.1-mini",
  "concurrency": 1,
  "priority": 0,
  "rate_multiplier": 1
}
```

`external_id` 在同一客户端内必须稳定且唯一。`group_name` 去除首尾空格后精确
匹配现有 active 分组；分组平台必须一致，并且不能启用 `RequireOAuthOnly`。
Base URL 必须为 HTTPS，可以带路径，但不能含 userinfo、query 或 fragment；
解析到 loopback、link-local、私网、保留地址或 IPv4 转换型 IPv6 地址时会被
拒绝。整个请求体不能超过 16 KiB。

先把最终 JSON 编码为 UTF-8 字节。计算这些**确切字节**的 SHA-256 小写十六进制
值，再用 Ed25519 私钥签名以下 LF 分隔内容，末尾不追加换行：

```text
sub2api-remote-ingest-v1
<client_id>
<challenge_id>
<nonce>
<unix_timestamp_seconds>
<lowercase_sha256_of_exact_request_body>
```

签名后不能重新序列化 JSON，否则字段顺序或空格变化都会使摘要不一致。把原始
签名字节用标准 Base64 编码后提交：

```http
POST /api/v1/remote-ingest/accounts
CF-Access-Client-Id: <service-token-id>
CF-Access-Client-Secret: <service-token-secret>
X-Remote-Client-Id: <client-id>
X-Remote-Challenge-Id: <challenge-id>
X-Remote-Timestamp: <unix-seconds>
X-Remote-Signature: <base64-ed25519-signature>
Content-Type: application/json

<the exact signed JSON bytes>
```

成功返回 `202 Accepted`。响应 `data` 包含 `delivery_id`、`query_token` 和
初始 `status`，不会回显 API Key。查询令牌只在创建或相同内容的幂等重试响应
中返回，应立即写入本机密钥存储。

## 5. 幂等重试规则

唯一键为 `(client_id, external_id)`：

- 相同 `external_id` 且请求体字节完全相同：返回原 delivery，不创建第二个账号。
- 相同 `external_id` 但请求体有任何变化：返回 `409 Conflict`。
- challenge 无论成功还是重试都不能复用；每次 HTTP 重试必须先获取新 challenge。

因此客户端应先生成并持久化最终请求体，再在每次发送前只重新生成 challenge、
时间戳和签名。不要在网络超时后改变 JSON 格式、字段顺序或 `external_id`。

## 6. 查询上线状态

```http
GET /api/v1/remote-ingest/deliveries/<delivery-id>
CF-Access-Client-Id: <service-token-id>
CF-Access-Client-Secret: <service-token-secret>
Authorization: Bearer <delivery-query-token>
```

状态含义：

| 状态 | 含义 | 客户端动作 |
| --- | --- | --- |
| `pending` | 已原子落库，等待 worker | 退避后继续查询 |
| `probing` | 正在测试上游连接 | 退避后继续查询 |
| `active` | 探测成功，账号已加入调度 | 完成 |
| `probe_failed` | 探测失败，账号保持 inactive | 查看脱敏原因并联系管理员重试 |

建议使用 2、4、8、15 秒的退避间隔，最长等待时间由业务决定。不要高频轮询。
`masked_error` 只包含通用分类或 HTTP 状态码，不包含上游响应正文。管理员可在
“远程接入”页面重试失败 delivery；客户端不能自行激活账号。

## 7. 常见失败

| HTTP 状态 | 常见原因 |
| --- | --- |
| `400` | 字段、平台、分组、Base URL 或请求体格式不合法 |
| `401` | Access JWT、注册令牌、challenge、时间戳、签名或查询令牌无效 |
| `403` | 客户端已吊销，或 Access 身份与客户端不匹配 |
| `409` | `external_id` 已存在但请求体不同，或公钥/Access 身份已注册 |
| `413` | 请求体超过 16 KiB |

排障时只记录 HTTP 状态、错误 code、`client_id`、`delivery_id` 和公钥指纹。
禁止记录请求体、Authorization、Access Secret、签名原文或任何 API Key。

## 8. 上线检查清单

- 一台机器只使用一枚 Cloudflare Service Token 和一个持久 Ed25519 身份。
- 私钥和所有令牌由机器密钥存储保护，进程日志中没有敏感值。
- 本机时间同步正常，能为每次提交创建新 challenge。
- 客户端签名并发送同一份原始 JSON 字节。
- `external_id` 和原始请求体在不确定结果的重试中保持不变。
- 只在 `active` 后认为账号上线；`probe_failed` 交由管理员处理。
- 已用对应语言的参考客户端在预发布环境完成一次真实接入演练。
