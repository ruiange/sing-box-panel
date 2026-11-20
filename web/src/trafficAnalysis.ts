import type { Device, DevicePresence, PeriodUsage, Traffic, Usage } from './api'
import { formatTime, shortDay, totalUsage } from './format'

export type AnalysisBucket = Usage & { key: string; label: string }
export type RankedDevice = Usage & { id: string; name: string }

const statusLabels = {
  active: '活跃',
  idle: '暂无活动',
  disabled: '已停用',
  unknown: '状态未知',
} as const

type DeviceStatus = keyof typeof statusLabels
export type DeviceStatusCount = { status: DeviceStatus; label: string; count: number }

export function analyzeTraffic(traffic: Traffic | undefined, hourly: boolean) {
  if (!traffic) return undefined
  const source = hourly ? traffic.hourly ?? [] : traffic.daily
  const buckets = source.map<AnalysisBucket>((bucket) => ({
    key: 'at' in bucket ? bucket.at : bucket.date,
    label: 'at' in bucket ? formatTime(bucket.at, traffic.timeZone) : shortDay(bucket.date),
    upload: bucket.upload,
    download: bucket.download,
  }))
  if (!buckets.length) return undefined
  if (hourly) distinguishRepeatedHours(buckets)

  let upload = 0n
  let download = 0n
  let peakBytes = 0n
  let peak: AnalysisBucket | undefined
  for (const bucket of buckets) {
    const up = BigInt(bucket.upload)
    const down = BigInt(bucket.download)
    const total = up + down
    upload += up
    download += down
    if (total > peakBytes) {
      peakBytes = total
      peak = bucket
    }
  }

  return { buckets, totals: { upload: upload.toString(), download: download.toString() }, peak }
}

function distinguishRepeatedHours(buckets: AnalysisBucket[]) {
  const counts = new Map<string, number>()
  for (const { label } of buckets) counts.set(label, (counts.get(label) ?? 0) + 1)
  for (const bucket of buckets) {
    if ((counts.get(bucket.label) ?? 0) < 2) continue
    const offset = bucket.key.match(/(?:Z|[+-]\d{2}:\d{2})$/)?.[0]
    if (offset) bucket.label += ` (UTC${offset === 'Z' ? '+00:00' : offset})`
  }
}

export function rankDevices(
  devices: Device[],
  usage: Traffic['devices'] | undefined,
  period: keyof PeriodUsage,
): RankedDevice[] {
  if (!usage?.length || !devices.length) return []
  const byId = new Map(usage.map((entry) => [entry.id, entry[period]]))
  const rows = devices.flatMap(({ id, name }) => {
    const bytes = byId.get(id)
    if (!bytes) return []
    const total = totalUsage(bytes)
    return total > 0n ? [{ id, name, bytes, total }] : []
  })
  rows.sort((left, right) => {
    if (left.total === right.total) return left.name.localeCompare(right.name, 'zh-CN') || left.id.localeCompare(right.id)
    return left.total > right.total ? -1 : 1
  })
  return rows.slice(0, 8).map(({ id, name, bytes }) => ({ id, name, ...bytes }))
}

export function countDeviceStates(devices: Device[], presence?: Record<string, DevicePresence>): DeviceStatusCount[] {
  const counts = { active: 0, idle: 0, disabled: 0, unknown: 0 }
  for (const device of devices) {
    const status = device.enabled ? presence?.[device.id]?.state : 'disabled'
    if (status === 'active' || status === 'idle' || status === 'disabled') counts[status] += 1
    else counts.unknown += 1
  }
  return [
    { status: 'active', label: statusLabels.active, count: counts.active },
    { status: 'idle', label: statusLabels.idle, count: counts.idle },
    { status: 'disabled', label: statusLabels.disabled, count: counts.disabled },
    { status: 'unknown', label: statusLabels.unknown, count: counts.unknown },
  ]
}
