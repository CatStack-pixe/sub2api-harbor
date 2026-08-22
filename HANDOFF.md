# Handoff

## 2026-08-22 TokenRhythm sess and referral resolver (in progress)

- Work is isolated in `E:\文件\Python Script\sub2api\sub2api-tokenrhythm-sess` on branch `feat/tokenrhythm-session-referral`, based on current `origin/main`. The original dirty `sub2api-harbor` worktree was not modified.
- Added an admin-only `POST /api/v1/admin/tokenrhythm/session/resolve` flow. It accepts a `sess_...` value plus an optional existing proxy ID, sends the credential only to the fixed TokenRhythm referral endpoint with redirects disabled and a 15-second timeout, captures the provider `tr_csrf`, and returns the minimal account Cookie, referral code, official `/i/{code}` link, and three allowlisted status booleans. It does not create an account or persist the submitted sess.
- Added a reusable TokenRhythm session resolver control to both create-account and edit-account forms. A successful resolve fills the existing Cookie field and displays the referral link/code; the raw sess is cleared and is never included in account create/update payloads.
- Local verification completed: frontend typecheck, full ESLint, frontend build, the new component tests, existing TokenRhythm account-selection source tests, and `git diff --check` passed. The resolver route now has an explicit audit action and its credential-bearing request body is omitted from persistent audit logs, covered by a middleware regression test. Focused Go tests remain locally unavailable because this Windows environment has no Go toolchain; GitHub CI is the authoritative backend compile/test gate.
- Version target is `0.1.176-nvidia.21-tokenrhythm-1`; the working tree is still uncommitted. Remaining before release: commit with an English Conventional Commit message; push, create and label the PR with `gh`, review it, squash-merge it, wait for the GitHub release/GHCR artifact, back up production, deploy only the new immutable `sub2api` image, and verify health plus the new endpoint. No production service has been changed or restarted.
- No sess, Cookie, API key, referral code/link, proxy credential, or administrator credential is recorded in this document.

## 2026-08-18 TokenRhythm DS Balance-Gated Scheduling

- Scope: DS group `18` (`deepseek-025`) contains 19 TokenRhythm API-key accounts. User `3933240147@qq.com` is `user_id=17`; the previous 72-hour sample had 386 operations, 68 final errors, and 28 transport-related errors. Account `6202` is already in `error` with `Payment required (402): 余额不足`.
- Code: TokenRhythm is now a supported upstream billing-probe identity. Its official `usage-summary` balance is stored as a sanitized snapshot; fresh positive `available_balance_cny` is required for scheduling when probing is enabled. New TokenRhythm accounts default to probing enabled, the advanced scheduler cache preserves both the enable flag and sanitized balance snapshot, and the initial TopK compatibility filter applies the same gate.
- GitHub: PR `#69` (`feat(scheduling): gate TokenRhythm accounts by balance probes`) is labeled `bug` and uses the `fix/deepseek-stream-resilience-main` branch. An initial lint run exposed formatting/test issues; fixes are pushed in commit `68c8c0f14`, and the replacement CI run is the authoritative build/test gate. Deployment must use the GitHub release workflow and GHCR image, never a local build.
- Production backup: completed before any deployment at `/opt/sub2api/backups/pre-release-20260818T152946Z` on the `/dev/sdb` backup disk. It contains compose/env/config copies, Docker inspection, PostgreSQL custom-format dump, optional Redis RDB, and `SHA256SUMS` (134M).
- Production deployment status: still running `ghcr.io/catstack-pixe/sub2api:0.1.176-nvidia.13`; no container has been restarted. PostgreSQL, Redis, and Mihomo must remain untouched; only `sub2api` may be recreated after PR squash-merge and successful GitHub release.
- Remaining gap: proxy selection still does not rank by measured CN latency, verify TokenRhythm balance before assignment, or enforce distributed proxy-IP diversity. Existing proxy latency probing is admin/display-only; implement that separately and do not claim it is fixed by this change.

## 2026-08-18 DeepSeek Long-Stream Disconnect Hardening

- Root cause: raw Chat Completions SSE forwarding discarded scanner errors and accepted EOF without `[DONE]`; Responses-to-Chat fallback keepalive was limited to NVIDIA; proxy stream quarantine only recognized `PlatformOpenAI` accounts.
- Code changes: raw Chat forwarding now reads asynchronously while emitting configured `gateway.stream_keepalive_interval` SSE comments, distinguishes clean `[DONE]`, upstream read failure/missing terminal event, and client disconnect drain, records/clears proxy stream failures, and emits OpenAI-compatible stream error termination after business output. Responses fallback keepalive now covers NVIDIA, DeepSeek, and TokenRhythm; committed fallback streams terminate with one `response.failed` event on upstream read failure or missing `[DONE]`. Stream quarantine now applies to all OpenAI-compatible HTTP accounts, including DeepSeek and TokenRhythm, without changing thresholds, windows, TTLs, or no-proxy behavior.
- Local checks: `git diff --check` passed. Go/gofmt are unavailable in this Windows environment; authoritative Go generation, formatting, unit/integration tests, lint, and security checks must run in GitHub Actions.
- Controlled production verification against `https://api.catpithos.top` used a short DeepSeek Flash prompt sequentially through both endpoints. Chat request ID `f35f116c-4608-4fca-b57d-5833ed785fb2`: HTTP `200`, headers `4.29s`, first token `4.37s`, completed `11.52s`, `[DONE]` present, no `response.failed`. Responses request ID `7109c52a-06c5-4b55-a23d-96b324674f5a`: HTTP `200`, headers `18.17s`, first token `18.17s`, completed `24.11s`, `[DONE]` present, no `response.failed`. No production container or data service was restarted or modified.
- The temporary production Bearer credential supplied for this verification is not recorded here and should be rotated after use.

## Current State

- Added the ChatAnywhere account and group platform with API-key-only authentication, official China/Global endpoint selection (`https://api.chatanywhere.tech/v1` and `https://api.chatanywhere.org/v1`), native OpenAI Chat Completions/Responses routing, and native Anthropic Messages routing.
- ChatAnywhere `/v1/messages/count_tokens` uses the local Anthropic-compatible estimator because the official documentation does not define a token-counting endpoint; this avoids forwarding to an unsupported URL or with the wrong authentication scheme.
- Added ChatAnywhere model defaults, upstream model synchronization, composite routing, quota/platform validation, URL allowlist hosts, admin account create/edit controls, model whitelist defaults, platform badges/icons, group/channel filters, and dashboard quota labels.
- ChatAnywhere account creation and editing use the existing localized region labels, base URL guidance, and API-key guidance in both supported UI languages.
- Migration `226_add_chatanywhere_platform_constraints.sql` aligns user quota and composite-route database constraints with all currently supported concrete platforms, including ChatAnywhere; the Ent quota validator is aligned as well.
- ChatAnywhere does not enable upstream billing balance probing because the official documentation does not define a stable balance endpoint.
- Added the TokenRhythm account platform with API-key inference through the fixed `https://tokenrhythm.studio/v1` endpoint.
- Admin account create/edit accepts a complete Cookie header, stores only `tr_session` and `tr_csrf`, and exposes presence flags without returning secret values.
- Added the admin-only TokenRhythm balance probe at `/admin/tokenrhythm/accounts/:id/balance`; it sends the stored Cookie only to the official usage endpoint and does not change scheduling state.
- Added OpenAI-compatible Chat Completions routing and native Anthropic Messages/count-tokens passthrough for TokenRhythm.
- Added TokenRhythm to platform/group/quota/model-selection surfaces and account usage UI with bounded, cached balance queries.
- Added the Kimi Open Platform account channel with API-key-only authentication and a controlled region selector for `api.moonshot.cn` and `api.moonshot.ai`.
- Added the read-only Kimi balance probe at `/admin/kimi/accounts/:id/balance`; it returns available, voucher, and cash balances without exposing credentials or changing scheduling state.
- Pool-mode insufficient-balance quarantine and error-log session correlation are already present in the preceding commits.

## Verification

- Frontend `pnpm typecheck`, `pnpm lint:check`, and focused ChatAnywhere-adjacent account/quota/credential tests pass locally (74 assertions); ChatAnywhere account URL helpers cover both OpenAI and Anthropic defaults.
- Backend Go formatting and unit tests could not run because `go`/`gofmt` are unavailable in the local Windows environment; run the repository GitHub Actions CI before release.
- Kimi frontend typecheck, lint, and focused account/platform/quota tests pass locally (54 assertions).
- Full frontend Vitest has two pre-existing transform failures caused by duplicate `getLiveCapability` declarations in the baseline GroupsView tests; unrelated assertions now pass.
- Go is unavailable in the local Windows environment. Run the repository GitHub Actions CI before release to execute Go generation, unit tests, integration tests, and frontend build.

## Release / Deploy

- Branch: create a feature branch from the current merged base before submitting the Kimi change.
- Required release path: English Conventional Commit, push and create a labeled PR with `gh`, review the PR diff, squash-merge, then dispatch the repository release workflow. Deploy the resulting application container only.
- No manual production account-state mutation is required; the next matching upstream insufficient-balance response activates the existing 30-minute pool quarantine.

## 2026-08-16 邮件发送慢与失败诊断

- `fix/email-delivery-reliability` 已实现待提交版本：邮件改为 `multipart/alternative`（纯文本 + HTML），新增可选 `smtp_reply_to`，587 只建立一次强制 STARTTLS 连接，465 保留隐式 TLS，并将 context 取消贯穿拨号、握手与 SMTP socket。
- 验证码发送现在在 HTTP 请求线程通过 Redis `SET NX` 原子预留 60 秒冷却，任务携带固定验证码；队列满/最终失败按 reservation ID 条件释放，临时且明确未被接受的错误最多重试 3 次，API 文案改为“accepted for delivery”。队列停止时会拒绝新任务并排空已入队任务。
- 新增迁移 `224_email_delivery_webhooks.sql`、Resend Svix 签名校验、事件幂等入库和按事件时间更新投递状态；公共端点为 `POST /api/v1/webhooks/resend`，管理员查询为 `GET /api/v1/admin/email-deliveries`。上线后仍需在 Resend 创建 webhook，并把返回的 signing secret 写入设置键 `resend_webhook_secret`。
- 本地验证：Go 文件已由缓存 Go 1.26.6 `gofmt`；`git diff --check`、前端 `pnpm typecheck`、目标 ESLint、SettingsView 35 项测试通过。当前本机 Go 安装缺标准库，后端编译/单测必须由 PR GitHub Actions 完成。

- 改善方案已分层记录：先补 `text/plain`/可回复发件身份和 Resend delivery webhook，再修复 587 STARTTLS、Redis 原子冷却、失败回滚/有限重试；若 QQ 仍慢，使用 Resend 与腾讯云 SES 对 `qq.com/foxmail.com` 做小比例 A/B，不直接全量切换或购买独立 IP。
- Resend 只读 API 复核：QQ 邮件累计 `31 delivered / 2 delivery_delayed`，Gmail `1 delivered`。两封延迟邮件的脱敏收件人此前均有成功投递记录；发信域、DKIM、SPF 均为 verified，Resend 当前没有平台级事故。结合应用约 `3.3-3.9s` 完成 SMTP 接受，半小时以上延迟发生在 Resend 到 QQ 的下游投递重试阶段，而非 Sub2API 本地队列。
- 当前邮件为 HTML-only 且使用 `noreply` 发件地址。后续投递优化应增加 `text/plain` alternative、改用可回复地址、接入 Resend delivery webhook，并评估 QQ 收件使用更适配国内邮箱的通道。
- 生产 SSH 复核已恢复：`sub2api` 为 `running/healthy`、restart count `0`。最近 24 小时统计为验证码入队 14、worker 成功 13、失败 1、SMTP 错误 0、队列满 0；成功任务从入队到 SMTP 接受约 `3.3-3.9s`。
- 唯一失败是接口先返回 HTTP 200，worker 同时因 `VERIFY_CODE_TOO_FREQUENT` 拒绝发送；其前一次成功发送约在 59 秒前。这是冷却检查在 worker 内导致的误报成功，生产日志已复现源码诊断。
- 注册验证码和忘记密码接口走 `EmailQueueService`：固定缓冲 100、默认仅 3 个 worker；接口返回成功只代表入队，实际 SMTP 失败只记录日志，没有重试、死信或用户可见状态。队列满时验证码请求直接报错，密码重置为防止枚举会静默成功。
- 每封邮件都会从数据库重新读取 7 个 SMTP 设置，并新建/认证/关闭一个 SMTP 连接。网络路径有 10 秒拨号和 20 秒 I/O 超时，认证、MAIL、RCPT、DATA 串行执行；慢 SMTP 会占满 3 个 worker，任务因此排队变慢。`UseTLS=true` 在 STARTTLS 端口还会先尝试一次隐式 TLS，再建立第二条连接，可能额外增加延迟。
- SMTP 发送没有使用调用方 context；队列的 30 秒 context 无法中断底层网络操作。同步 OAuth/邮箱绑定流程会直接等待 SMTP，可能把请求拖到超时。
- `SendVerifyCode` 先把验证码写入 Redis，再发邮件。发送失败时验证码和 60 秒冷却仍保留，用户立即重试会收到 `VERIFY_CODE_TOO_FREQUENT`；队列也不会重试该任务。冷却检查位于 worker 内而非入队处，并发请求可能被多个 worker 同时放行、生成不同验证码，最终只有 Redis 最后一次写入的验证码有效，先到达的邮件可能无法验证。密码重置 token 同样先保存，但其冷却标记只在成功后写入。
- 生产交接记录显示 Resend 本地发送链路曾在约 4 秒内完成（入队到 worker 成功）；QQ 的 `Delivery Delayed` 是 Resend 已接受后收件服务器临时 4xx/限流/信誉等外部投递问题，Gmail 为 `Delivered`。需要在 Resend Events/Logs 查看 QQ 的具体 SMTP 4xx 原因，应用当前无法看到收件箱内部投递结果。
- 本次为只读诊断，未修改业务代码、生产配置或部署。后续修复优先级：失败重试/死信与可观测指标、发送失败回滚验证码或清除冷却、SMTP 配置缓存与连接复用、将 context/超时贯穿网络调用，并区分“入队成功”和“发送成功”的 API 状态。
