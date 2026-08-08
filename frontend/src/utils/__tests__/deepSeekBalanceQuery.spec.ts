import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getDeepSeekBalanceMock } = vi.hoisted(() => ({
  getDeepSeekBalanceMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getDeepSeekBalance: getDeepSeekBalanceMock,
    },
  },
}))

import { invalidateDeepSeekBalanceCache, queryDeepSeekBalance } from '../deepSeekBalanceQuery'

const result = (accountId: number) => ({
  is_available: true,
  balance_infos: [{ currency: 'CNY', total_balance: `${accountId}.00` }],
  fetched_at: accountId,
})

describe('queryDeepSeekBalance', () => {
  beforeEach(() => {
    getDeepSeekBalanceMock.mockReset()
    invalidateDeepSeekBalanceCache()
  })

  it('deduplicates concurrent queries for the same account', async () => {
    let resolveRequest!: (value: ReturnType<typeof result>) => void
    getDeepSeekBalanceMock.mockImplementationOnce(
      () => new Promise((resolve) => { resolveRequest = resolve })
    )

    const first = queryDeepSeekBalance(42)
    const second = queryDeepSeekBalance(42)

    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(1)

    resolveRequest(result(42))
    await expect(first).resolves.toEqual(result(42))
    await expect(second).resolves.toEqual(result(42))
  })

  it('releases a failed account query so it can be retried', async () => {
    getDeepSeekBalanceMock
      .mockRejectedValueOnce(new Error('provider unavailable'))
      .mockResolvedValueOnce(result(42))

    await expect(queryDeepSeekBalance(42)).rejects.toThrow('provider unavailable')
    await expect(queryDeepSeekBalance(42)).resolves.toEqual(result(42))
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
  })

  it('uses a short cache for automatic remounts and bypasses it for explicit refreshes', async () => {
    getDeepSeekBalanceMock
      .mockResolvedValueOnce(result(42))
      .mockResolvedValueOnce(result(43))

    await expect(queryDeepSeekBalance(42)).resolves.toEqual(result(42))
    await expect(queryDeepSeekBalance(42)).resolves.toEqual(result(42))
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(1)

    await expect(queryDeepSeekBalance(42, { force: true })).resolves.toEqual(result(43))
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(2)
  })

  it('removes an abandoned queued query before it reaches the provider', async () => {
    const resolvers = new Map<number, (value: ReturnType<typeof result>) => void>()
    getDeepSeekBalanceMock.mockImplementation(
      (accountId: number) => new Promise((resolve) => { resolvers.set(accountId, resolve) })
    )

    const active = [1, 2, 3].map((accountId) => queryDeepSeekBalance(accountId))
    const controller = new AbortController()
    const queued = queryDeepSeekBalance(4, { signal: controller.signal })
    controller.abort()

    await expect(queued).rejects.toMatchObject({ name: 'AbortError' })
    for (const accountId of [1, 2, 3]) resolvers.get(accountId)?.(result(accountId))
    await expect(Promise.all(active)).resolves.toEqual([1, 2, 3].map(result))
    expect(getDeepSeekBalanceMock.mock.calls.map(([accountId]) => accountId)).toEqual([1, 2, 3])
  })

  it('keeps a shared provider query alive while another subscriber remains', async () => {
    let providerSignal!: AbortSignal
    let resolveProvider!: (value: ReturnType<typeof result>) => void
    getDeepSeekBalanceMock.mockImplementationOnce(
      (_accountId: number, signal: AbortSignal) => {
        providerSignal = signal
        return new Promise((resolve) => { resolveProvider = resolve })
      }
    )
    const firstController = new AbortController()
    const secondController = new AbortController()

    const first = queryDeepSeekBalance(42, { signal: firstController.signal })
    const second = queryDeepSeekBalance(42, { signal: secondController.signal })
    firstController.abort()

    await expect(first).rejects.toMatchObject({ name: 'AbortError' })
    expect(providerSignal.aborted).toBe(false)

    resolveProvider(result(42))
    await expect(second).resolves.toEqual(result(42))
    expect(getDeepSeekBalanceMock).toHaveBeenCalledTimes(1)
  })

  it('limits provider queries to three concurrent requests', async () => {
    const resolvers = new Map<number, (value: ReturnType<typeof result>) => void>()
    getDeepSeekBalanceMock.mockImplementation(
      (accountId: number) => new Promise((resolve) => { resolvers.set(accountId, resolve) })
    )

    const requests = [1, 2, 3, 4, 5].map((accountId) => queryDeepSeekBalance(accountId))
    expect(getDeepSeekBalanceMock.mock.calls.map(([accountId]) => accountId)).toEqual([1, 2, 3])

    resolvers.get(1)?.(result(1))
    await Promise.resolve()
    await Promise.resolve()
    expect(getDeepSeekBalanceMock.mock.calls.map(([accountId]) => accountId)).toEqual([1, 2, 3, 4])

    resolvers.get(2)?.(result(2))
    await Promise.resolve()
    await Promise.resolve()
    expect(getDeepSeekBalanceMock.mock.calls.map(([accountId]) => accountId)).toEqual([1, 2, 3, 4, 5])

    for (const accountId of [3, 4, 5]) resolvers.get(accountId)?.(result(accountId))
    await expect(Promise.all(requests)).resolves.toEqual([1, 2, 3, 4, 5].map(result))
  })
})
