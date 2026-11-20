import { lazy, Suspense } from 'react'
import { ArrowDownOutlined, ArrowUpOutlined } from '@ant-design/icons'
import { ProCard } from '@ant-design/pro-components'
import { Badge, Empty, Select, Spin, Statistic, Typography } from 'antd'
import type { Device, LiveRate, LiveRatePoint, Traffic } from '../api'
import { formatBytes, formatRate, formatTime, totalUsage } from '../format'
import './traffic.css'

const HistoryPlot = lazy(() => import('./TrafficPlots').then(({ HistoryPlot }) => ({ default: HistoryPlot })))
const LiveRatePlot = lazy(() => import('./TrafficPlots').then(({ LiveRatePlot }) => ({ default: LiveRatePlot })))

const periods = [
  { key: 'today', title: '今日用量' },
  { key: 'month', title: '本月用量' },
  { key: 'all', title: '累计用量' },
] as const

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

export const liveLabels = {
  connecting: '正在连接实时速率…',
  waiting: '等待速率采样',
  live: '实时速率更新中',
  reconnecting: '正在重新连接实时速率…',
} as const

interface TrafficPanelsProps {
  traffic?: Traffic
  chartData?: Traffic
  range: HistoryRange
  onRangeChange: (range: HistoryRange) => void
  loading: boolean
  device?: Device
  deviceId: string
  liveState: keyof typeof liveLabels
  rate?: LiveRate
  history: LiveRatePoint[]
  liveEnd: number
}

function TrafficDirections({ download, upload }: { download: string; upload: string }) {
  return (
    <div className="traffic-directions">
      <span><ArrowDownOutlined aria-hidden="true" />下载 <strong>{download}</strong></span>
      <span><ArrowUpOutlined aria-hidden="true" />上传 <strong>{upload}</strong></span>
    </div>
  )
}

function ChartLoading({ children }: { children: string }) {
  return (
    <div className="traffic-chart-state" role="status">
      <Spin size="small" />{children}
    </div>
  )
}

export default function TrafficPanels({
  traffic, chartData, range, onRangeChange, loading, device, deviceId, liveState, rate, history, liveEnd,
}: TrafficPanelsProps) {
  const hourly = range.startsWith('hours=')
  const hourlyData = hourly ? chartData?.hourly : undefined
  const dailyData = hourly ? undefined : chartData?.daily
  const hasHistory = Boolean((hourlyData ?? dailyData)?.length)
  const disabled = device?.enabled === false

  return (
    <div className="traffic-panels">
      <section className="traffic-summary" aria-label={device ? `${device.name}的流量汇总` : '全部设备流量汇总'}>
        {periods.map(({ key, title }) => {
          const usage = traffic?.totals[key]
          return (
            <ProCard key={key} bordered className="traffic-metric">
              <Statistic
                title={title}
                value={usage ? formatBytes(totalUsage(usage)) : '—'}
                formatter={(value) => value}
              />
              {usage ? (
                <TrafficDirections download={formatBytes(usage.download)} upload={formatBytes(usage.upload)} />
              ) : (
                <Typography.Text type="secondary">{loading ? '正在读取流量…' : '等待流量数据'}</Typography.Text>
              )}
            </ProCard>
          )
        })}
      </section>

      {deviceId && (
        <section aria-labelledby="live-heading">
          <ProCard bordered className="traffic-chart-card">
            <div className="traffic-chart-heading">
              <div>
                <h2 id="live-heading" tabIndex={-1}>最近 60 秒网速</h2>
                <Badge
                  status={disabled ? 'default' : liveState === 'live' ? 'success' : liveState === 'waiting' ? 'default' : 'processing'}
                  text={disabled ? '设备已停用' : liveLabels[liveState]}
                />
              </div>
              <TrafficDirections download={rate ? formatRate(rate.download) : '—'} upload={rate ? formatRate(rate.upload) : '—'} />
            </div>
            {history.length ? (
              <Suspense fallback={<ChartLoading>正在加载网速图表…</ChartLoading>}>
                <LiveRatePlot history={history} end={liveEnd} />
              </Suspense>
            ) : (
              <div className="traffic-chart-state"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无速率样本" /></div>
            )}
          </ProCard>
        </section>
      )}

      <section aria-labelledby="trend-heading">
        <ProCard bordered className="traffic-chart-card">
          <div className="traffic-chart-heading">
            <div>
              <h2 id="trend-heading">{device ? `${device.name} · 流量趋势` : '流量趋势'}</h2>
              {traffic?.collectedAt && (
                <Typography.Text type="secondary">
                  最近采集 {formatTime(traffic.collectedAt, traffic.timeZone)} · {traffic.timeZone}
                </Typography.Text>
              )}
            </div>
            <Select<HistoryRange> aria-label="统计范围" value={range} onChange={onRangeChange} options={ranges} />
          </div>
          {hourly && chartData?.hourlySince && (
            <Typography.Text type="secondary" className="traffic-chart-coverage">
              小时统计自 {formatTime(chartData.hourlySince, chartData.timeZone)} 起
            </Typography.Text>
          )}
          {hasHistory ? (
            <Suspense fallback={<ChartLoading>正在加载流量图表…</ChartLoading>}>
              <HistoryPlot daily={dailyData} hourly={hourlyData} timeZone={chartData?.timeZone} />
            </Suspense>
          ) : loading ? (
            <ChartLoading>正在读取趋势…</ChartLoading>
          ) : (
            <div className="traffic-chart-state">
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={hourly ? '所选时段暂无小时统计' : '暂无流量记录'} />
            </div>
          )}
        </ProCard>
      </section>
    </div>
  )
}
