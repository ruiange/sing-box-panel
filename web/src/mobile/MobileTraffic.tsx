import { Card, Tag } from 'antd-mobile'
import { lazy, Suspense } from 'react'
import { formatBytes, formatRate, formatTime, totalUsage } from '../format'
import { liveLabels } from '../trafficOptions'
import {
  MobileTrafficCoverage, MobileTrafficNotices, MobileTrafficRange, MobileTrafficScope, MobileTrafficState,
  type MobileTrafficDataProps,
} from './MobileTrafficControls'
import './mobile-traffic.css'

const HistoryPlot = lazy(() => import('../components/TrafficPlots').then(({ HistoryPlot }) => ({ default: HistoryPlot })))
const LiveRatePlot = lazy(() => import('../components/TrafficPlots').then(({ LiveRatePlot }) => ({ default: LiveRatePlot })))

const periods = [
  { key: 'today', label: '今日' },
  { key: 'month', label: '本月' },
  { key: 'all', label: '累计' },
] as const

function Directions({ download, upload }: { download: string; upload: string }) {
  return (
    <div className="mobile-traffic-directions">
      <span>下载 <strong>{download}</strong></span>
      <span>上传 <strong>{upload}</strong></span>
    </div>
  )
}

export default function MobileTraffic({
  chartData, deviceError, devices, live, loading, onRangeChange, onTraffic, range, scope, traffic, trafficError,
}: MobileTrafficDataProps & { scope: string }) {
  const device = devices.find((entry) => entry.id === scope)
  const snapshot = live.state === 'live' ? live.snapshot : undefined
  const disabled = device?.enabled === false
  const history = !disabled && snapshot?.deviceId === scope ? snapshot.history : undefined
  const rate = device?.enabled ? snapshot?.devices[scope] : undefined
  const hourly = range.startsWith('hours=')
  const hourlyData = hourly ? chartData?.hourly : undefined
  const dailyData = hourly ? undefined : chartData?.daily
  const hasHistory = Boolean((hourlyData ?? dailyData)?.length)
  const waitingForTrend = loading && !chartData && !trafficError

  return (
    <div className="mobile-traffic-stack">
      <MobileTrafficNotices deviceError={deviceError} trafficError={trafficError} />
      <div className="mobile-traffic-filters">
        <MobileTrafficScope value={scope} devices={devices} onChange={onTraffic} />
        <MobileTrafficRange value={range} onChange={onRangeChange} />
      </div>

      {scope && (
        <Card
          className="mobile-traffic-card"
          title={<h2 id="live-heading" tabIndex={-1}>最近 60 秒网速</h2>}
        >
          <div className="mobile-traffic-live-status">
            <Tag fill="outline" color={live.state === 'live' && !disabled ? '#246842' : '#52647e'}>
              {disabled ? '设备已停用' : liveLabels[live.state]}
            </Tag>
            <Directions download={rate ? formatRate(rate.download) : '—'} upload={rate ? formatRate(rate.upload) : '—'} />
          </div>
          {history?.length && snapshot?.at ? (
            <Suspense fallback={<MobileTrafficState loading label="正在加载网速曲线…" />}>
              <LiveRatePlot history={history} end={Date.parse(snapshot.at)} />
            </Suspense>
          ) : (
            <MobileTrafficState label={disabled ? '设备停用期间不采集实时网速' : '暂无速率样本'} />
          )}
        </Card>
      )}

      <Card className="mobile-traffic-card" title={<h2>流量汇总</h2>}>
        {traffic ? (
          <dl className="mobile-usage-totals">
            {periods.map(({ key, label }) => {
              const usage = traffic.totals[key]
              return (
                <div key={key} className="mobile-usage-row">
                  <dt>{label}</dt>
                  <dd>
                    <strong>{formatBytes(totalUsage(usage))}</strong>
                    <Directions download={formatBytes(usage.download)} upload={formatBytes(usage.upload)} />
                  </dd>
                </div>
              )
            })}
          </dl>
        ) : (
          <MobileTrafficState
            loading={loading && !trafficError}
            label={trafficError ? '暂时无法读取用量' : loading ? '正在读取用量…' : '暂无流量数据'}
          />
        )}
      </Card>

      <Card className="mobile-traffic-card" title={<h2>流量趋势</h2>}>
        {traffic?.collectedAt && (
          <p className="mobile-traffic-caption">
            最近采集 <time dateTime={traffic.collectedAt}>{formatTime(traffic.collectedAt, traffic.timeZone)}</time> · {traffic.timeZone}
          </p>
        )}
        {hourly && <MobileTrafficCoverage chartData={chartData} />}
        {hasHistory ? (
          <Suspense fallback={<MobileTrafficState loading label="正在加载流量图表…" />}>
            <HistoryPlot daily={dailyData} hourly={hourlyData} timeZone={chartData?.timeZone} />
          </Suspense>
        ) : (
          <MobileTrafficState
            loading={waitingForTrend}
            label={trafficError ? '暂时无法读取趋势' : waitingForTrend ? '正在读取趋势…'
              : hourly ? '所选时段暂无小时统计' : '暂无流量记录'}
          />
        )}
      </Card>
    </div>
  )
}
