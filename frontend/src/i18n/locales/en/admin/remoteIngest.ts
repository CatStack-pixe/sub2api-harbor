export default {
  remoteIngest: {
    title: 'Remote Ingest',
    description: 'Manage remote enrollment, trusted clients, and account delivery probes',
    loadFailed: 'Failed to load remote ingest data',
    tabs: {
      tokens: 'Registration Tokens',
      clients: 'Clients',
      deliveries: 'Deliveries'
    },
    filters: {
      status: 'Status'
    },
    columns: {
      createdAt: 'Created At',
      updatedAt: 'Updated At'
    },
    tokens: {
      generate: 'Generate Token',
      generated: 'Registration token generated',
      generateFailed: 'Failed to generate registration token',
      createdTitle: 'Registration Token',
      oneTimeWarning: 'This token is shown once. Keep it secure before closing this dialog.',
      token: 'Token',
      copied: 'Registration token copied',
      fingerprint: 'Fingerprint',
      expiresIn: 'Token Lifetime',
      expiresAt: 'Expires At',
      usedAt: 'Used At',
      empty: 'No registration tokens',
      lifetime: {
        '10m': '10 minutes',
        '30m': '30 minutes',
        '1h': '1 hour'
      },
      status: {
        available: 'Available',
        used: 'Used',
        expired: 'Expired'
      }
    },
    clients: {
      searchPlaceholder: 'Machine name, client ID, or fingerprint',
      machineName: 'Machine',
      publicKeyFingerprint: 'Public Key Fingerprint',
      accessSubject: 'Access Identity',
      lastActiveAt: 'Last Active',
      enrolledAt: 'Enrolled At',
      empty: 'No enrolled clients',
      revoke: 'Revoke',
      revokeTitle: 'Revoke Client',
      revokeConfirm: 'Revoke {name}? This client will no longer be able to submit accounts.',
      revoked: 'Client revoked',
      revokeFailed: 'Failed to revoke client',
      status: {
        active: 'Active',
        revoked: 'Revoked'
      }
    },
    deliveries: {
      searchPlaceholder: 'External ID, client, account, or group',
      externalId: 'External ID',
      client: 'Client',
      platform: 'Platform',
      group: 'Group',
      account: 'Account',
      attempts: 'Attempts',
      error: 'Probe Error',
      empty: 'No account deliveries',
      retry: 'Retry Probe',
      retryQueued: 'Probe retry queued',
      retryFailed: 'Failed to retry probe',
      status: {
        pending: 'Pending',
        probing: 'Probing',
        active: 'Active',
        probe_failed: 'Probe Failed'
      }
    }
  }
}
