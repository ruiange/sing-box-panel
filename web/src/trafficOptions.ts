const historyRanges = [
  { value: 'hours=6', label: '近 6 小时' },
  { value: 'hours=12', label: '近 12 小时' },
  { value: 'hours=24', label: '近 24 小时' },
  { value: 'hours=48', label: '近 48 小时' },
  { value: 'hours=72', label: '近 72 小时' },
  { value: 'days=7', label: '近 7 天' },
  { value: 'days=30', label: '近 30 天' },
] as const

export type HistoryRange = (typeof historyRanges)[number]['value']
export const ranges: Array<{ value: HistoryRange; label: string }> = [...historyRanges]
export const defaultRange: HistoryRange = 'days=7'

export const liveLabels = {
  connecting: '正在连接实时速率…',
  waiting: '等待速率采样',
  live: '实时速率更新中',
  reconnecting: '正在重新连接实时速率…',
} as const
