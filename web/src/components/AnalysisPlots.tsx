import { memo, useMemo } from 'react'
import { Area, Column, Pie } from '@ant-design/plots'
import type { Usage } from '../api'
import { formatBytes, totalUsage } from '../format'
import type { AnalysisBucket, DeviceStatusCount } from '../trafficAnalysis'

const directions = [
  { key: 'download', label: '下载', color: '#2468d6' },
  { key: 'upload', label: '上传', color: '#118381' },
] as const

type Direction = (typeof directions)[number]['key']
type TrendMetric = Direction | 'total'

const metricLabels = { total: '总流量', download: '下载流量', upload: '上传流量' }

const statusColors = {
  active: '#2b8a52',
  idle: '#2468d6',
  disabled: '#8492a6',
  unknown: '#a9630e',
} satisfies Record<DeviceStatusCount['status'], string>

const directionScale = {
  domain: directions.map(({ label }) => label),
  range: directions.map(({ color }) => color),
}
const statusScale = { domain: Object.keys(statusColors), range: Object.values(statusColors) }

const axis = { title: false, tick: false, labelFill: '#57657a', labelFontSize: 11 }
const byteAxis = {
  ...axis,
  grid: true,
  gridStroke: '#e0e6ee',
  gridLineDash: [3, 4],
  tickCount: 4,
  tickFilter: (value: unknown) => Number.isInteger(Number(value)),
  labelAutoHide: true,
  labelFormatter: (value: unknown) => formatBytes(Number(value)),
}

const usageOptions = {
  xField: 'key',
  yField: 'bytes',
  colorField: 'direction',
  autoFit: true,
  animate: false as const,
  scale: { color: directionScale, y: { domainMin: 0, nice: true } },
}

const donutOptions = {
  radius: 0.92,
  innerRadius: 0.68,
  autoFit: true,
  animate: false as const,
  legend: false as const,
  label: false as const,
  style: { stroke: '#fff', lineWidth: 2 },
}

interface UsagePoint {
  key: string
  label: string
  direction: (typeof directions)[number]['label']
  bytes: number
  exactBytes: string
}

function usagePoints(usage: Usage, key: string, label: string, metric: TrendMetric = 'total'): UsagePoint[] {
  return directions.filter(({ key: field }) => metric === 'total' || metric === field)
    .map(({ key: field, label: direction }) => ({
      key,
      label,
      direction,
      bytes: Number(usage[field]),
      exactBytes: usage[field],
    }))
}

const usageTooltip = {
  title: 'label',
  items: [(point: UsagePoint) => ({ name: point.direction, value: formatBytes(point.exactBytes) })],
}

export const AnalysisTrendPlot = memo(function AnalysisTrendPlot({
  buckets, hourly, metric,
}: { buckets: AnalysisBucket[]; hourly: boolean; metric: TrendMetric }) {
  const config = useMemo(() => {
    const labels = new Map(buckets.map(({ key, label }) => [key, label]))
    const stacked = metric === 'total'
    return {
      ...usageOptions,
      stack: stacked,
      legend: stacked ? { color: { position: 'top' } } : false as const,
      data: buckets.flatMap((bucket) => usagePoints(bucket, bucket.key, bucket.label, metric)),
      scale: { ...usageOptions.scale, x: { domain: [...labels.keys()] } },
      axis: {
        x: { ...axis, labelAutoHide: true, labelFormatter: (key: unknown) => labels.get(String(key)) ?? String(key) },
        y: byteAxis,
      },
      tooltip: usageTooltip,
    }
  }, [buckets, metric])

  return (
    <div className="analysis-plot analysis-trend-plot" role="img" aria-label={`${hourly ? '每小时' : '每日'}${metricLabels[metric]}趋势`}>
      <Column {...config} style={{ maxWidth: 26 }} />
    </div>
  )
})

export const AnalysisMiniPlot = memo(function AnalysisMiniPlot({
  buckets, direction,
}: { buckets: AnalysisBucket[]; direction: Direction }) {
  const config = useMemo(() => ({
    ...usageOptions,
    data: buckets.flatMap((bucket) => usagePoints(bucket, bucket.key, bucket.label, direction)),
    padding: 0,
    axis: false as const,
    legend: false as const,
    tooltip: usageTooltip,
  }), [buckets, direction])

  return (
    <div className="analysis-mini-plot" role="img" aria-label={`${metricLabels[direction]}区间趋势`}>
      {direction === 'download' ? (
        <Area
          {...config}
          style={{ fillOpacity: 0.3 }}
          line={{ style: { lineWidth: 1.5 } }}
          point={buckets.length === 1 ? { sizeField: 2 } : undefined}
        />
      ) : <Column {...config} style={{ maxWidth: 14 }} />}
    </div>
  )
})

const percentFormat = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 2 })

export const AnalysisDirectionPlot = memo(function AnalysisDirectionPlot({ totals }: { totals: Usage }) {
  const total = totalUsage(totals)
  const data = usagePoints(totals, '', '').filter(({ bytes }) => bytes > 0)
  return (
    <div className="analysis-plot analysis-direction-plot" role="img" aria-label="上传和下载流量占比">
      <Pie
        {...donutOptions}
        data={data}
        angleField="bytes"
        colorField="direction"
        scale={{ color: directionScale }}
        tooltip={{
          title: 'direction',
          items: [(point: UsagePoint) => {
            const basisPoints = (BigInt(point.exactBytes) * 10_000n + total / 2n) / total
            return { name: '流量', value: `${formatBytes(point.exactBytes)}（${percentFormat.format(Number(basisPoints) / 10_000)}）` }
          }],
        }}
      />
    </div>
  )
})

export const AnalysisDeviceStatusPlot = memo(function AnalysisDeviceStatusPlot({ counts }: { counts: DeviceStatusCount[] }) {
  return (
    <div className="analysis-plot analysis-status-plot" role="img" aria-label="设备实时状态分布">
      <Pie
        {...donutOptions}
        data={counts.filter(({ count }) => count > 0)}
        angleField="count"
        colorField="status"
        scale={{ color: statusScale }}
        tooltip={{
          title: 'label',
          items: [({ count }: DeviceStatusCount) => ({ name: '设备数', value: `${count} 台` })],
        }}
      />
    </div>
  )
}, (previous, current) => (
  previous.counts.length === current.counts.length && previous.counts.every(({ status, label, count }, index) => (
    status === current.counts[index].status && label === current.counts[index].label && count === current.counts[index].count
  ))
))
