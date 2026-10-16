import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

enableAutoUnmount(afterEach)
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerPush = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const paymentStore = vi.hoisted(() => ({
  config: { stripe_publishable_key: 'pk_test' } as { stripe_publishable_key?: string },
  fetchConfig: vi.fn(),
  pollOrderStatus: vi.fn(),
}))
const loadStripe = vi.hoisted(() => vi.fn())
const stripeElements = vi.hoisted(() => ({
  create: vi.fn(),
}))
const stripePaymentElement = vi.hoisted(() => ({
  mount: vi.fn(),
  on: vi.fn(),
}))
const stripeInstance = vi.hoisted(() => ({
  elements: vi.fn(),
  confirmPayment: vi.fn(),
  confirmAlipayPayment: vi.fn(),
  confirmWechatPayPayment: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({ push: routerPush }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'zh-CN' },
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => paymentStore,
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getOrder,
  },
}))

vi.mock('@stripe/stripe-js/pure', () => ({
  loadStripe,
}))

import StripePaymentView from '../StripePaymentView.vue'
import { formatPaymentAmount } from '@/components/payment/currency'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  clearPaymentRecoverySnapshot,
  readStripePaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import type { PaymentOrder } from '@/types/payment'

function orderFactory(overrides: Partial<PaymentOrder> = {}): PaymentOrder {
  return {
    id: 42,
    user_id: 7,
    amount: 100,
    pay_amount: 103,
    currency: 'CNY',
    fee_rate: 0.03,
    payment_type: 'stripe',
    out_trade_no: 'sub2_stripe_42',
    status: 'PENDING',
    order_type: 'balance',
    created_at: '2026-04-20T12:00:00Z',
    expires_at: '2026-04-20T12:30:00Z',
    refund_amount: 0,
    ...overrides,
  }
}

function stripeSnapshot(overrides: Partial<PaymentRecoverySnapshot> = {}): PaymentRecoverySnapshot {
  return {
    orderId: 42,
    amount: 100,
    qrCode: '',
    expiresAt: '2099-01-01T00:30:00.000Z',
    paymentType: 'stripe',
    payUrl: '/payment/stripe?order_id=42',
    outTradeNo: 'sub2_stripe_42',
    clientSecret: 'pi_secret_42',
    intentId: '',
    currency: 'CNY',
    countryCode: '',
    paymentEnv: '',
    payAmount: 103,
    orderType: 'balance',
    paymentMode: '',
    resumeToken: '',
    createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    ...overrides,
  }
}

function writeSnapshot(overrides: Partial<PaymentRecoverySnapshot> = {}) {
  window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify(stripeSnapshot(overrides)))
}

function mountView() {
  return shallowMount(StripePaymentView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
}

describe('StripePaymentView', () => {
  beforeEach(() => {
    routeState.query = {
      order_id: '42',
    }
    routerPush.mockReset()
    getOrder.mockReset().mockResolvedValue({ data: orderFactory() })
    paymentStore.config = { stripe_publishable_key: 'pk_test' }
    paymentStore.fetchConfig.mockReset().mockResolvedValue(undefined)
    paymentStore.pollOrderStatus.mockReset()
    loadStripe.mockReset().mockResolvedValue(stripeInstance)
    stripeElements.create.mockReset().mockReturnValue(stripePaymentElement)
    stripePaymentElement.mount.mockReset()
    stripePaymentElement.on.mockReset().mockImplementation((event: string, callback: () => void) => {
      if (event === 'ready') callback()
    })
    stripeInstance.elements.mockReset().mockReturnValue(stripeElements)
    stripeInstance.confirmPayment.mockReset()
    stripeInstance.confirmAlipayPayment.mockReset()
    stripeInstance.confirmWechatPayPayment.mockReset()
    window.localStorage.clear()
    writeSnapshot()
  })

  it('uses the client secret from the local snapshot of the same order', async () => {
    routeState.query.method = 'alipay'
    mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledWith('pi_secret_42', expect.any(Object))
  })

  it('ignores a client secret supplied in the URL query', async () => {
    window.localStorage.clear()
    routeState.query = { order_id: '42', client_secret: 'pi_other_secret', method: 'alipay' }

    const wrapper = mountView()
    await flushPromises()

    expect(getOrder).not.toHaveBeenCalled()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('rejects a snapshot that belongs to a different order', async () => {
    writeSnapshot({ orderId: 7, clientSecret: 'pi_secret_7' })
    routeState.query = { order_id: '42', client_secret: 'pi_secret_42', method: 'alipay' }

    const wrapper = mountView()
    await flushPromises()

    expect(getOrder).not.toHaveBeenCalled()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('uses the bound local secret even when the URL supplies another secret', async () => {
    routeState.query = { order_id: '42', client_secret: 'pi_other_secret', method: 'alipay' }
    stripeInstance.confirmAlipayPayment.mockResolvedValueOnce({})
    mountView()
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledWith('pi_secret_42', expect.any(Object))
  })

  it.each([false, true])('restores an older order after another tab creates a new checkout, completed=%s', async (completed) => {
    // The old current snapshot also covers upgrading an already pending checkout.
    writeSnapshot({ resumeToken: 'resume-42' })
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({
      orderId: 43, clientSecret: 'pi_secret_43', resumeToken: 'resume-43'
    }))
    if (completed) clearPaymentRecoverySnapshot(window.localStorage)
    routeState.query = { order_id: '42', resume_token: 'resume-42', method: 'alipay' }
    stripeInstance.confirmAlipayPayment.mockResolvedValueOnce({})
    mountView()
    await flushPromises()
    await flushPromises()
    expect(getOrder).toHaveBeenCalledWith(42)
    expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledWith('pi_secret_42', expect.any(Object))
  })

  it('clears a completed order without deleting another tab’s current or archived checkout', () => {
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot())
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({ orderId: 43, clientSecret: 'pi_secret_43' }))
    clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY, 42)
    expect(readStripePaymentRecoverySnapshot(window.localStorage, 42)).toBeNull()
    expect(readStripePaymentRecoverySnapshot(window.localStorage, 43)?.clientSecret).toBe('pi_secret_43')
    expect(JSON.parse(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)!).orderId).toBe(43)
  })

  it('rejects mismatched tokens for archived orders', async () => {
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({ resumeToken: 'resume-42' }))
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({ orderId: 43 }))
    routeState.query = { order_id: '42', resume_token: 'wrong-token', method: 'alipay' }
    const wrapper = mountView()
    await flushPromises()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('prunes expired archived orders while retaining a valid pending checkout', () => {
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({
      expiresAt: '2000-01-01T00:00:00Z',
    }))
    writePaymentRecoverySnapshot(window.localStorage, stripeSnapshot({
      orderId: 43, clientSecret: 'pi_secret_43'
    }))
    expect(readStripePaymentRecoverySnapshot(window.localStorage, 42)).toBeNull()
    expect(readStripePaymentRecoverySnapshot(window.localStorage, 43)?.clientSecret).toBe('pi_secret_43')
    expect(window.localStorage.getItem(`${PAYMENT_RECOVERY_STORAGE_KEY}.stripe`)).not.toContain('pi_secret_42')
  })

  it.each(['', 'alipay', 'wechat_pay'])('restores %s payment after a refresh using the matching resume token', async (method) => {
    writeSnapshot({ resumeToken: 'resume-42' })
    routeState.query = { order_id: '42', resume_token: 'resume-42', method }
    stripeInstance.confirmAlipayPayment.mockResolvedValue({})
    stripeInstance.confirmWechatPayPayment.mockResolvedValue({ paymentIntent: {
      status: 'requires_action',
      next_action: { wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,qr' } }
    } })
    const first = mountView()
    await flushPromises()
    await flushPromises()
    first.unmount()
    mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledTimes(2)
    if (method === 'alipay') {
      expect(stripeInstance.confirmAlipayPayment).toHaveBeenCalledTimes(2)
      expect(stripeInstance.confirmAlipayPayment).toHaveBeenLastCalledWith('pi_secret_42', expect.any(Object))
    } else if (method === 'wechat_pay') {
      expect(stripeInstance.confirmWechatPayPayment).toHaveBeenCalledTimes(2)
      expect(stripeInstance.confirmWechatPayPayment).toHaveBeenLastCalledWith('pi_secret_42', expect.any(Object))
    } else {
      expect(stripeInstance.elements).toHaveBeenCalledTimes(2)
      expect(stripeInstance.elements).toHaveBeenLastCalledWith(expect.objectContaining({ clientSecret: 'pi_secret_42' }))
      expect(stripePaymentElement.mount).toHaveBeenCalledTimes(2)
    }
  })

  it.each(['expired', 'malformed', 'wrong-token'])('rejects %s recovery data before initializing Stripe', async (scenario) => {
    if (scenario === 'expired') writeSnapshot({ expiresAt: '2000-01-01T00:00:00Z' })
    if (scenario === 'malformed') window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, '{')
    if (scenario === 'wrong-token') routeState.query.resume_token = 'another-order-token'
    const wrapper = mountView()
    await flushPromises()
    expect(getOrder).not.toHaveBeenCalled()
    expect(loadStripe).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('payment.stripeMissingParams')
  })

  it('shows an initialization error when browser recovery storage is unavailable', async () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('Storage blocked', 'SecurityError')
    })
    try {
      const wrapper = mountView()
      await flushPromises()
      expect(getOrder).not.toHaveBeenCalled()
      expect(loadStripe).not.toHaveBeenCalled()
      expect(wrapper.text()).toContain('Storage blocked')
    } finally {
      getItem.mockRestore()
    }
  })

  it.each(['alipay', 'wechat_pay', ''])('does not start %s payment after leaving during SDK loading', async (method) => {
    routeState.query.method = method
    let resolveSDK!: (value: typeof stripeInstance) => void
    loadStripe.mockReturnValueOnce(new Promise((resolve) => { resolveSDK = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    expect(loadStripe).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolveSDK(stripeInstance)
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmAlipayPayment).not.toHaveBeenCalled()
    expect(stripeInstance.confirmWechatPayPayment).not.toHaveBeenCalled()
    expect(stripeInstance.elements).not.toHaveBeenCalled()
  })

  it('does not restart polling when a WeChat QR result arrives after leaving', async () => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    let resolvePayment!: (value: unknown) => void
    stripeInstance.confirmWechatPayPayment.mockReturnValueOnce(new Promise((resolve) => { resolvePayment = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    expect(stripeInstance.confirmWechatPayPayment).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolvePayment({ paymentIntent: { status: 'requires_action', next_action: {
      wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,test' }
    } } })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(paymentStore.pollOrderStatus).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([false, true])('handles a completed poll with page unmounted=%s', async (unmounted) => {
    vi.useFakeTimers()
    routeState.query.method = 'wechat_pay'
    stripeInstance.confirmWechatPayPayment.mockResolvedValueOnce({ paymentIntent: {
      status: 'requires_action', next_action: { wechat_pay_display_qr_code: { image_data_url: 'data:image/png;base64,test' } }
    } })
    let resolvePoll!: (value: PaymentOrder) => void
    paymentStore.pollOrderStatus.mockReturnValueOnce(new Promise((resolve) => { resolvePoll = resolve }))
    const wrapper = mountView()
    await flushPromises()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(paymentStore.pollOrderStatus).toHaveBeenCalledTimes(1)
    if (unmounted) wrapper.unmount()
    resolvePoll(orderFactory({ status: 'PAID' }))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(routerPush).toHaveBeenCalledTimes(unmounted ? 0 : 1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('本地恢复快照未记录币种时使用订单接口返回的 Stripe 币种展示金额', async () => {
    writeSnapshot({ currency: '' })
    getOrder.mockResolvedValue({
      data: orderFactory({ currency: 'HKD', pay_amount: 103 }),
    })

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(getOrder).toHaveBeenCalledWith(42)
    expect(loadStripe).toHaveBeenCalledWith('pk_test')
    expect(wrapper.text()).toContain(formatPaymentAmount(103, 'HKD', 'zh-CN'))
  })
})
