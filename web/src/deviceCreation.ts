import { api, APIError, type Device } from './api'

const storageKey = 'sing-box-panel:pending-device-creates'
const requestIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

export type DeviceCreateRequest = { name: string; requestId: string }

const pending = new Map<string, DeviceCreateRequest>()
try {
  const saved: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? '[]')
  if (Array.isArray(saved)) {
    for (const request of saved) {
      if (typeof request?.name === 'string' && typeof request.requestId === 'string' && requestIdPattern.test(request.requestId)) {
        pending.set(request.name, request)
      }
    }
  }
} catch {
  // In-memory requests still survive a retry in this page when storage is unavailable.
}

function savePending() {
  try {
    if (pending.size) window.sessionStorage.setItem(storageKey, JSON.stringify([...pending.values()]))
    else window.sessionStorage.removeItem(storageKey)
  } catch {
    // Private browser settings may disable storage.
  }
}

export function hasPendingDeviceCreation(name: string): boolean {
  return pending.has(name)
}

export function beginDeviceCreation(name: string): DeviceCreateRequest {
  const previous = pending.get(name)
  if (previous) return previous

  // getRandomValues also works when the panel is served over plain HTTP.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
  const requestId = [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join('-')
  const request = { name, requestId }
  pending.set(name, request)
  savePending()
  return request
}

export function confirmDeviceCreations(devices: Device[]) {
  if (!pending.size) return
  const ids = new Set(devices.map((device) => device.id))
  let changed = false
  for (const [name, request] of pending) {
    if (ids.has(request.requestId)) changed = pending.delete(name) || changed
  }
  if (changed) savePending()
}

function canRetryCreate(error: unknown): boolean {
  if (!(error instanceof APIError)) return false
  const { status, hasEnvelope } = error
  return status === 0 || status === 408 || status === 502 || status === 504 ||
    (!hasEnvelope && (status >= 500 || (status >= 200 && status < 300)))
}

async function requestWithDeadline<T>(path: string, timeout: number, method = 'GET', body?: unknown): Promise<T> {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), timeout)
  try {
    return await api<T>(path, method, body, controller.signal)
  } finally {
    window.clearTimeout(timer)
  }
}

async function postDevice(request: DeviceCreateRequest): Promise<Device> {
  const device = await requestWithDeadline<Device>('/devices', 45_000, 'POST', request)
  if (device?.id !== request.requestId) throw new APIError('未收到完整的创建设备结果。', 0, false)
  return device
}

export async function submitDeviceCreation(request: DeviceCreateRequest, stillCurrent: () => boolean): Promise<Device> {
  try {
    return await postDevice(request)
  } catch (error) {
    if (!canRetryCreate(error) || !stillCurrent()) throw error
  }

  // Applying a device may briefly restart the proxy carrying the browser connection.
  await new Promise<void>((resolve) => window.setTimeout(resolve, 1_000))
  const outcomeUnknown = new Error('暂时无法确认提交结果。连接恢复后，可用原设备名称重试。')
  if (!stillCurrent()) throw outcomeUnknown
  try {
    const devices = await requestWithDeadline<Device[]>('/devices', 8_000)
    const existing = devices.find((device) => device.id === request.requestId)
    if (existing) return existing
  } catch (error) {
    if (error instanceof APIError && error.status === 401) throw error
  }

  if (!stillCurrent()) throw outcomeUnknown
  try {
    return await postDevice(request)
  } catch (error) {
    if (!canRetryCreate(error)) throw error
    throw outcomeUnknown
  }
}
