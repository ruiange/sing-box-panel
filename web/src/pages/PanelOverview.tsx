import { ProCard } from '@ant-design/pro-components'
import { Alert, Button, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { Device, DevicePresence, Traffic } from '../api'
import ServerStatusCards from '../components/ServerStatusCards'
import { liveLabels } from '../components/TrafficPanels'
import { formatTime } from '../format'
import { countDeviceStates } from '../trafficAnalysis'
import type { LiveState } from '../useLiveTraffic'
import type { ServerStatusState } from '../useServerStatus'

interface PanelOverviewProps {
  serverStatus: ServerStatusState
  collectionBadge: ReactNode
  traffic?: Traffic
  liveState: LiveState
  devices: Device[]
  presence?: Record<string, DevicePresence>
  loading: boolean
  deviceError: string
}

export default function PanelOverview({
  serverStatus, collectionBadge, traffic, liveState, devices, presence, loading, deviceError,
}: PanelOverviewProps) {
  const enabledCount = devices.filter((device) => device.enabled).length
  const counts = countDeviceStates(devices, liveState === 'live' ? presence : undefined)
  const activeCount = counts.find(({ status }) => status === 'active')?.count ?? 0
  const unknownCount = counts.find(({ status }) => status === 'unknown')?.count ?? 0
  const activityUnavailable = enabledCount > 0 && unknownCount === enabledCount

  return (
    <div className="panel-overview">
      <ServerStatusCards snapshot={serverStatus} />
      <ProCard bordered title={<h2 className="panel-section-title">运行状态</h2>}>
        <dl className="panel-overview-details">
          <dt>采集状态</dt>
          <dd>{collectionBadge}</dd>
          <dt>最近采集</dt>
          <dd>
            {traffic?.collectedAt ? (
              <time dateTime={traffic.collectedAt}>{formatTime(traffic.collectedAt, traffic.timeZone)} · {traffic.timeZone}</time>
            ) : '—'}
          </dd>
          <dt>实时连接</dt>
          <dd>{liveLabels[liveState]}</dd>
        </dl>
      </ProCard>

      <ProCard
        bordered
        title={<h2 className="panel-section-title">设备概况</h2>}
        extra={<Button type="link" href="#devices">管理设备</Button>}
      >
        {deviceError ? (
          <Alert type="error" showIcon message={deviceError} />
        ) : devices.length ? (
          <dl className="panel-overview-details">
            <dt>设备总数</dt>
            <dd>{devices.length} 台</dd>
            <dt>已启用</dt>
            <dd>{enabledCount} 台</dd>
            <dt>近 60 秒有流量</dt>
            <dd>{activityUnavailable ? '状态未知' : `${activeCount} 台`}</dd>
          </dl>
        ) : loading ? (
          <Typography.Text type="secondary" role="status">正在读取设备…</Typography.Text>
        ) : (
          <Typography.Text type="secondary">暂无设备</Typography.Text>
        )}
      </ProCard>
    </div>
  )
}
