import { useMemo, useState } from 'react'
import { ProCard } from '@ant-design/pro-components'
import { Alert, Badge, Empty, Progress, Select, Spin, Statistic, Tabs, Typography } from 'antd'
import type { Device, DevicePresence, PeriodUsage, Traffic } from '../api'
import { AnalysisDeviceStatusPlot, AnalysisDirectionPlot, AnalysisMiniPlot, AnalysisTrendPlot } from '../components/AnalysisPlots'
import { ranges, type HistoryRange } from '../components/TrafficPanels'
import { formatBytes, formatTime, totalUsage } from '../format'
import { analyzeTraffic, countDeviceStates, rankDevices } from '../trafficAnalysis'
import type { LiveState } from '../useLiveTraffic'
import './traffic-analysis.css'

type UsagePeriod = keyof PeriodUsage

const trendTabs = [
  { key: 'total', label: '总流量' },
  { key: 'download', label: '下载' },
  { key: 'upload', label: '上传' },
] as const

type TrendMetric = (typeof trendTabs)[number]['key']

const rankPeriods: Array<{ label: string; value: UsagePeriod }> = [
  { label: '今日', value: 'today' },
  { label: '本月', value: 'month' },
  { label: '累计', value: 'all' },
]

const directionLabels = [
  { key: 'download', label: '下载' },
  { key: 'upload', label: '上传' },
] as const

const liveLabels: Record<LiveState, string> = {
  connecting: '正在连接',
  waiting: '等待实时采样',
  live: '实时采样',
  reconnecting: '正在重连',
}

const formatShare = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

interface TrafficAnalysisProps {
  range: HistoryRange
  onRangeChange: (range: HistoryRange) => void
  chartData?: Traffic
  traffic?: Traffic
  devices: Device[]
  presence?: Record<string, DevicePresence>
  liveState: LiveState
  loading: boolean
  trafficError?: string
  deviceError: string
}

function ChartState({ label, loading }: { label: string; loading?: boolean }) {
  return (
    <div className="analysis-chart-state" role={loading ? 'status' : undefined}>
      {loading ? <><Spin size="small" />{label}</> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={label} />}
    </div>
  )
}

export default function TrafficAnalysis({
  range, onRangeChange, chartData, traffic, devices, presence, liveState, loading, trafficError, deviceError,
}: TrafficAnalysisProps) {
  const [metric, setMetric] = useState<TrendMetric>('total')
  const [rankPeriod, setRankPeriod] = useState<UsagePeriod>('month')
  const hourly = range.startsWith('hours=')
  const analysis = useMemo(() => analyzeTraffic(chartData, hourly), [chartData, hourly])
  const total = analysis ? totalUsage(analysis.totals) : 0n
  const hasCollected = Boolean(chartData?.collectedAt) || total > 0n
  const visibleAnalysis = hourly || hasCollected ? analysis : undefined
  const peak = visibleAnalysis?.peak
  const peakShare = peak ? Number(totalUsage(peak)) / Number(total) : undefined
  const collectionTime = chartData?.collectedAt ? formatTime(chartData.collectedAt, chartData.timeZone) : undefined
  const usage = traffic?.devices
  const rankedDevices = useMemo(() => rankDevices(devices, usage, rankPeriod), [devices, usage, rankPeriod])
  const livePresence = liveState === 'live' ? presence : undefined
  const deviceStates = useMemo(() => countDeviceStates(devices, livePresence), [devices, livePresence])

  const loadingAnalysis = loading && !chartData && !trafficError
  const loadingDevices = loading && !traffic && !devices.length && !deviceError
  const loadingRank = loading && !traffic && !trafficError && !deviceError
  const historyEmpty = trafficError ? '流量数据暂时不可用'
    : !hasCollected && chartData?.collectorStatus === 'error' ? '采集异常，暂无采集记录'
    : hourly ? '所选时段暂无小时采集记录'
    : chartData && !hasCollected ? '等待首次采集' : '所选时段暂无流量记录'
  const rankEmpty = (!traffic && trafficError) || (!devices.length && deviceError) ? '设备用量暂时不可用'
    : !devices.length ? '暂无设备' : '该周期暂无设备用量记录'
  const plotLoadingLabel = '正在读取流量…'
  const metrics = [
    {
      key: 'total',
      title: '区间总流量',
      value: visibleAnalysis && formatBytes(total),
      footer: '最近采集',
      detail: collectionTime,
      preview: hourly && visibleAnalysis && <Typography.Text type="secondary">采集小时数 {visibleAnalysis.buckets.length}</Typography.Text>,
    },
    ...directionLabels.map(({ key, label }) => ({
      key,
      title: label + '流量',
      value: visibleAnalysis && formatBytes(visibleAnalysis.totals[key]),
      footer: '占区间总流量',
      detail: visibleAnalysis && total > 0n ? formatShare.format(Number(visibleAnalysis.totals[key]) / Number(total)) : undefined,
      preview: visibleAnalysis && BigInt(visibleAnalysis.totals[key]) > 0n && <AnalysisMiniPlot buckets={visibleAnalysis.buckets} direction={key} />,
    })),
    {
      key: 'peak',
      title: hourly ? '单小时峰值' : '单日峰值',
      value: peak && formatBytes(totalUsage(peak)),
      footer: '峰值时段',
      detail: peak?.label,
      preview: peakShare !== undefined && (
        <div className="analysis-peak-progress" title="峰值占区间总流量比例">
          <Progress aria-label="峰值占区间总流量比例" percent={peakShare * 100} size="small" showInfo={false} strokeColor="#145abb" />
          <Typography.Text type="secondary">{formatShare.format(peakShare)}</Typography.Text>
        </div>
      ),
    },
  ]

  const trendContent = (
    <div className="analysis-trend-grid">
      <section className="analysis-trend-panel" aria-labelledby="analysis-trend-heading">
        <h2 className="panel-section-title" id="analysis-trend-heading">
          {metric === 'total' ? '总流量趋势' : metric === 'download' ? '下载流量趋势' : '上传流量趋势'}
        </h2>
        <div className="analysis-chart-caption">
          {collectionTime && <Typography.Text type="secondary" className="analysis-caption">时区 {chartData?.timeZone}</Typography.Text>}
          {hourly && chartData?.hourlySince && (
            <Typography.Text type="secondary" className="analysis-caption">
              小时统计自 {formatTime(chartData.hourlySince, chartData.timeZone)} 起，仅显示已有采集记录的小时
            </Typography.Text>
          )}
        </div>
        {visibleAnalysis ? <AnalysisTrendPlot buckets={visibleAnalysis.buckets} hourly={hourly} metric={metric} /> : (
          <ChartState label={loadingAnalysis ? plotLoadingLabel : historyEmpty} loading={loadingAnalysis} />
        )}
      </section>

      <section className="analysis-rank-panel" aria-labelledby="analysis-rank-heading">
        <div className="analysis-rank-heading">
          <h2 className="panel-section-title" id="analysis-rank-heading">设备用量排行</h2>
          <Select<UsagePeriod> aria-label="排行统计周期" size="small" value={rankPeriod} options={rankPeriods} onChange={setRankPeriod} />
        </div>
        {rankedDevices.length > 0 ? (
          <ol className="analysis-rank" role="list">
            {rankedDevices.map((device, index) => {
              const bytes = totalUsage(device)
              return (
                <li key={device.id}>
                <span className="analysis-rank-number" aria-hidden="true">{index + 1}</span>
                  <a className="analysis-rank-link" href={'#traffic?deviceId=' + encodeURIComponent(device.id)} title={device.name}>
                    {device.name}
                  </a>
                  <strong title={bytes.toLocaleString('zh-CN') + ' B'}>{formatBytes(bytes)}</strong>
                </li>
              )
            })}
          </ol>
        ) : (
          <ChartState label={loadingRank ? '正在读取设备用量…' : rankEmpty} loading={loadingRank} />
        )}
      </section>
    </div>
  )

  return (
    <div className="analysis-page">
      {trafficError && <Alert type="error" showIcon message="流量数据读取失败" description={trafficError} />}
      {deviceError && <Alert type="error" showIcon message="设备列表读取失败" description={deviceError} />}

      <section className="analysis-summary" aria-label="区间流量汇总">
        {metrics.map(({ key, title, value, footer, detail, preview }) => (
          <ProCard key={key} bordered headerBordered className="analysis-metric" title={<h2 className="analysis-metric-title">{title}</h2>}>
            <Statistic value={value ?? '—'} formatter={(content) => content} loading={loadingAnalysis} />
            <div className="analysis-metric-preview">{preview}</div>
            <div className="analysis-metric-footer">
              <Typography.Text type="secondary">{footer}</Typography.Text>
              <span>{detail ?? '—'}</span>
            </div>
          </ProCard>
        ))}
      </section>

      <ProCard bordered className="analysis-main">
        <Tabs
          className="analysis-tabs"
          activeKey={metric}
          animated={false}
          tabBarGutter={24}
          items={trendTabs.map((tab) => ({ ...tab, children: tab.key === metric ? trendContent : undefined }))}
          onChange={(key) => setMetric(key as TrendMetric)}
          tabBarExtraContent={(
            <Select<HistoryRange> aria-label="分析统计范围" className="analysis-range" value={range} options={ranges} onChange={onRangeChange} />
          )}
        />
      </ProCard>

      <div className="analysis-breakdown">
        <ProCard
          bordered
          headerBordered
          title={<h2 className="panel-section-title">设备状态分布</h2>}
          extra={devices.length > 0 && (
            <Badge status={liveState === 'live' ? 'success' : liveState === 'waiting' ? 'default' : 'processing'} text={liveLabels[liveState]} />
          )}
        >
          {devices.length > 0 ? (
            <>
              <Typography.Text type="secondary" className="analysis-caption">活跃表示最近 60 秒内观察到上传或下载流量</Typography.Text>
              <AnalysisDeviceStatusPlot counts={deviceStates} />
              <ul className="analysis-legend" aria-label="设备状态分布明细">
                {deviceStates.map(({ status, label, count }) => (
                  <li key={status}>
                    <i className="analysis-swatch" data-color={status} aria-hidden="true" />
                    <span>{label}</span>
                    <strong>{count} 台</strong>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <ChartState label={loadingDevices ? '正在读取设备…' : deviceError ? '设备状态暂时不可用' : '暂无设备'} loading={loadingDevices} />
          )}
        </ProCard>

        <ProCard bordered headerBordered title={<h2 className="panel-section-title">上传下载占比</h2>}>
          {visibleAnalysis && total > 0n ? (
            <>
              <AnalysisDirectionPlot totals={visibleAnalysis.totals} />
              <ul className="analysis-legend" aria-label="上传下载占比明细">
                {directionLabels.map(({ key, label }) => (
                  <li key={key}>
                    <i className="analysis-swatch" data-color={key} aria-hidden="true" />
                    <span>{label}</span>
                    <strong>{formatBytes(visibleAnalysis.totals[key])}</strong>
                    <Typography.Text type="secondary">{formatShare.format(Number(visibleAnalysis.totals[key]) / Number(total))}</Typography.Text>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <ChartState label={loadingAnalysis ? plotLoadingLabel : visibleAnalysis ? '所选时段流量为 0 B' : historyEmpty} loading={loadingAnalysis} />
          )}
        </ProCard>
      </div>
    </div>
  )
}
