export interface Device {
  id: string
  name: string
  enabled: boolean
  createdAt: string
}

export interface Usage {
  upload: string
  download: string
}

export interface PeriodUsage {
  today: Usage
  month: Usage
  all: Usage
}

export interface DailyUsage extends Usage {
  date: string
}

export interface HourlyUsage extends Usage {
  at: string
}

export interface LiveRate {
  upload: number
  download: number
}

export interface LiveRatePoint extends LiveRate {
  at: string
}

export interface DevicePresence {
  state: 'active' | 'idle' | 'unknown'
  lastActiveAt: string | null
}

export interface LiveTraffic {
  at: string | null
  devices: Record<string, LiveRate>
  deviceId: string
  history: LiveRatePoint[]
  presence?: Record<string, DevicePresence>
}

export type SubscriptionFormat = 'clash' | 'sing-box' | 'v2ray' | 'shadowrocket'

export interface Traffic {
  timeZone: string
  collectedAt: string | null
  collectorStatus: 'waiting' | 'ok' | 'error'
  totals: PeriodUsage
  daily: DailyUsage[]
  hourly?: HourlyUsage[]
  hourlySince?: string | null
  devices: Array<PeriodUsage & { id: string }>
}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly hasEnvelope = true,
  ) {
    super(message)
  }
}

export async function api<T>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
  let response: Response
  try {
    response = await fetch('/api' + path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: 'no-store',
      signal,
    })
  } catch {
    throw new APIError('无法连接面板服务。请检查服务是否启动。', 0, false)
  }
  if (response.status === 204) return undefined as T
  const result = await response.json().catch(() => null)
  if (!response.ok || result?.code !== 0) {
    const message = result?.message || (response.status === 401 ? '登录已过期，请重新登录。' : '面板服务未返回有效响应。')
    const hasEnvelope = typeof result?.code === 'number' && typeof result?.message === 'string'
    throw new APIError(message, response.status, hasEnvelope)
  }
  return result.data as T
}

export async function fetchSubscriptionQR(
  deviceId: string,
  format: SubscriptionFormat,
  signal: AbortSignal,
): Promise<Blob> {
  let response: Response
  try {
    response = await fetch('/api/devices/' + encodeURIComponent(deviceId) + '/subscription-qr?format=' + format, {
      credentials: 'same-origin',
      cache: 'no-store',
      signal,
    })
  } catch (error) {
    if (signal.aborted) throw error
    throw new APIError('无法连接面板服务。', 0)
  }
  if (!response.ok || response.headers.get('Content-Type')?.split(';')[0] !== 'image/png') {
    const result = await response.json().catch(() => null)
    throw new APIError(result?.message || '暂时无法获取订阅二维码。', response.status)
  }
  return response.blob()
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '操作未完成，请重试。'
}
