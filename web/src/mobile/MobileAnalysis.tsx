import { Button, Card, ProgressBar, Segmented } from 'antd-mobile'
import { lazy, Suspense, useMemo, useState } from 'react'
import type { PeriodUsage } from '../api'
import { formatBytes, formatTime, totalUsage } from '../format'
import { analyzeTraffic, countDeviceStates, rankDevices } from '../trafficAnalysis'
import { liveLabels } from '../trafficOptions'
import {
  MobileTrafficCoverage, MobileTrafficNotices, MobileTrafficRange, MobileTrafficState,
  type MobileTrafficDataProps,
} from './MobileTrafficControls'
import './mobile-traffic.css'

const AnalysisTrendPlot = lazy(() => import('../components/AnalysisPlots').then(({ AnalysisTrendPlot }) => ({ default: AnalysisTrendPlot })))

type UsagePeriod = keyof PeriodUsage
type TrendMetric = 'total' | 'download' | 'upload'

const trendOptions = [
  { value: 'total', label: '总流量' },
  { value: 'download', label: '下载' },
  { value: 'upload', label: '上传' },
]
const rankOptions = [
  { value: 'today', label: '今日' },
  { value: 'month', label: '本月' },
  { value: 'all', label: '累计' },
]
const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })

export default function MobileAnalysis({
  chartData, deviceError, devices, live, loading, onRangeChange, onTraffic, range, traffic, trafficError,
}: MobileTrafficDataProps) {
  const [metric, setMetric] = useState<TrendMetric>('total')
  const [rankPeriod, setRankPeriod] = useState<UsagePeriod>('month')
  const hourly = range.startsWith('hours=')
  const analysis = useMemo(() => analyzeTraffic(chartData, hourly), [chartData, hourly])
  const total = analysis ? totalUsage(analysis.totals) : 0n
  const hasCollected = Boolean(chartData?.collectedAt) || total > 0n
  const visibleAnalysis = hourly || hasCollected ? analysis : undefined
  const downloadShare = visibleAnalysis && total > 0n ? Number(visibleAnalysis.totals.download) / Number(total) : 0
  const peak = visibleAnalysis?.peak
  const usage = traffic?.devices
  const rankedDevices = useMemo(() => rankDevices(devices, usage, rankPeriod), [devices, usage, rankPeriod])
  const states = countDeviceStates(devices, live.state === 'live' ? live.snapshot?.presence : undefined)
  const loadingAnalysis = loading && !chartData && !trafficError
  const loadingRank = loading && !traffic && !trafficError && !deviceError
  const loadingDevices = loading && !devices.length && !deviceError
  const historyStatus = trafficError ? '流量数据暂时不可用'
    : loadingAnalysis ? '正在读取流量…'
    : !hasCollected && chartData?.collectorStatus === 'error' ? '采集异常，暂无采集记录'
    : hourly ? '所选时段暂无小时采集记录'
    : chartData && !hasCollected ? '等待首次采集' : '所选时段暂无流量记录'
  const rankStatus = deviceError || (!traffic && trafficError) ? '设备用量暂时不可用'
    : loadingRank ? '正在读取设备用量…'
    : !devices.length ? '暂无设备' : '该周期暂无设备用量记录'

  return (
    <div className="mobile-traffic-stack">
      <MobileTrafficNotices deviceError={deviceError} trafficError={trafficError} />
      <MobileTrafficRange value={range} onChange={onRangeChange} />

      <Card className="mobile-traffic-card" title={<h2>区间概况</h2>}>
        {visibleAnalysis ? (
          <>
            <dl className="mobile-analysis-summary">
              <div>
                <dt>总流量</dt>
                <dd>{formatBytes(total)}</dd>
              </div>
              <div>
                <dt>{hourly ? '单小时峰值' : '单日峰值'}</dt>
                <dd>{peak ? formatBytes(totalUsage(peak)) : '—'}</dd>
                {peak && <dd className="mobile-traffic-caption">{peak.label} · 占 {percent.format(Number(totalUsage(peak)) / Number(total))}</dd>}
              </div>
            </dl>
            {chartData?.collectedAt && (
              <p className="mobile-traffic-caption">
                最近采集 <time dateTime={chartData.collectedAt}>{formatTime(chartData.collectedAt, chartData.timeZone)}</time> · {chartData.timeZone}
              </p>
            )}
          </>
        ) : <MobileTrafficState label={historyStatus} loading={loadingAnalysis} />}
      </Card>

      <Card className="mobile-traffic-card" title={<h2>流量趋势</h2>}>
        <div role="group" aria-label="趋势指标">
          <Segmented
            block
            className="mobile-traffic-segment"
            options={trendOptions}
            value={metric}
            onChange={(value) => setMetric(value as TrendMetric)}
          />
        </div>
        {hourly && <MobileTrafficCoverage chartData={chartData} />}
        {visibleAnalysis ? (
          <Suspense fallback={<MobileTrafficState loading label="正在加载分析图表…" />}>
            <AnalysisTrendPlot buckets={visibleAnalysis.buckets} hourly={hourly} metric={metric} />
          </Suspense>
        ) : <MobileTrafficState label={historyStatus} loading={loadingAnalysis} />}
      </Card>

      <Card className="mobile-traffic-card" title={<h2>设备用量排行</h2>}>
        <div role="group" aria-label="排行统计周期">
          <Segmented
            block
            className="mobile-traffic-segment"
            options={rankOptions}
            value={rankPeriod}
            onChange={(value) => setRankPeriod(value as UsagePeriod)}
          />
        </div>
        {rankedDevices.length ? (
          <ol className="mobile-analysis-ranking" role="list">
            {rankedDevices.map((device, index) => {
              const volume = formatBytes(totalUsage(device))
              return (
                <li key={device.id}>
                  <Button
                    className="mobile-analysis-rank-button"
                    fill="none"
                    block
                    onClick={() => onTraffic(device.id)}
                    aria-label={`第 ${index + 1} 名，查看 ${device.name} 的流量，${volume}`}
                  >
                    <span className="mobile-analysis-rank-number">{index + 1}</span>
                    <span className="mobile-analysis-rank-name">{device.name}</span>
                    <strong>{volume}</strong>
                    <span className="mobile-traffic-filter-chevron" aria-hidden="true" />
                  </Button>
                </li>
              )
            })}
          </ol>
        ) : <MobileTrafficState label={rankStatus} loading={loadingRank} />}
      </Card>

      <Card className="mobile-traffic-card" title={<h2>上传下载占比</h2>}>
        {visibleAnalysis && total > 0n ? (
          <>
            <ProgressBar
              aria-hidden="true"
              className="mobile-analysis-share"
              percent={downloadShare * 100}
              style={{ '--track-width': '8px', '--fill-color': '#2468d6', '--track-color': '#118381' }}
            />
            <dl className="mobile-analysis-summary">
              <div data-direction="download">
                <dt>下载 · {percent.format(downloadShare)}</dt>
                <dd>{formatBytes(visibleAnalysis.totals.download)}</dd>
              </div>
              <div data-direction="upload">
                <dt>上传 · {percent.format(1 - downloadShare)}</dt>
                <dd>{formatBytes(visibleAnalysis.totals.upload)}</dd>
              </div>
            </dl>
          </>
        ) : <MobileTrafficState label={visibleAnalysis ? '所选时段流量为 0 B' : historyStatus} loading={loadingAnalysis} />}
      </Card>

      <Card className="mobile-traffic-card" title={<h2>设备状态分布</h2>}>
        {devices.length ? (
          <>
            <dl className="mobile-analysis-status">
              {states.map(({ status, label, count }) => (
                <div key={status} data-status={status}>
                  <dt>{label}</dt>
                  <dd>{count} 台</dd>
                </div>
              ))}
            </dl>
            <p className="mobile-traffic-caption">{liveLabels[live.state]} · 活跃表示近 60 秒有流量</p>
          </>
        ) : (
          <MobileTrafficState
            label={deviceError ? '设备状态暂时不可用' : loadingDevices ? '正在读取设备…' : '暂无设备'}
            loading={loadingDevices}
          />
        )}
      </Card>
    </div>
  )
}
