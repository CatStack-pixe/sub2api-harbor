# Handoff

## Current State

- Added the TokenRhythm account platform with API-key inference through the fixed `https://tokenrhythm.studio/v1` endpoint.
- Admin account create/edit accepts a complete Cookie header, stores only `tr_session` and `tr_csrf`, and exposes presence flags without returning secret values.
- Added the admin-only TokenRhythm balance probe at `/admin/tokenrhythm/accounts/:id/balance`; it sends the stored Cookie only to the official usage endpoint and does not change scheduling state.
- Added OpenAI-compatible Chat Completions routing and native Anthropic Messages/count-tokens passthrough for TokenRhythm.
- Added TokenRhythm to platform/group/quota/model-selection surfaces and account usage UI with bounded, cached balance queries.
- Added the Kimi Open Platform account channel with API-key-only authentication and a controlled region selector for `api.moonshot.cn` and `api.moonshot.ai`.
- Added the read-only Kimi balance probe at `/admin/kimi/accounts/:id/balance`; it returns available, voucher, and cash balances without exposing credentials or changing scheduling state.
- Pool-mode insufficient-balance quarantine and error-log session correlation are already present in the preceding commits.

## Verification

- Frontend `pnpm typecheck`, `pnpm lint:check`, targeted TokenRhythm tests, and updated quota contract tests pass locally.
- Kimi frontend typecheck, lint, and focused account/platform/quota tests pass locally (54 assertions).
- Full frontend Vitest has two pre-existing transform failures caused by duplicate `getLiveCapability` declarations in the baseline GroupsView tests; unrelated assertions now pass.
- Go is unavailable in the local Windows environment. Run the repository GitHub Actions CI before release to execute Go generation, unit tests, integration tests, and frontend build.

## Release / Deploy

- Branch: create a feature branch from the current merged base before submitting the Kimi change.
- Required release path: English Conventional Commit, push and create a labeled PR with `gh`, review the PR diff, squash-merge, then dispatch the repository release workflow. Deploy the resulting application container only.
- No manual production account-state mutation is required; the next matching upstream insufficient-balance response activates the existing 30-minute pool quarantine.
