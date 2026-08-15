import { adminAPI } from '@/api/admin'
import type { KimiBalanceResult } from '@/api/admin/accounts'

const MAX_CONCURRENT_QUERIES = 3
const CACHE_TTL_MS = 30 * 1000

interface QueryOptions {
  signal?: AbortSignal
  force?: boolean
}

interface QuerySubscriber {
  resolve: (result: KimiBalanceResult) => void
  reject: (reason: unknown) => void
  signal?: AbortSignal
  abortHandler?: () => void
}

interface QueuedQuery {
  accountId: number
  controller: AbortController
  subscribers: Set<QuerySubscriber>
  state: 'queued' | 'active'
}

const queue: QueuedQuery[] = []
const queries = new Map<number, QueuedQuery>()
const cache = new Map<number, { result: KimiBalanceResult; expiresAt: number }>()
let activeQueries = 0

const abortError = () => new DOMException('Kimi balance query was cancelled', 'AbortError')

const removeAbortHandler = (subscriber: QuerySubscriber) => {
  if (subscriber.signal && subscriber.abortHandler) {
    subscriber.signal.removeEventListener('abort', subscriber.abortHandler)
  }
}

const removeQueuedQuery = (query: QueuedQuery) => {
  const index = queue.indexOf(query)
  if (index >= 0) queue.splice(index, 1)
}

const cancelSubscriber = (query: QueuedQuery, subscriber: QuerySubscriber) => {
  if (!query.subscribers.delete(subscriber)) return
  removeAbortHandler(subscriber)
  subscriber.reject(abortError())
  if (query.subscribers.size > 0) return
  if (query.state === 'queued') removeQueuedQuery(query)
  if (query.state === 'active') query.controller.abort()
  if (queries.get(query.accountId) === query) queries.delete(query.accountId)
}

const settleQuery = (query: QueuedQuery, result: KimiBalanceResult | undefined, error: unknown) => {
  if (result && !query.controller.signal.aborted) {
    cache.set(query.accountId, { result, expiresAt: Date.now() + CACHE_TTL_MS })
  }
  for (const subscriber of query.subscribers) {
    removeAbortHandler(subscriber)
    if (result) subscriber.resolve(result)
    else subscriber.reject(error)
  }
  query.subscribers.clear()
  if (queries.get(query.accountId) === query) queries.delete(query.accountId)
}

const drainQueue = () => {
  while (activeQueries < MAX_CONCURRENT_QUERIES && queue.length > 0) {
    const query = queue.shift()
    if (!query || query.subscribers.size === 0) continue
    query.state = 'active'
    activeQueries += 1
    let providerRequest: Promise<KimiBalanceResult>
    try {
      providerRequest = adminAPI.accounts.getKimiBalance(query.accountId, query.controller.signal)
    } catch (error) {
      providerRequest = Promise.reject(error)
    }
    void providerRequest
      .then((result) => settleQuery(query, result, undefined), (error) => settleQuery(query, undefined, error))
      .finally(() => {
        activeQueries -= 1
        drainQueue()
      })
  }
}

export const invalidateKimiBalanceCache = (accountId?: number) => {
  if (accountId == null) cache.clear()
  else cache.delete(accountId)
}

export const queryKimiBalance = (accountId: number, options: QueryOptions = {}): Promise<KimiBalanceResult> => {
  if (options.signal?.aborted) return Promise.reject(abortError())
  const existing = queries.get(accountId)
  if (!existing && !options.force) {
    const cached = cache.get(accountId)
    if (cached && cached.expiresAt > Date.now()) return Promise.resolve(cached.result)
    if (cached) cache.delete(accountId)
  }
  const query = existing ?? {
    accountId,
    controller: new AbortController(),
    subscribers: new Set<QuerySubscriber>(),
    state: 'queued' as const,
  }
  if (!existing) {
    queries.set(accountId, query)
    queue.push(query)
  }
  const request = new Promise<KimiBalanceResult>((resolve, reject) => {
    const subscriber: QuerySubscriber = { resolve, reject, signal: options.signal }
    if (options.signal) {
      subscriber.abortHandler = () => cancelSubscriber(query, subscriber)
      options.signal.addEventListener('abort', subscriber.abortHandler, { once: true })
    }
    query.subscribers.add(subscriber)
  })
  drainQueue()
  return request
}
