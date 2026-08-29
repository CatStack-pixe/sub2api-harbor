# Heartbeat 账户组与代理路由

## 当前边界

Heartbeat 当前是 DeepSeek 专用的自动入库流水线，不是通用序列号仓库：

- 上报内容是 fingerprint、余额和检查时间，真实 key 由 worker 使用加密 session_key 从 Vault 查询。
- 每次最多 100 个 key，同一实例两次成功接收之间至少间隔 10 秒。
- provider 当前固定为 ds，目标账户组必须是启用状态的 DeepSeek 组。
- 同一 (provider, fingerprint) 只有一条任务记录；重复上报同一指纹不会产生重复账户。
- TokenRhythm 的历史 key 列表只返回掩码字段，完整 key 只在创建响应中出现一次。这是凭据生命周期约束，不是账户组下拉框的缺陷。

因此，GLM/Zhipu、TokenRhythm 等平台的 key 不会自动进入 Heartbeat 目标组。要做跨平台批量导入，需要为每个平台增加 Vault provider、账户平台映射、余额探测和目标组兼容矩阵，不能只放宽 provider 字符串校验。

## 代理路由模型

账户组和代理组是独立维度：

- 普通请求只读取账户的 accounts.proxy_id；账户组不会自动继承代理组。
- proxy_id 有值时，请求使用该账户代理；proxy_id 为空时走直连。
- Heartbeat 的 proxy_group_id 只在 worker 新建或修复账户时挑选代理，并把结果写回账户。

### 未分组代理池

Heartbeat 目标现在支持 proxy_group_id: 0，表示活动且未分配代理组的代理池（任务表中以 NULL 保存，避免触发代理组外键）。该池会：

1. 出现在 Heartbeat 设置页的代理选项中；
2. 通过 Ungrouped=true 参与活动代理预检；
3. 参与低延迟探测、前 10% 候选缓存和账户绑定。

示例：

    heartbeat_provisioning:
      default_group_id: 12
      targets:
        - group_id: 12
          proxy_group_id: 0

未分组代理池仍然要求至少有一个活动代理，并且至少有一个探测成功的代理。

## 调度刷新修复

账户解绑全部账户组时，仓储现在会发送包含旧组 ID 的 scheduler outbox 事件。调度器收到事件后会同时重建：

- 旧账户组桶，移除已解绑账户；
- group 0 未分组桶，把当前无组账户立即纳入；
- TokenRhythm/Antigravity 的兼容平台桶。

这样无需等待周期性全量快照刷新，未分组账户的路由状态会即时收敛。账户自身仍需配置 proxy_id 才会走代理。

## 重复 Heartbeat 上报

同一指纹重新上报时：

- 目标账户组或代理池发生变化，任务会重置尝试次数并重新排队；
- 任务此前处于失败状态，重新上报会重新排队；
- 正在处理的任务保留当前租约，避免并发上报打断 worker；
- 目标未变化且任务已完成时保持幂等，不重复创建账户。

## 运维检查

1. Heartbeat 设置页确认目标账户组为 DeepSeek 活动组。
2. 需要使用未分组代理时，将目标代理池选为 Unassigned proxies (#0)，并确认存在活动代理。
3. 普通未分组账户在账户详情中显式设置代理；只设置账户组不会产生代理绑定。
4. 查看 Heartbeat 状态中的 queued、processing、retry、failed、complete，确认重新上报后任务进入预期状态。

## 回滚

回滚应用镜像即可恢复代码版本。数据库中的账户组、账户代理和 Heartbeat 任务记录保持不变；回滚后将 proxy_group_id: 0 改回实际代理组 ID，可恢复旧版配置兼容路径。
