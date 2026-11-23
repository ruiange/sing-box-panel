import type { CSSProperties, ReactNode } from 'react'
import type { ServerCapacity } from '../api'
import { formatBytes } from '../format'
import type { ServerStatusState } from '../useServerStatus'
import './server-status.css'

const averageFormat = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 })

function capacityDetails(capacity: ServerCapacity | null | undefined) {
  if (!capacity) return null

  return (
    <>
      <strong>{formatBytes(capacity.usedBytes)}</strong> / <span>{formatBytes(capacity.totalBytes)}</span>
    </>
  )
}

function StatusCard({ label, percent, detail }: { label: string; percent?: number | null; detail?: ReactNode }) {
  const ringStyle = percent == null ? undefined : {
    '--server-status-usage': `${Math.min(100, Math.max(0, percent))}%`,
  } as CSSProperties

  return (
    <li className="server-status-card">
      <h3>{label}</h3>
      <p className="server-status-detail">{detail ?? '—'}</p>
      <div className="server-status-gauge" style={ringStyle} data-unavailable={percent == null || undefined}>
        {percent == null ? '—' : <span>{Math.round(percent)}<small>%</small></span>}
      </div>
    </li>
  )
}

export default function ServerStatusCards({ snapshot }: { snapshot: ServerStatusState }) {
  const status = snapshot.state === 'ready' ? snapshot.status : undefined
  const hasMetrics = status && (status.load || status.cpu || status.memory || status.disk)
  const message = snapshot.state === 'loading' ? '正在读取…' : hasMetrics ? '' : '服务器状态暂不可用'

  return (
    <section className="server-status" aria-labelledby="server-status-title">
      <header className="server-status-header">
        <h2 id="server-status-title">服务器运行状态</h2>
        <span role="status">{message}</span>
      </header>
      <ul className="server-status-grid" role="list">
        <StatusCard label="负载" percent={status?.load?.percent}
          detail={status?.load && `1 分钟均值 ${averageFormat.format(status.load.average1m)}`} />
        <StatusCard label="CPU" percent={status?.cpu?.percent}
          detail={status?.cpu && `${status.cpu.cores} 个逻辑核心`} />
        <StatusCard label="内存" percent={status?.memory?.percent} detail={capacityDetails(status?.memory)} />
        <StatusCard label="根分区 /" percent={status?.disk?.percent} detail={capacityDetails(status?.disk)} />
      </ul>
    </section>
  )
}
