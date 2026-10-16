import type {
  CreateOrderRequest,
  CreateOrderResult,
  MethodLimit,
  OrderType,
  WechatJSAPIPayload,
  WechatOAuthInfo,
} from '@/types/payment'

export const PAYMENT_RECOVERY_STORAGE_KEY = 'payment.recovery.current'

const VISIBLE_METHOD_ALIASES = {
  alipay: 'alipay',
  alipay_direct: 'alipay',
  wxpay: 'wxpay',
  wxpay_direct: 'wxpay',
  stripe: 'stripe',
  airwallex: 'airwallex',
} as const

export type VisiblePaymentMethod = 'alipay' | 'wxpay' | 'stripe' | 'airwallex'
export type StripeVisibleMethod = 'alipay' | 'wechat_pay'
export type PaymentLaunchKind =
  | 'qr_waiting'
  | 'alipay_deep_link'
  | 'redirect_waiting'
  | 'stripe_popup'
  | 'stripe_route'
  | 'airwallex_route'
  | 'wechat_oauth'
  | 'wechat_jsapi'
  | 'unhandled'

export interface PaymentRecoverySnapshot {
  orderId: number
  amount: number
  qrCode: string
  expiresAt: string
  paymentType: string
  payUrl: string
  outTradeNo: string
  clientSecret: string
  intentId: string
  currency: string
  countryCode: string
  paymentEnv: string
  payAmount: number
  orderType: OrderType | ''
  paymentMode: string
  resumeToken: string
  alipayMobilePrecreateDeepLink?: boolean
  createdAt: number
}

export interface PaymentLaunchContext {
  visibleMethod: string
  orderType: OrderType
  isMobile: boolean
  isWechatBrowser?: boolean
  /** When true, Alipay payments always use QR code regardless of device type */
  forceQRCode?: boolean
  /** When true, the new mobile Alipay precreate flow takes priority over forceQRCode */
  mobilePrecreateDeepLink?: boolean
  now?: number
  stripePopupUrl?: string
  stripeRouteUrl?: string
  airwallexRouteUrl?: string
}

export interface PaymentLaunchDecision {
  kind: PaymentLaunchKind
  paymentState: PaymentRecoverySnapshot
  recovery: PaymentRecoverySnapshot
  stripeMethod?: StripeVisibleMethod
  oauth?: WechatOAuthInfo
  jsapi?: WechatJSAPIPayload
}

export interface BuildCreateOrderPayloadInput {
  amount: number
  paymentType: string
  orderType: OrderType
  planId?: number
  origin?: string
  isMobile: boolean
  isWechatBrowser: boolean
  /** When true, Alipay payments always use QR code (passes is_mobile: false to backend) */
  forceQRCode?: boolean
  /** When true, keep the real mobile signal so the backend can select precreate */
  mobilePrecreateDeepLink?: boolean
}

type CreateOrderFlowResult = CreateOrderResult & {
  resume_token?: string
}

type StorageWriter = Pick<Storage, 'getItem' | 'removeItem' | 'setItem'>

export function normalizeVisibleMethod(method: string): VisiblePaymentMethod | '' {
  const normalized = VISIBLE_METHOD_ALIASES[method.trim() as keyof typeof VISIBLE_METHOD_ALIASES]
  return normalized ?? ''
}

export function getVisibleMethods(methods: Record<string, MethodLimit>): Record<string, MethodLimit> {
  const visible: Record<string, MethodLimit> = {}

  Object.entries(methods).forEach(([type, limit]) => {
    const normalized = normalizeVisibleMethod(type) || type.trim()
    if (!normalized) return

    const isCanonical = type === normalized
    const existing = visible[normalized]
    if (!existing || isCanonical) {
      visible[normalized] = { ...limit }
    }
  })

  return visible
}

export function buildCreateOrderPayload(input: BuildCreateOrderPayloadInput): CreateOrderRequest {
  const visibleMethod = normalizeVisibleMethod(input.paymentType) || input.paymentType.trim()
  const normalizedOrigin = (input.origin || '').trim().replace(/\/+$/, '')
  // When forceQRCode is enabled for alipay, always tell the backend this is not a mobile
  // request so it generates a QR code instead of a mobile-redirect URL.
  const effectiveMobile = (input.forceQRCode && !input.mobilePrecreateDeepLink && visibleMethod === 'alipay')
    ? false
    : input.isMobile
  const payload: CreateOrderRequest = {
    amount: input.amount,
    payment_type: visibleMethod,
    order_type: input.orderType,
    is_mobile: effectiveMobile,
    payment_source: visibleMethod === 'wxpay' && input.isWechatBrowser
      ? 'wechat_in_app_resume'
      : 'hosted_redirect',
  }

  if (input.planId) {
    payload.plan_id = input.planId
  }
  if (normalizedOrigin) {
    payload.return_url = `${normalizedOrigin}/payment/result`
  }

  return payload
}

export function decidePaymentLaunch(
  result: CreateOrderFlowResult,
  context: PaymentLaunchContext,
): PaymentLaunchDecision {
  const visibleMethod = normalizeVisibleMethod(context.visibleMethod) || context.visibleMethod
  const baseState = createPaymentRecoverySnapshot({
    orderId: result.order_id,
    amount: result.amount,
    qrCode: result.qr_code || '',
    expiresAt: result.expires_at || '',
    paymentType: visibleMethod,
    payUrl: result.pay_url || '',
    outTradeNo: result.out_trade_no || '',
    clientSecret: result.client_secret || '',
    intentId: result.intent_id || '',
    currency: result.currency || '',
    countryCode: result.country_code || '',
    paymentEnv: result.payment_env || '',
    payAmount: result.pay_amount,
    orderType: context.orderType,
    paymentMode: (result.payment_mode || '').trim(),
    resumeToken: result.resume_token || '',
    alipayMobilePrecreateDeepLink: result.alipay_mobile_precreate_deep_link === true,
  }, context.now)

  if (visibleMethod === 'airwallex' && baseState.clientSecret && baseState.intentId) {
    if (!context.airwallexRouteUrl) {
      return { kind: 'unhandled', paymentState: baseState, recovery: baseState }
    }
    const paymentState = { ...baseState, payUrl: context.airwallexRouteUrl || '' }
    return { kind: 'airwallex_route', paymentState, recovery: paymentState }
  }

  if (baseState.clientSecret) {
    // visibleMethod === 'stripe' means the user clicked the dedicated Stripe button
    // and should land on the full Payment Element to choose a sub-method themselves.
    const isStripeButton = visibleMethod === 'stripe'
    const stripeMethod: StripeVisibleMethod | undefined = isStripeButton
      ? undefined
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const kind: PaymentLaunchKind = stripeMethod === 'alipay' && !context.isMobile
      ? 'stripe_popup'
      : 'stripe_route'
    const payUrl = kind === 'stripe_popup'
      ? context.stripePopupUrl || context.stripeRouteUrl || ''
      : context.stripeRouteUrl || context.stripePopupUrl || ''
    const paymentState = { ...baseState, payUrl }
    return { kind, paymentState, recovery: paymentState, stripeMethod }
  }

  if (result.result_type === 'oauth_required' && result.oauth?.authorize_url) {
    return { kind: 'wechat_oauth', paymentState: baseState, recovery: baseState, oauth: result.oauth }
  }

  const jsapiPayload = result.jsapi ?? result.jsapi_payload
  if (result.result_type === 'jsapi_ready' && jsapiPayload) {
    return { kind: 'wechat_jsapi', paymentState: baseState, recovery: baseState, jsapi: jsapiPayload }
  }

  if (
    visibleMethod === 'alipay'
    && context.isMobile
    && baseState.alipayMobilePrecreateDeepLink
    && baseState.qrCode
  ) {
    return { kind: 'alipay_deep_link', paymentState: baseState, recovery: baseState }
  }

  const normalizedPaymentMode = baseState.paymentMode.trim().toLowerCase()
  // When forceQRCode is on for alipay, treat the device as desktop so the mobile-redirect
  // branch is bypassed and we fall through to qr_waiting.
  const effectiveMobile = (context.forceQRCode && !context.mobilePrecreateDeepLink && visibleMethod === 'alipay')
    ? false
    : context.isMobile
  const prefersRedirect = normalizedPaymentMode === 'redirect'
    || normalizedPaymentMode === 'popup'
    || (effectiveMobile && !!baseState.payUrl)
  const prefersQr = normalizedPaymentMode === 'qrcode'
    || normalizedPaymentMode === 'native'
    || (!prefersRedirect && !!baseState.qrCode)

  if (visibleMethod === 'wxpay' && context.isWechatBrowser && baseState.payUrl && !baseState.qrCode) {
    return { kind: 'redirect_waiting', paymentState: baseState, recovery: baseState }
  }

  if (prefersRedirect && baseState.payUrl) {
    return { kind: 'redirect_waiting', paymentState: baseState, recovery: baseState }
  }

  if (prefersQr && baseState.qrCode) {
    return { kind: 'qr_waiting', paymentState: baseState, recovery: baseState }
  }

  if (baseState.payUrl) {
    return { kind: 'redirect_waiting', paymentState: baseState, recovery: baseState }
  }

  return { kind: 'unhandled', paymentState: baseState, recovery: baseState }
}

export function createPaymentRecoverySnapshot(
  state: Omit<PaymentRecoverySnapshot, 'createdAt'>,
  now = Date.now(),
): PaymentRecoverySnapshot {
  return {
    ...state,
    createdAt: now,
  }
}

export function writePaymentRecoverySnapshot(
  storage: StorageWriter,
  snapshot: PaymentRecoverySnapshot,
  key = PAYMENT_RECOVERY_STORAGE_KEY,
): void {
  const previous = [
    ...readStripeRecoverySnapshots(storage.getItem(`${key}.stripe`)),
    readPaymentRecoverySnapshot(storage.getItem(key)),
  ].filter(saved => saved?.orderId !== snapshot.orderId)
  const snapshots = readStripeRecoverySnapshots(JSON.stringify([...previous, snapshot]))
  const byOrder = new Map(snapshots.map(saved => [saved.orderId, saved]))
  storeStripeRecoverySnapshots(storage, [...byOrder.values()], key)
  storage.setItem(key, JSON.stringify(snapshot))
}

export function clearPaymentRecoverySnapshot(
  storage: StorageWriter,
  key = PAYMENT_RECOVERY_STORAGE_KEY,
  orderId?: number,
): void {
  const current = readPaymentRecoverySnapshot(storage.getItem(key))
  const clearedOrderId = orderId ?? current?.orderId
  if (!orderId || !current || current.orderId === orderId) {
    storage.removeItem(key)
  }
  storeStripeRecoverySnapshots(storage, readStripeRecoverySnapshots(storage.getItem(`${key}.stripe`))
    .filter(saved => saved.orderId !== clearedOrderId), key)
}

function readStripeRecoverySnapshots(raw: string | null): PaymentRecoverySnapshot[] {
  try {
    const entries: unknown = JSON.parse(raw || '[]')
    if (!Array.isArray(entries)) return []
    return entries.map(entry => readPaymentRecoverySnapshot(JSON.stringify(entry)))
      .filter((snapshot): snapshot is PaymentRecoverySnapshot =>
        !!snapshot?.clientSecret && snapshot.paymentType !== 'airwallex')
  } catch {
    return []
  }
}

function storeStripeRecoverySnapshots(
  storage: Pick<Storage, 'removeItem' | 'setItem'>,
  snapshots: PaymentRecoverySnapshot[],
  key: string,
): void {
  if (snapshots.length) {
    storage.setItem(`${key}.stripe`, JSON.stringify(snapshots))
  } else {
    storage.removeItem(`${key}.stripe`)
  }
}

/** Keep pending Stripe checkouts independent when another tab replaces the current order. */
export function readStripePaymentRecoverySnapshot(
  storage: Pick<Storage, 'getItem'>,
  orderId: number,
  options: { resumeToken?: string } = {},
): PaymentRecoverySnapshot | null {
  const current = readPaymentRecoverySnapshot(storage.getItem(PAYMENT_RECOVERY_STORAGE_KEY), options)
  if (current?.orderId === orderId && current.paymentType !== 'airwallex') return current
  const saved = readStripeRecoverySnapshots(storage.getItem(`${PAYMENT_RECOVERY_STORAGE_KEY}.stripe`))
    .find(snapshot => snapshot.orderId === orderId)
  return saved ? readPaymentRecoverySnapshot(JSON.stringify(saved), options) : null
}

export function readPaymentRecoverySnapshot(
  raw: string | null | undefined,
  options: { now?: number; resumeToken?: string } = {},
): PaymentRecoverySnapshot | null {
  if (!raw) return null

  try {
    const parsed = JSON.parse(raw) as Partial<PaymentRecoverySnapshot>
    if (
      typeof parsed.orderId !== 'number'
      || typeof parsed.amount !== 'number'
      || typeof parsed.qrCode !== 'string'
      || typeof parsed.expiresAt !== 'string'
      || typeof parsed.paymentType !== 'string'
      || typeof parsed.payUrl !== 'string'
      || (parsed.outTradeNo != null && typeof parsed.outTradeNo !== 'string')
      || typeof parsed.clientSecret !== 'string'
      || (parsed.intentId != null && typeof parsed.intentId !== 'string')
      || (parsed.currency != null && typeof parsed.currency !== 'string')
      || (parsed.countryCode != null && typeof parsed.countryCode !== 'string')
      || (parsed.paymentEnv != null && typeof parsed.paymentEnv !== 'string')
      || typeof parsed.payAmount !== 'number'
      || typeof parsed.paymentMode !== 'string'
      || typeof parsed.resumeToken !== 'string'
      || (parsed.alipayMobilePrecreateDeepLink != null && typeof parsed.alipayMobilePrecreateDeepLink !== 'boolean')
      || typeof parsed.createdAt !== 'number'
    ) {
      return null
    }

    const now = options.now ?? Date.now()
    const expiresAt = Date.parse(parsed.expiresAt)
    if (Number.isFinite(expiresAt) && expiresAt <= now) {
      return null
    }
    if (options.resumeToken && parsed.resumeToken !== options.resumeToken) {
      return null
    }

    return {
      orderId: parsed.orderId,
      amount: parsed.amount,
      qrCode: parsed.qrCode,
      expiresAt: parsed.expiresAt,
      paymentType: parsed.paymentType,
      payUrl: parsed.payUrl,
      outTradeNo: parsed.outTradeNo || '',
      clientSecret: parsed.clientSecret,
      intentId: parsed.intentId || '',
      currency: parsed.currency || '',
      countryCode: parsed.countryCode || '',
      paymentEnv: parsed.paymentEnv || '',
      payAmount: parsed.payAmount,
      orderType: parsed.orderType === 'subscription' ? 'subscription' : 'balance',
      paymentMode: parsed.paymentMode,
      resumeToken: parsed.resumeToken,
      alipayMobilePrecreateDeepLink: parsed.alipayMobilePrecreateDeepLink === true,
      createdAt: parsed.createdAt,
    }
  } catch {
    return null
  }
}
