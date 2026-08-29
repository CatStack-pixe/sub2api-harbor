# Heartbeat 账户组与代理路由

## 多平台范围

Heartbeat 现在按 provider 将检查结果转换为对应账户平台，并支持当前调度器的全部 17 个具体平台：

- DeepSeek（`ds`、`deepseek`）
- Anthropic、OpenAI、Gemini、Antigravity、Grok、Agnes、NVIDIA
- TokenRhythm（`tokenrhythm`、`tr`）、Kimi（`kimi`、`moonshot`）
- Zhipu（`zhipu`、`bigmodel`）、ChatAnywhere、GLM、ModelScope
- DashScope（`dashscope`、`aliyun`、`qwen`）、MiniMax、Volcengine（`volcengine`、`ark`、`doubao`）

Heartbeat 设置页的账户组下拉框读取全部活动组，并展示组名、平台和 ID。`composite` 也是可选的目标组，它会按复合组已有路由接收具体平台账户。

上报请求中的 `provider` 会先规范化为稳定 ID，再写入任务表的 `provider` 列。历史任务没有 provider 时按 `ds` 兼容处理。相同 `(provider, fingerprint)` 仍保持幂等。

## 目标组选择

账户组和代理组是独立维度。目标组支持两种配置方式：

1. 在 key 条目中传 `group_id`，明确指定该条目的账户组。
2. 省略 `group_id`，先使用 `default_group_id`；若默认组与原生平台不兼容，则从 `targets` 中自动寻找第一个兼容目标组。

兼容关系遵循网关现有路由矩阵：

- OpenAI 兼容平台（包括 GLM、Zhipu、DeepSeek、Kimi、NVIDIA、ModelScope、DashScope、MiniMax、Volcengine 等）可进入 OpenAI 兼容组或 `composite` 组。
- Anthropic、Gemini、Antigravity 保持原生协议隔离，只进入相同平台组或 `composite` 组。

worker 在创建账户前会再次检查目标组状态和兼容关系。目标组必须处于活动状态；校验失败的任务会进入既有重试流程并记录原因。

## Vault 凭据协议

Vault 响应中的 key 项至少包含以下字段：

```json
{
  "key": "完整 API key",
  "provider": "glm",
  "credentials": {
    "base_url": "https://example.invalid/v1"
  }
}
```

`credentials` 是可选扩展。worker 只合并白名单字段：`base_url`、`api_protocol`、`account_mode`、`tokenrhythm_cookie`、`tr_session`、`tr_csrf`、`user_agent`、`header_overrides`。TokenRhythm 需要 `tr_session` 和 `tr_csrf`（或完整 `tokenrhythm_cookie`）；Antigravity API key 需要兼容网关的 `base_url`（路径以 `/antigravity` 结尾）。缺少这些平台专属字段时，该条目会按凭据校验失败处理。

创建账户后，DeepSeek 使用余额接口检查可用性；Kimi 的付费账户使用余额接口，Coding Plan 和其余 API-key 平台使用带认证的 `/models` 探测。没有公开 `/models` 的平台在返回 404/405 时保留账户，后续网关请求继续完成认证。

## 代理路由模型

- 普通请求读取账户自身的 `accounts.proxy_id`；账户组不会自动继承代理组。
- `proxy_id` 有值时使用该账户代理；`proxy_id` 为空时走直连。
- Heartbeat 的 `proxy_group_id` 只在 worker 新建或修复账户时选择代理，并把结果写回账户。

### 未分组代理池

`proxy_group_id: 0` 表示活动且未分配代理组的代理池，任务表中以 `NULL` 保存：

1. 出现在 Heartbeat 设置页的代理选项中；
2. 通过 `Ungrouped=true` 参与活动代理预检；
3. 参与低延迟探测、前 10% 候选缓存和账户绑定。

示例：

```yaml
heartbeat_provisioning:
  default_group_id: 12
  targets:
    - group_id: 12
      proxy_group_id: 0
    - group_id: 25
      proxy_group_id: 3
```

未分组代理池需要至少一个活动代理，并且至少有一个探测成功的代理。

## 调度刷新

账户解绑全部账户组时，仓储发送包含旧组 ID 的 scheduler outbox 事件。调度器收到事件后立即重建旧账户组桶和 group 0 未分组桶，账户的 `proxy_id` 仍按账户自身设置生效。

## 重复上报

- 目标账户组或代理池发生变化，任务会重置尝试次数并重新排队。
- 任务此前失败时，重新上报会再次排队。
- 正在处理的任务保留当前租约，避免并发上报打断 worker。
- 目标未变化且任务已完成时保持幂等，不重复创建账户。

## 运维检查

1. 在 Heartbeat 设置页确认需要的各平台活动组已加入 `targets`，默认组可继续使用历史 DS 组。
2. 需要未分组代理时选择 `Unassigned proxies (#0)`，并确认存在活动代理。
3. TokenRhythm 的 Vault key 项补充 `tokenrhythm_cookie` 或 `tr_session`、`tr_csrf`。
4. 查看 Heartbeat 状态的 `queued`、`processing`、`retry`、`failed`、`complete`，确认任务进入预期状态。

## 回滚

回滚应用镜像即可恢复代码版本。数据库中的账户组、账户代理和 Heartbeat 任务记录保持不变；回滚后可将 `proxy_group_id: 0` 改回实际代理组 ID，历史 DS 任务仍按兼容路径处理。
