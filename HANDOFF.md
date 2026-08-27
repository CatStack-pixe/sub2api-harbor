# Handoff

## 2026-08-27 Platform/group routing fix and v0.1.183-nvidia.30 deployment

- PR [#96](https://github.com/CatStack-pixe/sub2api-harbor/pull/96) restored the pre-`836d4e9` `accountPlatformMatchesGroup` behavior in gateway sticky selection, OpenAI-compatible eligibility, and OpenAI scheduler load balancing. The final change is limited to the historical OpenAI/ChatAnywhere and DeepSeek/TokenRhythm group mappings; broad cross-provider matching was removed. Existing routing tests cover both compatible mappings and reverse-direction rejection.
- PR #96 was labeled `bug`, received the required post-label code-audit comment, passed CI/Security Scan, and was squash-merged as `d8accb64c1303238878b8f17564783a9e7da7e62`. Backend CI run `33050140287` and Security Scan run `33050140286` passed. Local frontend lint and the two affected test files passed (`37` tests).
- Release [v0.1.183-nvidia.30](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.183-nvidia.30) completed successfully in GitHub Actions run `33050919962` with the repository's simple amd64 release profile. The immutable image is `ghcr.io/catstack-pixe/sub2api:0.1.183-nvidia.30-amd64@sha256:18ce35aeb398a18a6f538e8df57564ff14bc3bc701441c09c62cfd67b3accbbf`.
- Production target is `root@154.37.212.18` (`instance-0GDSx0Ws`), deployment directory `/opt/sub2api`. The pre-deployment Compose backup is `/opt/sub2api/docker-compose.yml.before-0.1.183-nvidia.30` with SHA-256 `0b31e455546b13f78f915a69b7eb9efa18e40eb4de3ee2c94373baeeb573e188`.
- Only `sub2api` was pulled and recreated. Compose now pins the image digest above; the container is `running/healthy` with image ID `sha256:18ce35aeb398a18a6f538e8df57564ff14bc3bc701441c09c62cfd67b3accbbf`. Docker healthcheck probes passed after deployment. PostgreSQL, Redis, and Mihomo were not restarted.
- Status: production is live on `v0.1.183-nvidia.30`. The previous `v0.1.183-nvidia.29` container was replaced. No production account, group, credential, or database data was changed.

## 2026-08-27 Production Recovery and v0.1.183-nvidia.29 Deployment

- Production target: `root@154.37.212.18` (`instance-0GDSx0Ws`), deployment directory `/opt/sub2api`.
- Incident: the first `v0.1.183-nvidia.27` deployment failed during startup because migration `224_user_platform_quotas_add_cn_providers.sql` had an existing database checksum of `98f0eabfbfc8a6e5761f3cb1d19a17a0064696a173a56e933e6e0764cc8126cb`, while the image contained `5227db3c1a6a1e2e422a9f9ba9d1f490c708b6c6dd91ce89f3c48115421a3e55`.
- The corrective `v0.1.183-nvidia.28` image then exposed the same class of issue in migration `227_composite_routes_add_cn_providers.sql`: database checksum `c04b5b5b38121f820514622967092ec71b92f346c842cecc223ddd9cbeda3224` versus image checksum `ff6e3323b4bcb195a4f11bfa9b1b22286e77169f551b5c4294ab3d31828d8ff8`.
- Both failed releases were rolled back without changing database checksum rows. The known-good `0.1.179-nvidia.26-amd64` image remains the rollback target at digest `sha256:82d876029debad13a332b224af415e82467f74ecb18b23bbd4b6ece66622e0b2`.
- PR [#93](https://github.com/CatStack-pixe/sub2api-harbor/pull/93) restored the applied content and checksum for migration 224 and was squash-merged as `13bacae5e5f8a2e945b78e3a91f6a251c8ae535e`. PR [#94](https://github.com/CatStack-pixe/sub2api-harbor/pull/94) restored migration 227, preserved the complete fork platform set, and was squash-merged as `11110a5f4d22b7d62c2d603a20a752047290eeda`. Both PRs were labeled `enhancement`, audited after labeling, and passed GitHub CI and Security Scan.
- Release [v0.1.183-nvidia.29](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.183-nvidia.29) was built only by GitHub Actions run `33006796305` using Go `1.27.0`. The immutable amd64 GHCR image is `ghcr.io/catstack-pixe/sub2api:0.1.183-nvidia.29@sha256:5fb2ee6a5daf66ba452e8ad1aa47a1c2155c9d251699bc6cccff508ae0b875d4`.
- Pre-deployment backup passed `sha256sum -c` at `/opt/sub2api/backups/pre-release-0.1.183-nvidia.29-20260826T195126Z`; it includes Compose, environment, container inspection, and a PostgreSQL custom-format dump of `168644426` bytes. The backup directory is mode `700` and protected files are mode `600`.
- Deployment completed by recreating only `sub2api` with `docker compose up -d --no-deps --wait sub2api`. PostgreSQL, Redis, and Mihomo were not restarted. The application is `running/healthy`, restart count `0`, and Compose pins the digest above. Local and public `/health` return `{"status":"ok"}`; public settings report version `0.1.183-nvidia.29`; no recent panic, fatal, checksum-mismatch, or initialization-error patterns were found.
- Status: production is live on `v0.1.183-nvidia.29`. Releases `.27` and `.28` are superseded and must not be redeployed. No local Go toolchain, compilation, test, generation, lint, or release build was used; backend validation and image construction ran in GitHub Actions. The original dirty worktree remains untouched.

## 2026-08-26 Upstream v0.1.183 Integration

- Working tree: `.codex-worktrees/upstream-v183-merge-test`; the original dirty worktree was not modified.
- Fork base: `origin/main` at `631f267a753a8b43c2174aa5d971a7a74d4c1fcd`.
- Upstream source: `upstream/main` at `efb46db0a960fdad94502b1c3a982a0051cf5245`, version `0.1.183`.
- Integration commit: `70c6b2bd1`; final fork version: `0.1.183-nvidia.27`.
- Fork behavior retained: Agnes, NVIDIA, TokenRhythm, ChatAnywhere, Heartbeat, API-key management, deployment configuration, and provider-specific routing/account behavior.
- Upstream behavior integrated: plugins, Codex model catalogs, context/tier/time pricing, service-tier billing, OAuth automatic quota reset, Responses/WS fixes, security dependency updates, and Go `1.27.0`.
- Plugin migrations use `231_plugins.sql` and `232_plugin_artifacts.sql`; existing fork migrations `229_restore_fork_platform_constraints.sql` and `230_add_official_platform_constraints.sql` remain unchanged. The plugin SQL uses idempotent DDL.
- Post-merge compatibility audit restored the complete fork provider set across routing, model synchronization, account URL/auth defaults, header overrides, failover/error passthrough, quota validation, billing probes, admin channel mapping, and frontend account/key controls. DeepSeek retains its dedicated `/models` sync endpoint; TokenRhythm session fields remain redacted; Anthropic API-key passthrough normalization and provider-managed credential sanitation run during account creation.
- The audit also preserved upstream Codex catalog, auto-reset credit, adaptive CN protocol, and service-tier behavior. Duplicate plaza test declarations were renamed without removing either the upstream Model Plaza coverage or the fork Channel Plaza coverage.
- The CI remediation restores atomic request-quota admission across Responses, Messages, Chat Completions, and WebSocket turns; preserves the fork GLM detector and all OpenAI-compatible composite targets; restores the default-enabled long-context group field; fixes stale test stubs/helpers; and expands platform quota contract fixtures to the complete fork set.

### Build and Verification Policy

- Do not install or execute Go locally on Windows for this sync. Backend generation, compilation, unit tests, integration tests, golangci-lint, security scan, and release image builds must run in GitHub Actions.
- The local Go toolchain removal request targeted `E:\tools\go1.27.0`; it was removed and verified absent. Its presence must not be treated as a local validation source.
- Frontend checks completed locally before this policy change: frozen-lockfile install, typecheck, lint (two existing unused-test-helper warnings), and full Vitest (`261` files, `1838` tests). Frontend build and all backend checks require GitHub Actions confirmation.
- After the compatibility audit, frontend `pnpm typecheck`, changed-file ESLint, and the profit-control regression test passed (`13` tests). The backend remains unbuilt locally by design; GitHub Actions is the only authority for Go generation, compilation, tests, lint, security, and release image construction.
- A local backend full test run was stopped after it exposed the stale group-platform validator; the validator has since been updated to include all fork platforms. GitHub Actions is authoritative for the corrected result.
- Run `git diff --check` before push. Do not deploy from a dirty local worktree; deploy the immutable GHCR image produced by GitHub Actions.

### PR and Deployment

- Required commit title: `feat(sync): integrate upstream main while preserving fork features`.
- PR label: `enhancement`.
- Required process: perform one post-label code audit, wait for GitHub CI and Security Scan, then squash-merge and delete the sync branch.
- Production target: `root@154.37.212.18`, deployment directory `/opt/sub2api`.
- Deployment status: superseded by the corrective recovery and production deployment recorded above.
---

## 2026-08-23 Upstream v0.1.179 sync (GitHub validation complete; final review pending)

- The integration is isolated on branch `sync/upstream-v0.1.179` in `.codex-worktrees/upstream-v179-sync`, based on fork `origin/main` commit `0fcf9100c750f0542e9fbcf5be04830813737231`. PR [#87](https://github.com/CatStack-pixe/sub2api-harbor/pull/87) carries the required `enhancement` label. The original dirty worktree remains untouched.
- The stable official `v0.1.179` release is integrated against the explicit official `v0.1.176` merge base. Fork behavior remains authoritative: Agnes, NVIDIA, TokenRhythm, ChatAnywhere, Kimi, DeepSeek, and GLM are preserved, including fork routing, model defaults and synchronization, pricing, quota, failover, and provider-specific usage behavior.
- Upstream CN-provider adaptive protocols, Zhipu support, Composite routing, channel time/tier/context pricing, quota monitoring, usage rollups, Codex turn state, remote compaction, WebSocket bridge, client-tools, failover, and usage-accounting fixes are included where compatible. `glm` remains the fork platform and upstream `zhipu` is represented separately.
- Migration `229_restore_fork_platform_constraints.sql` restores the complete 13-platform union after the fork and upstream duplicate-number migration sequences execute in full-filename order.
- Frontend verification passed locally: typecheck, ESLint, and the full Vitest suite (`251` files, `1738` tests). `git diff --check` and conflict-marker checks also passed. Implementation commit `c7fd06a40` passed duplicate GitHub CI runs `32634732960` and `32634735269` (unit tests, integration tests, golangci-lint, frontend, and shell) plus Security Scan runs `32634732958` and `32634735254` (backend and frontend security).
- Independent provider, migration/CI, and merge-artifact audits found no remaining fork-provider regression or blocking issue; all reported duplicate declarations, fields, and conflict-splice artifacts were resolved before submission. The supported migration path is fork `origin/main` to this sync; databases that previously ran official v0.1.177-v0.1.179 may require a separate checksum-compatible migration path.
- PR #87 is awaiting the required final post-label code review and squash merge. It has not been released, deployed, or applied to production data.

## 2026-08-22 TokenRhythm built-in model sync fallback (completed)

- Root cause: TokenRhythm model sync uses the generic OpenAI-compatible `/v1/models` request. A provider timeout, non-2xx response, or empty model list was surfaced directly as a failed sync; the existing built-in model catalog was only used by the separate fill-related-models action. Production logs confirmed repeated TokenRhythm upstream sync failures while normal TokenRhythm chat requests remained healthy.
- Changed the TokenRhythm built-in catalog to the two production DeepSeek V4 model IDs: `deepseek-v4-pro` and `deepseek-v4-flash`, aligned across backend defaults and frontend whitelist options.
- When TokenRhythm upstream model sync fails or returns an empty list, the model whitelist control now adds those built-in models and displays an explicit fallback notice. Other platforms retain the existing error behavior; the fallback never claims the models came from upstream.
- Verification passed: frontend typecheck, full ESLint, frontend build, model whitelist tests, TokenRhythm fallback component test, and `git diff --check`. Go tests remain unavailable locally because Go is not installed; GitHub CI is authoritative. The post-merge main CI run `32574998352`, tag CI run `32575540868`, and tag Security Scan run `32575540887` all passed.
- PR [#80](https://github.com/CatStack-pixe/sub2api-harbor/pull/80) was labeled `bug` and `enhancement`, received the required code-audit review with no blocking findings, and was squash-merged as `75f03542bb1da208a0063cffb034bbfc3583ce7d`. Release [v0.1.176-nvidia.21-tokenrhythm-2](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.176-nvidia.21-tokenrhythm-2) completed successfully. The immutable amd64 image is pinned to digest `sha256:5a0f6bf95390c35a83ac80c3ad47e4e06c0df74e3d811fbe6ab9dab53dcf6a08`.
- Production deployment completed on `root@154.37.212.18` (`instance-0GDSx0Ws`) with strict SSH authentication using the configured client key path. The protected backup `/opt/sub2api/backups/pre-release-0.1.176-nvidia.21-tokenrhythm-2-20260822T133050Z` has directory mode `700`, file mode `600`, a PostgreSQL custom-format dump of `138840570` bytes, and passing `SHA256SUMS` verification. The private key and all credentials were omitted from output and documentation.
- Only `sub2api` was recreated with `docker compose up -d --no-deps --wait sub2api`; PostgreSQL, Redis, and Mihomo were not restarted. `sub2api` is `running/healthy` with restart count `0`, and Compose pins the new immutable image. Public `/health` returned HTTP `200` with `{"status":"ok"}`, and public settings report version `0.1.176-nvidia.21-tokenrhythm-2`.
- No credential, Cookie, API key, sess value, referral code/link, account, group, proxy, or production data configuration is recorded here.

## 2026-08-22 TokenRhythm sess and referral resolver release

- Work is isolated in `E:\文件\Python Script\sub2api\sub2api-tokenrhythm-sess` on branch `feat/tokenrhythm-session-referral`, based on current `origin/main`. The original dirty `sub2api-harbor` worktree was not modified.
- Added an admin-only `POST /api/v1/admin/tokenrhythm/session/resolve` flow. It accepts a `sess_...` value plus an optional existing proxy ID, sends the credential only to the fixed TokenRhythm referral endpoint with redirects disabled and a 15-second timeout, captures the provider `tr_csrf`, and returns the minimal account Cookie, referral code, official `/i/{code}` link, and three allowlisted status booleans. It does not create an account or persist the submitted sess.
- Added a reusable TokenRhythm session resolver control to both create-account and edit-account forms. A successful resolve fills the existing Cookie field and displays the referral link/code; the raw sess is cleared and is never included in account create/update payloads.
- Local verification completed: frontend typecheck, full ESLint, frontend build, the new component tests, existing TokenRhythm account-selection source tests, and `git diff --check` passed. The resolver route now has an explicit audit action and its credential-bearing request body is omitted from persistent audit logs, covered by a middleware regression test. Focused Go tests remain locally unavailable because this Windows environment has no Go toolchain; GitHub CI is the authoritative backend compile/test gate.
- Version `0.1.176-nvidia.21-tokenrhythm-1` is complete. PR [#77](https://github.com/CatStack-pixe/sub2api-harbor/pull/77) was labeled `enhancement` and `security`, received the required code-audit review with no blocking findings, and was squash-merged as `269e2eb640fb861d66a5fb20ae239c49741e3686`. The release workflow completed successfully and published [v0.1.176-nvidia.21-tokenrhythm-1](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.176-nvidia.21-tokenrhythm-1); `main` now reports the same version and the GHCR artifact is available.
- Production deployment completed on `root@154.37.212.18` (`instance-0GDSx0Ws`) after strict SSH authentication with the configured client key path. A protected backup was created at `/opt/sub2api/backups/pre-release-0.1.176-nvidia.21-tokenrhythm-1-20260822T114532Z` with directory mode `700`, file mode `600`, Compose and container inspection copies, a PostgreSQL custom-format dump (`138206336` bytes), and passing `SHA256SUMS` verification. The private key was not printed or recorded.
- Only `sub2api` was pulled and recreated with `docker compose up -d --no-deps --wait sub2api`. Compose now pins `ghcr.io/catstack-pixe/sub2api:0.1.176-nvidia.21-tokenrhythm-1` to digest `sha256:24eb60dc79a5c6765eb5756669cdff2f977d6afbba441b98b7101907a4069ee5`. The container is `running/healthy` with restart count `0`; PostgreSQL, Redis, and Mihomo retained their `2026-08-14T15:32:17Z` start times and restart count `0`.
- Post-deploy validation returned local and public `/health` as `{"status":"ok"}`, public settings version `0.1.176-nvidia.21-tokenrhythm-1`, and no panic, fatal, migration-startup, or database-startup errors in the recent application log window. No production account, group, proxy, credential, or data configuration was changed.
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

## 2026-08-23 TokenRhythm server-side API-key management

- Replaced the earlier local-only provisioner direction with an in-product implementation on branch `fix/tokenrhythm-built-in-model-sync`. Added `POST /api/v1/admin/tokenrhythm/api-keys`, which accepts a short-lived `sess`, key name, and optional proxy ID; the server resolves the provider CSRF/session cookie, calls the official TokenRhythm `POST /api/api-keys`, and returns the generated `sk_tr_...` plus normalized `tr_session`/`tr_csrf` Cookie. The sess is never persisted or returned.
- Added a shared TokenRhythm “Manage API Key” dialog to both account create and edit forms. It fills the generated API key and Cookie back into the form; saving the account remains an explicit user action. The sess input is cleared after the request, and the new key is shown only in the dialog response.
- Added audit action `admin.tokenrhythm.api_key.create` and omitted request bodies for both TokenRhythm session/key-management routes so sess values are not written to audit logs.
- Verification: frontend `vue-tsc --noEmit`, production `pnpm run build`, and focused TokenRhythm component tests pass (`3/3`). Backend service tests were added for proxy routing, Cookie rotation, key response parsing, invalid names, and missing keys. Local Go/gofmt are unavailable; run GitHub Actions CI before release.
- No production deployment, account mutation, provider key creation, or server restart was performed in this source-change turn.

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

## 2026-08-23 TokenRhythm Key Management Audit

- 生产 `POST /api/v1/admin/tokenrhythm/api-keys` 连续返回 HTTP 401；使用当前提供的 sess 直接请求 TokenRhythm `/api/referrals/me` 和 `/api/api-keys` 同样返回 `UNAUTHORIZED`，因此创建失败的直接原因是 sess 已过期/失效。
- 当前后端仅注册 session resolve、api-key create、balance 三类接口；没有 provider key 列表、disable 或 delete 路由/处理器。前端 resolver 也只有一次性创建并显示 secret 的流程，API 层没有 list/delete 函数，故界面不会显示账号可用 key、状态或删除按钮。
- TokenRhythm provider 的实际接口为 `GET /api/api-keys`、`POST /api/api-keys`、`POST /api/api-keys/:id/disable`、`POST /api/api-keys/:id/delete`、`POST /api/api-keys/batch-delete`；非 GET 请求需 `tr_csrf` cookie 与 `X-CSRF-Token`。列表只返回 masked metadata，完整 secret 仅在创建响应中返回一次。
- 后续管理功能应复用账号已保存的 provider cookie/session（或显式 sess 输入），新增列表和 disable/delete 后端方法及前端表格、状态和操作按钮；401 应明确提示重新获取有效 sess。
- 交接安全说明：本审计文档不包含任何凭据；请忽略本任务协作消息中可能意外出现的长 token 字符串，并将相关凭据视为已泄露、需要轮换。
- 审计时发现的服务层 helper 缺口已在当前工作树补上；但整体改动仍未完成，Go 工具链在本环境不可用，恢复工作后必须先编译并运行后端测试。
- Provider bundle confirms list objects use camelCase keys (`maskedKey`, `keyPrefix`, `createdAt`, `lastUsedAt`, `deletedAt`, `status`); a provider wire struct should use camelCase tags (or custom normalization) before mapping to the backend's snake_case response fields. Provider list response is an array (frontend uses `k.data ?? []`), while create returns an object containing `key` plus metadata.
- During concurrent implementation review, handler/service signatures were found inconsistent and have since been partially reconciled; verify the final call signatures and `TokenRhythmAPIKeyActionResult` naming before compiling.
- The in-progress handler now accepts `account_id` in the request body and calls the existing `*ForAccount` service methods, which resolves the redacted-cookie issue. Frontend should use `account_id` for edit-account management; only new-account flow should send sess/Cookie.

## 2026-08-23 TokenRhythm key management paused

- Work was paused at the user's request. No commit, push, PR, release, deployment, production restart, or production data mutation was performed for this unfinished change.
- Uncommitted files currently contain partial backend/API work: `backend/internal/service/tokenrhythm_session.go`, `backend/internal/handler/admin/tokenrhythm_handler.go`, `backend/internal/server/routes/admin.go`, `backend/internal/server/middleware/audit_log.go`, and `frontend/src/api/admin/accounts.ts`.
- The partial implementation adds provider key list/disable/delete service types and routes, supports optional `account_id` for server-side reuse of stored TokenRhythm credentials, and adds frontend API client functions. The Vue management dialog has not yet been updated with the key table or action buttons.
- Before resuming, reconcile and compile the partial signatures. In particular, verify `CreateTokenRhythmAPIKeyWithCredential` callers, `TokenRhythmAPIKeyActionResult` naming, account-scoped handler paths, route ordering for `/api-keys/list`, and the provider list response parser. Run `gofmt`, backend tests, frontend typecheck/build, and focused component tests; Go is unavailable in this Windows environment.
- Do not deploy the current worktree. The last deployed production release remains `v0.1.176-nvidia.21-tokenrhythm-3`; no new image exists for this paused work.
- No session, Cookie, API key, administrator token, referral value, account data, or production configuration is recorded in this handoff.
- `EditAccountModal.vue` currently mounts `TokenRhythmSessionResolver` without passing `account.id`; add an `accountId` prop and bind `:account-id="account.id"`. Otherwise the resolver cannot select the account-scoped API and will continue requiring a manually pasted sess in edit mode.
- `frontend/src/api/admin/accounts.ts` defines list/disable/delete functions but the exported `adminAPI.accounts` object currently only includes resolve/create; add the three new functions there or component calls through `adminAPI.accounts.*` will be undefined at runtime.

## 2026-08-23 TokenRhythm API-key management completed

- Rebased the work onto `origin/main` at `05ea60adc` and continued on `fix/tokenrhythm-api-key-management`.
- Completed provider key inventory, masked-key parsing, disable/delete actions, account-scoped credential reuse, Cookie rotation persistence, persistence-failure recovery, and audit-body omission for all credential-bearing TokenRhythm routes.
- Account-scoped requests preserve the account proxy, account ID, configured concurrency, and TLS fingerprint path. Explicit sess/Cookie input remains request-scoped and is never persisted.
- Completed create/edit UI integration, one-time key display, inventory loading/error/empty states, confirmation actions, rotated-Cookie handling, and English/Chinese translations.
- Local verification passed: Go handler/middleware unit tests, frontend typecheck, targeted ESLint, focused resolver tests (`6/6`), production frontend build, `gofmt`, and `git diff --check`.
- The focused Go service test command reaches the existing Ent runtime initialization panic at `ent/runtime/runtime.go:1148` before executing tests; authoritative CI must run the service suite.
- No commit, PR, release, deployment, production restart, account mutation, or provider key creation has been performed for this completed worktree yet.
- No sess, Cookie, API key, referral value, proxy credential, or administrator credential is recorded in this document.

## 2026-08-23 TokenRhythm API-key management released

- PR [#83](https://github.com/CatStack-pixe/sub2api-harbor/pull/83) was labeled `enhancement` and `security`, received the required no-blocker code-audit review, and was squash-merged as `68945e8cee9ce0ca69d5489889b49abaf6b7d816` (`feat(tokenrhythm): manage provider API keys`).
- Release [v0.1.176-nvidia.21-tokenrhythm-4](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.176-nvidia.21-tokenrhythm-4) completed successfully in GitHub Actions run `32618926232`; the VERSION sync job also passed and `origin/main` now points to `158be676d`.
- The immutable GHCR image is published as `ghcr.io/catstack-pixe/sub2api:0.1.176-nvidia.21-tokenrhythm-4`. Production target is `root@154.37.212.18` (`instance-0GDSx0Ws`), deployment directory `/opt/sub2api`.
- Deployment completed with a protected backup at `/opt/sub2api/backups/pre-release-0.1.176-nvidia.21-tokenrhythm-4-20260823T050313Z` (directory `700`, files `600`, PostgreSQL dump about `137M`, restore list `1220` entries, all SHA-256 checks passing). Only `sub2api` was recreated; PostgreSQL, Redis, and Mihomo were not restarted.
- Production pins `ghcr.io/catstack-pixe/sub2api:0.1.176-nvidia.21-tokenrhythm-4@sha256:78f94604e5d99c21ac9be15982c9bbeb2c96f4dc0e58cc756db7200bfe903dd8`, OCI revision `68945e8cee9ce0ca69d5489889b49abaf6b7d816`. The container is `running/healthy` with restart count `0`; local and public `/health` return `{"status":"ok"}` and public settings report version `0.1.176-nvidia.21-tokenrhythm-4`.
- Local checks and PR CI are green: frontend typecheck/build, focused resolver tests (`6/6`), targeted ESLint, handler/middleware unit tests, gofmt, diff checks, backend CI, frontend CI, lint, shell, and security scans. The focused local service test remains blocked before test execution by the existing Ent runtime nil-to-bool panic; CI is authoritative.
- No sess, Cookie, API key, referral value, proxy credential, administrator credential, account mutation, or provider key creation is recorded in this document.

## 2026-08-23 TokenRhythm generated API-key form autofill

- Fixed the account create/edit forms so the TokenRhythm Manage API Key dialog writes the newly generated provider key through an explicit `v-model:api-key` binding. The generated `api_key` is now guaranteed to populate the form API Key field, while the existing `apiKeyCreated` event continues to update the rotated Cookie.
- Added a focused resolver regression assertion for the `update:apiKey` event. The frontend targeted test and TypeScript typecheck pass locally.
- The generated provider key is shown only once by TokenRhythm. If a user closes the dialog after generating it on an older build, the lost value cannot be recovered from the masked inventory; generate a replacement key or paste the still-visible value into the account API Key field.
- This fix is local and has not yet been committed, pushed, reviewed, merged, released, or deployed. Do not claim the production UI contains it until the next immutable release is deployed.

## 2026-08-23 TokenRhythm API-key form autofill released and deployed

- PR [#85](https://github.com/CatStack-pixe/sub2api-harbor/pull/85) was labeled `bug`, received a no-blocker code-audit comment, passed all backend, frontend, lint, shell, and security checks, and was squash-merged as `7527ecb22cae5812eac3105f38fcc6183de4e3d7` (`fix(tokenrhythm): autofill generated API key`).
- Release [v0.1.176-nvidia.21-tokenrhythm-5](https://github.com/CatStack-pixe/sub2api-harbor/releases/tag/v0.1.176-nvidia.21-tokenrhythm-5) completed in Actions run `32621441872`. The immutable GHCR image digest is `sha256:1bbbc511b9c8ba6ec672adfb6b5a2a0e6ba4b9e356747654550cd46f95bd93c3`, with OCI revision `7527ecb22cae5812eac3105f38fcc6183de4e3d7`.
- Production backup: `/opt/sub2api/backups/pre-release-0.1.176-nvidia.21-tokenrhythm-5-20260823T062601Z`; directory mode `700`, files `600`, PostgreSQL custom dump `143956464` bytes, restore list `1220` lines, and every SHA-256 entry passed.
- Only `sub2api` was recreated. It is `running/healthy` with restart count `0`; PostgreSQL, Redis, and Mihomo retained their `2026-08-14` start times and restart count `0`. Local and public health return `{"status":"ok"}`, public settings report `0.1.176-nvidia.21-tokenrhythm-5`, and the startup log scan found no panic, fatal, migration, or database startup errors.
- Generated TokenRhythm keys now populate the account API Key field in both create and edit forms. The Cookie continues to update separately. A key lost after closing an older one-time dialog cannot be recovered from the masked inventory; create a replacement or paste the still-visible key manually.

## 2026-08-24 Official account platform frontend support

- Added frontend platform identifiers, selectors, defaults, API-key hints, icons, colors, model whitelist mappings, quota lists, channel ordering, and English/Chinese labels for ModelScope, Alibaba Cloud DashScope, MiniMax, and Volcengine Ark.
- New account creation remains ungrouped by default when no group is selected; the new providers use their OpenAI-compatible API endpoints and do not enable the generic upstream billing probe.
- Verification passed in the clean `official-platforms` worktree: `vue-tsc --noEmit`, ESLint, 6 focused Vitest files with 106 tests, and `git diff --check`. Existing test warnings include stale Browserslist data and unresolved `router-link` in the SettingsView test harness.
- No commit, PR, release, deployment, production account mutation, or credential value is recorded here.
