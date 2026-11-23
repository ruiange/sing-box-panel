import type { Usage } from './api'

const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB']
const unitStep = 1000n

export function totalUsage(value: Usage) {
  return BigInt(value.upload) + BigInt(value.download)
}

export function formatBytes(value: string | number | bigint) {
  const bytes = typeof value === 'bigint' ? value : BigInt(typeof value === 'number' ? Math.max(0, Math.round(value)) : value)
  let factor = 1n
  let unit = 0
  while (unit < units.length - 1 && bytes >= factor * unitStep) {
    factor *= unitStep
    unit += 1
  }
  const fixed = Number(bytes * 100n / factor) / 100
  const amount = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: unit === 0 ? 0 : 2 }).format(fixed)
  return amount + ' ' + units[unit]
}

export function formatTime(value: string, timeZone?: string) {
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

export function formatRate(bytesPerSecond: number) {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond < 0) return '—'
  if (bytesPerSecond > 0 && bytesPerSecond < 1) return '< 1 B/s'
  return formatBytes(bytesPerSecond) + '/s'
}

export function shortDay(value: string) {
  const [year, month, day] = value.split('-')
  return year && month && day ? Number(month) + '/' + Number(day) : value
}
