export default {
  remoteIngest: {
    title: '远程接入',
    description: '管理远程注册、可信客户端和账号交付探测',
    loadFailed: '加载远程接入数据失败',
    tabs: {
      tokens: '注册令牌',
      clients: '客户端',
      deliveries: '交付记录'
    },
    filters: {
      status: '状态'
    },
    columns: {
      createdAt: '创建时间',
      updatedAt: '更新时间'
    },
    tokens: {
      generate: '生成令牌',
      generated: '注册令牌已生成',
      generateFailed: '生成注册令牌失败',
      createdTitle: '注册令牌',
      oneTimeWarning: '此令牌仅显示一次。关闭窗口前请妥善保管。',
      token: '令牌',
      copied: '注册令牌已复制',
      fingerprint: '指纹',
      expiresIn: '令牌有效期',
      expiresAt: '过期时间',
      usedAt: '使用时间',
      empty: '暂无注册令牌',
      lifetime: {
        '10m': '10 分钟',
        '30m': '30 分钟',
        '1h': '1 小时'
      },
      status: {
        available: '可用',
        used: '已使用',
        expired: '已过期'
      }
    },
    clients: {
      searchPlaceholder: '机器名称、客户端 ID 或指纹',
      machineName: '机器',
      publicKeyFingerprint: '公钥指纹',
      accessSubject: 'Access 身份',
      lastActiveAt: '最后活动',
      enrolledAt: '注册时间',
      empty: '暂无已注册客户端',
      revoke: '吊销',
      revokeTitle: '吊销客户端',
      revokeConfirm: '确定吊销 {name}？该客户端将无法继续提交账号。',
      revoked: '客户端已吊销',
      revokeFailed: '吊销客户端失败',
      status: {
        active: '正常',
        revoked: '已吊销'
      }
    },
    deliveries: {
      searchPlaceholder: '外部 ID、客户端、账号或分组',
      externalId: '外部 ID',
      client: '客户端',
      platform: '平台',
      group: '分组',
      account: '账号',
      attempts: '尝试次数',
      error: '探测错误',
      empty: '暂无账号交付记录',
      retry: '重新探测',
      retryQueued: '重新探测任务已加入队列',
      retryFailed: '重新探测失败',
      status: {
        pending: '等待中',
        probing: '探测中',
        active: '已上线',
        probe_failed: '探测失败'
      }
    }
  }
}
