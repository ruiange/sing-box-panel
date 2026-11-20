import { memo, useMemo } from 'react'
import { Column, Line, type ColumnConfig, type LineConfig } from '@ant-design/plots'
import type { DailyUsage, HourlyUsage, LiveRatePoint } from '../api'
import { formatBytes, formatRate, formatTime, shortDay } from '../format'

const directions = [
  { key: 'download', label: '下载', color: '#2468d6' },
  { key: 'upload', label: '上传', color: '#118381' },
] as const

type Direction = (typeof directions)[number]['label']

interface HistoryPoint {
  timestamp: string
  label: string
  direction: Direction
  bytes: number
  exactBytes: string
}

interface LivePoint {
  second: number
  direction: Direction
  rate: number | null
}

const colorScale = {
  domain: directions.map(({ label }) => label),
  range: directions.map(({ color }) => color),
}

const xAxis = { title: false as const, tick: false, labelAutoHide: true, labelFill: '#57657a', labelFontSize: 11 }
const yAxis = {
  ...xAxis,
  grid: true,
  gridLineDash: [3, 4] as [number, number],
  gridStroke: '#c9d3e1',
  tickCount: 5,
}

const legend = { color: { position: 'top' as const } }

function liveTimeLabel(value: number) {
  const elapsed = 60 - Math.round(value)
  return elapsed === 0 ? '现在' : `${elapsed} 秒前`
}

export const HistoryPlot = memo(function HistoryPlot({
  daily, hourly, timeZone,
}: { daily?: DailyUsage[]; hourly?: HourlyUsage[]; timeZone?: string }) {
  const config = useMemo<ColumnConfig>(() => {
    const values = hourly ?? daily ?? []
    const labels = new Map<string, string>()
    const data = values.flatMap<HistoryPoint>((point) => {
      const timestamp = 'at' in point ? point.at : point.date
      const label = 'at' in point ? formatTime(timestamp, timeZone) : shortDay(timestamp)
      labels.set(timestamp, label)
      return directions.map(({ key, label: direction }) => ({
        timestamp, label, direction, bytes: Number(point[key]), exactBytes: point[key],
      }))
    })

    return {
      data,
      xField: 'timestamp',
      yField: 'bytes',
      colorField: 'direction',
      group: true,
      autoFit: true,
      animate: false,
      legend,
      scale: { x: { domain: [...labels.keys()] }, color: colorScale, y: { domainMin: 0, nice: true } },
      axis: {
        x: { ...xAxis, labelFormatter: (value: unknown) => labels.get(String(value)) ?? String(value) },
        y: { ...yAxis, labelFormatter: (value: unknown) => formatBytes(Number(value)) },
      },
      style: { maxWidth: 24, radiusTopLeft: 3, radiusTopRight: 3 },
      tooltip: {
        title: 'label',
        items: [(point: HistoryPoint) => ({ name: point.direction, value: formatBytes(point.exactBytes) })],
      },
    }
  }, [daily, hourly, timeZone])

  return (
    <div className="traffic-history-plot" role="img" aria-label={hourly ? '每小时上传和下载流量柱状图' : '每日上传和下载流量柱状图'}>
      <Column {...config} />
    </div>
  )
})

export function LiveRatePlot({ history, end }: { history: LiveRatePoint[]; end: number }) {
  const data = useMemo(() => {
    const start = end - 60_000
    const values = history.map((sample) => ({ ...sample, time: Date.parse(sample.at) }))
      .filter(({ time }) => time >= start && time <= end)
      .sort((left, right) => left.time - right.time)

    return directions.flatMap(({ key, label: direction }) => {
      const points: LivePoint[] = []
      for (const sample of values) {
        const second = (sample.time - start) / 1000
        const previous = points.at(-1)
        if (previous && second - previous.second > 4) {
          points.push({ second: previous.second + 1, direction, rate: null })
        }
        points.push({ second, direction, rate: sample[key] })
      }
      return points
    })
  }, [end, history])

  const config: LineConfig = {
    data,
    xField: 'second',
    yField: 'rate',
    colorField: 'direction',
    autoFit: true,
    animate: false,
    legend,
    scale: {
      x: { type: 'linear', domain: [0, 60], tickMethod: () => [0, 15, 30, 45, 60] },
      y: { domainMin: 0, nice: true },
      color: colorScale,
    },
    axis: {
      x: { ...xAxis, labelFormatter: (value: unknown) => liveTimeLabel(Number(value)) },
      y: { ...yAxis, labelFormatter: (value: unknown) => formatRate(Number(value)) },
    },
    style: {
      lineWidth: 2,
      lineDash: ({ direction }: LivePoint) => direction === '上传' ? [4, 4] : [0, 0],
      connect: false,
    },
    point: { shapeField: 'circle', sizeField: 2, tooltip: false },
    tooltip: {
      title: ({ second }: LivePoint) => liveTimeLabel(second),
      items: [({ direction, rate }: LivePoint) => ({ name: direction, value: rate === null ? '—' : formatRate(rate) })],
    },
  }

  return (
    <div className="traffic-live-plot" role="img" aria-label="最近 60 秒的上传和下载速度曲线">
      <Line {...config} />
    </div>
  )
}
